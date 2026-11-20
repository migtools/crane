# Vertex WIF smoke test

The manually triggered **Vertex WIF smoke test** workflow verifies GitHub
Actions authentication through the centrally managed ITPC Workload Identity
Federation provider. It can then make one small Claude request on Vertex AI.
Use it to validate cloud access before configuring Fullsend.

The check runs here because WIF identifies the calling repository. A check in
an infrastructure repository would exercise a different federated principal.

## Configuration and ownership

Crane repository administrators maintain these Actions repository variables
under **Settings > Secrets and variables > Actions > Variables**:

| Variable | Value |
| --- | --- |
| `VERTEX_PROJECT_ID` | The GCP project ID used for inference and billing |
| `VERTEX_WIF_PROVIDER` | The full provider name supplied by ITPC, in the form `projects/<pool-project-number>/locations/global/workloadIdentityPools/<pool-id>/providers/<provider-id>` |

These are identifiers, not secrets. For example, an administrator can set them
with GitHub CLI after replacing the placeholders:

```bash
gh variable set VERTEX_PROJECT_ID --repo migtools/crane \
  --body '<inference-project-id>'
gh variable set VERTEX_WIF_PROVIDER --repo migtools/crane \
  --body 'projects/<pool-project-number>/locations/global/workloadIdentityPools/<pool-id>/providers/<provider-id>'
```

Crane maintainers own this workflow and its documentation. The inference
project administrators own IAM bindings, billing, API enablement, and model
access. ITPC owns the central WIF provider and owner allowlist. Repository
administrators must update the variables when the project or provider changes.
The workflow fails before authentication if either variable is missing.

## Prerequisites

- The ITPC GitHub provider must allow the `migtools` repository owner.
- The configured inference project must have billing and the Vertex AI API enabled.
- Grant the exact repository principal `roles/aiplatform.user` on that project:

  ```bash
  export GCP_PROJECT='<inference-project-id>'
  export WIF_POOL='projects/<pool-project-number>/locations/global/workloadIdentityPools/<pool-id>'
  gcloud projects add-iam-policy-binding "$GCP_PROJECT" \
    --member="principalSet://iam.googleapis.com/${WIF_POOL}/attribute.repository/migtools/crane" \
    --role=roles/aiplatform.user \
    --condition=None
  ```

- Enable the intended Claude model in the project's Model Garden and select a
  supported location. The workflow defaults to `claude-sonnet-4-6` in `global`;
  these defaults do not establish that the project can serve that model.

The pool project number in the principal belongs to ITPC's central project,
which can differ from the inference project. Use the pool portion of the
configured provider name. No service account, private key, or repository secret
is required for this direct WIF test.

## Run the check

After the workflow is merged into the default branch, open **Actions > Vertex
WIF smoke test > Run workflow** in `migtools/crane`.

1. To test federation only, uncheck **Also make one small Claude request after
   WIF authentication**. This confirms token exchange, not project IAM or model
   access.
2. To test Vertex access as well, leave that option checked and supply an
   enabled Claude model ID and its location.

Locations can be `global`, the `us` or `eu` multi-region, or a supported region
such as `us-east5`. Model availability must be checked for the chosen location.

The full check can also be started with GitHub CLI:

```bash
gh workflow run vertex-wif-smoke-test.yml --repo migtools/crane \
  --ref main \
  -f model=claude-sonnet-4-6 \
  -f location=global \
  -f invoke_model=true
```

The workflow runs only when manually requested in `migtools/crane`. It does
not check out repository code, install Fullsend, or change GitHub or GCP
configuration. The inference check sends the fixed prompt `Reply with OK.`
with at most 16 output tokens. Each inference check incurs normal Vertex usage.
Logs contain the served model and token usage, not the generated text or bearer credentials.

A successful authentication step is followed by `WIF exchange succeeded.`,
including on authentication-only runs. A successful inference step prints a
JSON summary like this, with the actual model and token counts:

```json
{
  "project": "<inference-project-id>",
  "location": "global",
  "model": "claude-sonnet-4-6",
  "usage": {"input_tokens": 11, "output_tokens": 2}
}
```

## Interpret failures

- **Repository configuration fails:** set both Actions variables listed above.
- **Identity exchange fails:** check that ITPC deployed the owner allowlist,
  the central provider is enabled, and the job has `id-token: write`.
- **Inference returns 400:** read the API message and check that the model ID,
  location, and request parameters are supported together.
- **Inference returns 401:** the federated token may have expired or been
  rejected. Rerun the workflow to obtain a fresh token; if it persists, inspect
  the API message and authentication configuration.
- **Exchange passes, inference returns 403:** check the repository IAM binding,
  Vertex API enablement, and model access in the inference project.
- **Inference returns 404:** check the exact model ID, Model Garden access, and
  location. A 404 alone does not distinguish those causes.
- **Inference returns 429:** check available quota and provider capacity.

Successful inference proves this repository can reach that model through WIF.
It does not validate Fullsend's runtime, subagents, or token refresh during a
long-running agent session.
