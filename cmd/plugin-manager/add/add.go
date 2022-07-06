package add

import (
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"syscall"

	"github.com/konveyor/crane/internal/flags"
	"github.com/konveyor/crane/internal/plugin"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

type Options struct {
	// Two GlobalFlags struct fields are needed
	// 1. cobraGlobalFlags for explicit CLI args parsed by cobra
	// 2. globalFlags for the args merged with values from the viper config fileno go-import meta tags ()

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
	PluginDir string `mapstructure:"plugin-dir"`
	Version   string `mapstructure:"version"`
}

func (o *Options) Complete(c *cobra.Command, args []string) error {
	// TODO: @jgabani
	return nil
}

func (o *Options) Validate(args []string) error {
	// TODO: @jgabani

	if len(args) != 1 {
		return errors.New("please input only one plugin name")
	}
	pluginDir, err := filepath.Abs(fmt.Sprintf("%v/%v", o.ManagedPluginDir(), o.Repo))
	if err != nil {
		return err
	}

	files, err := ioutil.ReadDir(pluginDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	paths, err := plugin.LocateBinaryInPluginDir(o.ManagedPluginDir(), args[0], files)
	if err != nil {
		return err
	}

	if len(paths) > 0 {
		// TODO: if a version is specified and the plugin is installed, have the discussion on what to do here
		for _, path := range paths {
			fmt.Printf("%s \n", path)
		}
		return errors.New("the binary is already installed in the above path, either delete the binary or mention a repo from which the binary is needed")
	}
	return nil
}

func (o *Options) Run(args []string) error {
	return o.run(args)
}

func NewAddCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		globalFlags: f,
	}
	log := o.globalFlags.GetLogger()
	cmd := &cobra.Command{
		Use:   "add <name>",
		Short: "installs the desired plugin",
		RunE: func(c *cobra.Command, args []string) error {
			if err := o.Complete(c, args); err != nil {
				return err
			}
			if err := o.Validate(args); err != nil {
				log.Errorf(fmt.Sprintf("%s", err.Error()))
				return nil
			}
			if err := o.Run(args); err != nil {
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
	cmd.Flags().StringVarP(&o.Version, "version", "", "", "Install specific plugin version (if not passed, installs latest plugin version or the only available one)")
}

func (o *Options) run(args []string) error {
	log := o.globalFlags.GetLogger()

	manifestMap, err := plugin.BuildManifestMap(log, args[0], o.Repo)
	if err != nil {
		return nil
	}

	installVersion := ""
	if o.Version != "" {
		installVersion = o.Version
	}

	if len(manifestMap) == 0 {
		log.Errorf(fmt.Sprintf("The plugin %s is not found", args[0]))
		fmt.Println(fmt.Sprintf("Run \"crane plugin-manager list\" to list all the available plugins \n"))
		return nil
	}

	if len(manifestMap) > 1 {
		// if the plugin is found across multiple repository then fail and ask for a specific repo
		// TODO: if the version is mentioned look for a plugin with the same version, if found in only one repo add the same else fail and ask for the repo
		log.Errorf(fmt.Sprintf("The plugin %s is found across multiple repos, please specify one repo with --repo flag", args[0]))
	}

	// here we iterate over the repos even there should only be one
	for repo, plugins := range manifestMap {
		// TODO(djzager): How should we handle the unlikely scenario where there are two plugins with the same name?
		// for right now what we do here is pick the first one.
		plug := plugins[0]
		pluginVersion, err := plug.GetVersion(installVersion)
		if err != nil {
			log.Error(err, "Failed to download plugin")
			return err
		}
		// TODO(djzager): currently this relies on magic from plugin.YamlToPlugin
		// the modifies the slice of binaries such that there is only ever one
		// binary...the one matching our arch/os. Should likely make this more explicit
		return downloadBinary(o.ManagedPluginDir() + "/" + repo, plug.Name(), pluginVersion.Binaries[0].URI, log)
	}

	return nil
}

func downloadBinary(filepath string, filename string, url string, log *logrus.Logger) error {
	var binaryContents io.Reader
	isUrl, url := plugin.IsUrl(url)
	if !isUrl {
		srcPlugin, err := os.Open(url)
		if err != nil {
			return err
		}
		defer srcPlugin.Close()
		binaryContents = srcPlugin
	} else {
		// Get the data
		resp, err := http.Get(url)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		binaryContents = resp.Body
	}
	// Create dir if not exists
	if _, err := os.Stat(filepath); os.IsNotExist(err) {
		err = os.MkdirAll(filepath, os.ModePerm)
		if err != nil {
			return err
		}
	}

	// Create the file
	pluginBinary, err := os.OpenFile(filepath+"/"+filename, syscall.O_RDWR|syscall.O_CREAT|syscall.O_TRUNC, 0777)
	if err != nil {
		return err
	}
	defer pluginBinary.Close()

	// Write the body to filePluginDir
	_, err = io.Copy(pluginBinary, binaryContents)
	if err != nil {
		return err
	}
	err = pluginBinary.Sync()
	if err != nil {
		return err
	}
	log.Infof("pluginBinary %s added to the path - %s", filename, filepath)
	return err
}

func (o *Options) ManagedPluginDir() string {
	return fmt.Sprintf("%v/%v", o.PluginDir, plugin.MANAGED_DIR)
}
