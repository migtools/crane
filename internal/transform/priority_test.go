package transform

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoAssignPriorities(t *testing.T) {
	stages := []Stage{
		{Priority: 10, PluginName: "kubernetes", DirName: "10_kubernetes"},
		{Priority: 20, PluginName: "openshift", DirName: "20_openshift"},
		{Priority: 30, PluginName: "imagestream", DirName: "30_imagestream"},
	}

	priorities := AutoAssignPriorities(stages)

	assert.Equal(t, 10, priorities["kubernetes"])
	assert.Equal(t, 20, priorities["openshift"])
	assert.Equal(t, 30, priorities["imagestream"])
	assert.Len(t, priorities, 3)
}

func TestAutoAssignPrioritiesFromDir(t *testing.T) {
	tempDir := t.TempDir()

	// Create stage directories
	stageDirs := []string{
		"10_kubernetes",
		"20_openshift",
		"30_imagestream",
	}

	for _, dir := range stageDirs {
		err := os.Mkdir(filepath.Join(tempDir, dir), 0755)
		require.NoError(t, err)
	}

	priorities, err := AutoAssignPrioritiesFromDir(tempDir)
	require.NoError(t, err)

	assert.Equal(t, 10, priorities["kubernetes"])
	assert.Equal(t, 20, priorities["openshift"])
	assert.Equal(t, 30, priorities["imagestream"])
}

func TestAutoAssignPrioritiesFromDirEmpty(t *testing.T) {
	tempDir := t.TempDir()

	priorities, err := AutoAssignPrioritiesFromDir(tempDir)
	require.NoError(t, err)
	assert.Empty(t, priorities)
}

func TestMergePriorities(t *testing.T) {
	auto := map[string]int{
		"kubernetes":  10,
		"openshift":   20,
		"imagestream": 30,
	}

	user := map[string]int{
		"openshift": 15, // Override
		"custom":    40, // New
	}

	merged := MergePriorities(user, auto)

	assert.Equal(t, 10, merged["kubernetes"], "auto-assigned priority preserved")
	assert.Equal(t, 15, merged["openshift"], "user priority overrides auto")
	assert.Equal(t, 30, merged["imagestream"], "auto-assigned priority preserved")
	assert.Equal(t, 40, merged["custom"], "user priority added")
	assert.Len(t, merged, 4)
}

func TestSuggestStageNameNoStages(t *testing.T) {
	tempDir := t.TempDir()

	stageName, err := SuggestStageName(tempDir, "myplugin")
	require.NoError(t, err)
	assert.Equal(t, "10_myplugin", stageName)
}

func TestSuggestStageNameWithGap(t *testing.T) {
	tempDir := t.TempDir()

	// Create stages with a gap: 10, 30 (gap at 20)
	err := os.Mkdir(filepath.Join(tempDir, "10_kubernetes"), 0755)
	require.NoError(t, err)
	err = os.Mkdir(filepath.Join(tempDir, "30_imagestream"), 0755)
	require.NoError(t, err)

	stageName, err := SuggestStageName(tempDir, "openshift")
	require.NoError(t, err)
	assert.Equal(t, "20_openshift", stageName, "should fill the gap")
}

func TestSuggestStageNameAppend(t *testing.T) {
	tempDir := t.TempDir()

	// Create stages with increment of 5: 10, 15, 20 (no gaps)
	stages := []string{"10_kubernetes", "15_openshift", "20_imagestream"}
	for _, stage := range stages {
		err := os.Mkdir(filepath.Join(tempDir, stage), 0755)
		require.NoError(t, err)
	}

	stageName, err := SuggestStageName(tempDir, "custom")
	require.NoError(t, err)
	assert.Equal(t, "30_custom", stageName, "should append after last stage")
}

