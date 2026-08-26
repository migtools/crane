package apply

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"github.com/konveyor/crane/internal/apply"
	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/kustomize"
	internalTransform "github.com/konveyor/crane/internal/transform"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type Options struct {
	// Two GlobalFlags struct fields are needed
	// 1. cobraGlobalFlags for explicit CLI args parsed by cobra
	// 2. globalFlags for the args merged with values from the viper config file
	cobraGlobalFlags *flags.GlobalFlags
	globalFlags      *flags.GlobalFlags
	// Two Flags struct fields are needed
	// 1. cobraFlags for explicit CLI args parsed by cobra
	// 2. Flags for the args merged with values from the viper config file
	cobraFlags Flags
	Flags
	// Positional arguments for stage selection
	RequestedStages []string
	log             *logrus.Logger
}

type Flags struct {
	TransformDir string `mapstructure:"transform-dir"`
	OutputDir    string `mapstructure:"output-dir"`
	// Kustomize arguments
	KustomizeArgs string `mapstructure:"kustomize-args"`
	// Skip cluster-scoped resources in output
	SkipClusterScoped bool `mapstructure:"skip-cluster-scoped"`
	Overwrite         bool `mapstructure:"overwrite"`
	// Enable ordered resource filenames for dependency-aware kubectl apply
	Ordered bool `mapstructure:"ordered"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// Store positional arguments as requested stages
	o.RequestedStages = args
	o.globalFlags.SetCmdName("apply")
	o.log = o.globalFlags.GetLoggerOrDefault()

	return nil
}

func (o *Options) Validate() error {
	log := o.log
	info, err := os.Stat(o.TransformDir)
	if err != nil {
		if os.IsNotExist(err) {
			log.Debugf("Transform directory %q does not exist", o.TransformDir)
			return fmt.Errorf("transform-dir %q does not exist", o.TransformDir)
		}
		log.Debugf("Transform directory %q is not accessible: %v", o.TransformDir, err)
		return fmt.Errorf("transform-dir %q is not accessible: %v", o.TransformDir, err)
	}
	if !info.IsDir() {
		log.Debugf("Transform path %q is not a directory", o.TransformDir)
		return fmt.Errorf("transform-dir %q is not a directory", o.TransformDir)
	}
	return nil
}

func (o *Options) Run() error {
	return o.run()
}

func NewApplyCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		cobraGlobalFlags: f,
		log:              logrus.StandardLogger(),
	}
	cmd := &cobra.Command{
		Use:   "apply [stage...]",
		Short: "Apply the transformations to the exported resources and save results in an output directory",
		Long: `Apply transformations from one or more stages to exported Kubernetes resources.

Stages can be specified by:
- Stage directory name (e.g., 10_KubernetesPlugin)
- Plugin name (e.g., KubernetesPlugin)

If no stages specified, all discovered stages are applied.`,
		Args: cobra.ArbitraryArgs,
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(c, args); err != nil {
				return err
			}
			if err := o.Validate(); err != nil {
				return err
			}
			c.SilenceUsage = true
			if err := o.Run(); err != nil {
				return err
			}

			return nil
		},
		PreRun: func(cmd *cobra.Command, args []string) {
			viper.BindPFlags(cmd.Flags())
			viper.Unmarshal(&o.Flags)
			viper.Unmarshal(&o.globalFlags)
		},
	}

	addFlagsForOptions(&o.cobraFlags, cmd)

	return cmd
}

// getStageNames returns a list of stage directory names for error messages
func getStageNames(stages []internalTransform.Stage) []string {
	names := make([]string, len(stages))
	for i, stage := range stages {
		names[i] = stage.DirName
	}
	return names
}

func addFlagsForOptions(o *Flags, cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.TransformDir, "transform-dir", "t", "transform", "The path where files that contain the transformations are saved")
	cmd.Flags().StringVarP(&o.OutputDir, "output-dir", "o", "output", "The path where files are to be saved after transformation are applied")

	// Kustomize arguments
	cmd.Flags().StringVar(&o.KustomizeArgs, "kustomize-args", "", "Additional arguments for kustomize (e.g., '--enable-helm --helm-command=helm3')")
	// Cluster-scoped filtering
	cmd.Flags().BoolVar(&o.SkipClusterScoped, "skip-cluster-scoped", false, "Exclude cluster-scoped resources (ClusterRole, ClusterRoleBinding, CRD, etc.) from output. Useful for non-admin migration scenarios.")
	cmd.Flags().BoolVar(&o.Overwrite, "overwrite", false, "Overwrite the output directory if it already exists")
	// Ordered resource filenames
	cmd.Flags().BoolVar(&o.Ordered, "ordered", false, "Add ordering prefix to resource filenames (e.g., 300_Role_*, 310_RoleBinding_*) to ensure dependency-aware kubectl apply")
}

func (o *Options) run() error {
	log := o.log
	log.Infof("Starting apply...")

	transformDir, err := filepath.Abs(o.TransformDir)
	if err != nil {
		log.Errorf("Failed to resolve transform directory path %q: %v", o.TransformDir, err)
		return err
	}

	outputDir, err := filepath.Abs(o.OutputDir)
	if err != nil {
		log.Errorf("Failed to resolve output directory path %q: %v", o.OutputDir, err)
		return err
	}

	if _, err := os.Stat(outputDir); err == nil {
		if !o.Overwrite {
			log.Debugf("Output directory %q already exists; use --overwrite to replace it", outputDir)
			return fmt.Errorf("output directory %q already exists; use --overwrite to replace it", outputDir)
		}
		if err := os.RemoveAll(outputDir); err != nil {
			log.Errorf("Failed to clear output directory %q: %v", outputDir, err)
			return fmt.Errorf("failed to clear output directory: %w", err)
		}
	}
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		log.Errorf("Failed to create output directory %q: %v", outputDir, err)
		return fmt.Errorf("failed to create output directory: %w", err)
	}
	log.Debugf("Output directory ready: %q", outputDir)

	// Parse and validate kustomize arguments
	kustomizeArgs, err := kustomize.ParseAndValidateArgs(o.KustomizeArgs)
	if err != nil {
		log.Debugf("Invalid kustomize-args: %v", err)
		return fmt.Errorf("invalid kustomize-args: %w", err)
	}

	// Create applier
	applier := &apply.KustomizeApplier{
		Log:               log.WithField("command", "apply").Logger,
		TransformDir:      transformDir,
		OutputDir:         outputDir,
		KustomizeArgs:     kustomizeArgs,
		SkipClusterScoped: o.SkipClusterScoped,
		Ordered:           o.Ordered,
	}

	// Determine which stages to apply
	var selector internalTransform.StageSelector

	if len(o.RequestedStages) > 0 {
		// User specified specific stages via positional arguments
		// Validate that all requested stages can be resolved
		existingStages, err := internalTransform.DiscoverStages(transformDir)
		if err != nil {
			log.Errorf("Failed to discover stages in %q: %v", transformDir, err)
			return fmt.Errorf("failed to discover stages: %w", err)
		}
		log.Debugf("Discovered %d existing stage(s)", len(existingStages))

		// Track which requested stages were found
		resolvedStages := make(map[string]bool)
		for _, requested := range o.RequestedStages {
			found := false
			for _, stage := range existingStages {
				// Match by directory name OR plugin name
				if stage.DirName == requested || stage.PluginName == requested {
					log.Debugf("Resolved stage %q → %q", requested, stage.DirName)
					found = true
					resolvedStages[requested] = true
					break
				}
			}
			if !found {
				resolvedStages[requested] = false
			}
		}

		// Check if any requested stages were not found
		var unresolved []string
		for _, requested := range o.RequestedStages {
			if !resolvedStages[requested] {
				unresolved = append(unresolved, requested)
			}
		}

		if len(unresolved) > 0 {
			log.Debugf("Requested stage(s) not found: %v. Available stages: %v", unresolved, getStageNames(existingStages))
			return fmt.Errorf("requested stage(s) not found: %v. Available stages: %v", unresolved, getStageNames(existingStages))
		}

		selector = internalTransform.StageSelector{
			Stages: o.RequestedStages,
		}
		log.Infof("Applying %d stage(s): %v", len(o.RequestedStages), o.RequestedStages)
	} else {
		// Default: apply all stages
		// This ensures sequential consistency - each stage output is materialized
		log.Info("Applying all stages...")
		// Empty selector means apply all discovered stages
	}

	if runErr := applier.ApplyMultiStage(selector); runErr != nil {
		log.Errorf("Apply failed: %v", runErr)
		return runErr
	}
	log.Infof("Apply complete")
	return nil
}
