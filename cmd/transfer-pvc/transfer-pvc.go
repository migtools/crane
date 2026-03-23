package transfer_pvc

import (
	"context"
	"crypto/md5"
	"fmt"
	"io"
	"log"
	random "math/rand"
	"os"
	"strings"
	"time"

	logrusr "github.com/bombsimon/logrusr/v3"
	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	errorsutil "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/backube/pvc-transfer/endpoint"
	ingressendpoint "github.com/backube/pvc-transfer/endpoint/ingress"
	routeendpoint "github.com/backube/pvc-transfer/endpoint/route"
	"github.com/backube/pvc-transfer/transfer"
	rsynctransfer "github.com/backube/pvc-transfer/transfer/rsync"
	"github.com/backube/pvc-transfer/transport"
	stunneltransport "github.com/backube/pvc-transfer/transport/stunnel"
	securityv1 "github.com/openshift/api/security/v1"
	openshiftuid "github.com/openshift/library-go/pkg/security/uid"
)

type endpointType string

const (
	endpointNginx endpointType = "nginx-ingress"
	endpointRoute endpointType = "route"
)

type TransferPVCCommand struct {
	configFlags *genericclioptions.ConfigFlags
	genericclioptions.IOStreams
	logger logrus.FieldLogger

	sourceContext      *clientcmdapi.Context
	destinationContext *clientcmdapi.Context

	// user defined flags for the subcommand
	Flags
}

// Flags defines options configured by users
// via command line flags of the subcommand
type Flags struct {
	PVC                PvcFlags
	Endpoint           EndpointFlags
	SourceContext      string
	DestinationContext string
	SourceImage        string
	DestinationImage   string
	Verify             bool
	RsyncFlags         []string
	ProgressOutput     string
}

// EndpointFlags defines command line flags specific
// to the endpoint to be used in transfer
type EndpointFlags struct {
	// Type defines the endpoint type
	Type endpointType
	// Subdomain defines host of the endpoint
	Subdomain string
	// IngressClass defines class for ingress
	IngressClass string
}

func (e EndpointFlags) Validate() error {
	// default endpoint type is nginx-ingress
	if e.Type == "" {
		e.Type = endpointNginx
	}
	switch e.Type {
	case endpointNginx:
		if e.Subdomain == "" {
			return fmt.Errorf("subdomain cannot be empty when using nginx ingress")
		}
	}
	return nil
}

// PvcFlags defines command line flags for the PVC to be transferred
type PvcFlags struct {
	// Name defines Name of the PVC,
	// mapped in format <source>:<destination>
	Name mappedNameVar
	// Namespace defines Namespace of the PVC,
	// mapped in format <source>:<destination>
	Namespace mappedNameVar
	// StorageClassName defines storage class of destination PVC
	StorageClassName string
	// StorageRequests defines requested capacity of destination PVC
	StorageRequests quantityVar
}

func (p *PvcFlags) Validate() error {
	if p.Name.source == "" {
		return fmt.Errorf("source pvc name cannot be empty")
	}
	if p.Name.destination == "" {
		return fmt.Errorf("destnation pvc name cannot be empty")
	}
	if p.Namespace.source == "" {
		return fmt.Errorf("source pvc namespace cannot be empty")
	}
	if p.Namespace.destination == "" {
		return fmt.Errorf("destination pvc namespace cannot be empty")
	}
	return nil
}

