package listplugins

import (
	"fmt"
	"path/filepath"

	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/plugin"
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
	PluginDir   string   `mapstructure:"plugin-dir"`
	SkipPlugins []string `mapstructure:"skip-plugins"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// TODO: @sseago
	return nil
}

func (o *Options) Validate() error {
	// TODO: @sseago
	return nil
}

func (o *Options) Run() error {
	return o.run()
}

func NewListPluginsCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		cobraGlobalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "list-plugins",
		Short: "Return a list of configured plugins",
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

	// No separate addFlags needed here since all options inherit from parent PersistentFlags()

	return cmd
}

func (o *Options) run() error {
	pluginDir, err := filepath.Abs(o.PluginDir)
	if err != nil {
		return err
	}

	log := o.globalFlags.GetLogger()

	plugins, err := plugin.GetFilteredPlugins(pluginDir, o.SkipPlugins, log)
	if err != nil {
		return err
	}

	for _, thisPlugin := range plugins {
		fmt.Printf("Plugin: %v (version %v)\n", thisPlugin.Metadata().Name, thisPlugin.Metadata().Version)
	}
	return nil
}
