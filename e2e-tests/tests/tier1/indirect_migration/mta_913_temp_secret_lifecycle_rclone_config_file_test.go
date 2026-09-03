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
		return "", fmt.Errorf("md5sum %q in pod %q (namespace %q): %w", path, pod, namespace, err)
	}
	return strings.TrimSpace(StripKubectlWarnings(out)), nil
}

var _ = Describe("Indirect transfer with a crane-managed rclone config file", func() {
	It("[MTA-913] Should create a temporary rclone config Secret from --rclone-config-file and clean it up after transfer",
		Label("tier1", "pvc-transfer", "indirect"), func() {

			if config.CloudStorage == "" || config.RcloneConfigFile == "" {
				Skip("indirect transfer not configured: requires --cloud-storage and --rclone-config-file")
			}

			const testFileName = "testfile.txt"
			// "app-with-empty-pvc" is the base k8sdeploy template (its PVC starts
			// empty); add_data:true below seeds a known file into it, so the PVC is
			// non-empty at transfer time. That seeded data is what we checksum on
			// source and verify on target.
			appName := "app-with-empty-pvc"
			namespace := "indirect-rclone-file-k8s"

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
			DeferCleanup(rbacCleanup)

			paths, err := NewScenarioPaths("crane-indirect-rclone-file-*")
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

			// crane derives the temp Secret name from the source PVC name:
			//   crane-rclone-config-<getValidatedResourceName(sourcePVC)>
			// For a PVC name shorter than 63 chars, getValidatedResourceName is a
			// no-op, so the name is exactly crane-rclone-config-<pvcName>. crane
			// creates this Secret on BOTH the source and destination namespaces from
			// the --rclone-config-file contents, and deletes both when the command
			// returns (via deferred cleanup, on success or failure).
			tmpSecret := "crane-rclone-config-" + pvcName

			By("Precondition: crane's temp rclone Secret does not exist on either cluster")
			for _, k := range []KubectlRunner{kubectlSrc, kubectlTgt} {
				out, err := k.Run("get", "secret", tmpSecret, "-n", namespace, "--ignore-not-found=true", "-o", "name")
				Expect(err).NotTo(HaveOccurred())
				Expect(strings.TrimSpace(out)).To(BeEmpty(),
					"temp Secret %q should not exist before transfer on context %q", tmpSecret, k.Context)
			}

			By("Run crane transfer-pvc in indirect mode using --rclone-config-file")
			runner := scenario.CraneNonAdmin
			runner.WorkDir = paths.TempDir
			transferOpts := TransferPVCOptions{
				SourceContext:    srcApp.Context,
				TargetContext:    tgtApp.Context,
				PVCName:          pvcName,
				PVCNamespaceMap:  fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
				CloudStorage:     config.CloudStorage,
				RcloneConfigFile: config.RcloneConfigFile,
			}
			Expect(runner.TransferPVC(transferOpts)).NotTo(HaveOccurred(),
				"indirect transfer-pvc should succeed with a valid --rclone-config-file")

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
			const verifierPod = "indirect-rclone-file-verifier"
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

			By("Verify crane cleaned up its temporary rclone Secret on both clusters")
			// A Secret built from --rclone-config-file is crane-owned and must be
			// deleted after the transfer completes (no leftover credential Secret).
			for _, k := range []KubectlRunner{kubectlSrc, kubectlTgt} {
				out, err := k.Run("get", "secret", tmpSecret, "-n", namespace, "--ignore-not-found=true", "-o", "name")
				Expect(err).NotTo(HaveOccurred())
				Expect(strings.TrimSpace(out)).To(BeEmpty(),
					"crane's temp Secret %q should be deleted after transfer on context %q", tmpSecret, k.Context)
			}
		})
})
