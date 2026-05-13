package kustomize

import (
	"fmt"
	"strings"
)

// AllowedKustomizeArgs contains whitelist of allowed kustomize arguments
var AllowedKustomizeArgs = map[string]bool{
	"--enable-alpha-plugins": true,
	"--enable-helm":          true,
	"--env":                  true,
	"-e":                     true,
	"--helm-command":         true,
	"--load-restrictor":      true,
}

// ParseAndValidateArgs parses and validates kustomize arguments
// Returns array of arguments ready for exec.Command
func ParseAndValidateArgs(argsString string) ([]string, error) {
	if argsString == "" {
		return nil, nil
	}

	// First check for dangerous characters in the entire string before parsing
	// This catches injection attempts before they're split into tokens
	dangerousChars := []string{";", "|", "&", "`", "$"}
	for _, char := range dangerousChars {
		if strings.Contains(argsString, char) {
			return nil, fmt.Errorf("kustomize arguments contain forbidden characters: %q", char)
		}
	}

	// Split into individual arguments
	// Supports simple quoted strings for values with spaces
	args := splitArgs(argsString)

	// Flags that can take a value as a separate argument (space-separated)
	valueTakingFlags := map[string]bool{
		"--env":             true,
		"-e":                true,
		"--helm-command":    true,
		"--load-restrictor": true,
	}

	// Validate each argument
	for i := 0; i < len(args); i++ {
		arg := args[i]

		// Extract argument name (before = or standalone)
		argName := arg
		hasValue := strings.Contains(arg, "=")
		if hasValue {
			parts := strings.SplitN(arg, "=", 2)
			argName = parts[0]
			// Check for empty value after =
			if len(parts) < 2 || parts[1] == "" {
				return nil, fmt.Errorf("kustomize argument %q has empty value", argName)
			}
		}

		// Check whitelist
		if !AllowedKustomizeArgs[argName] {
			return nil, fmt.Errorf("kustomize argument %q is not allowed (security restriction)", argName)
		}

		// For value-taking flags, validate and skip the value
		if valueTakingFlags[argName] {
			if !hasValue {
				// Flag doesn't have = syntax, so value must be next argument
				if i+1 >= len(args) {
					return nil, fmt.Errorf("kustomize argument %q requires a value", argName)
				}
				nextArg := args[i+1]
				// Validate that next argument is not another flag
				if strings.HasPrefix(nextArg, "-") {
					return nil, fmt.Errorf("kustomize argument %q requires a value, got flag %q instead", argName, nextArg)
				}
				i++ // skip next argument (it's the value)
			}
		}
	}

	return args, nil
}

// splitArgs splits argument string into array
// Supports simple quoted strings
func splitArgs(s string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range s {
		switch {
		case (r == '"' || r == '\'') && !inQuote:
			inQuote = true
			quoteChar = r
		case r == quoteChar && inQuote:
			inQuote = false
			quoteChar = 0
		case r == ' ' && !inQuote:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}
