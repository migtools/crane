package transform

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

var stageTokenRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

// unknownInstructionsFieldRegex extracts the line number and field name from strict
// YAML decode errors emitted by yaml.v3 KnownFields mode.
var unknownInstructionsFieldRegex = regexp.MustCompile(`line ([0-9]+): field ([^ ]+) not found in type .*InstructionsFile`)

// rootSequenceInstructionsRegex detects YAML where the root node is a sequence
// instead of an object containing top-level "stages".
var rootSequenceInstructionsRegex = regexp.MustCompile(`line ([0-9]+): cannot unmarshal !!seq into .*InstructionsFile`)

type InstructionsFile struct {
	Stages []string `yaml:"stages"`
}

// LoadInstructions reads a transform instructions file from disk, parses YAML, and validates
// the resulting structure before returning it.
func LoadInstructions(path string) (*InstructionsFile, error) {
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("instructions file path is required")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read instructions file %q: %w", path, err)
	}

	cfg := &InstructionsFile{}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil {
		return nil, fmt.Errorf("failed to parse instructions file %q: %s: %w", path, friendlyInstructionsDecodeError(err), err)
	}
	// Reject multi-document YAML; this instructions file format supports only a single document.
	var extra interface{}
	if err := decoder.Decode(&extra); err == nil {
		return nil, fmt.Errorf("invalid instructions file %q: only a single YAML document is allowed", path)
	} else if !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("invalid instructions file %q: only a single YAML document is allowed: %w", path, err)
	}
	if err := ValidateInstructions(cfg); err != nil {
		return nil, fmt.Errorf("invalid instructions file %q: %w", path, err)
	}
	return cfg, nil

}

// friendlyInstructionsDecodeError converts strict YAML decode errors into clearer,
// user-facing messages for unknown top-level keys.
func friendlyInstructionsDecodeError(err error) string {
	msg := strings.TrimSpace(err.Error())
	matches := unknownInstructionsFieldRegex.FindStringSubmatch(msg)
	if len(matches) == 3 {
		return fmt.Sprintf("line %s: unknown field %q (supported top-level keys: stages)", matches[1], matches[2])
	}
	matches = rootSequenceInstructionsRegex.FindStringSubmatch(msg)
	if len(matches) == 2 {
		return fmt.Sprintf("line %s: invalid root YAML type: expected a mapping with top-level key \"stages\" (example: stages: [KubernetesPlugin])", matches[1])
	}
	return msg
}

// ValidateInstructions validates the instructions schema and stage token rules.
// It also trims stage entries in place for normalized downstream usage.
func ValidateInstructions(cfg *InstructionsFile) error {
	if cfg == nil {
		return fmt.Errorf("instructions file is required")
	}

	if len(cfg.Stages) == 0 {
		return fmt.Errorf("instructions file must contain at least one stage")
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
