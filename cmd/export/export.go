package export

import (
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/vmware-tanzu/velero/pkg/discovery"
	"github.com/vmware-tanzu/velero/pkg/features"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd/api"
	"os"
	"path/filepath"
)

type ExportOptions struct {
	configFlags *genericclioptions.ConfigFlags

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
	config := o.configFlags.ToRawKubeConfigLoader()
	rawConfig, err := config.RawConfig()
	if err != nil {
		fmt.Printf("error in generating raw config")
		os.Exit(1)
	}
	if o.Context == "" {
		o.Context = rawConfig.CurrentContext
	}

	if o.Context == "" {
		fmt.Printf("current kubecontext is empty and not kubecontext is specified")
		os.Exit(1)
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
		fmt.Printf("currentContext is nil\n")
		os.Exit(1)
	}

	if o.Namespace == "" {
		fmt.Printf("--namespace is empty, defaulting to current context namespace\n")
		if currentContext.Namespace == "" {
			fmt.Printf("current context %s namespace is empty, exiting\n", contextName)
			os.Exit(1)
		}
		o.Namespace = currentContext.Namespace
	}

	fmt.Printf("current context is: %s\n", currentContext.AuthInfo)

	// create export directory if it doesnt exist
	err = os.MkdirAll(filepath.Join(o.ExportDir, "resources", o.Namespace), 0700)
	switch {
	case os.IsExist(err):
	case err != nil:
		fmt.Printf("error creating the resources directory: %#v", err)
		os.Exit(1)
	}

	// create export directory if it doesnt exist
	err = os.MkdirAll(filepath.Join(o.ExportDir, "failures", o.Namespace), 0700)
	switch {
	case os.IsExist(err):
	case err != nil:
		fmt.Printf("error creating the failures directory: %#v", err)
		os.Exit(1)
	}

	discoveryClient, err := o.configFlags.ToDiscoveryClient()
	if err != nil {
		fmt.Printf("cannot create discovery client: %#v", err)
		os.Exit(1)
	}

	// Always request fresh data from the server
	discoveryClient.Invalidate()

	restConfig, err := o.configFlags.ToRESTConfig()
	if err != nil {
		fmt.Printf("cannot create rest config: %#v", err)
		os.Exit(1)
	}

	dynamicClient := dynamic.NewForConfigOrDie(restConfig)

	features.NewFeatureFlagSet()

	discoveryHelper, err := discovery.NewHelper(discoveryClient, logrus.New())
	if err != nil {
		fmt.Printf("cannot create rest config: %#v", err)
		os.Exit(1)
	}

	errs := []error{}
	resourceList := discoveryHelper.Resources()
	if err != nil {
		fmt.Printf("unauthorized to get discovery service resources: %#v", err)
		return err
	}

	resources, resourceErrs := resourceToExtract(o.Namespace, dynamicClient, resourceList)
	for _, e := range errs {
		fmt.Printf("error exporting resource: %#v\n", e)
	}

	errs = writeResources(resources, filepath.Join(o.ExportDir, "resources", o.Namespace))
	for _, e := range errs {
		fmt.Printf("error writing maniffest to file: %#v\n", e)
	}

	errs = writeErrors(resourceErrs, filepath.Join(o.ExportDir, "failures", o.Namespace))

	return errorsutil.NewAggregate(errs)
}
