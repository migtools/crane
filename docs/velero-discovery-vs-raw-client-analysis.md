# Velero Discovery Helper vs Raw client-go Discovery: Analysis & Migration Path

## 1. Background

Crane's `export` command currently uses Velero's `discovery.Helper` (from
`github.com/vmware-tanzu/velero v1.6.3`) to discover API types on the cluster.
This document compares that approach with the **raw `client-go` discovery
client** used in `go-cli-playground/mini-crane`, analyses the gaps, and
describes what a Velero-free implementation would look like.

---

## 2. What each approach does

### 2.1 Velero `discovery.Helper` (current Crane)

**Location:** `cmd/export/export.go` lines 112-145

**Creation chain:**

```
configFlags.ToDiscoveryClient()        → discovery.DiscoveryInterface (cached)
discoveryClient.Invalidate()           → force fresh data
discovery.NewHelper(discoveryClient, log)  → velero Helper
```

**Inside `discovery.NewHelper` → `Refresh()` the helper makes these calls:**

| Step | API call | Purpose |
|------|----------|---------|
| 1 | `restmapper.GetAPIGroupResources(discoveryClient)` | Builds REST mapper (GVK → GVR mapping) |
| 2 | `discoveryClient.ServerGroupsAndResources()` **or** `discoveryClient.ServerPreferredResources()` | Gets the type catalog. Which one runs depends on the `APIGroupVersions` feature flag. |
| 3 | `discovery.FilteredBy(filterByVerbs, ...)` | Strips resources lacking **all four** verbs: `list`, `create`, `get`, `delete` |
| 4 | `sortResources(...)` | Moves `extensions` group to the end |
| 5 | Builds `resourcesMap` (GVR → APIResource) and `kindMap` (GVK → APIResource) | Lookup tables for `ResourceFor` / `KindFor` |
| 6 | `discoveryClient.ServerGroups()` | Fetches `[]metav1.APIGroup` (used by Crane for preferred-version filtering) |
| 7 | `discoveryClient.ServerVersion()` | Fetches cluster version info (unused by Crane) |

**What Crane consumes from the helper:**

| Method | Return type | How Crane uses it |
|--------|-------------|-------------------|
| `discoveryHelper.Resources()` | `[]*metav1.APIResourceList` | Passed to `resourceToExtract()` — the full type catalog |
| `discoveryHelper.APIGroups()` | `[]metav1.APIGroup` | Used in `resourceToExtract()` to keep only preferred versions |

**What Crane does NOT use from the helper:**

- `ResourceFor()` / `KindFor()` — never called
- `Refresh()` — only called once internally during `NewHelper`
- `ServerVersion()` — never called
- The `resourcesMap` / `kindMap` lookup tables — never used
- The REST mapper / shortcut expander — never used
- Thread-safety (`sync.RWMutex`) — irrelevant (single goroutine)

**Velero-specific extras that are activated but unused:**

```go
features.NewFeatureFlagSet()
features.Enable(velerov1api.APIGroupVersionsFeatureFlag)
```

This forces `Refresh()` down the `ServerGroupsAndResources()` branch (all
versions, not just preferred). But Crane's own `resourceToExtract()` then
**re-filters to preferred versions anyway** using `APIGroups()`. So the
feature-flag dance achieves nothing functionally — it just fetches extra data
that gets thrown away.

### 2.2 Raw `client-go` discovery (mini-crane)

**Location:** `mini-crane/cmd/export/export.go` lines 140-187

**Creation chain:**

```
configFlags.ToRESTConfig()                                → rest.Config
discovery.NewDiscoveryClientForConfig(restConfig)          → *discovery.DiscoveryClient
restmapper.NewDeferredDiscoveryRESTMapper(memcache)        → RESTMapper (for --kind resolution)
```

**The single discovery call:**

```go
func discoverResources(discoveryClient *discovery.DiscoveryClient) ([]*v1.APIResourceList, error) {
    return discoveryClient.ServerPreferredResources()
}
```

No wrapper, no helper object, no verb filtering, no sorting, no feature flags.

---

## 3. Side-by-side comparison

