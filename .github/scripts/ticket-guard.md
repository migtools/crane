# ticket-guard

The `ticket-guard` workflow checks new and edited PRs and issues, including reopened items.
It also checks created, edited, and deleted discussion and inline review comments,
and submitted, edited, or dismissed reviews. Closed items are skipped.

## Initial policy

- PRs need either a reference to an existing GitHub issue in this repository or a
  Jira ticket URL. One link is enough.
- Issues need a Jira ticket URL. If it is missing, the notification suggests adding
  the `jira` label to the issue to create a ticket automatically. The label itself
  does not replace the required link. Ticket creation is handled by separate automation.
- Links can appear in the description or any discussion or review comment.
- GitHub issue references can use `#123` or a full issue URL. A reference to another
  PR does not count. Both open and closed issues count.
- Jira URLs use `https://redhat.atlassian.net/browse/MTA-8073` by default.
  Only ticket keys in the `MTA` project are accepted.
  Set the repository Actions variable `TICKET_GUARD_JIRA_BASE_URL` to change the base URL.

ticket-guard creates one English notification when links are missing. It updates that
comment when the missing links change and removes it when all required links exist.
Its own comments do not count as links or trigger another check.
Missing links do not fail the job. API or configuration errors do.

The workflow executes scripts from the trusted base/default branch using
`pull_request_target` for PR events. It never executes PR head code.
Issue and comment triggers require the workflow on the default branch.

## Details to decide

- Whether to verify Jira tickets via the Jira API.
- Whether cross-repository references and GitHub development-sidebar links should count.
- Whether some items should be exempt or missing links should block merging.
- How to handle review events from fork PRs. GitHub restricts their token to
  read-only access, so posting a notification from those events needs a separate
  privileged follow-up workflow. PR description and discussion-comment events
  already support fork PRs through the triggers used here.

The current implementation checks textual references, not GitHub's development-sidebar
relationships. Jira validation only checks the configured host and ticket path.

The implementation uses Python 3 and its standard library, with no third-party
dependencies. GitHub Actions supplies the token, repository, API URL, and event
file through environment variables.

Run the tests with `python3 -B -m unittest discover -s .github/scripts -p 'test_*.py'`.
