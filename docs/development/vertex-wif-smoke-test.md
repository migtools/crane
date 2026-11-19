# Vertex WIF smoke test

The manually triggered **Vertex WIF smoke test** workflow verifies GitHub
Actions authentication through the centrally managed ITPC Workload Identity
Federation provider. It can then make one small Claude request on Vertex AI.
Use it to validate cloud access before configuring Fullsend.

## Prerequisites

- The ITPC GitHub provider must allow the `migtools` repository owner.
- The `mta-ops-fs-agents` project must have billing and the Vertex AI API enabled.
- Grant the exact repository principal `roles/aiplatform.user` on that project:

  ```bash
  gcloud projects add-iam-policy-binding mta-ops-fs-agents \
    --member='principalSet://iam.googleapis.com/projects/21066242673/locations/global/workloadIdentityPools/itpc-identity-pool/attribute.repository/migtools/crane' \
    --role=roles/aiplatform.user \
    --condition=None
  ```

- Enable the intended Claude model in the project's Model Garden and select a
  supported location. The workflow defaults to `claude-sonnet-4-6` in `global`;
  these defaults do not establish that the project can serve that model.

The pool belongs to ITPC's central project, number `21066242673`. The inference
project is `mta-ops-fs-agents`. No service account, private key, or repository
secret is required for this direct WIF test.

## Run the check

After the workflow is merged into the default branch, open **Actions > Vertex
WIF smoke test > Run workflow** in `migtools/crane`.

1. To test federation only, uncheck **Also make one small Claude request after
   WIF authentication**. This confirms token exchange, not project IAM or model
   access.
2. To test Vertex access as well, leave that option checked and supply an
   enabled Claude model ID and its location.

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

## Interpret failures

- **Identity exchange fails:** check that ITPC deployed the owner allowlist,
  the central provider is enabled, and the job has `id-token: write`.
- **Exchange passes, inference returns 403:** check the repository IAM binding,
  Vertex API enablement, and model access in the inference project.
- **Inference returns 404:** check the exact model ID, Model Garden access, and
  location. A 404 alone does not distinguish those causes.
- **Inference returns 429:** check available quota and provider capacity.

Successful inference proves this repository can reach that model through WIF.
It does not validate Fullsend's runtime, subagents, or token refresh during a
long-running agent session.
