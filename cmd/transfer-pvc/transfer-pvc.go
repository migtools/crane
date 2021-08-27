package transfer_pvc

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/konveyor/crane-lib/state_transfer"
	"github.com/konveyor/crane-lib/state_transfer/endpoint"
	ls "github.com/konveyor/crane-lib/state_transfer/endpoint/local_service"
	"github.com/konveyor/crane-lib/state_transfer/meta"
	"github.com/konveyor/crane-lib/state_transfer/transfer"
	"github.com/konveyor/crane-lib/state_transfer/transfer/rsync"
	"github.com/konveyor/crane-lib/state_transfer/transport/null"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/client"

	// Auth Things
	_ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
)

type TransferPVCOptions struct {
	configFlags *genericclioptions.ConfigFlags
	genericclioptions.IOStreams

	logger                  logrus.FieldLogger
	SourceContext           string
	DestinationContext      string
	SourcePVCName           string
	SourcePVCNamespace      string
	DestinationPVCName      string
	DestinationPVCNamespace string
	LocalCopy               bool
	SkipQuiesce             bool

	// TODO: add more fields for PVC mapping/think of a config file to get inputs?
	sourceContext      *clientcmdapi.Context
	destinationContext *clientcmdapi.Context
}

func NewTransferOptions(streams genericclioptions.IOStreams) *cobra.Command {
	t := &TransferPVCOptions{
		configFlags: genericclioptions.NewConfigFlags(false),

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
	addFlagsForTransferPVCOptions(t, cmd)

	return cmd
}

func addFlagsForTransferPVCOptions(t *TransferPVCOptions, cmd *cobra.Command) {
	cmd.Flags().StringVar(&t.SourceContext, "source-context", "", "The name of the source context in current kubeconfig")
	cmd.Flags().StringVar(&t.DestinationContext, "destination-context", "", "The name of destination context current kubeconfig")
	cmd.Flags().StringVar(&t.SourcePVCNamespace, "source-pvc-namespace", "", "The namespace of the pvc which is to be transferred, if empty it will try to use the namespace in source-context, if both are empty it will error")
	cmd.Flags().StringVar(&t.SourcePVCName, "source-pvc-name", "", "The pvc name which is to be transferred on the source")
	cmd.Flags().StringVar(&t.DestinationPVCNamespace, "destination-pvc-namespace", "", "The namespace of the pvc which is to be transferred, if empty it will try to use the namespace in destination-context, if both are empty it will error")
	cmd.Flags().StringVar(&t.DestinationPVCName, "destination-pvc-name", "", "The pvc name which is to be transferred on the source")
	cmd.Flags().BoolVarP(&t.LocalCopy, "local", "l", false, "Will use the source context as the destination context. Destination Namespace Must be set")
	cmd.Flags().BoolVarP(&t.SkipQuiesce, "skip-quiesce", "q", false, "Will skip quiesce for the application")
}

func (t *TransferPVCOptions) Complete(c *cobra.Command, args []string) error {
	config := t.configFlags.ToRawKubeConfigLoader()
	rawConfig, err := config.RawConfig()
	if err != nil {
		return err
	}

	if t.LocalCopy && t.DestinationPVCNamespace == "" {
		return fmt.Errorf("local copy with no new Destination PVC Namespace")
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

	if t.LocalCopy {
		t.destinationContext = t.sourceContext
		if t.DestinationPVCName == "" {
			t.DestinationPVCName = t.SourcePVCName
		}
	}

	if t.SourcePVCNamespace == "" && t.sourceContext != nil {
		t.SourcePVCNamespace = t.sourceContext.Namespace
	}

	return nil
}

func (t *TransferPVCOptions) Validate() error {
	if t.SourcePVCName == "" {
		return fmt.Errorf("flag pvc-name is not set")
	}

	if t.SourcePVCNamespace == "" {
		return fmt.Errorf("flag pvc-name is not set and source-context Namespace is empty")
	}

	if t.sourceContext == nil {
		return fmt.Errorf("cannot evaluate source context")
	}

	if t.destinationContext == nil {
		return fmt.Errorf("cannot evaluate destination context")
	}

	return nil
}

func (t *TransferPVCOptions) Run() error {
	return t.run()
}

func (t *TransferPVCOptions) getClientFromContext(ctx string) (client.Client, error) {
	restConfig, err := t.getRestConfigFromContext(ctx)
	if err != nil {
		return nil, err
	}

	return client.New(restConfig, client.Options{Scheme: scheme.Scheme})
}

func (t *TransferPVCOptions) getRestConfigFromContext(ctx string) (*rest.Config, error) {
	c := ctx
	t.configFlags.Context = &c

	return t.configFlags.ToRESTConfig()
}

func (t *TransferPVCOptions) run() error {
	srcCfg, err := t.getRestConfigFromContext(t.SourceContext)
	if err != nil {
		log.Fatal(err, "unable to get source config")
	}

	destCfg, err := t.getRestConfigFromContext(t.DestinationContext)
	if err != nil {
		log.Fatal(err, "unable to get destination config")
	}

	srcClient, err := t.getClientFromContext(t.SourceContext)
	if err != nil {
		log.Fatal(err, "unable to get source client")
	}
	destClient, err := t.getClientFromContext(t.DestinationContext)
	if err != nil {
		log.Fatal(err, "unable to get destination client")
	}

	// quiesce the applications if needed on the source side
	if !t.SkipQuiesce {
		err = state_transfer.QuiesceApplications(srcCfg, t.SourcePVCNamespace)
		if err != nil {
			log.Fatal(err, "unable to quiesce application on source cluster")
		}
	}

	// set up the PVC on destination to receive the data
	pvc := &corev1.PersistentVolumeClaim{}
	err = srcClient.Get(context.TODO(), client.ObjectKey{Namespace: t.SourcePVCNamespace, Name: t.SourcePVCName}, pvc)
	if err != nil {
		log.Fatal(err, "unable to get source PVC")
	}

	destPVC := pvc.DeepCopy()

	clearDestPVC(destPVC)
	destPVC.Namespace = t.DestinationPVCNamespace
	destPVC.Name = t.DestinationPVCName

	pvc.Annotations = map[string]string{}
	err = destClient.Create(context.TODO(), destPVC, &client.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		log.Fatal(err, "unable to create destination PVC")
	}

	pvcList, err := transfer.NewPVCPairList(
		transfer.NewPVCPair(pvc, destPVC),
	)
	if err != nil {
		log.Fatal(err, "invalid pvc list")
	}

	// create a route for data transfer
	l := ls.NewEndpoint(types.NamespacedName{
		Name:      destPVC.Name,
		Namespace: destPVC.Namespace,
	}, map[string]string{})

	e, err := endpoint.Create(l, destClient)
	if err != nil {
		log.Fatal(err, "unable to create endpoint")
	}

	_ = wait.PollUntil(time.Second*5, func() (done bool, err error) {
		ready, err := e.IsHealthy(destClient)
		if err != nil {
			log.Println(err, "unable to check endpoint health, retrying...")
			return false, nil
		}
		return ready, nil
	}, make(<-chan struct{}))

	// create an stunnel transport to carry the data over the route

	s := null.NewTransport(meta.NewNamespacedPair(
		types.NamespacedName{
			Name: pvc.Name, Namespace: pvc.Namespace},
		types.NamespacedName{
			Name: destPVC.Name, Namespace: destPVC.Namespace,
		},
	))

	err = s.CreateServer(destClient, e)
	if err != nil {
		log.Fatal(err, "error creating stunnel server")
	}

	err = s.CreateClient(srcClient, e)
	if err != nil {
		log.Fatal(err, "error creating stunnel client")
	}

	// Rsync Example
	rsyncTransferOptions := []rsync.TransferOption{
		rsync.StandardProgress(true),
		rsync.ArchiveFiles(true),
		rsync.WithSourcePodLabels(map[string]string{"app": "crane2"}),
		rsync.WithDestinationPodLabels(map[string]string{"app": "crane2", "pvc": destPVC.Name}),
		rsync.Username("root"),
	}

	rsyncTransfer, err := rsync.NewTransfer(s, l, srcCfg, destCfg, pvcList, rsyncTransferOptions...)
	if err != nil {
		log.Fatal(err, "error creating rsync transfer")
	} else {
		log.Printf("rsync transfer created for pvc %s\n", rsyncTransfer.PVCs()[0].Source().Claim().Name)
	}

	err = rsyncTransfer.CreateServer(destClient)
	if err != nil {
		log.Fatal(err, "error creating rsync server")
	}

	_ = wait.PollUntil(time.Second*5, func() (done bool, err error) {
		isHealthy, err := rsyncTransfer.IsServerHealthy(destClient)
		if err != nil {
			log.Println(err, "unable to check server health, retrying...")
			return false, nil
		}
		return isHealthy, nil
	}, make(<-chan struct{}))

	// Create Rclone Client Pod
	err = rsyncTransfer.CreateClient(srcClient)
	if err != nil {
		log.Fatal(err, "error creating rsync client")
	} else {
		log.Println("rsync client pod is created, attempting following logs")
	}

	err = followClientLogs(srcCfg, srcClient, t.SourcePVCNamespace, map[string]string{"app": "crane2"})
	if err != nil {
		log.Fatal(err, "error following rsync client logs")
	}

	return nil
}

func followClientLogs(srcConfig *rest.Config, c client.Client, namespace string, labels map[string]string) error {
	clientPod := &corev1.Pod{}

	err := wait.PollUntil(time.Second, func() (done bool, err error) {
		clientPodList := &corev1.PodList{}

		err = c.List(context.Background(), clientPodList, client.InNamespace(namespace), client.MatchingLabels(labels))
		if err != nil {
			return false, err
		}

		if len(clientPodList.Items) != 1 {
			log.Printf("expected 1 client pod found %d, with labels %v\n", len(clientPodList.Items), labels)
			return false, nil
		}

		clientPod = &clientPodList.Items[0]

		for _, containerStatus := range clientPod.Status.ContainerStatuses {
			if !containerStatus.Ready {
				log.Println(fmt.Errorf("container %s in pod %s is not ready", containerStatus.Name, client.ObjectKey{Namespace: namespace, Name: clientPod.Name}))
				return false, nil
			}
		}
		return true, nil
	}, make(<-chan struct{}))
	if err != nil {
		return err
	}

	clienset, err := kubernetes.NewForConfig(srcConfig)
	if err != nil {
		return err
	}

	podLogsRequest := clienset.CoreV1().Pods(namespace).GetLogs(clientPod.Name, &corev1.PodLogOptions{
		TypeMeta:  metav1.TypeMeta{},
		Container: "rsync",
		Follow:    true,
	})

	reader, err := podLogsRequest.Stream(context.Background())
	if err != nil {
		return err
	}

	defer reader.Close()
	_, err = io.Copy(os.Stdout, reader)
	if err != nil {
		return err
	}

	return err
}

func clearDestPVC(destPVC *corev1.PersistentVolumeClaim) {
	// TODO: some of this needs to be configuration option exposed to the user
	destPVC.ResourceVersion = ""
	destPVC.Spec.VolumeName = ""
	destPVC.Annotations = map[string]string{}
	// Test TODO: remove
	destPVC.Labels = map[string]string{}
	destPVC.Spec.StorageClassName = nil
	destPVC.Spec.VolumeMode = nil
	destPVC.Status = corev1.PersistentVolumeClaimStatus{}
}
