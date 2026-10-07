# MTC resource versioning behavior and Crane alignment options

This document describes how **Migration Toolkit for Containers (MTC)** behaves with respect to **Kubernetes API versions** and **custom resources (CRDs)** when migrating between clusters, without walking through source code. It then outlines **two practical ways** the **Crane CLI** could expose similar checks, given that Crane’s `export` command only sees the source cluster today.

The themes match [migtools/crane#185](https://github.com/migtools/crane/issues/185) (explore and document MTC logic for resources versioning).

---

## 1. Does MTC export multiple API versions of the same logical resource?

**Short answer: effectively no**, for the MTC stack that is commonly shipped today (mig-controller with **Velero 1.7.x**).

**What “export” means here** is whatever ends up in the Velero backup tarball from the source cluster: each object is stored with the representation the backup pipeline obtained from the API.

**How Velero backs up a type**

Velero discovers resources through the Kubernetes discovery API and, for each **logical resource** (same group + plural resource name, such as `deployments` in the `apps` API group), it works with a **single preferred API version** for that resource when it collects and backs up items. It does **not** iterate over **every** API version the apiserver still serves (for example `apps/v1`, `apps/v1beta1`, `apps/v1beta2`) and write separate copies of the same workload under each version. What you typically get is **one** `apiVersion` per object—the one associated with the **preferred** list path the server and Velero use for that kind.

**Newer Velero versions** can optionally record **multiple API group versions** when a specific feature flag is enabled. That path is **not** what the MTC line tied to Velero 1.7.x relies on by default. So for **“what MTC users run today,”** parity means: **one preferred representation per logical resource**, not a multi-version fan-out in the backup.

**How mig-controller’s own “GVK warning” logic thinks about versions**

When the controller compares what exists on the source versus the destination, it also bases that comparison on **preferred** discovery information on the source (what the apiserver advertises as the preferred way to list namespaced types), not on an exhaustive enumeration of every legacy version still available. So both **backup content** and **compatibility warnings** are aligned with **preferred** discovery, not “all served versions.”

**Takeaway for Crane (#185, bullet 1)**

MTC does **not** implement “export every served API version of the same resource.” Crane does not need to mimic a non-existent multi-version export to match MTC; documenting and using **server-preferred** discovery and manifests is the right mental model.

---

## 2. Does MTC explore supported versions on both clusters and automatically pick a matching version?

**Short answer: it compares discovery, but matching is strict—not semantic and not automatic conversion.**

**What gets compared**

- On the **source**, the controller considers **preferred** namespaced API resources (after filtering out subresources, types you cannot list, and similar noise).
- On the **destination**, it considers **namespaced** resources as exposed by discovery for that cluster (again in a preferred-oriented view appropriate to the client API).

**How a “match” is decided**

The comparison is **not** “find any version on the destination that can accept the same object.” Instead, it works roughly like this:

1. Take a **source** discovery slice keyed by a **full group/version string** (for example `apps/v1`).
2. Look for a **destination** discovery slice with the **exact same** group/version string.
3. Within that slice, check whether the **resource plural name** (for example `deployments`) appears on both sides.

If the destination does **not** expose that **same** `group/version` row, the controller does **not** try to map `apps/v1beta1` on the source to `apps/v1` on the destination, or rewrite manifests. There is **no** built-in “upgrade `apiVersion` for you” step in this path.

**Cohabitating resources (deployments, daemonsets, replicasets, network policies, events)**

Historically the same **logical** kind could appear under more than one API group (for example `extensions` vs `apps`). The controller carries a small, fixed list of such **cohabitating** resources. That logic **deduplicates** reporting so the same logical kind is not counted twice when both groups still appear in discovery. It does **not** mean “treat different API versions as interchangeable” or “pick the best version on the target.”

**CRDs (CustomResourceDefinitions)**

There is a **separate** branch for when the **CRD API itself** (`apiextensions.k8s.io`) is exposed differently between clusters (for example different eras of OpenShift or Kubernetes). If that situation is detected, the controller can narrow the comparison to API groups that are actually backed by CRDs installed on each cluster and surface mismatches there as well. Again, this is **detection and reporting**, not automatic schema or version rewriting for custom resources.

**What the user sees**

When mismatches are found in namespaces that participate in the migration plan, the **MigPlan** status is updated with **incompatible** namespace details and a **warning**-style condition (for example that some group-version-kinds are incompatible with the destination). That is **visibility** for planners and operators; it is **not** a guarantee that migration is blocked, and it is **not** automatic remediation.

**Takeaway for Crane (#185, bullet 2)**

MTC **discovers** and **flags** strict discovery mismatches. It does **not** automatically **choose** a different API version on the target when preferred rows differ. Any Crane feature that claims “MTC-like” behavior should mirror that: **report** incompatibility clearly, do not imply silent cross-version matching unless you add an explicit, separate feature.

---

## 3. Minimum resource-versioning logic for Crane that would satisfy MTC users

The goal is **behavioral alignment** with what MTC actually does, not with an imagined multi-version exporter or automatic conversion engine.


| Theme                         | MTC behavior                                                                                 | Practical Crane minimum                                                                                                                                                                                                                                                      |
| ----------------------------- | -------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **What gets exported**        | One preferred representation per type in the backup path                                     | Keep using **preferred** discovery for listing on export; document that YAML reflects **server-preferred** `apiVersion` where the apiserver returns it.                                                                                                                      |
| **Cross-cluster checks**      | Strict **group/version string** match plus **resource plural** name; warnings on **MigPlan** | Offer a **read-only check** (see options below) that compares **source** and **target** discovery in the same strict way **or** scans **exported YAML** for `apiVersion`/`kind` and checks those against **target** discovery. Emit a **report** (warn), not silent failure. |
| **Cohabitating / moved APIs** | Dedupe known overlapping groups for the same logical kind                                    | If Crane implements a compare step, **reuse the same dedupe idea** for those kinds so you do not flood users with duplicate warnings.                                                                                                                                        |
| **CRDs**                      | Extra attention when the **CRD API** or CRD-backed groups differ                             | Optional: mirror the intent of “CRD API skew” and “custom group missing on target” as **warnings** in the report.                                                                                                                                                            |
| **Apply on target**           | Velero restore / API server validation decide what is accepted                               | Crane **transform/apply** stays aligned with normal `**kubectl apply`** semantics; no obligation to rewrite `apiVersion` unless you add an explicit opt-in tool.                                                                                                             |
| **Future Velero**             | Optional multi-version backup when enabled in newer Velero                                   | Document two tiers in #185-style notes: **MTC + Velero 1.7 line** vs **newer Velero with optional multi-version backup**.                                                                                                                                                    |


---

## 4. Crane today: why this does not belong in `export` alone

- `**crane export`** is intentionally scoped to a **single cluster** (the source). It has **no target cluster context**, so it cannot run a source-vs-target discovery comparison by itself.
- `**crane transform`** and `**crane apply`** today operate on **directories on disk** (export output, transform output). They do **not** require a kubeconfig. That keeps the pipeline **offline-friendly** and easy to run where no cluster is available.

Therefore, target-aware versioning checks need either **new flags/commands** or a **documented external** workflow—not silent behavior inside export alone.

---

## 5. Option A : dedicated `crane validate` (or equivalent)

**Idea:** Add a **read-only** command whose purpose is to answer: “Given this bundle of manifests (and optionally live source namespaces), does the **target** cluster expose the same strict discovery rows we care about?”

**Typical inputs**

- Path to exported (and optionally transformed) YAML—e.g. `**--export-dir`**, or later-stage dirs if you want to validate post-transform output.
- **Target** cluster access: standard `**--kubeconfig`** / `**--context`** (and namespace if you scope checks).

**Behavior (can ship in phases)**

1. **Filesystem-first:** Walk YAML documents, collect distinct `**apiVersion`** + `**kind`** (and namespaces from metadata). Resolve or map these to resource plurals where possible using **target** discovery. Flag combinations that **do not** appear on the target under the **same strict group/version + resource name** rules MTC uses.
2. **Optional source-live mode (closer to mig-controller):** With **source** cluster credentials, optionally list objects in selected namespaces and cross-check **in-use** GVRs against the target the same way, for parity with MigPlan’s “something in this namespace uses an incompatible GVK” story.

**Pros**

- Clear separation: **validate** = cluster + report; **transform** = offline plugins.
- Fits CI: artifact tarball + target kubeconfig, no need to run transform.
- Aligns with existing “validate before apply” documentation you may already maintain.

**Cons**

- Another command to discover, document, and test.

---

## 6. Option B: optional **target** integration on `**crane transform`**

**Idea:** Extend `**crane transform`** with an **optional** path—e.g. `**--target-kubeconfig`** / `**--target-context`**—so that after (or before) running plugins, Crane runs the same discovery comparison against the target and prints suggestions.

**Variant:** A **subcommand** such as `**crane transform validate-target`** so default `**crane transform`** stays **fully offline** unless the user opts in.

**Pros**

- Single command for users who always transform immediately and want a **combined** “prepare + check” step.

**Cons**

- **Couples** the offline transform pipeline to **cluster availability** if the default flags pull in the network.
- Harder to reason about in automation unless you split defaults vs optional flags carefully.
- Transform plugins do not inherently need a cluster; adding a required cluster path to the default command muddies the product story.

**Recommendation if you choose B:** implement the **same library** as Option A under the hood, and expose it only via an **explicit flag or subcommand** so the default `**transform`** behavior remains **file-only**.

---

## 7. Other approaches (brief)



- **Apply-time only:** Rely on `**kubectl apply --dry-run=server`** on the target. That validates **concrete manifests** late in the pipeline; it does **not** replace an early **discovery-level** report like MigPlan’s incompatible GVK warning. Best used **together** with Option A or B, not as the only signal.

---

## 8. Summary

- MTC **does not** multi-export every served API version for the same resource in the Velero 1.7-era stack; it follows **preferred** discovery and **preferred** backup paths.
- MTC **does** compare source and destination discovery in a **strict** way and **warns** via **MigPlan**; it does **not** auto-pick alternate API versions on the target.
- Crane should align exports and docs with **preferred** representations and add **target-aware** checks via **Option A** (dedicated validate command, recommended) and/or **Option B** (explicit opt-in on transform), keeping default **transform** offline unless you deliberately add an opt-in subcommand or flag.

