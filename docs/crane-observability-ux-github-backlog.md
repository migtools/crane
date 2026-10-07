# Crane observability (UX-first) — story headline and GitHub issue backlog

Use this document to open an **Epic** (or parent tracking issue) and **child issues** on your GitHub project. **Each issue includes example terminal output** (target transcript or before/after) so scope is obvious when copied into GitHub.

Scope here is **terminal UX only**, **Approach A**: phased banners, aggregate or throttled progress where specified below, end-of-run summaries, and quieter default logs—not metrics vendors or OpenTelemetry. **Approach B** items (TTY single-line redraw, spinners, heartbeat-only mode, transfer-pvc log-format variant) are **out of scope** here; see [Optional follow-up](#optional-follow-up-separate-epic-or-low-priority).

**Related engineering conventions:** [logging-standards.md](./logging-standards.md) (stdout/stderr, log levels, `GlobalFlags`).

---

## Story headline (Epic title)

**As an operator running Crane on large namespaces or long pipelines, I always see where the tool is in its work, how much is left, and a clear summary when it finishes—so I trust the run is progressing and I can act on partial failures without parsing hundreds of log lines.**

Shorter option for the Epic title field:

> **Clear phased progress and end-of-run summaries for all long-running Crane commands**

---

## Recommended issue order

1. Foundation (shared patterns + export noise) before deep per-command polish.
2. **export** → **transform** → **apply** (same high-level UX shape: banner, phases, summary).
3. **transfer-pvc** last (special stdout/rsync + library logs).

You can split foundation into one or two issues depending on team capacity.

---

## Issue 1 — Foundation: shared CLI progress/summary patterns and export log noise

### Title

`feat(cli): shared helpers for phased UX + move export per-type noise to --debug`

### Output: what changes (at a glance)

**Today (problem)** — default `crane export` can look like an endless stream of:

```text
time="..." level=info msg="adding resource: configmaps to the list of GVRs to be extracted"
time="..." level=info msg="adding resource: secrets to the list of GVRs to be extracted"
... (one line per API type) ...
time="..." level=info msg="Writing objects of resource: configmaps to the output directory"
... (again per type) ...
```

**After this issue** — same run at default verbosity: **no** per-type spam; those lines appear only with `--debug`. Shared helpers should support a consistent shape for later issues, for example:

```text
Summary
-------
duration: 1m04s
```

(Phased banners for export land mainly in Issue 2; Issue 1 delivers **quiet default + reusable summary/phase primitives**.)

### Description

**Context**

Observability work will repeat the same patterns: an opening banner (command, key paths), numbered phases, throttled progress where called for (e.g. transform), and a closing **Summary** block (counts, duration, pointers to `failures/`). Today, `crane export` emits many `info` lines per API type (“adding resource”, “Writing objects”) that overwhelm users on busy clusters. [logging-standards.md](./logging-standards.md) asks to keep **Info** for milestones and move high-volume lines to **Debug**.

**User outcome**

- Future command issues can reuse one approach for banners/summaries.
- `crane export` default output is readable; detail remains available with `--debug`.

**Scope**

- Introduce or extend small internal helpers (package under `internal/` or next to existing CLI UI patterns) to print:
  - Consistent phase lines, e.g. `[i/N] Phase name …` to **stderr** (via `IOStreams.ErrOut` / logger—match [logging-standards.md](./logging-standards.md)).
  - A consistent **Summary** section (dashes or similar) with duration.
- Refactor **export** so per-resource-type enumeration and “writing objects per type” lines are **Debug** unless a single aggregate line per phase is kept at Info (product choice: prefer one aggregate + debug detail).
- Do **not** add NDJSON or `--progress=json` in this issue unless trivial; stay UX-first.

**Acceptance criteria**

- [ ] Documented pattern (short comment or doc pointer) for how commands should print phases + summary.
- [ ] `crane export` at default verbosity no longer spams one line per type for routine discovery/write loops (detail under `--debug`).
- [ ] Stdout remains unused for unstructured status (except existing legacy behavior elsewhere).
- [ ] `go test ./...` passes.

**Technical notes**

- Wire through `GlobalFlags` / `GetLogger()` per [logging-standards.md](./logging-standards.md).
- Avoid interleaved corrupted output if combining logger and raw writes; serialize or use one channel.

**References**

- `cmd/export/`, `internal/flags/global_flags.go`, [logging-standards.md](./logging-standards.md).

---

## Issue 2 — `crane export`: phased ladder + end summary (Approach A)

### Title

`feat(export): phased progress banner and summary for namespace export`

### Target output (example)

What users should see (stderr / logrus; illustrative):

```text
$ crane export -e ./out --namespace shop

crane export
namespace: shop
export-dir: ./out

[1/5] Discovery … ok (preferred API lists refreshed)
[2/5] Listing namespace resources … 38 API types with ≥1 object
      (use --debug to list each type)
[3/5] Related cluster-scoped RBAC/SCC … 4 resources → resources/shop/_cluster/
[4/5] Related CRDs … 1 fetched, 0 skipped
[5/5] Writing YAML … 412 files written

Summary
-------
resources:  ./out/resources/shop/  (+ _cluster/)
failures:   ./out/failures/shop/     (3 list/write issues recorded)
duration:   1m 04s
Done.
```

### Description

**Context**

Users running `crane export` see no opening line, no clear sense of progress, and duplicated-looking waves of messages (“adding resource” then “Writing objects”). Failures appear as warnings without a final rollup. The target UX is **Approach A**: numbered phases, aggregate counts, and a **Summary** with paths, failure counts, and duration.

**User outcome**

After this issue, a typical run shows:

- First lines: `crane export`, namespace, export-dir.
- Phases such as: Discovery → Listing → Related cluster RBAC/SCC → Related CRDs → Writing YAML (wording can match implementation).
- Final **Summary**: resource root path (`resources/<ns>/`, `_cluster/` note), `failures/` path if used, count of recorded issues, **duration**, clear “Done.”

**Scope**

- Implement phased output using foundation from Issue 1 (or inline if Issue 1 is split).
- Phase lines include **aggregate numbers** (e.g. “38 API types with ≥1 object”) instead of listing each type at Info.
- Mid-run warnings remain visible or roll into summary; **Summary must state** how many list/write issues were recorded when `failures/` is populated.

**Acceptance criteria**

- [ ] Opening banner: command name, namespace(s), export directory.
- [ ] Numbered phases with clear completion (ok / skipped counts where applicable).
- [ ] Closing Summary: key paths, failure/issue count, duration.
- [ ] With `--debug`, retain or improve detailed per-type logs for support.
- [ ] Manual smoke: small and large namespace; CI/non-TTY readable (no reliance on ANSI or TTY-only features for core flow).

**References**

- `cmd/export/export.go`, `cmd/export/discover.go`, `cmd/export/cluster.go`.

---

## Issue 3 — `crane transform`: banner, throttled file progress, summary (Approach A)

### Title

`feat(transform): start banner, file progress, and end summary`

### Target output (example)

```text
$ crane transform -e ./export -t ./transform -p ~/.crane/plugins

crane transform
export-dir:    /path/export
transform-dir: /path/transform
plugins:       8 loaded (0 skipped)
files:         520 YAML documents to process

[  52/520]  10%  apps_v1_deployment_shop_web.yaml
[ 104/520]  20%  route_openshift_io_v1_route_shop_api.yaml
...
[ 520/520] 100%  v1_configmap_shop_env.yaml

Summary
-------
transform files written: 518
whiteouts created:       2
ignored patches:       1 file (see --ignored-patches-dir)
duration:              2m 11s
Done.
```

### Description

**Context**

`crane transform` can sit silent for a long time on hundreds of files; users cannot tell if it is hung. Plugin messages are sparse. Target **Approach A** UX: banner with paths, plugin count, total files/docs to process; progress every **N** files or percent; **Summary** with outputs written, whiteouts, ignored patches pointer, duration.

**User outcome**

- Immediate confirmation after start with **file count** and **plugin count** (and skipped plugins if applicable).
- Periodic lines like `[ 104/520] 20% <filename>` at Info, **or** configurable N (implementation choice).
- Summary: transform outputs count, whiteouts created, ignored patches location/count, duration.

**Scope**

- Implement in `cmd/transform` + `internal/transform` + crane-lib hooks if counters must come from library (coordinate with crane-lib if needed).
- Per-file **Info** for every file is **out** unless `--debug`/`--verbose`—match logging standards (avoid spam).

**Acceptance criteria**

- [ ] Banner: export-dir, transform-dir, plugins loaded count, documents/files to process.
- [ ] Progress: throttled (every N or %) with current file name at Info.
- [ ] Summary: written count, whiteouts, ignored patches, duration.
- [ ] `--debug` remains usable for deep plugin/file detail without doubling Info noise.

**References**

- `cmd/transform/transform.go`, `internal/transform/orchestrator.go`, crane-lib `transform.Runner`.

---

## Issue 4 — `crane apply`: banner, phased steps, and summary (Approach A)

### Title

`feat(apply): phased banner and summary aligned with kubectl kustomize apply path`

### Behavior: two modes, one engine (always kustomize)

In the current codebase there is **no** `crane apply` path that skips **`kubectl kustomize`**. `kubectl` must be available; see [cmd/apply/apply.go](cmd/apply/apply.go) (`ValidateKubectlAvailable`) and [internal/apply/kustomize.go](internal/apply/kustomize.go) (`runKustomizeBuild`).

The **two** ways to run apply are:

| Mode | When | What happens |
|------|------|----------------|
| **Final stage only** (default) | No `--stage` / `--from-stage` / `--to-stage` / `--stages` | Discover stages under `transform-dir`, pick the **last** stage, run `kubectl kustomize` on that stage directory, write `output.yaml`, then split multi-doc YAML into per-resource files. |
| **Multi-stage** | User passes stage selector flags | For **each** selected stage, run `kubectl kustomize` on that stage’s directory and write `<stageName>.yaml` (split behavior per implementation). |

So observability for Issue 4 must read clearly in **both** modes; phases are “per stage” when multi-stage, or “once for final stage” by default.

### Target output (example)

**A) Default — final stage only** (illustrative):

