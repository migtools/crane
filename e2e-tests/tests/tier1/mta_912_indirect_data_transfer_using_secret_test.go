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

// md5sumFileInPod returns the MD5 checksum (hash only) of a file inside a pod.
func md5sumFileInPod(k KubectlRunner, namespace, pod, path string) (string, error) {
	out, err := k.Run("exec", pod, "-n", namespace, "--", "/bin/sh", "-c", fmt.Sprintf("md5sum %s | awk '{print $1}'", path))
	if err != nil {
		return "", fmt.Errorf("md5sum %q in pod %q (namespace %q): %w", path, pod, namespace, err)
	}
	return strings.TrimSpace(StripKubectlWarnings(out)), nil
}

var _ = Describe("Indirect transfer with a user-provided rclone config Secret", func() {
	It("[MTA-912] Should migrate PVC data via S3 when --rclone-config-secret references an existing Secret",
		Label("tier1", "pvc-transfer", "indirect"), func() {

			if config.CloudStorage == "" || config.RcloneConfigFile == "" {
				Skip("indirect transfer not configured: requires --cloud-storage and --rclone-config-file (source of rclone.conf for the Secret)")
			}

			const (
				testFileName = "testfile.txt"
				rcloneSecret = "crane-rclone-config-e2e"
			)
			// "app-with-empty-pvc" is the base k8sdeploy template (its PVC starts
			// empty); add_data:true below seeds a known file into it, so the PVC is
			// non-empty at transfer time. That seeded data is what we checksum on
			// source and verify on target.
			appName := "app-with-empty-pvc"
			namespace := "indirect-rclone-secret-k8s"

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
				"app_name":       appName,
				"add_data":       "true",
				"file_name":      testFileName,
				"file_size":      10,
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

			By("Deploy a source app with a PVC and seed it with known data")
			Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

			By("Get the source file MD5 checksum")
			srcMD5, err := md5sumFileInPod(kubectlSrc, srcApp.Namespace, appName, "/data/"+testFileName)
			Expect(err).NotTo(HaveOccurred())
			Expect(srcMD5).NotTo(BeEmpty(), "expected to compute an MD5 checksum on source")
			log.Printf("Source MD5 checksum: %s\n", srcMD5)

			By("List the PVC created by the source app")
			pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvcs).To(HaveLen(1), "expected exactly one PVC in namespace %q", srcApp.Namespace)
			pvcName := pvcs[0].Name
			log.Printf("Found PVC %s in namespace %q\n", pvcName, srcApp.Namespace)

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

			By("Run crane transfer-pvc in indirect mode using --rclone-config-secret")
			runner := scenario.CraneNonAdmin
			runner.WorkDir = paths.TempDir
			transferOpts := TransferPVCOptions{
				SourceContext:      srcApp.Context,
				TargetContext:      tgtApp.Context,
				PVCName:            pvcName,
				PVCNamespaceMap:    fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
				CloudStorage:       config.CloudStorage,
				RcloneConfigSecret: rcloneSecret,
			}
			Expect(runner.TransferPVC(transferOpts)).NotTo(HaveOccurred(),
				"indirect transfer-pvc should succeed with a valid --rclone-config-secret")

			By("Wait for the ephemeral indirect-transfer pods to be removed")
			Eventually(func() (string, error) {
				return kubectlSrc.Run("get", "pods", "-n", srcApp.Namespace, "-o", "name")
			}, "120s", "3s").ShouldNot(ContainSubstring("rclone"))
			Eventually(func() (string, error) {
				return kubectlTgt.Run("get", "pods", "-n", tgtApp.Namespace, "-o", "name")
			}, "120s", "3s").ShouldNot(ContainSubstring("rclone"))

			By("Verify the destination PVC was created on target")
			tgtPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
			Expect(err).NotTo(HaveOccurred())
			Expect(tgtPVCs).To(HaveLen(1), "expected the migrated PVC to exist on target")

			By("Verify the migrated data matches source via a throwaway verifier pod on the target PVC")
			const verifierPod = "indirect-rclone-secret-verifier"
			// The verifier image runs as root by default, so runAsNonRoot needs an
			// explicit runAsUser on plain Kubernetes. On OpenShift a fixed UID may
			// fall outside the namespace's SCC-assigned range and be rejected at
			// admission, so omit runAsUser there and let the SCC inject one.
			runAsUserLine := "      runAsUser: 1000\n"
			if scenario.KubectlTgt.IsOpenShift() {
				runAsUserLine = ""
			}
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
      mountPath: /data
    securityContext:
      runAsNonRoot: true
%s      allowPrivilegeEscalation: false
      seccompProfile:
        type: RuntimeDefault
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: %s
`, verifierPod, tgtApp.Namespace, runAsUserLine, pvcName)
			Expect(kubectlTgt.ApplyYAMLSpec(verifierPodYAML, tgtApp.Namespace)).NotTo(HaveOccurred())
			DeferCleanup(func() {
				if _, err := kubectlTgt.Run("delete", "pod", verifierPod, "-n", tgtApp.Namespace, "--ignore-not-found", "--wait=true"); err != nil {
					log.Printf("cleanup verifier pod %q: %v", verifierPod, err)
				}
			})
			_, err = kubectlTgt.Run("wait", "--for=condition=Ready", "pod/"+verifierPod, "-n", tgtApp.Namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())

			tgtMD5, err := md5sumFileInPod(kubectlTgt, tgtApp.Namespace, verifierPod, "/data/"+testFileName)
			Expect(err).NotTo(HaveOccurred())
			Expect(tgtMD5).To(Equal(srcMD5), "MD5 checksum on the migrated PVC should match source")
			log.Printf("Source and target MD5 checksums match: %s\n", srcMD5)

			By("Verify the user-provided rclone config Secret was left intact by crane")
			// crane creates and cleans up its own temporary Secrets only when
			// --rclone-config-file is used; a Secret supplied via
			// --rclone-config-secret is owned by the caller and must be left intact
			// (not deleted, replaced, or mutated).
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
