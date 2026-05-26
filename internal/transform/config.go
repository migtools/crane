package transform

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var stageTokenRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// unknownConfigFieldRegex extracts the line number and field name from strict
// YAML decode errors emitted by yaml.v3 KnownFields mode.
var unknownConfigFieldRegex = regexp.MustCompile(`line ([0-9]+): field ([^ ]+) not found in type .*ConfigFile`)

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
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %q: %s: %w", path, friendlyConfigDecodeError(err), err)
	}
	if err := ValidateConfig(cfg); err != nil {
		return nil, fmt.Errorf("invalid config file %q: %w", path, err)
	}
	return cfg, nil

}

// friendlyConfigDecodeError converts strict YAML decode errors into clearer,
// user-facing messages for unknown top-level keys.
func friendlyConfigDecodeError(err error) string {
	msg := strings.TrimSpace(err.Error())
	matches := unknownConfigFieldRegex.FindStringSubmatch(msg)
	if len(matches) == 3 {
		return fmt.Sprintf("line %s: unknown field %q (supported top-level keys: stages)", matches[1], matches[2])
	}
	return msg
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
