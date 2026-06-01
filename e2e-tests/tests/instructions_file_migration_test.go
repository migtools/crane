package e2e

import (
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Instructions-file migration", func() {
	It("should migrate a simple nginx app using transform --instructions-file", Label("tier0"), func() {
		appName := "simple-nginx-nopv"
		namespace := "simple-nginx-instructionsfile"
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)

		if scenario.KubectlSrcNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin instructions file migration test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin instructions file migration test")
		}

		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin
		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = srcApp.ExtraVars

		By("Grant ns admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, _, cleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Delete test namespace on source and target (wait for completion)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(cleanup)
		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-pipeline-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)

			}
		})
		runner.WorkDir = paths.TempDir
		By("Run crane export/transform/apply pipeline with instructions file")
		log.Printf("Running crane export for namespace %s\n", srcApp.Namespace)
		Expect(runner.Export(srcApp.Namespace, paths.ExportDir)).NotTo(HaveOccurred())
		log.Printf("Running crane transform --instructions-file for namespace %s\n", srcApp.Namespace)
		instructionsFile, err := utils.TestdataFilePath("basic-instructions-file.yaml")
		Expect(err).NotTo(HaveOccurred())
		Expect(runner.TransformWithInstructionsFile(paths.ExportDir, paths.TransformDir, instructionsFile, false)).NotTo(HaveOccurred())
		By("Assert instructions-file stages are present as stage-directories in transform dir")
		stageDirectories := []string{"10_KubernetesPlugin", "20_CustomStage"}
		for _, stageDir := range stageDirectories {
			dirPath := filepath.Join(paths.TransformDir, stageDir)
			_, err = os.Stat(dirPath)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("expected stage dir %q at %q to be present", stageDir, dirPath))
		}
		log.Printf("Running crane apply for namespace %s\n", srcApp.Namespace)
		Expect(runner.Apply(paths.ExportDir, paths.TransformDir, paths.OutputDir)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Apply rendered manifests to target")
		Expect(ApplyOutputToTargetNonAdmin(scenario.KubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app on target")
		Expect(scenario.KubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())

		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
	})
})