```text
$ crane apply -t ./transform -o ./output

crane apply
transform-dir: /path/transform
output-dir:    /path/output
mode:          final stage only → 30_LastPlugin  (example name)

[1/3] kubectl kustomize (stage 30_LastPlugin) … ok
[2/3] Writing bundle … /path/output/output.yaml
[3/3] Splitting multi-doc YAML into resource files … 42 resources

Summary
-------
output:          /path/output/output.yaml
resources dir:   /path/output/resources/  (per-namespace + _cluster as needed)
resources written: 42
duration:        48s
Done.
```

**B) Multi-stage** — e.g. `--stages` or `--from-stage` / `--to-stage` (illustrative; wording should match code):

```text
$ crane apply -t ./transform -o ./output --stages 10_Foo,20_Bar

crane apply
transform-dir: /path/transform
output-dir:    /path/output
mode:          multi-stage (2 stage(s))

--- stage 10_Foo ---
[1/2] kubectl kustomize … ok
[2/2] Writing … /path/output/10_Foo.yaml

--- stage 20_Bar ---
[1/2] kubectl kustomize … ok
[2/2] Writing … /path/output/20_Bar.yaml

Summary
-------
stage bundles:   10_Foo.yaml, 20_Bar.yaml
duration:        1m 02s
Done.
```