func NewTransferPVCCommand(streams genericclioptions.IOStreams) *cobra.Command {
	t := &TransferPVCCommand{
		configFlags: genericclioptions.NewConfigFlags(false),
		Flags: Flags{
			PVC: PvcFlags{
				Name:            mappedNameVar{},
				Namespace:       mappedNameVar{},
				StorageRequests: quantityVar{},
			},
		},
		IOStreams: streams,
		logger:    logrus.New(),
	}

	cmd := &cobra.Command{
		Use:   "transfer-pvc",
		Short: "transfer a pvc data from one kube context to another",
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
	addFlagsToTransferPVCCommand(&t.Flags, cmd)

	return cmd
}

func addFlagsToTransferPVCCommand(c *Flags, cmd *cobra.Command) {
	cmd.Flags().StringVar(&c.SourceContext, "source-context", "", "Name of the source context in current kubeconfig")
	cmd.Flags().StringVar(&c.DestinationContext, "destination-context", "", "Name of the destination context in current kubeconfig")
	cmd.Flags().StringVar(&c.SourceImage, "source-image", "", "The container image to use on the source cluster. Defaults to quay.io/konveyor/esync-transfer:latest")
	cmd.Flags().StringVar(&c.DestinationImage, "destination-image", "", "The container image to use on the destination cluster. Defaults to quay.io/konveyor/rsync-transfer:latest")

	cmd.Flags().Var(&c.PVC.Name, "pvc-name", "Name of the PVC to be transferred. Optionally, source name can be mapped to a different destination name in format <source>:<destination> ")
	cmd.Flags().Var(&c.PVC.Namespace, "pvc-namespace", "Namespace of the PVC to be transferred. Optionally, source namespace can be mapped to a different destination namespace in format <source>:<destination>")
	cmd.Flags().StringVar(&c.PVC.StorageClassName, "dest-storage-class", "", "Storage class for the destination PVC")
	cmd.Flags().Var(&c.PVC.StorageRequests, "dest-storage-requests", "Requested storage capacity for the destination PVC")
	cmd.Flags().Var(&c.Endpoint.Type, "endpoint", "The type of networking endpoint to use to accept traffic in destination cluster. Must be `nginx-ingress` or `route`.")
	cmd.Flags().StringVar(&c.Endpoint.Subdomain, "subdomain", "", "Subdomain to use for the ingress endpoint")
	cmd.Flags().StringVar(&c.Endpoint.IngressClass, "ingress-class", "", "IngressClass to use for the ingress endpoint")
	cmd.Flags().BoolVar(&c.Verify, "verify", false, "Enable checksum verification")
	cmd.Flags().StringVar(&c.ProgressOutput, "output", "", "Write data transfer stats to specified output file")
	cmd.MarkFlagRequired("source-context")
	cmd.MarkFlagRequired("destination-context")
	cmd.MarkFlagRequired("pvc-name")
}

func (t *TransferPVCCommand) Complete(c *cobra.Command, args []string) error {
	config := t.configFlags.ToRawKubeConfigLoader()
	rawConfig, err := config.RawConfig()
	if err != nil {
		return err
	}

	if t.Flags.DestinationContext == "" {
		t.Flags.DestinationContext = *t.configFlags.Context
	}

	for name, context := range rawConfig.Contexts {
		if name == t.Flags.SourceContext {
			t.sourceContext = context
		}
		if name == t.Flags.DestinationContext {
			t.destinationContext = context
		}
	}

	if t.PVC.Namespace.source == "" && t.sourceContext != nil {
		t.PVC.Namespace.source = t.sourceContext.Namespace
	}

	if t.PVC.Namespace.destination == "" && t.destinationContext != nil {
		t.PVC.Namespace.destination = t.destinationContext.Namespace
	}

	return nil
}

func (t *TransferPVCCommand) Validate() error {
	if t.sourceContext == nil {
		return fmt.Errorf("cannot evaluate source context")
	}

	if t.destinationContext == nil {
		return fmt.Errorf("cannot evaluate destination context")
	}

	if t.sourceContext.Cluster == t.destinationContext.Cluster {
		return fmt.Errorf("both source and destination cluster are the same, this is not support right now, coming soon")
	}

	err := t.PVC.Validate()
	if err != nil {
		return err
	}

	err = t.Endpoint.Validate()
	if err != nil {
		return err
	}

	return nil
}

func (t *TransferPVCCommand) Run() error {
	return t.run()
}

func (t *TransferPVCCommand) getClientFromContext(ctx string) (client.Client, error) {
	restConfig, err := t.getRestConfigFromContext(ctx)
	if err != nil {
		return nil, err
	}

	err = routev1.Install(scheme.Scheme)
	if err != nil {
		return nil, err
	}

	if t.Endpoint.Type == endpointRoute {
		err = configv1.AddToScheme(scheme.Scheme)
		if err != nil {
			return nil, err
		}
	}

	return client.New(restConfig, client.Options{Scheme: scheme.Scheme})
}

func (t *TransferPVCCommand) getRestConfigFromContext(ctx string) (*rest.Config, error) {
	c := ctx
	t.configFlags.Context = &c

	return t.configFlags.ToRESTConfig()
}

func (t *TransferPVCCommand) run() error {
	logrusLog := logrus.New()
	logrusLog.SetFormatter(&logrus.JSONFormatter{})
	logger := logrusr.New(logrusLog).WithName("transfer-pvc")

	srcCfg, err := t.getRestConfigFromContext(t.Flags.SourceContext)
	if err != nil {
		log.Fatal(err, "unable to get source rest config")
	}

	srcClient, err := t.getClientFromContext(t.Flags.SourceContext)
	if err != nil {
		log.Fatal(err, "unable to get source client")
	}
	destClient, err := t.getClientFromContext(t.Flags.DestinationContext)
	if err != nil {
		log.Fatal(err, "unable to get destination client")
	}

	// set up the PVC on destination to receive the data
	srcPVC := &corev1.PersistentVolumeClaim{}
	err = srcClient.Get(
		context.TODO(),
		client.ObjectKey{
			Namespace: t.PVC.Namespace.source,
			Name:      t.PVC.Name.source,
		},
		srcPVC,
	)
	if err != nil {
		log.Fatal(err, "unable to get source PVC")
	}

	destPVC := t.buildDestinationPVC(srcPVC)
	err = destClient.Create(context.TODO(), destPVC, &client.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		log.Fatal(err, "unable to create destination PVC")
	}

	labels := map[string]string{
		"app.kubernetes.io/name":          "crane",
		"app.kubernetes.io/component":     "transfer-pvc",
		"app.konveyor.io/created-for-pvc": getValidatedResourceName(srcPVC.Name),
	}

	e, err := createEndpoint(t.Endpoint, destPVC, labels, logger, destClient)
	if err != nil {
		log.Fatal(err, "failed creating endpoint")
	}

	if err := waitForEndpoint(e, destClient); err != nil {
		log.Fatal("endpoint not healthy")
	}

	stunnelServer, err := stunneltransport.NewServer(
		context.TODO(),
		destClient,
		logger,
		types.NamespacedName{
			Name:      getValidatedResourceName(destPVC.Name),
			Namespace: destPVC.Namespace,
		}, e, &transport.Options{
			Labels: labels,
			Image:  t.Flags.DestinationImage,
		})
	if err != nil {
		log.Fatal(err, "error creating stunnel server")
	}

	secretList := &corev1.SecretList{}
	err = destClient.List(
		context.TODO(),
		secretList,
		client.InNamespace(destPVC.Namespace),
		client.MatchingLabels(labels))
	if err != nil {
		log.Fatal(err, "failed to find certificate secrets")
	}

	for i := range secretList.Items {
		destSecret := &secretList.Items[i]
		srcSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:        destSecret.Name,
				Namespace:   srcPVC.Namespace,
				Labels:      destSecret.Labels,
				Annotations: destSecret.Annotations,
			},
			StringData: destSecret.StringData,
			Data:       destSecret.Data,
		}
		err = srcClient.Create(context.TODO(), srcSecret)
		if err != nil {
			log.Fatal(err, "failed to create certificate secret on source cluster")
		}
	}

	stunnelClient, err := stunneltransport.NewClient(
		context.TODO(),
		srcClient,
		logger,
		types.NamespacedName{
			Name:      getValidatedResourceName(srcPVC.Name),
			Namespace: srcPVC.Namespace,
		}, e.Hostname(), e.IngressPort(), &transport.Options{
			Labels: labels,
			Image:  t.Flags.DestinationImage,
		},
	)
	if err != nil {
		log.Fatal(err, "error creating stunnel server")
	}

	destPVCList := transfer.NewSingletonPVC(destPVC)
	srcPVCList := transfer.NewSingletonPVC(srcPVC)

	rsyncPassword := getRsyncPassword()

	serverPodSecContext, err := getRsyncServerPodSecurityContext(destClient, destPVC.Namespace)
	if err != nil {
		log.Fatal(err, "error creating security context for rsync server")
	}

	trueBool := bool(true)
	falseBool := bool(false)
	rsyncServer, err := rsynctransfer.NewServer(
		context.TODO(),
		destClient,
		logger, destPVCList, stunnelServer, e, labels, nil, rsyncPassword,
		transfer.PodOptions{
			ContainerSecurityContext: corev1.SecurityContext{
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
				RunAsNonRoot:             &trueBool,
				AllowPrivilegeEscalation: &falseBool,
				SeccompProfile: &corev1.SeccompProfile{
					Type: corev1.SeccompProfileTypeRuntimeDefault,
				},
			},
			PodSecurityContext: corev1.PodSecurityContext{
				FSGroup: serverPodSecContext.FSGroup,
			},
			Image: t.Flags.DestinationImage,
		},
	)
	if err != nil {
		log.Fatal(err, "error creating rsync transfer server")
	}

	_ = wait.PollUntil(time.Second*5, func() (done bool, err error) {
		ready, err := rsyncServer.IsHealthy(context.TODO(), destClient)
		if err != nil {
			log.Println(err, "unable to check rsync server health, retrying...")
			return false, nil
		}
		return ready, nil
	}, make(<-chan struct{}))

	nodeName, err := getNodeNameForPVC(srcClient, srcPVC.Namespace, srcPVC.Name)
	if err != nil {
		log.Fatal(err, "failed to find node name")
	}

	clientPodSecCtx, err := getRsyncClientPodSecurityContext(srcClient, srcPVC.Namespace)
	if err != nil {
		log.Fatal(err, "error creating security context for rsync server")
	}

	_, err = rsynctransfer.NewClient(
		context.TODO(),
		srcClient, srcPVCList, stunnelClient, e, logger, "rsync-client", labels, nil, rsyncPassword,
		transfer.PodOptions{
			NodeName: nodeName,
			CommandOptions: rsynctransfer.NewDefaultOptionsFrom(
				verify(t.Verify),
				restrictedContainers(true),
				verbose(true),
			),
			ContainerSecurityContext: corev1.SecurityContext{
				Privileged: &falseBool,
				Capabilities: &corev1.Capabilities{
					Drop: []corev1.Capability{"ALL"},
				},
				RunAsNonRoot:             &trueBool,
				RunAsUser:                clientPodSecCtx.RunAsUser,
				AllowPrivilegeEscalation: &falseBool,
			},
			PodSecurityContext: corev1.PodSecurityContext{
				FSGroup: clientPodSecCtx.FSGroup,
			},
			Image: t.Flags.SourceImage,
		},
	)
	if err != nil {
		log.Fatal(err, "failed to create rsync client")
	}

	err = followClientLogs(
		srcCfg, types.NamespacedName{Name: srcPVC.Name, Namespace: srcPVC.Namespace}, labels, t.ProgressOutput)
	if err != nil {
		log.Fatal(err, "error following rsync client logs")
	}

	return garbageCollect(srcClient, destClient, labels, t.Endpoint.Type, t.PVC.Namespace)
}

