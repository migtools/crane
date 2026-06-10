package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/internal/flags"
	internalValidate "github.com/konveyor/crane/internal/validate"
	"github.com/sirupsen/logrus"
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

	o.outputFormat = strings.ToLower(o.outputFormat)
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
		if o.configFlags.APIServer != nil && *o.configFlags.APIServer != "" {
			return fmt.Errorf("--api-resources and --server are mutually exclusive; use --api-resources for offline validation or --server for live validation")
		}
		if o.configFlags.BearerToken != nil && *o.configFlags.BearerToken != "" {
			return fmt.Errorf("--api-resources and --token are mutually exclusive; use --api-resources for offline validation or --token for live validation")
		}
		if o.configFlags.ClusterName != nil && *o.configFlags.ClusterName != "" {
			return fmt.Errorf("--api-resources and --cluster are mutually exclusive; use --api-resources for offline validation or --cluster for live validation")
		}
		if o.configFlags.AuthInfoName != nil && *o.configFlags.AuthInfoName != "" {
			return fmt.Errorf("--api-resources and --user are mutually exclusive; use --api-resources for offline validation or --user for live validation")
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

	log.Debugf("Input directory: %s", o.inputDir)
	log.Debugf("Validate directory: %s", o.validateDir)
	log.Debugf("Output format: %s", o.outputFormat)
	if o.apiResourcesFile != "" {
		log.Debugf("Mode: offline (api-resources: %s)", o.apiResourcesFile)
	} else {
		log.Debugf("Mode: live")
	}

	entries, err := internalValidate.ScanManifests(internalValidate.ScanOptions{Dirs: []string{o.inputDir}}, log)
	if err != nil {
		return fmt.Errorf("scanning manifests: %w", err)
	}

	log.Infof("Scanned %d distinct GVK+namespace tuples", len(entries))

	if len(entries) == 0 {
		return fmt.Errorf("no manifests found in %s: nothing to validate", o.inputDir)
	}

	var report *internalValidate.ValidationReport

	if o.apiResourcesFile != "" {
		index, err := internalValidate.ParseAPIResourcesJSON(o.apiResourcesFile, log)
		if err != nil {
			return fmt.Errorf("loading api-resources: %w", err)
		}
		report = internalValidate.MatchResultsFromIndex(entries, index, log)
		report.Mode = "offline"
		report.APIResourcesSource = o.apiResourcesFile
		log.Infof("Validating in offline mode using api-resources file %q", o.apiResourcesFile)
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
			log.Infof("Validating in live mode against context %q", *o.configFlags.Context)
		} else {
			log.Infof("Validating in live mode against current kubeconfig context")
		}
	}

	internalValidate.FormatTable(o.Out, report)

	if err := os.MkdirAll(o.validateDir, 0700); err != nil {
		return fmt.Errorf("creating validate directory: %w", err)
	}

	reportExt := o.outputFormat
	reportPath := filepath.Join(o.validateDir, "report."+reportExt)
	failuresDir := filepath.Join(o.validateDir, "failures")

	// Archive previous results. Check for report in any format (json or yaml)
	// to handle the case where the user switches --output format between runs.
	if err := archivePreviousResults(o.validateDir, failuresDir, log); err != nil {
		return err
	}

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
		if err := internalValidate.WriteFailures(failuresDir, report, log); err != nil {
			return fmt.Errorf("writing validation failures to %q: %w", failuresDir, err)
		}
		return internalValidate.ErrValidationFailed
	}

	return nil
}

// getArchiveTimestamp returns the modification time of the given path formatted
// as a timestamp string. Returns empty string if the path does not exist.
func getArchiveTimestamp(path string) (string, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("checking %q: %w", path, err)
	}
	return info.ModTime().Format("20060102-150405"), nil
}

// archiveWithTimestamp renames an existing file or directory by appending the
// given timestamp, preserving previous results across runs.
// For example, report.json becomes report-20260603-100942.json.
// Does nothing if the path does not exist.
func archiveWithTimestamp(path, timestamp string, log logrus.FieldLogger) error {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return fmt.Errorf("checking %q: %w", path, err)
	}

	ext := filepath.Ext(path)
	var archivePath string
	if ext != "" {
		archivePath = strings.TrimSuffix(path, ext) + "-" + timestamp + ext
	} else {
		archivePath = path + "-" + timestamp
	}

	if err := os.Rename(path, archivePath); err != nil {
		return fmt.Errorf("renaming %q to %q: %w", path, archivePath, err)
	}
	log.Infof("Archived previous results: %s", filepath.Base(archivePath))
	return nil
}

// archivePreviousResults finds any existing report file (json or yaml) in
// validateDir and archives it along with the failures directory using a shared
// timestamp. This handles the case where the user switches --output format
// between runs (e.g., first run -o json, second run -o yaml).
func archivePreviousResults(validateDir, failuresDir string, log logrus.FieldLogger) error {
	// Look for report in any format
	matches, err := filepath.Glob(filepath.Join(validateDir, "report.json"))
	if err != nil {
		return fmt.Errorf("checking for previous json report: %w", err)
	}
	yamlMatches, err := filepath.Glob(filepath.Join(validateDir, "report.yaml"))
	if err != nil {
		return fmt.Errorf("checking for previous yaml report: %w", err)
	}
	matches = append(matches, yamlMatches...)

	if len(matches) == 0 {
		log.Debugf("No previous report files found in %s", validateDir)
		return nil
	}

	log.Debugf("Found %d previous report file(s) to archive: %v", len(matches), matches)
	// Use the first found report's timestamp for both report and failures
	ts, err := getArchiveTimestamp(matches[0])
	if err != nil {
		return fmt.Errorf("checking previous report: %w", err)
	}
	if ts == "" {
		return nil
	}

	// Archive all found report files (could be both json and yaml from different runs)
	for _, reportFile := range matches {
		if err := archiveWithTimestamp(reportFile, ts, log); err != nil {
			return fmt.Errorf("archiving previous report: %w", err)
		}
	}

	// Archive failures directory with the same timestamp
	if err := archiveWithTimestamp(failuresDir, ts, log); err != nil {
		return fmt.Errorf("archiving previous failures: %w", err)
	}

	return nil
}

// NewValidateCommand builds the cobra validate command with flags and viper wiring.
func NewValidateCommand(streams genericclioptions.IOStreams, f *flags.GlobalFlags) *cobra.Command {
	o := &ValidateOptions{
		configFlags:      genericclioptions.NewConfigFlags(true),
		IOStreams:        streams,
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

Use --api-resources to validate offline against a captured API surface
JSON file (produced by scripts/capture-api-surface.sh) when the target
cluster is not directly reachable. Otherwise, supply kubeconfig/context
flags for live validation.

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
	cmd.Flags().StringVar(&o.apiResourcesFile, "api-resources", "", "Path to API surface JSON file from capture-api-surface.sh for offline validation (mutually exclusive with --context/--kubeconfig/--server/--token/--cluster/--user)")
	o.configFlags.AddFlags(cmd.Flags())
	flags.SetGroupedHelp(cmd, []string{
		"as",
		"as-group",
		"as-uid",
		"as-user-extra",
		"cache-dir",
		"certificate-authority",
		"client-certificate",
		"client-key",
		"cluster",
		"context",
		"disable-compression",
		"insecure-skip-tls-verify",
		"kubeconfig",
		"namespace",
		"request-timeout",
		"server",
		"tls-server-name",
		"token",
		"user",
	})
	return cmd
}
