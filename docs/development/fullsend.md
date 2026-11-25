# Using Fullsend

Fullsend helps with issue triage, implementation, pull request review, fixes,
retrospectives, and prioritization in `migtools/crane`. Request work by posting
a command in a GitHub issue or pull request comment. You do not need a local
Fullsend installation or model credentials to use it through GitHub.

For configuration, deployment status, GitHub Apps, models, and administration,
see [Fullsend setup and operations](fullsend_setup.md).

## Quick start

1. Open or choose a focused issue or PR in
   [migtools/crane](https://github.com/migtools/crane).
2. Describe the expected behavior and how the result should be checked.
3. Post one of the commands below as a new comment, with the command at the
   start of the comment.
4. Follow the agent's status comments and the linked GitHub Actions run.
5. Review the result and CI checks through the normal PR process.

For example, request a review by commenting on a PR:

```text
/fs-review
```

## Commands and permissions

| Command | Where | Result | Minimum repository permission |
| --- | --- | --- | --- |
| `/fs-triage` | Issue | Assess the issue, request missing details, and update triage labels | Triage |
| `/fs-review` | PR | Review the proposed changes and publish findings | Triage |
| `/fs-code` | Issue | Implement the issue and open a PR | Write |
| `/fs-fix <instruction>` | PR | Update the PR to address the instruction | Write |
| `/fs-retro` | Completed issue or PR | Analyze the completed work and suggest follow-up improvements | Write |
| `/fs-prioritize` | Issue | Post RICE prioritization scores and reasoning | Write |

GitHub maintain and admin roles satisfy both permission levels. Fullsend uses
effective GitHub repository access, including access granted through teams.
Being listed in `OWNERS` alone does not grant command access in this setup.

## Describe implementation work in an issue

Give the code agent a bounded task with reproduction steps or a concrete
example, expected behavior, and acceptance criteria. Include relevant code
paths, existing tests, and the source/target platforms when they matter.

For E2E work, state whether clusters and test dependencies are available.
Distinguish a request to add a test from a claim that the live scenario has
already passed.

Request implementation with a separate comment:

```text
/fs-code
```

The agent opens a PR for the implementation. Check the diff, tests, and any
assumptions reported by the agent. Automatic merging is disabled; merging
follows the repository's normal review and branch requirements.

## Review a pull request

Same-repository PRs can receive automatic reviews when opened, updated, or
marked ready for review. The `ready-for-review` label also requests review
through upstream routing.

Fork PRs and Dependabot PRs do not receive automatic review or retro. An
authorized user can comment `/fs-review` on a fork PR. Use the same command
when a merge conflict prevents an automatic PR workflow from running.

Review publishes findings at medium severity and above. Low-severity findings
are filtered out. Read the findings alongside CI results and human reviews.

Until Fullsend earns the team's trust, positive verdicts are posted as comments
instead of green `APPROVED` reviews, and the PR receives
`requires-manual-review`. A human reviewer supplies the approval needed for
merge. Fullsend can still submit `REQUEST_CHANGES` when it finds problems.

## Request a fix

On the PR, give a specific instruction after `/fs-fix`, for example:

```text
/fs-fix Add a regression test for an empty input directory and address the failing unit test.
```

Inspect the resulting commit and checks. Eligible Fullsend-authored PRs may
also receive automatic fixes in response to review feedback. Human-authored
PRs can opt in to automatic fixes with the `fullsend-fix` label.

To stop automatic fixes for one PR, post this command as the entire comment:

```text
/fs-fix-stop
```

This requires write permission or PR authorship. It adds `fullsend-no-fix`.
Remove that label or use `/fs-fix` to re-engage the fix agent.

## Triage and automatic follow-ups

New or edited issues and the `ready-for-triage` label can trigger triage,
subject to Fullsend authorization checks. `/fs-triage` requests it explicitly.
Answer any clarification questions in the issue before requesting another run.

Triage does not promote issues to implementation automatically. A human with
write permission or higher can start the code agent by adding `ready-to-code`
to an issue or posting `/fs-code` on it. Keep the issue's scope and acceptance
criteria current.

The `skip-triage` label suppresses automatic triage. An authorized human
`/fs-triage` request overrides it.

## Retrospectives and prioritization

Retro analyzes completed work only on request with `/fs-retro`. Closing or
merging a PR does not start it automatically.

Prioritize runs only on request. Comment `/fs-prioritize` on an issue to get
Reach, Impact, Confidence, and Effort scores with written reasoning. It posts
the result as a comment. No Projects board is selected in this installation.

## Follow a run and troubleshoot

Open the run linked from the agent's status comment, or find the matching
`fullsend` run in the repository's
[Actions tab](https://github.com/migtools/crane/actions/workflows/fullsend.yaml).
Check the job for the requested agent and read its result or failure message.

| Symptom | What to check |
| --- | --- |
| No agent starts after a command | The command starts a new comment, is posted on the right issue or PR, and your repository permission meets the requirement. Check deployment status in the setup guide. |
| Automatic review does not start on a fork PR | Request it with `/fs-review` as a user with triage permission or higher. |
| Automatic triage is skipped | Check for `skip-triage`; use `/fs-triage` when an explicit reassessment is needed. |
| Review omits minor suggestions | The configured publication threshold is medium. |
| A run fails during authentication or model access | Share the run URL and failing step with the repository administrators. See [setup and operations](fullsend_setup.md). |
