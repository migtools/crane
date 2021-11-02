package export

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/internal/flags"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/vmware-tanzu/velero/pkg/discovery"
	"github.com/vmware-tanzu/velero/pkg/features"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd/api"
)

type ExportOptions struct {
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

	configFlags *genericclioptions.ConfigFlags
	extras      map[string][]string
	genericclioptions.IOStreams
}

type Flags struct {
	ExportDir string `mapstructure:"export-dir"`
	Context   string `mapstructure:"context"`
	Namespace string `mapstructure:"namespace"`

	//User Impersonation Flags
	User  string   `mapstructure:"as-user"`
	Group []string `mapstructure:"as-group"`
	Extra string   `mapstructure:"as-extras"`
}

func (o *ExportOptions) setExtras() error {
	if o.Extra == "" {
		return nil
	}
	keysAndStrings := strings.Split(o.Extra, ";")
	o.extras = map[string][]string{}
	for _, keysAndString := range keysAndStrings {
		keyString := strings.Split(keysAndString, "=")
		if len(keyString) != 2 {
			// Todo: Much better error message here.
			return fmt.Errorf("invalid extra options")
		}
		o.extras[keyString[0]] = strings.Split(keyString[1], ",")
	}
	return nil
}

func (o *ExportOptions) Complete(c *cobra.Command, args []string) error {
	// TODO: @alpatel
	return nil
}

func (o *ExportOptions) Validate() error {
	// TODO: @alpatel
	if len(o.User) == 0 && len(o.Group) == 0 && len(o.Extra) != 0 {
		return fmt.Errorf("if adding extras must also provide a group or user")
	}

	if err := o.setExtras(); err != nil {
		return err
	}
	return nil
}

func (o *ExportOptions) Run() error {
	return o.run()
}

func NewExportCommand(streams genericclioptions.IOStreams, f *flags.GlobalFlags) *cobra.Command {
	o := &ExportOptions{
		configFlags: genericclioptions.NewConfigFlags(true),

		IOStreams:        streams,
		cobraGlobalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "export",
		Short: "Export the namespace resources in an output directory",
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
	cmd.Flags().StringVarP(&o.ExportDir, "export-dir", "e", "export", "The path where files are to be exported")
	cmd.Flags().StringVar(&o.Context, "context", "", "The kube context, if empty it will use the current context. If --namespace is set it will take precedence")
	cmd.Flags().StringVarP(&o.Namespace, "namespace", "n", "", "The kube namespace to export.")
	cmd.Flags().StringVar(&o.User, "as-user", "", "The user to impersonation.")
	cmd.Flags().StringSliceVar(&o.Group, "as-group", nil, "The group to impersonation.")
	cmd.Flags().StringVar(&o.Extra, "as-extras", "", "The extra info for impersonation can only be used with User or Group but is not required. An eample is --as-extras key=string1,string2;key2=string3")
}

func (o *ExportOptions) run() error {
	log := o.globalFlags.GetLogger()

	config := o.configFlags.ToRawKubeConfigLoader()
	rawConfig, err := config.RawConfig()
	if err != nil {
		log.Errorf("error in generating raw config")
		return err
	}
	if o.Context == "" {
		o.Context = rawConfig.CurrentContext
	}

	if o.Context == "" {
		log.Errorf("current kubecontext is empty and not kubecontext is specified")
		return fmt.Errorf("current kubecontext is empty and not kubecontext is specified")
	}

	var currentContext *api.Context
	contextName := ""

	for name, ctx := range rawConfig.Contexts {
		if name == o.Context {
			currentContext = ctx
			contextName = name
		}
	}

	if currentContext == nil {
		log.Errorf("currentContext is nil\n")
		return err
	}

	if o.Namespace == "" {
		log.Debugf("--namespace is empty, defaulting to current context namespace\n")
		if currentContext.Namespace == "" {
			log.Errorf("current context %s namespace is empty, exiting\n", contextName)
			return fmt.Errorf("current context %s namespace is empty, exiting", contextName)
		}
		o.Namespace = currentContext.Namespace
	}

	log.Debugf("current context is: %s\n", currentContext.AuthInfo)

	// create export directory if it doesnt exist
	err = os.MkdirAll(filepath.Join(o.ExportDir, "resources", o.Namespace), 0700)
	switch {
	case os.IsExist(err):
	case err != nil:
		log.Errorf("error creating the resources directory: %#v", err)
		return err
	}

	// create export directory if it doesnt exist
	err = os.MkdirAll(filepath.Join(o.ExportDir, "failures", o.Namespace), 0700)
	switch {
	case os.IsExist(err):
	case err != nil:
		log.Errorf("error creating the failures directory: %#v", err)
		return err
	}

	discoveryClient, err := o.configFlags.ToDiscoveryClient()
	if err != nil {
		log.Errorf("cannot create discovery client: %#v", err)
		return err
	}

	// Always request fresh data from the server
	discoveryClient.Invalidate()

	restConfig, err := o.configFlags.ToRESTConfig()
	if err != nil {
		log.Errorf("cannot create rest config: %#v", err)
		return err
	}

	// Act as th user requested by the CLI
	restConfig.Impersonate.UserName = o.User
	restConfig.Impersonate.Groups = o.Group
	restConfig.Impersonate.Extra = o.extras

	dynamicClient := dynamic.NewForConfigOrDie(restConfig)

	features.NewFeatureFlagSet()

	discoveryHelper, err := discovery.NewHelper(discoveryClient, log)
	if err != nil {
		log.Errorf("cannot create discovery helper: %#v", err)
		return err
	}

	var errs []error

	resources, resourceErrs := resourceToExtract(o.Namespace, dynamicClient, discoveryHelper.Resources(), log)

	log.Debugf("attempting to write resources to files\n")
	writeResourcesErrors := writeResources(resources, filepath.Join(o.ExportDir, "resources", o.Namespace), log)
	for _, e := range writeResourcesErrors {
		log.Warnf("error writing manifests to file: %#v, ignoring\n", e)
	}

	writeErrorsErrors := writeErrors(resourceErrs, filepath.Join(o.ExportDir, "failures", o.Namespace), log)
	for _, e := range writeErrorsErrors {
		log.Warnf("error writing errors to file: %#v, ignoring\n", e)
	}

	errs = append(errs, writeResourcesErrors...)
	errs = append(errs, writeErrorsErrors...)

	return errorsutil.NewAggregate(errs)
}
