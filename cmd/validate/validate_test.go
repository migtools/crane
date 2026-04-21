package validate

import (
	"os"
	"path/filepath"
	"testing"

	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestNewValidateCommand(t *testing.T) {
	streams := genericclioptions.NewTestIOStreamsDiscard()
	cmd := NewValidateCommand(streams, nil)

	if cmd.Use != "validate" {
		t.Fatalf("Use = %q, want %q", cmd.Use, "validate")
	}

	expectedFlags := []string{"export-dir", "validate-dir", "output"}
	for _, name := range expectedFlags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q not registered on validate command", name)
		}
	}

	if d := cmd.Flags().Lookup("export-dir").DefValue; d != "export" {
		t.Errorf("export-dir default = %q, want %q", d, "export")
	}
	if d := cmd.Flags().Lookup("output").DefValue; d != "table" {
		t.Errorf("output default = %q, want %q", d, "table")
	}
	if d := cmd.Flags().Lookup("validate-dir").DefValue; d != "validate" {
		t.Errorf("validate-dir default = %q, want %q", d, "validate")
	}
}

func TestValidate_Flags(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T) *ValidateOptions
		wantErr  bool
		errMatch string
	}{
		{
			name: "missing export-dir",
			setup: func(t *testing.T) *ValidateOptions {
				return &ValidateOptions{
					exportDir:    "/nonexistent/path/validate-test",
					outputFormat: "table",
				}
			},
			wantErr:  true,
			errMatch: "export-dir",
		},
		{
			name: "export-dir is a file",
			setup: func(t *testing.T) *ValidateOptions {
				dir := t.TempDir()
				f := filepath.Join(dir, "afile")
				if err := os.WriteFile(f, []byte("x"), 0600); err != nil {
					t.Fatal(err)
				}
				return &ValidateOptions{
					exportDir:    f,
					outputFormat: "table",
				}
			},
			wantErr:  true,
			errMatch: "not a directory",
		},
		{
			name: "invalid output format",
			setup: func(t *testing.T) *ValidateOptions {
				return &ValidateOptions{
					exportDir:    t.TempDir(),
					outputFormat: "xml",
				}
			},
			wantErr:  true,
			errMatch: "table",
		},
		{
			name: "valid table format",
			setup: func(t *testing.T) *ValidateOptions {
				return &ValidateOptions{
					exportDir:    t.TempDir(),
					outputFormat: "table",
				}
			},
			wantErr: false,
		},
		{
			name: "valid json format",
			setup: func(t *testing.T) *ValidateOptions {
				return &ValidateOptions{
					exportDir:    t.TempDir(),
					outputFormat: "json",
				}
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			o := tt.setup(t)
			err := o.Validate()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if tt.errMatch != "" {
					if got := err.Error(); !contains(got, tt.errMatch) {
						t.Fatalf("error = %q, want substring %q", got, tt.errMatch)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
