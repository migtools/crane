package transform

import (
	"testing"
)

// hasKustomizeCommand returns true since kustomize is embedded in the crane binary.
func hasKustomizeCommand(t *testing.T) bool {
	t.Helper()
	return true
}

// contains checks if substr is contained in s
func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(s == substr || len(s) >= len(substr) &&
		(s[:len(substr)] == substr ||
		s[len(s)-len(substr):] == substr ||
		findInString(s, substr)))
}

// findInString searches for substr in s
func findInString(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
