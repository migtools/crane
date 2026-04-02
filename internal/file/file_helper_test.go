package file_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/konveyor/crane/internal/file"
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
