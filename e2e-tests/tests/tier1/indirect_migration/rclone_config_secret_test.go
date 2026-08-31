package indirect_migration

import (
	"fmt"
	"log"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// md5sumFile returns the MD5 checksum (hash only) of a file inside a pod.
func md5sumFile(k KubectlRunner, namespace, pod, path string) (string, error) {
	out, err := k.Run("exec", pod, "-n", namespace, "--", "/bin/sh", "-c", fmt.Sprintf("md5sum %s | awk '{print $1}'", path))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(StripKubectlWarnings(out)), nil
}

var _ = Describe("Indirect transfer with a user-provided rclone config Secret", func() {
	It("Should migrate PVC data via S3 when --rclone-config-secret references an existing Secret",
		Label("tier1", "pvc-transfer", "indirect"), func() {

			// This test exercises the --rclone-config-secret path of indirect
			// (cloud/S3) transfer, where the caller pre-creates the Secret holding
			// rclone.conf rather than letting crane build a temporary one from a
			// local file. It requires a working S3-compatible backend, so it only
			// runs when both --cloud-storage and --rclone-config-file are provided.
			if config.CloudStorage == "" || config.RcloneConfigFile == "" {
				Skip("indirect transfer not configured: requires --cloud-storage and --rclone-config-file (source of rclone.conf for the Secret)")
			}

			const (
				testFileName = "testfile.txt"
				rcloneSecret = "crane-rclone-config-e2e"
			)
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
			// SetupActiveKubectlRunners creates the namespace on both clusters and,
			// in non-admin mode, grants namespace-scoped admin to the non-admin user;
			// the returned runners are bound to the active (non-admin) contexts.
			kubectlSrc, kubectlTgt, rbacCleanup, err := SetupActiveKubectlRunners(scenario, namespace)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(rbacCleanup)

			paths, err := NewScenarioPaths("crane-indirect-rclone-secret-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				By("Cleanup source and target resources")
				if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
					log.Printf("cleanup: %v", err)
				}
			})
			DeferCleanup(func() {
				By("Delete test namespace on source and target (wait for completion)")
				for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
					if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
						log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
					}
				}
			})

			By("Deploy a source app with a PVC and seed it with known data")
			Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

			By("Get the source file MD5 checksum")
			srcMD5, err := md5sumFile(kubectlSrc, srcApp.Namespace, appName, "/data/"+testFileName)
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

			By("Verify the referenced Secret exists before the transfer")
			for _, k := range []KubectlRunner{kubectlSrc, kubectlTgt} {
				out, err := k.Run("get", "secret", rcloneSecret, "-n", namespace, "-o", "jsonpath={.data.rclone\\.conf}")
				Expect(err).NotTo(HaveOccurred())
				Expect(strings.TrimSpace(out)).NotTo(BeEmpty(),
					"Secret %q on context %q should contain a rclone.conf key", rcloneSecret, k.Context)
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
      runAsUser: 1000
      allowPrivilegeEscalation: false
      seccompProfile:
        type: RuntimeDefault
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: %s
`, verifierPod, tgtApp.Namespace, pvcName)
			Expect(kubectlTgt.ApplyYAMLSpec(verifierPodYAML, tgtApp.Namespace)).NotTo(HaveOccurred())
			DeferCleanup(func() {
				if _, err := kubectlTgt.Run("delete", "pod", verifierPod, "-n", tgtApp.Namespace, "--ignore-not-found", "--wait=true"); err != nil {
					log.Printf("cleanup verifier pod %q: %v", verifierPod, err)
				}
			})
			_, err = kubectlTgt.Run("wait", "--for=condition=Ready", "pod/"+verifierPod, "-n", tgtApp.Namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())

			tgtMD5, err := md5sumFile(kubectlTgt, tgtApp.Namespace, verifierPod, "/data/"+testFileName)
			Expect(err).NotTo(HaveOccurred())
			Expect(tgtMD5).To(Equal(srcMD5), "MD5 checksum on the migrated PVC should match source")
			log.Printf("Source and target MD5 checksums match: %s\n", srcMD5)

			By("Verify the user-provided rclone config Secret was NOT deleted by crane")
			// crane creates and cleans up its own temporary Secrets only when
			// --rclone-config-file is used; a Secret supplied via
			// --rclone-config-secret is owned by the caller and must be left intact.
			for _, k := range []KubectlRunner{kubectlSrc, kubectlTgt} {
				out, err := k.Run("get", "secret", rcloneSecret, "-n", namespace, "--ignore-not-found=true", "-o", "name")
				Expect(err).NotTo(HaveOccurred())
				Expect(strings.TrimSpace(out)).To(ContainSubstring(rcloneSecret),
					"user-provided Secret %q on context %q should still exist after transfer", rcloneSecret, k.Context)
			}
		})
})
