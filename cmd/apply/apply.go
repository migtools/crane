package apply

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/internal/apply"
	"github.com/konveyor/crane/internal/flags"
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
}

type Flags struct {
	ExportDir    string `mapstructure:"export-dir"`
	TransformDir string `mapstructure:"transform-dir"`
	OutputDir    string `mapstructure:"output-dir"`
	// Multi-stage flag
	Stage string `mapstructure:"stage"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	return nil
}

func (o *Options) Validate() error {
	// Validate stage format if specified
	if o.Flags.Stage != "" {
		if err := internalTransform.ValidateStageName(o.Flags.Stage); err != nil {
			return err
		}
	}
	return nil
}

func (o *Options) Run() error {
	return o.run()
}

func NewApplyCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		cobraGlobalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the transformations to the exported resources and save results in an output directory",
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
			viper.Unmarshal(&o.Flags)
			viper.Unmarshal(&o.globalFlags)
		},
	}

	addFlagsForOptions(&o.cobraFlags, cmd)

	return cmd
}

func addFlagsForOptions(o *Flags, cmd *cobra.Command) {
	// Note: export-dir is kept for compatibility and consistency with other commands,
	// but is not used by apply (apply only reads from transform-dir)
	cmd.Flags().StringVarP(&o.ExportDir, "export-dir", "e", "export", "The path where the kubernetes resources are saved")
	cmd.Flags().StringVarP(&o.TransformDir, "transform-dir", "t", "transform", "The path where files that contain the transformations are saved")
	cmd.Flags().StringVarP(&o.OutputDir, "output-dir", "o", "output", "The path where files are to be saved after transformation are applied")

	// Multi-stage flag
	cmd.Flags().StringVar(&o.Stage, "stage", "", "Apply a specific stage only (e.g., '10_KubernetesPlugin'). If not specified, all stages are applied.")
}

func (o *Options) run() error {
	log := o.globalFlags.GetLogger()

	// Validate kubectl is available before proceeding
	// All apply operations require kubectl kustomize
	if err := apply.ValidateKubectlAvailable(); err != nil {
		return err
	}

	transformDir, err := filepath.Abs(o.TransformDir)
	if err != nil {
		return err
	}

	outputDir, err := filepath.Abs(o.OutputDir)
	if err != nil {
		return err
	}

	// Create output directory
	if err := os.MkdirAll(outputDir, 0700); err != nil {
		return fmt.Errorf("failed to create output directory: %w", err)
	}

	// Create applier
	applier := &apply.KustomizeApplier{
		Log:          log.WithField("command", "apply").Logger,
		TransformDir: transformDir,
		OutputDir:    outputDir,
	}

	// Determine which stages to apply
	var selector internalTransform.StageSelector

	if o.Flags.Stage != "" {
		// User specified a specific stage to apply
		selector = internalTransform.StageSelector{
			Stage: o.Flags.Stage,
		}
		log.Infof("Applying stage: %s", o.Flags.Stage)
	} else {
		// Default: apply all stages
		// This ensures sequential consistency - each stage output is materialized
		log.Info("Applying all stages...")
		// Empty selector means apply all discovered stages
	}

	return applier.ApplyMultiStage(selector)
}
