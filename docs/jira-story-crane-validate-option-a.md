# Jira story: `crane validate` (Option A only)

Use this document as the basis for a Jira **Story** (and optional **Sub-tasks**). **Option B** (target integration on `crane transform`) is **explicitly out of scope** here.

**References:** [migtools/crane#185](https://github.com/migtools/crane/issues/185), [mtc-resource-versioning-and-crane-options.md](./mtc-resource-versioning-and-crane-options.md) §5.

---

## Summary / title

**Implement `crane validate` — read-only target discovery check for exported manifests (MTC-aligned, Option A)**

---

## Description

### Context

Crane **`export`** is **source-only**; it cannot compare the **target** cluster. MTC/mig-controller still gives operators **early visibility** when **source** GVKs (strict **group/version** + resource) are **not** available on the **destination** the same way—**warnings**, not automatic `apiVersion` rewriting.

Per **Option A** in `docs/mtc-resource-versioning-and-crane-options.md`, add a **dedicated read-only command** (e.g. `crane validate`) that answers: *given this bundle of manifests, does the **target** cluster expose the same strict discovery rows we care about?*

### Goal

1. **Filesystem-first:** Walk YAML under an export (and optionally transform) directory, collect distinct **`apiVersion` + `kind`** (and namespaces from metadata), use **target** discovery to resolve/check resources, and **report** GVKs that do **not** appear on the target under **strict** same `group/version` + **resource plural** rules (MTC-like).
2. **Behavior:** **Read-only**; **warn/report** only—no silent rewrite of manifests. Define **exit codes** explicitly for human vs CI use.
3. **Alignment:** Keep **preferred** export behavior as today; **validate** is the place for **target-aware** checks.

### Out of scope

- **Option B:** No required target kubeconfig on default **`crane transform`**; no merging validate into transform in this story (track separately if ever desired).
- **Automatic** cross-version mapping or rewriting `apiVersion` on the target.
- Treating **`kubectl apply --dry-run=server`** as the only check (it complements but does not replace discovery-level reporting).

### User value

- **CI/CD:** Artifact directory + target kubeconfig → compatibility report before apply.
- **Operators:** MigPlan-style **early** incompatible-GVK visibility without running transform against a cluster.

---

## Proposed changes

### CLI / UX

- New command: **`crane validate`** (name subject to maintainer agreement).
- **Minimum inputs:**
  - **`--export-dir`** (or flag name consistent with Crane) — root of YAML to scan (recursive; multi-doc YAML).
  - **Target cluster:** **`--kubeconfig`**, **`--context`** (same patterns as other Crane commands that use a cluster).
  - Optional: namespace scoping / filters (product decision).
- **Output:** Human-readable **report** (e.g. table): `apiVersion`, `kind`, namespace (if namespaced), resolved resource where possible, **OK / incompatible** + short reason.
- **Exit code policy:** Document and implement (e.g. non-zero if any incompatible GVK for CI).

### Implementation

- **Scanner:** Filesystem walk; parse YAML; extract `apiVersion`, `kind`, `metadata.namespace` (and name for context).
- **Target discovery:** client-go **discovery**, **preferred-oriented** view consistent with MTC discussion in the options doc.
- **Strict match:** Exact **`group/version`** on target + **resource plural** present; no “pick another version on target.”
- **Cohabitating resources:** Dedupe known overlapping groups for the same logical kind (mirror mig-controller **intent**; explicit list in Crane).
- **CRDs (optional phase):** Warnings for **CRD API** / CRD-backed group skew—**detection only**.

### Documentation

- When to run validate; flags; exit codes; relationship to `export` / `transform` / `apply`.
- Links to **#185** and **`mtc-resource-versioning-and-crane-options.md`** (Option A).

### Tests

- Unit tests: parsing, GVK aggregation, matcher with **fake discovery**.
- Optional: envtest or fixture-based integration tests.

---

## Sub-tasks (suggested)

| # | Sub-task |
|---|----------|
| 1 | **Design & CLI spec** — Command name, flags, exit codes, output format. |
| 2 | **YAML / filesystem scanner** — Recursive walk, multi-document YAML, distinct `(apiVersion, kind, namespace)` aggregation. |
| 3 | **Target discovery client** — Kubeconfig/context; fetch API resources; document preferred-oriented behavior. |
| 4 | **Strict GVK ↔ discovery matcher** — Strict `group/version` + plural rules; unit tests with fake discovery. |
| 5 | **Cohabitating-resource dedupe** — Fixed list + tests (no duplicate warnings for same logical kind). |
| 6 | **Report formatter** — CLI output; optional `--output json` for CI (stretch). |
| 7 | **Documentation** — User-facing doc + cross-links. |
| 8 | *(Phase 2)* **Source-live mode** — Optional source kubeconfig; list in-use GVRs in namespaces vs target (mig-controller parity). |
| 9 | *(Phase 2)* **CRD / apiextensions skew warnings** — As in options doc. |

---

## Jira metadata (suggestions)

- **Issue type:** Story (with Sub-tasks as above).
- **Labels:** `crane`, `mtc-alignment`, `resource-versioning`, `validate`.
- **Links:** GitHub issue #185; this repo doc `mtc-resource-versioning-and-crane-options.md`.
