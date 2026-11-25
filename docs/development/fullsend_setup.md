# Fullsend setup and operations

This guide is for maintainers and administrators managing Fullsend
configuration, GitHub App access, inference, and workflow updates. For agent
commands and the GitHub development workflow, see [Using Fullsend](fullsend.md).

Fullsend provides triage, review, code, fix, retro, and prioritize agents for
`migtools/crane`. The workflows use Fullsend v0.57.0, commit
`3381ce46ba810d0f4cefe64905b0e6bc3ce36b48`. Agent sources in
`.fullsend/config.yaml` reference `fullsend-ai/agents` v0.57.0, commit
`41f89529863ea6c26e804a348f56953423d294a3`, with SHA-256 integrity checks.

All six agents use the Claude Code runtime on Google Vertex AI in `global`,
billed to the project in `VERTEX_PROJECT_ID`. Model selection is inherited
from upstream harnesses and personas, with no local model or tier overrides.

## Upstream model selection

All six v0.57.0 fleet harnesses select `opus` for the parent agent. Fullsend
v0.57.0 resolves that alias to `claude-opus-5-5`, with
`claude-opus-4-6` as its model-not-served fallback.

Review personas select different tiers. Correctness, security, and challenger
use Opus; cross-repository contracts, documentation, intent, risk, style, and
user experience use Sonnet; security triage uses Haiku.

Claude Code resolves those native subagent aliases separately from Fullsend's
parent aliases. The pinned fleet's `env/gcp-vertex.env` explicitly sets the
native Sonnet tier to `claude-sonnet-4-6`. Native Opus and Haiku retain the
Claude Code provider defaults. The documented Vertex defaults are Opus 5.5
and Haiku 4.5.

