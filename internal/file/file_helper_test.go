package file_test

import (
	"testing"

	"github.com/konveyor/crane/internal/file"
)

func TestGetWhiteOutFilePath(t *testing.T) {
	cases := []struct {
		Name        string
		Filepath    string
		Dir         string
		ResourceDir string
		Expected    string
	}{
		{
			Name:        "test whiteout file creation",
			Filepath:    "/fully/qualified/resources/ns/path-test",
			Dir:         "/fully/qualified/transform",
			ResourceDir: "/fully/qualified/resources",
			Expected:    "/fully/qualified/transform/ns/.wh.path-test",
		},
	}

	for _, test := range cases {
		opts := file.PathOpts{
			TransformDir: test.Dir,
			ExportDir:    test.ResourceDir,
		}
		if actual := opts.GetWhiteOutFilePath(test.Filepath); actual != test.Expected {
			t.Errorf("actual: %v did not match expected: %v", actual, test.Expected)
		}
	}
}

func TestGetTransformPath(t *testing.T) {
	cases := []struct {
		Name        string
		Filepath    string
		Dir         string
		ResourceDir string
		Expected    string
	}{
		{
			Name:        "test transform file creation",
			Filepath:    "/fully/qualified/ns/path-test",
			Dir:         "/fully/qualified/transform",
			ResourceDir: "/fully/qualified",
			Expected:    "/fully/qualified/transform/ns/transform-path-test",
		},
	}
	for _, test := range cases {
		opts := file.PathOpts{
			TransformDir: test.Dir,
			ExportDir:    test.ResourceDir,
		}
		if actual := opts.GetTransformPath(test.Filepath); actual != test.Expected {
			t.Errorf("actual: %v did not match expected: %v", actual, test.Expected)
		}
	}

}

func TestGetOutputFilePath(t *testing.T) {
	cases := []struct {
		Name        string
		Filepath    string
		Dir         string
		ResourceDir string
		Expected    string
	}{
		{
			Name:        "test transform file creation",
			Filepath:    "/fully/qualified/ns/path-test",
			Dir:         "/fully/qualified/output",
			ResourceDir: "/fully/qualified",
			Expected:    "/fully/qualified/output/ns/path-test",
		},
	}
	for _, test := range cases {
		opts := file.PathOpts{
			OutputDir: test.Dir,
			ExportDir: test.ResourceDir,
		}
		if actual := opts.GetOutputFilePath(test.Filepath); actual != test.Expected {
			t.Errorf("actual: %v did not match expected: %v", actual, test.Expected)
		}
	}
}

// Tests for Kustomize Layout Path Helpers

func TestKustomizeLayoutPaths(t *testing.T) {
	opts := file.PathOpts{
		TransformDir: "/transform",
		ExportDir:    "/export",
		OutputDir:    "/output",
	}

	t.Run("GetStageDir", func(t *testing.T) {
		result := opts.GetStageDir("10_kubernetes")
		expected := "/transform/10_kubernetes"
		if result != expected {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})

	t.Run("GetResourcesDir", func(t *testing.T) {
		result := opts.GetResourcesDir("10_kubernetes")
		expected := "/transform/10_kubernetes/resources"
		if result != expected {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})

	t.Run("GetPatchesDir", func(t *testing.T) {
		result := opts.GetPatchesDir("10_kubernetes")
		expected := "/transform/10_kubernetes/patches"
		if result != expected {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})

	t.Run("GetReportsDir", func(t *testing.T) {
		result := opts.GetReportsDir("10_kubernetes")
		expected := "/transform/10_kubernetes/reports"
		if result != expected {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})

	t.Run("GetWhiteoutsDir", func(t *testing.T) {
		result := opts.GetWhiteoutsDir("10_kubernetes")
		expected := "/transform/10_kubernetes/whiteouts"
		if result != expected {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})

	t.Run("GetKustomizationPath", func(t *testing.T) {
		result := opts.GetKustomizationPath("10_kubernetes")
		expected := "/transform/10_kubernetes/kustomization.yaml"
		if result != expected {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})

	t.Run("GetMetadataPath", func(t *testing.T) {
		result := opts.GetMetadataPath("10_kubernetes")
		expected := "/transform/10_kubernetes/.crane-metadata.json"
		if result != expected {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})

	t.Run("GetResourceTypeFilePath", func(t *testing.T) {
		result := opts.GetResourceTypeFilePath("10_kubernetes", "deployment.yaml")
		expected := "/transform/10_kubernetes/resources/deployment.yaml"
		if result != expected {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})

	t.Run("GetPatchFilePath", func(t *testing.T) {
		result := opts.GetPatchFilePath("10_kubernetes", "default--apps-v1--Deployment--nginx.patch.yaml")
		expected := "/transform/10_kubernetes/patches/default--apps-v1--Deployment--nginx.patch.yaml"
		if result != expected {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})

	t.Run("GetWhiteoutReportPath", func(t *testing.T) {
		result := opts.GetWhiteoutReportPath("10_kubernetes")
		expected := "/transform/10_kubernetes/whiteouts/whiteouts.json"
		if result != expected {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})

	t.Run("GetIgnoredPatchReportPath", func(t *testing.T) {
		result := opts.GetIgnoredPatchReportPath("10_kubernetes")
		expected := "/transform/10_kubernetes/reports/ignored-patches.json"
		if result != expected {
			t.Errorf("expected %v, got %v", expected, result)
		}
	})
}

func TestMultiStagePaths(t *testing.T) {
	opts := file.PathOpts{
		TransformDir: "transform",
		ExportDir:    "export",
		OutputDir:    "output",
	}

	stages := []struct {
		name     string
		expected string
	}{
		{"10_kubernetes", "transform/10_kubernetes"},
		{"20_openshift", "transform/20_openshift"},
		{"30_imagestream", "transform/30_imagestream"},
	}

	for _, stage := range stages {
		t.Run("Stage_"+stage.name, func(t *testing.T) {
			stageDir := opts.GetStageDir(stage.name)
			if stageDir != stage.expected {
				t.Errorf("expected %v, got %v", stage.expected, stageDir)
			}

			resourcesDir := opts.GetResourcesDir(stage.name)
			expectedResources := stage.expected + "/resources"
			if resourcesDir != expectedResources {
				t.Errorf("expected %v, got %v", expectedResources, resourcesDir)
			}

			kustomizationPath := opts.GetKustomizationPath(stage.name)
			expectedKustomization := stage.expected + "/kustomization.yaml"
			if kustomizationPath != expectedKustomization {
				t.Errorf("expected %v, got %v", expectedKustomization, kustomizationPath)
			}
		})
	}
}
