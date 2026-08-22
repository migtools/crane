package transfer_pvc

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
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
		configData, err := os.ReadFile(t.Flags.RcloneConfigFile)
		if err != nil {
			return fmt.Errorf("failed to read rclone config file %s: %w", t.Flags.RcloneConfigFile, err)
		}

		if t.Flags.Encrypt {
			if strings.Contains(string(configData), "[encrypted]") {
				return fmt.Errorf("rclone config already contains an [encrypted] section; remove it or omit --encrypt")
			}
			remotePath := fmt.Sprintf("%s/%s/%s", t.Flags.CloudStorage, t.PVC.Namespace.source, t.PVC.Name.source)
			cryptSection, err := generateCryptSection(remotePath)
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
		configSecret = secretName

		_, err = t.createTempRcloneSecretFromData(destClient, t.PVC.Namespace.destination, configData, t.PVC.Name.destination)
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

	defer func() {
		fmt.Fprintf(os.Stderr, "[6/6] Cleaning up transfer pods ...\n")
		if err := transfer.Cleanup(context.TODO(), srcClient, srcPVC.Namespace, srcPVC.Name); err != nil {
			log.Printf("WARN: source cleanup: %v", err)
		}
		if err := transfer.Cleanup(context.TODO(), destClient, destPVC.Namespace, destPVC.Name); err != nil {
			log.Printf("WARN: destination cleanup: %v", err)
		}
		fmt.Fprintf(os.Stderr, "[6/6] Cleaning up transfer pods ... ok\n")
	}()

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
	if t.Flags.KeepCloudData {
		fmt.Fprintf(os.Stderr, "[5/6] Cleaning up cloud storage ... skipped (--keep-cloud-data)\n")
	} else {
		cleanupPod, err := transfer.CleanupCloudData(context.TODO(), srcClient, srcPVC.Namespace, srcPVC.Name, srcPVC.Namespace, srcPVC.Name)
		if err != nil {
			log.Printf("WARN: cloud storage cleanup failed to start: %v", err)
			fmt.Fprintf(os.Stderr, "[5/6] Cleaning up cloud storage ... failed (non-fatal)\n")
		} else {
			if err := followPodLogsUntilComplete(srcCfg, srcClient, cleanupPod.Name, cleanupPod.Namespace, "rclone"); err != nil {
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

func followPodLogsUntilComplete(restCfg *rest.Config, c client.Client, podName, namespace, containerName string) error {
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
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
		return fmt.Errorf("timed out waiting for pod %s/%s to start: %w", namespace, podName, err)
	}

	// Follow logs — also capture output to detect rclone transfer stats
	req := clientset.CoreV1().Pods(namespace).GetLogs(podName, &corev1.PodLogOptions{
		Container: containerName,
		Follow:    true,
	})
	stream, err := req.Stream(context.TODO())
	if err != nil {
		return fmt.Errorf("failed to stream logs for pod %s/%s: %w", namespace, podName, err)
	}
	defer func() {
		if closeErr := stream.Close(); closeErr != nil {
			log.Printf("WARN: failed to close log stream for pod %s/%s: %v", namespace, podName, closeErr)
		}
	}()

	var logOutput strings.Builder
	buf := make([]byte, 4096)
	for {
		n, readErr := stream.Read(buf)
		if n > 0 {
			if _, writeErr := os.Stderr.Write(buf[:n]); writeErr != nil {
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
		return err
	}

	if podFailed {
		return checkRclonePartialSuccess(logOutput.String(), podName, namespace)
	}
	return nil
}

// checkRclonePartialSuccess examines rclone output when the pod exits non-zero.
// rclone treats permission-denied on a single unreadable directory (e.g., MongoDB's
// .mongodb) as a fatal sync error even though all data files transferred successfully.
// This function detects that case and treats it as success when files were transferred.
// It returns an error only when no files were transferred or the failure is not
// a permission issue.
func checkRclonePartialSuccess(output, podName, namespace string) error {
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
		return fmt.Errorf("pod %s/%s failed", namespace, podName)
	}

	if lastTransferred == 0 && lastTotal > 0 {
		return fmt.Errorf("pod %s/%s: rclone transferred 0 of %d files — all files may be unreadable (check UID/permissions)",
			namespace, podName, lastTotal)
	}

	hasPermissionError := strings.Contains(output, "permission denied")
	if lastTransferred > 0 && hasPermissionError {
		log.Printf("WARN: rclone completed with permission errors on %d of %d items (unreadable directories skipped)",
			lastTotal-lastTransferred, lastTotal)
		return nil
	}

	return fmt.Errorf("pod %s/%s failed", namespace, podName)
}

func (t *TransferPVCCommand) createTempRcloneSecretFromData(c client.Client, namespace string, configData []byte, labelPVCName string) (string, error) {
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

// generateCryptSection creates the [encrypted] crypt overlay section for rclone.conf
// with an auto-generated password. The password is random per transfer — encryption
// is for data-in-transit in the bucket, not long-term storage.
func generateCryptSection(cloudStoragePath string) (string, error) {
	password := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, password); err != nil {
		return "", fmt.Errorf("failed to generate encryption password: %w", err)
	}

	obscured, err := rcloneObscure(base64.RawURLEncoding.EncodeToString(password))
	if err != nil {
		return "", fmt.Errorf("failed to obscure encryption password: %w", err)
	}

	return indirect.BuildCryptSection(cloudStoragePath, obscured)
}

// rcloneObscure encodes a plaintext password in rclone's obscured format.
// This is AES-CTR with rclone's well-known key, identical to "rclone obscure".
func rcloneObscure(plaintext string) (string, error) {
	key := []byte{
		0x9c, 0x93, 0x5b, 0x48, 0x73, 0x0a, 0x55, 0x4d,
		0x6b, 0xfd, 0x7c, 0x63, 0xc8, 0x86, 0xa9, 0x2b,
		0xd3, 0x90, 0x19, 0x8e, 0xb8, 0x12, 0x8a, 0xfb,
		0xf4, 0xde, 0x16, 0x2b, 0x8b, 0x95, 0xf6, 0x38,
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	ciphertext := make([]byte, aes.BlockSize+len(plaintext))
	iv := ciphertext[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}
	stream := cipher.NewCTR(block, iv)
	stream.XORKeyStream(ciphertext[aes.BlockSize:], []byte(plaintext))
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}
