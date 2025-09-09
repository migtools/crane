package list

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	transform2 "github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/plugin"
	"github.com/olekukonko/tablewriter"
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
	Repo      string `mapstructure:"repo"`
	Installed bool   `mapstructure:"installed"`
	PluginDir string `mapstructure:"plugin-dir"`
	Params    bool   `mapstructure:"params"`
	Name      string `mapstructure:"name"`
	Versions  bool   `mapstructure:"versions"`
}

type AvailablePlugins struct {
	Name             string
	ShortDescription string
	Description      string
	Versions         []string
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
	// TODO: display installed plugin information
	cmd.Flags().BoolVarP(&o.Installed, "installed", "", false, "Flag to list installed plugins.")
	cmd.Flags().BoolVarP(&o.Params, "params", "", false, "If passed, returns with metadata information for all the version of specific plugin. This flag is to be used with \"--name\" flag. Takes precedence over \"--versions\" if both passed.")
	cmd.Flags().StringVarP(&o.Name, "name", "", "", "To be used with \"--params\" or \"--versions\" flag to specify the plugin for which additional information is needed. In case of conflict, command fails and asks for a specific repository information.")
	cmd.Flags().BoolVarP(&o.Versions, "versions", "", false, "If passed, returns with all the versions available for a plugin. This flag is to be used with \"--name\" flag.")
}

func (o *Options) run() error {
	log := o.globalFlags.GetLogger()
	if o.Installed {
		// retrieve list of all the plugins that are installed within plugin dir
		// TODO: differentiate between multiple repos
		plugins, err := plugin.GetFilteredPlugins(o.PluginDir, []string{}, log)
		if err != nil {
			return err
		}
		fmt.Println(fmt.Sprintf("Listing plugins from path - %s, along with default plugin", o.PluginDir))
		printInstalledInformation(plugins)
		return nil
	}

	// if the name flag is used then either get all the information of the plugin or get all the version of the plugin
	if o.Name != "" && (o.Params || o.Versions) {
		manifestMap, err := plugin.BuildManifestMap(log, o.Name, o.Repo)
		if err != nil {
			return nil
		}
		if len(manifestMap) == 0 {
			log.Errorf("The plugin %s is not found", o.Name)
			return nil
		}
		if o.Params {
			// retrieve all the information for all the versions available for a specific plugin
			for repo, pluginsMap := range manifestMap {
				fmt.Printf("Listing from the repo %s\n", repo)
				printParamsInformation(pluginsMap)
			}
		} else if o.Versions {
			// retrieve all the available version of the plugin
			for repo, pluginsMap := range manifestMap {
				for name, pluginVersions := range pluginsMap {
					fmt.Printf("Listing versions of plugin %s from the repo %s\n", name, repo)
					for _, pluginVersion := range pluginVersions {
						fmt.Printf("Version: %s\n", pluginVersion.Version)
					}
				}
			}
		} else {
			log.Errorf("name flag must be used with wither params or versions")
		}
	} else {
		manifestMap, err := plugin.BuildManifestMap(log, "", o.Repo)
		if err != nil {
			return nil
		}

		if o.Name != "" {
			log.Info(fmt.Sprintf("\"--name\" flag should be used with either \"--versions\" or \"--params\" flag to get more information about the plugin, example: \"crane plugin-manager --name %s --versions\" or \"crane plugin-manager --name %s --params\"\n", o.Name, o.Name))
		} else if o.Params {
			// retrieve all the information for all the versions available for a specific plugin
			for repo, pluginsMap := range manifestMap {
				fmt.Printf("Listing from the repo %s\n", repo)
				printParamsInformation(pluginsMap)
			}
		} else {
			for repo, pluginsMap := range manifestMap {
				// output information
				fmt.Printf("Listing from the repo %s\n", repo)
				groupInformationForPlugins(pluginsMap)
			}
		}
	}
	return nil
}

//TODO: this can be merged with printParamsInformation
func printInstalledInformation(plugins []transform2.Plugin) {
	for _, thisPlugin := range plugins {
		printTable([][]string{
			{"Name", thisPlugin.Metadata().Name},
			{"Version", thisPlugin.Metadata().Version},
			{"OptionalFields", getOptionalFields(thisPlugin.Metadata().OptionalFields)},
		})
	}
}

func groupInformationForPlugins(pluginsMap map[string][]plugin.PluginVersion) {
	availablePlugin := map[string]AvailablePlugins{}
	for _, pluginVersions := range pluginsMap {
		for _, pluginVersion := range pluginVersions {
			if _, ok := availablePlugin[pluginVersion.Name]; ok {
				availablePlugin[pluginVersion.Name] = AvailablePlugins{Name: pluginVersion.Name, ShortDescription: pluginVersion.ShortDescription, Versions: append(availablePlugin[pluginVersion.Name].Versions, string(pluginVersion.Version))}
			} else {
				availablePlugin[pluginVersion.Name] = AvailablePlugins{
					Name:             pluginVersion.Name,
					ShortDescription: pluginVersion.ShortDescription,
					Versions:         []string{string(pluginVersion.Version)},
				}
			}
		}
	}

	printInformation(availablePlugin)
}

func printInformation(availablePlugins map[string]AvailablePlugins) {
	for _, availablePlugin := range availablePlugins {
		if availablePlugin.Name != "" {
			printTable([][]string{
				{"Name", availablePlugin.Name},
				{"ShortDescription", availablePlugin.ShortDescription},
				{"AvailableVersions", strings.Join(availablePlugin.Versions, ", ")},
			})
		}
	}
}

func printParamsInformation(pluginsMap map[string][]plugin.PluginVersion) {
	for _, pluginVersions := range pluginsMap {
		for _, pluginVersion := range pluginVersions {
			printTable([][]string{
				{"Name", pluginVersion.Name},
				{"ShortDescription", pluginVersion.ShortDescription},
				{"Description", pluginVersion.Description},
				{"AvailableVersions", string(pluginVersion.Version)},
				{"OptionalFields", getOptionalFields(pluginVersion.OptionalFields)},
			})
		}
	}
}

func getOptionalFields(fields []transform2.OptionalFields) string {
	var retstr string
	if len(fields) > 0 {
		var strs []string
		for _, field := range fields {
			optionalField := reflect.ValueOf(&field).Elem()
			typeOfT := optionalField.Type()

			for i := 0; i < optionalField.NumField(); i++ {
				f := optionalField.Field(i)
				var prefix string
				if i == 0 {
					prefix = "- "
				} else {
					prefix = "  "
				}
				strs = append(strs, fmt.Sprintf("%s%s: %v", prefix,
					typeOfT.Field(i).Name, f.Interface()))
			}
		}
		retstr = strings.Join(strs, "\n")
	}
	return retstr
}

func printTable(data [][]string) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetAutoWrapText(false)
	table.AppendBulk(data)
	table.Render()
}
