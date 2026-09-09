package transfer_pvc

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"strings"
	"time"

	crlog "github.com/konveyor/crane/internal/log"
	"github.com/sirupsen/logrus"

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
	log := t.log
	crlog.InitControllerRuntimeLogger("transfer-pvc")
	log.Infof("Starting indirect PVC transfer: %s/%s -> %s/%s", t.PVC.Namespace.source, t.PVC.Name.source, t.PVC.Namespace.destination, t.PVC.Name.destination)

	fmt.Fprintf(os.Stderr, "\ncrane transfer-pvc (indirect via cloud storage)\n")
	fmt.Fprintf(os.Stderr, "source context:      %s\n", t.SourceContext)
	fmt.Fprintf(os.Stderr, "destination context: %s\n", t.DestinationContext)
	fmt.Fprintf(os.Stderr, "PVC:                 %s/%s -> %s/%s\n",
		t.PVC.Namespace.source, t.PVC.Name.source,
		t.PVC.Namespace.destination, t.PVC.Name.destination)
	fmt.Fprintf(os.Stderr, "cloud storage:       %s\n", t.CloudStorage)
	fmt.Fprintln(os.Stderr)

	srcClient, err := t.getClientFromContext(t.SourceContext)
	if err != nil {
		log.Debugf("Unable to get source client: %v", err)
		return fmt.Errorf("unable to get source client: %w", err)
	}
	destClient, err := t.getClientFromContext(t.DestinationContext)
	if err != nil {
		log.Debugf("Unable to get destination client: %v", err)
		return fmt.Errorf("unable to get destination client: %w", err)
	}

	srcCfg, err := t.getRestConfigFromContext(t.SourceContext)
	if err != nil {
		log.Debugf("Unable to get source rest config: %v", err)
		return fmt.Errorf("unable to get source rest config: %w", err)
	}
	destCfg, err := t.getRestConfigFromContext(t.DestinationContext)
	if err != nil {
		log.Debugf("Unable to get destination rest config: %v", err)
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
		log.Debugf("Unable to get source PVC %s/%s: %v", t.PVC.Namespace.source, t.PVC.Name.source, err)
		return fmt.Errorf("unable to get source PVC: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[1/6] Reading source PVC ... ok\n")

	// Resolve rclone config secret name and validate before creating destination resources
	configSecret := t.RcloneConfigSecret
	if t.RcloneConfigFile != "" {
		configData, err := os.ReadFile(t.RcloneConfigFile)
		if err != nil {
			log.Debugf("Failed to read rclone config file: %v", err)
			return fmt.Errorf("failed to read rclone config file %s: %w", t.RcloneConfigFile, err)
		}
		if len(bytes.TrimSpace(configData)) == 0 {
			return fmt.Errorf("rclone config file %s is empty", t.RcloneConfigFile)
		}

		if t.Encrypt {
			if strings.Contains(string(configData), "[encrypted]") {
				log.Debugf("Rclone config already contains an [encrypted] section")
				return fmt.Errorf("rclone config already contains an [encrypted] section; remove it or omit --encrypt")
			}
			remotePath := fmt.Sprintf("%s/%s/%s", t.CloudStorage, t.PVC.Namespace.source, t.PVC.Name.source)
			cryptSection, err := generateCryptSection(remotePath, log)
			if err != nil {
				return fmt.Errorf("failed to generate encryption config: %w", err)
			}
			configData = append(configData, '\n')
			configData = append(configData, []byte(cryptSection)...)
		}

		secretName, err := t.createTempRcloneSecretFromData(srcClient, t.PVC.Namespace.source, configData, t.PVC.Name.source)
		if err != nil {
			return fmt.Errorf("failed to create rclone config secret on source: %w", err)
		}
		defer func() {
			if err := srcClient.Delete(context.TODO(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: t.PVC.Namespace.source},
			}); err != nil && !errors.IsNotFound(err) {
				log.Warnf("Failed to delete temp rclone config secret %q in source namespace %q: %v", secretName, t.PVC.Namespace.source, err)
			}
		}()
		configSecret = secretName

		destSecretName, err := t.createTempRcloneSecretFromData(destClient, t.PVC.Namespace.destination, configData, t.PVC.Name.destination)
		if err != nil {
			return fmt.Errorf("failed to create rclone config secret on destination: %w", err)
		}
		defer func() {
			if err := destClient.Delete(context.TODO(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: destSecretName, Namespace: t.PVC.Namespace.destination},
			}); err != nil && !errors.IsNotFound(err) {
				log.Warnf("Failed to delete temp rclone config secret %q in destination namespace %q: %v", destSecretName, t.PVC.Namespace.destination, err)
			}
		}()
	}

	// Validate the secret that will actually be used for the transfer, regardless
	// of whether it came from --rclone-config-secret or was created from
	// --rclone-config-file
	if configSecret != "" {
		if err := t.validateRcloneConfigSecret(configSecret, srcClient, destClient); err != nil {
			return err
		}
	}

	// Create destination PVC
	fmt.Fprintf(os.Stderr, "[2/6] Creating destination PVC ...\n")
	destPVC := t.buildDestinationPVC(srcPVC)
	err = t.createDestinationPVC(context.TODO(), destClient, destPVC)
	if err != nil {
		log.Debugf("Unable to create destination PVC %s/%s: %v", destPVC.Namespace, destPVC.Name, err)
		return fmt.Errorf("unable to create destination PVC: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[2/6] Creating destination PVC ... ok\n")

	// Get security contexts for source and target separately
	uploadSecCtx, err := getSourcePodSecurityContext(srcClient, srcPVC.Namespace, srcPVC.Name, t.SourceImage)
	if err != nil {
		log.Warnf("Could not determine source security context: %v", err)
		uploadSecCtx = &corev1.PodSecurityContext{}
	}

	// Indirect transfer uses a single Image (SourceImage) for upload and download.
	downloadSecCtx, err := getTargetPodSecurityContext(destClient, destPVC.Namespace, destPVC.Name, t.SourceImage)
	if err != nil {
		log.Warnf("Could not determine target security context: %v", err)
		downloadSecCtx = &corev1.PodSecurityContext{}
	}
	if downloadSecCtx.RunAsUser == nil && uploadSecCtx.RunAsUser != nil {
		downloadSecCtx = uploadSecCtx
	}

	transfer := indirect.New(srcClient, destClient, indirect.Options{
		Image:                   t.SourceImage,
		CloudStorage:            t.CloudStorage,
		ConfigSecret:            configSecret,
		Encrypt:                 t.Encrypt,
		KeepCloudData:           t.KeepCloudData,
		UploadSecurityContext:   *uploadSecCtx,
		DownloadSecurityContext: *downloadSecCtx,
	})

	defer func() {
		fmt.Fprintf(os.Stderr, "[6/6] Cleaning up transfer pods ...\n")
		if err := transfer.Cleanup(context.TODO(), srcClient, srcPVC.Namespace, srcPVC.Name); err != nil {
			log.Warnf("Source cleanup warning: %v", err)
		}
		if err := transfer.Cleanup(context.TODO(), destClient, destPVC.Namespace, destPVC.Name); err != nil {
			log.Warnf("Destination cleanup warning: %v", err)
		}
		fmt.Fprintf(os.Stderr, "[6/6] Cleaning up transfer pods ... ok\n")
	}()

	// Upload
	fmt.Fprintf(os.Stderr, "[3/6] Uploading data to cloud storage ...\n")
	uploadPod, err := transfer.Upload(context.TODO(), srcPVC)
	if err != nil {
		log.Debugf("Upload failed: %v", err)
		return fmt.Errorf("upload failed: %w", err)
	}
	if err := followPodLogsUntilComplete(srcCfg, srcClient, uploadPod.Name, uploadPod.Namespace, "rclone", log); err != nil {
		return fmt.Errorf("upload pod failed: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[3/6] Uploading data to cloud storage ... ok\n")

	// Download
	fmt.Fprintf(os.Stderr, "[4/6] Downloading data from cloud storage ...\n")
	downloadPod, err := transfer.Download(context.TODO(), destPVC, srcPVC.Namespace, srcPVC.Name)
	if err != nil {
		log.Debugf("Download failed: %v", err)
		return fmt.Errorf("download failed: %w", err)
	}
	if err := followPodLogsUntilComplete(destCfg, destClient, downloadPod.Name, downloadPod.Namespace, "rclone", log); err != nil {
		return fmt.Errorf("download pod failed: %w", err)
	}
	fmt.Fprintf(os.Stderr, "[4/6] Downloading data from cloud storage ... ok\n")

	// Cleanup cloud data
	fmt.Fprintf(os.Stderr, "[5/6] Cleaning up cloud storage ...\n")
	if t.KeepCloudData {
		fmt.Fprintf(os.Stderr, "[5/6] Cleaning up cloud storage ... skipped (--keep-cloud-data)\n")
	} else {
		cleanupPod, err := transfer.CleanupCloudData(context.TODO(), srcClient, srcPVC.Namespace, srcPVC.Name, srcPVC.Namespace, srcPVC.Name)
		if err != nil {
			log.Printf("WARN: cloud storage cleanup failed to start: %v", err)
			fmt.Fprintf(os.Stderr, "[5/6] Cleaning up cloud storage ... failed (non-fatal)\n")
		} else {
			if err := followPodLogsUntilComplete(srcCfg, srcClient, cleanupPod.Name, cleanupPod.Namespace, "rclone", log); err != nil {
				log.Printf("WARN: cloud storage cleanup failed: %v", err)
				fmt.Fprintf(os.Stderr, "[5/6] Cleaning up cloud storage ... failed (non-fatal)\n")
			} else {
				fmt.Fprintf(os.Stderr, "[5/6] Cleaning up cloud storage ... ok\n")
			}
		}
	}

	fmt.Fprintf(os.Stderr, "\nSummary\n-------\n")
	fmt.Fprintf(os.Stderr, "PVC data copy: succeeded (indirect via cloud storage)\n")
	fmt.Fprintf(os.Stderr, "Done.\n")

	return nil
}

func followPodLogsUntilComplete(restCfg *rest.Config, c client.Client, podName, namespace, containerName string, log *logrus.Logger) error {
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		log.Debugf("Failed to create clientset: %v", err)
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	// Wait for pod to be running (5-minute timeout to catch ImagePullBackOff, Unschedulable, etc.)
	startCtx, startCancel := context.WithTimeout(context.TODO(), 5*time.Minute)
	defer startCancel()
	if err := wait.PollUntilContextCancel(startCtx, 3*time.Second, true, func(ctx context.Context) (bool, error) {
		pod := &corev1.Pod{}
		if err := c.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
			return false, nil
		}
		switch pod.Status.Phase {
		case corev1.PodRunning, corev1.PodSucceeded, corev1.PodFailed:
			return true, nil
		default:
			return false, nil
		}
	}); err != nil {
		log.Debugf("Timed out waiting for pod %s/%s to start: %v", namespace, podName, err)
		return fmt.Errorf("timed out waiting for pod %s/%s to start: %w", namespace, podName, err)
	}

	// Follow logs — also capture output to detect rclone transfer stats
	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		Follow:    true,
	})
	stream, err := req.Stream(context.TODO())
	if err != nil {
		log.Debugf("Failed to stream logs for pod %s/%s: %v", namespace, podName, err)
		return fmt.Errorf("failed to stream logs for pod %s/%s: %w", namespace, podName, err)
	}
	defer func() {
		if closeErr := stream.Close(); closeErr != nil {
			log.Warnf("Failed to close log stream for pod %s/%s: %v", namespace, podName, closeErr)
		}
	}()

	var logOutput strings.Builder
	buf := make([]byte, 4096)
	for {
		n, readErr := stream.Read(buf)
		if n > 0 {
			if _, writeErr := os.Stderr.Write(buf[:n]); writeErr != nil {
				log.Debugf("Failed to write logs for pod %s/%s: %v", namespace, podName, writeErr)
				return fmt.Errorf("failed to write logs for pod %s/%s: %w", namespace, podName, writeErr)
			}
			logOutput.Write(buf[:n])
		}
		if readErr != nil {
			break
		}
	}

	// Check final pod status — wait for terminal state in case the log stream
	// ended before the pod completed (e.g. network interruption).
	waitCtx, waitCancel := context.WithTimeout(context.TODO(), 2*time.Minute)
	defer waitCancel()
	var podFailed bool
	if err := wait.PollUntilContextCancel(waitCtx, 3*time.Second, true, func(ctx context.Context) (bool, error) {
		pod := &corev1.Pod{}
		if err := c.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod); err != nil {
			log.Debugf("Failed to get pod status for %s/%s: %v", namespace, podName, err)
			return false, fmt.Errorf("failed to get pod status: %w", err)
		}
		switch pod.Status.Phase {
		case corev1.PodSucceeded:
			return true, nil
		case corev1.PodFailed:
			podFailed = true
			return true, nil
		default:
			return false, nil
		}
	}); err != nil {
		log.Errorf("Failed waiting for pod %s/%s terminal state: %v", namespace, podName, err)
		return err
	}

	if podFailed {
		return checkRclonePartialSuccess(logOutput.String(), podName, namespace, log)
	}
	return nil
}

