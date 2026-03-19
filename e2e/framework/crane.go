package framework

import (
	"fmt"
	"os/exec"
)

type CraneRunner struct {
	Bin           string
	SourceContext string
	TargetContext string
	WorkDir       string
}

type CranePaths struct {
	ExportDir    string
	TransformDir string
}

func (c CraneRunner) Export(namespace, exportDir string) error {
	args := []string{"export", "--context", c.SourceContext, "--namespace", namespace, "--export-dir", exportDir}
	cmd := exec.Command(c.Bin, args...)
	cmd.Dir = c.WorkDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("crane export failed: %v, output: %s", err, string(out))
	}
	return nil
}

func (c CraneRunner) Transform(exportDir, transformDir string) error {
	args := []string{"transform", "--export-dir", exportDir, "--transform-dir", transformDir}
	cmd := exec.Command(c.Bin, args...)
	cmd.Dir = c.WorkDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("crane transform failed: %v, output: %s", err, string(out))
	}
	return nil
}

func (c CraneRunner) Apply(exportDir, transformDir string, outputDir string) error {
	args := []string{"apply", "--export-dir", exportDir, "--transform-dir", transformDir, "--output-dir", outputDir}
	cmd := exec.Command(c.Bin, args...)
	cmd.Dir = c.WorkDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("crane apply failed: %v, output: %s", err, string(out))
	}
	return nil
}