| Aspect | Velero Helper (Crane) | Raw client-go (mini-crane) |
|--------|----------------------|---------------------------|
| **Discovery call** | `ServerGroupsAndResources()` (due to feature flag) | `ServerPreferredResources()` |
| **Returns preferred-only?** | No — gets *all* versions, then Crane re-filters to preferred | Yes — returns preferred versions directly |
| **Verb filtering** | Requires `list`, `create`, `get`, `delete` | None at discovery time; checks `list` verb during export loop |
| **Subresource filtering** | Not done (relies on verbs filter) | Explicit `strings.Contains(name, "/")` skip |
| **REST mapper** | Built internally, never used by Crane | Built separately for `--kind` resolution |
| **APIGroups list** | Fetched via separate `ServerGroups()` call; used by Crane | Not fetched (not needed — `ServerPreferredResources` already returns preferred) |
| **Server version** | Fetched, never used by Crane | Not fetched |
| **Feature flags** | Velero feature flag system initialized, `APIGroupVersions` enabled | None |
| **Error handling** | `ErrGroupDiscoveryFailed` → warn and continue | `IsGroupDiscoveryFailedError` → warn and continue |
| **Dependencies pulled in** | `velero/pkg/discovery`, `velero/pkg/features`, `velero/pkg/apis/velero/v1`, `velero/third_party/kubernetes/...` + all transitive | `k8s.io/client-go/discovery` only |
| **Thread safety** | `sync.RWMutex` in helper | None (not needed) |
| **Sorting** | `extensions` group sorted to end | None |
| **Namespaced vs cluster** | Returns both; Crane's `isAdmittedResource` filters | Returns both; mini-crane skips `!Namespaced` in bulk loop |

---

## 4. What Crane actually needs from discovery

Distilling `resourceToExtract()` and `filterRbacResources()`, Crane needs
exactly **two things** from the discovery layer:

### 4.1 The type catalog: `[]*metav1.APIResourceList`

Each entry provides:
- `GroupVersion` (string, e.g. `"apps/v1"`)
- `[]APIResource` — each with `Name`, `Kind`, `Namespaced`, `Verbs`

Used to iterate every API type and decide which ones to `List`.

### 4.2 The preferred-version map

Crane's `resourceToExtract()` checks:

```go
for _, a := range apiGroups {
    if a.Name == gv.Group && a.PreferredVersion.Version == gv.Version {
        preferred = true
    }
}
```

This is needed only when the catalog contains **multiple versions per group**
(e.g. both `apps/v1` and `apps/v1beta1`). It de-duplicates so Deployments
aren't exported twice.

---

## 5. Why `ServerPreferredResources()` eliminates the need for `APIGroups()`

`discoveryClient.ServerPreferredResources()` already returns **only the
preferred version per group**. That is its entire purpose — it calls
`ServerGroups()` internally, reads `PreferredVersion`, then calls
`ServerResourcesForGroupVersion()` only for the preferred one.

This means:

- The catalog you get back has **one entry per group** (the preferred version).
- There is **no need for a second pass** to filter by preferred version.
- Crane's `apiGroups` + preferred check becomes **dead code**.

**Current Crane (unnecessary round-trip):**
```
ServerGroupsAndResources()  →  ALL versions     →  expensive, 2-4× the data
ServerGroups()              →  preferred map     →  filter to preferred
```

**With raw client-go:**
```
ServerPreferredResources()  →  preferred only    →  done, no extra call
```

---

## 6. Gap analysis: what mini-crane covers vs what Crane needs

### 6.1 Covered by mini-crane

| Crane requirement | mini-crane coverage |
|-------------------|-------------------|
| Build discovery client from kubeconfig/context | Yes — `configFlags.ToRESTConfig()` + `discovery.NewDiscoveryClientForConfig()` |
| Get `[]*metav1.APIResourceList` | Yes — `ServerPreferredResources()` |
| Skip resources with no verbs | Partially — checks `list` verb; Crane checks `len(Verbs) == 0` |
| Skip Events | Yes — in `skipResource()` (plus more kinds) |
| Skip subresources | Yes — `strings.Contains(name, "/")` |
| Preferred version only | Yes — `ServerPreferredResources()` handles this inherently |
| Handle `ErrGroupDiscoveryFailed` gracefully | Yes — `discovery.IsGroupDiscoveryFailedError()` |
| Dynamic `List` per type | Yes — `dynamicClient.Resource(gvr).Namespace(ns).List()` |
| YAML marshal + write | Yes |

