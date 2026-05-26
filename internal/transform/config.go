package transform

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var stageTokenRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type ConfigFile struct {
	Stages []string `yaml:"stages"`
}

// LoadConfig reads a transform config file from disk, parses YAML, and validates
// the resulting structure before returning it.
func LoadConfig(path string) (*ConfigFile, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("config file path is required")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %q: %w", path, err)
	}

	cfg := &ConfigFile{}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %q: %w", path, err)
	}
	if err := ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid config file %q: %w", path, err)
	}
	return cfg, nil

}

// ValidateConfig validates the config schema and stage token rules.
// It also trims stage entries in place for normalized downstream usage.
func ValidateConfig(cfg *ConfigFile) error {
	if cfg == nil {
		return fmt.Errorf("config file is required")
	}

	if len(cfg.Stages) == 0 {
		return fmt.Errorf("config file must contain at least one stage")
	}

	seen := make(map[string]struct{}, len(cfg.Stages))

	for i, s := range cfg.Stages {
		stage := strings.TrimSpace(s)
		if stage == "" {
			return fmt.Errorf("stage at index %d is empty", i)
		}

		if !stageTokenRegex.MatchString(stage) {
			return fmt.Errorf("stage %q contains invalid characters (allowed: letters, digits, '_' and '-')", stage)
		}

		if _, exists := seen[stage]; exists {
			return fmt.Errorf("duplicate stage %q", stage)
		}

		seen[stage] = struct{}{}

		cfg.Stages[i] = stage
	}
	return nil
}

// GenerateStageDirNames converts ordered stage tokens into deterministic stage
// directory names using 10-step numeric prefixes (10_, 20_, 30_, ...).
func GenerateStageDirNames(stageTokens []string) []string {
	stageNames := make([]string, 0, len(stageTokens))
	for i, token := range stageTokens {
		priority := 10 + (i * 10)
		stageNames = append(stageNames, fmt.Sprintf("%d_%s", priority, token))
	}
	return stageNames
}
