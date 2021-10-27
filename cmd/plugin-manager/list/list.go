package list

import (
	"fmt"
	transform2 "github.com/konveyor/crane-lib/transform"
	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/plugin"
	"github.com/olekukonko/tablewriter"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"os"
	"reflect"
	"strings"
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
		plugins, err := plugin.GetFilteredPlugins(o.PluginDir, []string{}, log)
		if err != nil {
			return err
		}
		fmt.Println(fmt.Sprintf("Listing plugins from path - %s", o.PluginDir))
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
			for repo, manifests := range manifestMap {
				fmt.Printf("Listing from the repo %s\n", repo)
				printParamsInformation(manifests)
			}
		} else if o.Versions {
			// retrieve all the available version of the plugin
			for repo, manifest := range manifestMap {
				fmt.Printf("Listing from the repo %s\n", repo)
				for _, manifest := range manifest {
					if manifest.Name == o.Name {
						fmt.Printf("Version: %s\n", manifest.Version)
					}
				}
			}
		} else {
			log.Errorf(fmt.Sprintf("name flag must be used with wither params or versions"))
		}
	} else {
		if o.Name != "" {
			log.Info(fmt.Sprintf("\"--name\" flag should be used with either \"--versions\" or \"--params\" flag to get more information about the plugin, example: \"crane plugin-manager --name %s --versions\" or \"crane plugin-manager --name %s --params\"\n", o.Name, o.Name))
		}
		manifestMap, err := plugin.BuildManifestMap(log, "", o.Repo)
		if err != nil {
			return nil
		}

		for repo, manifest := range manifestMap {
			// output information
			fmt.Printf("Listing from the repo %s\n", repo)
			groupInformationForPlugins(manifest)
		}
	}
	return nil
}

//TODO: this can be merged with printParamsInformation
func printInstalledInformation(plugins []transform2.Plugin) {
	headers := []string {"Name", "Version", "OptionalFields"}
	var data [][]string
	for _, thisPlugin := range plugins {
		data = append(data, []string{thisPlugin.Metadata().Name, thisPlugin.Metadata().Version, getOptionalFields(thisPlugin.Metadata().OptionalFields)})
	}
	printTable(headers, data)
}

func groupInformationForPlugins(manifestMap map[string]plugin.Manifest) {
	availablePlugin := map[string]AvailablePlugins{}
	for _, manifest := range manifestMap {
		if _, ok := availablePlugin[manifest.Name]; ok {
			availablePlugin[manifest.Name] = AvailablePlugins{Name: manifest.Name, ShortDescription: manifest.ShortDescription, Versions: append(availablePlugin[manifest.Name].Versions, string(manifest.Version))}
		} else {
			availablePlugin[manifest.Name] = AvailablePlugins{
				Name:             manifest.Name,
				ShortDescription: manifest.ShortDescription,
				Versions:         []string{string(manifest.Version)},
			}
		}
	}

	printInformation(availablePlugin)
}

func printInformation(plugins map[string]AvailablePlugins) {
	var data [][]string
	header := []string{"Name", "ShortDescription", "AvailableVersions"}
	for _, plugin := range plugins {
		if plugin.Name != "" {
			data = append(data, []string{
				plugin.Name,
				plugin.ShortDescription,
				strings.Join(plugin.Versions, ", "),
			})
		}
	}
	printTable(header, data)
}

func printParamsInformation(manifests map[string]plugin.Manifest) {
	var data [][]string
	header := []string{"Name", "ShortDescription", "Description", "AvailableVersions", "OptionalFields"}

	for _, manifest := range manifests {
		data = append(data,
		[]string{
			manifest.Name,
			manifest.ShortDescription,
			manifest.Description,
			string(manifest.Version),
			getOptionalFields(manifest.OptionalFields),
		})
	}
	printTable(header, data)
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

func printTable(headers []string, data [][]string) {
	table := tablewriter.NewWriter(os.Stdout)
	table.SetAutoWrapText(false)
	table.SetRowSeparator("-")
	table.SetRowLine(true)
	table.SetHeader(headers)
	table.AppendBulk(data)
	table.Render()
}
