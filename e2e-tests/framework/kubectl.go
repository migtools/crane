package framework

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

type KubectlRunner struct {
	Bin     string
	Context string
}

// CreateNamespace creates a namespace and treats AlreadyExists as success.
func (k KubectlRunner) CreateNamespace(ns string) error {
	args := []string{"create", "namespace", ns}
	if k.Context != "" {
		args = append(args, "--context", k.Context)
	}
	logVerboseCommand(k.Bin, args)
	cmd := exec.Command(k.Bin, args...)
	out, err := cmd.CombinedOutput()
	logVerboseOutput("kubectl create namespace", out)
	if err != nil {
		if strings.Contains(string(out), "AlreadyExists") {
			return nil
		}
		return fmt.Errorf("kubectl create namespace failed: %v, output: %s", err, string(out))
	}
	return nil
}

// ApplyDir applies all manifests recursively from the given directory.
func (k KubectlRunner) ApplyDir(dir string) error {
	args := []string{"apply", "-R", "-f", dir}
	if k.Context != "" {
		args = append(args, "--context", k.Context)
	}
	logVerboseCommand(k.Bin, args)
	cmd := exec.Command(k.Bin, args...)
	out, err := cmd.CombinedOutput()
	logVerboseOutput("kubectl apply", out)
	if err != nil {
		return fmt.Errorf("kubectl apply failed: %v, output: %s", err, string(out))
	}
	return nil
}

// ValidateApplyDir performs a server-side dry-run apply for a directory.
func (k KubectlRunner) ValidateApplyDir(dir string) error {
	args := []string{"apply", "-R", "-f", dir, "--dry-run=server"}
	if k.Context != "" {
		args = append(args, "--context", k.Context)
	}
	logVerboseCommand(k.Bin, args)
	cmd := exec.Command(k.Bin, args...)
	out, err := cmd.CombinedOutput()
	logVerboseOutput("kubectl dry-run apply", out)
	if err != nil {
		return fmt.Errorf("kubectl dry-run apply failed: %v, output: %s", err, string(out))
	}
	return nil
}

// ScaleDeployment scales matching deployment by label, then falls back to name.
func (k KubectlRunner) ScaleDeployment(ns, appName string, replicas int) error {
	selector := "name=" + appName
	checkArgs := []string{"get", "deployment", "--namespace", ns, "-l", selector, "-o", "name"}
	if k.Context != "" {
		checkArgs = append(checkArgs, "--context", k.Context)
	}
	logVerboseCommand(k.Bin, checkArgs)
	checkCmd := exec.Command(k.Bin, checkArgs...)
	checkOut, checkErr := checkCmd.CombinedOutput()
	logVerboseOutput("kubectl get deployment by label", checkOut)
	if checkErr != nil {
		return fmt.Errorf("kubectl get deployment by label failed: %v, output: %s", checkErr, string(checkOut))
	}

	baseArgs := []string{"scale", "deployment", "--namespace", ns, "--replicas", strconv.Itoa(replicas)}
	if strings.TrimSpace(string(checkOut)) != "" {
		args := append(baseArgs, "-l", selector)
		if k.Context != "" {
			args = append(args, "--context", k.Context)
		}
		logVerboseCommand(k.Bin, args)
		cmd := exec.Command(k.Bin, args...)
		out, err := cmd.CombinedOutput()
		logVerboseOutput("kubectl scale deployment", out)
		if err != nil {
			return fmt.Errorf("kubectl scale failed: %v, output: %s", err, string(out))
		}
		return nil
	}

	// Fallback when label-based scale doesn't find a deployment:
	// try "<appName>-deployment" name convention.
	fallbackNames := []string{appName + "-deployment"}

	var lastErr error
	var lastOut []byte
	for _, depName := range fallbackNames {
		args := append(baseArgs, depName)
		if k.Context != "" {
			args = append(args, "--context", k.Context)
		}
		logVerboseCommand(k.Bin, args)
		cmd := exec.Command(k.Bin, args...)
		out, err := cmd.CombinedOutput()
		logVerboseOutput("kubectl scale deployment", out)
		if err == nil {
			return nil
		}
		lastErr = err
		lastOut = out
	}

	return fmt.Errorf("kubectl scale failed after label and name fallbacks: %v, output: %s", lastErr, string(lastOut))

}
