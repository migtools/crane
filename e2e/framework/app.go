package framework

import (
	"fmt"
	"os/exec"
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
}

func (a K8sDeployApp) Deploy() error {
	args := []string{}
	if a.Context != "" {
		args = append(args, "--context", a.Context)
	}
	args = append(args, "deploy", a.Name, "-n", a.Namespace)
	logVerboseCommand(a.Bin, args)
	cmd := exec.Command(a.Bin, args...)
	out, err := cmd.CombinedOutput()
	logVerboseOutput("k8sdeploy deploy", out)
	if err != nil {
		return fmt.Errorf("failed to deploy app: %v, output: %s", err, string(out))
	}
	return nil
}

func (a K8sDeployApp) Validate() error {
	args := []string{}
	if a.Context != "" {
		args = append(args, "--context", a.Context)
	}
	args = append(args, "validate", a.Name, "-n", a.Namespace)
	logVerboseCommand(a.Bin, args)
	cmd := exec.Command(a.Bin, args...)
	out, err := cmd.CombinedOutput()
	logVerboseOutput("k8sdeploy validate", out)
	if err != nil {
		return fmt.Errorf("failed to validate app: %v, output: %s", err, string(out))
	}
	return nil
}

func (a K8sDeployApp) Cleanup() error {
	args := []string{}
	if a.Context != "" {
		args = append(args, "--context", a.Context)
	}
	args = append(args, "remove", a.Name, "-n", a.Namespace)
	logVerboseCommand(a.Bin, args)
	cmd := exec.Command(a.Bin, args...)
	out, err := cmd.CombinedOutput()
	logVerboseOutput("k8sdeploy remove", out)
	if err != nil {
		return fmt.Errorf("failed to remove app: %v, output: %s", err, string(out))
	}
	return nil
}