// checkRclonePartialSuccess examines rclone output when the pod exits non-zero.
// rclone treats permission-denied on a single unreadable directory (e.g., MongoDB's
// .mongodb) as a fatal sync error even though all data files transferred successfully.
// This function detects that case and treats it as success when files were transferred.
// It returns an error only when no files were transferred or the failure is not
// a permission issue.
func checkRclonePartialSuccess(output, podName, namespace string, log *logrus.Logger) error {
	// Parse the last "Transferred: N / M, P%" file count line
	var lastTransferred, lastTotal int
	fileCountFound := false
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "Transferred:") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}
		// Skip byte-count lines (contain B, KiB, MiB, GiB units)
		if strings.ContainsAny(parts[1], "BMKGi") {
			continue
		}
		var n, m int
		if _, err := fmt.Sscanf(parts[1], "%d", &n); err != nil {
			continue
		}
		// parts[2] is "/" separator, parts[3] is total with possible comma
		totalStr := strings.TrimRight(parts[3], ",")
		if _, err := fmt.Sscanf(totalStr, "%d", &m); err != nil {
			continue
		}
		lastTransferred = n
		lastTotal = m
		fileCountFound = true
	}

	if !fileCountFound {
		log.Debugf("Pod %s/%s failed: no file count found in rclone output", namespace, podName)
		return fmt.Errorf("pod %s/%s failed", namespace, podName)
	}

	if lastTransferred == 0 && lastTotal > 0 {
		log.Debugf("Pod %s/%s: rclone transferred 0 of %d files", namespace, podName, lastTotal)
		return fmt.Errorf("pod %s/%s: rclone transferred 0 of %d files — all files may be unreadable (check UID/permissions)",
			namespace, podName, lastTotal)
	}

	hasPermissionError := strings.Contains(output, "permission denied")
	if lastTransferred > 0 && hasPermissionError {
		log.Printf("WARN: rclone completed with permission errors (unreadable files/directories skipped), %d of %d files transferred",
			lastTransferred, lastTotal)
		return nil
	}

	log.Debugf("Pod %s/%s failed", namespace, podName)
	return fmt.Errorf("pod %s/%s failed", namespace, podName)
}