The following models are enabled in the
[project's Model Garden](https://console.cloud.google.com/vertex-ai/model-garden?project=mta-ops-fs-agents),
in addition to Sonnet 5.5:

| Model | Purpose |
| --- | --- |
| Claude Opus 5.5, `claude-opus-5-5` | All parent agents and native Opus personas |
| Claude Sonnet 4.6, `claude-sonnet-4-6` | Native Sonnet personas, pinned by the fleet environment |
| Claude Haiku 4.5, `claude-haiku-4-5` | Native Haiku personas |

Opus 4.6 is optional if the parent model-not-served fallback is needed.
Enabling Haiku 5.5 alone does not provide access to the native Haiku 4.5
default. Enable model access for the inference project and use models
available at the `global` endpoint.

## GitHub identity and inference

The hosted mint at `https://mint.fullsend.sh` issues short-lived GitHub App
tokens. These Apps must be installed in `migtools` with access to `crane`:

| App | Agents |
| --- | --- |
| `fullsend-ai-triage` | triage |
| `fullsend-ai-review` | review |
| `fullsend-ai-coder` | code and fix |
| `fullsend-ai-retro` | retro |
| `fullsend-ai-prioritize` | prioritize |

Inference uses the central ITPC WIF provider from `VERTEX_WIF_PROVIDER` and
a repository-scoped `roles/aiplatform.user` binding in `VERTEX_PROJECT_ID`.
The mint supplies GitHub identity; it does not supply model inference.
Fullsend and Claude Code derive the Vertex request URL from the project,
location, and model. The project and provider are supplied by the workflows,
with no fixed project ID or provider URL in `.fullsend/config.yaml`.

The workflows expect these Actions repository settings in `migtools/crane`:

| Setting | Storage | Value |
| --- | --- | --- |
| `FULLSEND_MINT_URL` | Variable | `https://mint.fullsend.sh` |
| `FULLSEND_APP_SET` | Variable | `fullsend-ai` |
| `FULLSEND_PER_REPO_INSTALL` | Variable | `true` |
| `FULLSEND_GCP_REGION` | Variable | `global` |
| `FULLSEND_REVIEW_CLIENT_ID` | Variable | `Iv23li1nIorNLIQy6NWK`, public OAuth client ID of the review App |
| `VERTEX_PROJECT_ID` | Variable | The GCP project ID used for inference and billing |
| `VERTEX_WIF_PROVIDER` | Variable | The full central ITPC WIF provider resource name |

Both workflows pass `vars.VERTEX_PROJECT_ID` as `FULLSEND_GCP_PROJECT_ID` and
`vars.VERTEX_WIF_PROVIDER` as `FULLSEND_GCP_WIF_PROVIDER` in the reusable
workflow's `secrets:` block. These names belong to the upstream interface;
the values come from repository variables, and same-named repository secrets
are not used. Authentication exchanges the Actions OIDC token through WIF;
no service-account private key or model-provider API key is needed.
`FULLSEND_GITHUB_RUNNER_SECRETS` and the OTLP header secrets are optional.

Crane maintainers own `.fullsend/config.yaml`, its pinned agent sources, and the
repository-specific workflow guards. Repository administrators maintain App
access and Actions settings. GCP project administrators maintain model access
and IAM. ITPC maintains the central WIF provider.

## User authorization

Fullsend checks the requesting user's effective GitHub repository permissions
through `GET /repos/migtools/crane/collaborators/<username>/permission`.
Permissions granted through organization teams count too.

Triage and review commands require repository triage permission or higher.
Code, fix, retro, and prioritize commands require write permission or higher.
The maintain and admin roles satisfy both requirements. A failed permission
lookup does not grant access.

The configuration does not enable the `owners_file` authorization provider,
so membership in root `OWNERS` alone does not authorize a user. User
authorization is separate from the GitHub App permissions used by agents.

## Agent configuration and routing

All six agents are enabled. Prioritize is request-only, with an explicit
`agents[].trigger` matching a human `/fs-prioritize` command. The other agents
retain upstream routing and effort settings, with triage-to-code promotion
disabled as described below. Configuration changes take effect when merged
into the branch used by the trusted workflow.

Review publishes findings at medium severity and above. The
`REVIEW_FINDING_SEVERITY_THRESHOLD` is set to `medium` in both runner and
sandbox environments so analysis and publishing use the same threshold.

### Temporary approval policy

Until Fullsend earns the team's trust, it must not issue formal `APPROVED`
reviews. The review harness explicitly enables risk assessment with
`REVIEW_RISK_ASSESSMENT_ENABLED: "true"` and sets
`REVIEW_RISK_VERDICT_THRESHOLD: "1"` in both runner and sandbox environments.

The pinned upstream risk gate converts every `approve` verdict into a
`comment`, since valid risk scores are 1-5. Missing, invalid, or degraded risk
assessments also withhold approval. The result receives the
`requires-manual-review` label instead of `ready-for-merge`. Negative verdicts
can still submit `REQUEST_CHANGES`.
Risk notices describe the configured threshold, which also blocks low-risk
approvals during this temporary period.

Retire this policy only after the team agrees to trust bot approvals. Remove
the two risk-variable overrides from both environment sections to inherit the
pinned upstream defaults. Risk assessment is enabled and the approval-gating
threshold is 4.
Keep the independent medium finding-severity threshold. Changing this policy
does not dismiss any previously submitted approvals.

### Triage and implementation

Upstream authorization checks apply to every command and event. Triage sets
`TRIAGE_AUTO_CODE: "off"` in both `harness.env.runner` and
`harness.env.sandbox`, disabling its automatic promotion to implementation.
An authorized human can still start code by adding `ready-to-code` to an issue
or posting `/fs-code`. Label routing remains upstream behavior, including its
bot handoff support; this setting disables promotion by triage rather than
blocking all bot-added labels.
The `skip-triage` label suppresses automatic triage; a human `/fs-triage`
request overrides it. Code and fix can create and update PR branches.
`CODE_AUTO_MERGE` retains the upstream default of disabled, and normal branch
rules govern merging. Issue creation targets are restricted to `migtools/crane`.

The v0.57.0 shim uses `pull_request`, not `pull_request_target`. It forwards
issue, issue-comment, same-repository PR, and PR-review events to the pinned
upstream dispatcher. Fork PRs and Dependabot PRs do not receive automatic
review or retro. An authorized `/fs-review` comment requests a review of a
fork PR or a PR whose merge conflict prevents a `pull_request` run.
Workflow jobs run only in `migtools/crane`, matching its authorized WIF
principal.

`.github/workflows/prioritize.yml` is a manual dispatch entry point. It
requires `source_repo` to match the current repository and passes its inputs
to the pinned reusable prioritize workflow.

Prioritize posts RICE scoring as a comment. Optional variable
`FULLSEND_PROJECT_NUMBER` enables updates to an organization Projects V2 board
when the App has access and the expected fields exist. No board is selected
by this installation. Prioritize does not run automatically on issue or PR
changes, and there is no repository-local scheduled prioritization.

## Current status

| Capability | Status |
| --- | --- |
| GitHub Actions OIDC authentication through ITPC WIF | Working |
| Vertex inference in `global` with Opus 5.5, Sonnet 4.6, Haiku 4.5, and Sonnet 5.5 | Working |
| Fullsend GitHub App access and token minting | Not yet verified |
| End-to-end Fullsend agent execution and native subagent model selection | Not yet verified |
| Credential refresh during long-running agent sessions | Not yet verified |

Use the [Vertex WIF smoke test](vertex-wif-smoke-test.md) to check model
availability and cloud authentication. Fullsend execution, delegation, and
credential refresh require agent runs.

## Operations and updates

Set `kill_switch: true` in `.fullsend/config.yaml` to stop agent dispatch.
Disabling the `fullsend` and `Prioritize` workflows stops new workflow runs;
cancel already active runs separately when needed.

On upgrade, refresh the complete shim and thin caller from the matching
Fullsend scaffold, then retain the `migtools/crane` job guards, `VERTEX_*`
variable mappings, and selected secret forwarding. Update workflow commit
pins and all six fleet harness commit/integrity pins together. Inspect upstream
model aliases, persona frontmatter, and the fleet Vertex environment for changes
to the required model access, then run lint and actionlint.
The inherited fleet harnesses already pin their sandbox images by digest.

Check configuration changes from the repository root with Fullsend v0.57.0
and actionlint:

```bash
fullsend lint --forge github
actionlint .github/workflows/fullsend.yaml .github/workflows/prioritize.yml .github/workflows/vertex-wif-smoke-test.yml
git diff --check
```

Lint resolves the six pinned harnesses and their dependencies. It may report
deprecations from upstream harnesses; inspect their origins before changing
repository configuration.

Release notes: [Fullsend v0.57.0](https://github.com/fullsend-ai/fullsend/releases/tag/v0.57.0)
and [fleet agents v0.57.0](https://github.com/fullsend-ai/agents/releases/tag/v0.57.0).

For deployment plans, test results, and pilot findings, see the
[Fullsend deployment and testing notes](https://github.com/aufi/move-crane/tree/main/fullsend-ai).
