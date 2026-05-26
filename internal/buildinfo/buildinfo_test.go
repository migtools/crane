package buildinfo

import (
	"runtime/debug"
	"testing"
)

func TestReadKustomizeVersion_BuildInfoUnavailable(t *testing.T) {
	original := readBuildInfo
	defer func() { readBuildInfo = original }()

	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return nil, false
	}

	result := readKustomizeVersion()
	if result != "unknown" {
		t.Errorf("expected \"unknown\" when build info unavailable, got %q", result)
	}
}

func TestReadKustomizeVersion_DepNotFound(t *testing.T) {
	original := readBuildInfo
	defer func() { readBuildInfo = original }()

	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Deps: []*debug.Module{
				{Path: "github.com/some/other-dep", Version: "v1.0.0"},
			},
		}, true
	}

	result := readKustomizeVersion()
	if result != "unknown" {
		t.Errorf("expected \"unknown\" when dep not found, got %q", result)
	}
}

func TestReadKustomizeVersion_DepFound(t *testing.T) {
	original := readBuildInfo
	defer func() { readBuildInfo = original }()

	readBuildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{
			Deps: []*debug.Module{
				{Path: "github.com/some/other-dep", Version: "v1.0.0"},
				{Path: "sigs.k8s.io/kustomize/kustomize/v5", Version: "v5.8.1"},
			},
		}, true
	}

	result := readKustomizeVersion()
	if result != "v5.8.1" {
		t.Errorf("expected \"v5.8.1\", got %q", result)
	}
}
