# Vertex WIF smoke test

Use the manually triggered **Vertex WIF smoke test** workflow to verify
GitHub Actions authentication through the central ITPC Workload Identity
Federation provider and model availability on Vertex AI. Keep this check
available for testing newly enabled models and diagnosing inference failures.

## Configuration

The workflow uses the same Actions repository variables as Fullsend:

| Variable | Purpose |
| --- | --- |
| `VERTEX_PROJECT_ID` | GCP project used for inference and billing |
| `VERTEX_WIF_PROVIDER` | Full central ITPC WIF provider resource name |

The provider must allow the `migtools` owner, and the inference project must
have billing, the Vertex AI API, and access to the selected Claude model.
Grant `roles/aiplatform.user` on the inference project to the exact repository
principal for `attribute.repository/migtools/crane` in the provider's WIF pool.
The pool project can differ from the inference project.

Repository administrators maintain the variables. GCP project administrators
maintain IAM, billing, and model access. ITPC maintains the central provider.
No service-account private key or repository secret is required by this test.

## Run the check

Open **Actions > Vertex WIF smoke test > Run workflow** in `migtools/crane`.
Select the model ID and location. Disable **Also make one small Claude request
after WIF authentication** to check federation only.

For example, to verify Opus 5.5 in `global`:

```bash
gh workflow run vertex-wif-smoke-test.yml --repo migtools/crane \
  --ref main \
  -f model=claude-opus-5-5 \
  -f location=global \
  -f invoke_model=true
```

The default inputs are `claude-sonnet-4-6` and `global`. Other locations can
include `us`, `eu`, or a supported region such as `us-east5`. Supply an exact
model ID available to the project at the selected endpoint.

The workflow runs only when explicitly requested in `migtools/crane`. It
does not check out code, install Fullsend, or change GCP configuration. A
model request sends the fixed prompt `Reply with OK.` with at most 16 output
tokens and incurs normal Vertex usage. Logs report the served model and token
counts without exposing bearer credentials or generated text.

## Interpret results

- A successful exchange prints `WIF exchange succeeded.` This alone does not
  prove project IAM or model access.
- Successful inference prints the project, location, served model, and usage.
- Exchange failure points to the provider, owner allowlist, or Actions OIDC
  permissions. The job requires `id-token: write`.
- HTTP 403 after exchange points to project IAM, API enablement, or access.
- HTTP 404 requires checking the exact model ID, Model Garden access, and
  location. It does not distinguish those causes by itself.
- HTTP 429 indicates quota or provider capacity constraints.

The [Fullsend guide](fullsend.md#current-status) lists model availability and
integration status. This standalone test verifies cloud
access; agent execution, native subagent selection, and long-running credential
refresh are verified separately through Fullsend runs.
