package listplugins

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestGetPluginNames(t *testing.T) {
	log := logrus.New()
	log.SetOutput(os.Stderr)

	tests := []struct {
		name         string
		setupPlugins func(t *testing.T, pluginDir string)
		skipPlugins  []string
		wantNames    []string
		wantErr      bool
	}{
		{
			name: "empty plugin directory returns only default plugin",
			setupPlugins: func(t *testing.T, pluginDir string) {
				// Create empty plugin directory
				if err := os.MkdirAll(pluginDir, 0755); err != nil {
					t.Fatalf("failed to create plugin dir: %v", err)
				}
			},
			skipPlugins: []string{},
			wantNames:   []string{"KubernetesPlugin"}, // Default plugin is always included
			wantErr:     false,
		},
		{
			name: "skip default plugin",
			setupPlugins: func(t *testing.T, pluginDir string) {
				if err := os.MkdirAll(pluginDir, 0755); err != nil {
					t.Fatalf("failed to create plugin dir: %v", err)
				}
			},
			skipPlugins: []string{"KubernetesPlugin"},
			wantNames:   []string{},
			wantErr:     false,
		},
		{
			name: "nonexistent plugin directory",
			setupPlugins: func(t *testing.T, pluginDir string) {
				// Don't create the directory
			},
			skipPlugins: []string{},
			wantNames:   []string{"KubernetesPlugin"}, // Default plugin is still included
			wantErr:     false,
		},
		{
			name: "plugin directory with mock plugins",
			setupPlugins: func(t *testing.T, pluginDir string) {
				if err := os.MkdirAll(pluginDir, 0755); err != nil {
					t.Fatalf("failed to create plugin dir: %v", err)
				}
				// Note: We can't easily test with real plugin binaries in unit tests
				// This test verifies the function works with an empty directory
				// Integration tests would be needed for testing with actual plugin binaries
			},
			skipPlugins: []string{},
			wantNames:   []string{"KubernetesPlugin"},
			wantErr:     false,
		},
		{
			name: "skip multiple plugins",
			setupPlugins: func(t *testing.T, pluginDir string) {
				if err := os.MkdirAll(pluginDir, 0755); err != nil {
					t.Fatalf("failed to create plugin dir: %v", err)
				}
			},
			skipPlugins: []string{"KubernetesPlugin", "NonExistentPlugin"},
			wantNames:   []string{},
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tempDir := t.TempDir()
			pluginDir := filepath.Join(tempDir, "plugins")

			// Setup plugins directory
			tt.setupPlugins(t, pluginDir)

			// Call GetPluginNames
			names, err := GetPluginNames(pluginDir, tt.skipPlugins, log)

			// Check error
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)

			// Check names
			assert.Equal(t, len(tt.wantNames), len(names), "expected %d plugins, got %d", len(tt.wantNames), len(names))

			// Check that expected names are present (order might differ)
			nameSet := make(map[string]bool)
			for _, name := range names {
				nameSet[name] = true
			}
			for _, expectedName := range tt.wantNames {
				assert.True(t, nameSet[expectedName], "expected plugin %q not found in result", expectedName)
			}
		})
	}
}

func TestGetPluginNames_RelativeAndAbsolutePaths(t *testing.T) {
	log := logrus.New()
	log.SetOutput(os.Stderr)

	tempDir := t.TempDir()
	pluginDir := filepath.Join(tempDir, "plugins")

	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	// Test with absolute path
	t.Run("absolute path", func(t *testing.T) {
		names, err := GetPluginNames(pluginDir, []string{}, log)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(names), 1, "should have at least default plugin")
	})

	// Test with relative path
	t.Run("relative path", func(t *testing.T) {
		// Change to temp dir and use relative path
		originalDir, err := os.Getwd()
		assert.NoError(t, err)
		defer os.Chdir(originalDir)

		err = os.Chdir(tempDir)
		assert.NoError(t, err)

		names, err := GetPluginNames("plugins", []string{}, log)
		assert.NoError(t, err)
		assert.GreaterOrEqual(t, len(names), 1, "should have at least default plugin")
	})
}

func TestGetPluginNames_Integration(t *testing.T) {
	// This is an integration test that verifies GetPluginNames works with
	// the actual plugin system. It requires the default KubernetesPlugin.
	log := logrus.New()
	log.SetOutput(os.Stderr)

	// Use a temporary directory to ensure clean state
	tempDir := t.TempDir()
	pluginDir := filepath.Join(tempDir, "plugins")

	if err := os.MkdirAll(pluginDir, 0755); err != nil {
		t.Fatalf("failed to create plugin dir: %v", err)
	}

	t.Run("default plugin is always included", func(t *testing.T) {
		names, err := GetPluginNames(pluginDir, []string{}, log)
		assert.NoError(t, err)
		assert.Contains(t, names, "KubernetesPlugin", "default KubernetesPlugin should be included")
	})

	t.Run("default plugin can be skipped", func(t *testing.T) {
		names, err := GetPluginNames(pluginDir, []string{"KubernetesPlugin"}, log)
		assert.NoError(t, err)
		assert.NotContains(t, names, "KubernetesPlugin", "KubernetesPlugin should be skipped")
	})
}
