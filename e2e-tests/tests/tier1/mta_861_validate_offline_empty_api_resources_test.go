package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	cranevalidate "github.com/konveyor/crane/internal/validate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Validate offline mode with empty API resources", func() {
	It("[MTA-861] should fail early when apiResourceLists is empty in offline mode", Label("tier1", "validate"), func() {
		tempDir, err := os.MkdirTemp("", "crane-validate-offline-mta-861-empty-lists-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(os.RemoveAll(tempDir)).To(Succeed())
		})

		inputDir := filepath.Join(tempDir, "input")
		validateDir := filepath.Join(tempDir, "validate")
		Expect(os.MkdirAll(inputDir, 0o755)).NotTo(HaveOccurred())

		goldenOutputDir, err := utils.GoldenManifestsDir("simple-nginx-nopv", "output")
		Expect(err).NotTo(HaveOccurred())
		sourceManifestPath := filepath.Join(goldenOutputDir, "output.yaml")
		Expect(sourceManifestPath).To(BeAnExistingFile())

		manifestData, err := os.ReadFile(sourceManifestPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(inputDir, "output.yaml"), manifestData, 0o644)).NotTo(HaveOccurred())

		apiResourcesFile := filepath.Join(tempDir, "target-api-empty_resources-v2.json")
		Expect(os.WriteFile(apiResourcesFile, []byte(`{"apiResourceLists":[]}`), 0o644)).NotTo(HaveOccurred())

		runner := CraneRunner{
			Bin:     config.CraneBin,
			WorkDir: tempDir,
		}

		stdout, err := runner.Validate(ValidateOptions{
			InputDir:         inputDir,
			ValidateDir:      validateDir,
			OutputFormat:     "json",
			APIResourcesFile: apiResourcesFile,
		})
		Expect(err).To(HaveOccurred(), "validate should fail when apiResourceLists is empty")
		Expect(err.Error()).To(ContainSubstring("loading api-resources"))
		Expect(err.Error()).To(ContainSubstring(apiResourcesFile))
		Expect(err.Error()).To(ContainSubstring("contains no API resource lists"))
		Expect(stdout).NotTo(BeEmpty())

		reportPath := filepath.Join(validateDir, "report.json")
		_, reportErr := os.Stat(reportPath)
		Expect(os.IsNotExist(reportErr)).To(BeTrue(), "report.json should not exist for parse/load failure")

		failuresDir := filepath.Join(validateDir, "failures")
		_, failuresErr := os.Stat(failuresDir)
		Expect(os.IsNotExist(failuresErr)).To(BeTrue(), "failures directory should not exist for parse/load failure")
	})

	It("[MTA-861] should mark all scanned resources incompatible when API lists are present but resources are empty", Label("tier1", "validate"), func() {
		tempDir, err := os.MkdirTemp("", "crane-validate-offline-mta-861-empty-resources-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(os.RemoveAll(tempDir)).To(Succeed())
		})

		inputDir := filepath.Join(tempDir, "input")
		validateDir := filepath.Join(tempDir, "validate")
		Expect(os.MkdirAll(inputDir, 0o755)).NotTo(HaveOccurred())

		goldenOutputDir, err := utils.GoldenManifestsDir("simple-nginx-nopv", "output")
		Expect(err).NotTo(HaveOccurred())
		sourceManifestPath := filepath.Join(goldenOutputDir, "output.yaml")
		Expect(sourceManifestPath).To(BeAnExistingFile())

		manifestData, err := os.ReadFile(sourceManifestPath)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(inputDir, "output.yaml"), manifestData, 0o644)).NotTo(HaveOccurred())

		apiResourcesFile := filepath.Join(tempDir, "target-api-empty_resources.json")
		apiSurface := `{
  "apiResourceLists": [
    {
      "kind": "APIResourceList",
      "apiVersion": "v1",
      "groupVersion": "v1",
      "resources": []
    }
  ]
}`
		Expect(os.WriteFile(apiResourcesFile, []byte(apiSurface), 0o644)).NotTo(HaveOccurred())

		runner := CraneRunner{
			Bin:     config.CraneBin,
			WorkDir: tempDir,
		}

		stdout, err := runner.Validate(ValidateOptions{
			InputDir:         inputDir,
			ValidateDir:      validateDir,
			OutputFormat:     "json",
			APIResourcesFile: apiResourcesFile,
		})
		Expect(err).To(HaveOccurred(), "validate should fail when scanned resources are incompatible")
		Expect(err.Error()).To(ContainSubstring("validation failed"))
		Expect(stdout).To(ContainSubstring("Mode: offline"))
		Expect(stdout).To(ContainSubstring("Result: FAILED"))

		reportPath := filepath.Join(validateDir, "report.json")
		Expect(reportPath).To(BeAnExistingFile())
		reportBytes, err := os.ReadFile(reportPath)
		Expect(err).NotTo(HaveOccurred())

		var report cranevalidate.ValidationReport
		Expect(json.Unmarshal(reportBytes, &report)).To(Succeed())

		Expect(report.Mode).To(Equal("offline"))
		Expect(report.APIResourcesSource).To(Equal(apiResourcesFile))
		Expect(report.ClusterContext).To(BeEmpty(), "offline validation should not carry cluster context")
		Expect(report.TotalScanned).To(BeNumerically(">", 0))
		Expect(report.Compatible).To(Equal(0))
		Expect(report.Incompatible).To(Equal(report.TotalScanned))
		Expect(report.Compatible + report.Incompatible).To(Equal(report.TotalScanned))
		Expect(report.Results).To(HaveLen(report.TotalScanned))

		foundKinds := map[string]bool{}
		for _, result := range report.Results {
			Expect(result.Status).To(Equal(cranevalidate.StatusIncompatible))
			Expect(strings.TrimSpace(result.Reason)).NotTo(BeEmpty())
			foundKinds[result.Kind] = true
		}
		Expect(foundKinds["Service"]).To(BeTrue(), "expected Service to be present in validation results")
		Expect(foundKinds["Deployment"]).To(BeTrue(), "expected Deployment to be present in validation results")

		failuresDir := filepath.Join(validateDir, "failures")
		Expect(failuresDir).To(BeADirectory())
		failureFiles, err := filepath.Glob(filepath.Join(failuresDir, "*.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(failureFiles).To(HaveLen(report.Incompatible))
	})
})
