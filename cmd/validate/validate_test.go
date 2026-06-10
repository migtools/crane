package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/konveyor/crane/internal/flags"
	"github.com/sirupsen/logrus"
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
			wantErr:  true,
			errMatch: "--api-resources and --context are mutually exclusive",
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
			wantErr:  true,
			errMatch: "--api-resources and --kubeconfig are mutually exclusive",
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
			wantErr:  true,
			errMatch: "--api-resources and --server are mutually exclusive",
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
			wantErr:  true,
			errMatch: "--api-resources and --token are mutually exclusive",
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
			wantErr:  true,
			errMatch: "--api-resources and --cluster are mutually exclusive",
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
			wantErr:  true,
			errMatch: "--api-resources and --user are mutually exclusive",
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

func TestArchiveWithTimestamp_FileExists(t *testing.T) {
	dir := t.TempDir()
	reportPath := filepath.Join(dir, "report.json")

	if err := os.WriteFile(reportPath, []byte(`{"totalScanned":5}`), 0600); err != nil {
		t.Fatal(err)
	}

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	err := archiveWithTimestamp(reportPath, "20260603-100942", log)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Original should be gone
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Error("original report.json should not exist after archiving")
	}

	// Archived file should have exact name
	archivedPath := filepath.Join(dir, "report-20260603-100942.json")
	data, err := os.ReadFile(archivedPath)
	if err != nil {
		t.Fatalf("archived file not found: %v", err)
	}
	if string(data) != `{"totalScanned":5}` {
		t.Errorf("archived content mismatch: got %q", string(data))
	}
}

func TestArchiveWithTimestamp_DirectoryExists(t *testing.T) {
	dir := t.TempDir()
	failuresDir := filepath.Join(dir, "failures")

	if err := os.MkdirAll(failuresDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(failuresDir, "Deployment.yaml"), []byte("test"), 0600); err != nil {
		t.Fatal(err)
	}

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	err := archiveWithTimestamp(failuresDir, "20260603-100942", log)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Original should be gone
	if _, err := os.Stat(failuresDir); !os.IsNotExist(err) {
		t.Error("original failures/ should not exist after archiving")
	}

	// Archived directory should have exact name and contents
	archivedFile := filepath.Join(dir, "failures-20260603-100942", "Deployment.yaml")
	if _, err := os.Stat(archivedFile); os.IsNotExist(err) {
		t.Error("archived directory should contain Deployment.yaml")
	}
}

func TestArchiveWithTimestamp_NothingToArchive(t *testing.T) {
	dir := t.TempDir()
	nonexistent := filepath.Join(dir, "report.json")

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	err := archiveWithTimestamp(nonexistent, "20260603-100942", log)
	if err != nil {
		t.Fatalf("should not error when file doesn't exist: %v", err)
	}
}

func TestArchiveTimestamp_ReportAndFailuresShareTimestamp(t *testing.T) {
	dir := t.TempDir()
	reportPath := filepath.Join(dir, "report.json")
	failuresDir := filepath.Join(dir, "failures")

	if err := os.WriteFile(reportPath, []byte(`{"totalScanned":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(failuresDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(failuresDir, "Deployment.yaml"), []byte("fail"), 0600); err != nil {
		t.Fatal(err)
	}

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	// Get timestamp from report
	ts, err := getArchiveTimestamp(reportPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts == "" {
		t.Fatal("expected non-empty timestamp")
	}

	// Archive both with same timestamp
	if err := archiveWithTimestamp(reportPath, ts, log); err != nil {
		t.Fatalf("archive report: %v", err)
	}
	if err := archiveWithTimestamp(failuresDir, ts, log); err != nil {
		t.Fatalf("archive failures: %v", err)
	}

	// Both should have the same timestamp suffix
	archivedReport := filepath.Join(dir, "report-"+ts+".json")
	archivedFailures := filepath.Join(dir, "failures-"+ts)

	if _, err := os.Stat(archivedReport); os.IsNotExist(err) {
		t.Error("archived report not found")
	}
	if _, err := os.Stat(archivedFailures); os.IsNotExist(err) {
		t.Error("archived failures not found")
	}
}

func TestGetArchiveTimestamp_FileNotFound(t *testing.T) {
	ts, err := getArchiveTimestamp(filepath.Join(t.TempDir(), "nonexistent"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ts != "" {
		t.Errorf("expected empty timestamp for nonexistent file, got %q", ts)
	}
}

func TestArchivePreviousResults_FormatSwitch(t *testing.T) {
	dir := t.TempDir()

	// Simulate first run with -o json
	if err := os.WriteFile(filepath.Join(dir, "report.json"), []byte(`{"mode":"live"}`), 0600); err != nil {
		t.Fatal(err)
	}
	failuresDir := filepath.Join(dir, "failures")
	if err := os.MkdirAll(failuresDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(failuresDir, "Deployment.yaml"), []byte("fail"), 0600); err != nil {
		t.Fatal(err)
	}

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	// Now archive as if user switched to -o yaml
	// The function should find report.json even though current format is yaml
	err := archivePreviousResults(dir, failuresDir, log)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// report.json should be archived
	if _, err := os.Stat(filepath.Join(dir, "report.json")); !os.IsNotExist(err) {
		t.Error("report.json should have been archived")
	}

	// failures/ should be archived
	if _, err := os.Stat(failuresDir); !os.IsNotExist(err) {
		t.Error("failures/ should have been archived")
	}

	// Archived files should exist
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}

	foundReport := false
	foundFailures := false
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "report-") && strings.HasSuffix(entry.Name(), ".json") {
			foundReport = true
		}
		if strings.HasPrefix(entry.Name(), "failures-") && entry.IsDir() {
			foundFailures = true
		}
	}

	if !foundReport {
		t.Error("expected archived report-<timestamp>.json")
	}
	if !foundFailures {
		t.Error("expected archived failures-<timestamp>/ directory")
	}
}

func TestArchivePreviousResults_NoPreviousResults(t *testing.T) {
	dir := t.TempDir()
	failuresDir := filepath.Join(dir, "failures")

	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)

	err := archivePreviousResults(dir, failuresDir, log)
	if err != nil {
		t.Fatalf("should not error when no previous results exist: %v", err)
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
