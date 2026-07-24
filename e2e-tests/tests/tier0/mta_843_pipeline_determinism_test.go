package e2e

import (
	"log"
	"os"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Pipeline determinism", func() {
	It("[MTA-843] crane pipeline produces identical output on consecutive runs", Label("tier0"), func() {
		appName := "simple-nginx-nopv"
		namespace := "crane-determinism-test"
		serviceName := "my-" + appName
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.SourceContext,
		)
		srcApp := scenario.SrcAppNonAdmin

		paths1, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts1 := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths1.ExportDir}
		transformOpts1 := TransformOptions{ExportDir: paths1.ExportDir, TransformDir: paths1.TransformDir}
		applyOpts1 := ApplyOptions{TransformDir: paths1.TransformDir, OutputDir: paths1.OutputDir}

		paths2, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts2 := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths2.ExportDir}
		transformOpts2 := TransformOptions{ExportDir: paths2.ExportDir, TransformDir: paths2.TransformDir}
		applyOpts2 := ApplyOptions{TransformDir: paths2.TransformDir, OutputDir: paths2.OutputDir}

		srcApp.ExtraVars = map[string]any{"non_admin_user": "true"}

		By("Grant ns admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, cleanup, err := SetupActiveNamespaceAdmin(scenario.KubectlSrc, config.SourceNonAdminContext, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup temporary directories")
			if err := os.RemoveAll(paths1.TempDir); err != nil {
				log.Printf("cleanup: %v", err)
			}
			if err := os.RemoveAll(paths2.TempDir); err != nil {
				log.Printf("cleanup: %v", err)
			}
			By("Cleanup app resources")
			if err := srcApp.Cleanup(); err != nil {
				log.Printf("cleanup: %v", err)
			}
			By("Delete test namespace")
			if _, err := scenario.KubectlSrc.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=60s"); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})
		DeferCleanup(cleanup)
		isOpenShift := kubectlSrcNonAdmin.IsOpenShift()

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		runner1 := scenario.CraneNonAdmin
		runner1.WorkDir = paths1.TempDir

		runner2 := scenario.CraneNonAdmin
		runner2.WorkDir = paths2.TempDir

		By("Wait for source quiesce to stabilize before export")
		WaitForSourceQuiesce(kubectlSrcNonAdmin, namespace, "app="+appName, serviceName)

		By("Run pipeline - first run")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner1, exportOpts1, transformOpts1, applyOpts1)).NotTo(HaveOccurred())

		By("Run pipeline - second run")
		Expect(RunCranePipelineWithChecks(runner2, exportOpts2, transformOpts2, applyOpts2)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Compare export dirs between runs")
		compareExport := utils.CompareDirectoryYAMLSemanticsExport
		if isOpenShift {
			compareExport = utils.CompareDirectoryYAMLSemanticsExportAllowOptionalOCPOutputDefaults
		}
		Expect(compareExport(paths1.ExportDir, paths2.ExportDir)).NotTo(HaveOccurred())

		By("Compare transform dirs between runs")
		Expect(utils.CompareDirectoryYAMLSemanticsUnordered(paths1.TransformDir, paths2.TransformDir)).NotTo(HaveOccurred())

		By("Compare output dirs between runs")
		Expect(utils.CompareDirectoryYAMLSemantics(paths1.OutputDir, paths2.OutputDir)).NotTo(HaveOccurred())

	})
})
