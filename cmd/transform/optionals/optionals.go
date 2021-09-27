package optionals

import (
	"fmt"
	"path/filepath"

	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/plugin"
	"github.com/spf13/cobra"
)

type Options struct {
	globalFlags *flags.GlobalFlags
	PluginDir   string
	SkipPlugins string
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

func NewOptionalsCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		globalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "optionals",
		Short: "Return a list of optional fields accepted by configured plugins",
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
	}

	addFlagsForOptions(o, cmd)

	return cmd
}

func addFlagsForOptions(o *Options, cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.PluginDir, "plugin-dir", "p", "plugins", "The path where binary plugins are located")
	cmd.Flags().StringVarP(&o.SkipPlugins, "skip-plugins", "s", "", "A comma-separated list of plugins to skip")
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
		if len(thisPlugin.Metadata().OptionalFields) > 0 {
			fmt.Printf("Plugin: %v (version %v)\n", thisPlugin.Metadata().Name, thisPlugin.Metadata().Version)
			for _, field := range thisPlugin.Metadata().OptionalFields {
				fmt.Printf("    %v: %v\n", field.FlagName, field.Help)
				fmt.Printf("        Example: %v\n", field.Example)
			}
		}
	}
	return nil
}