func (t *TransferPVCCommand) validateRcloneConfigSecret(secretName string, srcClient, destClient client.Client) error {
	for _, check := range []struct {
		c         client.Client
		namespace string
		side      string
	}{
		{srcClient, t.PVC.Namespace.source, "source"},
		{destClient, t.PVC.Namespace.destination, "destination"},
	} {
		secret := &corev1.Secret{}
		if err := check.c.Get(context.TODO(), client.ObjectKey{
			Name: secretName, Namespace: check.namespace,
		}, secret); err != nil {
			return fmt.Errorf("rclone config secret %q (namespace %q, %s cluster): %w",
				secretName, check.namespace, check.side, err)
		}
		if len(bytes.TrimSpace(secret.Data["rclone.conf"])) == 0 {
			return fmt.Errorf("rclone config secret %q in namespace %q on %s cluster is missing the rclone.conf key or it is empty",
				secretName, check.namespace, check.side)
		}
	}
	return nil
}

func (t *TransferPVCCommand) createTempRcloneSecretFromData(c client.Client, namespace string, configData []byte, labelPVCName string) (string, error) {
	log := t.log
	secretName := fmt.Sprintf("crane-rclone-config-%s", getValidatedResourceName(t.PVC.Name.source))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":          "crane",
				"app.kubernetes.io/component":     "indirect-transfer",
				"app.kubernetes.io/managed-by":    "crane",
				"app.konveyor.io/created-for-pvc": getValidatedResourceName(labelPVCName),
			},
		},
		Data: map[string][]byte{
			"rclone.conf": configData,
		},
	}

	err := c.Create(context.TODO(), secret)
	if errors.IsAlreadyExists(err) {
		existing := &corev1.Secret{}
		if getErr := c.Get(context.TODO(), client.ObjectKey{Name: secretName, Namespace: namespace}, existing); getErr != nil {
			log.Debugf("Failed to get existing rclone secret %q: %v", secretName, getErr)
			return "", fmt.Errorf("failed to get existing rclone secret: %w", getErr)
		}
		existing.Data = secret.Data
		if updateErr := c.Update(context.TODO(), existing); updateErr != nil {
			log.Debugf("Failed to update rclone secret %q: %v", secretName, updateErr)
			return "", fmt.Errorf("failed to update rclone secret: %w", updateErr)
		}
	} else if err != nil {
		log.Debugf("Failed to create rclone secret %q: %v", secretName, err)
		return "", fmt.Errorf("failed to create rclone secret: %w", err)
	}

	return secretName, nil
}

