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
		"10_kubernetes",
		"20_openshift",
		"30_imagestream",
		"15_custom-tweaks", // Out of order to test sorting
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
	assert.Equal(t, "kubernetes", stages[0].PluginName)
	assert.Equal(t, "10_kubernetes", stages[0].DirName)

	assert.Equal(t, 15, stages[1].Priority)
	assert.Equal(t, "custom-tweaks", stages[1].PluginName)

	assert.Equal(t, 20, stages[2].Priority)
	assert.Equal(t, "openshift", stages[2].PluginName)

	assert.Equal(t, 30, stages[3].Priority)
	assert.Equal(t, "imagestream", stages[3].PluginName)
}

func TestDiscoverStagesNonexistentDir(t *testing.T) {
	stages, err := DiscoverStages("/nonexistent/path")
	require.NoError(t, err)
	assert.Empty(t, stages, "should return empty list for nonexistent directory")
}

func TestFilterStages(t *testing.T) {
	stages := []Stage{
		{Priority: 10, PluginName: "kubernetes", DirName: "10_kubernetes"},
		{Priority: 20, PluginName: "openshift", DirName: "20_openshift"},
		{Priority: 30, PluginName: "imagestream", DirName: "30_imagestream"},
	}

	t.Run("no selectors - return all", func(t *testing.T) {
		filtered := FilterStages(stages, StageSelector{})
		assert.Equal(t, stages, filtered)
	})

	t.Run("specific stage", func(t *testing.T) {
		filtered := FilterStages(stages, StageSelector{Stage: "20_openshift"})
		require.Len(t, filtered, 1)
		assert.Equal(t, "openshift", filtered[0].PluginName)
	})

	t.Run("from-stage", func(t *testing.T) {
		filtered := FilterStages(stages, StageSelector{FromStage: "20_openshift"})
		require.Len(t, filtered, 2)
		assert.Equal(t, "openshift", filtered[0].PluginName)
		assert.Equal(t, "imagestream", filtered[1].PluginName)
	})

	t.Run("to-stage", func(t *testing.T) {
		filtered := FilterStages(stages, StageSelector{ToStage: "20_openshift"})
		require.Len(t, filtered, 2)
		assert.Equal(t, "kubernetes", filtered[0].PluginName)
		assert.Equal(t, "openshift", filtered[1].PluginName)
	})

	t.Run("from-stage to to-stage", func(t *testing.T) {
		filtered := FilterStages(stages, StageSelector{
			FromStage: "10_kubernetes",
			ToStage:   "20_openshift",
		})
		require.Len(t, filtered, 2)
		assert.Equal(t, "kubernetes", filtered[0].PluginName)
		assert.Equal(t, "openshift", filtered[1].PluginName)
	})

	t.Run("specific stages list", func(t *testing.T) {
		filtered := FilterStages(stages, StageSelector{
			Stages: []string{"10_kubernetes", "30_imagestream"},
		})
		require.Len(t, filtered, 2)
		assert.Equal(t, "kubernetes", filtered[0].PluginName)
		assert.Equal(t, "imagestream", filtered[1].PluginName)
	})
}

func TestGetFirstStage(t *testing.T) {
	stages := []Stage{
		{Priority: 10, PluginName: "kubernetes", DirName: "10_kubernetes"},
		{Priority: 20, PluginName: "openshift", DirName: "20_openshift"},
		{Priority: 30, PluginName: "imagestream", DirName: "30_imagestream"},
	}

	first := GetFirstStage(stages)
	require.NotNil(t, first)
	assert.Equal(t, 10, first.Priority)
	assert.Equal(t, "kubernetes", first.PluginName)
}

func TestGetFirstStageEmpty(t *testing.T) {
	first := GetFirstStage([]Stage{})
	assert.Nil(t, first)
}

func TestGetLastStage(t *testing.T) {
	stages := []Stage{
		{Priority: 10, PluginName: "kubernetes", DirName: "10_kubernetes"},
		{Priority: 20, PluginName: "openshift", DirName: "20_openshift"},
		{Priority: 30, PluginName: "imagestream", DirName: "30_imagestream"},
	}

	last := GetLastStage(stages)
	require.NotNil(t, last)
	assert.Equal(t, 30, last.Priority)
	assert.Equal(t, "imagestream", last.PluginName)
}

func TestGetPreviousStage(t *testing.T) {
	stages := []Stage{
		{Priority: 10, PluginName: "kubernetes", DirName: "10_kubernetes"},
		{Priority: 20, PluginName: "openshift", DirName: "20_openshift"},
		{Priority: 30, PluginName: "imagestream", DirName: "30_imagestream"},
	}

	prev := GetPreviousStage(stages, stages[1])
	require.NotNil(t, prev)
	assert.Equal(t, "kubernetes", prev.PluginName)

	prevFirst := GetPreviousStage(stages, stages[0])
	assert.Nil(t, prevFirst, "first stage should have no previous")
}

func TestGetNextStage(t *testing.T) {
	stages := []Stage{
		{Priority: 10, PluginName: "kubernetes", DirName: "10_kubernetes"},
		{Priority: 20, PluginName: "openshift", DirName: "20_openshift"},
		{Priority: 30, PluginName: "imagestream", DirName: "30_imagestream"},
	}

	next := GetNextStage(stages, stages[0])
	require.NotNil(t, next)
	assert.Equal(t, "openshift", next.PluginName)

	nextLast := GetNextStage(stages, stages[2])
	assert.Nil(t, nextLast, "last stage should have no next")
}

func TestValidateStageName(t *testing.T) {
	validNames := []string{
		"10_kubernetes",
		"20_openshift",
		"100_custom-plugin",
		"5_test_plugin",
	}

	for _, name := range validNames {
		t.Run("valid_"+name, func(t *testing.T) {
			err := ValidateStageName(name)
			assert.NoError(t, err)
		})
	}

	invalidNames := []string{
		"kubernetes_10",     // Wrong order
		"10-kubernetes",     // Dash instead of underscore
		"10_kubernetes:fix", // Colon not allowed
		"test",              // No priority
		"_10_kubernetes",    // Leading underscore
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
		{10, "kubernetes", "10_kubernetes"},
		{20, "openshift", "20_openshift"},
		{100, "custom-plugin", "100_custom-plugin"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			result := GenerateStageName(tt.priority, tt.pluginName)
			assert.Equal(t, tt.expected, result)
		})
	}
}
