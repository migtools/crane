package e2e

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	cranevalidate "github.com/konveyor/crane/internal/validate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Crane validate: multi-document YAML scanning", func() {
	It("[MTA-847] Crane should scan all resources from a single multi-document YAML file (tier1)", Label("tier1", "validate"), func() {
		appName := "multi-resource-app"
		namespace := "multi-doc-yaml"

		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)

		if scenario.SrcAppNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin validation test")
		}
		if scenario.TgtAppNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin validation test")
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
		DeferCleanup(cleanup) // Cleanup rolebindings

		paths, err := NewScenarioPaths("crane-validate-multi-doc-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		runner.WorkDir = paths.TempDir
		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, srcApp.Namespace, paths)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Create a single multi-document YAML file with all exported resources")
		// Find all exported resource files
		resourceDir := filepath.Join(paths.OutputDir, "resources", namespace)
		resourceFiles, err := filepath.Glob(filepath.Join(resourceDir, "*.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(resourceFiles).To(HaveLen(4), "expected exactly 4 resource files (Service, Secret, Deployment, RoleBinding)")
		log.Printf("Found %d exported resource files", len(resourceFiles))

		// Combine all resources into a single multi-document YAML file
		var multiDocContent strings.Builder
		for i, resourceFile := range resourceFiles {
			content, err := os.ReadFile(resourceFile)
			Expect(err).NotTo(HaveOccurred())

			if i > 0 {
				multiDocContent.WriteString("\n---\n")
			}
			multiDocContent.Write(content)
			log.Printf("Added resource from %s", filepath.Base(resourceFile))
		}

		// Create isolated directory with only the multi-document file to ensure validate scans it
		validateInputDir := filepath.Join(paths.TempDir, "validate-input")
		Expect(os.MkdirAll(validateInputDir, 0o755)).NotTo(HaveOccurred())
		multiDocFile := filepath.Join(validateInputDir, "multi-doc-resources.yaml")
		Expect(os.WriteFile(multiDocFile, []byte(multiDocContent.String()), 0644)).NotTo(HaveOccurred())
		log.Printf("Created multi-document YAML file at %s", multiDocFile)

		By("Verify multi-document YAML file has multiple documents")
		multiDocBytes, err := os.ReadFile(multiDocFile)
		Expect(err).NotTo(HaveOccurred())
		separatorCount := strings.Count(string(multiDocBytes), "\n---\n")
		Expect(separatorCount).To(Equal(3), "expected 3 document separators (---) for 4 documents")
		log.Printf("Multi-document YAML has %d document separators", separatorCount)

		By("Run crane validate on the multi-document YAML file")
		validateDir := filepath.Join(paths.TempDir, "validate")
		stdout, err := runner.Validate(ValidateOptions{
			Context:      scenario.KubectlTgtNonAdmin.Context,
			InputDir:     validateInputDir,
			ValidateDir:  validateDir,
			OutputFormat: "json",
		})

		Expect(err).NotTo(HaveOccurred(), "validate should succeed for all compatible resources")
		log.Printf("Validate stdout: %s", stdout)

		By("Verify validation report exists")
		reportPath := filepath.Join(validateDir, "report.json")
		Expect(reportPath).To(BeAnExistingFile(), "expected report.json at %s", reportPath)

		By("Parse and verify validation report")
		reportData, err := os.ReadFile(reportPath)
		Expect(err).NotTo(HaveOccurred())

		var report cranevalidate.ValidationReport
		err = json.Unmarshal(reportData, &report)
		Expect(err).NotTo(HaveOccurred(), "failed to parse report.json")

		By("Verify report shows live mode")
		Expect(report.Mode).To(Equal("live"), "expected validation mode to be 'live'")
		log.Printf("Validation mode: %s", report.Mode)

		By("Verify all 4 resources from multi-document YAML were scanned")
		Expect(report.TotalScanned).To(Equal(4), "expected exactly 4 resources scanned from multi-document YAML")
		Expect(report.Compatible).To(Equal(4), "expected all 4 resources to be compatible")
		Expect(report.Incompatible).To(Equal(0), "expected no incompatible resources")
		Expect(report.Compatible + report.Incompatible).To(Equal(report.TotalScanned),
			"expected Compatible + Incompatible to equal TotalScanned (found %d + %d != %d)",
			report.Compatible, report.Incompatible, report.TotalScanned)
		log.Printf("Total: %d, Compatible: %d, Incompatible: %d", report.TotalScanned, report.Compatible, report.Incompatible)

		By("Verify all expected resource types were scanned")
		expectedResources := map[string]string{
			"Service":     "v1",
			"Secret":      "v1",
			"Deployment":  "apps/v1",
			"RoleBinding": "rbac.authorization.k8s.io/v1",
		}

		foundResources := make(map[string]bool)
		for _, result := range report.Results {
			log.Printf("Scanned resource: %s/%s (namespace: %s, status: %s)",
				result.APIVersion, result.Kind, result.Namespace, result.Status)

			if expectedAPIVersion, expected := expectedResources[result.Kind]; expected {
				foundResources[result.Kind] = true
				Expect(result.APIVersion).To(Equal(expectedAPIVersion),
					"expected %s to have apiVersion %s", result.Kind, expectedAPIVersion)
				Expect(result.Status).To(Equal(cranevalidate.StatusOK),
					"expected %s to have status OK", result.Kind)
				Expect(result.Namespace).To(Equal(namespace),
					"expected %s to be in namespace %s", result.Kind, namespace)
			}
		}

		By("Verify all resource types were found in the scan")
		for kind := range expectedResources {
			Expect(foundResources[kind]).To(BeTrue(), "expected to find %s in report", kind)
			log.Printf("✓ Found %s in multi-document YAML scan", kind)
		}

		By("Verify no failures directory exists (all resources compatible)")
		failuresDir := filepath.Join(validateDir, "failures")
		Expect(failuresDir).NotTo(BeADirectory(), "expected no failures/ directory for all compatible resources")
		log.Printf("No failures directory - all resources compatible")

		By("Verify stdout indicates multi-document YAML was processed")
		Expect(stdout).To(ContainSubstring("multi-doc-resources.yaml"),
			"stdout should reference the multi-document YAML file")

		log.Printf("\n"+
			"========================================\n"+
			"MULTI-DOCUMENT YAML SCANNING SUCCESS\n"+
			"========================================\n"+
			"Mode: %s\n"+
			"Input File: multi-doc-resources.yaml\n"+
			"Total Documents: 4\n"+
			"Total Scanned: %d\n"+
			"Compatible: %d (Service, Secret, Deployment, RoleBinding)\n"+
			"Incompatible: %d\n"+
			"All resources from multi-document YAML scanned successfully!\n"+
			"========================================\n",
			report.Mode,
			report.TotalScanned,
			report.Compatible,
			report.Incompatible)
	})
})