// generateCryptSection creates the [encrypted] crypt overlay section for rclone.conf
// with an auto-generated password. The password is random per transfer — encryption
// is for data-in-transit in the bucket, not long-term storage.
func generateCryptSection(cloudStoragePath string, log *logrus.Logger) (string, error) {
	password := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, password); err != nil {
		log.Debugf("Failed to generate encryption password: %v", err)
		return "", fmt.Errorf("failed to generate encryption password: %w", err)
	}

	obscured, err := rcloneObscure(base64.RawURLEncoding.EncodeToString(password), log)
	if err != nil {
		return "", fmt.Errorf("failed to obscure encryption password: %w", err)
	}

	return indirect.BuildCryptSection(cloudStoragePath, obscured)
}

// rcloneObscure encodes a plaintext password in rclone's obscured format.
// This is AES-CTR with rclone's well-known key, identical to "rclone obscure".
func rcloneObscure(plaintext string, log *logrus.Logger) (string, error) {
	key := []byte{
		0x9c, 0x93, 0x5b, 0x48, 0x73, 0x0a, 0x55, 0x4d,
		0x6b, 0xfd, 0x7c, 0x63, 0xc8, 0x86, 0xa9, 0x2b,
		0xd3, 0x90, 0x19, 0x8e, 0xb8, 0x12, 0x8a, 0xfb,
		0xf4, 0xde, 0x16, 0x2b, 0x8b, 0x95, 0xf6, 0x38,
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		log.Errorf("Failed to create AES cipher: %v", err)
		return "", err
	}
	ciphertext := make([]byte, aes.BlockSize+len(plaintext))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		log.Errorf("Failed to generate IV: %v", err)
		return "", err
	}
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], []byte(plaintext))
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}
