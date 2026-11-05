"""Check PR and issue links using only the Python standard library."""

import json
import os
import re
from urllib.error import HTTPError
from urllib.parse import urlsplit
from urllib.request import Request, urlopen


MARKER = "<!-- ticket-guard -->"
DEFAULT_JIRA_BASE_URL = "https://redhat.atlassian.net"


class GitHubAPIError(Exception):
    def __init__(self, method, url, status, detail):
        self.status = status
        super().__init__(f"GitHub API {method} {url} returned {status}: {detail}")


class GitHubAPI:
    def __init__(self, token, base_url="https://api.github.com"):
        self.token = token
        self.base_url = base_url.rstrip("/")

    def request(self, method, path, data=None):
        url = f"{self.base_url}{path}"
        request = Request(
            url,
            method=method,
            data=json.dumps(data).encode("utf-8") if data is not None else None,
            headers={
                "Authorization": f"Bearer {self.token}",
                "Accept": "application/vnd.github+json",
                "Content-Type": "application/json",
                "User-Agent": "ticket-guard",
                "X-GitHub-Api-Version": "2022-11-28",
            },
        )
        try:
            with urlopen(request, timeout=30) as response:
                body = response.read()
        except HTTPError as error:
            detail = error.read().decode("utf-8", errors="replace")
            raise GitHubAPIError(method, url, error.code, detail) from error
        return json.loads(body) if body else None

    def paginate(self, path):
        items = []
        page = 1
        while True:
            batch = self.request("GET", f"{path}?per_page=100&page={page}")
            items.extend(batch)
            if len(batch) < 100:
                return items
            page += 1


def is_ticket_guard_comment(comment):
    return (
        comment.get("user", {}).get("login") == "github-actions[bot]"
        and MARKER in (comment.get("body") or "")
    )


def urls(text):
    for value in re.findall(r"https?://[^\s<>\[\]()]+", text):
        try:
            url = urlsplit(value.rstrip(".,;!?"))
            if url.hostname and "%" not in url.hostname:
                # Accessing the port also validates malformed port values.
                url.port
                yield url
        except ValueError:
            continue


def origin(url):
    return url.scheme, url.hostname, url.port or (443 if url.scheme == "https" else 80)


def issue_numbers(text, owner, repo, server_url):
    numbers = set()
    server = urlsplit(server_url)
    # Initial policy: only references to issues in this repository count.
    for url in urls(text):
        parts = url.path.split("/")
        if (
            origin(url) == origin(server)
            and len(parts) >= 5
            and parts[1].lower() == owner.lower()
            and parts[2].lower() == repo.lower()
            and parts[3] == "issues"
            and re.fullmatch(r"[1-9][0-9]*", parts[4])
        ):
            numbers.add(int(parts[4]))
    numbers.update(int(value) for value in re.findall(r"(?:^|[\s(])#([1-9][0-9]*)\b", text))
    return sorted(numbers)


def has_jira_link(text, base_url):
    base = urlsplit(base_url)
    if base.scheme not in ("https", "http") or not base.hostname:
        raise ValueError(f"TICKET_GUARD_JIRA_BASE_URL must be an HTTP(S) URL, got {base_url!r}")
    prefix = f"{base.path.rstrip('/')}/browse/"
    return any(
        origin(url) == origin(base)
        and url.path.startswith(prefix)
        and re.fullmatch(r"MTA-[1-9][0-9]*/?", url.path[len(prefix):])
        for url in urls(text)
    )


def ticket_guard(api, repository, event, jira_base_url=DEFAULT_JIRA_BASE_URL,
                 server_url="https://github.com"):
    owner, repo = repository.split("/")
    number = (event.get("pull_request") or event["issue"])["number"]
    root = f"/repos/{owner}/{repo}"
    issue_path = f"{root}/issues/{number}"
    # Fetch current state, since events may have waited in the concurrency queue.
    item = api.request("GET", issue_path)
    if item["state"] != "open":
        return

    comments = api.paginate(f"{issue_path}/comments")
    notifications = [comment for comment in comments if is_ticket_guard_comment(comment)]
    bodies = [item.get("body")]
    bodies.extend(comment.get("body") for comment in comments if not is_ticket_guard_comment(comment))
    is_pr = "pull_request" in item
    if is_pr:
        for endpoint in ("comments", "reviews"):
            entries = api.paginate(f"{root}/pulls/{number}/{endpoint}")
            bodies.extend(entry.get("body") for entry in entries)
    text = "\n".join(body for body in bodies if body)
    missing = []
    # TODO: Agree on Jira API ticket validation.
    jira_base_url = jira_base_url.rstrip("/")
    linked_jira = has_jira_link(text, jira_base_url)
    jira_requirement = f"a Jira ticket link ({jira_base_url}/browse/MTA-8073)"

    if is_pr and not linked_jira:
        linked_issue = False
        for candidate_number in issue_numbers(text, owner, repo, server_url):
            if candidate_number == number:
                continue
            try:
                candidate = api.request("GET", f"{root}/issues/{candidate_number}")
            except GitHubAPIError as error:
                if error.status != 404:
                    raise
                continue
            if "pull_request" not in candidate:
                linked_issue = True
                break
        if not linked_issue:
            missing.append(
                "a link to an existing GitHub issue in this repository OR " + jira_requirement
            )

    if not is_pr and not linked_jira:
        missing.append(jira_requirement)

    if missing:
        body = (
            f"{MARKER}\n### Missing links\n\n"
            "Please add the following to the description or a comment:\n\n"
            + "\n".join(f"- {link}" for link in missing)
        )
        if not is_pr:
            body += (
                "\n\nIf you do not have a Jira ticket yet, add the `jira` label "
                "to this issue to create one automatically."
            )
        body += "\n\nticket-guard checks the description and comments again when they change."
        if not notifications:
            api.request("POST", f"{issue_path}/comments", {"body": body})
        elif notifications[0]["body"] != body:
            api.request("PATCH", f"{root}/issues/comments/{notifications[0]['id']}", {"body": body})

    # Remove obsolete notices, including duplicates left by earlier runs.
    for comment in notifications[1 if missing else 0:]:
        api.request("DELETE", f"{root}/issues/comments/{comment['id']}")
    print(f"Missing links: {', '.join(missing)}" if missing else "Required links found.")


def main():
    with open(os.environ["GITHUB_EVENT_PATH"], encoding="utf-8") as event_file:
        event = json.load(event_file)
    api = GitHubAPI(os.environ["GITHUB_TOKEN"], os.environ.get("GITHUB_API_URL", "https://api.github.com"))
    ticket_guard(
        api,
        os.environ["GITHUB_REPOSITORY"],
        event,
        os.environ.get("TICKET_GUARD_JIRA_BASE_URL") or DEFAULT_JIRA_BASE_URL,
        os.environ.get("GITHUB_SERVER_URL", "https://github.com"),
    )


if __name__ == "__main__":
    main()
