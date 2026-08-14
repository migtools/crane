package e2e

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	cranevalidate "github.com/konveyor/crane/internal/validate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Validate scanner multi-document YAML behavior [Live Mode]", func() {
	It("[MTA-847] should scan all resources from a single multi-document YAML file", Label("tier1", "validate"), func() {
		scenario := NewMigrationScenario(
			"scanner-multi-doc-validate-live",
			"validate-multi-doc-yaml",
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)

		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required")
		}

		runner := scenario.CraneNonAdmin
		paths, err := NewScenarioPaths("crane-validate-multi-doc-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(os.RemoveAll(paths.TempDir)).To(Succeed())
		})
		runner.WorkDir = paths.TempDir

		By("Copy multi-document YAML fixture into isolated input directory")
		inputDir := filepath.Join(paths.TempDir, "input")
		Expect(os.MkdirAll(inputDir, 0o755)).NotTo(HaveOccurred())

		content, err := utils.ReadTestdataFile(filepath.Join("scanner-multi-doc", "app-multi-doc.yaml"))
		Expect(err).NotTo(HaveOccurred(), "read multi-doc fixture")
		destPath := filepath.Join(inputDir, "app-multi-doc.yaml")
		Expect(os.WriteFile(destPath, []byte(content), 0o644)).NotTo(HaveOccurred())

		By("Run crane validate in live mode against target context")
		stdout, err := runner.Validate(ValidateOptions{
			Context:      scenario.KubectlTgtNonAdmin.Context,
			InputDir:     inputDir,
			ValidateDir:  paths.ValidateDir,
			OutputFormat: "json",
		})
		Expect(err).NotTo(HaveOccurred(), "validate should pass for all compatible resources")
		Expect(stdout).To(ContainSubstring("Mode: live"))
		Expect(stdout).To(ContainSubstring("Result: PASSED"))

		By("Parse validation report")
		reportPath := filepath.Join(paths.ValidateDir, "report.json")
		Expect(reportPath).To(BeAnExistingFile())

		reportBytes, err := os.ReadFile(reportPath)
		Expect(err).NotTo(HaveOccurred())

		var report cranevalidate.ValidationReport
		Expect(json.Unmarshal(reportBytes, &report)).To(Succeed())

		By("Verify report metadata")
		Expect(report.Mode).To(Equal("live"))
		Expect(report.ClusterContext).To(Equal(scenario.KubectlTgtNonAdmin.Context))

		By("Verify all 3 resources from multi-document YAML were scanned")
		Expect(report.TotalScanned).To(Equal(3), "expected 3 resources scanned from multi-document YAML (Namespace, ConfigMap, Service)")
		Expect(report.Compatible).To(Equal(3))
		Expect(report.Incompatible).To(Equal(0))
		Expect(report.Results).To(HaveLen(3))

		By("Verify each expected resource appears in results with correct metadata")
		expectedKinds := map[string]struct {
			apiVersion string
			namespace  string
		}{
			"Namespace": {apiVersion: "v1", namespace: ""},
			"ConfigMap": {apiVersion: "v1", namespace: "test-app"},
			"Service":   {apiVersion: "v1", namespace: "test-app"},
		}

		foundKinds := map[string]bool{}
		for _, result := range report.Results {
			expected, ok := expectedKinds[result.Kind]
			if !ok {
				continue
			}
			foundKinds[result.Kind] = true
			Expect(result.APIVersion).To(Equal(expected.apiVersion),
				"expected %s to have apiVersion %s", result.Kind, expected.apiVersion)
			Expect(result.Status).To(Equal(cranevalidate.StatusOK),
				"expected %s to have status OK", result.Kind)
			Expect(result.Namespace).To(Equal(expected.namespace),
				fmt.Sprintf("unexpected namespace for %s", result.Kind))
		}

		for kind := range expectedKinds {
			Expect(foundKinds[kind]).To(BeTrue(), "expected %s in validation results", kind)
		}

		By("Verify no failures directory exists")
		failuresDir := filepath.Join(paths.ValidateDir, "failures")
		Expect(failuresDir).NotTo(BeADirectory(), "expected no failures/ directory for all compatible resources")
	})
})