// getValidatedResourceName returns a name for resources
// created by the command such that they don't fail validations
func getValidatedResourceName(name string) string {
	if len(name) < 63 {
		return name
	} else {
		return fmt.Sprintf("crane-%x", md5.Sum([]byte(name)))
	}
}

// getNodeNameForPVC returns name of the node on which the PVC is currently mounted on
// returns name of the node as a string, and an error
func getNodeNameForPVC(srcClient client.Client, namespace string, pvcName string) (string, error) {
	podList := corev1.PodList{}
	err := srcClient.List(context.TODO(), &podList, client.InNamespace(namespace))
	if err != nil {
		return "", err
	}
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning {
			for _, vol := range pod.Spec.Volumes {
				if vol.PersistentVolumeClaim != nil {
					if vol.PersistentVolumeClaim.ClaimName == pvcName {
						return pod.Spec.NodeName, nil
					}
				}
			}
		}
	}
	return "", nil
}

// getRsyncPassword returns a random password for rsync
func getRsyncPassword() string {
	var letters = []byte("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")
	random.Seed(time.Now().UnixNano())
	password := make([]byte, 6)
	for i := range password {
		password[i] = letters[random.Intn(len(letters))]
	}
	return string(password)
}

func getIDsForNamespace(client client.Client, namespace string) (*corev1.SecurityContext, error) {
	ctx := &corev1.SecurityContext{}
	ns := &corev1.Namespace{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: namespace}, ns)
	if err != nil {
		return nil, err
	}
	if annotationVal, found := ns.Annotations[securityv1.UIDRangeAnnotation]; found {
		uidBlock, err := openshiftuid.ParseBlock(annotationVal)
		if err != nil {
			return nil, nil
		}
		min := int64(uidBlock.Start)
		ctx.RunAsUser = &min
	}
	if annotationVal, found := ns.Annotations[securityv1.SupplementalGroupsAnnotation]; found {
		uidBlock, err := openshiftuid.ParseBlock(annotationVal)
		if err != nil {
			return nil, nil
		}
		min := int64(uidBlock.Start)
		ctx.RunAsGroup = &min
	}
	return ctx, nil
}

