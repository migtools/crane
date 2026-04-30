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

	inputDir         string
	validateDir      string
	outputFormat     string
	apiResourcesFile string

	genericclioptions.IOStreams
}

// Complete loads the kubeconfig to ensure the target cluster is reachable.
// Skipped in offline mode (--api-resources).
func (o *ValidateOptions) Complete(c *cobra.Command, args []string) error {
	if o.apiResourcesFile != "" {
		return nil
	}
	_, err := o.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return fmt.Errorf("loading kubeconfig for target cluster: %w", err)
	}
	return nil
}

// Validate checks that flags have valid values.
func (o *ValidateOptions) Validate() error {
	info, err := os.Stat(o.inputDir)
	if err != nil {
		return fmt.Errorf("input-dir %q: %w", o.inputDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("input-dir %q is not a directory", o.inputDir)
	}

	if o.outputFormat != "yaml" && o.outputFormat != "json" {
		return fmt.Errorf("--output must be \"yaml\" or \"json\", got %q", o.outputFormat)
	}

	if o.apiResourcesFile != "" {
		if o.configFlags.Context != nil && *o.configFlags.Context != "" {
			return fmt.Errorf("--api-resources and --context are mutually exclusive; use --api-resources for offline validation or --context for live validation")
		}
		if o.configFlags.KubeConfig != nil && *o.configFlags.KubeConfig != "" {
			return fmt.Errorf("--api-resources and --kubeconfig are mutually exclusive; use --api-resources for offline validation or --kubeconfig for live validation")
		}
		if _, err := os.Stat(o.apiResourcesFile); err != nil {
			return fmt.Errorf("api-resources file %q: %w", o.apiResourcesFile, err)
		}
	}

	return nil
}

// Run performs the scan, match, and report steps.
func (o *ValidateOptions) Run() error {
	log := o.globalFlags.GetLogger()

	entries, err := internalValidate.ScanManifests(internalValidate.ScanOptions{Dirs: []string{o.inputDir}}, log)
	if err != nil {
		return fmt.Errorf("scanning manifests: %w", err)
	}

	log.Infof("Scanned %d distinct GVK+namespace tuples", len(entries))

	var report *internalValidate.ValidationReport

	if o.apiResourcesFile != "" {
		index, err := internalValidate.ParseAPIResourcesJSON(o.apiResourcesFile)
		if err != nil {
			return fmt.Errorf("loading api-resources: %w", err)
		}
		report = internalValidate.MatchResultsFromIndex(entries, index)
		report.Mode = "offline"
		report.APIResourcesSource = o.apiResourcesFile
	} else {
		discoveryClient, err := o.configFlags.ToDiscoveryClient()
		if err != nil {
			return fmt.Errorf("creating discovery client: %w", err)
		}
		discoveryClient.Invalidate()

		report, err = internalValidate.MatchResults(entries, internalValidate.MatchOptions{DiscoveryClient: discoveryClient}, log)
		if err != nil {
			return fmt.Errorf("matching against target cluster: %w", err)
		}
		report.Mode = "live"
		if o.configFlags.Context != nil && *o.configFlags.Context != "" {
			report.ClusterContext = *o.configFlags.Context
		}
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
		Short: "Validate final manifests against a target cluster",
		Long: `Validate checks the final rendered manifests (from crane apply's output)
for compatibility with a target cluster. It verifies that every
apiVersion+kind is served by the target cluster's API surface (strict
GVK matching).

Pipeline: export → transform → apply → validate

Use --api-resources to validate offline against the JSON output of
'kubectl api-resources -o json' when the target cluster is not directly
reachable. Otherwise, supply kubeconfig/context flags for live validation.

Incompatible resources are written to a failures/ directory under
the validate-dir for auditability.

Exit code 0 means all checks pass; exit code 1 means one or more checks
failed (or another error occurred).`,
		Args:         cobra.NoArgs,
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
		},
	}

	cmd.Flags().StringVarP(&o.inputDir, "input-dir", "i", "output", "The path to the apply output directory containing final manifests")
	cmd.Flags().StringVar(&o.validateDir, "validate-dir", "validate", "The path where validation results and failures are saved")
	cmd.Flags().StringVarP(&o.outputFormat, "output", "o", "json", "Report file format: json or yaml")
	cmd.Flags().StringVar(&o.apiResourcesFile, "api-resources", "", "Path to kubectl api-resources -o json output for offline validation (mutually exclusive with --context/--kubeconfig)")
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}
