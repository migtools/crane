package listplugins

import (
	"fmt"
	"path/filepath"

	"github.com/konveyor/crane/internal/plugin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

type Options struct {
	logger            logrus.FieldLogger
	PluginDir         string
	SkipPlugins       string
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

func NewListPluginsCommand() *cobra.Command {
	o := &Options{
		logger: logrus.New(),
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

	plugins, err := plugin.GetFilteredPlugins(pluginDir, o.SkipPlugins)
	if err != nil {
		return err
	}

	for _, thisPlugin := range plugins {
		fmt.Printf("Plugin: %v (version %v)\n", thisPlugin.Metadata().Name, thisPlugin.Metadata().Version)
	}
	return nil
}
