package framework

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type App interface {
	Deploy() error
	Validate() error
	Cleanup() error
}

type K8sDeployApp struct {
	Name      string
	Namespace string
	Bin       string
	Context   string
	ExtraVars map[string]string
}

// Deploy runs k8sdeploy deploy for the configured app and namespace.
func (a K8sDeployApp) Deploy() error {
	args := []string{}
	if a.Context != "" {
		args = append(args, "--context", a.Context)
	}
	args = append(args, "deploy", a.Name, "-n", a.Namespace)
	var err error
	args, err = a.withExtraVars(args)
	if err != nil {
		return err
	}
	logVerboseCommand(a.Bin, args)
	cmd := a.buildCommand(args...)
	out, err := cmd.CombinedOutput()
	logVerboseOutput("k8sdeploy deploy", out)
	if err != nil {
		return fmt.Errorf("failed to deploy app: %v, output: %s", err, string(out))
	}
	return nil
}

// Validate runs k8sdeploy validate for the configured app and namespace.
func (a K8sDeployApp) Validate() error {
	args := []string{}
	if a.Context != "" {
		args = append(args, "--context", a.Context)
	}
	args = append(args, "validate", a.Name, "-n", a.Namespace)
	var err error
	args, err = a.withExtraVars(args)
	if err != nil {
		return err
	}
	logVerboseCommand(a.Bin, args)
	cmd := a.buildCommand(args...)
	out, err := cmd.CombinedOutput()
	logVerboseOutput("k8sdeploy validate", out)
	if err != nil {
		return fmt.Errorf("failed to validate app: %v, output: %s", err, string(out))
	}
	return nil
} // End of Validate

// Cleanup runs k8sdeploy remove for the configured app and namespace.
func (a K8sDeployApp) Cleanup() error {
	args := []string{}
	if a.Context != "" {
		args = append(args, "--context", a.Context)
	}
	args = append(args, "remove", a.Name, "-n", a.Namespace)
	var err error
	args, err = a.withExtraVars(args)
	if err != nil {
		return err
	}
	logVerboseCommand(a.Bin, args)
	cmd := a.buildCommand(args...)
	out, err := cmd.CombinedOutput()
	logVerboseOutput("k8sdeploy remove", out)
	if err != nil {
		return fmt.Errorf("failed to remove app: %v, output: %s", err, string(out))
	}
	return nil
}

// buildCommand constructs an exec command with environment adjustments applied.
func (a K8sDeployApp) buildCommand(args ...string) *exec.Cmd {
	cmd := exec.Command(a.Bin, args...)
	cmd.Env = envWithBinDir(a.Bin)
	return cmd
}

// envWithBinDir prepends the binary directory to PATH when bin is a path.
func envWithBinDir(bin string) []string {
	env := os.Environ()
	if !strings.Contains(bin, string(filepath.Separator)) {
		return env
	}

	binDir := filepath.Dir(bin)
	pathVal := os.Getenv("PATH")
	updatedPath := binDir
	if pathVal != "" {
		updatedPath = binDir + string(os.PathListSeparator) + pathVal
	}
	return append(env, "PATH="+updatedPath)
}

// withExtraVars appends --extra-vars to k8sdeploy arguments when ExtraVars is non-empty.
func (a K8sDeployApp) withExtraVars(args []string) ([]string, error) {
	if len(a.ExtraVars) == 0 {
		return args, nil
	}

	extraVarsJSON, err := json.Marshal(a.ExtraVars)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal extra vars: %v", err)
	}

	args = append(args, "--extra-vars", string(extraVarsJSON))
	return args, nil
}
