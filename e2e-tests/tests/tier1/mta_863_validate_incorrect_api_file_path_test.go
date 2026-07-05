package e2e

import (
	"log"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Crane validate: Verify behavior with incorrect API file path", func() {
	It("[MTA-863] Should fail gracefully when API resources file path does not exist (tier1)", Label("tier1", "validate"), func() {
		appName := "validate-incorrect-api-path"
		namespace := appName
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
		runner := scenario.CraneNonAdmin
		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = srcApp.ExtraVars

		By("Grant ns admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, _, cleanup, err := SetupActiveKubectlRunners(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Delete test namespace on source and target (wait for completion)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(cleanup) // Cleanup rolebindings

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-validate-incorrect-api-*")
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
		})

		runner.WorkDir = paths.TempDir
		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Attempt to run crane validate with non-existent API resources file")
		nonExistentAPIFile := filepath.Join(paths.TempDir, "non-existent-api-surface.json")
		validateDir := filepath.Join(paths.TempDir, "validate")

		// Expect crane validate to fail when API resources file doesn't exist
		stdout, err := runner.Validate(ValidateOptions{
			InputDir:         paths.OutputDir,
			ValidateDir:      validateDir,
			APIResourcesFile: nonExistentAPIFile,
		})

		Expect(err).To(HaveOccurred(), "crane validate should fail when API resources file does not exist")
		log.Printf("Expected failure occurred: %v", err)
		log.Printf("Validate output: %s", stdout)

		By("Verify error message indicates file not found")
		errMsg := err.Error()
		Expect(errMsg).To(ContainSubstring(nonExistentAPIFile),
			"error message should mention the non-existent file path")
		Expect(errMsg).To(ContainSubstring("no such file or directory"),
			"error message should contain 'no such file or directory'")
		log.Printf("Error message correctly references missing file: %s", errMsg)

		By("Test with directory path instead of file - should fail gracefully")
		dirPathValidateDir := filepath.Join(paths.TempDir, "validate-dir-path")
		dirPath := paths.TempDir // Use existing directory as the API file path

		stdout3, err3 := runner.Validate(ValidateOptions{
			InputDir:         paths.OutputDir,
			ValidateDir:      dirPathValidateDir,
			APIResourcesFile: dirPath,
		})

		Expect(err3).To(HaveOccurred(), "crane validate should fail when API resources file is a directory")
		log.Printf("Directory path test - expected failure: %v", err3)
		log.Printf("Directory path test output: %s", stdout3)

		By("Verify error message indicates directory instead of file")
		errMsg3 := err3.Error()
		Expect(errMsg3).To(ContainSubstring(dirPath),
			"error message should mention the directory path")
		Expect(errMsg3).To(ContainSubstring("is a directory"),
			"error message should contain 'is a directory'")
		log.Printf("Error message correctly indicates directory issue: %s", errMsg3)

		By("All negative test cases verified successfully")
		log.Printf("Issue #374 test completed: crane validate correctly handles incorrect API file paths")
	})
})
