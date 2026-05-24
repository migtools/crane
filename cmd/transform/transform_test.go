// Many tests in this file let stage execution fail intentionally — no real plugins
// or export data are provided. These tests verify the logic around execution (directory
// lifecycle, stage discovery, input validation, flag wiring) rather than the
// transformation itself. A successful end-to-end run requires real plugins and
// kubectl/kustomize, which belongs in integration tests.
package transform

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/konveyor/crane/internal/flags"
	internalTransform "github.com/konveyor/crane/internal/transform"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// testEnv provides a reusable test environment for transform tests
type testEnv struct {
	t            *testing.T
	TempDir      string
	TransformDir string
	ExportDir    string
	PluginDir    string
	Log          *logrus.Logger
	Opts         *Options
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	tempDir := t.TempDir()
	env := &testEnv{
		t:            t,
		TempDir:      tempDir,
		TransformDir: tempDir + "/transform",
		ExportDir:    tempDir + "/export",
		PluginDir:    tempDir + "/plugins",
		Log:          newQuietLogger(),
		Opts:         &Options{},
	}
	env.mkdirAll(env.TransformDir, env.ExportDir, env.PluginDir)
	return env
}

func (e *testEnv) mkdirAll(dirs ...string) {
	e.t.Helper()
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			e.t.Fatalf("failed to create directory %s: %v", dir, err)
		}
	}
}

func (e *testEnv) newOrchestrator() *internalTransform.Orchestrator {
	return &internalTransform.Orchestrator{
		Log:                e.Log,
		ExportDir:          e.ExportDir,
		TransformDir:       e.TransformDir,
		PluginDir:          e.PluginDir,
		NewlyCreatedStages: make(map[string]bool),
	}
}

func (e *testEnv) newRunOptions(f Flags) *Options {
	gf := &flags.GlobalFlags{}
	return &Options{
		cobraGlobalFlags: gf,
		globalFlags:      gf,
		Flags:            f,
	}
}

func (e *testEnv) stageDir(stageName string) string {
	return e.TransformDir + "/" + stageName
}

func (e *testEnv) stageOutputDir(stageName string) string {
	return e.TransformDir + "/.work/" + stageName + "/output"
}

func newQuietLogger() *logrus.Logger {
	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)
	log.SetOutput(io.Discard)
	return log
}

func TestNewTransformCommand_subcommands_registered(t *testing.T) {
	cmd := NewTransformCommand(&flags.GlobalFlags{})

	subcommands := cmd.Commands()

	if len(subcommands) != 2 {
		t.Errorf("len(cmd.Commands()) = %d, want 2", len(subcommands))
	}

	wantSubcommands := map[string]bool{
		"optionals":    false,
		"list-plugins": false,
	}

	for _, subcmd := range subcommands {
		if _, exists := wantSubcommands[subcmd.Use]; exists {
			wantSubcommands[subcmd.Use] = true
		} else {
			t.Errorf("unexpected subcommand %q", subcmd.Use)
		}
	}

	for name, found := range wantSubcommands {
		if !found {
			t.Errorf("subcommand %q not found", name)
		}
	}
}

func TestNewTransformCommand_flags_registered(t *testing.T) {
	cmd := NewTransformCommand(&flags.GlobalFlags{})

	// Test regular flags
	regularFlags := []struct {
		name     string
		defValue string
	}{
		{"export-dir", "export"},
		{"transform-dir", "transform"},
		{"stage", ""},
		{"force", "false"},
		{"optional-flags", ""},
		{"kustomize-args", ""},
		{"ignored-patches-dir", ""},
	}

	for _, tt := range regularFlags {
		flag := cmd.Flags().Lookup(tt.name)
		if flag == nil {
			t.Errorf("flag %q not registered on transform command", tt.name)
			continue
		}
		if flag.DefValue != tt.defValue {
			t.Errorf("flag %q default = %q, want %q", tt.name, flag.DefValue, tt.defValue)
		}
	}

	// Test persistent flags (passed down to subcommands)
	persistentFlags := []string{
		"plugin-dir",
		"skip-plugins",
	}

	for _, name := range persistentFlags {
		if cmd.PersistentFlags().Lookup(name) == nil {
			t.Errorf("persistent flag %q not registered on transform command", name)
		}
	}
}

