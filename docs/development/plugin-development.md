# Plugin Development

Crane uses a plugin system for transforming Kubernetes resources during migration. Plugins generate JSONPatch (RFC 6902) operations that are applied via Kustomize.

## How Plugins Work

1. Crane discovers plugin binaries in the plugin directory (`~/.local/share/crane/plugins/` by default)
2. During transform, each plugin receives a Kubernetes resource on stdin
3. The plugin analyzes the resource and returns JSONPatch operations on stdout
4. Crane writes the patches to the stage's `patches/` directory
5. During apply, `kubectl kustomize` applies the patches to the resources

## Plugin Interface

A plugin is any executable binary that:

- Reads a Kubernetes resource (JSON) from **stdin**
- Writes JSONPatch operations (JSON array) to **stdout**
- Returns exit code `0` on success, non-zero on error
- Writes error messages to **stderr**

### Input (stdin)

A single Kubernetes resource in JSON format:

```json
{
  "apiVersion": "apps/v1",
  "kind": "Deployment",
  "metadata": {
    "name": "my-app",
    "namespace": "default",
    "uid": "abc-123",
    "resourceVersion": "12345"
  },
  "spec": { ... },
  "status": { ... }
}
```

### Output (stdout)

A JSON array of RFC 6902 JSONPatch operations:

```json
[
  {"op": "remove", "path": "/metadata/uid"},
  {"op": "remove", "path": "/metadata/resourceVersion"},
  {"op": "remove", "path": "/status"},
  {"op": "add", "path": "/metadata/labels/migrated", "value": "true"}
]
```

Return an empty array `[]` if no transformations are needed.

## Writing a Plugin in Go

```go
package main

import (
    "encoding/json"
    "fmt"
    "os"

    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type PatchOp struct {
    Op    string      `json:"op"`
    Path  string      `json:"path"`
    Value interface{} `json:"value,omitempty"`
}

func main() {
    var resource unstructured.Unstructured
    if err := json.NewDecoder(os.Stdin).Decode(&resource); err != nil {
        fmt.Fprintf(os.Stderr, "failed to decode resource: %v\n", err)
        os.Exit(1)
    }

    var patches []PatchOp

    // Example: add a migration label
    patches = append(patches, PatchOp{
        Op:    "add",
        Path:  "/metadata/labels/migrated-by",
        Value: "my-plugin",
    })

    // Example: remove a specific annotation
    annotations := resource.GetAnnotations()
    if _, ok := annotations["source-cluster-only"]; ok {
        patches = append(patches, PatchOp{
            Op:   "remove",
            Path: "/metadata/annotations/source-cluster-only",
        })
    }

    if err := json.NewEncoder(os.Stdout).Encode(patches); err != nil {
        fmt.Fprintf(os.Stderr, "failed to encode patches: %v\n", err)
        os.Exit(1)
    }
}
```

Build and install:

```bash
go build -o ~/.local/share/crane/plugins/MyCustomPlugin
```

## Writing a Plugin in Bash

```bash
#!/bin/bash
# Simple plugin that adds a label to all resources

cat <<EOF
[
  {"op": "add", "path": "/metadata/labels/environment", "value": "production"}
]
EOF
```

Install:

```bash
chmod +x my-plugin.sh
cp my-plugin.sh ~/.local/share/crane/plugins/MyCustomPlugin
```

## Plugin Naming and Stages

Plugin names correspond to stage directory names. When Crane encounters a stage like `20_MyCustomPlugin`, it looks for a plugin binary named `MyCustomPlugin` in the plugin directory.

Stage naming convention:
- `<priority>_<PluginName>Plugin` — Plugin-based stage (must have matching plugin)
- `<priority>_<CustomName>` — Pass-through stage (no plugin needed)

## Testing Plugins

### Manual Testing

```bash
# Test with a sample resource
cat sample-deployment.json | ./MyCustomPlugin

# Test in a full pipeline
crane transform --stage 20_MyCustomPlugin
kubectl kustomize transform/20_MyCustomPlugin/
```

### Unit Testing

Test your plugin with various resource types to ensure it handles edge cases:

- Resources without the fields you're trying to modify
- Resources with deeply nested structures
- Cluster-scoped resources (no namespace)
- Custom resources (CRDs)

## Plugin Priority

Plugins are executed in stage order (by the numeric prefix). Lower numbers run first:

| Priority | Typical Use |
|----------|------------|
| 10 | Core cleanup (KubernetesPlugin) |
| 20 | Platform-specific (OpenshiftPlugin) |
| 30-40 | Security, networking |
| 50-70 | Storage, images |
| 80-90 | Custom application transformations |

## Existing Plugins

- **KubernetesPlugin** (built-in via crane-lib): Removes server-managed fields
- [crane-plugins](https://github.com/konveyor/crane-plugins): Community plugins
- [crane-plugin-openshift](https://github.com/konveyor/crane-plugin-openshift): OpenShift-specific transformations

## Best Practices

1. **Idempotent**: Running the plugin multiple times should produce the same result
2. **Defensive**: Check if fields exist before removing them
3. **Focused**: Each plugin should handle one concern
4. **Documented**: Include usage instructions and examples
5. **Tested**: Cover edge cases (missing fields, different resource types)
