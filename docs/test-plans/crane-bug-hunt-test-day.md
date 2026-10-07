# Crane Feature-by-Feature Bug-Hunt Test Catalog

## Objective

Test every user-facing part of Crane independently and in combination. Each feature below has multiple happy-path, negative, permission, recovery, scale, and customer-realistic cases.

We will execute one case at a time. After each case, record `PASS`, `FAIL`, `BLOCKED`, or `INCONCLUSIVE`, preserve evidence, and describe any potential bug before continuing.

Priorities:

- **P0:** Data safety, false success, security, or a primary workflow.
- **P1:** Important edge, retry, permissions, compatibility, or scale case.
- **P2:** Secondary command, diagnostics, or uncommon input.

## Common environment and evidence

- Two OpenShift clusters with namespace-admin contexts `src_cluster` and `tgt_cluster`.
- Admin contexts only for fixture setup or explicitly administrative checks.
- A fresh namespace per case: `crane-<case-id-lowercase>`.
- Two StorageClasses on at least one cluster.
- S3-compatible storage for indirect migration.
- Optional NFS/CephFS class for network-filesystem tests.
- Exact Crane commit and version recorded for the run.

For every case capture the command, stdout, stderr, exit code, source/target resources and events, generated directory trees/YAML, data proof, remaining helper resources, and the result of one identical rerun.

For every data-migration case, define the application data contract before transfer. Classify paths as application-critical, derived/recoverable, or ephemeral. After transfer, verify pod readiness, run a real application query or API operation, perform a safe target write/read when possible, replace or restart the target pod, and repeat the critical checks. Investigate each failed path before judging impact: a missing cache, lock, socket, log, or temporary file can be acceptable when the application regenerates it and all critical checks pass.

`transferPercentage` represents byte progress, not the migration verdict. Evaluate the final status and exit code, inspect failed paths, and then decide success from the declared data contract and target application behavior.

---

# 1. Global CLI, configuration, and contexts

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| CLI-01 | P0 | Explicit flags versus equivalent `--flags-file` | Same behavior; find Viper/Cobra mapping defects |
| CLI-02 | P0 | Conflicting flags-file and CLI values | CLI wins consistently; find precedence bugs |
| CLI-03 | P0 | Merged kubeconfig while current context points elsewhere | Explicit context always selects intended cluster |
| CLI-04 | P0 | Run supported flows as namespace-admin | No hidden admin-context or cluster-admin fallback |
| CLI-05 | P1 | Missing, malformed, expired, and unreadable kubeconfig | Fast error names path/context; no panic or hang |
| CLI-06 | P1 | Context has no default namespace | Namespace is required/resolved clearly, never empty silently |
| CLI-07 | P1 | Paths with spaces, Unicode, long names, and relative components | Correct safe path handling |
| CLI-08 | P1 | Interrupt commands with `Ctrl-C` | Prompt cancellation and safe partial state |
| CLI-09 | P1 | Unreachable API and short request timeout | Bounded failure identifies server/context |
| CLI-10 | P2 | Invalid enums, missing required flags, extra arguments | Nonzero concise error; help matches behavior |

---

# 2. `crane export`

## Namespace, permissions, and output

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| EXP-01 | P0 | Ordinary app export as namespace-admin | All readable required namespaced resources exported |
| EXP-02 | P0 | Nonexistent namespace | Nonzero namespace-specific NotFound, not empty success |
| EXP-03 | P1 | Empty namespace | Valid understandable empty result; no malformed output |
| EXP-04 | P0 | Forbidden from one resource type but allowed others | Partial failure explicit; no silent incomplete export |
| EXP-05 | P1 | Existing output with/without `--overwrite` | Safe refusal or full clean replacement; no stale files |
| EXP-06 | P1 | Output path is a file or unwritable directory | Actionable filesystem error; no partial success |
| EXP-07 | P1 | Interrupt export, then rerun same directory | Partial export not treated as complete; safe recovery |

