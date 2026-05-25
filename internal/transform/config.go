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
