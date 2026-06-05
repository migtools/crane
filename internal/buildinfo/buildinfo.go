package buildinfo

import (
	"runtime/debug"

	cranelibversion "github.com/konveyor/crane-lib/version"
	// Blank import to keep the kustomize CLI module in the dependency graph
	// so debug.ReadBuildInfo() can report its version.
	_ "sigs.k8s.io/kustomize/kustomize/v5/commands/version"
)

var (
	// Version and BuildCommit can be overridden at build time with:
	// go build -ldflags="-X github.com/konveyor/crane/internal/buildinfo.Version=<version> -X github.com/konveyor/crane/internal/buildinfo.BuildCommit=$(git rev-parse HEAD)"
	Version string = "v0.0.6"

	CranelibVersion string = cranelibversion.Version
	BuildCommit     string = "main"

	KustomizeVersion string = readKustomizeVersion()
)

var readBuildInfo = debug.ReadBuildInfo

func readKustomizeVersion() string {
	info, ok := readBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == "sigs.k8s.io/kustomize/kustomize/v5" {
			return dep.Version
		}
	}
	return "unknown"
}
