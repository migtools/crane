# Crane 2.0

Crane 2.0 cli

TODO: Add section explaining what this is

## Getting Started

Crane is available via source, so it must be built. A user is expected to have
a usable go build environment on their workstation.

First, checkout crane into your `GOPATH`:

```
mkdir -p $GOPATH/konveyor
git clone https://github.com/konveyor/crane.git $GOPATH/konveyor/crane
```

The following command will build the crane binary, which can be moved to a user's
PATH for regular usage:

`go build -o crane main.go`

## Known issues

- v0.0.2 (alpha1)
  - The new-namespace optional arg (and associated functionality) in the
    built-in kubernetes plugin is incomplete. `metadata.namespace` will be
    modified, but other required changes will not be made. It will be
    removed from this plugin in the next release and expanded
    functionality will most likely be added via a separate (optional)
    plugin.

## Usage

TODO
