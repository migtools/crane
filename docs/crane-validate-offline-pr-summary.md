# PR: Add offline validation via `--api-resources` flag

**Issue:** https://github.com/migtools/crane/issues/319

## Summary

Adds offline GVK validation to `crane validate` by accepting the JSON output of `kubectl api-resources -o json` via a new `--api-resources` flag. This enables validation against a target cluster's API surface without a live kubeconfig connection.

## Problem

`crane validate` required a live connection to the target cluster. This blocked validation in air-gapped environments, CI/CD pipelines without cluster access, and team handoff scenarios where the migration team doesn't have kubeconfig access to the target.

## Solution

One new flag, no new commands, no new file formats:

```bash
# On target cluster (no crane needed):
kubectl api-resources -o json > api-resources.json

# On migration workstation:
crane validate -i output/ --api-resources api-resources.json
```

The `kubectl api-resources -o json` output is a native Kubernetes `APIResourceList` — the same type the Go discovery client returns internally. The parser is a straightforward `json.Unmarshal` with zero custom parsing logic.

`--api-resources` is mutually exclusive with `--context`/`--kubeconfig`. When neither is set, existing default kubeconfig behavior is unchanged.

## Changes

| File | Change |
|------|--------|
| `internal/validate/matcher.go` | Export `DiscoveryIndex` type, extract `MatchResultsFromIndex` from `MatchResults` |
| `internal/validate/api_resources.go` | **New:** `ParseAPIResourcesJSON` — parses kubectl JSON output into `DiscoveryIndex` |
| `internal/validate/types.go` | Add `Mode`, `APIResourcesSource`, `ClusterContext` fields to `ValidationReport` |
| `internal/validate/report.go` | Show mode/source line in table output |
| `cmd/validate/validate.go` | Add `--api-resources` flag, mutual exclusion logic, offline branch in `Run()` |
| `internal/validate/api_resources_test.go` | **New:** 9 parser unit tests |
| `internal/validate/matcher_test.go` | 3 new `MatchResultsFromIndex` tests |
| `cmd/validate/validate_test.go` | 4 new tests (flag registration, mutual exclusion, file validation) |

## Design decisions

- **JSON over tabular text:** `kubectl api-resources -o json` gives structured data with separate `group`/`version` fields. No column alignment ambiguity, no empty-field gaps, no custom parsing heuristics.
- **No intermediate format:** The kubectl JSON output is used directly — no ClusterProfile conversion step needed.
- **Refactor, not rewrite:** `MatchResults` now delegates to `MatchResultsFromIndex`. Both live and offline paths produce the same `DiscoveryIndex` type, so all matching logic (`matchEntry`, `addSuggestion`, `buildKindIndex`) is untouched.

## Test plan

- [x] `go build ./...` compiles
- [x] All 43 unit tests pass (`go test ./internal/validate/... ./cmd/validate/...`)
- [x] Existing live-mode tests unchanged and passing
- [ ] Manual: `kubectl api-resources -o json > /tmp/api-resources.json && crane validate -i output/ --api-resources /tmp/api-resources.json`
- [ ] Manual: `crane validate -i output/ --api-resources /tmp/api-resources.json --context foo` returns mutual exclusion error
- [ ] Manual: existing `crane validate -i output/ --context <cluster>` still works