**Implementation note:** Today [internal/apply/kustomize.go](internal/apply/kustomize.go) runs **`splitMultiDocYAMLToFiles` only for `ApplyFinalStage`** (default path). **`ApplyMultiStage` writes one YAML per stage only**—no `resources/` split. Phase text for Issue 4 should reflect that difference so users are not promised a split step in multi-stage mode unless the code is extended later.

### Description

**Context**

`crane apply` is very quiet on success; users think the process froze. The implementation **always** runs **`kubectl kustomize`** on one or more stage directories under `transform-dir`, then writes YAML output (and for the default **final-stage** path, splits multi-doc YAML into per-resource files). It does **not** iterate merge operations per export file like `crane transform`. **Approach A** here means: opening banner, **phased steps** that match that pipeline (kustomize → write → split when applicable), and a closing **Summary** with paths, resource/document counts, and duration—for **both** default and multi-stage entry points.

**User outcome**

- Clear phases so the run does not look hung.
- Summary lists output location(s), approximate **resource count** (e.g. documents written in split step), and duration.

**Scope**

- Implement in `cmd/apply/apply.go` and [internal/apply/kustomize.go](internal/apply/kustomize.go).
- **Out of scope for this issue:** per-resource labels like “merged vs export-only” for each file (would require different instrumentation or pipeline)—optional future enhancement.

**Acceptance criteria**

- [ ] Banner: transform-dir, output-dir, stage selection (final vs multi-stage) as applicable.
- [ ] Phase lines for: kustomize build (and per stage if multi-stage), write consolidated output, split to per-resource files if applicable.
- [ ] Summary: key output paths, resource/file count, duration.
- [ ] Default verbosity: no per-document **Info** spam; details in `--debug` where appropriate.

**References**

- `cmd/apply/apply.go`, `internal/apply/kustomize.go`.

---

## Issue 5 — `crane transfer-pvc`: Crane-framed phases + aligned cleanup summary (Approach A)

### Title

`feat(transfer-pvc): numbered phases and final summary around rsync progress`

### Target output (example)

Phases are plain lines; rsync block stays as today (live-updating region):

```text
$ crane transfer-pvc --source-context src --destination-context dst \
    --pvc-name data:data --pvc-namespace app:app ...

crane transfer-pvc
source context:      src
destination context: dst
PVC:                 app/data → app/data
endpoint:            nginx-ingress  (subdomain: transfers.example.com)

[1/7] Reading source PVC … ok
[2/7] Creating destination PVC … ok
[3/7] Creating endpoint (Ingress) … ok
[4/7] Waiting for endpoint healthy … ok
[5/7] Starting stunnel + rsync server on destination … ok
[6/7] Copying data (rsync) …

Status: Transfer in-progress
Progress:
  Percentage:  45%
  ...

[6/7] Copying data … finished  exit=0
[7/7] Cleaning up temporary pods/routes/secrets … ok

Summary
-------
PVC data copy: succeeded
duration:      3m 02s
Done.
```