## Filters and dependency completeness

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| EXP-08 | P0 | Labelled Deployment references unlabelled Secret, ConfigMap, PVC, and SA | Dependencies included or omission explicit; find undeployable filtered exports |
| EXP-09 | P0 | Service selector matches pods but Service labels differ | Networking resource behavior predictable; no silent Service loss |
| EXP-10 | P1 | Equality, set, `notin`, compound, and invalid selectors | Correct selection; invalid selector fails early |
| EXP-11 | P1 | Selector matches nothing | Clear empty result; no unrelated cluster resources |
| EXP-12 | P0 | `--include-gk` with core and named API groups | Exact kinds only; find core-group parsing bugs |
| EXP-13 | P0 | `--exclude-gk` removes a referenced kind | Exact exclusion; no overbroad filtering |
| EXP-14 | P1 | Same GroupKind across API versions | Consistent documented matching semantics |
| EXP-15 | P1 | Label selector plus include/exclude GroupKind | Deterministic precedence/intersection |

## Discovery, relationships, serialization, and scale

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| EXP-16 | P0 | SA → RoleBinding → ClusterRoleBinding graph | Relevant RBAC exported; unrelated cluster RBAC excluded |
| EXP-17 | P1 | CRB subjects as SA, User-form SA, Group, and other namespace | Correct subject relevance matching |
| EXP-18 | P0 | Namespaced custom resource and its CRD | CR plus relevant readable CRD; no CR without schema |
| EXP-19 | P1 | OLM-managed CRD and include/skip group flags | Deterministic skip/include and clear warnings |
| EXP-20 | P1 | Deployment, DC, StatefulSet, DaemonSet, Job, CronJob, HPA, PDB, NetworkPolicy | Every supported kind serializes correctly |
| EXP-21 | P1 | Opaque, TLS, dockerconfigjson, and binary Secrets | Secret data preserved byte-for-byte |
| EXP-22 | P1 | Finalizers, deletionTimestamp, ownerReferences, managedFields | Complete export; no serialization failure |
| EXP-23 | P1 | Long/dotted names that normalize similarly | Stable unique filenames; no overwrite collision |
| EXP-24 | P0 | More than one list page, e.g. 1,200 ConfigMaps | Exact source/export count; find missing pagination |
| EXP-25 | P1 | Repeat large export into two directories | Normalized output identical; find nondeterminism |
| EXP-26 | P1 | Very low QPS/burst and intermittent API errors | No resource loss; throttling/failures visible |
| EXP-27 | P1 | Broken/unavailable aggregated APIService | Exact API named; unrelated export handled safely |

---

# 3. `crane transform` core pipeline

## Plugin discovery and stages

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| TRN-01 | P0 | Default Kubernetes + installed OpenShift plugins | Correct stable stage order and patches |
| TRN-02 | P0 | Select stage by plugin name and directory name | Same intended stage resolved |
| TRN-03 | P0 | Unknown or misspelled plugin | Fail closed; never silent pass-through |
| TRN-04 | P1 | Skip first, middle, or final plugin | Remaining input chain coherent and explicit |
| TRN-05 | P1 | Empty/unavailable plugin directory | Clear built-in-only behavior or error, not no-op success |
| TRN-06 | P0 | Plugin exits nonzero, hangs, or emits invalid JSONPatch | Plugin/resource-specific bounded failure |
| TRN-07 | P1 | Plugin creates replacement and whiteouts original | Replacement once; original excluded |

## Options and whiteouts

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| TRN-08 | P0 | `pvc-rename-map` across Deployment, DC, StatefulSet, Job, CronJob | Every supported PVC reference coherent |
| TRN-09 | P0 | Multiple overlapping PVC names/mappings | Exact non-cascading rename |
| TRN-10 | P1 | Registry replacement in containers/initContainers | Every supported image changed exactly once |
| TRN-11 | P1 | Add/remove annotations with absent maps/special values | Valid idempotent patches; no type panic |
| TRN-12 | P0 | `extra-whiteouts` for PVC and one workload kind | Only requested GroupKinds excluded |
| TRN-13 | P0 | `include-only` with OLM and app resources | Exact allowlist survives; exclusions auditable |
| TRN-14 | P1 | `disable-whiteout-owned` on Pods/ReplicaSets | Only documented owned-resource behavior changes |
| TRN-15 | P0 | Whiteout in stage one followed by another stage | Resource never reappears downstream |

