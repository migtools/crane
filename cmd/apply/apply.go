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
	// Multi-stage flags
	Stage     string   `mapstructure:"stage"`
	FromStage string   `mapstructure:"from-stage"`
	ToStage   string   `mapstructure:"to-stage"`
	Stages    []string `mapstructure:"stages"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	return nil
}

func (o *Options) Validate() error {
	// Validate mutually exclusive flags
	flagCount := 0
	if o.Flags.Stage != "" {
		flagCount++
	}
	if o.Flags.FromStage != "" || o.Flags.ToStage != "" {
		flagCount++
	}
	if len(o.Flags.Stages) > 0 {
		flagCount++
	}

	if flagCount > 1 {
		return fmt.Errorf("--stage, --from-stage/--to-stage, and --stages are mutually exclusive")
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

	// Multi-stage flags
	cmd.Flags().StringVar(&o.Stage, "stage", "", "Apply a specific stage only (e.g., '10_KubernetesPlugin')")
	cmd.Flags().StringVar(&o.FromStage, "from-stage", "", "Apply from this stage onwards (e.g., '20_OpenshiftPlugin')")
	cmd.Flags().StringVar(&o.ToStage, "to-stage", "", "Apply up to and including this stage (e.g., '30_ImagestreamPlugin')")
	cmd.Flags().StringSliceVar(&o.Stages, "stages", nil, "Apply specific stages (comma-separated, e.g., '10_KubernetesPlugin,30_ImagestreamPlugin')")
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
	if o.Flags.Stage != "" || o.Flags.FromStage != "" ||
		o.Flags.ToStage != "" || len(o.Flags.Stages) > 0 {
		// User specified which stages to apply
		selector := internalTransform.StageSelector{
			Stage:     o.Flags.Stage,
			FromStage: o.Flags.FromStage,
			ToStage:   o.Flags.ToStage,
			Stages:    o.Flags.Stages,
		}

		log.Info("Applying selected stages...")
		return applier.ApplyMultiStage(selector)
	}

	// Default: apply all stages
	// This ensures sequential consistency - each stage output is materialized
	log.Info("Applying all stages...")

	// Empty selector means apply all discovered stages
	emptySelector := internalTransform.StageSelector{}
	return applier.ApplyMultiStage(emptySelector)
}
