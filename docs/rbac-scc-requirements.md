# RBAC, SCC, and Capabilities Required for Crane Migration

This document catalogs every permission needed to migrate data with crane, addressing [issue #61](https://github.com/konveyor-ecosystem/kubectl-migrate/issues/61). Permissions are broken down by migration phase, user role, and cluster (source vs target).

---

## Two Personas

Every crane migration involves two levels of access:

| Persona | Used for | Typical identity |
|---------|----------|-----------------|
| **Cluster admin** | One-time setup: create user, namespace, grant RoleBinding, optionally grant SCC | `kubeadmin` |
| **Migration user** | Runs the migration pipeline: export, transfer-pvc, oc apply | Non-admin user with namespace-level permissions |

---

## Permissions by Migration Phase

### `crane export` (source cluster -- read-only)

Lists all resources in the source namespace and writes YAML to disk.

| Resource | API Group | Verbs | Notes |
|----------|-----------|-------|-------|
| All namespaced resources | _(discovered)_ | `list` | API discovery enumerates all resource types |
| Namespaces | `""` (core) | `get` | Read namespace metadata |
| ImageStreamTags, ImageTags | `image.openshift.io` | `list`, `get` | OpenShift-specific |

With `--cluster-scoped-rbac` flag (optional):

| Resource | API Group | Verbs | Notes |
|----------|-----------|-------|-------|
| ClusterRoleBinding | `rbac.authorization.k8s.io` | `list` | Cluster-scoped |
| ClusterRole | `rbac.authorization.k8s.io` | `list` | Cluster-scoped |
| SecurityContextConstraints | `security.openshift.io` | `list` | Cluster-scoped |

### `crane transform` (local only)

No cluster access. Operates on local files. No permissions needed.

### `crane transfer-pvc` (both clusters)

The most permission-intensive step. Creates rsync pods, stunnel tunnels, and endpoints.

**Source cluster:**

| Resource | API Group | Verbs | Purpose |
|----------|-----------|-------|---------|
| PersistentVolumeClaim | `""` | `get` | Read source PVC spec |
| Namespace | `""` | `get` | Read UID range annotations for security context |
| Pod | `""` | `get`, `list`, `create`, `update`, `delete` | Rsync client pod lifecycle |
| Pod (logs) | `""` | `get` (subresource: `log`) | Stream rsync progress |
| Secret | `""` | `create`, `delete` | TLS certificates for stunnel |
| ConfigMap | `""` | `create`, `update`, `delete` | Stunnel client config |

**Target (destination) cluster:**

| Resource | API Group | Verbs | Purpose |
|----------|-----------|-------|---------|
| PersistentVolumeClaim | `""` | `create` | Create destination PVC |
| Pod | `""` | `get`, `create`, `update`, `delete` | Rsync server pod lifecycle |
| Secret | `""` | `get`, `list`, `create`, `update`, `delete` | TLS certs (generated and copied to source) |
| ConfigMap | `""` | `create`, `update`, `delete` | Stunnel server + rsync config |
| Service | `""` | `get`, `list`, `create`, `update`, `delete` | Endpoint service for rsync |
| Route | `route.openshift.io` | `get`, `create`, `update`, `delete` | Route endpoint (when `--endpoint=route`) |
| Ingress | `networking.k8s.io` | `get`, `create`, `update`, `delete` | Ingress endpoint (when `--endpoint=nginx-ingress`) |
| Ingress | `config.openshift.io` | `get` | **Cluster-scoped** -- reads `ingresses/cluster` for route hostname |

### `crane apply` (local only)

No cluster access. Merges export + transform output to local files. No permissions needed.

### `oc apply` (target cluster)

Applies the generated manifests. Exact permissions depend on what was exported, but typically:

| Resource | API Group | Verbs |
|----------|-----------|-------|
| Deployment | `apps` | `create`, `update`, `patch` |
| ServiceAccount | `""` | `create`, `update`, `patch` |
| ConfigMap | `""` | `create`, `update`, `patch` |
| Secret | `""` | `create`, `update`, `patch` |
| Service | `""` | `create`, `update`, `patch` |
| RoleBinding | `rbac.authorization.k8s.io` | `create`, `update`, `patch` |
| PersistentVolumeClaim | `""` | `create`, `update`, `patch` |

---

## SCC Requirements

### Rsync pods (created by transfer-pvc)

Rsync pods are designed to run under `restricted-v2` SCC with no special privileges:

| Setting | Rsync Client (source) | Rsync Server (target) |
|---------|----------------------|----------------------|
| `runAsUser` | Namespace UID (from annotation) | _(not set; SCC assigns)_ |
| `runAsGroup` | Namespace supplemental group | _(not set)_ |
| `fsGroup` | Namespace supplemental group | Namespace supplemental group |
| `runAsNonRoot` | `true` | `true` |
| `allowPrivilegeEscalation` | `false` | `false` |
| `capabilities.drop` | `["ALL"]` | `["ALL"]` |
| `privileged` | `false` | _(not set)_ |
| `seccompProfile` | _(not set)_ | `RuntimeDefault` |

**No special SCC is needed for the rsync pods themselves.** They are `restricted-v2` compatible.

UIDs/GIDs come from `getIDsForNamespace()` which reads `openshift.io/sa.scc.uid-range` and `openshift.io/sa.scc.supplemental-groups` annotations on the namespace.

### Workload SCC (for the app being migrated)

| Workload Pattern | SCC Required | Grant Command |
|-----------------|-------------|---------------|
| Namespace-allocated UID (no explicit `runAsUser`) | `restricted-v2` (default) | None needed |
| Explicit `runAsUser` outside namespace range (e.g. 1001) | `nonroot-v2` | `oc adm policy add-scc-to-user nonroot-v2 -z default -n <namespace>` |
| `supplementalGroups` outside namespace range | `nonroot-v2` | Same as above |
| Privileged containers | Not supported by crane | N/A |

### SCC RoleBinding export caveat

When a namespace has a non-default SCC grant (e.g. `nonroot-v2`), `crane export` captures the RoleBinding. Applying it on the target as a non-admin user fails because the user lacks permission to grant SCC access. **Workaround**: grant the SCC on the target as kubeadmin before running `oc apply`, and remove the SCC RoleBinding files from the crane output directory.

---

## Minimum RBAC Role for Crane Migration

The following Role captures the minimum permissions a migration user needs. This avoids granting the full `admin` ClusterRole.

### Source cluster Role

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: crane-migration-source
  namespace: <source-namespace>
rules:
# --- REQUIRED: crane export (read all namespace resources) ---
- apiGroups: [""]
  resources: ["pods", "persistentvolumeclaims", "configmaps", "secrets",
              "services", "serviceaccounts", "endpoints", "events",
              "replicationcontrollers"]
  verbs: ["get", "list", "watch"]
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["get"]
- apiGroups: ["apps"]
  resources: ["deployments", "replicasets", "statefulsets", "daemonsets",
              "controllerrevisions"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["batch"]
  resources: ["jobs", "cronjobs"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["autoscaling"]
  resources: ["horizontalpodautoscalers"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["policy"]
  resources: ["poddisruptionbudgets"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["networking.k8s.io"]
  resources: ["ingresses", "networkpolicies"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["", "route.openshift.io"]
  resources: ["routes"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["", "authorization.openshift.io"]
  resources: ["rolebindings", "roles"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["rbac.authorization.k8s.io"]
  resources: ["rolebindings", "roles"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["get", "list", "watch"]

# OpenShift-specific (remove if migrating vanilla K8s)
- apiGroups: ["", "image.openshift.io"]
  resources: ["imagestreams", "imagestreamtags", "imagetags"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["", "apps.openshift.io"]
  resources: ["deploymentconfigs"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["", "build.openshift.io"]
  resources: ["buildconfigs", "builds"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["", "template.openshift.io"]
  resources: ["templateinstances", "templates"]
  verbs: ["get", "list", "watch"]
- apiGroups: ["snapshot.storage.k8s.io"]
  resources: ["volumesnapshots"]
  verbs: ["get", "list", "watch"]

# --- REQUIRED: crane transfer-pvc (rsync client lifecycle) ---
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["create", "update", "delete"]
- apiGroups: [""]
  resources: ["pods/log"]
  verbs: ["get"]
- apiGroups: [""]
  resources: ["secrets", "configmaps"]
  verbs: ["create", "update", "delete"]

# Scale down source workload before transfer
- apiGroups: ["apps"]
  resources: ["deployments/scale", "statefulsets/scale"]
  verbs: ["update", "patch"]

# --- OPTIONAL: crane export --cluster-scoped-rbac (requires ClusterRole) ---
# Uncomment if you need cluster-scoped RBAC/SCC export:
# - apiGroups: ["rbac.authorization.k8s.io"]
#   resources: ["clusterroles", "clusterrolebindings"]
#   verbs: ["list"]
# - apiGroups: ["security.openshift.io"]
#   resources: ["securitycontextconstraints"]
#   verbs: ["list"]
```

### Target cluster Role

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: crane-migration-target
  namespace: <target-namespace>
rules:
# --- REQUIRED: crane transfer-pvc (rsync server + endpoint) ---
- apiGroups: [""]
  resources: ["persistentvolumeclaims"]
  verbs: ["create", "get"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "create", "update", "delete"]
- apiGroups: [""]
  resources: ["secrets"]
  verbs: ["get", "list", "create", "update", "delete"]
- apiGroups: [""]
  resources: ["configmaps"]
  verbs: ["create", "update", "delete"]
- apiGroups: [""]
  resources: ["services"]
  verbs: ["get", "list", "create", "update", "delete"]
- apiGroups: ["", "route.openshift.io"]
  resources: ["routes"]
  verbs: ["get", "create", "update", "delete"]
- apiGroups: ["networking.k8s.io"]
  resources: ["ingresses"]
  verbs: ["get", "create", "update", "delete"]
- apiGroups: [""]
  resources: ["namespaces"]
  verbs: ["get"]

# --- REQUIRED: oc apply (core workload manifests) ---
- apiGroups: [""]
  resources: ["configmaps", "secrets", "serviceaccounts", "services",
              "endpoints", "persistentvolumeclaims"]
  verbs: ["create", "update", "patch", "get", "list"]
- apiGroups: ["apps"]
  resources: ["deployments", "replicasets", "statefulsets", "daemonsets"]
  verbs: ["create", "update", "patch", "get", "list"]
- apiGroups: ["", "authorization.openshift.io"]
  resources: ["rolebindings", "roles"]
  verbs: ["create", "update", "patch", "get", "list"]
- apiGroups: ["rbac.authorization.k8s.io"]
  resources: ["rolebindings", "roles"]
  verbs: ["create", "update", "patch", "get", "list"]

# --- LIKELY NEEDED: common workload resources ---
- apiGroups: ["batch"]
  resources: ["jobs", "cronjobs"]
  verbs: ["create", "update", "patch", "get", "list"]
- apiGroups: ["autoscaling"]
  resources: ["horizontalpodautoscalers"]
  verbs: ["create", "update", "patch", "get", "list"]
- apiGroups: ["policy"]
  resources: ["poddisruptionbudgets"]
  verbs: ["create", "update", "patch", "get", "list"]
- apiGroups: ["networking.k8s.io"]
  resources: ["networkpolicies"]
  verbs: ["create", "update", "patch", "get", "list"]
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["create", "update", "patch", "get", "list"]

# --- OpenShift-specific (remove if migrating vanilla K8s) ---
- apiGroups: ["", "apps.openshift.io"]
  resources: ["deploymentconfigs"]
  verbs: ["create", "update", "patch", "get", "list"]
- apiGroups: ["", "build.openshift.io"]
  resources: ["buildconfigs", "builds"]
  verbs: ["create", "update", "patch", "get", "list"]
- apiGroups: ["", "image.openshift.io"]
  resources: ["imagestreams", "imagestreamtags", "imagetags"]
  verbs: ["create", "update", "patch", "get", "list"]
- apiGroups: ["", "template.openshift.io"]
  resources: ["templateinstances", "templates"]
  verbs: ["create", "update", "patch", "get", "list"]
- apiGroups: ["snapshot.storage.k8s.io"]
  resources: ["volumesnapshots"]
  verbs: ["create", "update", "patch", "get", "list"]
```

### Cluster-scoped ClusterRole (target cluster, for route endpoint)

Only needed when using `--endpoint=route`:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: crane-migration-cluster
rules:
- apiGroups: ["config.openshift.io"]
  resources: ["ingresses"]
  resourceNames: ["cluster"]
  verbs: ["get"]
```

---

## Real-World Permission Assessment

`crane export` uses **Kubernetes API discovery** to enumerate ALL namespaced resource types and lists every instance. It does not have a hardcoded resource list -- if a resource type exists in the namespace and supports `list`, crane exports it. This means the permissions needed depend entirely on what is deployed in the source namespace.

The following assessment categorizes every resource type from the OpenShift `admin` ClusterRole into three tiers.

### NEEDS -- Crane actively calls these APIs

These are resources crane's Go code explicitly creates, gets, or deletes via its API clients. Without these, crane commands fail.

| Resource | API Group | Why | Which crane command | Cluster |
|----------|-----------|-----|---------------------|---------|
| **Pods** | `""` | Rsync client/server pod lifecycle | `transfer-pvc` | Both |
| **Pods/log** | `""` | Stream rsync progress | `transfer-pvc` | Source |
| **PersistentVolumeClaims** | `""` | Read source PVC, create destination PVC | `transfer-pvc` | Both |
| **Secrets** | `""` | TLS certs for stunnel tunnel | `transfer-pvc` | Both |
| **ConfigMaps** | `""` | Stunnel + rsync config | `transfer-pvc` | Both |
| **Services** | `""` | Endpoint service for rsync server | `transfer-pvc` | Target |
| **Routes** | `route.openshift.io` | Route endpoint for rsync tunnel | `transfer-pvc` (when `--endpoint=route`) | Target |
| **Ingresses** | `networking.k8s.io` | Ingress endpoint for rsync tunnel | `transfer-pvc` (when `--endpoint=nginx-ingress`) | Target |
| **Namespaces** | `""` | Read UID range annotations for security context | `transfer-pvc`, `export` | Both |
| **Ingresses** | `config.openshift.io` | Cluster-scoped: read `cluster` ingress for route hostname | `transfer-pvc` | Target |
| **All namespaced resources** | _(discovery)_ | List/get for export | `export` | Source |

### MIGHT NEED -- Depends on what is in the namespace

`crane export` discovers and exports these if they exist. `oc apply` on the target then needs write access to recreate them. Whether you need these permissions depends on your workload.

**Commonly needed in real-world migrations:**

| Resource | API Group | Real-world scenario | Likelihood |
|----------|-----------|---------------------|------------|
| **Deployments** | `apps` | Almost every workload | Very high |
| **StatefulSets** | `apps` | Databases (PostgreSQL, MySQL, MongoDB, Elasticsearch, Kafka) | High |
| **ReplicaSets** | `apps` | Owned by Deployments, exported as-is | High |
| **ServiceAccounts** | `""` | Custom SAs with RBAC bindings | High |
| **RoleBindings** | `rbac.authorization.k8s.io` | RBAC for custom SAs, SCC grants | High |
| **Roles** | `rbac.authorization.k8s.io` | Custom namespace roles | Medium |
| **Endpoints** | `""` | Exported with Services (but whiteout by transform) | Medium |
| **CronJobs** | `batch` | Scheduled tasks (backups, reports, cleanup) | Medium |
| **Jobs** | `batch` | One-off tasks, database migrations | Medium |
| **HorizontalPodAutoscalers** | `autoscaling` | Auto-scaling production workloads | Medium |
| **NetworkPolicies** | `networking.k8s.io` | Namespace isolation, zero-trust | Medium |
| **DaemonSets** | `apps` | Node-level agents (logging, monitoring) | Low-Medium |
| **PodDisruptionBudgets** | `policy` | HA workloads | Medium |
| **DeploymentConfigs** | `apps.openshift.io` | Legacy OpenShift workloads (pre-Deployment) | Medium (OCP 3.x migrations) |
| **BuildConfigs** | `build.openshift.io` | S2I / Docker builds on OpenShift | Low-Medium |
| **ImageStreams** | `image.openshift.io` | OpenShift image management | Low-Medium |
| **Leases** | `coordination.k8s.io` | Leader election for HA controllers | Low |
| **VolumeSnapshots** | `snapshot.storage.k8s.io` | Pre-migration snapshots | Low |
| **Custom CRDs/CRs** | _(varies)_ | Operator-managed resources (KafkaTopic, PostgresCluster, etc.) | Depends on workload |

**Rarely needed but possible:**

| Resource | API Group | Real-world scenario | Likelihood |
|----------|-----------|---------------------|------------|
| **ReplicationControllers** | `""` | Very old workloads (pre-Deployment) | Very low |
| **Templates** | `template.openshift.io` | OpenShift template instances | Low |
| **ImageStreamImports** | `image.openshift.io` | Importing external images into OpenShift | Very low |
| **network-attachment-definitions** | `k8s.cni.cncf.io` | Multus / secondary network interfaces | Low |
| **Helm chart repos** | `helm.openshift.io` | Project-level Helm repos | Very low |

### WILL NEVER NEED -- Admin has these, crane cannot use them

These are in the `admin` ClusterRole but have no relevance to crane migration. Crane never calls these APIs, and they would never appear in a `crane export` output.

| Resource | API Group | Why crane will never need this |
|----------|-----------|-------------------------------|
| **Velero backups/restores/schedules** | `velero.io` | Crane is not OADP; completely separate tool |
| **Velero BSLs, VSLs, download/delete requests** | `velero.io` | Same -- OADP-specific |
| **OADP DPAs, cloudstorages** | `oadp.openshift.io` | Same -- OADP-specific |
| **MTC migplans, migclusters, migmigrations** | `migration.openshift.io` | Crane is not MTC; separate tool |
| **MTC directvolumemigrations, mighooks** | `migration.openshift.io` | Same -- MTC-specific |
| **OLM subscriptions (write)** | `operators.coreos.com` | Crane does not install/manage operators |
| **OLM catalogsources, installplans (write)** | `operators.coreos.com` | Same -- operator lifecycle |
| **OLM operatorgroups** | `operators.coreos.com` | Same -- operator lifecycle |
| **packagemanifests** | `packages.operators.coreos.com` | OLM package catalog; not workload data |
| **pods/attach** | `""` | Crane creates pods but never attaches to them |
| **pods/exec** | `""` | Crane does not exec into pods |
| **pods/portforward** | `""` | Crane uses routes/ingress, not portforward |
| **pods/proxy** | `""` | Crane does not proxy to pods |
| **pods/eviction** | `""` | Crane scales down; does not evict |
| **services/proxy** | `""` | Crane does not proxy through services |
| **serviceaccounts (impersonate)** | `""` | Crane runs as itself, never impersonates |
| **serviceaccounts/token (create)** | `""` | Crane uses existing auth tokens |
| **podsecuritypolicyreviews** | `security.openshift.io` | Policy review APIs; crane doesn't evaluate policies |
| **localsubjectaccessreviews** | `authorization.openshift.io` | Auth review APIs; crane doesn't check permissions |
| **subjectaccessreviews** | `authorization.openshift.io` | Same |
| **subjectrulesreviews** | `authorization.openshift.io` | Same |
| **resourceaccessreviews** | `authorization.openshift.io` | Same |
| **rolebindingrestrictions** | `authorization.openshift.io` | Policy metadata; not workload data |
| **projects (delete/update/patch)** | `project.openshift.io` | Crane operates in existing namespaces |
| **routes/custom-host (create)** | `route.openshift.io` | Crane uses auto-generated hostnames |
| **routes/status (update)** | `route.openshift.io` | Managed by the router; never user-set |
| **builds/clone** | `build.openshift.io` | Crane exports builds, doesn't clone them |
| **builds/details (update)** | `build.openshift.io` | Build detail management; not migration |
| **buildconfigs/instantiate** | `build.openshift.io` | Triggering builds; not migration |
| **buildconfigs/instantiatebinary** | `build.openshift.io` | Binary builds; not migration |
| **deploymentconfigs/instantiate** | `apps.openshift.io` | DC rollout triggers; not migration |
| **deploymentconfigs/rollback** | `apps.openshift.io` | DC rollbacks; not migration |
| **deploymentconfigrollbacks** | `apps.openshift.io` | Same |
| **imagestreams/layers (update)** | `image.openshift.io` | Layer push; crane only reads |
| **imagestreams/secrets** | `image.openshift.io` | Pull secret management; not migration |
| **imagestreamimports** | `image.openshift.io` | Image importing; not migration |
| **resourcequotas** | `""` | Cluster-managed; not workload data |
| **resourcequotausages** | `""` | Same |
| **limitranges** | `""` | Same -- admin-set limits, not workload |
| **appliedclusterresourcequotas** | `quota.openshift.io` | Same |
| **bindings** | `""` | Pod scheduling bindings; internal |
| **metrics (nodes/pods)** | `metrics.k8s.io` | Monitoring data; not workload state |
| **events (write)** | `""` | Crane reads events but never creates them |
| **namespaces/status** | `""` | Namespace status; read-only metadata |
| **deletecollection (any resource)** | _(all)_ | Crane deletes individually, never bulk-deletes |

### Summary by the numbers

| Category | Resource types | % of admin |
|----------|---------------|------------|
| **NEEDS** (crane API calls) | ~10 | ~6% |
| **MIGHT NEED** (workload-dependent, via export/apply) | ~20 | ~13% |
| **WILL NEVER NEED** | ~125+ distinct resource/verb combinations | ~81% |

---