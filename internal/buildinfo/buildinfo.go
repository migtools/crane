package buildinfo

import (
	"runtime/debug"

	cranelibversion "github.com/konveyor/crane-lib/version"
)

var (
	Version string = "v0.0.6"

	CranelibVersion string = cranelibversion.Version

	KustomizeVersion string = readKustomizeVersion()
)

func readKustomizeVersion() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == "sigs.k8s.io/kustomize/api" {
			return dep.Version
		}
	}
	return "unknown"
}
