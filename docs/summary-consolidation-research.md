# Research: Summary Consolidation — Observability + Auditability

**Date:** 2026-04-20
**Status:** Resolved. Both the unified plan and the auditability plan have been updated.

---

## Decision

Observability and auditability summaries are **combined into one**. Auditability is
**always on** -- there is no `--audit` flag. Every run writes audit artifacts and the
end-of-command summary always includes audit artifact paths.

## What Changed

1. **No `--audit` flag.** All audit artifacts (`.crane-run.json`, `.crane-audit.log`,
   reports, inventories) are written on every run. No opt-in needed.

2. **No `--summary` flag.** Summaries are always printed. `--summary-file` remains
   as an optional way to write the summary to Markdown.

3. **One summary function.** `internal/cli/summary.go` handles everything. There is
   no `internal/audit/summary.go`. The `audit/` package writes JSON files only.

4. **One summary block** that shows both operational counters and audit artifact paths:

```
Summary
-------
namespace:   my-app
context:     eks-prod
resources:   47 exported, 2 failed, 84 skipped
cluster:     2 CRBs, 1 CR, 3 CRDs
failures:    ConfigMap (Forbidden), Secret (Forbidden)
duration:    1m 04s

run manifest:  export/.crane-run.json
audit log:     .crane-audit.log
Done.
```

## Updated Documents

- `docs/forensic-postmortem-auditability-plan.md` -- removed `--audit`/`--summary` flags,
  updated Section 4.8 (always-on log), Section 4.9 (unified summary), removed Section 4.10
  (`--audit` meta-flag), updated flag table and package structure.

- `docs/observability-auditability-unified-plan.md` -- removed `--audit` flag from all
  sections, updated `GetLogger()` to always attach file hook, merged summary into
  `cli/summary.go`, updated PR checklists and wave schedule.