## Rerun and filesystem safety

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| TRN-16 | P0 | Identical transforms into separate directories | Normalized stages and output identical |
| TRN-17 | P0 | Existing plugin stages with/without overwrite | Safe refusal or deterministic clean replacement |
| TRN-18 | P0 | Manual edits in generated and custom stages | Ownership rules protect user changes |
| TRN-19 | P1 | Interrupt between stages and rerun | Partial final stage never appears complete |
| TRN-20 | P1 | Symlinked paths, spaces, malformed resource among valid files | No path escape; exact bad file reported; no silent sibling loss |
| TRN-21 | P1 | Thousands of resources | Bounded memory/time and exact resource count |

---

# 4. `transform --instructions-file`

## Schema and validation

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| INS-01 | P0 | Valid string-form stage | Correct numbered stage |
| INS-02 | P0 | Object-form stage with optionals | Options reach only that stage |
| INS-03 | P1 | Mixed string/object list | Order/defaults preserved |
| INS-04 | P0 | Empty file/stages, missing key, null stage | Clear schema error; no empty successful pipeline |
| INS-05 | P0 | Duplicate stage names | Rejected before filesystem changes |
| INS-06 | P0 | Unknown top-level/stage fields | Strict error; typo not ignored |
| INS-07 | P1 | Wrong YAML types | Error gives field and actual type; no panic |
| INS-08 | P1 | Multi-document YAML, anchors, aliases, comments | Explicit supported behavior; no silent first-doc-only parsing |
| INS-09 | P0 | Instructions plus positional stages | Clear mutual-exclusion error |
| INS-10 | P0 | Instructions plus global/stage option flags | Clear precedence or rejection |

## Pipeline semantics and reconciliation

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| INS-11 | P0 | File lists same plugins as default transform | Final normalized outputs equal |
| INS-12 | P0 | Misspelled plugin in file | Fail closed with available plugin names |
| INS-13 | P0 | Reverse Kubernetes/OpenShift order | Reject unsafe order or deterministic documented result |
| INS-14 | P0 | Duplicate resolved identity via alternate spelling | Plugin cannot run twice accidentally |
| INS-15 | P0 | Per-stage PVC rename plus different next-stage option | Rename once; no option leakage |
| INS-16 | P1 | External/custom stage between built-ins | Receives previous materialized output |
| INS-17 | P1 | Pass-through manual stage | Manual patch appears exactly once |
| INS-18 | P1 | Whiteout followed by instructed stage | Whiteout remains excluded/auditable |
| INS-19 | P0 | Remove a prior stage and rerun without overwrite | Stale stage never remains silently active |
| INS-20 | P0 | Same reconciliation with overwrite | Directory exactly matches new file |
| INS-21 | P0 | Reorder existing stages | Prefixes and inputs rebuilt coherently |
| INS-22 | P1 | Change one stage’s optionals | Affected/downstream output rebuilt; no stale cache |
| INS-23 | P1 | Delete intermediate output then rerun | Repaired or clear failure; no empty next-stage input |
| INS-24 | P1 | Interrupt reconciliation | Recoverable state; no valid custom stage destroyed |

---

# 5. `crane apply`

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| APP-01 | P0 | Render normal multi-stage transform | Correct final stage in aggregate and split YAML |
| APP-02 | P0 | Select stage by plugin/directory name | Exact requested stage selected |
| APP-03 | P0 | Missing/empty/corrupt final kustomization | Nonzero actionable error, not empty success |
| APP-04 | P1 | Multi-document and plugin-generated resources | Every object appears exactly once |
| APP-05 | P1 | Valid, invalid, and injection-like kustomize args | Supported options work; unsafe values rejected |
| APP-06 | P0 | Existing output with/without overwrite | Safe refusal or clean replacement; no stale YAML |
| APP-07 | P1 | Interrupt while generating output, then rerun | No truncated output represented as complete |
| APP-08 | P0 | Skip cluster-scoped resources in mixed bundle | Namespaced output complete; cluster objects absent |
| APP-09 | P0 | Ordered Namespace, CRD/CR, SA/Role/Binding/workload | Stable dependency-aware file order |
| APP-10 | P1 | Same kind/name patterns across namespaces/scopes | Correct unique split paths |
| APP-11 | P0 | Render identical transform twice | Deterministic output |
| APP-12 | P0 | Apply over existing PVC, Service, and workload | Idempotent or actionable immutable conflicts |
| APP-13 | P1 | Existing target replicas/annotations differ | Reconciliation predictable; no hidden destructive drift |
| APP-14 | P1 | Namespace remap with RBAC subjects/references | Structural namespaces consistent |

