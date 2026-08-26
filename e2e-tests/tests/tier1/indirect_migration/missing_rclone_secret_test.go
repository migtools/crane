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

var _ = Describe("Indirect transfer with missing rclone config Secret)", func() {

	It("Should fail when the referenced rclone-config-secret does not exist",
		Label("tier1", "pvc-transfer", "indirect"), func() {

			appName := "app-with-empty-pvc"
			namespace := "indirect-missing-secret-k8s"
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

			By("Grant namespace-admin permissions to non-admin user on source and target")
			kubectlSrcNonAdmin, _, cleanup, err := SetupActiveKubectlRunners(scenario, namespace)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(cleanup)

			DeferCleanup(func() {
				By("Delete test namespace on source and target (wait for completion)")
				for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
					if _, err := k.Run("delete", "namespace", namespace,
						"--ignore-not-found=true", "--wait=true"); err != nil {
						log.Printf("cleanup: failed to delete namespace %q on context %q: %v",
							namespace, k.Context, err)
					}
				}
			})

			By("Deploy source app with a PVC")
			log.Printf("Deploying %s in namespace %s on source cluster", appName, namespace)
			Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())
			log.Printf("Source app deployed successfully")

			By("Verify source PVC exists")
			pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvcs).NotTo(BeEmpty(), "expected at least one PVC in source namespace %q", srcApp.Namespace)
			log.Printf("Found %d PVC(s) in source namespace %q", len(pvcs), srcApp.Namespace)

			By("Verify the nonexistent Secret is truly absent")
			const bogusSecret = "nonexistent-rclone-secret"
			out, _ := kubectlSrcNonAdmin.Run(
				"get", "secret", bogusSecret, "-n", namespace, "--ignore-not-found=true")
			Expect(strings.TrimSpace(out)).To(BeEmpty(),
				"precondition: Secret %q should not exist before the test", bogusSecret)

			By("Attempt indirect transfer-pvc with a nonexistent rclone config Secret")
			runner := scenario.CraneNonAdmin
			opts := TransferPVCOptions{
				SourceContext:      srcApp.Context,
				TargetContext:      tgtApp.Context,
				PVCName:            pvcs[0].Name,
				PVCNamespaceMap:    fmt.Sprintf("%s:%s", namespace, namespace),
				CloudStorage:       "remote:crane-e2e",
				RcloneConfigSecret: bogusSecret,
			}

			transferErr := runner.TransferPVC(opts)

			By("Verify transfer-pvc failed")
			Expect(transferErr).To(HaveOccurred(),
				"transfer-pvc should fail when the rclone config Secret does not exist")
			errMsg := transferErr.Error()
			log.Printf("transfer-pvc returned expected error: %s", errMsg)

			Expect(errMsg).To(ContainSubstring("crane transfer-pvc failed"),
				"error should originate from crane transfer-pvc")

			By("Verify no orphaned indirect-transfer pods remain on source")
			podOut, err := kubectlSrcNonAdmin.Run(
				"get", "pods", "-n", namespace,
				"-l", "app.kubernetes.io/component=indirect-transfer",
				"-o", "name")
			if err == nil && strings.TrimSpace(podOut) != "" {
				log.Printf("WARNING: orphaned indirect-transfer pods on source: %s",
					strings.TrimSpace(podOut))
			} else {
				log.Printf("No orphaned indirect-transfer pods on source")
			}

			By("Verify no orphaned indirect-transfer Secrets remain on source")
			secretOut, err := kubectlSrcNonAdmin.Run(
				"get", "secrets", "-n", namespace,
				"-l", "app.kubernetes.io/component=indirect-transfer",
				"-o", "name")
			if err == nil && strings.TrimSpace(secretOut) != "" {
				log.Printf("WARNING: orphaned indirect-transfer Secrets on source: %s",
					strings.TrimSpace(secretOut))
			} else {
				log.Printf("No orphaned indirect-transfer Secrets on source")
			}
		})
})
