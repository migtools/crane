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

	expectedFlags := []string{"input-dir", "validate-dir", "output", "api-resources"}
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
	if d := cmd.Flags().Lookup("api-resources").DefValue; d != "" {
		t.Errorf("api-resources default = %q, want empty", d)
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
		{
			name: "api-resources file not found",
			setup: func(t *testing.T) *ValidateOptions {
				return &ValidateOptions{
					configFlags:      genericclioptions.NewConfigFlags(true),
					inputDir:         t.TempDir(),
					outputFormat:     "json",
					apiResourcesFile: "/nonexistent/api-resources.json",
				}
			},
			wantErr:  true,
			errMatch: "api-resources file",
		},
		{
			name: "api-resources with context is mutually exclusive",
			setup: func(t *testing.T) *ValidateOptions {
				dir := t.TempDir()
				f := filepath.Join(dir, "api-resources.json")
				if err := os.WriteFile(f, []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
				ctx := "some-context"
				cf := genericclioptions.NewConfigFlags(true)
				cf.Context = &ctx
				return &ValidateOptions{
					configFlags:      cf,
					inputDir:         dir,
					outputFormat:     "json",
					apiResourcesFile: f,
				}
			},
			wantErr:  true,
			errMatch: "mutually exclusive",
		},
		{
			name: "api-resources with kubeconfig is mutually exclusive",
			setup: func(t *testing.T) *ValidateOptions {
				dir := t.TempDir()
				f := filepath.Join(dir, "api-resources.json")
				if err := os.WriteFile(f, []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
				kc := "/some/kubeconfig"
				cf := genericclioptions.NewConfigFlags(true)
				cf.KubeConfig = &kc
				return &ValidateOptions{
					configFlags:      cf,
					inputDir:         dir,
					outputFormat:     "json",
					apiResourcesFile: f,
				}
			},
			wantErr:  true,
			errMatch: "mutually exclusive",
		},
		{
			name: "api-resources valid file accepted",
			setup: func(t *testing.T) *ValidateOptions {
				dir := t.TempDir()
				f := filepath.Join(dir, "api-resources.json")
				if err := os.WriteFile(f, []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
				return &ValidateOptions{
					configFlags:      genericclioptions.NewConfigFlags(true),
					inputDir:         dir,
					outputFormat:     "json",
					apiResourcesFile: f,
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

