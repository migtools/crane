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

	if o.outputFormat != "table" && o.outputFormat != "json" {
		return fmt.Errorf("--output must be \"table\" or \"json\", got %q", o.outputFormat)
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

	switch o.outputFormat {
	case "json":
		if err := internalValidate.FormatJSON(o.Out, report); err != nil {
			return fmt.Errorf("writing JSON report: %w", err)
		}
	default:
		internalValidate.FormatTable(o.Out, report)
	}

	if err := os.MkdirAll(o.validateDir, 0700); err != nil {
		return fmt.Errorf("creating validate directory: %w", err)
	}
	reportPath := filepath.Join(o.validateDir, "report.json")
	reportFile, err := os.Create(reportPath)
	if err != nil {
		log.Warnf("error creating report file: %v", err)
	} else {
		if err := internalValidate.FormatJSON(reportFile, report); err != nil {
			log.Warnf("error writing report file: %v", err)
		}
		reportFile.Close()
		log.Infof("Wrote validation report to %s", reportPath)
	}

	if report.HasIncompatible() {
		failuresDir := filepath.Join(o.validateDir, "failures")
		if err := internalValidate.WriteFailures(failuresDir, report, log); err != nil {
			log.Warnf("error writing failures: %v", err)
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
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
			viper.Unmarshal(&o.globalFlags)
			viper.Unmarshal(&o.configFlags)
			viper.UnmarshalKey("export-dir", &o.exportDir)
		},
	}

	cmd.Flags().StringVarP(&o.exportDir, "export-dir", "e", "export", "The path to the exported resources directory")
	cmd.Flags().StringVar(&o.validateDir, "validate-dir", "validate", "The path where validation results and failures are saved")
	cmd.Flags().StringVarP(&o.outputFormat, "output", "o", "table", "Report format: table or json")
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}