func TestParsePluginPrioritiesFromStrings(t *testing.T) {
	t.Run("valid input", func(t *testing.T) {
		input := []string{
			"kubernetes:10",
			"openshift:20",
			"imagestream:30",
		}

		priorities, err := ParsePluginPrioritiesFromStrings(input)
		require.NoError(t, err)

		assert.Equal(t, 10, priorities["kubernetes"])
		assert.Equal(t, 20, priorities["openshift"])
		assert.Equal(t, 30, priorities["imagestream"])
	})

	t.Run("invalid format - no colon", func(t *testing.T) {
		input := []string{"kubernetes10"}
		_, err := ParsePluginPrioritiesFromStrings(input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid priority format")
	})

	t.Run("invalid format - empty plugin name", func(t *testing.T) {
		input := []string{":10"}
		_, err := ParsePluginPrioritiesFromStrings(input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty plugin name")
	})

	t.Run("invalid format - non-numeric priority", func(t *testing.T) {
		input := []string{"kubernetes:abc"}
		_, err := ParsePluginPrioritiesFromStrings(input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid priority value")
	})

	t.Run("with whitespace", func(t *testing.T) {
		input := []string{
			" kubernetes : 10 ",
			"openshift: 20",
		}

		priorities, err := ParsePluginPrioritiesFromStrings(input)
		require.NoError(t, err)

		assert.Equal(t, 10, priorities["kubernetes"])
		assert.Equal(t, 20, priorities["openshift"])
	})
}

func TestGetPriorityAssignments(t *testing.T) {
	tempDir := t.TempDir()

	// Create stages
	stages := []string{"10_kubernetes", "20_openshift"}
	for _, stage := range stages {
		err := os.Mkdir(filepath.Join(tempDir, stage), 0755)
		require.NoError(t, err)
	}

	assignments, err := GetPriorityAssignments(tempDir)
	require.NoError(t, err)

	require.Len(t, assignments, 2)
	assert.Equal(t, "kubernetes", assignments[0].PluginName)
	assert.Equal(t, 10, assignments[0].Priority)
	assert.Equal(t, "10_kubernetes", assignments[0].StageName)

	assert.Equal(t, "openshift", assignments[1].PluginName)
	assert.Equal(t, 20, assignments[1].Priority)
	assert.Equal(t, "20_openshift", assignments[1].StageName)
}

func TestValidatePriorityAssignments(t *testing.T) {
	t.Run("no conflicts", func(t *testing.T) {
		assignments := []PriorityAssignment{
			{PluginName: "kubernetes", Priority: 10},
			{PluginName: "openshift", Priority: 20},
		}

		err := ValidatePriorityAssignments(assignments)
		assert.NoError(t, err)
	})

	t.Run("duplicate priorities", func(t *testing.T) {
		assignments := []PriorityAssignment{
			{PluginName: "kubernetes", Priority: 10},
			{PluginName: "openshift", Priority: 10},
		}

		err := ValidatePriorityAssignments(assignments)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "priority conflicts")
		assert.Contains(t, err.Error(), "priority 10")
	})
}

func TestRecommendPriorityOrder(t *testing.T) {
	pluginNames := []string{
		"imagestream-plugin",
		"custom-app",
		"kubernetes-base",
		"openshift-specific",
		"network-routes",
	}

	assignments := RecommendPriorityOrder(pluginNames)

	// Verify assignments
	require.Len(t, assignments, 5)

	// Check that kubernetes comes first (priority 10)
	var kubernetesAssignment *PriorityAssignment
	for i := range assignments {
		if assignments[i].PluginName == "kubernetes-base" {
			kubernetesAssignment = &assignments[i]
			break
		}
	}
	require.NotNil(t, kubernetesAssignment)
	assert.Equal(t, 10, kubernetesAssignment.Priority)
	assert.Equal(t, "10_kubernetes-base", kubernetesAssignment.StageName)

	// Check that openshift comes second (priority 20)
	var openshiftAssignment *PriorityAssignment
	for i := range assignments {
		if assignments[i].PluginName == "openshift-specific" {
			openshiftAssignment = &assignments[i]
			break
		}
	}
	require.NotNil(t, openshiftAssignment)
	assert.Equal(t, 20, openshiftAssignment.Priority)

	// Check that imagestream is assigned (priority 70)
	var imagestreamAssignment *PriorityAssignment
	for i := range assignments {
		if assignments[i].PluginName == "imagestream-plugin" {
			imagestreamAssignment = &assignments[i]
			break
		}
	}
	require.NotNil(t, imagestreamAssignment)
	assert.Equal(t, 70, imagestreamAssignment.Priority)

	// Check that network is assigned (priority 50)
	var networkAssignment *PriorityAssignment
	for i := range assignments {
		if assignments[i].PluginName == "network-routes" {
			networkAssignment = &assignments[i]
			break
		}
	}
	require.NotNil(t, networkAssignment)
	assert.Equal(t, 50, networkAssignment.Priority)

	// Check that custom app gets a default priority (100+)
	var customAssignment *PriorityAssignment
	for i := range assignments {
		if assignments[i].PluginName == "custom-app" {
			customAssignment = &assignments[i]
			break
		}
	}
	require.NotNil(t, customAssignment)
	assert.Equal(t, 90, customAssignment.Priority) // custom keyword matches tier
}
