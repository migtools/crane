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
	cmd := exec.Command(k.Bin, args...)
	out, err := cmd.CombinedOutput()
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
	cmd := exec.Command(k.Bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl apply failed: %v, output: %s", err, string(out))
	}
	return nil
}

func (k KubectlRunner) ScaleDeployment(ns, name string, replicas int) error {
	args := []string{"scale", "deployment", name, "--namespace", ns, "--replicas", strconv.Itoa(replicas)}
	if k.Context != "" {
		args = append(args, "--context", k.Context)
	}
	cmd := exec.Command(k.Bin, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("kubectl scale failed: %v, output: %s", err, string(out))
	}
	return nil

}
