package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/konveyor/crane/internal/flags"
	"github.com/spf13/cobra"
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
		name                     string
		setup                    func(t *testing.T) *ValidateOptions
		wantErr                  bool
		errMatch                 string
		setMutualExclusionFlags  bool // If true, explicitly mark kubeconfig-related flags as changed
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
					inputDir:     f,
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
					inputDir:     t.TempDir(),
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
					inputDir:     t.TempDir(),
					outputFormat: "yaml",
				}
			},
			wantErr: false,
		},
		{
			name: "valid json format",
			setup: func(t *testing.T) *ValidateOptions {
				return &ValidateOptions{
					inputDir:     t.TempDir(),
					outputFormat: "json",
				}
			},
			wantErr: false,
		},
		{
			name: "uppercase JSON accepted",
			setup: func(t *testing.T) *ValidateOptions {
				return &ValidateOptions{
					inputDir:     t.TempDir(),
					outputFormat: "JSON",
				}
			},
			wantErr: false,
		},
		{
			name: "uppercase YAML accepted",
			setup: func(t *testing.T) *ValidateOptions {
				return &ValidateOptions{
					inputDir:     t.TempDir(),
					outputFormat: "YAML",
				}
			},
			wantErr: false,
		},
		{
			name: "mixed case Json accepted",
			setup: func(t *testing.T) *ValidateOptions {
				return &ValidateOptions{
					inputDir:     t.TempDir(),
					outputFormat: "Json",
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
			name: "api-resources with --context is mutually exclusive",
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
			wantErr:                 true,
			errMatch:                "--api-resources and --context are mutually exclusive",
			setMutualExclusionFlags: true,
		},
		{
			name: "api-resources with --kubeconfig is mutually exclusive",
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
			wantErr:                 true,
			errMatch:                "--api-resources and --kubeconfig are mutually exclusive",
			setMutualExclusionFlags: true,
		},
		{
			name: "api-resources with --server is mutually exclusive",
			setup: func(t *testing.T) *ValidateOptions {
				dir := t.TempDir()
				f := filepath.Join(dir, "api-resources.json")
				if err := os.WriteFile(f, []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
				server := "https://127.0.0.1:6443"
				cf := genericclioptions.NewConfigFlags(true)
				cf.APIServer = &server
				return &ValidateOptions{
					configFlags:      cf,
					inputDir:         dir,
					outputFormat:     "json",
					apiResourcesFile: f,
				}
			},
			wantErr:                 true,
			errMatch:                "--api-resources and --server are mutually exclusive",
			setMutualExclusionFlags: true,
		},
		{
			name: "api-resources with --token is mutually exclusive",
			setup: func(t *testing.T) *ValidateOptions {
				dir := t.TempDir()
				f := filepath.Join(dir, "api-resources.json")
				if err := os.WriteFile(f, []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
				token := "some-bearer-token"
				cf := genericclioptions.NewConfigFlags(true)
				cf.BearerToken = &token
				return &ValidateOptions{
					configFlags:      cf,
					inputDir:         dir,
					outputFormat:     "json",
					apiResourcesFile: f,
				}
			},
			wantErr:                 true,
			errMatch:                "--api-resources and --token are mutually exclusive",
			setMutualExclusionFlags: true,
		},
		{
			name: "api-resources with --cluster is mutually exclusive",
			setup: func(t *testing.T) *ValidateOptions {
				dir := t.TempDir()
				f := filepath.Join(dir, "api-resources.json")
				if err := os.WriteFile(f, []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
				cluster := "some-cluster"
				cf := genericclioptions.NewConfigFlags(true)
				cf.ClusterName = &cluster
				return &ValidateOptions{
					configFlags:      cf,
					inputDir:         dir,
					outputFormat:     "json",
					apiResourcesFile: f,
				}
			},
			wantErr:                 true,
			errMatch:                "--api-resources and --cluster are mutually exclusive",
			setMutualExclusionFlags: true,
		},
		{
			name: "api-resources with --user is mutually exclusive",
			setup: func(t *testing.T) *ValidateOptions {
				dir := t.TempDir()
				f := filepath.Join(dir, "api-resources.json")
				if err := os.WriteFile(f, []byte(`{}`), 0600); err != nil {
					t.Fatal(err)
				}
				user := "some-user"
				cf := genericclioptions.NewConfigFlags(true)
				cf.AuthInfoName = &user
				return &ValidateOptions{
					configFlags:      cf,
					inputDir:         dir,
					outputFormat:     "json",
					apiResourcesFile: f,
				}
			},
			wantErr:                 true,
			errMatch:                "--api-resources and --user are mutually exclusive",
			setMutualExclusionFlags: true,
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
			// Create a minimal cobra command for testing
			cmd := &cobra.Command{}

			// Ensure configFlags is initialized (some tests don't set it)
			if o.configFlags == nil {
				o.configFlags = genericclioptions.NewConfigFlags(true)
			}
			o.configFlags.AddFlags(cmd.Flags())

			// For mutual exclusivity tests, explicitly mark kubeconfig-related flags as changed
			// to simulate user-provided flags (vs environment/default values)
			if tt.setMutualExclusionFlags {
				if o.configFlags.Context != nil && *o.configFlags.Context != "" {
					if err := cmd.Flags().Set("context", *o.configFlags.Context); err != nil {
						t.Fatalf("failed to set context flag: %v", err)
					}
				}
				if o.configFlags.KubeConfig != nil && *o.configFlags.KubeConfig != "" {
					if err := cmd.Flags().Set("kubeconfig", *o.configFlags.KubeConfig); err != nil {
						t.Fatalf("failed to set kubeconfig flag: %v", err)
					}
				}
				if o.configFlags.APIServer != nil && *o.configFlags.APIServer != "" {
					if err := cmd.Flags().Set("server", *o.configFlags.APIServer); err != nil {
						t.Fatalf("failed to set server flag: %v", err)
					}
				}
				if o.configFlags.BearerToken != nil && *o.configFlags.BearerToken != "" {
					if err := cmd.Flags().Set("token", *o.configFlags.BearerToken); err != nil {
						t.Fatalf("failed to set token flag: %v", err)
					}
				}
				if o.configFlags.ClusterName != nil && *o.configFlags.ClusterName != "" {
					if err := cmd.Flags().Set("cluster", *o.configFlags.ClusterName); err != nil {
						t.Fatalf("failed to set cluster flag: %v", err)
					}
				}
				if o.configFlags.AuthInfoName != nil && *o.configFlags.AuthInfoName != "" {
					if err := cmd.Flags().Set("user", *o.configFlags.AuthInfoName); err != nil {
						t.Fatalf("failed to set user flag: %v", err)
					}
				}
			}

			err := o.Validate(cmd)
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

func TestRun_EmptyInputDirReturnsError(t *testing.T) {
	emptyDir := t.TempDir()
	validateDir := filepath.Join(t.TempDir(), "validate")

	// Create a minimal valid API resources file for offline mode
	apiFile := filepath.Join(t.TempDir(), "api-resources.json")
	if err := os.WriteFile(apiFile, []byte(`{}`), 0600); err != nil {
		t.Fatal(err)
	}

	o := &ValidateOptions{
		configFlags:      genericclioptions.NewConfigFlags(true),
		IOStreams:        genericclioptions.NewTestIOStreamsDiscard(),
		inputDir:         emptyDir,
		validateDir:      validateDir,
		outputFormat:     "json",
		apiResourcesFile: apiFile,
		globalFlags:      &flags.GlobalFlags{},
	}

	err := o.Run()
	if err == nil {
		t.Fatal("expected error when input dir has no manifests, got nil")
	}
	if !strings.Contains(err.Error(), "no manifests found") {
		t.Fatalf("expected 'no manifests found' error, got: %v", err)
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

func TestValidate_HelpGroupsFlags(t *testing.T) {
	streams, _, outBuf, _ := genericclioptions.NewTestIOStreams()
	cmd := NewValidateCommand(streams, nil)
	cmd.SetOut(outBuf)
	cmd.SetErr(outBuf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("unexpected error running --help: %v", err)
	}

	help := outBuf.String()

	commandSpecificHeader := "Command-specific Flags:"
	kubeHeader := "Inherited Kubernetes Client Flags:"

	commandSpecificIdx := strings.Index(help, commandSpecificHeader)
	if commandSpecificIdx == -1 {
		t.Fatalf("missing %q section in help output:\n%s", commandSpecificHeader, help)
	}

	kubeIdx := strings.Index(help, kubeHeader)
	if kubeIdx == -1 {
		t.Fatalf("missing %q section in help output:\n%s", kubeHeader, help)
	}

	if commandSpecificIdx > kubeIdx {
		t.Fatalf("expected command-specific section before inherited kube/client section")
	}

	commandSpecificSection := help[commandSpecificIdx:kubeIdx]
	kubeSection := help[kubeIdx:]

	if !strings.Contains(commandSpecificSection, "--validate-dir") {
		t.Fatalf("expected --validate-dir in command-specific section, got:\n%s", commandSpecificSection)
	}

	if strings.Contains(commandSpecificSection, "--as-uid") {
		t.Fatalf("did not expect --as-uid in command-specific section, got:\n%s", commandSpecificSection)
	}

	if !strings.Contains(kubeSection, "--as-uid") {
		t.Fatalf("expected --as-uid in inherited kube/client section, got:\n%s", kubeSection)
	}
}

func TestComplete_InvalidContextFailsBeforeRun(t *testing.T) {
	ctx := "nonexistent-context-that-does-not-exist"
	cf := genericclioptions.NewConfigFlags(true)
	cf.Context = &ctx

	// Use a temp kubeconfig so the test is environment-independent
	kc := filepath.Join(t.TempDir(), "kubeconfig")
	kubeconfig := `apiVersion: v1
kind: Config
clusters:
- name: local
  cluster:
    server: https://127.0.0.1:6443
users:
- name: local-user
  user:
    token: fake
contexts:
- name: existing-context
  context:
    cluster: local
    user: local-user
current-context: existing-context
`
	if err := os.WriteFile(kc, []byte(kubeconfig), 0600); err != nil {
		t.Fatal(err)
	}
	cf.KubeConfig = &kc

	o := &ValidateOptions{
		configFlags:  cf,
		globalFlags:  &flags.GlobalFlags{},
		inputDir:     t.TempDir(),
		outputFormat: "json",
	}

	cmd := &cobra.Command{}
	err := o.Complete(cmd, nil)

	if err == nil {
		t.Fatal("Complete() should fail for nonexistent context, but got nil")
	}
	if !strings.Contains(err.Error(), "nonexistent-context-that-does-not-exist") {
		t.Fatalf("error should mention the invalid context name, got: %v", err)
	}
}

func TestComplete_SkippedInOfflineMode(t *testing.T) {
	o := &ValidateOptions{
		configFlags:      genericclioptions.NewConfigFlags(true),
		globalFlags:      &flags.GlobalFlags{},
		inputDir:         t.TempDir(),
		outputFormat:     "json",
		apiResourcesFile: "/some/file.json",
	}

	cmd := &cobra.Command{}
	err := o.Complete(cmd, nil)

	if err != nil {
		t.Fatalf("Complete() should skip in offline mode, got: %v", err)
	}
}
