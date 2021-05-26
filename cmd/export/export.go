package export

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/vmware-tanzu/velero/pkg/discovery"
	"github.com/vmware-tanzu/velero/pkg/features"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd/api"
)

type ExportOptions struct {
	configFlags *genericclioptions.ConfigFlags

	logger    logrus.FieldLogger
	ExportDir string
	Context   string
	Namespace string
	genericclioptions.IOStreams
}

func (o *ExportOptions) Complete(c *cobra.Command, args []string) error {
	// TODO: @alpatel
	return nil
}

func (o *ExportOptions) Validate() error {
	// TODO: @alpatel
	return nil
}

func (o *ExportOptions) Run() error {
	return o.run()
}

func NewExportCommand(streams genericclioptions.IOStreams) *cobra.Command {
	o := &ExportOptions{
		configFlags: genericclioptions.NewConfigFlags(true),

		IOStreams: streams,
		logger:    logrus.New(),
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
	}

	addFlagsForOptions(o, cmd)

	return cmd
}

func addFlagsForOptions(o *ExportOptions, cmd *cobra.Command) {
	cmd.Flags().StringVar(&o.ExportDir, "export-dir", "export", "The path where files are to be exported")
	cmd.Flags().StringVar(&o.Context, "context", "", "The kube context, if empty it will use the current context. If --namespace is set it will take precedence")
	cmd.Flags().StringVar(&o.Namespace, "namespace", "", "The kube namespace to export.")
}

func (o *ExportOptions) run() error {
	log := o.logger

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
			return fmt.Errorf("current context %s namespace is empty, exiting\n", contextName)
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

	dynamicClient := dynamic.NewForConfigOrDie(restConfig)

	features.NewFeatureFlagSet()

	discoveryHelper, err := discovery.NewHelper(discoveryClient, log)
	if err != nil {
		log.Errorf("cannot create discovery helper: %#v", err)
		return err
	}

	var errs []error

	resources, resourceErrs := resourceToExtract(o.Namespace, dynamicClient, discoveryHelper.Resources(), log)
	for _, e := range resourceErrs {
		log.Warnf("error exporting resource: %#v, ignoring\n", e)
		errs = append(errs, e.Error)
	}

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
