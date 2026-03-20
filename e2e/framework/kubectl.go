package framework

import (
	"fmt"
	"os/exec"
	"strconv"
)

type KubectlRunner struct {
	Bin     string
	Context string
}

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
		return fmt.Errorf("kubectl create namespace failed: %v, output: %s", err, string(out))
	}
	return nil
}

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

func (k KubectlRunner) ScaleDeployment(ns, name string, replicas int) error {
	args := []string{"scale", "deployment", name, "--namespace", ns, "--replicas", strconv.Itoa(replicas)}
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