---

# 6. `crane validate`

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| VAL-01 | P0 | Compatible bundle, live target | Exit 0 and complete inventory |
| VAL-02 | P0 | Deprecated API with valid alternative | Nonzero with accurate suggestion |
| VAL-03 | P0 | CR whose CRD is absent on target | Exact GVK incompatible |
| VAL-04 | P0 | Same input live and offline | Equivalent decisions |
| VAL-05 | P1 | Core `v1` kinds/omitted group | Correct core-group and plural handling |
| VAL-06 | P1 | Nested directories and multi-document files | Every object scanned once |
| VAL-07 | P1 | Duplicate GVK across many objects | Accurate object count and deduplicated discovery |
| VAL-08 | P1 | Malformed YAML beside valid files | Bad filename identified; no partial-success lie |
| VAL-09 | P1 | Empty/malformed API surface | Early schema error, not misleading incompatibility |
| VAL-10 | P0 | Live validation as namespace-admin | No hidden cluster-admin requirement |
| VAL-11 | P1 | Existing validation output with/without overwrite | No stale failures after success |
| VAL-12 | P1 | JSON/YAML reports and exit codes | Formats agree; failures always nonzero |
| VAL-13 | P1 | Cluster-scoped input | Empty namespace and correct scope/plural |
| VAL-14 | P2 | Partially unavailable discovery API | Unavailable group identified; bounded failure |

---

# 7. Direct `transfer-pvc` through Route/Ingress

## Connectivity and topology

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| PVC-D01 | P0 | OCP→OCP using Route | Exact data and full helper cleanup |
| PVC-D02 | P0 | Kubernetes→Kubernetes using NGINX SSL passthrough | Exact data through requested class/subdomain |
| PVC-D03 | P1 | OCP↔Kubernetes mixed platforms | Endpoint determined correctly for destination |
| PVC-D04 | P0 | Same cluster, different namespace | No client/server selector collision |
| PVC-D05 | P0 | Same cluster, same namespace, renamed PVC | Source/destination helpers and volumes remain distinct |
| PVC-D06 | P1 | Namespace and PVC rename together | Exact mapping, no current-namespace leakage |
| PVC-D07 | P1 | Empty and unattached PVCs | Correct standalone transfer |
| PVC-D08 | P0 | `--verify` plus independent hashes | Hashes match; no checksum false confidence |

## PVC compatibility and recovery

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| PVC-D09 | P0 | Destination absent | Intended class/size/mode created |
| PVC-D10 | P0 | Compatible destination exists with data | Safe explicit reuse; unrelated data not silently destroyed |
| PVC-D11 | P0 | Existing wrong class/access/volume mode | Fail before copy with both specs |
| PVC-D12 | P0 | Destination smaller than source/used bytes | Early failure or accurate ENOSPC; never false success |
| PVC-D13 | P1 | WaitForFirstConsumer class | Helper scheduling binds PVC; no unexplained Pending |
| PVC-D14 | P0 | Destination mounted by active writer | Safe warning/failure, no silent corruption |
| PVC-D15 | P0 | Nested, empty, Unicode, symlink, hardlink, sparse, executable files | Supported semantics exact; unsupported explicit |
| PVC-D16 | P0 | One unreadable/restrictive file | Nonzero/partial status names missing file |
| PVC-D17 | P0 | Interrupt midway and rerun | Safe convergence; no stale-helper conflict |
| PVC-D18 | P1 | Kill client, server, or stunnel pod independently | Phase-specific failure and retryable cleanup |
| PVC-D19 | P1 | DNS, bad Ingress class, closed 443, TLS mismatch | Actionable endpoint/TLS error |
| PVC-D20 | P0 | Missing Route-domain permission as namespace-admin | Exact RBAC need; no cluster-admin assumption |
| PVC-D21 | P1 | Concurrent same-namespace transfers | Names, labels, Services, Secrets isolated |
| PVC-D22 | P1 | Recopy after same-size/same-mtime data change | Destination converges; checksum behavior correct |
| PVC-D23 | P1 | Active writer then quiesced final pass | Final pass consistent; no snapshot claim |
| PVC-D24 | P1 | Inventory helpers after success/failure | Clean success; diagnosable and retryable failure state |

