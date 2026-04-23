package transform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverStages(t *testing.T) {
	tempDir := t.TempDir()

	// Create stage directories
	stageDirs := []string{
		"10_KubernetesPlugin",
		"20_OpenshiftPlugin",
		"30_ImagestreamPlugin",
		"15_CustomTweaks", // Out of order to test sorting
	}

	for _, dir := range stageDirs {
		err := os.Mkdir(filepath.Join(tempDir, dir), 0755)
		require.NoError(t, err)
	}

	// Create a non-stage directory (should be ignored)
	err := os.Mkdir(filepath.Join(tempDir, "not-a-stage"), 0755)
	require.NoError(t, err)

	// Create a file (should be ignored)
	err = os.WriteFile(filepath.Join(tempDir, "file.txt"), []byte("test"), 0644)
	require.NoError(t, err)

	// Discover stages
	stages, err := DiscoverStages(tempDir)
	require.NoError(t, err)

	// Should have 4 stages (excluding non-stage directory and file)
	assert.Len(t, stages, 4)

	// Verify stages are sorted by priority
	assert.Equal(t, 10, stages[0].Priority)
	assert.Equal(t, "KubernetesPlugin", stages[0].PluginName)
	assert.Equal(t, "10_KubernetesPlugin", stages[0].DirName)

	assert.Equal(t, 15, stages[1].Priority)
	assert.Equal(t, "CustomTweaks", stages[1].PluginName)

	assert.Equal(t, 20, stages[2].Priority)
	assert.Equal(t, "OpenshiftPlugin", stages[2].PluginName)

	assert.Equal(t, 30, stages[3].Priority)
	assert.Equal(t, "ImagestreamPlugin", stages[3].PluginName)
}

func TestDiscoverStagesNonexistentDir(t *testing.T) {
	stages, err := DiscoverStages("/nonexistent/path")
	require.NoError(t, err)
	assert.Empty(t, stages, "should return empty list for nonexistent directory")
}

func TestFilterStages(t *testing.T) {
	stages := []Stage{
		{Priority: 10, PluginName: "KubernetesPlugin", DirName: "10_KubernetesPlugin"},
		{Priority: 20, PluginName: "OpenshiftPlugin", DirName: "20_OpenshiftPlugin"},
		{Priority: 30, PluginName: "ImagestreamPlugin", DirName: "30_ImagestreamPlugin"},
	}

	t.Run("no selectors - return all", func(t *testing.T) {
		filtered := FilterStages(stages, StageSelector{})
		assert.Equal(t, stages, filtered)
	})

	t.Run("specific stage", func(t *testing.T) {
		filtered := FilterStages(stages, StageSelector{Stage: "20_OpenshiftPlugin"})
		require.Len(t, filtered, 1)
		assert.Equal(t, "OpenshiftPlugin", filtered[0].PluginName)
	})

	t.Run("non-existent stage", func(t *testing.T) {
		filtered := FilterStages(stages, StageSelector{Stage: "99_NonExistent"})
		require.Len(t, filtered, 0)
	})
}

func TestGetFirstStage(t *testing.T) {
	stages := []Stage{
		{Priority: 10, PluginName: "KubernetesPlugin", DirName: "10_KubernetesPlugin"},
		{Priority: 20, PluginName: "OpenshiftPlugin", DirName: "20_OpenshiftPlugin"},
		{Priority: 30, PluginName: "ImagestreamPlugin", DirName: "30_ImagestreamPlugin"},
	}

	first := GetFirstStage(stages)
	require.NotNil(t, first)
	assert.Equal(t, 10, first.Priority)
	assert.Equal(t, "KubernetesPlugin", first.PluginName)
}

func TestGetFirstStageEmpty(t *testing.T) {
	first := GetFirstStage([]Stage{})
	assert.Nil(t, first)
}

func TestGetLastStage(t *testing.T) {
	stages := []Stage{
		{Priority: 10, PluginName: "KubernetesPlugin", DirName: "10_KubernetesPlugin"},
		{Priority: 20, PluginName: "OpenshiftPlugin", DirName: "20_OpenshiftPlugin"},
		{Priority: 30, PluginName: "ImagestreamPlugin", DirName: "30_ImagestreamPlugin"},
	}

	last := GetLastStage(stages)
	require.NotNil(t, last)
	assert.Equal(t, 30, last.Priority)
	assert.Equal(t, "ImagestreamPlugin", last.PluginName)
}