func TestOptionalFlagsToLower(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]string
		expected map[string]string
	}{
		{
			name:     "empty map returns empty map",
			input:    map[string]string{},
			expected: map[string]string{},
		},
		{
			name: "already lowercase keys unchanged",
			input: map[string]string{
				"foo-flag": "foo-value",
				"bar-flag": "bar-value",
			},
			expected: map[string]string{
				"foo-flag": "foo-value",
				"bar-flag": "bar-value",
			},
		},
		{
			name: "mixed case keys converted to lowercase",
			input: map[string]string{
				"Foo-Flag": "foo-value",
				"BAR-FLAG": "bar-value",
			},
			expected: map[string]string{
				"foo-flag": "foo-value",
				"bar-flag": "bar-value",
			},
		},
		{
			name: "values are preserved unchanged",
			input: map[string]string{
				"My-Key": "PreserveCase-Value",
			},
			expected: map[string]string{
				"my-key": "PreserveCase-Value",
			},
		},
		{
			name: "JSON parsed result scenario",
			input: map[string]string{
				"foo-flag": "foo-a=/data,foo-b=/data",
				"bar-flag": "bar-value",
			},
			expected: map[string]string{
				"foo-flag": "foo-a=/data,foo-b=/data",
				"bar-flag": "bar-value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := optionalFlagsToLower(tt.input)

			if len(result) != len(tt.expected) {
				t.Errorf("len(result) = %d, want %d", len(result), len(tt.expected))
			}

			for key, expectedVal := range tt.expected {
				actualVal, ok := result[key]
				if !ok {
					t.Errorf("expected key %q not found in result", key)
					continue
				}
				if actualVal != expectedVal {
					t.Errorf("result[%q] = %q, want %q", key, actualVal, expectedVal)
				}
			}

			// Verify all keys are lowercase
			for key := range result {
				if key != strings.ToLower(key) {
					t.Errorf("key %q is not lowercase", key)
				}
			}
		})
	}
}

func TestEnsureStagesHaveOutput(t *testing.T) {
	tests := []struct {
		name            string
		stages          []internalTransform.Stage
		setupStageDirs  []string
		setupOutputDirs []string
		wantErr         bool
		wantErrContains string
	}{
		{
			name:   "empty stages",
			stages: []internalTransform.Stage{},
		},
		{
			name: "single stage with output",
			stages: []internalTransform.Stage{
				{Priority: 10, PluginName: "KubernetesPlugin", DirName: "10_KubernetesPlugin"},
			},
			setupOutputDirs: []string{"10_KubernetesPlugin"},
		},
		{
			// Intentional failure: no real plugins. Tests that missing output triggers RunMultiStage.
			name: "single stage no output",
			stages: []internalTransform.Stage{
				{Priority: 10, PluginName: "TestPlugin", DirName: "10_TestPlugin"},
			},
			setupStageDirs:  []string{"10_TestPlugin"},
			wantErr:         true,
			wantErrContains: "failed to run stage 10_TestPlugin",
		},
		{
			// Intentional failure: no real plugins. Tests that only stages without output are run.
			name: "multiple stages mixed",
			stages: []internalTransform.Stage{
				{Priority: 10, PluginName: "Stage1", DirName: "10_Stage1"},
				{Priority: 20, PluginName: "Stage2", DirName: "20_Stage2"},
			},
			setupStageDirs:  []string{"10_Stage1", "20_Stage2"},
			setupOutputDirs: []string{"10_Stage1"},
			wantErr:         true,
			wantErrContains: "failed to run stage 20_Stage2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)

			for i := range tt.stages {
				tt.stages[i].Path = env.stageDir(tt.stages[i].DirName)
			}
			for _, name := range tt.setupStageDirs {
				env.mkdirAll(env.stageDir(name))
			}
			for _, name := range tt.setupOutputDirs {
				env.mkdirAll(env.stageOutputDir(name))
			}

			orchestrator := env.newOrchestrator()
			err := env.Opts.ensureStagesHaveOutput(orchestrator, tt.stages, env.TransformDir, env.Log)

			if tt.wantErr {
				if err == nil {
					t.Fatal("ensureStagesHaveOutput() returned nil error, want error")
				}
				if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErrContains)
				}
			} else if err != nil {
				t.Errorf("ensureStagesHaveOutput() returned error = %v, want nil", err)
			}
		})
	}
}

