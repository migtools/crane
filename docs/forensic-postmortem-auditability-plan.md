# Forensic Post-Mortem & Auditability Plan for Crane 2.0

**Issue:** [migtools/crane#200](https://github.com/migtools/crane/issues/200)
**Date:** 2026-04-18 (updated 2026-04-20)
**Scope:** crane CLI (`github.com/konveyor/crane`) + crane-lib (`github.com/konveyor/crane-lib`)
**GitHub issues backlog:** [`docs/crane-auditability-github-backlog.md`](./crane-auditability-github-backlog.md)
**Unified plan (shared with observability):** [`docs/observability-auditability-unified-plan.md`](./observability-auditability-unified-plan.md)

---

## 1. Problem Statement

Crane currently has **no permanent, structured record** of what it does during a migration run.
Security audits require knowing _who ran what, when, on which resources, and what decisions
were made_. Support engineers need to reproduce failures without re-running the tool.

Today, auditing a Crane migration means:
- Scrolling through transient console logs (lost once the terminal closes)
- Manually diffing export vs. output YAML files
- Guessing which plugins made which changes
- Having no record of which resources were skipped, whited-out, or errored

---

## 2. Current State Audit

### 2.1 What Exists Today

| Capability | Where | Status |
|---|---|---|
| Console logging (logrus) | `internal/flags/global_flags.go` | Works, but ephemeral -- not persisted to disk |
| `--debug` flag | Global flag | Increases verbosity, but output is still ephemeral |
| Export failure files | `export/failures/<ns>/<resource>.yaml` | Partial -- only records _list_ errors per API type |
| `TransformArtifact` struct | `crane-lib/transform/types.go` | Tracks patches, whiteouts, plugin name per resource |
| `IgnoredOperation` struct | `crane-lib/transform/types.go` | Defined with Plugin, Reason, WinnerPlugin fields |
| `RunnerResponse.IgnoredPatches` | `crane-lib/transform/runner.go` | Serialized but **never consumed** by crane CLI |
| `PathOpts.GetMetadataPath()` | `internal/file/file_helper.go:159` | Path to `.crane-metadata.json` defined, **never written** |
| `PathOpts.GetReportsDir()` | `internal/file/file_helper.go:140` | Reports directory path defined, **never used** |
| `PathOpts.GetWhiteoutReportPath()` | `internal/file/file_helper.go:178` | Whiteout report path defined, **never used** |
| `PathOpts.GetIgnoredPatchReportPath()` | `internal/file/file_helper.go:183` | Ignored-patch report path defined, **never used** |
| `sanitizePatches()` conflict logging | `crane-lib/transform/runner.go:174` | Logs conflicts at Debug level only, no structured output |
| Version command | `cmd/version/version.go` | Shows crane + crane-lib versions |

### 2.2 What's Missing

1. **No Run Manifest** -- no record of _who_ ran _what command_ with _which flags_ at _what time_
2. **No Resource Inventory** -- no structured list of all resources discovered, exported, skipped, or errored
3. **No Decision Log** -- no record of _why_ a resource was whited out, patches were ignored, or errors were tolerated
4. **No Plugin Audit Trail** -- no record of which plugins ran, their versions, what they did to each resource
5. **No Persistent Log File** -- all console output vanishes when the terminal session ends
6. **No Change Summary** -- no diffable record of what the transform/apply phases actually changed
7. **Metadata files are scaffolded but empty** -- `.crane-metadata.json`, reports, whiteout reports never written
8. **IgnoredOps always `[]`** -- `orchestrator.go:86,215` hardcodes empty `[]IgnoredOperation{}` despite runner returning data
9. **No end-to-end summary** -- user has no quick way to see "X resources exported, Y transformed, Z whited out, W errors"

---

## 3. Proposed Architecture

### 3.1 Design Principles

- **File-first**: All audit data written to disk alongside existing output (not just logs)
- **Machine-readable + Human-readable**: JSON for tooling, Markdown/text summaries for humans
- **Non-breaking**: New files are additive; existing directory layout unchanged
- **Always-on**: All audit artifacts are written on every run -- no flag required. Crane always records what it does.
- **Spans the full pipeline**: Export, Transform, and Apply all produce audit artifacts

### 3.2 Proposed Directory Layout

```
<working-dir>/
├── export/
│   ├── resources/<ns>/...          # (existing) exported YAML
│   ├── failures/<ns>/...           # (existing) per-type error YAML
│   └── .crane-run.json             # NEW: export run manifest
│
├── transform/
│   ├── <stage>/
│   │   ├── resources/              # (existing)
│   │   ├── patches/                # (existing)
│   │   ├── kustomization.yaml      # (existing)
│   │   ├── .crane-metadata.json    # NEW (path exists, content new)
│   │   ├── reports/
│   │   │   ├── ignored-patches.json    # NEW (path exists, content new)
│   │   │   ├── resource-inventory.json # NEW
│   │   │   └── plugin-audit.json       # NEW
│   │   └── whiteouts/
│   │       └── whiteouts.json      # NEW (path exists, content new)
│   └── .crane-run.json             # NEW: transform run manifest
│
├── output/
│   ├── resources/...               # (existing) final manifests
│   ├── output.yaml                 # (existing) combined output
│   └── .crane-run.json             # NEW: apply run manifest
│
└── .crane-audit.log                # NEW: persistent structured log file
```

---

## 4. Detailed Feature Specifications

### 4.1 Run Manifest (`.crane-run.json`)

Written at the end of every `export`, `transform`, or `apply` invocation. Records the
full invocation context needed to reproduce or audit the run.

**Where to implement:** Each command's `Run()` method, after the operation completes.

**Schema:**

```json
{
  "schemaVersion": "v1",
  "command": "export",
  "craneVersion": "v0.0.6",
  "cranelibVersion": "v0.1.6-...",
  "timestamp": "2026-04-18T14:32:01Z",
  "duration": "12.4s",
  "user": "ssingla",
  "hostname": "macbook.local",
  "workingDirectory": "/Users/ssingla/Documents/crane",
  "flags": {
    "namespace": "my-app",
    "export-dir": "export",
    "label-selector": "app=frontend",
    "debug": false,
    "kubeconfig": "/Users/ssingla/.kube/config",
    "context": "eks-prod"
  },
  "kubernetes": {
    "serverVersion": "v1.28.3",
    "context": "eks-prod",
    "cluster": "arn:aws:eks:us-east-1:...",
    "apiEndpoint": "https://..."
  },
  "result": {
    "status": "completed_with_warnings",
    "resourcesProcessed": 47,
    "resourcesSucceeded": 45,
    "resourcesFailed": 2,
    "warnings": 3,
    "errors": ["configmaps: Forbidden", "..."]
  }
}
```

**Implementation plan:**

| Step | File | Change |
|---|---|---|
| 1 | `internal/audit/run_manifest.go` (new) | Define `RunManifest` struct with JSON tags |
| 2 | `internal/audit/run_manifest.go` | `NewRunManifest(cmd, version)` populates timestamp, user, hostname, versions |
| 3 | `internal/audit/run_manifest.go` | `Complete(result)` sets duration, status, counts |
| 4 | `internal/audit/run_manifest.go` | `WriteToDir(dir)` serializes to `.crane-run.json` |
| 5 | `cmd/export/export.go` | In `Run()`, create manifest at start, complete+write at end |
| 6 | `cmd/transform/transform.go` | Same pattern |
| 7 | `cmd/apply/apply.go` | Same pattern |

**crane-lib changes:** None -- this is purely CLI-layer.

---

### 4.2 Stage Metadata (`.crane-metadata.json`)

The path already exists via `PathOpts.GetMetadataPath()` but the file is never written.
This records per-stage transform context.

**Where to implement:** `internal/transform/writer.go` in `WriteStage()`.

**Schema:**

```json
{
  "schemaVersion": "v1",
  "stageName": "10_KubernetesPlugin",
  "craneVersion": "v0.0.6",
  "timestamp": "2026-04-18T14:33:15Z",
  "duration": "3.2s",
  "plugins": [
    {
      "name": "KubernetesTransformPlugin",
      "version": "v0.0.1",
      "source": "built-in",
      "optionalFlags": {
        "strip-default-rbac": "true",
        "registry-replacement": "quay.io/old=quay.io/new"
      }
    }
  ],
  "inputSource": "export/resources/my-app",
  "summary": {
    "totalResources": 47,
    "patched": 32,
    "whitedOut": 8,
    "unchanged": 7,
    "ignoredPatchConflicts": 3
  }
}
```

**Implementation plan:**

| Step | File | Change |
|---|---|---|
| 1 | `internal/audit/stage_metadata.go` (new) | Define `StageMetadata` struct |
| 2 | `internal/transform/writer.go` | Accept metadata, write `.crane-metadata.json` in `WriteStage()` |
| 3 | `internal/transform/orchestrator.go` | Collect metadata during `RunSingleStage()` and `executeStage()`, pass to writer |

**crane-lib changes:** Expose plugin metadata from `Runner.Run()` -- either return it in `RunnerResponse` or add a method to retrieve loaded plugins.

---

### 4.3 Resource Inventory Report (`reports/resource-inventory.json`)

A per-stage structured inventory of every resource and what happened to it.
This is the core "forensic" artifact -- given any resource, you can look up its entire journey.

**Where to implement:** `internal/transform/writer.go` after all artifacts are processed.

**Schema:**

```json
{
  "schemaVersion": "v1",
  "stageName": "10_KubernetesPlugin",
  "resources": [
    {
      "apiVersion": "apps/v1",
      "kind": "Deployment",
      "namespace": "my-app",
      "name": "frontend",
      "uid": "abc-123-...",
      "outcome": "patched",
      "patchCount": 5,
      "patchOperations": ["remove /metadata/uid", "remove /metadata/resourceVersion", "replace /spec/template/spec/containers/0/image"],
      "pluginName": "KubernetesTransformPlugin",
      "sourcePath": "export/resources/my-app/Deployment_apps_v1_my-app_frontend.yaml",
      "patchPath": "transform/10_KubernetesPlugin/patches/my-app--v1--Deployment--frontend.patch.yaml"
    },
    {
      "apiVersion": "v1",
      "kind": "Endpoints",
      "namespace": "my-app",
      "name": "frontend",
      "uid": "def-456-...",
      "outcome": "whited-out",
      "reason": "KubernetesTransformPlugin whiteouts Endpoints by default",
      "pluginName": "KubernetesTransformPlugin"
    },
    {
      "apiVersion": "v1",
      "kind": "ConfigMap",
      "namespace": "my-app",
      "name": "my-config",
      "uid": "ghi-789-...",
      "outcome": "unchanged",
      "patchCount": 0,
      "pluginName": "KubernetesTransformPlugin"
    }
  ]
}
```

**Implementation plan:**

| Step | File | Change |
|---|---|---|
| 1 | `internal/audit/resource_inventory.go` (new) | Define `ResourceInventory`, `ResourceEntry` structs |
| 2 | `internal/audit/resource_inventory.go` | `BuildFromArtifacts([]TransformArtifact)` constructs inventory |
| 3 | `internal/transform/writer.go` | After writing kustomization, generate + write inventory |
| 4 | `internal/file/file_helper.go` | Add `GetResourceInventoryPath(stageName)` (for consistency) |

**crane-lib changes:** None -- all data already available in `TransformArtifact`.

---

### 4.4 Plugin Audit Trail (`reports/plugin-audit.json`)

Records exactly which plugins were loaded, in what order, and their per-resource decisions.
Critical for debugging "why did my Deployment lose its nodeSelector?"

**Where to implement:** `internal/audit/plugin_audit.go` (new), called from orchestrator.

**Schema:**

```json
{
  "schemaVersion": "v1",
  "stageName": "10_KubernetesPlugin",
  "pluginsLoaded": [
    {
      "name": "KubernetesTransformPlugin",
      "version": "v0.0.1",
      "source": "built-in",
      "priority": 0
    },
    {
      "name": "OpenShiftPlugin",
      "version": "v1.2.0",
      "source": "binary:/Users/ssingla/.local/share/crane/plugins/OpenShiftPlugin",
      "priority": 100
    }
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

**Implementation plan:**

| Step | File | Change |
|---|---|---|
| 1 | `internal/audit/plugin_audit.go` (new) | Define structs |
| 2 | crane-lib `transform/runner.go` | Return per-plugin responses (not just merged result) |
| 3 | `internal/transform/orchestrator.go` | Collect per-plugin decisions, build audit trail |
| 4 | `internal/transform/writer.go` | Write `plugin-audit.json` alongside other reports |

**crane-lib changes required:**

The current `Runner.Run()` merges all plugin responses into a single `RunnerResponse`,
discarding per-plugin attribution. To support the plugin audit trail, we need to either:

**Option A (preferred):** Add a `RunDetailed()` method to `Runner`:

```go
type DetailedRunnerResponse struct {
    RunnerResponse                         // Existing merged response
    PerPluginResponses []PluginResult      // NEW: per-plugin breakdown
}

type PluginResult struct {
    PluginName string
    Response   PluginResponse
    Duration   time.Duration
    Error      error
}

func (r *Runner) RunDetailed(object unstructured.Unstructured, plugins []Plugin) (DetailedRunnerResponse, error)
```

**Option B:** Add a callback/observer interface:

```go
type RunObserver interface {
    OnPluginStart(pluginName string, resource unstructured.Unstructured)
    OnPluginComplete(pluginName string, response PluginResponse, duration time.Duration, err error)
    OnConflictResolved(path string, winner, loser PluginOperation)
}
```

**Recommendation:** Option A is simpler and more testable. Option B is more extensible but
adds interface complexity. Start with A; evolve to B if needed.

---

### 4.5 Whiteout Report (`whiteouts/whiteouts.json`)

Path already defined. Records which resources were excluded and why.

**Schema:**

```json
{
  "schemaVersion": "v1",
  "stageName": "10_KubernetesPlugin",
  "whitedOutResources": [
    {
      "apiVersion": "v1",
      "kind": "Endpoints",
      "namespace": "my-app",
      "name": "frontend",
      "pluginName": "KubernetesTransformPlugin",
      "reason": "default-whiteout"
    },
    {
      "apiVersion": "v1",
      "kind": "Pod",
      "namespace": "my-app",
      "name": "frontend-6d9f8b7c5-abc12",
      "pluginName": "KubernetesTransformPlugin",
      "reason": "default-whiteout"
    }
  ],
  "totalWhitedOut": 8,
  "totalRetained": 39
}
```

**Implementation plan:**

| Step | File | Change |
|---|---|---|
| 1 | `internal/audit/whiteout_report.go` (new) | Define struct, build from artifacts |
| 2 | `internal/transform/writer.go` | Collect whiteout artifacts (currently skipped in loop), write report |

**crane-lib changes:** Add a whiteout reason to `PluginResponse` or `TransformArtifact`.
Currently `IsWhiteOut` is a bare boolean -- there's no way to know _why_ a resource was
whited out. Options:

```go
// In crane-lib/transform/plugin.go
type PluginResponse struct {
    Version       string          `json:"version,omitempty"`
    IsWhiteOut    bool            `json:"isWhiteOut,omitempty"`
    WhiteOutReason string         `json:"whiteOutReason,omitempty"`  // NEW
    Patches       jsonpatch.Patch `json:"patches,omitempty"`
}
```

This is backward-compatible (omitempty) and doesn't break existing plugins.

---

### 4.6 Ignored Patches Report (`reports/ignored-patches.json`)

Path already defined. Surfaces the data that `Runner.sanitizePatches()` computes but that
the CLI currently throws away.

**Schema:**

```json
{
  "schemaVersion": "v1",
  "stageName": "10_KubernetesPlugin",
  "ignoredPatches": [
    {
      "resource": "Deployment/my-app/frontend",
      "operation": {"op": "replace", "path": "/spec/replicas", "value": 3},
      "proposedByPlugin": "ScalingPlugin",
      "rejectedInFavorOf": "KubernetesTransformPlugin",
      "reason": "path-conflict-priority",
      "winnerOperation": {"op": "remove", "path": "/spec/replicas"}
    }
  ],
  "totalIgnored": 3
}
```

**Implementation plan -- the wiring is 90% done:**

| Step | File | Change |
|---|---|---|
| 1 | `internal/audit/ignored_patches_report.go` (new) | Define struct |
| 2 | `internal/transform/orchestrator.go:86,215` | **Fix the TODO**: parse `response.IgnoredPatches` instead of hardcoding `[]` |
| 3 | `internal/transform/writer.go` | Write report from `artifact.IgnoredOps` |

**crane-lib changes:** None -- `sanitizePatches()` already returns `ignoredPatches`, and
`RunnerResponse.IgnoredPatches` already serializes them. The CLI just needs to consume them.

---

### 4.7 Export Inventory Report

The export phase currently records failures but has no structured record of successes.

**Where to write:** `export/.crane-export-inventory.json`

**Schema:**

```json
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
      "names": ["frontend", "backend", "worker"],
      "outputPath": "export/resources/my-app/Deployment_apps_v1_my-app_*.yaml"
    }
  ],
  "clusterResources": [
    {
      "kind": "ClusterRoleBinding",
      "count": 2,
      "names": ["frontend-binding", "backend-binding"],
      "relatedServiceAccounts": ["frontend-sa", "backend-sa"]
    }
  ],
  "failures": [
    {
      "apiVersion": "v1",
      "kind": "ConfigMap",
      "error": "Forbidden: configmaps is forbidden",
      "errorType": "RBAC"
    }
  ],
  "totalExported": 47,
  "totalFailed": 2
}
```

**Implementation plan:**

| Step | File | Change |
|---|---|---|
| 1 | `internal/audit/export_inventory.go` (new) | Define struct |
| 2 | `cmd/export/export.go` | Collect resource counts during `Run()`, write inventory at end |
| 3 | `cmd/export/discover.go` | Return structured discovery stats (admitted/skipped counts) |

**crane-lib changes:** None.

---

### 4.8 Persistent Structured Log File (`.crane-audit.log`)

Console logs vanish. A persistent, structured log file survives.

**Format:** JSON Lines (one JSON object per line) for easy grep/jq processing.

```jsonl
{"ts":"2026-04-18T14:32:01.123Z","level":"info","cmd":"export","msg":"Starting export","namespace":"my-app","flags":{"label-selector":"app=frontend"}}
{"ts":"2026-04-18T14:32:01.456Z","level":"info","cmd":"export","msg":"Discovered 127 API resource types"}
{"ts":"2026-04-18T14:32:02.789Z","level":"warn","cmd":"export","msg":"Cannot list resource","kind":"ConfigMap","error":"Forbidden"}
{"ts":"2026-04-18T14:32:12.345Z","level":"info","cmd":"export","msg":"Export complete","exported":47,"failed":2,"duration":"11.2s"}
```

**Always-on behavior:** The audit log file is always written. Every run produces a
`.crane-audit.log` in the working directory by default. The `--audit-log` flag exists
only to customize the output path (e.g., `--audit-log /var/log/crane/audit.log`).

**Implementation plan:**

| Step | File | Change |
|---|---|---|
| 1 | `internal/audit/audit_logger.go` (new) | Create `AuditLogger` wrapping logrus with file + JSON formatter |
| 2 | `internal/flags/global_flags.go` | Add `--audit-log` flag (default: `.crane-audit.log`) for path override |
| 3 | `internal/flags/global_flags.go` | In `GetLogger()`, always attach file hook |
| 4 | All commands | No changes -- they already use `log.Infof(...)`, the file hook captures it |

**Key design decision:** The audit log uses a logrus `Hook` that writes to a file.
This means _every existing log statement_ is automatically captured without changing
any command code. The console output remains as-is (text formatter); the file gets
JSON Lines.

**crane-lib changes:** None -- crane-lib already uses `logrus.Logger` passed from the CLI.

---

## 5. crane-lib Changes Summary

All proposed crane-lib changes are backward-compatible additions:

| Change | File | Breaking? | Priority |
|---|---|---|---|
| Add `WhiteOutReason` to `PluginResponse` | `transform/plugin.go` | No (omitempty) | Medium |
| Add `RunDetailed()` to `Runner` | `transform/runner.go` | No (new method) | High |
| Define `DetailedRunnerResponse` + `PluginResult` | `transform/runner.go` | No (new types) | High |
| Add `time.Duration` tracking in `Run()`/`RunDetailed()` | `transform/runner.go` | No | Medium |
| Parse `IgnoredPatches` into `[]IgnoredOperation` helper | `transform/runner.go` or `transform/types.go` | No (new func) | High |

**Changes NOT needed in crane-lib (CLI-only concerns):**
- Run manifest (CLI-only concern)
- Export inventory (CLI handles discovery)
- Persistent log file (logrus hook in CLI)
- Summary output (CLI formatting)

---

## 6. Implementation Plan

Five GitHub issues, ordered by dependency and risk. See
[`crane-auditability-github-backlog.md`](./crane-auditability-github-backlog.md)
for full issue bodies ready to paste into GitHub.

### Issue 1 — Persistent structured audit log (`.crane-audit.log`)

Always-on logrus file hook in `GetLogger()`. Every run writes a JSON Lines log file.
`--audit-log` flag for path override only. No changes to existing commands.

**Scope:** `internal/audit/audit_logger.go` (new), `internal/flags/global_flags.go` (modified).
**crane-lib changes:** None.

### Issue 2 — Run manifest (`.crane-run.json`)

Written at the end of every `export`, `transform`, and `apply` invocation. Records
command, versions, timestamp, duration, user, hostname, flags, kubernetes context, result.

**Scope:** `internal/audit/run_manifest.go` (new), each `cmd/*/` `Run()` method.
**crane-lib changes:** None.

### Issue 3 — Export inventory report (`.crane-export-inventory.json`)

Structured record of discovery results: API types discovered, admitted, skipped (with
reasons), per-type resource lists, failures.

**Scope:** `internal/audit/export_inventory.go` (new), `cmd/export/export.go`, `cmd/export/discover.go`.
**crane-lib changes:** None.

### Issue 4 — Transform audit artifacts

Populates the four scaffolded-but-empty report files. Fixes the `IgnoredOps` TODO.

| Artifact | Path | Status |
|---|---|---|
| `.crane-metadata.json` | `PathOpts.GetMetadataPath()` | Path exists, never written |
| `resource-inventory.json` | `reports/` | New |
| `whiteouts.json` | `PathOpts.GetWhiteoutReportPath()` | Path exists, never written |
| `ignored-patches.json` | `PathOpts.GetIgnoredPatchReportPath()` | Path exists, never written |

**Scope:** `internal/transform/orchestrator.go` (fix TODO), `internal/transform/writer.go`,
new files in `internal/audit/`.
**crane-lib changes:** None.

### Issue 5 — Deep plugin audit trail (`plugin-audit.json`, requires crane-lib)

Per-plugin, per-resource decision tracking with timing and conflict resolution details.
Adds `RunDetailed()` to crane-lib Runner and `WhiteOutReason` to `PluginResponse`.

**Scope:** crane-lib `transform/runner.go`, `transform/plugin.go`, `transform/kubernetes/`;
crane `internal/audit/plugin_audit.go` (new), `internal/transform/orchestrator.go`.
**crane-lib changes:** Required (backward-compatible additions only).

### Recommended order

1. **Issue 1** (audit log) — foundation, unblocks everything.
2. **Issue 2** (run manifest) — depends on nothing, high standalone value.
3. **Issue 3** (export inventory) — independent of transform work.
4. **Issue 4** (transform artifacts) — largest scope, but no crane-lib changes.
5. **Issue 5** (plugin audit) — last, requires crane-lib PR coordination.

Issues 1–3 can be worked in parallel. Issue 4 is independent. Issue 5 comes last.

---

## 7. New Package Structure

```
internal/audit/
├── audit.go               # Shared types: SchemaVersion, ResourceRef, etc.
├── audit_logger.go        # Logrus file hook for persistent JSON Lines log
├── run_manifest.go        # RunManifest struct + NewRunManifest() + WriteToDir()
├── export_inventory.go    # ExportInventory struct + builder
├── resource_inventory.go  # Per-stage ResourceInventory + ResourceEntry
├── stage_metadata.go      # StageMetadata struct (populates .crane-metadata.json)
├── plugin_audit.go        # PluginAuditTrail struct
├── whiteout_report.go     # WhiteoutReport struct
└── ignored_patches_report.go  # IgnoredPatchesReport struct
```

All types implement a common interface:

```go
type AuditArtifact interface {
    SchemaVersion() string
    WriteJSON(path string) error
}
```

---

## 8. Flag Summary

| Flag | Scope | Default | Description |
|---|---|---|---|
| `--audit-log <path>` | Global | `.crane-audit.log` | Override path for persistent JSON Lines log |

All audit artifacts (run manifest, inventories, reports, persistent log) are **always
written**. No opt-in flag is needed. The `--audit-log` flag exists only to customize
the log file path.

---

## 9. Testing Strategy

| Layer | What to Test | Approach |
|---|---|---|
| Unit | Each `internal/audit/` type serializes correctly | Table-driven tests with golden JSON files |
| Unit | `RunManifest` captures all flags | Mock cobra command, verify all fields populated |
| Unit | `ResourceInventory` correctly classifies artifacts | Build test artifacts, verify counts |
| Unit | `IgnoredOps` parsing (fix the TODO) | Use existing `sanitizePatches` test data |
| Integration | Full export+transform+apply writes all audit files | E2E test that checks file existence + schema |
| Integration | Audit log captures all log levels | Run command with `--audit-log`, parse JSON Lines |

---

## 10. Risks & Mitigations

| Risk | Impact | Mitigation |
|---|---|---|
| Audit files grow large on big namespaces | Disk usage | Keep reports per-stage (not cumulative); lightweight JSON keeps size manageable |
| crane-lib changes may break binary plugins | Plugin ecosystem | `WhiteOutReason` uses `omitempty`; `RunDetailed()` is a new method, not a change to `Run()` |
| Performance overhead of per-plugin timing | Latency | Use `time.Since()` (nanosecond precision, negligible overhead) |
| JSON schema evolution | Backward compat | `schemaVersion` field in every file; versioned schemas from day 1 |
| Sensitive data in audit logs (secrets, tokens) | Security | Audit log captures logrus output (which already avoids secrets); run manifest captures flag _names_ not kubeconfig _contents_ |

---

## 11. Open Questions

1. **Should audit files be `.gitignore`-able?** The `.crane-*` prefix makes this easy:
   ```
   .crane-audit.log
   .crane-run.json
   ```

2. **Should `crane apply` record which resources it would create/update/delete on the target cluster?** This is a _deployment_ audit, beyond the scope of transform auditability, but worth considering for Phase 4.

3. **Should there be a `crane validate --audit` that reads audit files and checks for common issues?** (e.g., "you have 3 ignored patch conflicts -- review them before applying")

4. **Should audit data include checksums (SHA256) of input/output files?** Useful for tamper-detection in regulated environments but adds complexity.

5. **Plugin protocol versioning:** Should `RunDetailed()` response data be available to binary plugins via a new protocol version (`v2`), or is it purely CLI-internal?

---

## 12. Success Criteria

When this feature is complete, a security auditor or support engineer should be able to:

1. **Identify** who ran Crane, when, on which cluster, with which flags -- from `.crane-run.json`
2. **Enumerate** every resource that was exported, transformed, or excluded -- from `resource-inventory.json` and `.crane-export-inventory.json`
3. **Explain** why any specific resource was modified the way it was -- from `plugin-audit.json`
4. **Review** patch conflicts and understand their resolution -- from `ignored-patches.json`
5. **List** all resources excluded from migration -- from `whiteouts.json`
6. **Replay** the full sequence of events -- from `.crane-audit.log`

All without re-running the migration or needing access to the original cluster.