func TestGetPreviousStage(t *testing.T) {
	stages := []Stage{
		{Priority: 10, PluginName: "KubernetesPlugin", DirName: "10_KubernetesPlugin"},
		{Priority: 20, PluginName: "OpenshiftPlugin", DirName: "20_OpenshiftPlugin"},
		{Priority: 30, PluginName: "ImagestreamPlugin", DirName: "30_ImagestreamPlugin"},
	}

	prev := GetPreviousStage(stages, stages[1])
	require.NotNil(t, prev)
	assert.Equal(t, "KubernetesPlugin", prev.PluginName)

	prevFirst := GetPreviousStage(stages, stages[0])
	assert.Nil(t, prevFirst, "first stage should have no previous")
}

func TestGetNextStage(t *testing.T) {
	stages := []Stage{
		{Priority: 10, PluginName: "KubernetesPlugin", DirName: "10_KubernetesPlugin"},
		{Priority: 20, PluginName: "OpenshiftPlugin", DirName: "20_OpenshiftPlugin"},
		{Priority: 30, PluginName: "ImagestreamPlugin", DirName: "30_ImagestreamPlugin"},
	}

	next := GetNextStage(stages, stages[0])
	require.NotNil(t, next)
	assert.Equal(t, "OpenshiftPlugin", next.PluginName)

	nextLast := GetNextStage(stages, stages[2])
	assert.Nil(t, nextLast, "last stage should have no next")
}

func TestValidateStageName(t *testing.T) {
	validNames := []string{
		"10_KubernetesPlugin",
		"20_OpenshiftPlugin",
		"100_CustomPlugin",
		"5_TestPlugin",
	}

	for _, name := range validNames {
		t.Run("valid_"+name, func(t *testing.T) {
			err := ValidateStageName(name)
			assert.NoError(t, err)
		})
	}

	invalidNames := []string{
		"KubernetesPlugin_10", // Wrong order
		"10-KubernetesPlugin", // Dash instead of underscore
		"10_Kubernetes:Fix",   // Colon not allowed
		"test",                // No priority
		"_10_KubernetesPlugin", // Leading underscore
	}

	for _, name := range invalidNames {
		t.Run("invalid_"+name, func(t *testing.T) {
			err := ValidateStageName(name)
			assert.Error(t, err)
		})
	}
}

func TestGenerateStageName(t *testing.T) {
	tests := []struct {
		priority   int
		pluginName string
		expected   string
	}{
		{10, "KubernetesPlugin", "10_KubernetesPlugin"},
		{20, "OpenshiftPlugin", "20_OpenshiftPlugin"},
		{100, "CustomPlugin", "100_CustomPlugin"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := GenerateStageName(tt.priority, tt.pluginName)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestFilterPluginsByStage_NonExistentPlugin verifies behavior when stage name
// doesn't match any known plugin
func TestFilterPluginsByStage_NonExistentPlugin(t *testing.T) {
	// Test that stages with non-existent plugin names are handled correctly
	// This is tested through orchestrator integration tests
	stages := []Stage{
		{Priority: 10, PluginName: "KubernetesPlugin", DirName: "10_KubernetesPlugin"},
		{Priority: 50, PluginName: "NonExistentPlugin", DirName: "50_NonExistentPlugin"},
		{Priority: 90, PluginName: "ManualStage", DirName: "90_ManualStage"},
	}

	t.Run("filters stages correctly", func(t *testing.T) {
		// Stage selection should still work with non-existent plugin names
		filtered := FilterStages(stages, StageSelector{Stage: "50_NonExistentPlugin"})
		require.Len(t, filtered, 1)
		assert.Equal(t, "NonExistentPlugin", filtered[0].PluginName)
	})

	t.Run("manual stage can be selected", func(t *testing.T) {
		// Manual stages (non-plugin names) can still be selected
		filtered := FilterStages(stages, StageSelector{Stage: "90_ManualStage"})
		require.Len(t, filtered, 1)
		assert.Equal(t, "ManualStage", filtered[0].PluginName)
	})
}