---

# 8. Indirect `transfer-pvc` through object storage

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| PVC-I01 | P0 | Baseline config-file upload/download | Exact data without direct cluster connectivity |
| PVC-I02 | P0 | Same with encryption | Cloud objects not plaintext; target decrypts |
| PVC-I03 | P0 | Pre-created rclone Secret | Correct cluster/namespace Secret used |
| PVC-I04 | P0 | Missing config, both config sources, encrypt+Secret | Fail before PVC/helper creation |
| PVC-I05 | P0 | Default cleanup versus keep-cloud-data | Only intended prefix deleted/retained |
| PVC-I06 | P0 | Interrupt after upload before download, then retry | Upload safely reusable or limitation explicit |
| PVC-I07 | P0 | Revoke credentials during upload | Upload failure nonzero; no false next phase |
| PVC-I08 | P0 | Revoke credentials during download | Recoverable uploaded data not unexpectedly deleted |
| PVC-I09 | P1 | Delete/corrupt one object before download | Exact nonzero integrity failure |
| PVC-I10 | P1 | Fill destination during download | Clear partial/ENOSPC result |
| PVC-I11 | P0 | Same PVC name in two namespaces/shared bucket | Isolated prefixes; no cross-data |
| PVC-I12 | P0 | Concurrent transfers in one bucket | No key, Secret, or cleanup collision |
| PVC-I13 | P1 | Remote path with trailing slash/special characters | Predictable scoped prefix/cleanup |
| PVC-I14 | P0 | Inspect logs/pods/Secrets/object data | No credential, password, or plaintext leak |
| PVC-I15 | P1 | Large file count and large file | Accurate progress, bounded memory, exact result |
| PVC-I16 | P1 | Temporary Secret left from prior failure | Safe ownership/reuse or explicit conflict |
| PVC-I17 | P1 | Storage throttling/transient 5xx | Bounded visible retry, no corruption |
| PVC-I18 | P1 | Cleanup fails after data succeeds | Cleanup failure not hidden by transfer success |

---

# 9. StorageClass conversion

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| SC-01 | P0 | Same-cluster Deployment/DC conversion with PVC rename | New class, correct claim, exact data |
| SC-02 | P0 | Cross-cluster conversion to differently named class | No source class leaks to target |
| SC-03 | P0 | Conversion using instructions-file rename | Equivalent to CLI optional flag |
| SC-04 | P0 | Multiple PVC mappings/classes | Each workload volume gets correct data |
| SC-05 | P0 | StatefulSet ordinals and volumeClaimTemplates | Existing and future PVC behavior correct; immutable path safe |
| SC-06 | P1 | Job/CronJob/initContainer PVC consumers | All pod-template references changed |
| SC-07 | P0 | Apply when old and new PVCs both exist | No mutation of old class; idempotent workload change |
| SC-08 | P0 | Same target PVC name already on wrong class | Refuse/requires rename; no unsafe reuse |
| SC-09 | P0 | RWO→RWX, filesystem→block, bad topology | Incompatibility fails early |
| SC-10 | P0 | New capacity below source used bytes | No truncation/false success |
| SC-11 | P1 | WaitForFirstConsumer destination | Correct helper binding; no deadlock |
| SC-12 | P1 | Interrupt after copy before workload apply | Old app safe; conversion resumable |
| SC-13 | P1 | Roll workload back to old PVC | Rollback path remains until explicit deletion |
| SC-14 | P1 | Delete old PVC after proof, then rerun | Completed conversion recognized safely |

