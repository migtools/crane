package file_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/konveyor/crane/internal/file"
	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func createTestDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "crane-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatalf("failed to create parent dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}
}

func TestReadFilesWithLogger_UsesProvidedLogger(t *testing.T) {
	dir := createTestDir(t)
	validYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: logger-test-cm
  namespace: default
`
	writeFile(t, filepath.Join(dir, "cm.yaml"), validYAML)

	log := logrus.New()
	files, err := file.ReadFilesWithLogger(context.TODO(), dir, log)
	if err != nil {
		t.Fatalf("ReadFilesWithLogger: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Unstructured.GetName() != "logger-test-cm" {
		t.Errorf("name = %q, want %q", files[0].Unstructured.GetName(), "logger-test-cm")
	}
}

func TestReadFilesValidResource(t *testing.T) {
	dir := createTestDir(t)
	validYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: default
data:
  key: value
`
	writeFile(t, filepath.Join(dir, "cm.yaml"), validYAML)

	files, err := file.ReadFiles(context.TODO(), dir)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if files[0].Unstructured.GetName() != "test-cm" {
		t.Errorf("expected resource name 'test-cm', got %q", files[0].Unstructured.GetName())
	}
}

func TestReadFilesInvalidContent(t *testing.T) {
	cases := []struct {
		name     string
		filename string
		content  string
	}{
		{"empty file", "empty.yaml", ""},
		{"null YAML", "null.yaml", "null"},
		{"invalid YAML syntax", "bad.yaml", "this is not yaml {{{"},
		{"YAML missing Kind", "nokind.yaml", "foo: bar"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := createTestDir(t)
			writeFile(t, filepath.Join(dir, tc.filename), tc.content)

			_, err := file.ReadFiles(context.TODO(), dir)
			if err == nil {
				t.Fatalf("expected error for %s, got nil", tc.name)
			}
			if !strings.Contains(err.Error(), tc.filename) {
				t.Errorf("error should contain file name, got: %v", err)
			}
			if !strings.Contains(err.Error(), "is not a valid Kubernetes resource") {
				t.Errorf("error should contain descriptive message, got: %v", err)
			}
		})
	}
}

func TestReadFilesNestedBadFile(t *testing.T) {
	dir := createTestDir(t)
	writeFile(t, filepath.Join(dir, "deep", "nested", "bad.yaml"), "null")

	_, err := file.ReadFiles(context.TODO(), dir)
	if err == nil {
		t.Fatal("expected error for nested bad file, got nil")
	}
	if !strings.Contains(err.Error(), filepath.Join("deep", "nested", "bad.yaml")) {
		t.Errorf("error should contain full nested path, got: %v", err)
	}
}