func getRsyncClientPodSecurityContext(client client.Client, namespace string) (*corev1.PodSecurityContext, error) {
	ps := &corev1.PodSecurityContext{}
	ctx, err := getIDsForNamespace(client, namespace)
	if err != nil {
		return ps, err
	}
	ps.RunAsUser = ctx.RunAsUser
	ps.RunAsGroup = ctx.RunAsGroup
	ps.FSGroup = ctx.RunAsGroup
	return ps, nil
}

func getRsyncServerPodSecurityContext(client client.Client, namespace string) (*corev1.PodSecurityContext, error) {
	ps := &corev1.PodSecurityContext{}
	ctx, err := getIDsForNamespace(client, namespace)
	if err != nil {
		return ps, err
	}
	ps.RunAsUser = ctx.RunAsUser
	ps.RunAsGroup = ctx.RunAsGroup
	ps.FSGroup = ctx.RunAsGroup
	return ps, nil
}

func garbageCollect(srcClient client.Client, destClient client.Client, labels map[string]string, endpoint endpointType, namespace mappedNameVar) error {
	srcGVK := []client.Object{
		&corev1.Pod{},
		&corev1.ConfigMap{},
		&corev1.Secret{},
	}
	destGVK := []client.Object{
		&corev1.Pod{},
		&corev1.ConfigMap{},
		&corev1.Secret{},
	}
	switch endpoint {
	case endpointRoute:
		destGVK = append(destGVK, &routev1.Route{})
	case endpointNginx:
		destGVK = append(destGVK, &networkingv1.Ingress{})
	}

	err := deleteResourcesForGVK(srcClient, srcGVK, labels, namespace.source)
	if err != nil {
		return err
	}

	err = deleteResourcesForGVK(destClient, destGVK, labels, namespace.destination)
	if err != nil {
		return err
	}

	return deleteResourcesIteratively(destClient, []client.Object{
		&corev1.Service{
			TypeMeta: metav1.TypeMeta{
				Kind:       "Service",
				APIVersion: corev1.SchemeGroupVersion.Version,
			},
		}}, labels, namespace.destination)
}

