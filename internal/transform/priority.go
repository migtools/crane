package transform

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// PriorityAssignment represents a plugin-to-priority mapping
type PriorityAssignment struct {
	PluginName string
	Priority   int
	StageName  string // e.g., "10_kubernetes"
}

// AutoAssignPriorities generates priority assignments from discovered stages
// Stage priority (from directory name) becomes plugin priority
// Example: 10_kubernetes -> plugin "kubernetes" gets priority 10
func AutoAssignPriorities(stages []Stage) map[string]int {
	priorities := make(map[string]int)

	for _, stage := range stages {
		// Use stage priority as plugin priority
		priorities[stage.PluginName] = stage.Priority
	}

	return priorities
}

// AutoAssignPrioritiesFromDir discovers stages and assigns priorities
func AutoAssignPrioritiesFromDir(transformDir string) (map[string]int, error) {
	stages, err := DiscoverStages(transformDir)
	if err != nil {
		return nil, fmt.Errorf("failed to discover stages: %w", err)
	}

	if len(stages) == 0 {
		// No stages found - return empty map
		return make(map[string]int), nil
	}

	return AutoAssignPriorities(stages), nil
}

// MergePriorities merges user-specified priorities with auto-assigned priorities
// User priorities take precedence over auto-assigned ones
func MergePriorities(userPriorities, autoPriorities map[string]int) map[string]int {
	merged := make(map[string]int)

	// Start with auto-assigned priorities
	for plugin, priority := range autoPriorities {
		merged[plugin] = priority
	}

	// Override with user-specified priorities
	for plugin, priority := range userPriorities {
		merged[plugin] = priority
	}

	return merged
}

// SuggestStageName suggests a stage directory name for a plugin
// Uses existing stages to find the next available priority slot
func SuggestStageName(transformDir, pluginName string) (string, error) {
	stages, err := DiscoverStages(transformDir)
	if err != nil {
		return "", fmt.Errorf("failed to discover stages: %w", err)
	}

	if len(stages) == 0 {
		// No stages exist - suggest priority 10
		return GenerateStageName(10, pluginName), nil
	}

	// Find gaps in priority sequence
	priorities := make([]int, 0, len(stages))
	for _, stage := range stages {
		priorities = append(priorities, stage.Priority)
	}
	sort.Ints(priorities)

	// Look for significant gaps (gap > 5)
	for i := 0; i < len(priorities)-1; i++ {
		gap := priorities[i+1] - priorities[i]
		if gap > 5 {
			// Found a significant gap - use middle priority
			suggestedPriority := priorities[i] + gap/2
			return GenerateStageName(suggestedPriority, pluginName), nil
		}
	}

	// No gaps found - append after last stage with increment of 10
	lastPriority := priorities[len(priorities)-1]
	suggestedPriority := lastPriority + 10
	return GenerateStageName(suggestedPriority, pluginName), nil
}

// ParsePluginPrioritiesFromStrings parses plugin priorities from string slice
// Format: "plugin1:10", "plugin2:20"
func ParsePluginPrioritiesFromStrings(priorityStrings []string) (map[string]int, error) {
	priorities := make(map[string]int)

	for i, s := range priorityStrings {
		parts := strings.SplitN(s, ":", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid priority format at index %d: expected 'plugin:priority', got '%s'", i, s)
		}

		pluginName := strings.TrimSpace(parts[0])
		priorityStr := strings.TrimSpace(parts[1])

		if pluginName == "" {
			return nil, fmt.Errorf("empty plugin name at index %d", i)
		}

		priority, err := strconv.Atoi(priorityStr)
		if err != nil {
			return nil, fmt.Errorf("invalid priority value for plugin '%s': %w", pluginName, err)
		}

		priorities[pluginName] = priority
	}

	return priorities, nil
}

// GetPriorityAssignments returns all plugin priority assignments with stage info
func GetPriorityAssignments(transformDir string) ([]PriorityAssignment, error) {
	stages, err := DiscoverStages(transformDir)
	if err != nil {
		return nil, fmt.Errorf("failed to discover stages: %w", err)
	}

	assignments := make([]PriorityAssignment, 0, len(stages))
	for _, stage := range stages {
		assignments = append(assignments, PriorityAssignment{
			PluginName: stage.PluginName,
			Priority:   stage.Priority,
			StageName:  stage.DirName,
		})
	}

	return assignments, nil
}

// ValidatePriorityAssignments checks for conflicts in priority assignments
func ValidatePriorityAssignments(assignments []PriorityAssignment) error {
	// Check for duplicate priorities
	priorityMap := make(map[int][]string)
	for _, a := range assignments {
		priorityMap[a.Priority] = append(priorityMap[a.Priority], a.PluginName)
	}

	conflicts := []string{}
	for priority, plugins := range priorityMap {
		if len(plugins) > 1 {
			conflicts = append(conflicts, fmt.Sprintf("priority %d assigned to multiple plugins: %v", priority, plugins))
		}
	}

	if len(conflicts) > 0 {
		return fmt.Errorf("priority conflicts detected:\n  %s", strings.Join(conflicts, "\n  "))
	}

	return nil
}

// RecommendPriorityOrder recommends priority ordering based on plugin types
// This is a heuristic-based recommendation for common plugin patterns
func RecommendPriorityOrder(pluginNames []string) []PriorityAssignment {
	// Define priority tiers
	type tierRule struct {
		keywords []string
		priority int
	}

	tiers := []tierRule{
		{[]string{"kubernetes", "k8s", "core"}, 10},
		{[]string{"openshift", "ocp"}, 20},
		{[]string{"namespace", "project"}, 30},
		{[]string{"security", "scc", "psp"}, 40},
		{[]string{"network", "route", "ingress"}, 50},
		{[]string{"storage", "pvc", "pv"}, 60},
		{[]string{"image", "imagestream", "registry"}, 70},
		{[]string{"build", "buildconfig", "shipwright"}, 80},
		{[]string{"custom", "app", "application"}, 90},
	}

	assignments := make([]PriorityAssignment, 0, len(pluginNames))
	unmatchedPriority := 100

	for _, pluginName := range pluginNames {
		matched := false
		lowerPlugin := strings.ToLower(pluginName)

		// Try to match against tier rules
		for _, tier := range tiers {
			for _, keyword := range tier.keywords {
				if strings.Contains(lowerPlugin, keyword) {
					assignments = append(assignments, PriorityAssignment{
						PluginName: pluginName,
						Priority:   tier.priority,
						StageName:  GenerateStageName(tier.priority, pluginName),
					})
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}

		// If no match found, assign to end with incrementing priority
		if !matched {
			assignments = append(assignments, PriorityAssignment{
				PluginName: pluginName,
				Priority:   unmatchedPriority,
				StageName:  GenerateStageName(unmatchedPriority, pluginName),
			})
			unmatchedPriority += 10
		}
	}

	return assignments
}
