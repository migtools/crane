package add

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateFileInput(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name     string
		filename string
		wantErr  bool
	}{
		{name: "valid name", filename: "my-plugin", wantErr: false},
		{name: "empty name", filename: "", wantErr: true},
		{name: "dot", filename: ".", wantErr: true},
		{name: "dotdot", filename: "..", wantErr: true},
		{name: "forward slash", filename: "foo/bar", wantErr: true},
		{name: "back slash", filename: "foo\\bar", wantErr: true},
		{name: "traversal", filename: "../evil", wantErr: true},
		{name: "absolute path", filename: "/etc/passwd", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validateFileInput(dir, tt.filename); (err != nil) != tt.wantErr {
				t.Errorf("validateFileInput(%q, %q) error = %v, wantErr %v", dir, tt.filename, err, tt.wantErr)
			}
		})
	}
}

func TestValidateFileInputExistingFile(t *testing.T) {
	dir := t.TempDir()
	filename := "existing-plugin"
	if err := os.WriteFile(filepath.Join(dir, filename), []byte("x"), 0755); err != nil {
		t.Fatal(err)
	}

	if err := validateFileInput(dir, filename); err == nil {
		t.Errorf("expected error for existing file, got nil")
	}
}

func TestValidateFileInputExistingSymlink(t *testing.T) {
	dir := t.TempDir()
	filename := "broken-link"
	if err := os.Symlink(filepath.Join(dir, "does-not-exist"), filepath.Join(dir, filename)); err != nil {
		t.Fatal(err)
	}

	if err := validateFileInput(dir, filename); err == nil {
		t.Errorf("expected error for existing broken symlink, got nil")
	}
}