func deleteResourcesIteratively(c client.Client, iterativeTypes []client.Object, labels map[string]string, namespace string) error {
	listOptions := []client.ListOption{
		client.MatchingLabels(labels),
		client.InNamespace(namespace),
	}
	errs := []error{}
	for _, objList := range iterativeTypes {
		ulist := &unstructured.UnstructuredList{}
		ulist.SetGroupVersionKind(objList.GetObjectKind().GroupVersionKind())
		err := c.List(context.TODO(), ulist, listOptions...)
		if err != nil {
			// if we hit error with one api still try all others
			errs = append(errs, err)
			continue
		}
		for _, item := range ulist.Items {
			err = c.Delete(context.TODO(), &item, client.PropagationPolicy(metav1.DeletePropagationBackground))
			if err != nil {
				// if we hit error deleting on continue delete others
				errs = append(errs, err)
			}
		}
	}
	return errorsutil.NewAggregate(errs)
}

func deleteResourcesForGVK(c client.Client, gvk []client.Object, labels map[string]string, namespace string) error {
	for _, obj := range gvk {
		err := c.DeleteAllOf(context.TODO(), obj, client.InNamespace(namespace), client.MatchingLabels(labels))
		if err != nil {
			return err
		}
	}
	return nil
}

// LogStreams defines functions to read from a stream of pod logs
type LogStreams interface {
	// Init initiates the log streams
	Init() error
	// Streams returns streams for output and error logs
	// returns a stream to communicate errors
	Streams() (stdout chan string, stderr chan string, err chan error)
	// Close closes log streams
	Close()
}

