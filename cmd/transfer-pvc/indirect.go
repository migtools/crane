package transfer_pvc

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/konveyor/crane-lib/state_transfer/transfer/indirect"
)

func (t *TransferPVCCommand) runIndirect() error {
	fmt.Fprintf(os.Stderr, "\ncrane transfer-pvc (indirect via cloud storage)\n")
	fmt.Fprintf(os.Stderr, "source context:      %s\n", t.Flags.SourceContext)
	fmt.Fprintf(os.Stderr, "destination context: %s\n", t.Flags.DestinationContext)
	fmt.Fprintf(os.Stderr, "PVC:                 %s/%s -> %s/%s\n",
		t.PVC.Namespace.source, t.PVC.Name.source,
		t.PVC.Namespace.destination, t.PVC.Name.destination)
	fmt.Fprintf(os.Stderr, "cloud storage:       %s\n", t.Flags.CloudStorage)
	fmt.Fprintln(os.Stderr)

	srcClient, err := t.getClientFromContext(t.Flags.SourceContext)
	if err != nil {
		return fmt.Errorf("unable to get source client: %w", err)
	}
	destClient, err := t.getClientFromContext(t.Flags.DestinationContext)
	if err != nil {
		return fmt.Errorf("unable to get destination client: %w", err)
	}

	srcCfg, err := t.getRestConfigFromContext(t.Flags.SourceContext)
	if err != nil {
		return fmt.Errorf("unable to get source rest config: %w", err)
	}
	destCfg, err := t.getRestConfigFromContext(t.Flags.DestinationContext)
	if err != nil {
		return fmt.Errorf("unable to get destination rest config: %w", err)
	}

	// Read source PVC
	fmt.Fprintf(os.Stderr, "[1/6] Reading source PVC ...\n")
	srcPVC := &corev1.PersistentVolumeClaim{}
	err = srcClient.Get(context.TODO(), client.ObjectKey{
		Namespace: t.PVC.Namespace.source,
		Name:      t.PVC.Name.source,
	}, srcPVC)
	if err != nil {
		return fmt.Errorf("unable to get source PVC: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[1/6] Reading source PVC ... ok\n")

	// Create destination PVC
	fmt.Fprintf(os.Stderr, "[2/6] Creating destination PVC ...\n")
	destPVC := t.buildDestinationPVC(srcPVC)
	err = destClient.Create(context.TODO(), destPVC, &client.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("unable to create destination PVC: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[2/6] Creating destination PVC ... ok\n")

	// Resolve rclone config secret name
	configSecret := t.Flags.RcloneConfigSecret
	if t.Flags.RcloneConfigFile != "" {
		secretName, err := t.createTempRcloneSecret(srcClient, t.PVC.Namespace.source, t.Flags.RcloneConfigFile)
		if err != nil {
			return fmt.Errorf("failed to create rclone config secret on source: %w", err)
		}
		configSecret = secretName

		_, err = t.createTempRcloneSecret(destClient, t.PVC.Namespace.destination, t.Flags.RcloneConfigFile)
		if err != nil {
			return fmt.Errorf("failed to create rclone config secret on destination: %w", err)
		}
	}

	// Get security contexts for source and target separately
	uploadSecCtx, err := getSourcePodSecurityContext(srcClient, srcPVC.Namespace, srcPVC.Name)
	if err != nil {
		log.Printf("WARN: could not determine source security context: %v", err)
		uploadSecCtx = &corev1.PodSecurityContext{}
	}

	downloadSecCtx, err := getTargetPodSecurityContext(destClient, destPVC.Namespace, destPVC.Name)
	if err != nil {
		log.Printf("WARN: could not determine target security context: %v", err)
		downloadSecCtx = &corev1.PodSecurityContext{}
	}
	if downloadSecCtx.RunAsUser == nil && uploadSecCtx.RunAsUser != nil {
		downloadSecCtx = uploadSecCtx
	}

	image := t.Flags.SourceImage
	if image == "" {
		image = "quay.io/konveyor/rsync-transfer:latest"
	}

	transfer := indirect.New(srcClient, destClient, indirect.Options{
		Image:                  image,
		CloudStorage:           t.Flags.CloudStorage,
		ConfigSecret:           configSecret,
		Encrypt:                t.Flags.Encrypt,
		KeepCloudData:          t.Flags.KeepCloudData,
		UploadSecurityContext:  *uploadSecCtx,
		DownloadSecurityContext: *downloadSecCtx,
	})

	// Upload
	fmt.Fprintf(os.Stderr, "[3/6] Uploading data to cloud storage ...\n")
	uploadPod, err := transfer.Upload(context.TODO(), srcPVC)
	if err != nil {
		return fmt.Errorf("upload failed: %w", err)
	}
	if err := followPodLogsUntilComplete(srcCfg, srcClient, uploadPod.Name, uploadPod.Namespace, "rclone"); err != nil {
		return fmt.Errorf("upload pod failed: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[3/6] Uploading data to cloud storage ... ok\n")

	// Download
	fmt.Fprintf(os.Stderr, "[4/6] Downloading data from cloud storage ...\n")
	downloadPod, err := transfer.Download(context.TODO(), destPVC, srcPVC.Namespace, srcPVC.Name)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	if err := followPodLogsUntilComplete(destCfg, destClient, downloadPod.Name, downloadPod.Namespace, "rclone"); err != nil {
		return fmt.Errorf("download pod failed: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[4/6] Downloading data from cloud storage ... ok\n")

	// Cleanup cloud data
	fmt.Fprintf(os.Stderr, "[5/6] Cleaning up cloud storage ...\n")
	if !t.Flags.KeepCloudData {
		fmt.Fprintf(os.Stderr, "[5/6] Cleaning up cloud storage ... skipped (not implemented yet)\n")
	} else {
		fmt.Fprintf(os.Stderr, "[5/6] Cleaning up cloud storage ... skipped (--keep-cloud-data)\n")
	}

	// Cleanup pods
	fmt.Fprintf(os.Stderr, "[6/6] Cleaning up transfer pods ...\n")
	if err := transfer.Cleanup(context.TODO(), srcClient, srcPVC.Namespace, srcPVC.Name); err != nil {
		log.Printf("WARN: source cleanup: %v", err)
	}
	if err := transfer.Cleanup(context.TODO(), destClient, destPVC.Namespace, destPVC.Name); err != nil {
		log.Printf("WARN: destination cleanup: %v", err)
	}
	fmt.Fprintf(os.Stderr, "[6/6] Cleaning up transfer pods ... ok\n")

	fmt.Fprintf(os.Stderr, "\nSummary\n-------\n")
	fmt.Fprintf(os.Stderr, "PVC data copy: succeeded (indirect via cloud storage)\n")
	fmt.Fprintf(os.Stderr, "Done.\n")

	return nil
}

func followPodLogsUntilComplete(restCfg *rest.Config, c client.Client, podName, namespace, containerName string) error {
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	if err := wait.PollUntil(time.Second*3, func() (done bool, err error) {
		pod := &corev1.Pod{}
		if err := c.Get(context.TODO(), client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
			return false, nil
		}
		switch pod.Status.Phase {
		case corev1.PodRunning, corev1.PodSucceeded, corev1.PodFailed:
			return true, nil
		default:
			return false, nil
		}
	}, make(<-chan struct{})); err != nil {
		return fmt.Errorf("timed out waiting for pod %s/%s to start: %w", namespace, podName, err)
	}

	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		Follow:    true,
	})
	stream, err := req.Stream(context.TODO())
	if err != nil {
		return fmt.Errorf("failed to stream logs for pod %s/%s: %w", namespace, podName, err)
	}
	defer stream.Close()

	buf := make([]byte, 4096)
	for {
		n, readErr := stream.Read(buf)
		if n > 0 {
			os.Stderr.Write(buf[:n])
		}
		if readErr != nil {
			break
		}
	}

	pod := &corev1.Pod{}
	if err := c.Get(context.TODO(), client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
		return fmt.Errorf("failed to get pod status: %w", err)
	}
	if pod.Status.Phase == corev1.PodFailed {
		return fmt.Errorf("pod %s/%s failed", namespace, podName)
	}
	return nil
}

func (t *TransferPVCCommand) createTempRcloneSecret(c client.Client, namespace, configFilePath string) (string, error) {
	data, err := os.ReadFile(configFilePath)
	if err != nil {
		return "", fmt.Errorf("failed to read rclone config file %s: %w", configFilePath, err)
	}

	secretName := fmt.Sprintf("crane-rclone-config-%s", getValidatedResourceName(t.PVC.Name.source))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":          "crane",
				"app.kubernetes.io/component":     "indirect-transfer",
				"app.kubernetes.io/managed-by":    "crane",
				"app.konveyor.io/created-for-pvc": getValidatedResourceName(t.PVC.Name.source),
			},
		},
		Data: map[string][]byte{
			"rclone.conf": data,
		},
	}

	err = c.Create(context.TODO(), secret)
	if errors.IsAlreadyExists(err) {
		existing := &corev1.Secret{}
		if getErr := c.Get(context.TODO(), client.ObjectKey{Name: secretName, Namespace: namespace}, existing); getErr != nil {
			return "", fmt.Errorf("failed to get existing rclone secret: %w", getErr)
		}
		existing.Data = secret.Data
		if updateErr := c.Update(context.TODO(), existing); updateErr != nil {
			return "", fmt.Errorf("failed to update rclone secret: %w", updateErr)
		}
	} else if err != nil {
		return "", fmt.Errorf("failed to create rclone secret: %w", err)
	}

	return secretName, nil
}
