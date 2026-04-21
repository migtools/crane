package validate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/internal/flags"
	internalValidate "github.com/konveyor/crane/internal/validate"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// ValidateOptions holds CLI flags and runtime state for a validate run.
type ValidateOptions struct {
	configFlags *genericclioptions.ConfigFlags

	cobraGlobalFlags *flags.GlobalFlags
	globalFlags      *flags.GlobalFlags

	exportDir    string
	validateDir  string
	outputFormat string

	genericclioptions.IOStreams
}

// Complete loads the kubeconfig to ensure the target cluster is reachable.
func (o *ValidateOptions) Complete(c *cobra.Command, args []string) error {
	_, err := o.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return err
	}
	return nil
}

// Validate checks that flags have valid values.
func (o *ValidateOptions) Validate() error {
	info, err := os.Stat(o.exportDir)
	if err != nil {
		return fmt.Errorf("export-dir %q: %w", o.exportDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("export-dir %q is not a directory", o.exportDir)
	}

	if o.outputFormat != "yaml" && o.outputFormat != "json" {
		return fmt.Errorf("--output must be \"yaml\" or \"json\", got %q", o.outputFormat)
	}

	return nil
}

// Run performs the scan, match, and report steps.
func (o *ValidateOptions) Run() error {
	log := o.globalFlags.GetLogger()

	entries, err := internalValidate.ScanManifests(internalValidate.ScanOptions{Dirs: []string{o.exportDir}}, log)
	if err != nil {
		return fmt.Errorf("scanning manifests: %w", err)
	}

	log.Infof("Scanned %d distinct GVK+namespace tuples", len(entries))

	discoveryClient, err := o.configFlags.ToDiscoveryClient()
	if err != nil {
		return fmt.Errorf("creating discovery client: %w", err)
	}
	discoveryClient.Invalidate()

	report, err := internalValidate.MatchResults(entries, internalValidate.MatchOptions{DiscoveryClient: discoveryClient}, log)
	if err != nil {
		return fmt.Errorf("matching against target cluster: %w", err)
	}

	internalValidate.FormatTable(o.Out, report)

	if err := os.MkdirAll(o.validateDir, 0700); err != nil {
		return fmt.Errorf("creating validate directory: %w", err)
	}
	reportExt := o.outputFormat
	reportPath := filepath.Join(o.validateDir, "report."+reportExt)
	reportFile, err := os.Create(reportPath)
	if err != nil {
		return fmt.Errorf("creating validation report %q: %w", reportPath, err)
	}
	var writeErr error
	switch o.outputFormat {
	case "json":
		writeErr = internalValidate.FormatJSON(reportFile, report)
	default:
		writeErr = internalValidate.FormatYAML(reportFile, report)
	}
	if writeErr != nil {
		_ = reportFile.Close()
		return fmt.Errorf("writing validation report %q: %w", reportPath, writeErr)
	}
	if err := reportFile.Close(); err != nil {
		return fmt.Errorf("closing validation report %q: %w", reportPath, err)
	}
	log.Infof("Wrote validation report to %s", reportPath)

	if report.HasIncompatible() {
		failuresDir := filepath.Join(o.validateDir, "failures")
		if err := internalValidate.WriteFailures(failuresDir, report, log); err != nil {
			return fmt.Errorf("writing validation failures to %q: %w", failuresDir, err)
		}
		return internalValidate.ErrValidationFailed
	}

	return nil
}

// NewValidateCommand builds the cobra validate command with flags and viper wiring.
func NewValidateCommand(streams genericclioptions.IOStreams, f *flags.GlobalFlags) *cobra.Command {
	o := &ValidateOptions{
		configFlags:      genericclioptions.NewConfigFlags(true),
		IOStreams:         streams,
		cobraGlobalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "validate",
		Short: "Validate exported manifests against a target cluster",
		Long: `Validate checks exported Kubernetes manifests for compatibility with a
target cluster. Currently it verifies that every apiVersion+kind in the
export is served by the target cluster's API surface (strict GVK matching).

Incompatible resources are written to a failures/ directory under
the validate-dir for auditability.

Exit code 0 means all checks pass; exit code 1 means one or more checks
failed (or another error occurred).`,
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(c, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			if err := o.Run(); err != nil {
				return err
			}
			return nil
		},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := viper.BindPFlags(cmd.Flags()); err != nil {
				return fmt.Errorf("binding validate flags: %w", err)
			}
			if err := viper.Unmarshal(&o.globalFlags); err != nil {
				return fmt.Errorf("loading global flags: %w", err)
			}
			if err := viper.Unmarshal(&o.configFlags); err != nil {
				return fmt.Errorf("loading kube config flags: %w", err)
			}
			if err := viper.UnmarshalKey("export-dir", &o.exportDir); err != nil {
				return fmt.Errorf("loading export-dir: %w", err)
			}
			if err := viper.UnmarshalKey("validate-dir", &o.validateDir); err != nil {
				return fmt.Errorf("loading validate-dir: %w", err)
			}
			if err := viper.UnmarshalKey("output", &o.outputFormat); err != nil {
				return fmt.Errorf("loading output: %w", err)
			}
			return nil
		},
	}

	cmd.Flags().StringVarP(&o.exportDir, "export-dir", "e", "export", "The path to the exported resources directory")
	cmd.Flags().StringVar(&o.validateDir, "validate-dir", "validate", "The path where validation results and failures are saved")
	cmd.Flags().StringVarP(&o.outputFormat, "output", "o", "json", "Report file format: json or yaml")
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}
