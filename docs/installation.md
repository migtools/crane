# Installation

## Prerequisites

- **Go** 1.21+ (only required if building from source)
- **kubectl** — required for `crane apply` (uses `kubectl kustomize`) and `crane validate`
- **Docker** (optional) — for container-based workflows

## Install from Release

Download a prebuilt binary from the [releases page](https://github.com/konveyor/crane/releases):

```bash
# Download the latest release (adjust OS/arch as needed)
curl -L https://github.com/konveyor/crane/releases/latest/download/crane-linux-amd64 -o crane
chmod +x crane
sudo mv crane /usr/local/bin/
```

## Build from Source

```bash
git clone https://github.com/konveyor/crane.git
cd crane
go build -o crane main.go
sudo mv crane /usr/local/bin/
```

## Verify Installation

```bash
crane version
```

You should see the Crane version output.

## Quick Test

Verify Crane works with an accessible cluster:

```bash
# Export resources from a namespace
crane export -n default --export-dir /tmp/crane-test

# Check exported resources
ls /tmp/crane-test/resources/default/
```

If you see YAML files for resources in the `default` namespace, Crane is working correctly.

## Next Steps

- [Quickstart Tutorial](./quickstart-tutorial.md) — Run your first migration in under 10 minutes
- [Command Reference](./README.md#command-reference) — Detailed documentation for each command
