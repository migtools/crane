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
		srcApp := scenario.SrcApp
		kubectlSrc := scenario.KubectlSrc
	    isOpenShift := kubectlSrc.IsOpenShift()

		paths1, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts1 := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths1.ExportDir}
		transformOpts1 := TransformOptions{ExportDir: paths1.ExportDir, TransformDir: paths1.TransformDir}
		applyOpts1 := ApplyOptions{ExportDir: paths1.ExportDir, TransformDir: paths1.TransformDir, OutputDir: paths1.OutputDir}
		
		paths2, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts2 := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths2.ExportDir}
		transformOpts2 := TransformOptions{ExportDir: paths2.ExportDir, TransformDir: paths2.TransformDir}
		applyOpts2 := ApplyOptions{ExportDir: paths2.ExportDir, TransformDir: paths2.TransformDir, OutputDir: paths2.OutputDir}
		
		DeferCleanup(func() {
			By("Cleanup temporary directories")
			os.RemoveAll(paths1.TempDir)
			os.RemoveAll(paths2.TempDir)
			By("Cleanup app resources")
			if err := srcApp.Cleanup(); err != nil {
				log.Printf("cleanup: %v", err)
			}
			By("Delete test namespace")
			if _, err := kubectlSrc.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=60s"); err != nil {
				log.Printf("cleanup: %v", err)
			 } 
		})

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)
		
		By("Run crane export/transform/apply pipeline")
		runner1 := scenario.Crane
		runner1.WorkDir = paths1.TempDir

		runner2 := scenario.Crane
		runner2.WorkDir = paths2.TempDir
	    
		By("Wait for source quiesce to stabilize before export")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		By("Run pipeline - first run")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner1, exportOpts1, transformOpts1, applyOpts1)).NotTo(HaveOccurred())
		
		By("Run pipeline - second run")
		Expect(RunCranePipelineWithChecks(runner2, exportOpts2, transformOpts2, applyOpts2)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Compare export dirs between runs")
		compareExport := utils.CompareDirectoryYAMLSemanticsExport
        if isOpenShift {
            compareExport =
            utils.CompareDirectoryYAMLSemanticsExportAllowOptionalOCPOutputDefaults
        }
		Expect(compareExport(paths1.ExportDir, paths2.ExportDir)).NotTo(HaveOccurred())
		
		By("Compare transform dirs between runs")
		Expect(utils.CompareDirectoryYAMLSemanticsUnordered(paths1.TransformDir, paths2.TransformDir)).NotTo(HaveOccurred())
		
		By("Compare output dirs between runs")
		Expect(utils.CompareDirectoryYAMLSemantics(paths1.OutputDir, paths2.OutputDir)).NotTo(HaveOccurred())

	})})