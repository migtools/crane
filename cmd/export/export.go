package export

import (
	"os"
	"path/filepath"

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
	configFlags *genericclioptions.ConfigFlags

	// Two GlobalFlags struct fields are needed
	// 1. cobraGlobalFlags for explicit CLI args parsed by cobra
	// 2. globalFlags for the args merged with values from the viper config file
	cobraGlobalFlags *flags.GlobalFlags
	globalFlags      *flags.GlobalFlags

	rawConfig api.Config
	exportDir string `mapstructure:"export-dir"`
	// TODO(djzager): how do we handle viper + k8s cli options?
	namespace string

	genericclioptions.IOStreams
}

func (o *ExportOptions) Complete(c *cobra.Command, args []string) error {
	var err error
	o.rawConfig, err = o.configFlags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return err
	}

	o.namespace, _, err = o.configFlags.ToRawKubeConfigLoader().Namespace()
	if err != nil {
		return err
	}

	return nil
}

func (o *ExportOptions) Validate() error {
	// This is where pre-run validations should be performed
	return nil
}

func (o *ExportOptions) Run() error {
	var err error

	log := o.globalFlags.GetLogger()

	// create export directory if it doesnt exist
	err = os.MkdirAll(filepath.Join(o.exportDir, "resources", o.namespace), 0700)
	switch {
	case os.IsExist(err):
	case err != nil:
		log.Errorf("error creating the resources directory: %#v", err)
		return err
	}

	// create export directory if it doesnt exist
	err = os.MkdirAll(filepath.Join(o.exportDir, "failures", o.namespace), 0700)
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

	dynamicClient := dynamic.NewForConfigOrDie(restConfig)

	features.NewFeatureFlagSet()

	discoveryHelper, err := discovery.NewHelper(discoveryClient, log)
	if err != nil {
		log.Errorf("cannot create discovery helper: %#v", err)
		return err
	}

	var errs []error

	resources, resourceErrs := resourceToExtract(o.namespace, dynamicClient, discoveryHelper.Resources(), log)

	log.Debugf("attempting to write resources to files\n")
	writeResourcesErrors := writeResources(resources, filepath.Join(o.exportDir, "resources", o.namespace), log)
	for _, e := range writeResourcesErrors {
		log.Warnf("error writing manifests to file: %#v, ignoring\n", e)
	}

	writeErrorsErrors := writeErrors(resourceErrs, filepath.Join(o.exportDir, "failures", o.namespace), log)
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
		},
	}

	cmd.Flags().StringVarP(&o.exportDir, "export-dir", "e", "export", "The path where files are to be exported")
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}
