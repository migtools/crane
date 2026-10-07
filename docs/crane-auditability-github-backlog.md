# Crane Auditability — story headline and GitHub issue backlog

Use this document to open an **Epic** (or parent tracking issue) and **child issues** on your GitHub project. Each issue includes example output so scope is obvious when copied into GitHub.

Scope here is **persistent, structured audit artifacts** written to disk alongside existing output. All artifacts are always written — no opt-in flag required. Console UX (banners, phased progress, summaries) is handled by the **observability** epic ([#254](https://github.com/migtools/crane/issues/254)); this epic covers the machine-readable files and persistent log that survive after the terminal closes.

**Related:**
- Observability UX epic: [#254](https://github.com/migtools/crane/issues/254) (Issues #269–#272)
- Unified implementation plan: `docs/observability-auditability-unified-plan.md`
- Detailed auditability plan: `docs/forensic-postmortem-auditability-plan.md`

---

## Story headline (Epic title)

**As a security auditor or support engineer, I can reconstruct exactly what Crane did during a migration — who ran it, what was exported/transformed/applied, what was skipped or whited out, and why — without re-running the tool or needing access to the original cluster.**

Shorter option for the Epic title field:

> **Persistent structured audit trail for all Crane commands**

---

## Recommended issue order

1. **Persistent audit log** — foundation that captures all existing log output to disk.
2. **Run manifest** — records invocation context for every command.
3. **Export inventory** — structured record of discovery results.
4. **Transform audit artifacts** — populates the scaffolded-but-empty report files + fixes IgnoredOps TODO.
5. **Deep plugin audit trail** — per-plugin per-resource decisions (requires crane-lib changes).

Issues 1–4 require no crane-lib changes. Issue 5 does.

---

## Issue 1 — Persistent structured audit log (`.crane-audit.log`)

### Title

`feat(audit): always-on persistent structured log file`

### Output: what changes

**Today** — all log output goes to stderr only. When the terminal closes, it's gone.

**After this issue** — every `crane` invocation also writes a JSON Lines log file:

```
$ crane export -n my-app -e ./export
... (normal console output) ...

$ cat .crane-audit.log
{"ts":"2026-04-18T14:32:01.123Z","level":"info","cmd":"export","msg":"Starting export","namespace":"my-app"}
{"ts":"2026-04-18T14:32:01.456Z","level":"info","cmd":"export","msg":"Discovered 127 API resource types"}
{"ts":"2026-04-18T14:32:02.789Z","level":"warn","cmd":"export","msg":"Cannot list resource","kind":"ConfigMap","error":"Forbidden"}
{"ts":"2026-04-18T14:32:12.345Z","level":"info","cmd":"export","msg":"Export complete","exported":47,"failed":2,"duration":"11.2s"}
```

The file captures **all** log levels (including Debug) regardless of console verbosity. Support engineers get full detail without needing `--debug`.

### Description

**Context**

Console logs are ephemeral. Support cases require log replay. The audit log is a logrus `Hook` attached in `GetLogger()` that writes every log entry as a JSON Lines file. It is always active — `--audit-log` exists only to override the default path.

**User outcome**

- Every run produces a persistent, grep-able, jq-parseable log file.
- No existing command code needs changes — the hook captures automatically.

**Scope**

- Implement logrus file hook in `internal/audit/audit_logger.go`.
- Attach hook in `GetLogger()` (always-on).
- Add `--audit-log` flag for path override (default: `.crane-audit.log`).

**Acceptance criteria**

- [ ] Every `crane export/transform/apply` run creates `.crane-audit.log` (or custom path via `--audit-log`).
- [ ] Log file is JSON Lines format — one JSON object per line.
- [ ] File captures all log levels including Debug even when console is at Info.
- [ ] Append mode — successive runs in the same directory append, not overwrite.
- [ ] `go test ./...` passes.

**References**

- `internal/flags/global_flags.go`, auditability plan Section 4.8.

---

## Issue 2 — Run manifest (`.crane-run.json`)

### Title

`feat(audit): write run manifest recording invocation context for every command`

### Output: what changes

**After this issue** — every command writes a `.crane-run.json` in its output directory:

```
$ crane export -n my-app -e ./export
... (normal output) ...

$ cat export/.crane-run.json
{
  "schemaVersion": "v1",
  "command": "export",
  "craneVersion": "v0.0.6",
  "cranelibVersion": "v0.1.6",
  "timestamp": "2026-04-18T14:32:01Z",
  "duration": "12.4s",
  "user": "ssingla",
  "hostname": "macbook.local",
  "flags": {
    "namespace": "my-app",
    "export-dir": "export",
    "context": "eks-prod"
  },
  "kubernetes": {
    "serverVersion": "v1.28.3",
    "context": "eks-prod"
  },
  "result": {
    "status": "completed_with_warnings",
    "resourcesProcessed": 47,
    "warnings": 3
  }
}
```

Written for `export`, `transform`, and `apply`.

### Description

**Context**

There is no record of _who_ ran _what command_ with _which flags_ at _what time_. Reproducing a migration run requires asking the operator. The run manifest captures the full invocation context so anyone can understand or reproduce it later.

**User outcome**

- Security auditors can identify who, when, where, and with what flags.
- Support engineers can reproduce the exact invocation.

**Scope**

- Define `RunManifest` struct in `internal/audit/run_manifest.go`.
- Write `.crane-run.json` at the end of `export`, `transform`, and `apply` `Run()` methods.
- Capture: command, versions, timestamp, duration, user, hostname, flags, kubernetes context (export only), result status.

**Acceptance criteria**

- [ ] `crane export` writes `export/.crane-run.json`.
- [ ] `crane transform` writes `transform/.crane-run.json`.
- [ ] `crane apply` writes `output/.crane-run.json`.
- [ ] JSON includes `schemaVersion`, command, versions, timestamp, duration, flags, and result status.
- [ ] Export run manifest includes kubernetes context (server version, context name).
- [ ] Sensitive values (kubeconfig contents, tokens) are NOT recorded — only flag names and safe values.
- [ ] `go test ./...` passes.

**References**

- `cmd/export/export.go`, `cmd/transform/transform.go`, `cmd/apply/apply.go`, auditability plan Section 4.1.

---

## Issue 3 — Export inventory report

### Title

`feat(audit): structured export inventory recording discovered, exported, and skipped resources`

### Output: what changes

**After this issue** — `crane export` writes an inventory alongside the exported resources:

```
$ crane export -n my-app -e ./export
... (normal output) ...

$ cat export/.crane-export-inventory.json
{
  "schemaVersion": "v1",
  "namespace": "my-app",
  "timestamp": "2026-04-18T14:32:01Z",
  "apiResourcesDiscovered": 127,
  "apiResourcesAdmitted": 43,
  "apiResourcesSkipped": 84,
  "skippedReasons": {
    "clusterScoped": 62,
    "noObjects": 18,
    "events": 1,
    "unsupportedVerbs": 3
  },
  "resources": [
    {
      "apiVersion": "apps/v1",
      "kind": "Deployment",
      "count": 3,
      "names": ["frontend", "backend", "worker"]
    }
  ],
  "failures": [
    {
      "kind": "ConfigMap",
      "error": "Forbidden: configmaps is forbidden",
      "errorType": "RBAC"
    }
  ],
  "totalExported": 47,
  "totalFailed": 2
}
```

### Description

**Context**

Today, export records failures in `failures/<ns>/` but has no structured record of successes or skipped resources. There is no way to know how many API types were discovered vs. admitted without re-running with `--debug`.

**User outcome**

- Auditors can see exactly what was exported, what was skipped, and why.
- Support engineers can diagnose "missing resource" reports without cluster access.

**Scope**

- Define `ExportInventory` struct in `internal/audit/export_inventory.go`.
- Collect discovery counts during `ExportOptions.Run()` — admitted, skipped (with reasons), failed.
- Write `.crane-export-inventory.json` at end of export.

**Acceptance criteria**

- [ ] `crane export` writes `export/.crane-export-inventory.json`.
- [ ] Inventory includes total discovered, admitted, and skipped API resource types.
- [ ] Skipped reasons are broken down (cluster-scoped, no objects, events, unsupported verbs).
- [ ] Per-type resource list with names and counts.
- [ ] Failures recorded with error message and type.
- [ ] `go test ./...` passes.

**References**

- `cmd/export/export.go`, `cmd/export/discover.go`, auditability plan Section 4.7.

---

## Issue 4 — Transform audit artifacts

### Title

`feat(audit): populate transform stage metadata, resource inventory, whiteout and ignored-patches reports`

### Output: what changes

**Today** — path helpers exist for `.crane-metadata.json`, `whiteouts.json`, and `ignored-patches.json` but the files are **never written**. `IgnoredOps` is hardcoded to `[]` despite the runner returning data.

**After this issue** — every transform stage produces four audit files:

```
transform/10_KubernetesPlugin/
├── .crane-metadata.json           # stage context: plugins, counts, duration
├── reports/
│   ├── resource-inventory.json    # per-resource outcome (patched/whited-out/unchanged)
│   └── ignored-patches.json       # patch conflicts and their resolution
└── whiteouts/
    └── whiteouts.json             # which resources were excluded
```

**Example `.crane-metadata.json`:**
```json
{
  "schemaVersion": "v1",
  "stageName": "10_KubernetesPlugin",
  "timestamp": "2026-04-18T14:33:15Z",
  "duration": "3.2s",
  "plugins": [{"name": "KubernetesTransformPlugin", "version": "v0.0.1"}],
  "summary": {
    "totalResources": 47,
    "patched": 32,
    "whitedOut": 8,
    "unchanged": 7,
    "ignoredPatchConflicts": 3
  }
}
```

**Example `resource-inventory.json`:**
```json
{
  "schemaVersion": "v1",
  "resources": [
    {
      "kind": "Deployment", "namespace": "my-app", "name": "frontend",
      "outcome": "patched", "patchCount": 5, "pluginName": "KubernetesTransformPlugin"
    },
    {
      "kind": "Endpoints", "namespace": "my-app", "name": "frontend",
      "outcome": "whited-out", "pluginName": "KubernetesTransformPlugin"
    }
  ]
}
```

### Description

**Context**

The codebase already has `PathOpts` methods for these file paths and `IgnoredOperation`/`TransformArtifact` structs with the data — but the CLI never writes the files and hardcodes `IgnoredOps: []`. This issue connects existing plumbing.

**User outcome**

- Auditors can trace every resource's journey through the transform stage.
- Support engineers can answer "why was this resource whited out?" or "which patches were dropped?" from files, not guesswork.

**Scope**

- **Fix `IgnoredOps` TODO** (`orchestrator.go:86,215`): parse `response.IgnoredPatches` instead of hardcoding `[]`.
- Write `.crane-metadata.json` in `WriteStage()` with stage name, plugins, counts, duration.
- Write `resource-inventory.json` per stage from `TransformArtifact` data.
- Write `whiteouts.json` from whited-out artifacts.
- Write `ignored-patches.json` from parsed `IgnoredOps`.

**Acceptance criteria**

- [ ] `IgnoredOps` is populated from `response.IgnoredPatches` — no longer hardcoded `[]`.
- [ ] `.crane-metadata.json` written per stage with plugin list and summary counts.
- [ ] `resource-inventory.json` written per stage with per-resource outcome.
- [ ] `whiteouts.json` written per stage listing whited-out resources.
- [ ] `ignored-patches.json` written per stage with conflict details (if any conflicts exist).
- [ ] All files use existing `PathOpts` path helpers.
- [ ] `go test ./...` passes.

**References**

- `internal/transform/orchestrator.go:86,215`, `internal/transform/writer.go`, `internal/file/file_helper.go`, auditability plan Sections 4.2–4.6.

---

## Issue 5 — Deep plugin audit trail (requires crane-lib)

### Title

`feat(audit): per-plugin per-resource decision tracking with reasons`

### Output: what changes

**After this issue** — each transform stage produces a `plugin-audit.json` with full plugin attribution:

```json
{
  "schemaVersion": "v1",
  "stageName": "10_KubernetesPlugin",
  "pluginsLoaded": [
    {"name": "KubernetesTransformPlugin", "version": "v0.0.1", "source": "built-in"},
    {"name": "OpenShiftPlugin", "version": "v1.2.0", "source": "binary:~/.crane/plugins/OpenShiftPlugin"}
  ],
  "pluginsSkipped": ["DeprecatedPlugin"],
  "perResourceDecisions": [
    {
      "resource": "Deployment/my-app/frontend",
      "pluginName": "KubernetesTransformPlugin",
      "action": "patch",
      "patchCount": 5,
      "duration": "2ms"
    },
    {
      "resource": "Deployment/my-app/frontend",
      "pluginName": "OpenShiftPlugin",
      "action": "patch",
      "patchCount": 1,
      "conflictsWithPrior": true,
      "conflictResolution": "priority-wins",
      "winnerPlugin": "KubernetesTransformPlugin"
    }
  ]
}
```

Whiteout reports also gain a `reason` field:

```json
{
  "kind": "Endpoints", "name": "frontend",
  "pluginName": "KubernetesTransformPlugin",
  "reason": "default-whiteout"
}
```

### Description

**Context**

Today, `Runner.Run()` merges all plugin responses into one result — per-plugin attribution is lost. When multiple plugins conflict, `sanitizePatches()` resolves them at Debug level with no structured output. This issue adds per-plugin tracking by introducing `RunDetailed()` to crane-lib.

**User outcome**

- Auditors can answer "which plugin changed this field?" for any resource.
- Plugin conflict resolution is fully transparent — who won, who lost, why.

**Scope**

**crane-lib changes (backward-compatible):**
- Add `RunDetailed()` method to `Runner` returning per-plugin responses.
- Add `WhiteOutReason` field to `PluginResponse` (with `omitempty`).
- Add per-plugin timing.
- Update `KubernetesTransformPlugin` to set `WhiteOutReason`.

**crane CLI changes:**
- Use `RunDetailed()` in orchestrator when available.
- Write `plugin-audit.json` per stage.
- Update whiteout report to include reasons.

**Acceptance criteria**

- [ ] `RunDetailed()` returns per-plugin responses with timing.
- [ ] `WhiteOutReason` populated by `KubernetesTransformPlugin`.
- [ ] `plugin-audit.json` written per stage with per-resource plugin decisions.
- [ ] Whiteout report includes reason when available.
- [ ] Existing `Run()` behavior unchanged — `RunDetailed()` is additive.
- [ ] crane-lib and crane `go test ./...` pass.

**References**

- `crane-lib/transform/runner.go`, `crane-lib/transform/plugin.go`, `internal/transform/orchestrator.go`, auditability plan Sections 4.4–4.5.

---

## Labels suggestion (for your repo)

- `area/audit` or `enhancement`
- `command/export`, `command/transform`, `command/apply` as applicable
- `priority` / `epic` as you prefer

---

## Epic body (paste into GitHub Epic or parent issue)

**Problem:** Crane has no permanent, structured record of what it does during a migration run. Console logs vanish when the terminal closes. There is no way to know which resources were exported, skipped, whited out, or what plugins decided — without re-running the tool.

**Goal:** Always-on audit artifacts written alongside existing output: run manifests (`.crane-run.json`), persistent structured log (`.crane-audit.log`), export inventory, per-stage resource inventories, whiteout reports, ignored-patches reports, and plugin audit trails. All machine-readable (JSON), all additive (no changes to existing directory layout), no opt-in flag required.

**Shared infrastructure with observability:** Both features use the same `RunStats` data model and `RunSingleStage` instrumentation. See `docs/observability-auditability-unified-plan.md`. The end-of-command summary is unified — one summary block covers both.

**Child issues:** Link Issues 1–5 from this document.
