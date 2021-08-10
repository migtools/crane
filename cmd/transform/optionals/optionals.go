package optionals

import (
	"fmt"
	"path/filepath"

	"github.com/konveyor/crane/internal/plugin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type Options struct {
	logger    logrus.FieldLogger
	PluginDir string
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

func NewOptionalsCommand() *cobra.Command {
	o := &Options{
		logger: logrus.New(),
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
}

func (o *Options) run() error {
	pluginDir, err := filepath.Abs(o.PluginDir)
	if err != nil {
		return err
	}

	plugins, err := plugin.GetPlugins(pluginDir)
	if err != nil {
		return err
	}

	for _, plugin := range plugins {
		if len(plugin.Metadata().OptionalFields) > 0 {
			fmt.Printf("Plugin: %v (version %v)\n", plugin.Metadata().Name, plugin.Metadata().Version)
			for _, field := range plugin.Metadata().OptionalFields {
				fmt.Printf("    %v: %v\n", field.FlagName, field.Help)
				fmt.Printf("        Example: %v\n", field.Example)
			}
		}
	}
	return nil
}
