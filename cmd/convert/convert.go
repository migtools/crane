package convert

import (
	"github.com/konveyor/crane-lib/convert"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	buildv1 "github.com/openshift/api/build/v1"
	imagev1 "github.com/openshift/api/image/v1"
)

type ConvertOptions struct {
	configFlags *genericclioptions.ConfigFlags
	genericclioptions.IOStreams
	SourceContext      string
	Namespace          string
	Logger             logrus.FieldLogger
	ResourceType       string
	SearchRegistries   []string
	InsecureRegistries []string
	BlockRegistries    []string
	exportDir          string
	debug              bool
}

func NewConvertOptions(streams genericclioptions.IOStreams) *cobra.Command {
	logger := logrus.New()
	logger.SetOutput(streams.Out)
	logger.SetFormatter(&logrus.TextFormatter{})

	t := &ConvertOptions{
		configFlags: genericclioptions.NewConfigFlags(false),
		IOStreams:   streams,
		Logger:      logger,
	}

	cmd := &cobra.Command{
		Use:   "convert",
		Short: "Convert a deprecated resource to its replacement",
		RunE: func(c *cobra.Command, args []string) error {
			if err := t.Complete(c, args); err != nil {
				return err
			}
			//			if err := t.Validate(); err != nil {
			//			return err
			//	}
			if err := t.Run(); err != nil {
				return err
			}

			return nil
		},
	}
	addFlagsForConvertOptions(t, cmd)

	return cmd
}

func addFlagsForConvertOptions(t *ConvertOptions, cmd *cobra.Command) {
	cmd.Flags().StringVar(&t.SourceContext, "source-context", "", "The source context in current kubeconfig")
	cmd.Flags().StringVarP(&t.Namespace, "namespace", "n", "", "The namespace to convert resources from")
	cmd.Flags().StringVarP(&t.ResourceType, "resource", "r", "", "The deprecated plural resource type to convert, e.g. BuildConfigs")
	cmd.Flags().StringSliceVarP(&t.SearchRegistries, "search-registries", "s", []string{}, "List of search registries")
	cmd.Flags().StringSliceVar(&t.InsecureRegistries, "insecure-registries", []string{}, "List of search registries")
	cmd.Flags().StringSliceVar(&t.BlockRegistries, "block-registries", []string{}, "List of search registries")
	cmd.Flags().StringVarP(&t.exportDir, "export-dir", "e", "convert", "The path where files are to be exported")
	cmd.Flags().BoolVar(&t.debug, "debug", false, "Enable debug logging")
}

func (t *ConvertOptions) Complete(c *cobra.Command, args []string) error {
	if t.debug {
		if logger, ok := t.Logger.(*logrus.Logger); ok {
			logger.SetLevel(logrus.DebugLevel)
		}
	}
	return nil
}

func (t *ConvertOptions) Run() error {
	return t.run()
}

func (t *ConvertOptions) run() error {
	srcClient, err := t.getClientFromContext()
	if err != nil {
		return err
	}

	convertOptions := convert.ConvertOptions{
		Client:             srcClient,
		Namespace:          t.Namespace,
		ResourceType:       t.ResourceType,
		SearchRegistries:   t.SearchRegistries,
		InsecureRegistries: t.InsecureRegistries,
		BlockRegistries:    t.BlockRegistries,
		ExportDir:          t.exportDir,
		Logger:             t.Logger,
	}

	err = convertOptions.Convert()
	if err != nil {
		return err
	}

	return nil
}

func (t *ConvertOptions) getClientFromContext() (client.Client, error) {
	err := buildv1.Install(scheme.Scheme)
	if err != nil {
		return nil, err
	}

	err = imagev1.Install(scheme.Scheme)
	if err != nil {
		return nil, err
	}

	restConfig, err := t.getRestConfigFromContext(t.SourceContext)
	if err != nil {
		return nil, err
	}

	restConfig.Burst = 1000
	restConfig.QPS = 100

	return client.New(restConfig, client.Options{Scheme: scheme.Scheme})
}

func (t *ConvertOptions) getRestConfigFromContext(ctx string) (*rest.Config, error) {
	c := ctx
	t.configFlags.Context = &c

	return t.configFlags.ToRESTConfig()
}
