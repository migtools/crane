package tunnel_api

import (
	"fmt"
	"log"

	"github.com/konveyor/crane-lib/connect/tunnel_api"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type TunnelAPIOptions struct {
	configFlags *genericclioptions.ConfigFlags
	genericclioptions.IOStreams

	logger             logrus.FieldLogger
	SourceContext      string
	DestinationContext string
	Namespace          string
	SourceImage        string
	DestinationImage   string
	ProxyHost          string
	ProxyPort          string
	ProxyUser          string
	ProxyPass          string
	sourceContext      *clientcmdapi.Context
	destinationContext *clientcmdapi.Context
}

func NewTunnelAPIOptions(streams genericclioptions.IOStreams) *cobra.Command {
	t := &TunnelAPIOptions{
		configFlags: genericclioptions.NewConfigFlags(false),

		IOStreams: streams,
		logger:    logrus.New(),
	}

	cmd := &cobra.Command{
		Use:   "tunnel-api",
		Short: "set up an openvpn tunnel to access an (source) on premise cluster from a (cloud) destination cluster",
		RunE: func(c *cobra.Command, args []string) error {
			if err := t.Complete(c, args); err != nil {
				return err
			}
			if err := t.Validate(); err != nil {
				return err
			}
			if err := t.Run(); err != nil {
				return err
			}

			return nil
		},
	}
	addFlagsForTunnelAPIOptions(t, cmd)

	return cmd
}

func addFlagsForTunnelAPIOptions(t *TunnelAPIOptions, cmd *cobra.Command) {
	cmd.Flags().StringVar(&t.SourceContext, "source-context", "", "The name of the source context in current kubeconfig")
	cmd.Flags().StringVar(&t.DestinationContext, "destination-context", "", "The name of destination context current kubeconfig")
	cmd.Flags().StringVar(&t.Namespace, "namespace", "", "The namespace of the pvc which is to be transferred, if empty it will try to use the openvpn namespace")
	cmd.Flags().StringVar(&t.SourceImage, "source-image", "", "The container image to use on the source cluster. Defaults to quay.io/konveyor/openvpn:latest")
	cmd.Flags().StringVar(&t.DestinationImage, "destination-image", "", "The container image to use on the destination cluster. Defaults to quay.io/konveyor/openvpn:latest")
	cmd.Flags().StringVar(&t.ProxyHost, "proxy-host", "", "The hostname of an http-proxy to use on the source cluster for connecting to the destination cluster")
	cmd.Flags().StringVar(&t.ProxyPort, "proxy-port", "", "The port the http-proxy is listening on. If no specified it will default to 3128")
	cmd.Flags().StringVar(&t.ProxyUser, "proxy-user", "", "The username for the http-proxy. If specified you must also specify a password or it will be ignored.")
	cmd.Flags().StringVar(&t.ProxyPass, "proxy-pass", "", "The password for the http-proxy. If specified you must also specify a username or it will be ignored.")
}

func (t *TunnelAPIOptions) Complete(c *cobra.Command, args []string) error {
	config := t.configFlags.ToRawKubeConfigLoader()
	rawConfig, err := config.RawConfig()
	if err != nil {
		return err
	}

	if t.DestinationContext == "" {
		t.DestinationContext = *t.configFlags.Context
	}

	for name, context := range rawConfig.Contexts {
		if name == t.SourceContext {
			t.sourceContext = context
		}
		if name == t.DestinationContext {
			t.destinationContext = context
		}
	}

	return nil
}

func (t *TunnelAPIOptions) Validate() error {
	if t.sourceContext == nil {
		return fmt.Errorf("cannot evaluate source context")
	}

	if t.destinationContext == nil {
		return fmt.Errorf("cannot evaluate destination context")
	}

	if t.sourceContext.Cluster == t.destinationContext.Cluster {
		return fmt.Errorf("both source and destination cluster are same, this is not supported")
	}

	return nil
}

func (t *TunnelAPIOptions) Run() error {
	return t.run()
}

func (t *TunnelAPIOptions) getClientFromContext(ctx string) (client.Client, error) {
	restConfig, err := t.getRestConfigFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return client.New(restConfig, client.Options{Scheme: scheme.Scheme})
}

func (t *TunnelAPIOptions) getRestConfigFromContext(ctx string) (*rest.Config, error) {
	c := ctx
	t.configFlags.Context = &c

	return t.configFlags.ToRESTConfig()
}

func (t *TunnelAPIOptions) run() error {
	tunnel := tunnel_api.Tunnel{}

	fmt.Println("Generating SSL certificates. This may take several minutes.")
	ca, serverCrt, serverKey, clientCrt, clientKey, dh, err := tunnel_api.GenOpenvpnSSLCrts()
	if err != nil {
		return err
	}
	tunnel.Options.CACrt = ca
	tunnel.Options.ServerCrt = serverCrt
	tunnel.Options.ServerKey = serverKey
	tunnel.Options.ClientCrt = clientCrt
	tunnel.Options.ClientKey = clientKey
	tunnel.Options.RSADHKey = dh
	fmt.Println("SSL Certificate generation complete.")

	srcConfig, err := t.getRestConfigFromContext(t.SourceContext)
	if err != nil {
		log.Fatal(err, "unable to get source config")
	}

	dstConfig, err := t.getRestConfigFromContext(t.DestinationContext)
	if err != nil {
		log.Fatal(err, "unable to get destination config")
	}

	_, err = t.getClientFromContext(t.SourceContext)
	if err != nil {
		log.Fatal(err, "unable to get source client")
	}
	_, err = t.getClientFromContext(t.DestinationContext)
	if err != nil {
		log.Fatal(err, "unable to get destination client")
	}

	tunnel.SrcConfig = srcConfig
	tunnel.DstConfig = dstConfig
	tunnel.Options.Namespace = t.Namespace
	tunnel.Options.ClientImage = t.SourceImage
	tunnel.Options.ServerImage = t.DestinationImage
	tunnel.Options.ProxyHost = t.ProxyHost
	tunnel.Options.ProxyPort = t.ProxyPort
	tunnel.Options.ProxyUser = t.ProxyUser
	tunnel.Options.ProxyPass = t.ProxyPass

	err = tunnel_api.Openvpn(tunnel)
	if err != nil {
		log.Fatal(err, "Unable to create Tunnel")
	}

	return nil
}
