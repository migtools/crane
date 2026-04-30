package e2e

import (
	"fmt"
	"log"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Stateless migration", func() {
	It("[MTC-329] nginx app quiesce pod and apply to target cluster", Label("tier0"), func() {
		appName := "simple-nginx-nopv"
		namespace := "simple-nginx-nopv"
		serviceName := "my-" + appName
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		srcApp := scenario.SrcApp
		tgtApp := scenario.TgtApp
		kubectlSrc := scenario.KubectlSrc
		kubectlTgt := scenario.KubectlTgt
		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})
		runner := scenario.Crane
		runner.WorkDir = paths.TempDir
		By("Run crane export/transform/apply pipeline")
		By("Wait for source quiesce to stabilize before export")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, srcApp.Namespace, paths)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Compare YAML semantic diff of golden and actual export files")
		goldenExportDir, err := utils.GoldenManifestsDir(appName, "export")
		Expect(err).NotTo(HaveOccurred())
		if err := utils.CompareDirectoryYAMLSemanticsExport(goldenExportDir, paths.ExportDir); err != nil {
			Fail(fmt.Sprintf("YAML semantic diff of golden and actual export files: %v", err))
		} else {
			log.Printf("YAML semantic diff of golden and actual export files: no differences found")
		}
		By("Compare YAML semantic diff of golden and actual output files")
		goldenOutputDir, err := utils.GoldenManifestsDir(appName, "output")
		Expect(err).NotTo(HaveOccurred())
		if err := utils.CompareDirectoryYAMLSemantics(goldenOutputDir, paths.OutputDir); err != nil {
			Fail(fmt.Sprintf("YAML semantic diff of golden and actual output files: %v", err))
		} else {
			log.Printf("YAML semantic diff of golden and actual output files: no differences found")
		}
		log.Printf("Yaml diff comparison completed for output files successfully")
		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app")
		log.Printf("Scaling target deployment(s) with label app=%s to 1\n", appName)
		Expect(kubectlTgt.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())

		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)
	})
})
