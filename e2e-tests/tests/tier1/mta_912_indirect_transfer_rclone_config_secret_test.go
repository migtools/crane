package e2e

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// sha256Hex returns the hex-encoded SHA-256 digest of s. Used to compare the
// rclone.conf Secret contents without printing the (credential-bearing) value
// in an assertion failure message.
func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

// waitForMySQLSocket returns nil once mysqld is accepting connections, which also
// means the role has finished seeding /test-data.
func waitForMySQLSocket(k KubectlRunner, namespace, podName string) error {
	_, err := k.Run(
		"exec", podName, "-n", namespace, "--",
		"sh", "-c",
		`test -S /var/lib/mysql/mysql.sock`,
	)
	return err
}

// md5OfFileInPod returns the MD5 checksum (hash only) of a file inside a pod.
func md5OfFileInPod(k KubectlRunner, namespace, pod, path string) (string, error) {
	out, err := k.Run("exec", pod, "-n", namespace, "--", "/bin/sh", "-c",
		fmt.Sprintf("md5sum %s | awk '{print $1}'", path))
	if err != nil {
		return "", fmt.Errorf("md5sum %q in pod %q (namespace %q): %w", path, pod, namespace, err)
	}
	return strings.TrimSpace(StripKubectlWarnings(out)), nil
}

