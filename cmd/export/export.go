// Package export implements the crane export subcommand: discover API types,
// list objects in a namespace and related cluster-scoped RBAC (CRB, CR, SCC),
// and write manifests and list failures under an export directory.
package export

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/internal/flags"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd/api"
)

// ExportOptions holds CLI flags and runtime state for a single export run.
type ExportOptions struct {
	configFlags *genericclioptions.ConfigFlags

	// Two GlobalFlags struct fields are needed
	// 1. cobraGlobalFlags for explicit CLI args parsed by cobra
	// 2. globalFlags for the args merged with values from the viper config file
	cobraGlobalFlags *flags.GlobalFlags
	globalFlags      *flags.GlobalFlags

	rawConfig              api.Config
	exportDir              string
	labelSelector          string
	userSpecifiedNamespace string
	asExtras               string
	extras                 map[string][]string
	QPS                    float32
	Burst                  int

	genericclioptions.IOStreams
}

// Complete loads kubeconfig context, namespace, and parses --as-extras into o.extras.
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

// Validate checks flag combinations (e.g. --as-extras requires impersonation).
func (o *ExportOptions) Validate() error {
	if o.asExtras != "" && *o.configFlags.Impersonate == "" && len(*o.configFlags.ImpersonateGroup) == 0 {
		return fmt.Errorf("extras requires specifying a user or group to impersonate")
	}
	return nil
}

// Run performs discovery, lists resources, filters cluster-scoped RBAC to related
// ServiceAccounts, writes YAML under exportDir, and returns an aggregate of non-fatal write errors.
func (o *ExportOptions) Run() error {
	var err error

	log := o.globalFlags.GetLogger()

	// create export directory if it doesnt exist
	resourceDir := filepath.Join(o.exportDir, "resources", o.userSpecifiedNamespace)
	err = os.MkdirAll(resourceDir, 0700)
	switch {
	case os.IsExist(err):
	case err != nil:
		log.Errorf("error creating the resources directory: %#v", err)
		return err
	}
	// create failures directory if it doesnt exist
	failuresDir := filepath.Join(o.exportDir, "failures", o.userSpecifiedNamespace)
	if err = prepareFailuresDir(failuresDir); err != nil {
		log.Errorf("error preparing the failures directory: %#v", err)
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
	restConfig.Burst = o.Burst
	restConfig.QPS = o.QPS

	dynamicClient := dynamic.NewForConfigOrDie(restConfig)

	resourceLists, err := discoverPreferredResources(discoveryClient, log)
	if err != nil {
		return err
	}

	var errs []error

	resources, resourceErrs := resourceToExtract(o.userSpecifiedNamespace, o.labelSelector, dynamicClient, resourceLists, log)
	clusterScopeHandler := NewClusterScopeHandler()
	resources = clusterScopeHandler.filterRbacResources(resources, log)

	// create cluster resources directory if it needs to be created
	clusterResourceDir := filepath.Join(o.exportDir, "resources", o.userSpecifiedNamespace, "_cluster")
	if err = prepareClusterResourceDir(clusterResourceDir, resources); err != nil {
		log.Errorf("error preparing cluster resources directory: %#v", err)
		return err
	}

	crdResources, crdErrs := collectRelatedCRDs(resources, dynamicClient, log)
	resourceErrs = append(resourceErrs, crdErrs...)
	resources = append(resources, crdResources...)

	//count and log the no of crds
	crdCount := len(crdResources)
	if crdCount > 0 {
		log.Infof("Exported %d CRDs for referenced custom resources to the _cluster resources directory\n", crdCount)
	}

	log.Debugf("attempting to write resources to files\n")
	writeResourcesErrors := writeResources(resources, clusterResourceDir, resourceDir, log)
	for _, e := range writeResourcesErrors {
		log.Warnf("error writing manifests to file: %#v, ignoring\n", e)
	}

	writeErrorsErrors := writeErrors(resourceErrs, failuresDir, log)
	for _, e := range writeErrorsErrors {
		log.Warnf("error writing errors to file: %#v, ignoring\n", e)
	}

	errs = append(errs, writeResourcesErrors...)
	errs = append(errs, writeErrorsErrors...)

	return errorsutil.NewAggregate(errs)
}

// NewExportCommand builds the cobra export command with flags and viper wiring.
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
	cmd.Flags().StringVarP(&o.labelSelector, "label-selector", "l", "", "Restrict export to resources matching a label selector")
	cmd.Flags().StringVar(&o.asExtras, "as-extras", "", "The extra info for impersonation can only be used with User or Group but is not required. An example is --as-extras key=string1,string2;key2=string3")
	cmd.Flags().Float32VarP(&o.QPS, "qps", "q", 100, "Query Per Second Rate.")
	cmd.Flags().IntVarP(&o.Burst, "burst", "b", 1000, "API Burst Rate.")
	o.configFlags.AddFlags(cmd.Flags())

	return cmd
}
