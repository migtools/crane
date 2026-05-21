package buildinfo

import (
	cranelibversion "github.com/konveyor/crane-lib/version"
)

var (
	// Version and BuildCommit can be overridden at build time with:
	// go build -ldflags="-X github.com/konveyor/crane/internal/buildinfo.Version=<version> -X github.com/konveyor/crane/internal/buildinfo.BuildCommit=$(git rev-parse HEAD)"
	Version string = "v0.0.6"

	CranelibVersion string = cranelibversion.Version
	BuildCommit     string = ""
)