---

# 10. Plugin management and plugin interface

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| PLG-01 | P1 | List installed/available plugins online and offline | Clear distinction; remote errors not empty success |
| PLG-02 | P1 | Install valid plugin | Atomic executable/version/metadata install |
| PLG-03 | P0 | Bad checksum or malformed metadata | Reject; existing plugin untouched |
| PLG-04 | P1 | Reinstall, downgrade, and upgrade | Explicit deterministic version behavior |
| PLG-05 | P1 | Remove installed, missing, and default plugin | Safe correct-path behavior |
| PLG-06 | P0 | Invalid patch, wrong resource, stderr noise, nonzero exit | Exact plugin/resource failure |
| PLG-07 | P1 | Name collision/non-executable binary | Unambiguous discovery and error |
| PLG-08 | P1 | list-plugins/optionals versus actual runtime | Advertised metadata matches accepted behavior |

---

# 11. `skopeo-sync-gen`

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| SKO-01 | P1 | Internal-registry ImageStreams | Complete correct source YAML |
| SKO-02 | P1 | Mixed internal/external references | Only intended internal images selected |
| SKO-03 | P1 | Custom registry host and port | Exact host matching |
| SKO-04 | P1 | Multiple tags/digests/duplicates | Deterministic deduplicated output |
| SKO-05 | P2 | No ImageStreams, malformed file, missing export | Valid empty or actionable nonzero result |
| SKO-06 | P2 | Special characters in registry URL/path | Valid YAML and exact key |

---

# 12. `convert`

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| CNV-01 | P1 | Supported deprecated resource | Valid complete replacement |
| CNV-02 | P1 | Unsupported type | Nonzero with supported types, not no-op success |
| CNV-03 | P1 | Namespace-admin conversion | No hidden cluster-admin requirement |
| CNV-04 | P1 | Search/insecure/block registry combinations | Correct precedence/configuration |
| CNV-05 | P1 | Existing output and rerun | Deterministic, no stale/duplicates |
| CNV-06 | P2 | Missing source API or malformed resource | Resource/type-specific error; no panic |

---

# 13. `tunnel-api`

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| TUN-01 | P1 | Valid source/destination | Ready tunnel and verified cross-cluster traffic |
| TUN-02 | P1 | Same cluster both contexts | Rejected before creating resources |
| TUN-03 | P1 | Missing/bad context | Exact context error; no nil dereference |
| TUN-04 | P1 | Proxy with valid credentials | Works without credential exposure |
| TUN-05 | P1 | Incomplete proxy credentials | Clear validation, not silent ignore |
| TUN-06 | P1 | Interrupt certificate/pod creation | Safe cleanup and retry |
| TUN-07 | P1 | Insufficient namespace-admin permissions | Exact RBAC need; no abrupt unclean exit |
| TUN-08 | P2 | Invalid/unpullable custom images | Pod/image failure surfaced, no indefinite wait |

---

# 14. Version, completion, and UX

| ID | Pri | Test | Expected / bug target |
|---|---|---|---|
| UX-01 | P2 | Version from source/release binary | Accurate Crane, crane-lib, Kustomize metadata |
| UX-02 | P2 | Bash/zsh completion for commands/stages/plugins | Correct suggestions and no filesystem/network surprise |
| UX-03 | P2 | Invalid flags/arguments across commands | Nonzero focused error; no panic/ignored input |
| UX-04 | P2 | Debug versus normal mode | More detail without changing outcome |
| UX-05 | P1 | Wrong runtime object type/API resource errors | Actual type and API resource named |

---

# 15. Customer end-to-end scenarios

