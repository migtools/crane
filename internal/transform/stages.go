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
// Selectors can be: specific stage, from-stage, to-stage, or list of stages
type StageSelector struct {
	Stage     string   // Specific stage to run
	FromStage string   // Run from this stage to end
	ToStage   string   // Run from start to this stage
	Stages    []string // Specific list of stages to run
}

// FilterStages applies selectors to stage list
func FilterStages(allStages []Stage, selector StageSelector) []Stage {
	// If no selectors specified, return all stages
	if selector.Stage == "" && selector.FromStage == "" && selector.ToStage == "" && len(selector.Stages) == 0 {
		return allStages
	}

	// If specific stage is selected
	if selector.Stage != "" {
		for _, stage := range allStages {
			if stage.DirName == selector.Stage {
				return []Stage{stage}
			}
		}
		return []Stage{}
	}

	// If specific stages list is provided
	if len(selector.Stages) > 0 {
		stageMap := make(map[string]bool)
		for _, s := range selector.Stages {
			stageMap[s] = true
		}

		var filtered []Stage
		for _, stage := range allStages {
			if stageMap[stage.DirName] {
				filtered = append(filtered, stage)
			}
		}
		return filtered
	}

	// If from-stage or to-stage is specified
	var filtered []Stage
	inRange := selector.FromStage == "" // Start collecting if no from-stage
	foundFrom := selector.FromStage == "" // True if no from-stage specified
	foundTo := selector.ToStage == ""     // True if no to-stage specified

	for _, stage := range allStages {
		// Check if we should start collecting
		if selector.FromStage != "" && stage.DirName == selector.FromStage {
			inRange = true
			foundFrom = true
		}

		// Collect if in range
		if inRange {
			filtered = append(filtered, stage)
		}

		// Check if we should stop collecting
		if selector.ToStage != "" && stage.DirName == selector.ToStage {
			foundTo = true
			break
		}
	}

	// Validate that requested stages were found
	if selector.FromStage != "" && !foundFrom {
		return []Stage{} // FromStage not found
	}
	if selector.ToStage != "" && !foundTo {
		return []Stage{} // ToStage not found
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
