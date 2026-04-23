package transform

import (
	"os/exec"
	"testing"

	"github.com/konveyor/crane/internal/file"
)

// hasKustomizeCommand checks if kubectl or oc is available for kustomize
func hasKustomizeCommand(t *testing.T) bool {
	t.Helper()
	cmd := file.GetKustomizeCommand()
	// Try to run "<cmd> version" to verify it's available
	if err := exec.Command(cmd, "version", "--client").Run(); err != nil {
		t.Logf("Kustomize command '%s' not available: %v", cmd, err)
		return false
	}
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
