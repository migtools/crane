# Fullsend agents

Fullsend provides triage, review, code, fix, retro, and prioritize agents for
`migtools/crane`. The workflows use Fullsend v0.57.0, commit
`3381ce46ba810d0f4cefe64905b0e6bc3ce36b48`. Local harness wrappers inherit
`fullsend-ai/agents` v0.57.0, commit
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
and Haiku 4.5; confirm the served IDs in the first delegated review run.

On November 20, 2026, the user confirmed enabling the following models in the
[project's Model Garden](https://console.cloud.google.com/vertex-ai/model-garden?project=mta-ops-fs-agents),
in addition to the already enabled Sonnet 5.5. Live Fullsend inference with
these additional models remains to be verified.

| Model | Purpose |
| --- | --- |
| Claude Opus 5.5, `claude-opus-5-5` | All parent agents and native Opus personas |
| Claude Sonnet 4.6, `claude-sonnet-4-6` | Native Sonnet personas, pinned by the fleet environment |
| Claude Haiku 4.5, `claude-haiku-4-5` | Native Haiku personas |

### Live WIF model validation

On November 20, 2026, all three models passed the standalone WIF smoke test
on `migtools/crane` `main`, using `VERTEX_PROJECT_ID` and
`VERTEX_WIF_PROVIDER`. Each run completed the GitHub OIDC exchange and an
authenticated Vertex `rawPredict` request in `mta-ops-fs-agents`, location
`global`, with `invoke_model=true`.

| Requested model | Served model | Input/output tokens | Successful run |
| --- | --- | --- | --- |
| `claude-opus-5-5` | `claude-opus-5-5` | 15 / 5 | [44704824536](https://github.com/migtools/crane/actions/runs/44704824536) |
| `claude-haiku-4-5` | `claude-haiku-4-5-20251001` | 11 / 5 | [44704824636](https://github.com/migtools/crane/actions/runs/44704824636) |
| `claude-sonnet-4-6` | `claude-sonnet-4-6` | 11 / 5 | [44704824961](https://github.com/migtools/crane/actions/runs/44704824961) |

This verifies repository-scoped WIF and model access. Fullsend runtime,
native subagent selection, and long-running credential refresh still need
live agent validation after deployment.

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

Crane maintainers own `.fullsend/config.yaml`, its harness wrappers, and the
repository-specific workflow guards. Repository administrators maintain App
access and Actions settings. GCP project administrators maintain model access
and IAM. ITPC maintains the central WIF provider.

## Commands and automatic behavior

All six agents are enabled. Prioritize is request-only, with an explicit
`agents[].trigger` matching a human `/fs-prioritize` command. The other agents
retain upstream automatic routing and effort settings. Merging these files
into `migtools/crane` activates the event handlers.

| Agent | Manual command | Other upstream triggers |
| --- | --- | --- |
| triage | `/fs-triage` on an issue | Issue opened or edited, or `ready-for-triage` label |
| review | `/fs-review` on a PR | Same-repo PR opened, updated, marked ready, or `ready-for-review` label |
| code | `/fs-code` on an issue | `ready-to-code` label |
| fix | `/fs-fix <instruction>` on a PR | Review feedback on eligible agent PRs; human PRs can opt in with `fullsend-fix` |
| retro | `/fs-retro` on an issue or PR | Same-repo PR closed or merged |
| prioritize | `/fs-prioritize` on an issue | Explicit dispatch through the manual `Prioritize` workflow only |

Upstream authorization checks apply to every command and event. Triage keeps
automatic code promotion enabled for `bug`, `documentation`, and `performance`.
The `skip-triage` label suppresses automatic triage; a human `/fs-triage`
request overrides it. Code and fix can create and update PR branches.
`CODE_AUTO_MERGE` retains the upstream default of disabled, and normal branch
rules govern merging. Issue creation targets are restricted to `migtools/crane`.

The v0.57.0 shim uses `pull_request`, not `pull_request_target`. Fork PRs and
Dependabot PRs do not receive automatic review or retro. Request a fork review
with `/fs-review` as a user with triage or higher permission, or an OWNERS
reviewer or approver. This also works while a merge conflict prevents GitHub
from running the `pull_request` workflow. Workflow jobs run only in
`migtools/crane`, matching its authorized WIF principal.

Prioritize posts RICE scoring as a comment. Optional variable
`FULLSEND_PROJECT_NUMBER` enables updates to an organization Projects V2 board
when the App has access and the expected fields exist. No board is selected
by this installation. Prioritize does not run automatically on issue or PR
changes, and there is no repository-local scheduled prioritization.

## Activation and verification

Before merging, confirm that all five GitHub Apps can access `migtools/crane`
and that the settings above match the selected project and provider. The
`VERTEX_PROJECT_ID` and `VERTEX_WIF_PROVIDER` variables were read back and
verified on November 20, 2026. App installation access still needs verification.

From the repository root, using Fullsend v0.57.0 and actionlint:

```bash
fullsend --version
fullsend lint --forge github
actionlint .github/workflows/fullsend.yaml .github/workflows/prioritize.yml
git diff --check
```

Lint resolves the six pinned harnesses and their dependencies. It may report
deprecations from upstream harnesses; inspect their origins before changing
repository configuration.

After merge, verify the agents with small representative work items:

1. Run `/fs-triage` and inspect WIF authentication, mint access, sandbox startup,
   model, labels, and structured output. Check skip-label behavior on a separate
   issue.
2. Run `/fs-review` on a small PR and inspect native subagent model usage.
   Also verify automatic review on a same-repo PR and manual review on a fork.
3. Run `/fs-code` on a narrow issue, then `/fs-fix` on its resulting PR.
   Confirm the branch and PR target `migtools/crane` and normal CI runs.
4. Run `/fs-retro` on a completed change and `/fs-prioritize` on an issue.
5. Check a run that crosses a credential-refresh interval and confirm model
   requests still succeed afterward.

Record run URLs, requested and served models, child-model usage, test results,
latency, and token counts. Use the standalone
[Vertex WIF smoke test](vertex-wif-smoke-test.md) for future model availability
checks and cloud authentication diagnostics. Verify Fullsend execution,
delegation, and credential refresh through the agent runs.

## Operations and updates

Use `/fs-fix-stop` on a PR to stop automatic fixes for that PR. Set
`kill_switch: true` in `.fullsend/config.yaml` to stop agent dispatch. Disabling
the `fullsend` and `Prioritize` workflows stops new workflow runs; cancel
already active runs separately when needed.

On upgrade, refresh the complete shim and thin caller from the matching
Fullsend scaffold, then retain the `migtools/crane` job guards, `VERTEX_*`
variable mappings, and selected secret forwarding. Update workflow commit
pins and all six fleet harness commit/integrity pins together. Inspect upstream
model aliases, persona frontmatter, and the fleet Vertex environment for changes
to the required model access, then run lint and actionlint.
The inherited fleet harnesses already pin their sandbox images by digest.

The per-repository setup command uses GitHub APIs and writes remote settings
and scaffold files. It does not install this local tree. For this prepared
installation, submit the local files through the normal PR process rather
than running setup again to publish them.

Release notes: [Fullsend v0.57.0](https://github.com/fullsend-ai/fullsend/releases/tag/v0.57.0)
and [fleet agents v0.57.0](https://github.com/fullsend-ai/agents/releases/tag/v0.57.0).
