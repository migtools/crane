# Unified Implementation Plan: Observability UX + Forensic Auditability

**Related issues:**
- Observability UX: [migtools/crane#199](https://github.com/migtools/crane/issues/199) — see `docs/crane-observability-ux-github-backlog.md`
- Auditability: [migtools/crane#200](https://github.com/migtools/crane/issues/200) — see `docs/forensic-postmortem-auditability-plan.md`

**Date:** 2026-04-18

---

## 1. Why a Unified Plan

These two features touch the same code paths, the same data, and the same infrastructure.
Built independently they would create redundant counters, conflicting logger changes, and
duplicate packages. Built together they reinforce each other: observability collects the
data, auditability persists it.

| Concern | Observability (#199) | Auditability (#200) |
|---|---|---|
| **Consumer** | Human watching the terminal right now | Auditor/support engineer reading files later |
| **Medium** | stderr / console | JSON files on disk + persistent log |
| **Lifecycle** | Ephemeral (gone when terminal closes) | Permanent (survives across sessions) |
| **Data needed** | Phase names, resource counts, duration | Same + flags, versions, k8s context, per-resource decisions |

They are **not competing features** — they are **two views of the same instrumentation**.

---

## 2. Feasibility Assessment of Observability Backlog

### Issue 1 — Foundation: shared patterns + export log noise

**Feasibility: Straightforward.**

The work is well-scoped. `GetLogger()` in `internal/flags/global_flags.go` is the single
entry point — adding shared helpers for phase/summary output alongside it is clean. Moving
per-type `log.Infof("adding resource: %s ...")` lines in `cmd/export/discover.go:268` and
`log.Infof("Writing objects of resource: %s ...")` in `cmd/export/discover.go:89` to
`log.Debugf` is a safe change (~10 lines).

The only design decision: where to put the shared helpers. Recommendation: `internal/cli/`
for presentation helpers (phase lines, summary blocks) since they are console-only concerns,
separate from the audit persistence code.

### Issue 2 — Export: phased ladder + summary

**Feasibility: Straightforward.**

`ExportOptions.Run()` already has clear sequential steps: discovery, listing, cluster RBAC
filtering, CRD collection, writing. Wrapping each in a phase line is mechanical. The
challenge is that `resourceToExtract()` returns `([]*groupResource, []*groupResourceError)`
but doesn't return aggregate counts for "admitted vs skipped" — a small signature change
or separate counter is needed.

### Issue 3 — Transform: banner + throttled progress + summary

**Feasibility: Moderate.**

The inner loop is in `orchestrator.go` (`RunSingleStage` and `executeStage`), which
iterates over files and calls `runner.Run()` per resource. Adding a counter + throttled
progress line (every N files or every 10%) is straightforward. The complexity is in
multi-stage mode where each stage is a sub-loop.

Plugin count is available from `plugin.GetFilteredPlugins()`, which returns `[]cranelib.Plugin`.
File/document count comes from `file.ReadFiles()`. Both are already computed before the loop.

### Issue 4 — Apply: banner + phases + summary

**Feasibility: Straightforward.**

Apply has very few steps (validate kubectl, run kustomize, write output, optionally split).
The `splitMultiDocYAMLToFiles` method already iterates resources and could count them.
Multi-stage mode runs a loop over stages, each with the same shape.

### Issue 5 — Transfer-PVC: phases + summary

**Feasibility: Moderate-to-Hard.**

`cmd/transfer-pvc/transfer-pvc.go` is 896 lines, uses a different logging pattern (standard
library `log` + logr bridge), mixes stdout rsync progress with stderr logs, and depends on
crane-lib's state_transfer package. Wrapping phases around the existing flow is doable but
requires understanding the full state machine (PVC create, endpoint, stunnel, rsync, cleanup).

This is the highest-risk issue and should come last, as the backlog document recommends.

### Issue 6 — Transfer-PVC: honor `--debug`

**Feasibility: Moderate.**

The transfer-pvc command doesn't use `GlobalFlags.GetLogger()` — it has its own logger
setup. Wiring it through requires touching the command's initialization and potentially the
logr-to-logrus bridge. Not hard, but requires care to avoid breaking the rsync progress output.

### Overall Verdict

**All 6 issues are feasible.** Issues 1-4 are clean, well-scoped work. Issues 5-6 are
messier due to transfer-pvc's legacy patterns but still achievable. The backlog document's
recommended order (Foundation -> Export -> Transform -> Apply -> Transfer-PVC) is correct.

---

## 3. Overlap Map

### 3.1 Shared Data

Both features need the same runtime counters. Here is every data point and who consumes it:

| Data Point | Observability Uses It For | Auditability Uses It For |
|---|---|---|
| Command name, flags | Banner opening line | Run manifest `.crane-run.json` |
| Namespace, context | Banner opening line | Run manifest kubernetes context |
| crane/crane-lib versions | (not shown in UX) | Run manifest version fields |
| API types discovered count | Phase line `[2/5] ... 38 API types` | Export inventory `apiResourcesDiscovered` |
| API types admitted vs skipped | Phase line aggregate | Export inventory `skippedReasons` |
| Resources exported count | Summary `resources: 47 exported` | Export inventory `totalExported` |
| Resources failed count | Summary `failures: 3 list/write issues` | Export inventory `totalFailed` + failures array |
| Cluster resources count | Phase line `[3/5] ... 4 resources` | Export inventory `clusterResources` array |
| CRDs fetched count | Phase line `[4/5] ... 1 fetched` | Export inventory (included in resources) |
| Files written count | Summary `412 files written` | Export inventory (derivable from resources) |
| Duration | Summary `duration: 1m 04s` | Run manifest `duration` |
| Plugins loaded count | Banner `plugins: 8 loaded` | Stage metadata `plugins` array |
| Plugins skipped | Banner `(0 skipped)` | Plugin audit `pluginsSkipped` |
| Files/docs to process | Banner `520 YAML documents` | Stage metadata `summary.totalResources` |
| Resources patched count | Summary `transform files written: 518` | Resource inventory (count by outcome) |
| Whiteouts count | Summary `whiteouts created: 2` | Whiteout report `totalWhitedOut` |
| Ignored patches count | Summary `ignored patches: 1 file` | Ignored patches report `totalIgnored` |
| Transform duration | Summary `duration: 2m 11s` | Stage metadata `duration` |
| Kustomize build success | Phase line `[1/3] ... ok` | Run manifest result status |
| Output resources count | Summary `resources written: 42` | Run manifest `resourcesProcessed` |
| Apply duration | Summary `duration: 48s` | Run manifest `duration` |

**Key insight:** Every number shown in an observability summary is also a field in an
auditability report. They must come from the same counter, not be computed independently.

### 3.2 Shared Infrastructure

| Infrastructure | Observability Needs | Auditability Needs | Shared Solution |
|---|---|---|---|
| Logger changes | Move per-type Info->Debug | Add file hook for persistent log | Both modify `GetLogger()` |
| Phase tracking | `[1/N]` console lines | Structured phase records | `PhaseTracker` that does both |
| Resource counters | Totals for summary | Totals + per-resource details | `RunStats` accumulator |
| Summary printer | Console table after each cmd | Same data serialized to JSON | Shared `RunStats` -> two renderers |
| New global flags | (none proposed) | `--audit-log` (path override) | Add alongside existing flags |
| Package location | `internal/cli/` for presentation | `internal/audit/` for persistence | Both, with shared types in `internal/runstats/` |

### 3.3 Conflicts and Tensions

| Tension | Resolution |
|---|---|
| Observability **reduces** console noise; auditability **captures** everything | Complementary: quieter console + persistent file. Move spam to Debug AND add a file hook that captures all levels. |
| Observability summaries are simple text; auditability needs JSON artifacts | Same `RunStats` struct -> one `PrintSummary()` for console (includes audit artifact paths), separate `WriteJSON()` for machine-readable files. |
| Observability is "ship fast, iterate"; auditability needs stable schemas | Define `schemaVersion` from day one. Console format can evolve freely. |
| Observability doesn't need crane-lib changes; auditability Phase 3 does | Sequence work so observability ships first (Phase 1-2), auditability Phase 3 comes later. |

---

## 4. Unified Architecture

### 4.1 Package Layout

```
internal/
├── cli/                          # Console presentation (observability + unified summary)
│   ├── phase.go                  # PhaseTracker: prints [1/N] lines to stderr
│   ├── summary.go                # Renders RunStats as text table to stderr (includes audit artifact paths)
│   ├── summary_markdown.go       # Optional --summary-file Markdown writer
│   └── banner.go                 # Prints command opening banner
│
├── runstats/                     # Shared data model (both features)
│   ├── stats.go                  # RunStats struct: all counters + metadata
│   ├── resource_entry.go         # Per-resource outcome record
│   └── phase_record.go           # Structured phase start/end with duration
│
├── audit/                        # Persistence (auditability) -- JSON artifacts only, no console output
│   ├── audit_logger.go           # Logrus file hook for .crane-audit.log (always-on)
│   ├── run_manifest.go           # Serializes RunStats -> .crane-run.json
│   ├── export_inventory.go       # Serializes RunStats -> export inventory
│   ├── resource_inventory.go     # Serializes per-resource records
│   ├── stage_metadata.go         # Serializes -> .crane-metadata.json
│   ├── plugin_audit.go           # Per-plugin decision records
│   ├── whiteout_report.go        # Whiteout report writer
│   └── ignored_patches_report.go # Ignored patches report writer
│
├── flags/                        # (existing) GlobalFlags
│   └── global_flags.go           # Modified: new flags, enhanced GetLogger()
│
├── file/                         # (existing) file helpers
│   └── file_helper.go            # (existing) PathOpts
│
└── transform/                    # (existing) orchestrator, writer
    ├── orchestrator.go            # Modified: collects RunStats during execution
    └── writer.go                  # Modified: writes audit artifacts after stage
```

### 4.2 The RunStats Struct (Heart of Both Features)

This is the shared data structure that both observability and auditability consume:

```go
// internal/runstats/stats.go
package runstats

import "time"

// RunStats accumulates all data needed by both console summaries
// and persistent audit files during a single command invocation.
type RunStats struct {
    // Invocation context
    Command       string            `json:"command"`
    CraneVersion  string            `json:"craneVersion"`
    CranelibVersion string          `json:"cranelibVersion"`
    Timestamp     time.Time         `json:"timestamp"`
    User          string            `json:"user"`
    Hostname      string            `json:"hostname"`
    Flags         map[string]string `json:"flags"`

    // Kubernetes context (export only)
    KubernetesContext *KubeContext  `json:"kubernetes,omitempty"`

    // Phases (populated as command progresses)
    Phases        []PhaseRecord     `json:"phases"`
    currentPhase  int

    // Export-specific counters
    Export        *ExportStats      `json:"export,omitempty"`

    // Transform-specific counters
    Transform     *TransformStats   `json:"transform,omitempty"`

    // Apply-specific counters
    Apply         *ApplyStats       `json:"apply,omitempty"`

    // Timing
    Duration      time.Duration     `json:"duration"`

    // Overall result
    Status        string            `json:"status"` // "completed", "completed_with_warnings", "failed"
    Errors        []string          `json:"errors,omitempty"`
    Warnings      int               `json:"warnings"`

    // Output paths (for summary display)
    AuditLogPath  string            `json:"-"`
    OutputDir     string            `json:"-"`
    SummaryFile   string            `json:"-"` // optional --summary-file path
}

type ExportStats struct {
    Namespace             string `json:"namespace"`
    APIResourcesDiscovered int   `json:"apiResourcesDiscovered"`
    APIResourcesAdmitted   int   `json:"apiResourcesAdmitted"`
    APIResourcesSkipped    int   `json:"apiResourcesSkipped"`
    ResourcesExported      int   `json:"resourcesExported"`
    ResourcesFailed        int   `json:"resourcesFailed"`
    ClusterResources       int   `json:"clusterResources"`
    CRDsFetched            int   `json:"crdsFetched"`
    FilesWritten           int   `json:"filesWritten"`
}

type TransformStats struct {
    StageName       string `json:"stageName"`
    PluginsLoaded   int    `json:"pluginsLoaded"`
    PluginsSkipped  int    `json:"pluginsSkipped"`
    TotalResources  int    `json:"totalResources"`
    Patched         int    `json:"patched"`
    WhitedOut       int    `json:"whitedOut"`
    Unchanged       int    `json:"unchanged"`
    PatchOpsApplied int    `json:"patchOpsApplied"`
    ConflictsResolved int  `json:"conflictsResolved"`
}

type ApplyStats struct {
    Mode             string `json:"mode"` // "final-stage" or "multi-stage"
    StagesApplied    int    `json:"stagesApplied"`
    ResourcesWritten int    `json:"resourcesWritten"`
}

type PhaseRecord struct {
    Index     int           `json:"index"`
    Total     int           `json:"total"`
    Name      string        `json:"name"`
    Status    string        `json:"status"` // "ok", "skipped", "failed"
    Detail    string        `json:"detail,omitempty"`
    StartTime time.Time     `json:"startTime"`
    Duration  time.Duration `json:"duration"`
}
```

**Usage in commands:**

```go
// In cmd/export/export.go Run():
stats := runstats.New("export", craneVersion, cranelibVersion)
stats.Export = &runstats.ExportStats{Namespace: o.userSpecifiedNamespace}

// Each phase updates the stats:
stats.StartPhase("Discovery")
// ... do work ...
stats.Export.APIResourcesDiscovered = len(resourceLists)
stats.EndPhase("ok", fmt.Sprintf("%d API resource types", len(resourceLists)))

// At the end:
stats.Complete()

// One summary for both features: prints counters + audit artifact paths
cli.PrintSummary(o.IOStreams.ErrOut, stats)
if stats.SummaryFile != "" {
    cli.WriteSummaryMarkdown(stats.SummaryFile, stats)
}

// Audit artifacts (always written, no flag needed)
audit.WriteRunManifest(o.exportDir, stats)
audit.WriteExportInventory(o.exportDir, stats)
```

### 4.3 PhaseTracker (Dual-Purpose)

```go
// internal/cli/phase.go
package cli

// PhaseTracker prints [i/N] lines to stderr AND records
// structured phase data in RunStats for audit persistence.
type PhaseTracker struct {
    w     io.Writer       // stderr
    stats *runstats.RunStats
    total int
}

func NewPhaseTracker(w io.Writer, stats *runstats.RunStats, totalPhases int) *PhaseTracker

// Start prints "[1/5] Discovery ..." and records phase start time
func (p *PhaseTracker) Start(name string)

// End prints "ok" / detail and records phase completion + duration
func (p *PhaseTracker) End(status, detail string)
```

When `Start("Discovery")` is called, it simultaneously:
1. Prints `[1/5] Discovery ...` to stderr (observability)
2. Appends a `PhaseRecord{Index: 1, Name: "Discovery", StartTime: now}` to `stats.Phases` (auditability)

When `End("ok", "38 API types")` is called:
1. Appends ` ok (38 API types)\n` to the console line (observability)
2. Sets `Duration` and `Status` on the PhaseRecord (auditability)

One call, two outputs, zero duplication.

### 4.4 Enhanced GetLogger() (Dual-Purpose)

```go
// internal/flags/global_flags.go
type GlobalFlags struct {
    ConfigFile string
    Debug      bool
    AuditLog   string // NEW: override path for persistent log file (default: .crane-audit.log)
}

func (g *GlobalFlags) ApplyFlags(cmd *cobra.Command) {
    cobra.OnInitialize(g.initConfig)
    cmd.PersistentFlags().BoolVar(&g.Debug, "debug", false, "Debug output")
    cmd.PersistentFlags().StringVarP(&g.ConfigFile, "flags-file", "f", "", "Path to flags file")
    cmd.PersistentFlags().StringVar(&g.AuditLog, "audit-log", ".crane-audit.log",
        "Path to persistent structured log file")
    viper.BindPFlags(cmd.PersistentFlags())
}

func (g *GlobalFlags) GetLogger() *logrus.Logger {
    log := logrus.New()
    if g.Debug {
        log.SetLevel(logrus.DebugLevel)
    }

    // Always attach file hook -- audit log is always-on
    hook, err := audit.NewFileHook(g.AuditLog)
    if err == nil {
        log.AddHook(hook)
    }

    return log
}
```

This is the key integration point:
- **Observability** reduces console noise by moving lines to Debug
- **Auditability** captures those Debug lines anyway via the always-on file hook
- Both are controlled from one `GetLogger()` call
- Existing command code that calls `log.Infof(...)` / `log.Debugf(...)` benefits from both
  changes with zero modifications
- No flag needed -- the audit log is always written

---

## 5. Unified Implementation Schedule

The two features share a foundation layer but have different risk profiles. This schedule
interleaves them to maximize shared work and minimize rework.

### Wave 1: Shared Foundation (Weeks 1-2)

**One PR. Enables everything that follows.**

| Task | Source | Touches |
|---|---|---|
| Create `internal/runstats/` with `RunStats`, `PhaseRecord`, counter structs | Both | New package |
| Create `internal/cli/` with `PhaseTracker`, `PrintSummary`, `PrintBanner` | Obs #1 | New package |
| Create `internal/audit/audit_logger.go` with logrus `FileHook` | Aud 4.8 | New file |
| Add `--audit-log` to `GlobalFlags` (default `.crane-audit.log`) | Aud 4.8 | `global_flags.go` |
| Enhance `GetLogger()` to always attach file hook | Aud 4.8 | `global_flags.go` |
| Move export per-type spam from `Info` to `Debug` | Obs #1 | `discover.go:89,268` |

**Result:** Shared infrastructure exists. Console is already quieter. Audit log works
(every existing `log.Infof/Debugf` is now always captured to `.crane-audit.log`).

### Wave 2: Export Observability + Auditability (Weeks 2-3)

**Two PRs that can be parallel (one for console UX, one for audit files) or one combined PR.**

| Task | Source | Touches |
|---|---|---|
| Add phase ladder to `ExportOptions.Run()` using `PhaseTracker` | Obs #2 | `export.go` |
| Collect `ExportStats` counters during discovery/listing/writing | Both | `export.go`, `discover.go` |
| Print summary after export completes | Obs #2 | `export.go` |
| Write `.crane-run.json` from `RunStats` | Aud 4.1 | `export.go` + `run_manifest.go` |
| Write `.crane-export-inventory.json` from `RunStats.Export` | Aud 4.7 | `export.go` + `export_inventory.go` |

**Result:** `crane export` shows phased progress, prints a summary, AND writes
audit files — all from the same counters collected during the run.

### Wave 3: Transform Observability + Auditability (Weeks 3-5)

**The largest wave. Multiple PRs recommended.**

| Task | Source | Touches |
|---|---|---|
| Fix `IgnoredOps` TODO (parse `response.IgnoredPatches`) | Aud 4.6 | `orchestrator.go:86,215` |
| Add banner + throttled progress to transform | Obs #3 | `transform.go`, `orchestrator.go` |
| Collect `TransformStats` during `RunSingleStage`/`executeStage` | Both | `orchestrator.go` |
| Print transform summary | Obs #3 | `transform.go` |
| Write `.crane-run.json` for transform | Aud 4.1 | `transform.go` + `run_manifest.go` |
| Write `.crane-metadata.json` per stage | Aud 4.2 | `writer.go` + `stage_metadata.go` |
| Write `whiteouts.json` from whited-out artifacts | Aud 4.5 | `writer.go` + `whiteout_report.go` |
| Write `ignored-patches.json` from `IgnoredOps` | Aud 4.6 | `writer.go` + `ignored_patches_report.go` |
| Write `resource-inventory.json` per stage | Aud 4.3 | `writer.go` + `resource_inventory.go` |

**Result:** Transform shows progress and summary; all scaffolded-but-empty audit files
are now populated. The `IgnoredOps` TODO that has existed since the initial code is fixed.

### Wave 4: Apply Observability + Auditability (Week 5-6)

| Task | Source | Touches |
|---|---|---|
| Add banner + phases to apply (both modes) | Obs #4 | `apply.go`, `kustomize.go` |
| Collect `ApplyStats` (stages applied, resources written) | Both | `kustomize.go` |
| Print apply summary | Obs #4 | `apply.go` |
| Write `.crane-run.json` for apply | Aud 4.1 | `apply.go` + `run_manifest.go` |

**Result:** All three core commands (export, transform, apply) have consistent
phased output + summaries + audit files.

### Wave 5: Deep Plugin Audit (Week 6-8, requires crane-lib PR)

| Task | Source | Touches |
|---|---|---|
| Add `RunDetailed()` to crane-lib `Runner` | Aud 4.4 | crane-lib `runner.go` |
| Add `WhiteOutReason` to crane-lib `PluginResponse` | Aud 4.5 | crane-lib `plugin.go` |
| Update `KubernetesTransformPlugin` to set `WhiteOutReason` | Aud 4.5 | crane-lib `kubernetes.go` |
| Implement `plugin-audit.json` report | Aud 4.4 | `plugin_audit.go`, `orchestrator.go` |
| Update whiteout report to include reasons | Aud 4.5 | `whiteout_report.go` |

**Result:** Full per-plugin, per-resource decision tracking with reasons.

### Wave 6: Transfer-PVC + Polish (Week 8-10)

| Task | Source | Touches |
|---|---|---|
| Add phase ladder to transfer-pvc | Obs #5 | `transfer-pvc.go` |
| Wire `GlobalFlags`/`--debug` to transfer-pvc | Obs #6 | `transfer-pvc.go` |
| Optional: `crane audit` read/display subcommand | Aud Phase 4 | New `cmd/audit/` |

---

## 6. How Specific Code Paths Get Modified

### 6.1 `ExportOptions.Run()` — Before vs After

**Today** (`cmd/export/export.go:122-222`):
```go
func (o *ExportOptions) Run() error {
    log := o.globalFlags.GetLogger()
    // ... setup clients ...
    resources, resourceErrs := resourceToExtract(...)
    // ... filter, write ...
    return errorsutil.NewAggregate(errs)
}
```

**After (unified):**
```go
func (o *ExportOptions) Run() error {
    log := o.globalFlags.GetLogger()
    stats := runstats.New("export", buildinfo.Version, buildinfo.CranelibVersion)
    stats.Export = &runstats.ExportStats{Namespace: o.userSpecifiedNamespace}
    stats.CaptureFlags(o.configFlags, o.exportDir, o.labelSelector)
    phases := cli.NewPhaseTracker(o.IOStreams.ErrOut, stats, 5)

    // --- print banner ---
    cli.PrintBanner(o.IOStreams.ErrOut, "export", map[string]string{
        "namespace":  o.userSpecifiedNamespace,
        "export-dir": o.exportDir,
    })

    // --- phase 1: discovery ---
    phases.Start("Discovery")
    resourceLists, err := discoverPreferredResources(discoveryClient, log)
    // ...
    stats.Export.APIResourcesDiscovered = countAPIResources(resourceLists)
    phases.End("ok", fmt.Sprintf("preferred API lists refreshed, %d types", stats.Export.APIResourcesDiscovered))

    // --- phase 2: listing ---
    phases.Start("Listing namespace resources")
    resources, resourceErrs := resourceToExtract(...)
    stats.Export.APIResourcesAdmitted = len(resources)
    stats.Export.ResourcesFailed = len(resourceErrs)
    phases.End("ok", fmt.Sprintf("%d API types with objects", len(resources)))

    // ... phases 3-5 similarly ...

    // --- finalize ---
    stats.Complete()
    cli.PrintSummary(o.IOStreams.ErrOut, stats)           // unified summary (counters + artifact paths)
    audit.WriteRunManifest(o.exportDir, stats)             // always written
    audit.WriteExportInventory(o.exportDir, stats)         // always written

    return errorsutil.NewAggregate(errs)
}
```

Every counter is set once, used twice (console + file). No duplication.

### 6.2 `Orchestrator.RunSingleStage()` — Before vs After

**Today** (`internal/transform/orchestrator.go:36-107`):
```go
func (o *Orchestrator) RunSingleStage(stageName, pluginName string) error {
    files, err := file.ReadFiles(...)
    allPlugins, err := plugin.GetFilteredPlugins(...)
    runner := cranelib.Runner{...}

    for _, f := range files {
        response, err := runner.Run(f.Unstructured, plugins)
        // ...
        artifact := cranelib.TransformArtifact{
            IgnoredOps: []cranelib.IgnoredOperation{}, // TODO: Parse IgnoredPatches
        }
    }

    writer := NewKustomizeWriter(opts, stageName)
    writer.WriteStage(artifacts, o.Force)
    return nil
}
```

**After (unified):**
```go
func (o *Orchestrator) RunSingleStage(stageName, pluginName string, stats *runstats.RunStats) error {
    files, err := file.ReadFiles(...)
    allPlugins, err := plugin.GetFilteredPlugins(...)
    stats.Transform.TotalResources = len(files)
    stats.Transform.PluginsLoaded = len(allPlugins)

    runner := cranelib.Runner{...}

    for i, f := range files {
        response, err := runner.Run(f.Unstructured, plugins)

        // FIX THE TODO: parse IgnoredPatches
        var ignoredOps []cranelib.IgnoredOperation
        if len(response.IgnoredPatches) > 2 {
            _ = json.Unmarshal(response.IgnoredPatches, &ignoredOps)
            stats.Transform.ConflictsResolved += len(ignoredOps)
        }

        artifact := cranelib.TransformArtifact{
            IgnoredOps: ignoredOps,  // No longer hardcoded []
            // ... rest same ...
        }

        // Update counters (used by both summary + audit)
        switch {
        case response.HaveWhiteOut:
            stats.Transform.WhitedOut++
        case len(response.TransformFile) > 2:
            stats.Transform.Patched++
            stats.Transform.PatchOpsApplied += countOps(response.TransformFile)
        default:
            stats.Transform.Unchanged++
        }
    }

    writer := NewKustomizeWriter(opts, stageName)
    writer.WriteStage(artifacts, o.Force)
    // Audit artifacts written by writer (metadata, inventories, reports)
    return nil
}
```

### 6.3 `KustomizeWriter.WriteStage()` — Before vs After

**Today** (`internal/transform/writer.go:32-147`): writes resources, patches, kustomization.yaml.

**After:** same, plus at the end:

```go
func (w *KustomizeWriter) WriteStage(artifacts []cranelib.TransformArtifact, force bool, stats *runstats.RunStats) error {
    // ... existing resource + patch + kustomization writing ...

    // Auditability: write reports that were scaffolded but never populated
    audit.WriteStageMetadata(w.opts, w.stageName, stats)
    audit.WriteResourceInventory(w.opts, w.stageName, artifacts)
    audit.WriteWhiteoutReport(w.opts, w.stageName, artifacts)
    audit.WriteIgnoredPatchesReport(w.opts, w.stageName, artifacts)

    return nil
}
```

Four function calls added to the end of an existing method. Each writes one JSON file
using paths that `PathOpts` already defines. Minimal risk.

---

## 7. Logging Pipeline (How Noise Reduction + Persistent Capture Coexist)

```
                  ┌──────────────────────────┐
                  │   Application Code       │
                  │   log.Infof("milestone") │
                  │   log.Debugf("detail")   │
                  └──────────┬───────────────┘
                             │
                    logrus.Logger
                   (level filter)
                             │
              ┌──────────────┴──────────────┐
              │                             │
     ┌────────▼────────┐          ┌─────────▼─────────┐
     │ Console Handler │          │  Audit File Hook   │
     │ (stderr)        │          │ (.crane-audit.log) │
     │                 │          │                    │
     │ Level: Info+    │          │ Level: ALL         │
     │ Format: text    │          │ Format: JSON Lines │
     │ (human-readable)│          │ (machine-readable) │
     └─────────────────┘          └────────────────────┘

     Observability wins:           Auditability wins:
     quiet, scannable console      nothing lost, fully replayable
```

The file hook is always active and captures at ALL levels regardless of the console
level setting. This means:
- Default run: console shows Info+, file captures Info+
- Debug run: console shows Debug+, file captures Debug+
- The file hook captures Debug even when console is at Info, giving support engineers
  full detail without the user needing `--debug`

---

## 8. Migration Path for Existing Code

Neither feature requires a flag day. Changes can be additive:

| Existing Pattern | Change | Risk |
|---|---|---|
| `log.Infof("adding resource: %s ...")` in discover.go | Change to `log.Debugf` | None: the info was noise |
| `log.Infof("Writing objects of resource: %s ...")` in discover.go | Change to `log.Debugf` | None: same |
| `o.globalFlags.GetLogger()` in every command | Returns enhanced logger with always-on audit hook | None: transparent |
| `writer.WriteStage(artifacts, force)` | Add `stats` parameter, write audit files at end | Low: additive output |
| `runner.Run()` in crane-lib | Unchanged until Wave 5 (`RunDetailed` is a new method) | None |
| `PluginResponse` in crane-lib | Add `WhiteOutReason` with `omitempty` | None: backward compatible |

---

## 9. What Ships When (PR Checklist)

### PR 1: Foundation (Wave 1)
```
internal/runstats/stats.go          NEW
internal/runstats/phase_record.go   NEW
internal/cli/phase.go               NEW
internal/cli/summary.go             NEW  (unified summary: counters + audit artifact paths)
internal/cli/summary_markdown.go    NEW  (optional --summary-file writer)
internal/cli/banner.go              NEW
internal/audit/audit_logger.go      NEW  (always-on file hook)
internal/flags/global_flags.go      MODIFIED (add --audit-log, --summary-file, enhance GetLogger)
cmd/export/discover.go              MODIFIED (Info -> Debug for per-type lines)
```
Tests: unit tests for RunStats, PhaseTracker, FileHook; existing tests still pass.

### PR 2: Export (Wave 2)
```
cmd/export/export.go                MODIFIED (phases, stats, summary, audit writes)
cmd/export/discover.go              MODIFIED (return discovery counts)
internal/audit/run_manifest.go      NEW
internal/audit/export_inventory.go  NEW
```
Tests: export e2e tests verify `.crane-run.json` and inventory exist and have valid JSON.

### PR 3: Transform Audit Files (Wave 3a)
```
internal/transform/orchestrator.go  MODIFIED (fix IgnoredOps TODO, collect stats)
internal/transform/writer.go        MODIFIED (write audit files at end of WriteStage)
internal/audit/stage_metadata.go    NEW
internal/audit/resource_inventory.go NEW
internal/audit/whiteout_report.go   NEW
internal/audit/ignored_patches_report.go NEW
```
Tests: transform e2e tests verify all 4 audit files; unit test for IgnoredOps parsing.

### PR 4: Transform + Apply UX (Wave 3b + Wave 4)
```
cmd/transform/transform.go         MODIFIED (banner, progress, summary)
cmd/apply/apply.go                  MODIFIED (banner, phases, summary)
internal/apply/kustomize.go         MODIFIED (collect ApplyStats)
internal/audit/run_manifest.go      MODIFIED (apply variant)
```
Tests: manual smoke test for console output; e2e for apply `.crane-run.json`.

### PR 5: Deep Plugin Audit (Wave 5, crane-lib PR + crane PR)
```
crane-lib/transform/runner.go      MODIFIED (add RunDetailed)
crane-lib/transform/plugin.go      MODIFIED (add WhiteOutReason)
crane-lib/transform/kubernetes/     MODIFIED (set WhiteOutReason)
internal/audit/plugin_audit.go     NEW
internal/transform/orchestrator.go MODIFIED (use RunDetailed, write plugin audit)
internal/audit/whiteout_report.go  MODIFIED (include reasons)
```

### PR 6: Transfer-PVC (Wave 6)
```
cmd/transfer-pvc/transfer-pvc.go   MODIFIED (phases, --debug wiring, summary)
```

---

## 10. Recommendation

**Build them together, ship them in waves.**

The alternative — building observability first, then retrofitting auditability — would mean:
1. Writing counter logic in Wave 1-4 for console summaries
2. Rewriting that same counter logic in a later auditability effort to also produce JSON
3. Two sets of tests for the same data
4. Two PRs touching the same lines in `export.go`, `orchestrator.go`, `writer.go`

By designing `RunStats` as the shared backbone from day one, every counter collected for
a `[2/5] ... 38 API types` console line is automatically available for the
`apiResourcesAdmitted: 38` field in the audit JSON. No duplication, no rework.

**Key design decisions:**
- **Auditability is always on.** No `--audit` flag. Every run writes `.crane-run.json`,
  `.crane-audit.log`, and all report files. This is lightweight and ensures audit data
  is never missing when you need it.
- **One summary, not two.** `internal/cli/summary.go` prints one end-of-command block
  that includes both operational counters (observability) and audit artifact paths
  (auditability). There is no separate `internal/audit/summary.go`.
- **`audit/` package writes JSON only.** All human-facing console output lives in `cli/`.
  Clean boundary: `cli/` owns the terminal, `audit/` owns the files.

The schedule above delivers **visible UX improvements early** (Wave 1-2 ship in weeks 1-3)
while **building audit infrastructure in parallel** (audit files start appearing in Wave 2).
The expensive crane-lib changes for deep plugin audit are deferred to Wave 5, after the
foundation is battle-tested.