func followClientLogs(srcConfig *rest.Config, pvc types.NamespacedName, labels map[string]string, outputFile string) error {
	logReader := NewRsyncLogStream(srcConfig, pvc, labels, outputFile)
	err := logReader.Init()
	if err != nil {
		return err
	}
	defer logReader.Close()
	stdout, stderr, errChan := logReader.Streams()
	for {
		closed := false
		select {
		case out := <-stdout:
			os.Stdout.WriteString(out)
		case err := <-stderr:
			os.Stderr.WriteString(err)
		case e := <-errChan:
			if e != io.EOF {
				err = e
			}
			closed = true
		}
		if err != nil || closed {
			break
		}
	}
	return err
}

// waitForEndpoint waits for endpoint to become ready
func waitForEndpoint(e endpoint.Endpoint, destClient client.Client) error {
	return wait.PollUntil(time.Second*5, func() (done bool, err error) {
		ready, err := e.IsHealthy(context.TODO(), destClient)
		if err != nil {
			log.Println(err, "unable to check endpoint health, retrying...")
			return false, nil
		}
		return ready, nil
	}, make(<-chan struct{}))
}

// createEndpoint creates an endpoint based on provided endpointFlags
func createEndpoint(
	endpointFlags EndpointFlags, pvc *corev1.PersistentVolumeClaim,
	labels map[string]string, logger logr.Logger, destClient client.Client) (endpoint.Endpoint, error) {
	switch endpointFlags.Type {
	case endpointNginx:
		annotations := map[string]string{
			ingressendpoint.NginxIngressPassthroughAnnotation: "true",
		}
		err := ingressendpoint.AddToScheme(scheme.Scheme)
		if err != nil {
			return nil, err
		}
		e, err := ingressendpoint.New(
			context.TODO(), destClient, logger,
			types.NamespacedName{
				Namespace: pvc.Namespace,
				Name:      getValidatedResourceName(pvc.Name),
			}, &endpointFlags.IngressClass,
			endpointFlags.Subdomain,
			labels, annotations, nil)
		return e, err
	case endpointRoute:
		err := routeendpoint.AddToScheme(scheme.Scheme)
		if err != nil {
			return nil, err
		}
		resourceName := types.NamespacedName{
			Namespace: pvc.Namespace,
			Name:      getValidatedResourceName(pvc.Name),
		}
		hostname, err := getRouteHostName(destClient, resourceName)
		if err != nil {
			return nil, err
		}
		e, err := routeendpoint.New(
			context.TODO(), destClient, logger,
			resourceName, routeendpoint.EndpointTypePassthrough,
			hostname, labels, nil)
		return e, err
	default:
		return nil, fmt.Errorf("unrecognized endpoint type")
	}
}

// getRouteHostName returns a hostname for Route created by the subcommand
func getRouteHostName(client client.Client, namespacedName types.NamespacedName) (*string, error) {
	routeNamePrefix := fmt.Sprintf("%s-%s", namespacedName.Name, namespacedName.Namespace)
	// if route prefix is within limits, default hostname can be used
	if len(routeNamePrefix) <= 62 {
		return nil, nil
	}
	// if route prefix exceeds limits, a custom hostname will be provided
	ingressConfig := &configv1.Ingress{}
	err := client.Get(context.TODO(), types.NamespacedName{Name: "cluster"}, ingressConfig)
	if err != nil {
		return nil, err
	}
	hostname := fmt.Sprintf("%s.%s", routeNamePrefix[:62], ingressConfig.Spec.Domain)
	return &hostname, nil
}