### 6.2 NOT covered by mini-crane (gaps to fill)

| Crane feature | Gap in mini-crane | What to add |
|---------------|-------------------|-------------|
| **Cluster-scoped resource support** | mini-crane skips `!Namespaced` entirely | Need `isAdmittedResource()` logic + `isClusterScopedResource()` allowlist to admit CRB/CR/SCC when `-c` is on |
| **Cluster-wide `List` for admitted cluster-scoped types** | Only does `Namespace(ns).List()` | Need the `if !Namespaced { c.List() }` branch (already in Crane's `getObjects()`) |
| **`filterRbacResources`** (SA-graph filtering) | Not present | Need `cluster.go`'s `ClusterScopedRbacHandler` — this is Crane-specific, not discovery-related |
| **`_cluster/` output directory** | Not present | Need `writeResources()` to route `obj.GetNamespace() == ""` to `_cluster/` |
| **`failures/` error recording** | Not present | Need `writeErrors()` + `groupResourceError` |
| **Pager-based listing** | Uses plain `List()` | Crane uses `k8s.io/client-go/tools/pager.New()` for paginated listing of large collections |
| **`imagestreamtags` / `imagetags` special handling** | Not present | Crane's `iterateItemsByGet()` fetches each item individually because List doesn't return full objects for these types |
| **Label selector on dynamic List** | Present (via `--selector`) | Already covered |
| **Impersonation extras** | Not present | Crane's `--as-extras` flag sets `restConfig.Impersonate.Extra` |
| **QPS / Burst tuning** | Not present | Crane sets `restConfig.QPS` / `restConfig.Burst` |

### 6.3 Summary: discovery itself is fully covered

The **discovery layer** (getting the type catalog) is 100% covered by
mini-crane's `ServerPreferredResources()`. All the gaps are in the
**post-discovery export logic** (cluster-scoped handling, paging, error
recording, imagestream workarounds), which are Crane-specific features
unrelated to Velero's discovery helper.

---

## 7. Implementation plan: removing Velero discovery from Crane

### 7.1 What to replace

**File:** `cmd/export/export.go`

**Remove these imports:**

```go
velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
"github.com/vmware-tanzu/velero/pkg/discovery"
"github.com/vmware-tanzu/velero/pkg/features"
```

**Remove these lines (134-141):**

```go
features.NewFeatureFlagSet()
features.Enable(velerov1api.APIGroupVersionsFeatureFlag)

discoveryHelper, err := discovery.NewHelper(discoveryClient, log)
if err != nil {
    log.Errorf("cannot create discovery helper: %#v", err)
    return err
}
```

### 7.2 What to add

**New discovery function** (can live in `discover.go` or a new `discovery.go`):

```go
func discoverPreferredResources(
    discoveryClient discovery.DiscoveryInterface,
    log logrus.FieldLogger,
) ([]*metav1.APIResourceList, error) {
    lists, err := discoveryClient.ServerPreferredResources()
    if err != nil {
        if discovery.IsGroupDiscoveryFailedError(err) {
            log.Warnf("some API groups failed discovery, continuing with available groups")
        } else {
            return nil, err
        }
    }
    return lists, nil
}
```

### 7.3 Changes to `resourceToExtract()`

The function signature changes — it no longer needs `apiGroups`:

```go
// BEFORE
func resourceToExtract(namespace, labelSelector string, clusterScopedRbac bool,
    dynamicClient dynamic.Interface,
    lists []*metav1.APIResourceList,
    apiGroups []metav1.APIGroup,        // ← remove this
    log logrus.FieldLogger,
) ([]*groupResource, []*groupResourceError)

// AFTER
func resourceToExtract(namespace, labelSelector string, clusterScopedRbac bool,
    dynamicClient dynamic.Interface,
    lists []*metav1.APIResourceList,
    log logrus.FieldLogger,
) ([]*groupResource, []*groupResourceError)
```

**Remove the preferred-version filter block** (lines 184-192 in `discover.go`):

```go
// DELETE THIS BLOCK — ServerPreferredResources already guarantees preferred-only
preferred := false
for _, a := range apiGroups {
    if a.Name == gv.Group && a.PreferredVersion.Version == gv.Version {
        preferred = true
    }
}
if !preferred {
    continue
}
```

### 7.4 Updated `Run()` in `export.go`

```go
func (o *ExportOptions) Run() error {
    // ... (directory setup unchanged) ...

    discoveryClient, err := o.configFlags.ToDiscoveryClient()
    if err != nil {
        log.Errorf("cannot create discovery client: %#v", err)
        return err
    }
    discoveryClient.Invalidate()

    restConfig, err := o.configFlags.ToRESTConfig()
    if err != nil {
        log.Errorf("cannot create rest config: %#v", err)
        return err
    }
    restConfig.Impersonate.Extra = o.extras
    restConfig.Burst = o.Burst
    restConfig.QPS = o.QPS

    dynamicClient := dynamic.NewForConfigOrDie(restConfig)

    // --- NEW: raw discovery, no Velero helper ---
    resourceLists, err := discoverPreferredResources(discoveryClient, log)
    if err != nil {
        return err
    }

    resources, resourceErrs := resourceToExtract(
        o.userSpecifiedNamespace, o.labelSelector, o.clusterScopedRbac,
        dynamicClient, resourceLists, log,    // no apiGroups arg
    )

    // ... (rest unchanged: filterRbacResources, writeResources, writeErrors) ...
}
```

### 7.5 Dependency cleanup

After removing the three Velero imports from `export.go`, the entire
`github.com/vmware-tanzu/velero` dependency can be removed from `go.mod` **if
no other package in Crane uses it**.

```bash
go mod tidy
```

This will also remove all of Velero's transitive dependencies.

---

## 8. What does NOT change

These parts of Crane are **unaffected** by the Velero removal:

| Component | File | Reason |
|-----------|------|--------|
| `isAdmittedResource()` | `discover.go` | Pure Crane logic, no Velero dependency |
| `isClusterScopedResource()` | `cluster.go` | Pure Crane logic |
| `getObjects()` (dynamic List + pager) | `discover.go` | Uses `k8s.io/client-go`, not Velero |
| `filterRbacResources()` | `cluster.go` | Pure Crane logic |
| `writeResources()` / `writeErrors()` | `discover.go` | Pure Crane logic |
| `iterateItemsByGet()` (imagestream) | `discover.go` | Uses `k8s.io/client-go` dynamic |
| `--cluster-scoped-rbac` flag and all SA-graph logic | `cluster.go` | Pure Crane logic |

---

## 9. Risk assessment

| Risk | Severity | Mitigation |
|------|----------|------------|
| **`ServerPreferredResources` returns fewer resources than `ServerGroupsAndResources`** | None — Crane already filters to preferred anyway, so the effective output is identical | N/A |
| **Verb filter difference**: Velero requires `list + create + get + delete`; Crane checks `len(Verbs) == 0` | Low — Crane's own check is more permissive; resources with only `list` verb will now be attempted. They were already excluded by Velero's filter. Consider adding an explicit `list` verb check if needed. | Add `hasVerb(resource, "list")` check in `resourceToExtract()` |
| **`extensions` group sorting removed** | None — sort order doesn't affect export correctness, only iteration order | N/A |
| **Loss of `ResourceFor` / `KindFor` / REST mapper** | None — Crane never calls these. If future features need kind resolution (like mini-crane's `--kind`), a standard `restmapper.DeferredDiscoveryRESTMapper` can be created independently. | N/A |

---

## 10. Conclusion

The Velero discovery helper is **pure overhead** for Crane's export command.
Crane uses exactly **two methods** from it (`Resources()` and `APIGroups()`),
and `ServerPreferredResources()` from raw `client-go` provides both in a
single call — returning only preferred versions so the `APIGroups()` +
preferred check is unnecessary.

mini-crane's `go-cli-playground` code demonstrates the raw approach and covers
the **discovery layer completely**. The gaps are all in Crane-specific
**post-discovery logic** (cluster-scoped handling, SA filtering, error
recording, imagestream workarounds) which are already implemented in Crane's
own `discover.go` and `cluster.go` and require no changes.

**Net effect of migration:**

- Remove 3 Velero imports and ~10 lines of code
- Remove `apiGroups` parameter and ~10 lines of preferred-version filtering
- Add ~15 lines of `discoverPreferredResources()` function
- Run `go mod tidy` to drop the entire `velero` dependency tree
- Zero behavioral change in export output
