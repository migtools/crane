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

// preRcloneImage is a genuine pre-rclone rsync-transfer tag: it ships rsync but
// NOT rclone. Indirect transfer runs the mover pod with Command ["rclone",
// "sync", ...] (crane-lib state_transfer/transfer/indirect), so this image
// cannot exec the entrypoint and the upload pod terminates with
//
//	StartError: exec: "rclone": executable file not found in $PATH

const preRcloneImage = "quay.io/konveyor/rsync-transfer:release-1.7.0"

var _ = Describe("Indirect transfer with a pre-rclone source image", func() {
	It("[MTA-921] Should fail clearly when --source-image does not contain an rclone binary",
		Label("tier2", "pvc-transfer", "indirect"), func() {

			// This test drives the real indirect (cloud storage) path far enough
			// to launch the upload mover pod, so it needs a reachable bucket and a
			// valid rclone.conf to build the config Secret from.
			if config.CloudStorage == "" || config.RcloneConfigFile == "" {
				Skip("indirect transfer not configured: requires --cloud-storage and --rclone-config-file")
			}

			const rcloneSecret = "crane-rclone-config-e2e"
			appName := "app-with-empty-pvc"
			namespace := "indirect-pre-rclone-image"

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
			}

			By("Grant namespace-admin permissions to the non-admin user on source and target")
			kubectlSrc, kubectlTgt, rbacCleanup, err := SetupActiveKubectlRunners(scenario, namespace)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(rbacCleanup)

			DeferCleanup(func() {
				By("Delete test namespace on source and target (wait for completion)")
				// Namespace deletion also reaps the orphaned destination PVC that
				// crane leaves behind on this failure path (see KNOWN GAP below).
				for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
					if _, err := k.Run("delete", "namespace", namespace,
						"--ignore-not-found=true", "--wait=true"); err != nil {
						log.Printf("cleanup: failed to delete namespace %q on context %q: %v",
							namespace, k.Context, err)
					}
				}
			})

			By("Deploy the source app with a PVC")
			log.Printf("Deploying %s in namespace %s on source cluster", appName, namespace)
			Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

			By("List the source PVC(s)")
			pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvcs).NotTo(BeEmpty(), "expected at least one PVC in source namespace %q", srcApp.Namespace)
			pvcName := pvcs[0].Name
			log.Printf("Found source PVC %q in namespace %q", pvcName, srcApp.Namespace)

			// crane's indirect transfer reads a Secret of the SAME name from both
			// the source and destination namespaces. Pre-creating a valid one lets
			// the run pass secret validation and reach the upload pod, so the ONLY
			// thing that fails is the rclone-less image under test.
			By("Pre-create a valid rclone config Secret on both source and target namespaces")
			for _, k := range []KubectlRunner{kubectlSrc, kubectlTgt} {
				if _, err := k.Run("delete", "secret", rcloneSecret, "-n", namespace, "--ignore-not-found=true"); err != nil {
					log.Printf("pre-clean of Secret %q on context %q: %v", rcloneSecret, k.Context, err)
				}
				_, err := k.Run("create", "secret", "generic", rcloneSecret,
					"-n", namespace,
					"--from-file=rclone.conf="+config.RcloneConfigFile)
				Expect(err).NotTo(HaveOccurred(),
					"failed to create rclone config Secret %q in namespace %q on context %q", rcloneSecret, namespace, k.Context)
			}

			// Free the RWO PVC so the upload mover pod can mount it and fail on the
			// missing rclone binary, rather than getting stuck ContainerCreating on
			// a volume still held by the app pod (which would time out for the wrong
			// reason).
			By("Scale the source app down so it releases the RWO PVC")
			Expect(kubectlSrc.ScaleDeploymentIfPresent(srcApp.Namespace, appName, 0)).NotTo(HaveOccurred())
			Eventually(func() (string, error) {
				out, err := kubectlSrc.Run("get", "pods", "-n", namespace, "-l", "app="+appName, "-o", "name")
				return strings.TrimSpace(out), err
			}, "90s", "3s").Should(BeEmpty(), "source app pod should terminate and release the PVC")

			By("Attempt indirect transfer-pvc with a pre-rclone --source-image")
			runner := scenario.CraneNonAdmin
			opts := TransferPVCOptions{
				SourceContext:      srcApp.Context,
				TargetContext:      tgtApp.Context,
				PVCName:            pvcName,
				PVCNamespaceMap:    fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
				CloudStorage:       config.CloudStorage,
				RcloneConfigSecret: rcloneSecret,
				// Indirect mode uses --source-image for BOTH the upload and download
				// mover pods; --destination-image is ignored on this path, so setting
				// the rclone-less image here is what exercises the failure.
				RsyncImage: preRcloneImage,
			}

			out, transferErr := runner.TransferPVCWithOutput(opts)

			By("Verify transfer-pvc failed (non-zero exit, no hang, no false success)")
			Expect(transferErr).To(HaveOccurred(),
				"transfer-pvc must fail when --source-image has no rclone binary")
			errMsg := transferErr.Error()
			log.Printf("transfer-pvc returned expected error: %s", errMsg)

			Expect(errMsg).To(ContainSubstring("crane transfer-pvc failed"),
				"error should originate from crane transfer-pvc")

			By("Verify the run reached the upload phase and the error names the failing step")
			Expect(out).To(ContainSubstring("[3/6] Uploading data to cloud storage"),
				"run should progress through setup to the upload phase before failing on the image")
			Expect(out).To(ContainSubstring("upload pod failed"),
				"the failure should be attributed to the upload mover pod, not an earlier step")
			Expect(out).To(MatchRegexp("upload pod failed.*(?:StartError|rclone.*executable)"),
				"the failure should include the StartError or missing rclone executable detail")

			By("Verify the transfer did not falsely report success")
			Expect(out).NotTo(ContainSubstring("PVC data copy: succeeded"),
				"a run with an rclone-less image must not print the success summary")

			By("Verify no orphaned indirect-transfer pods remain on source or target")
			for _, k := range []KubectlRunner{kubectlSrc, kubectlTgt} {
				podOut, err := k.Run("get", "pods", "-n", namespace,
					"-l", "app.kubernetes.io/component=indirect-transfer", "-o", "name")
				Expect(err).NotTo(HaveOccurred(), "failed to query indirect-transfer pods on context %q", k.Context)
				Expect(strings.TrimSpace(podOut)).To(BeEmpty(),
					"expected no orphaned indirect-transfer pods on context %q, got: %s", k.Context, strings.TrimSpace(podOut))
			}

			By("Verify the temporary indirect-transfer Secrets were cleaned up")
			// The pre-created rcloneSecret is caller-owned and untouched; crane's own
			// indirect-transfer Secrets (if any) must not be left behind.
			for _, k := range []KubectlRunner{kubectlSrc, kubectlTgt} {
				secretOut, err := k.Run("get", "secrets", "-n", namespace,
					"-l", "app.kubernetes.io/component=indirect-transfer", "-o", "name")
				Expect(err).NotTo(HaveOccurred(), "failed to query indirect-transfer Secrets on context %q", k.Context)
				Expect(strings.TrimSpace(secretOut)).To(BeEmpty(),
					"expected no orphaned indirect-transfer Secrets on context %q, got: %s", k.Context, strings.TrimSpace(secretOut))
			}
			log.Printf("No orphaned indirect-transfer pods or Secrets remain")
		})
})
