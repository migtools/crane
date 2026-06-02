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

var _ = Describe("Instructions-file force reconcile migration", func() {
	It("[MTA-830] should reconcile pre-existing transform stages with --force and complete end-to-end migration", Label("tier1"), func() {

		appName := "simple-nginx-nopv"
		namespace := "simple-nginx-force-reconcile"
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
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
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

		By("Create transform dir with orphan stage and existing stage present in instructions-file")
		err = os.MkdirAll(paths.TransformDir, 0o755)
		Expect(err).NotTo(HaveOccurred())
		//Create orphan stage directory for extra stage that should be deleted with --force
		orphanStagePath := filepath.Join(paths.TransformDir, "99_OrphanStage")
		err = os.MkdirAll(orphanStagePath, 0o755)
		Expect(err).NotTo(HaveOccurred())
		// Create mirrored orphan work directory that should also be deleted with --force
		workOrphanPath := filepath.Join(paths.TransformDir, ".work", "99_OrphanStage")
		err = os.MkdirAll(workOrphanPath, 0o755)
		Expect(err).NotTo(HaveOccurred())
		//Create path for CustomStage that should be overwritten with --force
		customStagePath := filepath.Join(paths.TransformDir, "20_CustomStage")
		err = os.MkdirAll(customStagePath, 0o755)
		Expect(err).NotTo(HaveOccurred())
		customStageExistingFilePath := filepath.Join(customStagePath, "preexisting.txt")
		err = os.WriteFile(customStageExistingFilePath, []byte("preexisting custom stage content"), 0o644)
		Expect(err).NotTo(HaveOccurred())

		By("Assert orphan stage dir exists before running transform")
		orphanDirInfo, err := os.Stat(orphanStagePath)
		Expect(err).NotTo(HaveOccurred())
		Expect(orphanDirInfo.IsDir()).To(BeTrue())

		By("Assert orphan stage work dir exists before running transform")
		workOrphanDirInfo, err := os.Stat(workOrphanPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(workOrphanDirInfo.IsDir()).To(BeTrue())

		By("Assert 20_CustomStage exists before running transform")
		customStageDirInfo, err := os.Stat(customStagePath)
		Expect(err).NotTo(HaveOccurred())
		Expect(customStageDirInfo.IsDir()).To(BeTrue())

		By("Assert 20_CustomStage has preexisting.txt file")
		customStageFileInfo, err := os.Stat(customStageExistingFilePath)
		Expect(err).NotTo(HaveOccurred())
		Expect(customStageFileInfo.IsDir()).To(BeFalse())

		log.Printf("Running crane transform --instructions-file for namespace %s\n", srcApp.Namespace)
		instructionsFile, err := utils.TestdataFilePath("basic-instructions-file.yaml")
		Expect(err).NotTo(HaveOccurred())
		Expect(runner.TransformWithInstructionsFile(paths.ExportDir, paths.TransformDir, instructionsFile, true)).NotTo(HaveOccurred())

		By("Assert post transform orphan stage is removed and existing CustomStage is overwritten ")
		By("Assert orphan stage dir is removed by --force")
		_, err = os.Stat(orphanStagePath)
		Expect(os.IsNotExist(err)).To(BeTrue(), "expected orphan stage to be removed by --force")

		By("Assert orphan stage work dir is removed by --force")
		_, err = os.Stat(workOrphanPath)
		Expect(os.IsNotExist(err)).To(BeTrue(), "expected orphan stage work dir to be removed by --force")

		By("Assert preexisting custom stage file is removed by --force")
		_, err = os.Stat(customStageExistingFilePath)
		Expect(os.IsNotExist(err)).To(BeTrue(), "expected preexisting custom stage file to be removed by --force")

		By("Assert instructions-file stages are present as stage-directories in transform dir")
		stageDirectories := []string{"10_KubernetesPlugin", "20_CustomStage"}
		for _, stageDir := range stageDirectories {
			dirPath := filepath.Join(paths.TransformDir, stageDir)
			dirInfo, err := os.Stat(dirPath)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("expected stage dir %q at %q to be present", stageDir, dirPath))
			Expect(dirInfo.IsDir()).To(BeTrue())
		}

		log.Printf("Running crane apply for namespace %s\n", srcApp.Namespace)
		Expect(runner.Apply(paths.ExportDir, paths.TransformDir, paths.OutputDir)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Apply rendered manifests to target")
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app on target")
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())

		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
	})
})
