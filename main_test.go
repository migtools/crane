package main

import (
	"os"
	"path/filepath"
	"testing"

	modzip "golang.org/x/mod/zip"
)

// moduleZipErrors reports every problem that would stop the Go toolchain from
// packaging dir as a module zip (and so break
// `go install github.com/konveyor/crane@<version>`), e.g. file names with
// characters such as ':' or case-insensitive path collisions.
func moduleZipErrors(t *testing.T, dir string) []string {
	t.Helper()

	cf, err := modzip.CheckDir(dir)
	if err == nil {
		return nil
	}

	var problems []string
	for _, fe := range cf.Invalid {
		problems = append(problems, fe.Error())
	}
	if cf.SizeError != nil {
		problems = append(problems, cf.SizeError.Error())
	}
	if len(problems) == 0 {
		t.Fatalf("checking module files in %q: %v", dir, err)
	}
	return problems
}

// TestModuleFilesAreValidForGoInstall guards against committing files (for
// example generated golden manifests) whose paths are not allowed in a Go
// module zip. See https://github.com/migtools/crane/issues/822.
func TestModuleFilesAreValidForGoInstall(t *testing.T) {
	for _, problem := range moduleZipErrors(t, ".") {
		t.Errorf("file is not allowed in a Go module zip, `go install` would fail: %s", problem)
	}
}

func TestModuleZipErrorsDetectsInvalidPaths(t *testing.T) {
	tests := []struct {
		name        string
		files       []string
		wantInvalid bool
	}{
		{
			name:  "valid paths",
			files: []string{"main.go", "resources/RoleBinding_rbac_v1_ns_system-deployers.yaml"},
		},
		{
			name:        "colon in file name",
			files:       []string{"main.go", "resources/RoleBinding_rbac_v1_ns_system:deployers.yaml"},
			wantInvalid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			files := append([]string{"go.mod"}, tt.files...)
			for _, name := range files {
				path := filepath.Join(dir, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("creating directory for %q: %v", name, err)
				}
				content := "package main\n"
				if name == "go.mod" {
					content = "module example.com/m\n\ngo 1.25\n"
				}
				if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
					// Some file systems (e.g. Windows) reject these names outright.
					t.Skipf("cannot create %q on this file system: %v", name, err)
				}
			}

			problems := moduleZipErrors(t, dir)
			if gotInvalid := len(problems) > 0; gotInvalid != tt.wantInvalid {
				t.Errorf("moduleZipErrors() = %v, want invalid paths reported: %v", problems, tt.wantInvalid)
			}
		})
	}
}
