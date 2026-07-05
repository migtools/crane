package e2e

import (
      
      "log"
     

      "github.com/konveyor/crane/e2e-tests/config"
      . "github.com/konveyor/crane/e2e-tests/framework"
      . "github.com/onsi/ginkgo/v2"
      . "github.com/onsi/gomega"
  )
var _ = Describe("Same-cluster namespace migration", func() {
	It("[MTA-840] nginx app migrated to a different namespace on the same cluster", Label("tier0"), func() {
		appName := "simple-nginx-nopv"
		srcNamespace := "crane-same-src"
        tgtNamespace := "crane-same-tgt"
		serviceName := "my-" + appName
		scenario := NewMigrationScenario(
			appName,
			srcNamespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.SourceContext,
		)
		scenario.TgtApp.Namespace = tgtNamespace
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
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
			By("Delete source and target namespaces")
			for _, ns := range []string{srcNamespace, tgtNamespace} {
               if _, err := kubectlSrc.Run("delete", "namespace", ns, "--ignore-not-found=true", "--wait=true", "--timeout=60s"); err != nil {
				log.Printf("cleanup: %v", err)
			 } 
            }
		})
		runner := scenario.Crane
		runner.WorkDir = paths.TempDir
		By("Run crane export/transform/apply pipeline")
		By("Wait for source quiesce to stabilize before export")
		WaitForSourceQuiesce(kubectlSrc, srcNamespace, "app="+appName, serviceName)

		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Apply remapped manifests to target namespace on same cluster")
        Expect(ApplyOutputToTargetWithNamespaceRemap(kubectlTgt, srcNamespace, tgtNamespace, paths.OutputDir)).NotTo(HaveOccurred())
		
		By("Scale target deployment and validate app")
		log.Printf("Scaling target deployment(s) with label app=%s to 1\n", appName)
		Expect(kubectlTgt.ScaleDeployment(tgtNamespace, appName, 1)).NotTo(HaveOccurred())

		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)

	})
})
