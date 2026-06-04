package transform

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

// Stage represents a transform stage in the multi-stage pipeline
type Stage struct {
	Priority   int
	PluginName string
	DirName    string
	Path       string
}

// DiscoverStages scans transform directory for stage subdirectories
// and returns them sorted by priority (numeric prefix)
func DiscoverStages(transformDir string) ([]Stage, error) {
	// Pattern: <number>_<pluginName>
	pattern := regexp.MustCompile(`^([0-9]+)_([a-zA-Z0-9_-]+)$`)

	// Read transform directory
	entries, err := os.ReadDir(transformDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Transform directory doesn't exist yet - return empty list
			return []Stage{}, nil
		}
		return nil, fmt.Errorf("failed to read transform directory: %w", err)
	}

	var stages []Stage

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		// Match stage directory pattern
		matches := pattern.FindStringSubmatch(entry.Name())
		if matches == nil {
			// Not a stage directory - skip
			continue
		}

		priority, err := strconv.Atoi(matches[1])
		if err != nil {
			// Should not happen due to regex, but handle gracefully
			continue
		}

		pluginName := matches[2]

		stages = append(stages, Stage{
			Priority:   priority,
			PluginName: pluginName,
			DirName:    entry.Name(),
			Path:       filepath.Join(transformDir, entry.Name()),
		})
	}

	// Sort by priority ascending
	sort.Slice(stages, func(i, j int) bool {
		return stages[i].Priority < stages[j].Priority
	})

	return stages, nil
}

// FilterStages filters stages based on selectors
// StageSelector specifies which stage(s) to run
// If Stages is empty, all stages are run
type StageSelector struct {
	Stages []string // Specific stages to run (empty = all stages)
}

// FilterStages applies selector to stage list
// Matches stages by directory name OR plugin name
func FilterStages(allStages []Stage, selector StageSelector) []Stage {
	// If no stages specified, return all stages
	if len(selector.Stages) == 0 {
		return allStages
	}

	var filtered []Stage
	seen := make(map[string]bool) // Prevent duplicates

	for _, requested := range selector.Stages {
		for _, stage := range allStages {
			// Skip if already added
			if seen[stage.DirName] {
				continue
			}

			// Match by directory name OR plugin name
			if stage.DirName == requested || stage.PluginName == requested {
				filtered = append(filtered, stage)
				seen[stage.DirName] = true
				break
			}
		}
	}

	return filtered
}

// GetFirstStage returns the stage with the lowest priority
func GetFirstStage(stages []Stage) *Stage {
	if len(stages) == 0 {
		return nil
	}

	// Stages are already sorted by priority
	return &stages[0]
}

// GetLastStage returns the stage with the highest priority
func GetLastStage(stages []Stage) *Stage {
	if len(stages) == 0 {
		return nil
	}

	// Stages are already sorted by priority
	return &stages[len(stages)-1]
}

// GetPreviousStage returns the stage that comes before the given stage
func GetPreviousStage(stages []Stage, current Stage) *Stage {
	for i, stage := range stages {
		if stage.DirName == current.DirName && i > 0 {
			return &stages[i-1]
		}
	}
	return nil
}

// GetNextStage returns the stage that comes after the given stage
func GetNextStage(stages []Stage, current Stage) *Stage {
	for i, stage := range stages {
		if stage.DirName == current.DirName && i < len(stages)-1 {
			return &stages[i+1]
		}
	}
	return nil
}

// ValidateStageName validates that a stage name follows the required pattern
func ValidateStageName(name string) error {
	pattern := regexp.MustCompile(`^([0-9]+)_([a-zA-Z0-9_-]+)$`)
	if !pattern.MatchString(name) {
		return fmt.Errorf("invalid stage name '%s': must match pattern '<number>_<pluginName>'", name)
	}
	return nil
}

// GenerateStageName generates a stage directory name from priority and plugin name
func GenerateStageName(priority int, pluginName string) string {
	return fmt.Sprintf("%d_%s", priority, pluginName)
}
