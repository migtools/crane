package e2e

import (
	"fmt"
	"log"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This spec is the negative control for the --keep-cloud-data flag. MTA-914
// proves that with --keep-cloud-data the staged objects remain in the bucket;
// on its own that assertion only has meaning if we also know crane deletes
// those objects when the flag is NOT set. This test exercises the default
// (flag omitted) path and asserts the bucket prefix is emptied after a
// successful transfer.
//
// crane cleans up by running `rclone sync /tmp/empty <cloud-storage>/<src-ns>/<src-pvc>`
// which deletes every object under that prefix (see crane-lib
// state_transfer/transfer/indirect/cleanup.go). So the post-condition we assert
// is that the prefix contains no objects.
var _ = Describe("Indirect transfer cloud cleanup on default (no --keep-cloud-data)", func() {
	It("[MTA-922] Should delete the staged objects from cloud storage after a successful indirect transfer",
		Label("tier1", "pvc-transfer", "indirect"), func() {

			if config.CloudStorage == "" || config.RcloneConfigFile == "" {
				Skip("indirect transfer not configured: requires --cloud-storage and --rclone-config-file")
			}

			namespace := "indirect-default-cleanup-k8s"
			app := deployIndirectApp(namespace, "crane-indirect-default-cleanup-*")

			// crane stages objects under "<cloud-storage>/<src-namespace>/<src-pvc>"
			// and, by default, empties that prefix once the transfer succeeds.
			remotePath := fmt.Sprintf("%s/%s/%s", config.CloudStorage, app.srcApp.Namespace, app.pvcName)

			By("Run crane transfer-pvc in indirect mode WITHOUT --keep-cloud-data (default cleanup)")
			runner := app.scenario.CraneNonAdmin
			runner.WorkDir = app.workDir
			targetStorageClass, err := DefaultStorageClassName(app.scenario.KubectlTgt.Context)
			Expect(err).NotTo(HaveOccurred())
			Expect(runner.TransferPVC(TransferPVCOptions{
				SourceContext:    app.srcApp.Context,
				TargetContext:    app.tgtApp.Context,
				PVCName:          app.pvcName,
				PVCNamespaceMap:  fmt.Sprintf("%s:%s", app.srcApp.Namespace, app.tgtApp.Namespace),
				DestStorageClass: targetStorageClass,
				CloudStorage:     config.CloudStorage,
				RcloneConfigFile: config.RcloneConfigFile,
				// KeepCloudData deliberately left false: this is the default path
				// whose cleanup behavior we are verifying.
			})).NotTo(HaveOccurred(), "indirect transfer-pvc should succeed with a valid --rclone-config-file")

			By("Wait for the ephemeral indirect-transfer pods (upload/download/cleanup) to be removed")
			Eventually(func() (string, error) {
				return app.kubectlSrc.Run("get", "pods", "-n", app.srcApp.Namespace, "-o", "name")
			}, "120s", "3s").ShouldNot(ContainSubstring("rclone"))
			Eventually(func() (string, error) {
				return app.kubectlTgt.Run("get", "pods", "-n", app.tgtApp.Namespace, "-o", "name")
			}, "120s", "3s").ShouldNot(ContainSubstring("rclone"))

			// Prove the transfer actually moved data. Without this, an empty bucket
			// could simply mean nothing was ever uploaded, which would make the
			// cleanup assertion below meaningless.
			By("Verify the destination PVC exists and contains the migrated data")
			// VerifyPVCSchedulable + VerifyPVCHasData both bind the named PVC on the
			// target, so they prove the PVC exists there and that the round-trip
			// actually delivered data.
			Expect(VerifyPVCSchedulable(app.scenario.KubectlTgt.Context, app.tgtApp.Namespace, app.pvcName)).NotTo(HaveOccurred())
			Expect(VerifyPVCHasData(app.kubectlTgt, app.tgtApp.Namespace, app.pvcName, "/data")).NotTo(HaveOccurred())

			By("Inspect the bucket prefix via a plain rclone pod and verify crane emptied it")
			const inspectSecret = "rclone-cleanup-inspect-conf"
			const inspectPod = "inspect-s3-bucket-after-cleanup"
			if _, err := app.kubectlTgt.Run("delete", "secret", inspectSecret, "-n", namespace, "--ignore-not-found=true"); err != nil {
				log.Printf("pre-clean of inspect secret: %v", err)
			}
			_, err = app.kubectlTgt.Run("create", "secret", "generic", inspectSecret,
				"-n", namespace, "--from-file=rclone.conf="+config.RcloneConfigFile)
			Expect(err).NotTo(HaveOccurred(), "failed to create rclone inspect Secret")

			// Vanilla k8s needs an explicit non-root runAsUser for the root-based
			// rclone image; OpenShift's restricted SCC rejects out-of-range UIDs, so
			// omit it there and let the SCC assign one.
			inspectRunAsUser := ""
			if !app.kubectlTgt.IsOpenShift() {
				inspectRunAsUser = "      runAsUser: 1000\n"
			}
			inspectPodYAML := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  containers:
  - name: rclone
    image: %s
    command: ["/bin/sh", "-c", "sleep 600"]
    env:
    - name: HOME
      value: /tmp
    volumeMounts:
    - name: cfg
      mountPath: /cfg
      readOnly: true
    - name: tmp
      mountPath: /tmp
    securityContext:
      runAsNonRoot: true
%s      allowPrivilegeEscalation: false
      capabilities:
        drop:
        - ALL
      seccompProfile:
        type: RuntimeDefault
  volumes:
  - name: cfg
    secret:
      secretName: %s
  - name: tmp
    emptyDir: {}
`, inspectPod, namespace, rcloneInspectImage, inspectRunAsUser, inspectSecret)
			Expect(app.kubectlTgt.ApplyYAMLSpec(inspectPodYAML, namespace)).NotTo(HaveOccurred())
			DeferCleanup(func() {
				if _, err := app.kubectlTgt.Run("delete", "pod", inspectPod, "-n", namespace, "--ignore-not-found", "--wait=true"); err != nil {
					log.Printf("cleanup inspect pod %q: %v", inspectPod, err)
				}
				if _, err := app.kubectlTgt.Run("delete", "secret", inspectSecret, "-n", namespace, "--ignore-not-found=true"); err != nil {
					log.Printf("cleanup inspect secret %q: %v", inspectSecret, err)
				}
			})
			_, err = app.kubectlTgt.Run("wait", "--for=condition=Ready", "pod/"+inspectPod, "-n", namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())

			// Best-effort purge of remotePath so a failed run (crane did NOT clean up)
			// leaves nothing behind for the next run. Registered after the pod-delete
			// cleanup so it executes first.
			DeferCleanup(func() {
				purge := fmt.Sprintf("export HOME=/tmp; rclone --config /cfg/rclone.conf purge %q", remotePath)
				if _, err := app.kubectlTgt.Run("exec", inspectPod, "-n", namespace, "--", "/bin/sh", "-c", purge); err != nil {
					log.Printf("cleanup: failed to purge cloud objects at %q: %v", remotePath, err)
				}
			})

			// `rclone lsf -R` lists every object under the prefix; an emptied or
			// non-existent S3 prefix prints nothing and exits 0. set -e makes a genuine
			// listing failure abort the script (caught by the err assertion below)
			// instead of being masked by the trailing echo's exit 0 and looking empty.
			listScript := fmt.Sprintf(
				"set -e; export HOME=/tmp; echo '===OBJECTS_START==='; "+
					"rclone --config /cfg/rclone.conf lsf -R %q; echo '===OBJECTS_END==='",
				remotePath)
			listOut, err := app.kubectlTgt.Run("exec", inspectPod, "-n", namespace, "--", "/bin/sh", "-c", listScript)
			Expect(err).NotTo(HaveOccurred(), "failed to list raw bucket objects")
			out := StripKubectlWarnings(listOut)
			log.Printf("Bucket prefix listing after default transfer:\n%s", out)

			objects := sectionBetween(out, "===OBJECTS_START===", "===OBJECTS_END===")
			Expect(strings.TrimSpace(objects)).To(BeEmpty(),
				"crane should have emptied the staged objects at %q after a successful transfer without --keep-cloud-data", remotePath)
		})
})
