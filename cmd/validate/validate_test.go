package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/cli-runtime/pkg/genericclioptions"
)

func TestNewValidateCommand(t *testing.T) {
	streams := genericclioptions.NewTestIOStreamsDiscard()
	cmd := NewValidateCommand(streams, nil)

	if cmd.Use != "validate" {
		t.Fatalf("Use = %q, want %q", cmd.Use, "validate")
	}

	expectedFlags := []string{"input-dir", "validate-dir", "output"}
	for _, name := range expectedFlags {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("flag %q not registered on validate command", name)
		}
	}

	if d := cmd.Flags().Lookup("input-dir").DefValue; d != "output" {
		t.Errorf("input-dir default = %q, want %q", d, "output")
	}
	if d := cmd.Flags().Lookup("output").DefValue; d != "json" {
		t.Errorf("output default = %q, want %q", d, "json")
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
			name: "missing input-dir",
			setup: func(t *testing.T) *ValidateOptions {
				missingDir := filepath.Join(t.TempDir(), "missing")
				return &ValidateOptions{
					inputDir:     missingDir,
					outputFormat: "yaml",
				}
			},
			wantErr:  true,
			errMatch: "input-dir",
		},
		{
			name: "input-dir is a file",
			setup: func(t *testing.T) *ValidateOptions {
				dir := t.TempDir()
				f := filepath.Join(dir, "afile")
				if err := os.WriteFile(f, []byte("x"), 0600); err != nil {
					t.Fatal(err)
				}
				return &ValidateOptions{
					inputDir:    f,
					outputFormat: "yaml",
				}
			},
			wantErr:  true,
			errMatch: "not a directory",
		},
		{
			name: "invalid output format",
			setup: func(t *testing.T) *ValidateOptions {
				return &ValidateOptions{
					inputDir:    t.TempDir(),
					outputFormat: "xml",
				}
			},
			wantErr:  true,
			errMatch: "yaml",
		},
		{
			name: "valid yaml format",
			setup: func(t *testing.T) *ValidateOptions {
				return &ValidateOptions{
					inputDir:    t.TempDir(),
					outputFormat: "yaml",
				}
			},
			wantErr: false,
		},
		{
			name: "valid json format",
			setup: func(t *testing.T) *ValidateOptions {
				return &ValidateOptions{
					inputDir:    t.TempDir(),
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
					if got := err.Error(); !strings.Contains(got, tt.errMatch) {
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

func TestValidateCommand_RejectsArgs(t *testing.T) {
	streams := genericclioptions.NewTestIOStreamsDiscard()
	cmd := NewValidateCommand(streams, nil)
	cmd.SetArgs([]string{"./manifests"})
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for positional args, got nil")
	}
	if !strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("expected 'unknown command' error, got: %v", err)
	}
}

