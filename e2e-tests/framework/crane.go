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

// ValidateOptions contains arguments for the crane validate command.
type ValidateOptions struct {
	Context          string
	InputDir         string
	ValidateDir      string
	OutputFormat     string
	APIResourcesFile string
	ExtraArgs        []string
}

type ExportOptions struct {
	Namespace        string
	ExportDir        string
	LabelSelector    string
	CRDSkipGroups    []string
	CRDIncludeGroups []string
	AsExtras         string
	QPS              float32
	Burst            int
}

type TransformOptions struct {
	ExportDir         string
	TransformDir      string
	PluginDir         string
	IgnoredPatchesDir string
	SkipPlugins       []string
	OptionalFlags     string
	Force             bool
	KustomizeArgs     string
	InstructionsFile  string
	Stages            []string
}

type ApplyOptions struct {
	ExportDir         string
	TransformDir      string
	OutputDir         string
	KustomizeArgs     string
	SkipClusterScoped bool
	Stages            []string
}

// Export runs crane export for a namespace into the given export directory.
func (c CraneRunner) Export(opts ExportOptions) error {
	args := []string{"export", "--context", c.SourceContext}

	if opts.Namespace != "" {
		args = append(args, "--namespace", opts.Namespace)
	}
	if opts.ExportDir != "" {
		args = append(args, "--export-dir", opts.ExportDir)
	}
	if opts.LabelSelector != "" {
		args = append(args, "--label-selector", opts.LabelSelector)
	}
	for _, g := range opts.CRDSkipGroups {
		args = append(args, "--crd-skip-group", g)
	}
	for _, g := range opts.CRDIncludeGroups {
		args = append(args, "--crd-include-group", g)
	}
	if opts.AsExtras != "" {
		args = append(args, "--as-extras", opts.AsExtras)
	}
	if opts.QPS > 0 {
		args = append(args, "--qps", fmt.Sprintf("%g", opts.QPS))
	}
	if opts.Burst > 0 {
		args = append(args, "--burst", fmt.Sprintf("%d", opts.Burst))
	}
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
func (c CraneRunner) Transform(opts TransformOptions) error {
	args := []string{"transform"}

	if opts.ExportDir != "" {
		args = append(args, "--export-dir", opts.ExportDir)
	}
	if opts.TransformDir != "" {
		args = append(args, "--transform-dir", opts.TransformDir)
	}
	if opts.PluginDir != "" {
		args = append(args, "--plugin-dir", opts.PluginDir)
	}
	if opts.IgnoredPatchesDir != "" {
		args = append(args, "--ignored-patches-dir", opts.IgnoredPatchesDir)
	}
	for _, p := range opts.SkipPlugins {
		args = append(args, "--skip-plugins", p)
	}
	if opts.OptionalFlags != "" {
		args = append(args, "--optional-flags", opts.OptionalFlags)
	}
	if opts.Force {
		args = append(args, "--force")
	}
	if opts.KustomizeArgs != "" {
		args = append(args, "--kustomize-args", opts.KustomizeArgs)
	}
	if opts.InstructionsFile != "" {
		args = append(args, "--instructions-file", opts.InstructionsFile)
	}
	args = append(args, opts.Stages...)

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

// Apply runs crane apply to render manifests into the output directory.
func (c CraneRunner) Apply(opts ApplyOptions) error {
	args := []string{"apply"}

	if opts.ExportDir != "" {
		args = append(args, "--export-dir", opts.ExportDir)
	}
	if opts.TransformDir != "" {
		args = append(args, "--transform-dir", opts.TransformDir)
	}
	if opts.OutputDir != "" {
		args = append(args, "--output-dir", opts.OutputDir)
	}
	if opts.KustomizeArgs != "" {
		args = append(args, "--kustomize-args", opts.KustomizeArgs)
	}
	if opts.SkipClusterScoped {
		args = append(args, "--skip-cluster-scoped")
	}
	args = append(args, opts.Stages...)

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

// Validate runs crane validate and returns command output or an error.
func (c CraneRunner) Validate(opts ValidateOptions) (stdout string, err error) {
	args := []string{"validate"}

	if opts.Context != "" {
		args = append(args, "--context", opts.Context)
	}
	if opts.InputDir != "" {
		args = append(args, "--input-dir", opts.InputDir)
	}
	if opts.ValidateDir != "" {
		args = append(args, "--validate-dir", opts.ValidateDir)
	}
	if opts.OutputFormat != "" {
		args = append(args, "--output", opts.OutputFormat)
	}
	if opts.APIResourcesFile != "" {
		args = append(args, "--api-resources", opts.APIResourcesFile)
	}
	if len(opts.ExtraArgs) > 0 {
		args = append(args, opts.ExtraArgs...)
	}
	logVerboseCommand(c.Bin, args)
	cmd := exec.Command(c.Bin, args...)
	cmd.Dir = c.WorkDir
	out, cmdErr := cmd.CombinedOutput()
	logVerboseOutput("crane validate", out)
	stdout = string(out)
	if cmdErr != nil {
		return stdout, fmt.Errorf("crane validate failed: %v, output: %s", cmdErr, stdout)
	}
	return stdout, nil
}