### Description

**Context**

`transfer-pvc` mixes JSON logs, plain `log` lines, and a live rsync block; users cannot map output to **which phase** (ingress, endpoint ready, rsync, cleanup). Target **Approach A**: Crane prints a phase ladder; rsync live block stays; cleanup gets a visible completion and **Summary** (success, duration; optional progress file path if `--output` exists).

**User outcome**

- Clear `[i/N]` phases for: source PVC read, dest PVC, endpoint/ingress, health wait, rsync server, **copy (rsync UI)**, cleanup.
- Final Summary: copy result, duration, pointers to artifacts if any.

**Scope**

- Add phase banners around major steps in `cmd/transfer-pvc`.
- Ensure final exit path prints Summary (avoid silent success after rsync).
- **Out of scope for this issue:** full log-format unification (see optional follow-ups); wiring `crane --debug` everywhere (Issue 6).

**Acceptance criteria**

- [ ] User sees phase list and knows current stage without reading library JSON.
- [ ] Rsync in-place progress preserved for the data copy phase.
- [ ] Summary on success and on controlled failure paths where feasible.
- [ ] Document any remaining raw library logs as follow-up.

**References**

- `cmd/transfer-pvc/transfer-pvc.go`, `cmd/transfer-pvc/progress.go`, [logging-standards.md](./logging-standards.md) (legacy stdout exception).

---

## Issue 6 — `crane transfer-pvc`: honor global `--debug` for pvc-transfer verbosity

### Title

`fix(transfer-pvc): wire GlobalFlags / --debug to pvc-transfer and library log verbosity`

### Output: what changes (at a glance)

**Goal:** One knob for support—compare default vs debug:

```text
# Default: less noise from embedded libraries; phases still visible (Issue 5)
$ crane transfer-pvc ...

# Deep diagnostics
$ crane --debug transfer-pvc ...
```

**Done when:** `--debug` clearly increases log detail (e.g. more `debug` lines from transfer + libraries) without requiring a separate undocumented env var.

### Description

**Context**

Root `crane --debug` should control verbosity for subcommands consistently. Today transfer-pvc may not fully propagate global debug to embedded library behavior.

**User outcome**

Support and users can get deep logs with one familiar flag.

**Scope**

- Wire `GlobalFlags` where missing; map `--debug` to library/logger levels for transfer-pvc and dependencies.
- Document in command help if any exceptions remain.

**Acceptance criteria**

- [ ] `crane --debug transfer-pvc ...` increases diagnostic detail compared to default.
- [ ] Help text mentions debug behavior.

**References**

- `cmd/transfer-pvc/transfer-pvc.go`, `internal/flags/global_flags.go`.

---

## Optional follow-up (separate Epic or low priority)

Items below were **Approach B** or adjacent UX in the original roadmap; track separately if needed.

| Title | Notes |
| ----- | ----- |
| `feat(export): optional single-line TTY progress during listing/write` | Carriage-return / ANSI progress bar; non-TTY unchanged. |
| `feat(transform): optional TTY spinner with current file` | Animated single line; non-TTY uses throttled lines only. |
| `feat(apply): optional heartbeat progress for huge manifests` | “Still working … elapsed” on an interval instead of many lines. |
| `feat(transfer-pvc): consistent log format on TTY vs JSON opt-in` | Human-readable default on TTY; structured JSON when explicit or CI. |
| `feat(transform): plugin-centric timing rollup` | Per-plugin stats and duration (Approach C)—requires crane-lib aggregation. |
| `feat(cli): opt-in machine-readable progress (NDJSON)` | After UX baseline; see [logging-standards.md](./logging-standards.md). |

---

## Labels suggestion (for your repo)

- `area/ux` or `enhancement`
- `command/export`, `command/transform`, `command/apply`, `command/transfer-pvc`
- `priority` / `epic` as you prefer

---

## Epic body (paste into GitHub Epic or parent issue)

**Problem:** Long-running Crane commands often produce silence, log spam, or mixed formats. Operators cannot tell progress or final state without parsing logs.

**Goal:** UX-first observability (**Approach A**): phased banners, aggregate or throttled progress where appropriate, and consistent end summaries for export, transform, apply, and transfer-pvc—per team spec and [logging-standards.md](./logging-standards.md). TTY-only spinners, single-line redraw progress, heartbeat-only apply, and transfer-pvc log-format unification are **not** part of this Epic; track as optional follow-ups.

**Child issues:** Link **Issues 1–6** from this document (plus optional follow-ups if scheduled).
