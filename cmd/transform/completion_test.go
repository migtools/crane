package transform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/konveyor/crane/internal/flags"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestGetPluginCompletions(t *testing.T) {
	tests := []struct {
		name              string
		setupCmd          func(t *testing.T) *cobra.Command
		args              []string
		toComplete        string
		wantDirective     cobra.ShellCompDirective
		wantPlugins       []string
		wantErr           bool
		checkContains     bool // if true, check that wantPlugins are contained in result, not exact match
	}{
		{
			name: "success path - returns plugin names",
			setupCmd: func(t *testing.T) *cobra.Command {
				// Create a minimal command with just the flags we need
				tempDir := t.TempDir()
				pluginDir := filepath.Join(tempDir, "plugins")
				if err := os.MkdirAll(pluginDir, 0755); err != nil {
					t.Fatalf("failed to create plugin dir: %v", err)
				}

				cmd := &cobra.Command{
					Use: "test",
				}
				cmd.Flags().String("plugin-dir", pluginDir, "")
				cmd.Flags().StringSlice("skip-plugins", []string{}, "")

				return cmd
			},
			args:          []string{},
			toComplete:    "",
			wantDirective: cobra.ShellCompDirectiveNoFileComp,
			wantPlugins:   []string{"KubernetesPlugin"}, // Default plugin is always present
			checkContains: true,
		},
		{
			name: "success path - with skip-plugins flag",
			setupCmd: func(t *testing.T) *cobra.Command {
				tempDir := t.TempDir()
				pluginDir := filepath.Join(tempDir, "plugins")
				if err := os.MkdirAll(pluginDir, 0755); err != nil {
					t.Fatalf("failed to create plugin dir: %v", err)
				}

				cmd := &cobra.Command{
					Use: "test",
				}
				cmd.Flags().String("plugin-dir", pluginDir, "")
				cmd.Flags().StringSlice("skip-plugins", []string{"KubernetesPlugin"}, "")

				return cmd
			},
			args:          []string{},
			toComplete:    "",
			wantDirective: cobra.ShellCompDirectiveNoFileComp,
			wantPlugins:   []string{}, // KubernetesPlugin skipped, so empty
		},
		{
			name: "error path - plugin-dir flag does not exist",
			setupCmd: func(t *testing.T) *cobra.Command {
				// Create command without the plugin-dir flag
				cmd := &cobra.Command{
					Use: "test",
				}
				// Don't add any flags - GetString will fail
				return cmd
			},
			args:          []string{},
			toComplete:    "",
			wantDirective: cobra.ShellCompDirectiveError,
			wantPlugins:   nil,
		},
		{
			name: "error path - skip-plugins flag does not exist",
			setupCmd: func(t *testing.T) *cobra.Command {
				// Create command with only plugin-dir flag
				tempDir := t.TempDir()
				pluginDir := filepath.Join(tempDir, "plugins")
				if err := os.MkdirAll(pluginDir, 0755); err != nil {
					t.Fatalf("failed to create plugin dir: %v", err)
				}

				cmd := &cobra.Command{
					Use: "test",
				}
				// Add only plugin-dir flag, skip-plugins will fail
				cmd.Flags().String("plugin-dir", pluginDir, "")

				return cmd
			},
			args:          []string{},
			toComplete:    "",
			wantDirective: cobra.ShellCompDirectiveError,
			wantPlugins:   nil,
		},
		{
			name: "error path - plugin loading fails with invalid directory",
			setupCmd: func(t *testing.T) *cobra.Command {
				// Use a file as plugin-dir to cause an error
				tempDir := t.TempDir()
				invalidPath := filepath.Join(tempDir, "not-a-dir")

				// Create a file instead of directory
				if err := os.WriteFile(invalidPath, []byte("test"), 0644); err != nil {
					t.Fatalf("failed to create file: %v", err)
				}

				cmd := &cobra.Command{
					Use: "test",
				}
				cmd.Flags().String("plugin-dir", invalidPath, "")
				cmd.Flags().StringSlice("skip-plugins", []string{}, "")

				return cmd
			},
			args:          []string{},
			toComplete:    "",
			wantDirective: cobra.ShellCompDirectiveError,
			wantPlugins:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.setupCmd(t)

			// Get the completion function
			f := &flags.GlobalFlags{}
			log := logrus.New()
			log.SetOutput(os.Stderr)

			completionFunc := getPluginCompletions(f)

			// Call the completion function
			plugins, directive := completionFunc(cmd, tt.args, tt.toComplete)

			// Check directive
			assert.Equal(t, tt.wantDirective, directive, "unexpected directive")

			// Check plugins
			if tt.checkContains {
				// Check that all expected plugins are present
				pluginSet := make(map[string]bool)
				for _, p := range plugins {
					pluginSet[p] = true
				}
				for _, expected := range tt.wantPlugins {
					assert.True(t, pluginSet[expected], "expected plugin %q not found in results", expected)
				}
			} else {
				assert.Equal(t, tt.wantPlugins, plugins, "unexpected plugin list")
			}
		})
	}
}

func TestGetPluginCompletions_Integration(t *testing.T) {
	// Integration test with real plugin directory structure
	tempDir := t.TempDir()
	pluginDir := filepath.Join(tempDir, "plugins")
	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	f := &flags.GlobalFlags{}

	t.Run("default plugin is included", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().String("plugin-dir", pluginDir, "")
		cmd.Flags().StringSlice("skip-plugins", []string{}, "")

		completionFunc := getPluginCompletions(f)
		plugins, directive := completionFunc(cmd, []string{}, "")

		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
		assert.Contains(t, plugins, "KubernetesPlugin", "default plugin should be included")
	})

	t.Run("skip-plugins filters results", func(t *testing.T) {
		cmd := &cobra.Command{Use: "test"}
		cmd.Flags().String("plugin-dir", pluginDir, "")
		cmd.Flags().StringSlice("skip-plugins", []string{"KubernetesPlugin"}, "")

		completionFunc := getPluginCompletions(f)
		plugins, directive := completionFunc(cmd, []string{}, "")

		assert.Equal(t, cobra.ShellCompDirectiveNoFileComp, directive)
		assert.NotContains(t, plugins, "KubernetesPlugin", "skipped plugin should not be included")
	})
}