| ID | Pri | Scenario | Features / main bug target |
|---|---|---|---|
| E2E-01 | P0 | MongoDB with 25,000 records | Full OCP direct migration; workload success with exact data |
| E2E-02 | P0 | PostgreSQL/MySQL clean shutdown | Secrets, WAL/recovery, application query |
| E2E-03 | P0 | Kafka/clustered app with multiple PVCs | StatefulSet mapping and cross-volume consistency |
| E2E-04 | P0 | Active copy then quiesced final pass | Delta/retry and false snapshot confidence |
| E2E-05 | P0 | Argo CD-managed app adopted on target | GitOps drift and field ownership |
| E2E-06 | P0 | NFS/CephFS fixed UID/GID, restrictive files | Partial transfer and mixed ownership |
| E2E-07 | P1 | Namespace rename with RBAC/network/PVC refs | Source namespace left in structural references |
| E2E-08 | P1 | Operator/CRD app across OCP versions | CRD, OLM, plugin, validation compatibility |
| E2E-09 | P0 | Disconnected encrypted indirect migration | Credential, encryption, retry, cleanup correctness |
| E2E-10 | P1 | Partially pre-provisioned target | Immutable fields and unsafe overwrite |
| E2E-11 | P1 | Thousands of resources and multiple PVCs | Pagination, determinism, concurrency |
| E2E-12 | P1 | Least-privilege split admin handoff | Hidden privilege and cluster-resource handling |

---

# One-day P0 shortlist

The full catalog is a reusable backlog. For one focused day, run:

1. `CLI-04` — namespace-admin isolation.
2. `EXP-08` — filtered export dependency completeness.
3. `EXP-24` — export pagination.
4. `TRN-03` — unknown plugin fail-closed behavior.
5. `TRN-09` — overlapping PVC rename mappings.
6. `INS-12` — misspelled instructed plugin.
7. `INS-19` — stale stage reconciliation.
8. `APP-12` — existing target and immutable fields.
9. `VAL-04` — live/offline equivalence.
10. `PVC-D17` — direct transfer interruption and retry.
11. `PVC-D16` — unreadable file and false success.
12. `SC-05` — StatefulSet StorageClass conversion.
13. `PVC-I06` — indirect interruption between phases.
14. `PVC-I14` — credential/encryption-material leakage.
15. `E2E-03` — multi-PVC consistency.

# Results index

Add one row immediately after every test. Store detailed evidence in `test-results/<run>/<ID>/report.md`.

| ID | Status | Commit | Environment | Duration | Evidence | Potential bug | Issue |
|---|---|---|---|---:|---|---|---|
| SC-01 | FAIL | `11b3d5fa4204` | Minikube `src`, Kubernetes v1.34.0 | ~15m | [`report.md`](../../test-results/20260831-minikube-sc/SC-01/report.md) | Declared fixture data omitted; application impact was not modeled | TBD |
| SC-02 | PASS | `11b3d5fa4204` | Minikube `src` → `tgt`, Kubernetes v1.34.0 | ~10m | [`report.md`](../../test-results/20260831-minikube-sc/SC-02/report.md) | Rsync exit 23 swallowed; contradictory final success summary | TBD |

Allowed statuses: `PASS`, `FAIL`, `BLOCKED`, `INCONCLUSIVE`, `NOT RUN`.

# Per-test report template

```markdown
# <ID> — <title>

- Status: PASS | FAIL | BLOCKED | INCONCLUSIVE
- Date/time and duration:
- Crane commit/version:
- Cluster versions, contexts, and effective users:
- Namespaces and StorageClasses:

## Purpose
## Setup
## Commands executed
## Expected result
## Actual result

## Evidence

- stdout/stderr and exit code:
- source/target resources and events:
- generated trees/YAML/reports:
- file hashes or application query:
- temporary resources after completion:
- identical rerun result:

## Verdict

Explain why this is PASS, FAIL, BLOCKED, or INCONCLUSIVE. Exit code zero alone is not a pass.

## Potential bug

- Summary and user impact:
- Suspected component:
- Severity: critical | high | medium | low
- Reproducibility:
- Minimal reproduction:
- Expected versus actual:
- Workaround:
- Existing issue/PR search:

## Cleanup
```

# Execution workflow

For each case: create only its fixtures, record pre-state, execute, collect evidence before cleanup, rerun once, write the report, update the index, and isolate/search any failure before starting the next case.