func TestReadFilesSkipsFailuresDir(t *testing.T) {
	dir := createTestDir(t)
	validYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: default
`
	writeFile(t, filepath.Join(dir, "cm.yaml"), validYAML)
	// files in "failures" dir should be skipped, even if invalid
	writeFile(t, filepath.Join(dir, "failures", "bad.yaml"), "null")

	files, err := file.ReadFiles(context.TODO(), dir)
	if err != nil {
		t.Fatalf("expected no error (failures dir should be skipped), got: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file (skipping failures dir), got %d", len(files))
	}
}

func TestReadFilesNonExistentDir(t *testing.T) {
	dir := "/does/not/exist"
	_, err := file.ReadFiles(context.TODO(), dir)
	if err == nil {
		t.Fatal("expected error for non-existent dir, got nil")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error should contain directory path, got: %v", err)
	}
}

func TestGetResourceFilename_SanitizesWindowsReservedChars(t *testing.T) {
	tests := []struct {
		name     string
		obj      unstructured.Unstructured
		expected string
	}{
		{
			name: "system:deployers RoleBinding",
			obj: func() unstructured.Unstructured {
				u := unstructured.Unstructured{}
				u.SetName("system:deployers")
				u.SetNamespace("my-ns")
				u.SetGroupVersionKind(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"})
				return u
			}(),
			expected: "RoleBinding_rbac.authorization.k8s.io_v1_my-ns_system_deployers_7ce771ca.yaml",
		},
		{
			name: "system:image-builders RoleBinding",
			obj: func() unstructured.Unstructured {
				u := unstructured.Unstructured{}
				u.SetName("system:image-builders")
				u.SetNamespace("my-ns")
				u.SetGroupVersionKind(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"})
				return u
			}(),
			expected: "RoleBinding_rbac.authorization.k8s.io_v1_my-ns_system_image-builders_faff99cd.yaml",
		},
		{
			name: "name without reserved chars is unchanged",
			obj: func() unstructured.Unstructured {
				u := unstructured.Unstructured{}
				u.SetName("my-deployment")
				u.SetNamespace("default")
				u.SetGroupVersionKind(schema.GroupVersionKind{Group: "apps", Version: "v1", Kind: "Deployment"})
				return u
			}(),
			expected: "Deployment_apps_v1_default_my-deployment.yaml",
		},
		{
			name: "name with multiple reserved chars",
			obj: func() unstructured.Unstructured {
				u := unstructured.Unstructured{}
				u.SetName("a<b>c:d")
				u.SetNamespace("ns")
				u.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
				return u
			}(),
			expected: "ConfigMap__v1_ns_a_b_c_d_20b20061.yaml",
		},
		{
			name: "cluster-scoped resource with colon",
			obj: func() unstructured.Unstructured {
				u := unstructured.Unstructured{}
				u.SetName("system:admin")
				u.SetGroupVersionKind(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "ClusterRole"})
				return u
			}(),
			expected: "ClusterRole_rbac.authorization.k8s.io_v1_clusterscoped_system_admin_f7295a44.yaml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := file.GetResourceFilename(tt.obj)
			if got != tt.expected {
				t.Errorf("GetResourceFilename() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestGetResourceFilename_TruncatesLongNames(t *testing.T) {
	obj := unstructured.Unstructured{}
	obj.SetName(strings.Repeat("a", 253))
	obj.SetNamespace("default")
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "ConfigMap"})

	filename := file.GetResourceFilename(obj)
	if len(filename) > 255 {
		t.Errorf("GetResourceFilename() length = %d, want <= 255: %q", len(filename), filename)
	}
	if !strings.HasSuffix(filename, ".yaml") {
		t.Errorf("GetResourceFilename() = %q, must end with .yaml", filename)
	}
}

func TestGetResourceFilename_LongNameWithColonTruncates(t *testing.T) {
	obj := unstructured.Unstructured{}
	obj.SetName("system:" + strings.Repeat("x", 246))
	obj.SetNamespace("default")
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "example.com", Version: "v1", Kind: "ConfigMap"})

	filename := file.GetResourceFilename(obj)
	if len(filename) > 255 {
		t.Errorf("GetResourceFilename() length = %d, want <= 255: %q", len(filename), filename)
	}
	if !strings.HasSuffix(filename, ".yaml") {
		t.Errorf("GetResourceFilename() = %q, must end with .yaml", filename)
	}
	if strings.Contains(filename, ":") {
		t.Errorf("GetResourceFilename() = %q, must not contain ':'", filename)
	}
}

func TestGetResourceFilename_NoWindowsReservedChars(t *testing.T) {
	reserved := []string{"<", ">", ":", "\"", "/", "\\", "|", "?", "*"}
	obj := unstructured.Unstructured{}
	obj.SetName("system:deployers")
	obj.SetNamespace("my-ns")
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "authorization.openshift.io", Version: "v1", Kind: "RoleBinding"})

	got := file.GetResourceFilename(obj)
	for _, ch := range reserved {
		if strings.Contains(got, ch) {
			t.Errorf("GetResourceFilename() = %q, contains Windows-reserved character %q", got, ch)
		}
	}
}

func TestGetResourceFilename_LongNameCollisionPrevention(t *testing.T) {
	name1 := strings.Repeat("a", 253)
	name2 := strings.Repeat("a", 252) + "b"

	obj1 := unstructured.Unstructured{}
	obj1.SetKind("ConfigMap")
	obj1.SetName(name1)
	obj1.SetNamespace("default")
	obj1.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})

	obj2 := unstructured.Unstructured{}
	obj2.SetKind("ConfigMap")
	obj2.SetName(name2)
	obj2.SetNamespace("default")
	obj2.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})

	path1 := file.GetResourceFilename(obj1)
	path2 := file.GetResourceFilename(obj2)

	if path1 == path2 {
		t.Errorf("GetResourceFilename() produced identical paths for different resources:\npath1=%q\npath2=%q", path1, path2)
	}
	if len(path1) > 255 {
		t.Errorf("path1 length %d exceeds 255", len(path1))
	}
	if len(path2) > 255 {
		t.Errorf("path2 length %d exceeds 255", len(path2))
	}
}

func TestGetResourceFilename_LongPrefixAndName(t *testing.T) {
	longNamespace := strings.Repeat("n", 63)
	longGroup := strings.Repeat("g", 100)
	longName := strings.Repeat("x", 253)

	obj := unstructured.Unstructured{}
	obj.SetKind("CustomResource")
	obj.SetName(longName)
	obj.SetNamespace(longNamespace)
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: longGroup, Version: "v1beta1", Kind: "CustomResource"})

	path := file.GetResourceFilename(obj)
	if len(path) > 255 {
		t.Errorf("path length %d exceeds 255: %q", len(path), path)
	}
	if !strings.HasSuffix(path, ".yaml") {
		t.Errorf("path does not end with .yaml: %q", path)
	}
	if len(path) < 22 {
		t.Errorf("path too short to contain hash: %q", path)
	}
}

func TestGetResourceFilename_NoCollisionAfterSanitization(t *testing.T) {
	colonObj := unstructured.Unstructured{}
	colonObj.SetName("system:deployers")
	colonObj.SetNamespace("ns")
	colonObj.SetGroupVersionKind(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"})

	underscoreObj := unstructured.Unstructured{}
	underscoreObj.SetName("system_deployers")
	underscoreObj.SetNamespace("ns")
	underscoreObj.SetGroupVersionKind(schema.GroupVersionKind{Group: "rbac.authorization.k8s.io", Version: "v1", Kind: "RoleBinding"})

	colonFile := file.GetResourceFilename(colonObj)
	underscoreFile := file.GetResourceFilename(underscoreObj)

	if colonFile == underscoreFile {
		t.Errorf("system:deployers and system_deployers must produce distinct filenames, both got %q", colonFile)
	}
}

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

	t.Run("GetInputDir", func(t *testing.T) {
		result := opts.GetInputDir("10_kubernetes")
		expected := "/transform/10_kubernetes/input"
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
		expected := "/transform/10_kubernetes/input/deployment.yaml"
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

			inputDir := opts.GetInputDir(stage.name)
			expectedInput := stage.expected + "/input"
			if inputDir != expectedInput {
				t.Errorf("expected %v, got %v", expectedInput, inputDir)
			}

			kustomizationPath := opts.GetKustomizationPath(stage.name)
			expectedKustomization := stage.expected + "/kustomization.yaml"
			if kustomizationPath != expectedKustomization {
				t.Errorf("expected %v, got %v", expectedKustomization, kustomizationPath)
			}
		})
	}
}
