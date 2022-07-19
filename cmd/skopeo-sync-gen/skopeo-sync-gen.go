package skopeo_sync_gen

import (
	"context"
	"encoding/json"
	// "fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/internal/file"
	"github.com/konveyor/crane/internal/flags"
	imagev1 "github.com/openshift/api/image/v1"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

const (
	InternalRegistryDefault = "image-registry.openshift-image-registry.svc:5000"
)

var (
	ImageStreamGroupKind = schema.GroupKind{Group: "image.openshift.io", Kind: "ImageStream"}
)

type Options struct {

	// Two GlobalFlags struct fields are needed
	// 1. cobraGlobalFlags for explicit CLI args parsed by cobra
	// 2. globalFlags for the args merged with values from the viper config file
	cobraGlobalFlags *flags.GlobalFlags
	globalFlags      *flags.GlobalFlags

	Flags
}

type Flags struct {
	ExportDir           string
	RegistryURL         string
	InternalRegistryURL string
}

// Redefining types from skopeo here as they are unexported.
// https://github.com/containers/skopeo/blob/7ddc5ce06cf86bd9d774fe4789db2c495b035619/cmd/skopeo/sync.go#L64-L75
// registrySyncConfig contains information about a single registry, read from
// the source YAML file
type registrySyncConfig struct {
	Images map[string][]string `json:"images"` // Images map images name to slices with the images' references (tags, digests)
	// TODO(djzager): Do we even need to expose these?
	// ImagesByTagRegex map[string]string      `yaml:"images-by-tag-regex"` // Images map images name to regular expression with the images' tags
	// Credentials      types.DockerAuthConfig // Username and password used to authenticate with the registry
	// TLSVerify        tlsVerifyConfig        `yaml:"tls-verify"` // TLS verification mode (enabled by default)
	// CertDir          string                 `yaml:"cert-dir"`   // Path to the TLS certificates of the registry
}

// sourceConfig contains all registries information read from the source YAML file
type sourceConfig map[string]registrySyncConfig

func (o *Options) Complete(c *cobra.Command, args []string) error {
	return nil
}

func (o *Options) Validate() error {
	return nil
}

func NewSkopeoSyncGenCommand(f *flags.GlobalFlags) *cobra.Command {
	o := &Options{
		cobraGlobalFlags: f,
	}
	cmd := &cobra.Command{
		Use:   "skopeo-sync-gen",
		Short: "Generate source yaml for skopeo sync and write the result to stdout",
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

	cmd.Flags().StringVarP(&o.ExportDir, "export-dir", "e", "export", "The path where kube resources are saved")
	cmd.Flags().StringVar(&o.RegistryURL, "registry-url", "", "Publicly accessible URL to registry")
	cmd.Flags().StringVar(&o.InternalRegistryURL, "internal-registry-url", InternalRegistryDefault, "Internal registry hostname[:port] used to determine whether an image should be synced")
	cmd.MarkFlagRequired("registry-url")

	return cmd
}

func shouldAddImageStream(internalRegistryURL string, tags []imagev1.NamedTagEventList) bool {
	for _, tag := range tags {
		for _, item := range tag.Items {
			if strings.HasPrefix(item.DockerImageReference, internalRegistryURL) {
				return true
			}
		}
	}
	return false
}

func (o *Options) Run() error {
	// Load all the resources from the export dir
	exportDir, err := filepath.Abs(o.ExportDir)
	if err != nil {
		// Handle errors better for users.
		return err
	}

	files, err := file.ReadFiles(context.TODO(), exportDir)
	if err != nil {
		return err
	}

	srcConfig := sourceConfig{
		o.RegistryURL: registrySyncConfig{
			Images: map[string][]string{},
		},
	}

	for _, f := range files {
		obj := f.Unstructured
		if obj.GetObjectKind().GroupVersionKind().GroupKind() == ImageStreamGroupKind {
			rawJSON, err := obj.MarshalJSON()
			if err != nil {
				return err
			}
			imageStream := &imagev1.ImageStream{}
			err = json.Unmarshal(rawJSON, imageStream)
			if err != nil {
				return err
			}
			if shouldAddImageStream(o.InternalRegistryURL, imageStream.Status.Tags) {
				imageName := obj.GetNamespace() + "/" + obj.GetName()
				srcConfig[o.RegistryURL].Images[imageName] = []string{}
			}
		}
	}

	yamlBytes, err := yaml.Marshal(srcConfig)
	if err != nil {
		return err
	}
	os.Stdout.Write(yamlBytes)

	return nil
}
