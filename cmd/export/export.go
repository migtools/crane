package export

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/internal/flags"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	velerov1api "github.com/vmware-tanzu/velero/pkg/apis/velero/v1"
	"github.com/vmware-tanzu/velero/pkg/discovery"
	"github.com/vmware-tanzu/velero/pkg/features"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd/api"
)

type ExportOptions struct {
	configFlags *genericclioptions.ConfigFlags

	// Two GlobalFlags struct fields are needed
	// 1. cobraGlobalFlags for explicit CLI args parsed by cobra
	// 2. globalFlags for the args merged with values from the viper config file
	cobraGlobalFlags *flags.GlobalFlags
	globalFlags      *flags.GlobalFlags

	rawConfig              api.Config
	exportDir              string
	userSpecifiedNamespace string
	asExtras               string
	extras                 map[string][]string

	genericclioptions.IOStreams
}

func (o *ExportOptions) Complete(c *cobra.Command, args []string) error {
	var err error

	o.rawConfig, err = o.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return err
	}

	o.userSpecifiedNamespace, _, err = o.configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	if o.asExtras != "" {
		keysAndStrings := strings.Split(o.asExtras, ";")
		o.extras = map[string][]string{}
		for _, keysAndString := range keysAndStrings {
			keyString := strings.Split(keysAndString, "=")
			if len(keyString) != 2 {
				return fmt.Errorf("extra options (%v) formatted incorrectly", o.asExtras)
			}
			o.extras[keyString[0]] = strings.Split(keyString[1], ",")
		}
	}

	return nil
}

func (o *ExportOptions) Validate() error {
	if o.asExtras != "" && *o.configFlags.Impersonate == "" && len(*o.configFlags.ImpersonateGroup) == 0 {
		return fmt.Errorf("extras requires specifying a user or group to impersonate")
	}
	return nil
}

func (o *ExportOptions) Run() error {
	var err error

	log := o.globalFlags.GetLogger()

	// create export directory if it doesnt exist
	err = os.MkdirAll(filepath.Join(o.exportDir, "resources", o.userSpecifiedNamespace), 0700)
	switch {
	case os.IsExist(err):
	case err != nil:
		log.Errorf("error creating the resources directory: %#v", err)
		return err
	}

	// create export directory if it doesnt exist
	err = os.MkdirAll(filepath.Join(o.exportDir, "failures", o.userSpecifiedNamespace), 0700)
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

	// user/group impersonation is handled from genericclioptions.ConfigFlags
	restConfig.Impersonate.Extra = o.extras

	dynamicClient := dynamic.NewForConfigOrDie(restConfig)

	features.NewFeatureFlagSet()
	features.Enable(velerov1api.APIGroupVersionsFeatureFlag)

	discoveryHelper, err := discovery.NewHelper(discoveryClient, log)
	if err != nil {
		log.Errorf("cannot create discovery helper: %#v", err)
		return err
	}

	var errs []error

	resources, resourceErrs := resourceToExtract(o.userSpecifiedNamespace, dynamicClient, discoveryHelper.Resources(), discoveryHelper.APIGroups(), log)

	log.Debugf("attempting to write resources to files\n")
	writeResourcesErrors := writeResources(resources, filepath.Join(o.exportDir, "resources", o.userSpecifiedNamespace), log)
	for _, e := range writeResourcesErrors {
		log.Warnf("error writing manifests to file: %#v, ignoring\n", e)
	}

	writeErrorsErrors := writeErrors(resourceErrs, filepath.Join(o.exportDir, "failures", o.userSpecifiedNamespace), log)
	for _, e := range writeErrorsErrors {
		log.Warnf("error writing errors to file: %#v, ignoring\n", e)
	}

	errs = append(errs, writeResourcesErrors...)
	errs = append(errs, writeErrorsErrors...)

	return errorsutil.NewAggregate(errs)
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
			viper.Unmarshal(&o.globalFlags)
			viper.Unmarshal(&o.configFlags)
			viper.UnmarshalKey("export-dir", &o.exportDir)
		},
	}

	cmd.Flags().StringVarP(&o.exportDir, "export-dir", "e", "export", "The path where files are to be exported")
	cmd.Flags().StringVar(&o.asExtras, "as-extras", "", "The extra info for impersonation can only be used with User or Group but is not required. An example is --as-extras key=string1,string2;key2=string3")
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}