var _ = Describe("Indirect transfer with a user-provided rclone config Secret", func() {
	It("[MTA-912] Should migrate MySQL PVC data via S3 when --rclone-config-secret references an existing Secret",
		Label("tier1", "pvc-transfer", "indirect"), func() {

			if config.CloudStorage == "" || config.RcloneConfigFile == "" {
				Skip("indirect transfer not configured: requires --cloud-storage and --rclone-config-file (source of rclone.conf for the Secret)")
			}

			const (
				rcloneSecret = "crane-rclone-config-e2e"
				// The mysql role seeds /test-data/test1 plus a companion test1.md5,
				// mounted from the "<app>-data1" PVC — a byte-exact, self-checking
				// fingerprint that survives the S3 round-trip as a plain file.
				testDataFile = "/test-data/test1"
				md5File      = "/test-data/test1.md5"
			)
			appName := "mysql"
			namespace := "indirect-rclone-secret-mysql"

			scenario := NewMigrationScenario(
				appName,
				namespace,
				config.K8sDeployBin,
				config.CraneBin,
				config.SourceContext,
				config.TargetContext,
			)
			srcApp := scenario.SrcAppNonAdmin
			tgtApp := scenario.TgtAppNonAdmin

			isOCP := scenario.KubectlSrc.IsOpenShift()
			srcApp.ExtraVars = map[string]any{
				"non_admin_user": "true",
				"has_scc":        isOCP,
			}

			By("Grant namespace-admin permissions to the non-admin user on source and target")
			kubectlSrc, kubectlTgt, rbacCleanup, err := SetupActiveKubectlRunners(scenario, namespace)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				By("Delete test namespace on source and target (wait for completion)")
				for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
					if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
						log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
					}
				}
			})
			DeferCleanup(rbacCleanup)

			paths, err := NewScenarioPaths("crane-indirect-rclone-secret-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				By("Cleanup source and target resources")
				if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
					log.Printf("cleanup: %v", err)
				}
			})

			By("Deploy the source MySQL app (auto-seeds the authors table and /test-data/test1)")
			Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

			By("Capture the source test-data fingerprint")
			srcPod, err := GetPodNameByLabel(kubectlSrc, srcApp.Namespace, "app="+appName)
			Expect(err).NotTo(HaveOccurred())
			Expect(srcPod).NotTo(BeEmpty(), "expected a running MySQL pod on source")
			Eventually(func() error {
				return waitForMySQLSocket(kubectlSrc, srcApp.Namespace, srcPod)
			}, "2m", "5s").Should(Succeed())
			srcMD5, err := md5OfFileInPod(kubectlSrc, srcApp.Namespace, srcPod, testDataFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(srcMD5).NotTo(BeEmpty(), "expected to compute an MD5 for the source test-data file")
			log.Printf("Source test-data MD5: %s", srcMD5)

			// crane's indirect transfer reads a Secret of the SAME name from both
			// the source and destination namespaces (upload pod reads it on source,
			// download pod reads it on destination), keyed by "rclone.conf".
			By("Pre-create the rclone config Secret on both source and target namespaces")
			for _, k := range []KubectlRunner{kubectlSrc, kubectlTgt} {
				// Delete first so the test is idempotent across reruns.
				if _, err := k.Run("delete", "secret", rcloneSecret, "-n", namespace, "--ignore-not-found=true"); err != nil {
					log.Printf("pre-clean of Secret %q on context %q: %v", rcloneSecret, k.Context, err)
				}
				_, err := k.Run("create", "secret", "generic", rcloneSecret,
					"-n", namespace,
					"--from-file=rclone.conf="+config.RcloneConfigFile)
				Expect(err).NotTo(HaveOccurred(),
					"failed to create rclone config Secret %q in namespace %q on context %q", rcloneSecret, namespace, k.Context)
			}

			By("Capture the referenced Secret's identity and contents before the transfer")
			// Snapshot the UID, data.rclone.conf, and labels per context so that,
			// after the transfer, we can assert crane left the caller-provided Secret
			// untouched (neither replaced, mutated, nor relabeled), not merely still
			// present.
			preUID := map[string]string{}
			preData := map[string]string{}
			preLabels := map[string]string{}
			for _, k := range []KubectlRunner{kubectlSrc, kubectlTgt} {
				uid, err := k.Run("get", "secret", rcloneSecret, "-n", namespace, "-o", "jsonpath={.metadata.uid}")
				Expect(err).NotTo(HaveOccurred())
				data, err := k.Run("get", "secret", rcloneSecret, "-n", namespace, "-o", "jsonpath={.data.rclone\\.conf}")
				Expect(err).NotTo(HaveOccurred())
				Expect(strings.TrimSpace(data)).NotTo(BeEmpty(),
					"Secret %q on context %q should contain a rclone.conf key", rcloneSecret, k.Context)
				labels, err := k.Run("get", "secret", rcloneSecret, "-n", namespace, "-o", "jsonpath={.metadata.labels}")
				Expect(err).NotTo(HaveOccurred())
				preUID[k.Context] = strings.TrimSpace(uid)
				preData[k.Context] = strings.TrimSpace(data)
				preLabels[k.Context] = strings.TrimSpace(labels)
			}

			By("Quiesce the source app so the PVC copy is consistent and the volumes are released")
			Expect(kubectlSrc.ScaleDeploymentIfPresent(srcApp.Namespace, appName, 0)).NotTo(HaveOccurred())
			Eventually(func() (string, error) {
				out, err := kubectlSrc.Run("get", "pods", "-n", namespace, "-l", "app="+appName, "-o", "name")
				return strings.TrimSpace(out), err
			}, "90s", "3s").Should(BeEmpty())

			By("List the PVCs created by the source app")
			pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvcs).NotTo(BeEmpty(), "expected at least one PVC in namespace %q", srcApp.Namespace)
			log.Printf("Found %d PVCs in namespace %q", len(pvcs), srcApp.Namespace)

			By("Transfer each PVC in indirect mode using --rclone-config-secret")
			runner := scenario.CraneNonAdmin
			runner.WorkDir = paths.TempDir
			for _, pvc := range pvcs {
				opts := TransferPVCOptions{
					SourceContext:      srcApp.Context,
					TargetContext:      tgtApp.Context,
					PVCName:            pvc.Name,
					PVCNamespaceMap:    fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
					CloudStorage:       config.CloudStorage,
					RcloneConfigSecret: rcloneSecret,
				}
				log.Printf("Transferring PVC %s via indirect S3", pvc.Name)
				Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred(),
					"indirect transfer-pvc should succeed for PVC %q with a valid --rclone-config-secret", pvc.Name)
			}

			By("Wait for the ephemeral indirect-transfer pods to be removed")
			Eventually(func() (string, error) {
				return kubectlSrc.Run("get", "pods", "-n", srcApp.Namespace, "-o", "name")
			}, "120s", "3s").ShouldNot(ContainSubstring("rclone"))
			Eventually(func() (string, error) {
				return kubectlTgt.Run("get", "pods", "-n", tgtApp.Namespace, "-o", "name")
			}, "120s", "3s").ShouldNot(ContainSubstring("rclone"))

			By("Verify all destination PVCs exist and contain data")
			tgtPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
			Expect(err).NotTo(HaveOccurred())
			Expect(VerifyPVCsExistByName(pvcs, tgtPVCs)).NotTo(HaveOccurred())
			var dataPVC string
			for _, pvc := range tgtPVCs {
				Expect(VerifyPVCSchedulable(scenario.KubectlTgt.Context, tgtApp.Namespace, pvc.Name)).NotTo(HaveOccurred())
				mountPath := "/var/lib/mysql"
				if strings.Contains(pvc.Name, "data1") {
					mountPath = "/test-data"
					dataPVC = pvc.Name
				}
				Expect(VerifyPVCHasData(kubectlTgt, tgtApp.Namespace, pvc.Name, mountPath)).NotTo(HaveOccurred())
			}
			Expect(dataPVC).NotTo(BeEmpty(), "expected to find the migrated /test-data PVC (<app>-data1) on target")

			By("Verify the migrated test-data file is byte-for-byte identical to source")
			// Mount the migrated test-data PVC in a throwaway pod and compare the
			// file's MD5 both to the checksum recorded alongside it (self-check) and
			// to the source MD5. This proves the indirect S3 round-trip preserved the
			// bytes without needing to boot MySQL on the copied data dir (rclone does
			// not reconstruct a live datadir the way the direct/rsync path does).
			const verifierPod = "indirect-mysql-verifier"
			verifierPodYAML := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  containers:
  - name: verifier
    image: quay.io/openshifttest/alpine:multiarch
    command: ["sleep", "300"]
    volumeMounts:
    - name: data
      mountPath: /test-data
    securityContext:
      runAsNonRoot: true
      runAsUser: 1000
      allowPrivilegeEscalation: false
      seccompProfile:
        type: RuntimeDefault
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: %s
`, verifierPod, tgtApp.Namespace, dataPVC)
			Expect(kubectlTgt.ApplyYAMLSpec(verifierPodYAML, tgtApp.Namespace)).NotTo(HaveOccurred())
			DeferCleanup(func() {
				if _, err := kubectlTgt.Run("delete", "pod", verifierPod, "-n", tgtApp.Namespace, "--ignore-not-found", "--wait=true"); err != nil {
					log.Printf("cleanup verifier pod %q: %v", verifierPod, err)
				}
			})
			_, err = kubectlTgt.Run("wait", "--for=condition=Ready", "pod/"+verifierPod, "-n", tgtApp.Namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())

			tgtMD5, err := md5OfFileInPod(kubectlTgt, tgtApp.Namespace, verifierPod, testDataFile)
			Expect(err).NotTo(HaveOccurred())
			recordedMD5, err := kubectlTgt.Run("exec", verifierPod, "-n", tgtApp.Namespace, "--", "/bin/sh", "-c",
				fmt.Sprintf("awk '{print $1}' %s", md5File))
			Expect(err).NotTo(HaveOccurred())
			Expect(tgtMD5).To(Equal(strings.TrimSpace(StripKubectlWarnings(recordedMD5))),
				"migrated test-data checksum should match its recorded md5")
			Expect(tgtMD5).To(Equal(srcMD5),
				"migrated test-data checksum should match the source")
			log.Printf("Source and target test-data MD5 match: %s", tgtMD5)

			By("Verify the user-provided rclone config Secret was left intact by crane")
			for _, k := range []KubectlRunner{kubectlSrc, kubectlTgt} {
				out, err := k.Run("get", "secret", rcloneSecret, "-n", namespace, "--ignore-not-found=true", "-o", "name")
				Expect(err).NotTo(HaveOccurred())
				Expect(strings.TrimSpace(out)).To(ContainSubstring(rcloneSecret),
					"user-provided Secret %q on context %q should still exist after transfer", rcloneSecret, k.Context)

				uid, err := k.Run("get", "secret", rcloneSecret, "-n", namespace, "-o", "jsonpath={.metadata.uid}")
				Expect(err).NotTo(HaveOccurred())
				Expect(strings.TrimSpace(uid)).To(Equal(preUID[k.Context]),
					"user-provided Secret %q on context %q should not be replaced (UID changed)", rcloneSecret, k.Context)

				data, err := k.Run("get", "secret", rcloneSecret, "-n", namespace, "-o", "jsonpath={.data.rclone\\.conf}")
				Expect(err).NotTo(HaveOccurred())
				Expect(sha256Hex(strings.TrimSpace(data))).To(Equal(sha256Hex(preData[k.Context])),
					"user-provided Secret %q on context %q should not be mutated (rclone.conf changed)", rcloneSecret, k.Context)

				labels, err := k.Run("get", "secret", rcloneSecret, "-n", namespace, "-o", "jsonpath={.metadata.labels}")
				Expect(err).NotTo(HaveOccurred())
				Expect(strings.TrimSpace(labels)).To(Equal(preLabels[k.Context]),
					"user-provided Secret %q on context %q should not be relabeled by crane", rcloneSecret, k.Context)
				Expect(labels).NotTo(ContainSubstring("app.kubernetes.io/component"),
					"crane must not add its indirect-transfer labels to a caller-provided Secret %q on context %q", rcloneSecret, k.Context)
			}
		})
})
