package e2e

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	cranevalidate "github.com/konveyor/crane/internal/validate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Crane validate with duplicate GVK resources in live mode", func() {
	It("[MTA-850] Should deduplicate resources with same GVK and show only 1 resource scanned with exit code 0",
		Label("tier1", "validate"), func() {
		By("Create temporary directory for test")
		tempDir, err := os.MkdirTemp("", "crane-validate-duplicate-gvk-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup temporary directory")
			if err := os.RemoveAll(tempDir); err != nil {
				log.Printf("cleanup: failed to remove temp dir %q: %v", tempDir, err)
			}
		})

		By("Create input directory structure with duplicate GVK files")
		inputDir := filepath.Join(tempDir, "input")
		Expect(os.MkdirAll(inputDir, 0o755)).NotTo(HaveOccurred())

		By("Copy static manifest files from testdata to input directory")
		manifestFiles := []struct {
			testdataFile string
			outputFile   string
		}{
			{"test-850-duplicate-deployment-1.yaml", "deployment-1.yaml"},
			{"test-850-duplicate-deployment-2.yaml", "deployment-2.yaml"},
		}

		for _, mf := range manifestFiles {
			sourcePath, err := filepath.Abs(filepath.Join("../../testdata", mf.testdataFile))
			Expect(err).NotTo(HaveOccurred())
			Expect(sourcePath).To(BeAnExistingFile(), "%s should exist in testdata", mf.testdataFile)

			manifestData, err := os.ReadFile(sourcePath)
			Expect(err).NotTo(HaveOccurred())

			destPath := filepath.Join(inputDir, mf.outputFile)
			Expect(os.WriteFile(destPath, manifestData, 0o644)).NotTo(HaveOccurred())
		}

		log.Printf("Created 2 files with same GVK (apps/v1 Deployment) in %s", inputDir)

		By("Setup scenario for crane runner")
		scenario := NewMigrationScenario(
			"duplicate-gvk-validate",
			"test-namespace",
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)


		Expect(scenario.KubectlTgtNonAdmin.Context).NotTo(BeEmpty(), "target-nonadmin-context is required for this test")

		runner := scenario.CraneNonAdmin
		runner.WorkDir = tempDir

		By("Run crane validate in live mode with duplicate GVK files")
		validateDir := filepath.Join(tempDir, "validate")
		stdout, err := runner.Validate(ValidateOptions{
			Context:      scenario.KubectlTgtNonAdmin.Context,
			InputDir:     inputDir,
			ValidateDir:  validateDir,
			OutputFormat: "json",
		})

		By("Verify crane validate succeeds with exit code 0")
		Expect(err).NotTo(HaveOccurred(), "crane validate should succeed (exit 0) for compatible duplicate GVK resources")
		log.Printf("Validate stdout: %s", stdout)

		By("Parse validation report")
		reportPath := filepath.Join(validateDir, "report.json")
		Expect(reportPath).To(BeAnExistingFile(), "expected report.json at %s", reportPath)

		reportData, err := os.ReadFile(reportPath)
		Expect(err).NotTo(HaveOccurred())

		var report cranevalidate.ValidationReport
		err = json.Unmarshal(reportData, &report)
		Expect(err).NotTo(HaveOccurred(), "failed to parse report.json")

		By("Verify report shows live mode")
		Expect(report.Mode).To(Equal("live"), "expected validation mode to be 'live'")
		log.Printf("Validation mode: %s", report.Mode)

		By("Verify cluster context is set")
		Expect(report.ClusterContext).To(Equal(scenario.KubectlTgtNonAdmin.Context),
			"expected clusterContext to match the target non-admin context")
		log.Printf("Cluster context: %s", report.ClusterContext)

		By("Verify deduplication: only 1 resource scanned despite 2 files with same GVK")
		Expect(report.TotalScanned).To(Equal(1), "expected exactly 1 resource scanned (deduplicated from 2 files)")
		log.Printf("Total scanned: %d (deduplication successful)", report.TotalScanned)

		By("Verify the single resource is compatible")
		Expect(report.Compatible).To(Equal(1), "expected 1 compatible resource")
		Expect(report.Incompatible).To(Equal(0), "expected 0 incompatible resources")
		log.Printf("Compatible: %d, Incompatible: %d", report.Compatible, report.Incompatible)

		By("Verify Deployment appears exactly once in results")
		deploymentCount := 0
		for _, result := range report.Results {
			if result.Kind == "Deployment" && result.APIVersion == "apps/v1" {
				deploymentCount++
				Expect(result.Status).To(Equal(cranevalidate.StatusOK),
					"expected Deployment to be compatible")
				Expect(result.Namespace).To(Equal("test-namespace"),
					"expected Deployment namespace to be test-namespace")
				log.Printf("✓ Found Deployment: apiVersion=%s, kind=%s, namespace=%s, status=%s",
					result.APIVersion, result.Kind, result.Namespace, result.Status)
			}
		}
		Expect(deploymentCount).To(Equal(1), "expected exactly 1 Deployment in results (deduplicated)")

		By("Verify no failures directory was created for all compatible resources")
		failuresDir := filepath.Join(validateDir, "failures")
		_, err = os.Stat(failuresDir)
		Expect(os.IsNotExist(err)).To(BeTrue(), "expected no failures/ directory for all compatible resources")
		log.Printf("No failures directory created (as expected)")

		log.Printf("✅ MTA-850: Successfully validated deduplication of duplicate GVK resources")
		log.Printf("   - Input: 2 files with same GVK (apps/v1 Deployment)")
		log.Printf("   - Output: 1 resource scanned (deduplicated)")
		log.Printf("   - Exit code: 0 (success)")
	})
})
