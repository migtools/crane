# Crane 

## Intro
[Crane](https://konveyor.github.io/crane/overview/) is a migration tool under the [Konveyor](https://www.konveyor.io/) community that helps application owners migrate Kubernetes workloads and their state between clusters.

## YouTube Demo
[![Alt text](https://img.youtube.com/vi/PoSivlgVLf8/0.jpg)](https://www.youtube.com/watch?v=PoSivlgVLf8)

## Overview

Migrating an application between Kubernetes clusters may be more nuanced than one would imagine.  In an ideal situation, this would be as simple as applying the YAML manifests to the new cluster and adjusting DNS records to redirect external traffic, yet often there is more that is needed.  Below are a few of the common concerns that need to be addressed:
* _YAML manifests_ - do we have the original YAML manifests stored in version control or accessible so we can reapply to the new cluster?
* _Configuration Drift_ - if we do have the YAML manifests, do we have confidence they are still accurate and represent the application as it’s running in the cluster? Perhaps the application has been running for a period of time, been modified, and we no longer have confidence we can reproduce it exactly as it’s currently running.
* _State_ - we may need to address persisted state that has been generated inside of the cluster, either small elements of state such as generated certificates stored in a Secret, data stored in Custom Resources, or gigabytes of data in persistent volumes.  
* _Customizations needed for new environment_ - we may be migrating across cloud vendors or environments that require transformations to the applications so they run in the new environment.

Crane helps users do more than just handle a point in time migration of a workload, it is intended to help users adopt current best practices such as onboarding to GitOps by reconstructing redeployable YAML manifests from inspecting a running application.  The project is the result of several years of experience performing large-scale production Kubernetes migrations and addressing the lessons learned.

Crane follows the Unix philosophy of building small sharply focused tools that can be assembled in powerful ways.  It is designed with transparency and ease-of-diagnostics in mind. It drives migration through a pipeline of non-destructive tasks that output results to disk so the operation can be easily audited and versioned without impacting live workloads. The tasks can be run repeatedly and will output consistent results given the same inputs without side-effects on the system at large.

Crane is composed of several repositories:
* [konveyor/crane](https://github.com/konveyor/crane): (this repo) The command line tool that migrates applications to the terminal.
* [konveyor/crane-lib](https://github.com/konveyor/crane-lib): The brains behind Crane functionality responsible for transforming resources.
* [konveyor/crane-plugins](https://github.com/konveyor/crane-plugins): Collection of plugins from the Konveyor community based on experience from performing Kube migrations.
* [konveyor/crane-plugin-openshift](https://github.com/konveyor/crane-plugin-openshift): An optional plugin specifically tailored to manage OpenShift migration workloads and an example of a repeatable best-practice.
* [backube/pvc-transfer](https://github.com/backube/pvc-transfer): The library that powers the Persistent Volume migration ability, shared with the [VolSync](https://volsync.readthedocs.io/en/stable/index.html) project.  State migration of Persistent Volumes is handled by rsync allowing storage migrations between different storage classes.  
* [konveyor/crane-runner](https://github.com/konveyor/crane-runner): A collection of resources showing how to leverage Tekton to build migration workflows with Crane
* [konveyor/crane-ui-plugin](https://github.com/konveyor/crane-ui-plugin): A dynamic UI plugin for the [openshift/console](https://github.com/openshift/console)
* [konveyor/mtrho-operator](https://github.com/konveyor/mtrho-operator): An Operator which deploys Crane in an opinionated manner leveraging Tekton for migrating applications

How does it work? Crane works by:
1) Inspecting a running application and exporting all associated resources
2) Leveraging a library of plugins to aid in transforming the exported manifests to yield redeployable manifests
3) Applying the transformed manifests into the destination cluster
4) Optionally orchestrating persistent state migrations

## Install

  * Obtain the `crane` binary from either:
    * Download a prebuilt release from https://github.com/konveyor/crane/releases
    * Clone this repo and build the crane binary via: `go build -o crane main.go`
  * Install the `crane` binary in your `$PATH`

## Usage Example
1. `$ kubectl create namespace guestbook`
1. `$ kubectl --namespace guestbook apply -k github.com/konveyor/crane-runner/examples/resources/guestbook`
1. `$ crane export -n guestbook`
  * Discovers and exports all resources in the 'guestbook' namespace
  * A directory 'export/resources/guestbook' is populated with the raw YAML content of each exported resource 
  * Example:
    * `$ cat export/resources/guestbook/Secret_guestbook_builder-dockercfg-5ztj6.yaml`

            kind: Secret
            apiVersion: v1
            metadata:
              name: builder-dockercfg-5ztj6
              namespace: guestbook
              resourceVersion: "3213488"
              uid: 8fb75dcd-68b2-4939-bfb9-1c8241a7b146
              ... 
            data:
              .dockercfg: < ...SNIP.... >

4. `$ crane transform`
  * A directory 'transform/resources/guestbook' is populated with 'transforms' which are JSONPatch content to be applied to each of the YAML files produced by the prior 'export'
  * Example: 
    * `$ cat transform/resources/guestbook/transform-Secret_guestbook_builder-dockercfg-5ztj6.yaml` 

            [{"op":"remove","path":"/metadata/uid"},{"op":"remove","path":"/metadata/resourceVersion"},{"op":"remove","path":"/metadata/creationTimestamp"}]
          
      * We can see that this transform is part of the standard Kubernetes plugin included with Crane and is configured to remove several fields from the 'metadata' section.

5. `$ crane apply`
  * A directory `output/resources/guestbook/` is populated with redeployable YAML content, this is the result of the 'export' content modified via the transforms produced by 'transform'.
  * Example:
    * `$ cat output/resources/guestbook/Secret_guestbook_builder-dockercfg-5ztj6.yaml`

            kind: Secret
            apiVersion: v1
            metadata:
              name: builder-dockercfg-5ztj6
              namespace: guestbook
              ... 
            data:
              .dockercfg: < ...SNIP.... > 

    * Note, that the fields 'metadata.uid', 'metadata.resourceVersion', and 'metadata.creationTimestamp' are removed from this YAML.
6. The content in the  `output/resources/guestbook` directory is now ready to be used as needed, this could be redeployed to a new cluster or checked into Git to be leveraged with a GitOps solution.

## Further Examples

Please see [konveyor/crane-runner/main/examples](https://github.com/konveyor/crane-runner/tree/main/examples#readme) for further scenarios to explore what can be done with Crane + Tekton for migrating applications. 

## Known issues

- v0.0.2 (alpha1)
  - The new-namespace optional arg (and associated functionality) in the
    built-in kubernetes plugin is incomplete. `metadata.namespace` will be
    modified, but other required changes will not be made. It will be
    removed from this plugin in the next release and expanded
    functionality will most likely be added via a separate (optional)
    plugin.