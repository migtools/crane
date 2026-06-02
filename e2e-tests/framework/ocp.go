package framework

import (
	"fmt"
	"os/exec"
	"strings"
)

// getOCRegistryURL returns the externally-accessible route of the OCP internal
// registry for the given kubeconfig context via `oc registry info --internal=false`.
func getOCRegistryURL(kubectlContext string) (string, error) {
	cmd := exec.Command("oc", "registry", "info", "--internal=false", "--context", kubectlContext)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("oc registry info --context %q failed: %w", kubectlContext, err)
	}
	return strings.TrimSpace(string(out)), nil
}

// getOCToken retrieves the current OAuth token for a kubeconfig context via
// `oc whoami --show-token`. The username is irrelevant for OCP registry auth;
// skopeo accepts any non-empty string (we use "unused").
func getOCToken(kubectlContext string) (string, error) {
	cmd := exec.Command("oc", "whoami", "--show-token", "--context", kubectlContext)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("oc whoami --show-token --context %q failed: %w", kubectlContext, err)
	}
	return strings.TrimSpace(string(out)), nil
}