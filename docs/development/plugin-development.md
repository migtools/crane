# Plugin Development

Crane uses a plugin system for transforming Kubernetes resources during migration. Plugins generate JSONPatch (RFC 6902) operations that are applied via Kustomize.

## How Plugins Work

1. Crane discovers plugin binaries in the plugin directory (`~/.local/share/crane/plugins/` by default)
2. During transform, each plugin receives a Kubernetes resource on stdin
3. The plugin analyzes the resource and returns a PluginResponse object on stdout, which contains JSONPatch operations and optional new resources
4. Crane writes the patches to the stage's `patches/` directory and new resources to the `new/` directory
5. During apply, the embedded kustomize engine applies the patches to the resources

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
  "spec": { "..." : "..." },
  "status": { "..." : "..." }
}
```

### Output (stdout)

Plugins return a PluginResponse object containing an optional array of RFC 6902 JSONPatch operations in the `patches` field. Additionally, plugins can optionally return entirely new resources in the `newResources` field to be added to the transformation pipeline.

```json
{
  "version": "v1",
  "isWhiteOut": false,
  "patches": [
    {"op": "remove", "path": "/metadata/uid"}
  ],
  "newResources": [
    {
      "apiVersion": "v1",
      "kind": "ConfigMap",
      "metadata": {"name": "generated-config", "namespace": "default"},
      "data": {"key": "value"}
    }
  ]
}
```

Return an empty array for `patches` if no transformations are needed. `newResources` is optional; if provided, each resource must contain a valid `kind`, `name`, and `apiVersion`.

## Writing a Plugin in Go

```go
package main

import (
    "encoding/json"
    "os"

    "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
    "github.com/evanphx/json-patch"
    cranelib "github.com/konveyor/crane-lib/transform"
)

func main() {
    var resource unstructured.Unstructured
    if err := json.NewDecoder(os.Stdin).Decode(&resource); err != nil {
        os.Exit(1)
    }

    // Build JSONPatch operations
    patchData := []byte(`[{"op": "add", "path": "/metadata/labels/migrated", "value": "true"}]`)
    patches, err := jsonpatch.DecodePatch(patchData)
    if err != nil {
        os.Exit(1)
    }

    response := cranelib.PluginResponse{
        Version: "v1",
        Patches: patches,
    }

    if err := json.NewEncoder(os.Stdout).Encode(response); err != nil {
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
# Simple plugin that returns patches
cat <<EOF
{
  "patches": [{"op": "add", "path": "/metadata/labels/environment", "value": "production"}]
}
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
crane transform 20_MyCustomPlugin
crane apply
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
2. **Defensive**: Check if fields and parent objects exist before modifying them
3. **Focused**: Each plugin should handle one concern
4. **Documented**: Include usage instructions and examples
5. **Tested**: Cover edge cases (missing fields, different resource types)
