# Plugins

Crane uses plugins to transform Kubernetes resources during migration. Plugins generate JSONPatch operations that clean, modify, or adapt resources for the target cluster.

## Built-in Plugin

### KubernetesPlugin

The built-in Kubernetes plugin (from [crane-lib](https://github.com/konveyor/crane-lib)) automatically removes server-managed fields that would conflict when applying to a new cluster:

- `metadata.uid`
- `metadata.resourceVersion`
- `metadata.creationTimestamp`
- `metadata.managedFields`
- `status`

This plugin runs as the default first stage (`10_KubernetesPlugin`) and is always available without additional installation.

## Community Plugins

### crane-plugins

The [konveyor/crane-plugins](https://github.com/konveyor/crane-plugins) repository contains community-contributed plugins based on experience from real-world Kubernetes migrations.

### OpenShift Plugin

The [konveyor/crane-plugin-openshift](https://github.com/konveyor/crane-plugin-openshift) plugin handles OpenShift-specific migration concerns:

- Route transformations
- ImageStream references
- SecurityContextConstraints adjustments
- DeploymentConfig to Deployment conversion

Install:

```bash
# Download to the default plugin directory
curl -L <release-url> -o ~/.local/share/crane/plugins/OpenshiftPlugin
chmod +x ~/.local/share/crane/plugins/OpenshiftPlugin
```

Use in a transform stage:

```bash
crane transform --stage 20_OpenshiftPlugin
```

## Managing Plugins

### Plugin Directory

Plugins are discovered from `~/.local/share/crane/plugins/` by default. Override with `--plugin-dir`:

```bash
crane transform --plugin-dir /path/to/plugins
```

### Listing Available Plugins

```bash
crane transform list-plugins
```

### Skipping Plugins

Skip specific plugins during transform:

```bash
crane transform --skip-plugins OpenshiftPlugin
```

### Plugin Optional Flags

Pass configuration to plugins:

```bash
crane transform --optional-flags '{"new-namespace": "production"}'
```

## Writing Custom Plugins

Plugins are executable binaries that read a Kubernetes resource from stdin and write JSONPatch operations to stdout. See the [Plugin Development Guide](./development/plugin-development.md) for details.
