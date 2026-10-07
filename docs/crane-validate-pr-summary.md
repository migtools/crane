## Summary

Adds `crane validate` — a pre-apply compatibility check that verifies exported manifests against a target cluster's API surface. Catches incompatible apiVersions early (e.g., `extensions/v1beta1` on a cluster that only serves `apps/v1`) and suggests alternatives when available.

**New step in the pipeline: export → transform → validate → apply**

### What it does

- Scans `--export-dir` for YAML/JSON manifests, deduplicates by GVK+namespace
- Queries target cluster discovery (`ServerGroupsAndResources`) for strict GVK matching
- Reports results as table (default) or JSON (`-o json`)
- Suggests alternative GroupVersions when available (e.g., "available as apps/v1")
- Writes `report.json` and per-resource `failures/*.yaml` to `--validate-dir`
- Exits non-zero on incompatibility (CI/CD friendly)

### Files changed

| File | |
|------|--|
| `main.go` | +2 lines: register validate subcommand |
| `cmd/validate/validate.go` | Cobra command: flags, Complete/Validate/Run |
| `cmd/validate/validate_test.go` | Flag registration and validation tests |
| `internal/validate/types.go` | Data types: ManifestEntry, ValidationResult, ValidationReport |
| `internal/validate/scanner.go` | Recursive dir walker + multi-doc YAML parser + dedup |
| `internal/validate/scanner_test.go` | 10 tests |
| `internal/validate/matcher.go` | Discovery index, GVK matching, suggestion engine |
| `internal/validate/matcher_test.go` | 8 tests |
| `internal/validate/report.go` | Table/JSON formatters + WriteFailures to disk |
| `internal/validate/report_test.go` | 4 tests |

**10 files, ~1,336 insertions, 0 deletions. No existing behavior changed. No new dependencies.**

### Demo

```
$ crane validate -e export/ --context target-cluster

Scanned 3 distinct GVK+namespace tuples
APIVERSION              KIND        NAMESPACE  RESOURCE     STATUS        REASON                                                                                  SUGGESTION
apps/v1                 Deployment  prod       deployments  OK
extensions/v1beta1      Deployment  prod                    Incompatible  API version extensions/v1beta1 not available on target cluster (available as apps/v1)   available as apps/v1
route.openshift.io/v1   Route       prod                    Incompatible  API version route.openshift.io/v1 not available on target cluster

Summary: 3 scanned, 1 compatible, 2 incompatible
Wrote validation report to validate/report.json
Wrote 2 validation failure(s) to validate/failures
Error: validation failed: one or more resources are incompatible with the target cluster
```

Output on disk:
```
validate/
├── report.json
└── failures/
    ├── Deployment_extensions_v1beta1_prod.yaml
    └── Route_route.openshift.io_v1_prod.yaml
```

## Test plan

- [x] `go build ./...`
- [x] `go test ./cmd/validate/ ./internal/validate/...` — 27 unit tests pass
- [x] Verified on minikube: compatible resources → exit 0; `extensions/v1beta1` + `route.openshift.io/v1` → exit 1 with suggestions
- [x] Error paths produce clean output (no Cobra help dump)
- [x] Validate output goes to `--validate-dir`, export dir untouched
