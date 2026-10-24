"""Tests for ticket-guard's link policy, notifications, and GitHub API client."""

import io
import json
import unittest
from contextlib import redirect_stdout
from unittest.mock import MagicMock, patch
from urllib.error import HTTPError

from ticket_guard import GitHubAPI, GitHubAPIError, ticket_guard


JIRA = "https://redhat.atlassian.net/browse/MTA-8073"
ROOT = "/repos/migtools/crane"
NOTICE = {
    "id": 99,
    "user": {"login": "github-actions[bot]"},
    "body": f"<!-- ticket-guard -->\nMissing links, for example #123 or {JIRA}",
}


class FakeAPI:
    def __init__(self, body="", pr=True, comments=(), inline=(), reviews=(), issues=None):
        self.item = {"number": 10, "state": "open", "body": body}
        if pr:
            self.item["pull_request"] = {}
        self.issues = issues or {}
        self.writes = []
        self.lists = {
            f"{ROOT}/issues/10/comments": list(comments),
            f"{ROOT}/pulls/10/comments": list(inline),
            f"{ROOT}/pulls/10/reviews": list(reviews),
        }

    def request(self, method, path, data=None):
        if method != "GET":
            self.writes.append((method, path, data))
            return
        number = int(path.rsplit("/", 1)[1])
        if number == 10:
            return self.item
        if number not in self.issues:
            raise GitHubAPIError(method, path, 404, "Not found")
        candidate = self.issues[number]
        if isinstance(candidate, Exception):
            raise candidate
        return candidate

    def paginate(self, path):
        return self.lists[path]

    def run(self, **kwargs):
        with redirect_stdout(io.StringIO()):
            ticket_guard(self, "migtools/crane", {"issue": {"number": 10}}, **kwargs)


