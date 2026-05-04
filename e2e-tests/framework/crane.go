package framework

import (
	"fmt"
	"os/exec"
)

type CraneRunner struct {
	Bin           string
	SourceContext string
	WorkDir       string
}

// TransferPVCOptions contains arguments for the crane transfer-pvc command.
type TransferPVCOptions struct {
	SourceContext   string
	TargetContext   string
	PVCName         string
	PVCNamespaceMap string
	Endpoint        string
	IngressClass    string
	Subdomain       string
}

// Export runs crane export for a namespace into the given export directory.
func (c CraneRunner) Export(namespace, exportDir string) error {
	args := []string{"export", "--context", c.SourceContext, "--namespace", namespace, "--export-dir", exportDir}
	logVerboseCommand(c.Bin, args)
	cmd := exec.Command(c.Bin, args...)
	cmd.Dir = c.WorkDir
	out, err := cmd.CombinedOutput()
	logVerboseOutput("crane export", out)
	if err != nil {
		return fmt.Errorf("crane export failed: %v, output: %s", err, string(out))
	}
	return nil
}

// Transform runs crane transform from export directory to transform directory.
func (c CraneRunner) Transform(exportDir, transformDir string) error {
	args := []string{"transform", "--export-dir", exportDir, "--transform-dir", transformDir}
	logVerboseCommand(c.Bin, args)
	cmd := exec.Command(c.Bin, args...)
	cmd.Dir = c.WorkDir
	out, err := cmd.CombinedOutput()
	logVerboseOutput("crane transform", out)
	if err != nil {
		return fmt.Errorf("crane transform failed: %v, output: %s", err, string(out))
	}
	return nil
}

// TransformStage runs crane transform with a specific stage.
func (c CraneRunner) TransformStage(exportDir, transformDir, stage string) error {
	if stage == "" {
		return fmt.Errorf("crane transform --stage requires a non-empty stage")
	}
	args := []string{
		"transform",
		"--export-dir", exportDir,
		"--transform-dir", transformDir,
		"--stage", stage,
	}
	logVerboseCommand(c.Bin, args)
	cmd := exec.Command(c.Bin, args...)
	cmd.Dir = c.WorkDir
	out, err := cmd.CombinedOutput()
	logVerboseOutput("crane transform --stage", out)
	if err != nil {
		return fmt.Errorf("crane transform --stage %q failed: %v, output: %s", stage, err, string(out))
	}
	return nil
}

// Apply runs crane apply to render manifests into the output directory.
func (c CraneRunner) Apply(exportDir, transformDir string, outputDir string) error {
	args := []string{"apply", "--export-dir", exportDir, "--transform-dir", transformDir, "--output-dir", outputDir}
	logVerboseCommand(c.Bin, args)
	cmd := exec.Command(c.Bin, args...)
	cmd.Dir = c.WorkDir
	out, err := cmd.CombinedOutput()
	logVerboseOutput("crane apply", out)
	if err != nil {
		return fmt.Errorf("crane apply failed: %v, output: %s", err, string(out))
	}
	return nil
}

// TransferPVC runs crane transfer-pvc with the provided transfer options.
func (c CraneRunner) TransferPVC(opts TransferPVCOptions) error {
	args := []string{"transfer-pvc",
		"--source-context",
		opts.SourceContext,
		"--destination-context", opts.TargetContext,
		"--pvc-name", opts.PVCName,
		"--pvc-namespace", opts.PVCNamespaceMap,
		"--endpoint", opts.Endpoint,
		"--ingress-class", opts.IngressClass,
		"--subdomain", opts.Subdomain,
	}
	logVerboseCommand(c.Bin, args)
	cmd := exec.Command(c.Bin, args...)
	cmd.Dir = c.WorkDir
	out, err := cmd.CombinedOutput()
	logVerboseOutput("crane transfer-pvc", out)
	if err != nil {
		return fmt.Errorf("crane transfer-pvc failed: %v, output: %s", err, string(out))
	}
	return nil
}
