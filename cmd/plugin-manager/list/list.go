package list

import (
	"fmt"
	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/plugin"
	"github.com/spf13/cobra"
	"os"
	"text/tabwriter"
)

type Options struct {
	globalFlags *flags.GlobalFlags
	Repo        string
	Installed   bool
	PluginDir   string
	Params      bool
	Name        string
	Versions    bool
}

type AvailablePlugins struct {
	Name             string
	ShortDescription string
	Description      string
	Versions         []plugin.Version
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// TODO: @jgabani
	return nil
}

func (o *Options) Validate() error {
	// TODO: @jgabani
	return nil
}

func (o *Options) Run() error {
	return o.run()
}

func NewListCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		globalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Lists all the available plugins",
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

// TODO: flags to implement repo, installed, name

func addFlagsForOptions(o *Options, cmd *cobra.Command) {
	cmd.Flags().StringVarP(&o.Repo, "repo", "", "", "The name of the repository from which to list the plugins from")
	cmd.Flags().BoolVarP(&o.Installed, "installed", "", false, "Flag to list installed plugins")
	// TODO: look into plugin-dir to see what is installed
	cmd.Flags().StringVarP(&o.PluginDir, "plugin-dir", "p", "plugins", "The path where binary plugins are located")
	cmd.Flags().BoolVarP(&o.Params, "params", "", false, "If passed, returns with metadata information for all the version of specific plugin. This flag is to be used with \"--name\" flag")
	cmd.Flags().StringVarP(&o.Name, "name", "", "", "To be used with \"--params\" or \"--versions\" flag to specify the plugin for which additional information is needed. In case of conflict, command fails and asks for a specific repository information.")
	cmd.Flags().BoolVarP(&o.Versions, "versions", "", false, "If passed, returns with all the versions available for a plugin. This flag is to be used with \"--name\" flag.")
}

func (o *Options) run() error {
	log := o.globalFlags.GetLogger()

	manifestMap, err := plugin.BuildManifestMap(log, "", o.Repo)
	if err != nil {
		return nil
	}

	if o.Name != "" && (o.Params || o.Versions) {
		if o.Params {
			// retrieve all the information for all the versions available for a specific plugin
			for repo, manifests := range manifestMap {
				fmt.Printf("Listing from the repo %s\n", repo)
				for name, manifest := range manifests {
					if manifest.Name != o.Name {
						delete(manifestMap, name)
					} else {
						printParamsInformation(manifest)
					}
				}
			}
		} else if o.Versions {
			// retrieve all the available version of the plugin
			for repo, manifest := range manifestMap {
				fmt.Printf("Listing from the repo %s\n", repo)
				for name, manifest := range manifest {
					if manifest.Name != o.Name {
						delete(manifestMap, name)
					} else {
						fmt.Printf("Version: %s\n", manifest.Version)
					}
				}
			}
		}
	} else {
		for repo, manifest := range manifestMap {
			// output information
			fmt.Printf("Listing from the repo %s\n", repo)
			groupInformationForPlugins(manifest)
		}
	}
	return nil
}

func groupInformationForPlugins(manifestMap map[string]plugin.Manifest) {
	availablePlugin := map[string]AvailablePlugins{}
	for _, manifest := range manifestMap {
		if _, ok := availablePlugin[manifest.Name]; ok {
			availablePlugin[manifest.Name] = AvailablePlugins{Name: manifest.Name, ShortDescription: manifest.ShortDescription, Versions: append(availablePlugin[manifest.Name].Versions, manifest.Version)}
		} else {
			availablePlugin[manifest.Name] = AvailablePlugins{
				Name:             manifest.Name,
				ShortDescription: manifest.ShortDescription,
				Versions:         []plugin.Version{manifest.Version},
			}
		}
	}

	printInformation(availablePlugin)
}

func printInformation(plugins map[string]AvailablePlugins) {
	w := tabwriter.NewWriter(os.Stdout, 1, 1, 1, ' ', 0)
	fmt.Fprintf(w, "Name \t ShortDescription \t AvailableVersions \n")
	for _, plugin := range plugins {
		if plugin.Name != "" {
			fmt.Fprintf(w, "%v \t %v \t %v \n", plugin.Name, plugin.ShortDescription, plugin.Versions)
		}
	}
	w.Flush()
}

func printParamsInformation(plugin plugin.Manifest) {
	w := tabwriter.NewWriter(os.Stdout, 1, 1, 1, ' ', 0)
	fmt.Fprintf(w, "Name \t ShortDescription \t AvailableVersions \t Binaries \n")
	fmt.Fprintf(w, "%v \t %v \t %v \t %#v \n ", plugin.Name, plugin.ShortDescription, plugin.Version, plugin.Binaries)
	w.Flush()
}