class TicketGuardTests(unittest.TestCase):
    def test_pr_requests_either_issue_or_jira_link(self):
        api = FakeAPI()
        api.run()
        self.assertEqual(len(api.writes), 1)
        method, path, data = api.writes[0]
        self.assertEqual((method, path), ("POST", f"{ROOT}/issues/10/comments"))
        self.assertIn("existing GitHub issue", data["body"])
        self.assertIn("OR a Jira ticket link", data["body"])
        self.assertNotIn("`jira` label", data["body"])

    def test_pr_accepts_issue_without_jira(self):
        api = FakeAPI(body="Fixes #12", issues={12: {}})
        api.run()
        self.assertEqual(api.writes, [])

    def test_pr_accepts_jira_without_issue(self):
        api = FakeAPI(body=JIRA)
        api.run()
        self.assertEqual(api.writes, [])

    def test_issue_requires_jira_and_suggests_label(self):
        api = FakeAPI(pr=False, body="Related to #12", issues={12: {}})
        api.run()
        body = api.writes[0][2]["body"]
        self.assertIn("a Jira ticket link", body)
        self.assertIn("add the `jira` label to this issue to create one automatically", body)
        self.assertNotIn("existing GitHub issue", body)

    def test_jira_label_does_not_replace_ticket_link(self):
        api = FakeAPI(pr=False)
        api.item["labels"] = [{"name": "jira"}]
        api.run()
        self.assertIn("a Jira ticket link", api.writes[0][2]["body"])

    def test_issues_only_require_jira_and_accept_other_bots(self):
        api = FakeAPI(pr=False, comments=[{"body": JIRA, "user": {"login": "jira[bot]"}}])
        api.run()
        self.assertEqual(api.writes, [])

    def test_accepts_existing_issues_and_links_in_any_comment_type(self):
        for reference in ("Fixes #12", "https://github.com/migtools/crane/issues/12"):
            for source in ("comments", "inline", "reviews"):
                with self.subTest(reference=reference, source=source):
                    api = FakeAPI(issues={12: {}}, **{source: [{"body": reference}]})
                    api.run()
                    self.assertEqual(api.writes, [])

    def test_pr_accepts_jira_in_any_comment_type(self):
        for source in ("comments", "inline", "reviews"):
            with self.subTest(source=source):
                api = FakeAPI(**{source: [{"body": JIRA}]})
                api.run()
                self.assertEqual(api.writes, [])

    def test_rejects_missing_issues_prs_self_and_other_repositories(self):
        for reference in ("#404", "#11", "#10", "https://github.com/other/crane/issues/12"):
            with self.subTest(reference=reference):
                api = FakeAPI(body=reference, issues={11: {"pull_request": {}}, 12: {}})
                api.run()
                self.assertIn("existing GitHub issue", api.writes[0][2]["body"])
                self.assertIn("OR a Jira ticket link", api.writes[0][2]["body"])

    def test_ignores_and_updates_own_notification(self):
        api = FakeAPI(comments=[NOTICE], issues={123: {}})
        api.run()
        self.assertEqual(len(api.writes), 1)
        method, path, data = api.writes[0]
        self.assertEqual((method, path), ("PATCH", f"{ROOT}/issues/comments/99"))
        self.assertIn("existing GitHub issue", data["body"])
        self.assertIn("a Jira ticket link", data["body"])

    def test_deletes_obsolete_notification(self):
        for body in ("Fixes #12", JIRA, f"Fixes #12\n{JIRA}"):
            with self.subTest(body=body):
                api = FakeAPI(body=body, issues={12: {}}, comments=[NOTICE])
                api.run()
                self.assertEqual(api.writes, [("DELETE", f"{ROOT}/issues/comments/99", None)])

    def test_deletes_issue_notification_after_jira_link_is_added(self):
        api = FakeAPI(pr=False, body=JIRA, comments=[NOTICE])
        api.run()
        self.assertEqual(api.writes, [("DELETE", f"{ROOT}/issues/comments/99", None)])

    def test_does_not_rewrite_unchanged_notification(self):
        api = FakeAPI()
        api.run()
        notice = {**NOTICE, "body": api.writes[0][2]["body"]}
        api = FakeAPI(comments=[notice])
        api.run()
        self.assertEqual(api.writes, [])

    def test_reports_link_removed_from_comment(self):
        api = FakeAPI(pr=False, comments=[{"body": "Link removed."}])
        api.run()
        self.assertIn("a Jira ticket link", api.writes[0][2]["body"])

    def test_handles_malformed_urls_and_trailing_punctuation(self):
        api = FakeAPI(body=f"https://% https://example.com:bad Fixes #12. See {JIRA}.", issues={12: {}})
        api.run()
        self.assertEqual(api.writes, [])

    def test_rejects_wrong_jira_host_and_invalid_ticket_path(self):
        for body in (
            "https://example.com/browse/MTA-8073",
            "https://redhat.atlassian.net/browse/not-a-ticket",
            "https://redhat.atlassian.net/browse/MTRHO-123",
            "https://redhat.atlassian.net/browse/MTA-0",
            "https://redhat.atlassian.net/browse/MTA-8073extra",
        ):
            with self.subTest(body=body):
                api = FakeAPI(pr=False, body=body)
                api.run()
                self.assertIn("a Jira ticket link", api.writes[0][2]["body"])

    def test_accepts_configured_jira_url(self):
        api = FakeAPI(pr=False, body="https://jira.example.com/jira/browse/MTA-8073")
        api.run(jira_base_url="https://jira.example.com/jira/")
        self.assertEqual(api.writes, [])

    def test_propagates_api_failures(self):
        error = GitHubAPIError("GET", f"{ROOT}/issues/12", 403, "Rate limited")
        api = FakeAPI(body="#12", issues={12: error})
        with self.assertRaisesRegex(GitHubAPIError, "Rate limited"):
            api.run()
        self.assertEqual(api.writes, [])

    def test_skips_closed_items(self):
        api = FakeAPI()
        api.item["state"] = "closed"
        api.run()
        self.assertEqual(api.writes, [])


class GitHubAPITests(unittest.TestCase):
    @patch("ticket_guard.urlopen")
    def test_sends_authenticated_json_and_handles_empty_response(self, urlopen):
        response = urlopen.return_value.__enter__.return_value
        response.read.return_value = b""
        api = GitHubAPI("test-token")
        self.assertIsNone(api.request("POST", "/test", {"body": "Hello"}))
        request = urlopen.call_args.args[0]
        self.assertEqual(request.full_url, "https://api.github.com/test")
        self.assertEqual(request.get_header("Authorization"), "Bearer test-token")
        self.assertEqual(json.loads(request.data), {"body": "Hello"})
        self.assertEqual(urlopen.call_args.kwargs["timeout"], 30)

    def test_paginates_all_comments(self):
        api = GitHubAPI("test-token")
        api.request = MagicMock(side_effect=[[{}] * 100, [{"id": 101}]])
        self.assertEqual(len(api.paginate("/comments")), 101)
        self.assertEqual(api.request.call_args_list[1].args, ("GET", "/comments?per_page=100&page=2"))

    @patch("ticket_guard.urlopen")
    def test_api_errors_include_method_resource_and_status(self, urlopen):
        urlopen.side_effect = HTTPError(
            "https://api.github.com/test", 403, "Forbidden", {}, io.BytesIO(b"Rate limited")
        )
        with self.assertRaisesRegex(GitHubAPIError, "GET https://api.github.com/test returned 403: Rate limited"):
            GitHubAPI("test-token").request("GET", "/test")


if __name__ == "__main__":
    unittest.main()