func TestEnsurePreviousStagesRun_no_stages(t *testing.T) {
	env := newTestEnv(t)
	orchestrator := env.newOrchestrator()

	// Transform directory is empty (no stage subdirectories)
	err := env.Opts.ensurePreviousStagesRun(orchestrator, env.TransformDir, env.Log)

	if err != nil {
		t.Errorf("ensurePreviousStagesRun() returned error = %v, want nil", err)
	}
}

func TestRunStageWithCleanup(t *testing.T) {
	tests := []struct {
		name             string
		stageName        string
		setupValidStage  bool
		cleanupOnError   bool
		wantError        bool
		wantDirPreserved bool
	}{
		{
			name:             "success - no cleanup needed",
			stageName:        "10_PassThrough",
			setupValidStage:  true,
			cleanupOnError:   true,
			wantError:        false,
			wantDirPreserved: true,
		},
		{
			// Intentional failure: no real plugins.
			name:             "error with cleanupOnError true - directory removed",
			stageName:        "10_MissingPlugin",
			setupValidStage:  false,
			cleanupOnError:   true,
			wantError:        true,
			wantDirPreserved: false,
		},
		{
			// Intentional failure: no real plugins.
			name:             "error with cleanupOnError false - directory preserved",
			stageName:        "10_MissingPlugin",
			setupValidStage:  false,
			cleanupOnError:   false,
			wantError:        true,
			wantDirPreserved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			stageDir := env.stageDir(tt.stageName)
			env.mkdirAll(stageDir)

			if tt.setupValidStage {
				exportResourcesDir := env.ExportDir + "/resources/default"
				env.mkdirAll(exportResourcesDir)
				configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-config
  namespace: default
data:
  key: value
`
				if err := os.WriteFile(exportResourcesDir+"/ConfigMap_default_test-config.yaml", []byte(configMapYAML), 0644); err != nil {
					t.Fatalf("failed to write configmap: %v", err)
				}
			}

			orchestrator := env.newOrchestrator()
			orchestrator.Force = true

			selector := internalTransform.StageSelector{Stage: tt.stageName}
			err := env.Opts.runStageWithCleanup(orchestrator, selector, stageDir, tt.cleanupOnError, env.Log)

			if tt.wantError && err == nil {
				t.Errorf("runStageWithCleanup() returned nil error, want error")
			}
			if !tt.wantError && err != nil {
				t.Errorf("runStageWithCleanup() returned error = %v, want nil", err)
			}

			_, statErr := os.Stat(stageDir)
			dirExists := statErr == nil

			if tt.wantDirPreserved && !dirExists {
				t.Errorf("stageDir should be preserved but was removed")
			}
			if !tt.wantDirPreserved && dirExists {
				t.Errorf("stageDir should be removed but still exists")
			}
		})
	}
}

func TestNewTransformCommand_prerun_binds_flags(t *testing.T) {
	viper.Reset()

	cmd := NewTransformCommand(&flags.GlobalFlags{})

	var capturedFlags Flags
	cmd.RunE = func(c *cobra.Command, args []string) error {
		viper.Unmarshal(&capturedFlags)
		return nil
	}

	cmd.SetArgs([]string{
		"--export-dir", "/tmp/test-export",
		"--transform-dir", "/tmp/test-transform",
		"--stage", "20_TestStage",
		"--force",
		"--optional-flags", `{"key":"val"}`,
		"--kustomize-args", "--enable-helm",
		"--skip-plugins", "pluginA,pluginB",
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("cmd.Execute() returned error: %v", err)
	}

	if capturedFlags.ExportDir != "/tmp/test-export" {
		t.Errorf("ExportDir = %q, want %q", capturedFlags.ExportDir, "/tmp/test-export")
	}
	if capturedFlags.TransformDir != "/tmp/test-transform" {
		t.Errorf("TransformDir = %q, want %q", capturedFlags.TransformDir, "/tmp/test-transform")
	}
	if capturedFlags.Stage != "20_TestStage" {
		t.Errorf("Stage = %q, want %q", capturedFlags.Stage, "20_TestStage")
	}
	if !capturedFlags.Force {
		t.Error("Force = false, want true")
	}
	if capturedFlags.OptionalFlags != `{"key":"val"}` {
		t.Errorf("OptionalFlags = %q, want %q", capturedFlags.OptionalFlags, `{"key":"val"}`)
	}
	if capturedFlags.KustomizeArgs != "--enable-helm" {
		t.Errorf("KustomizeArgs = %q, want %q", capturedFlags.KustomizeArgs, "--enable-helm")
	}
}

func TestNewTransformCommand_rejects_invalid_flags(t *testing.T) {
	tests := []struct {
		name            string
		args            []string
		wantErrContains string
	}{
		{
			name:            "unknown flag",
			args:            []string{"--does-not-exist", "value"},
			wantErrContains: "unknown flag",
		},
		{
			name: "missing flag value",
			args: []string{"--export-dir"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			viper.Reset()
			cmd := NewTransformCommand(&flags.GlobalFlags{})
			cmd.SetArgs(tt.args)

			err := cmd.Execute()

			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErrContains)
			}
		})
	}
}

func TestRun_invalid_input(t *testing.T) {
	tests := []struct {
		name            string
		flags           Flags
		wantErrContains string
	}{
		{
			name:            "invalid optional flags JSON",
			flags:           Flags{OptionalFlags: "not-json"},
			wantErrContains: "invalid character",
		},
		{
			name:            "kustomize args with shell injection",
			flags:           Flags{KustomizeArgs: "--enable-helm; rm -rf /"},
			wantErrContains: "invalid kustomize-args",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			tt.flags.ExportDir = env.ExportDir
			tt.flags.PluginDir = env.PluginDir
			tt.flags.TransformDir = env.TransformDir
			opts := env.newRunOptions(tt.flags)

			err := opts.run()

			if err == nil {
				t.Fatal("run() returned nil error, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErrContains) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tt.wantErrContains)
			}
		})
	}
}

// Intentional failure: no real plugins. Tests that run() bootstraps the default stage directory.
func TestRun_no_stages_creates_default_stage_dir(t *testing.T) {
	env := newTestEnv(t)
	opts := env.newRunOptions(Flags{
		ExportDir:    env.ExportDir,
		PluginDir:    env.PluginDir,
		TransformDir: env.TransformDir,
	})

	_ = opts.run()

	defaultStageDir := env.stageDir("10_KubernetesPlugin")
	if _, err := os.Stat(defaultStageDir); os.IsNotExist(err) {
		t.Error("run() should create default stage directory 10_KubernetesPlugin")
	}
}

// Intentional failure: no real plugins. Tests directory cleanup behavior on error.
func TestRun_specific_stage_directory_lifecycle(t *testing.T) {
	tests := []struct {
		name             string
		stageName        string
		preCreateStage   bool
		wantDirPreserved bool
	}{
		{
			name:             "existing stage preserved on error",
			stageName:        "10_ExistingPlugin",
			preCreateStage:   true,
			wantDirPreserved: true,
		},
		{
			name:             "new stage cleaned up on error",
			stageName:        "20_FailingPlugin",
			preCreateStage:   false,
			wantDirPreserved: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			env := newTestEnv(t)
			if tt.preCreateStage {
				env.mkdirAll(env.stageDir(tt.stageName))
			}

			opts := env.newRunOptions(Flags{
				ExportDir:    env.ExportDir,
				PluginDir:    env.PluginDir,
				TransformDir: env.TransformDir,
				Stage:        tt.stageName,
			})

			_ = opts.run()

			_, statErr := os.Stat(env.stageDir(tt.stageName))
			dirExists := statErr == nil

			if tt.wantDirPreserved && !dirExists {
				t.Error("stage directory should be preserved but was removed")
			}
			if !tt.wantDirPreserved && dirExists {
				t.Error("stage directory should be cleaned up but still exists")
			}
		})
	}
}

// Intentional failure: no real plugins. Tests that run() discovers stages from the directory.
func TestRun_discovers_existing_stages(t *testing.T) {
	env := newTestEnv(t)

	// Create two stage directories matching the naming convention
	env.mkdirAll(env.stageDir("10_FirstPlugin"), env.stageDir("20_SecondPlugin"))

	opts := env.newRunOptions(Flags{
		ExportDir:    env.ExportDir,
		PluginDir:    env.PluginDir,
		TransformDir: env.TransformDir,
	})

	err := opts.run()

	if err == nil {
		t.Fatal("run() returned nil error, want error from stage execution")
	}
}