// buildDestinationPVC given a source PVC, returns a PVC to be created in the destination cluster
func (t *TransferPVCCommand) buildDestinationPVC(sourcePVC *corev1.PersistentVolumeClaim) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{}
	pvc.Namespace = t.PVC.Namespace.destination
	pvc.Name = t.PVC.Name.destination
	pvc.Labels = sourcePVC.Labels
	pvc.Spec = *sourcePVC.Spec.DeepCopy()
	if t.PVC.StorageRequests.quantity != nil {
		pvc.Spec.Resources.Requests[corev1.ResourceStorage] = *t.PVC.StorageRequests.quantity
	}
	if t.PVC.StorageClassName != "" {
		pvc.Spec.StorageClassName = &t.PVC.StorageClassName
	}
	// clear fields
	pvc.Spec.VolumeMode = nil
	pvc.Spec.VolumeName = ""
	return pvc
}

// verify enables/disables --checksum option in Rsync
type verify bool

func (v verify) ApplyTo(opts *rsynctransfer.CommandOptions) error {
	if bool(v) {
		opts.Extras = append(opts.Extras, "--checksum")
	} else {
		newExtras := []string{}
		for _, opt := range opts.Extras {
			if opt != "--checksum" &&
				opt != "-c" {
				newExtras = append(newExtras, opt)
			}
		}
		opts.Extras = newExtras
	}
	return nil
}

// restrictedContainers enables/disables Rsync options that
// require privileged containers
type restrictedContainers bool

func (r restrictedContainers) ApplyTo(opts *rsynctransfer.CommandOptions) error {
	opts.Groups = bool(!r)
	opts.Owners = bool(!r)
	opts.DeviceFiles = bool(!r)
	opts.SpecialFiles = bool(!r)
	opts.Extras = append(
		opts.Extras, "--omit-dir-times")
	return nil
}

type verbose bool

func (i verbose) ApplyTo(opts *rsynctransfer.CommandOptions) error {
	opts.Info = []string{
		"COPY", "DEL", "STATS2", "PROGRESS2", "FLIST2",
	}
	opts.Extras = append(opts.Extras, "--progress")
	return nil
}

// mappedNameVar defines a mapping of source to destination names
type mappedNameVar struct {
	source      string
	destination string
}

// String returns string repr of mapped name
// follows format <source>:<destination>
func (m *mappedNameVar) String() string {
	return fmt.Sprintf("%s:%s", m.source, m.destination)
}

func (m *mappedNameVar) Set(val string) error {
	source, destination, err := parseSourceDestinationMapping(val)
	if err != nil {
		return err
	}
	m.source = source
	m.destination = destination
	return nil
}

func (m *mappedNameVar) Type() string {
	return "string"
}

// parseSourceDestinationMapping given a mapping of source to destination names,
// returns two separate strings. mapping follows format <source>:<destination>.
func parseSourceDestinationMapping(mapping string) (source string, destination string, err error) {
	split := strings.Split(string(mapping), ":")
	switch len(split) {
	case 1:
		if split[0] == "" {
			return "", "", fmt.Errorf("source name cannot be empty")
		}
		return split[0], split[0], nil
	case 2:
		if split[1] == "" || split[0] == "" {
			return "", "", fmt.Errorf("source or destination name cannot be empty")
		}
		return split[0], split[1], nil
	default:
		return "", "", fmt.Errorf("invalid name mapping. must be of format <source>:<destination>")
	}
}

type quantityVar struct {
	quantity *resource.Quantity
}

func (q *quantityVar) String() string {
	return q.quantity.String()
}

func (q *quantityVar) Set(val string) error {
	parsedQuantity, err := resource.ParseQuantity(val)
	if err != nil {
		return err
	}
	q.quantity = &parsedQuantity
	return nil
}

func (q *quantityVar) Type() string {
	return "string"
}

func (e endpointType) String() string {
	return string(e)
}

func (e *endpointType) Set(val string) error {
	switch val {
	case string(endpointNginx), string(endpointRoute):
		*e = endpointType(val)
		return nil
	default:
		return fmt.Errorf("unsupported endpoint type %s", val)
	}
}

func (e endpointType) Type() string {
	return "string"
}
