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

var _ = Describe("Crane validate: all compatible standard resources in live mode", func() {
	It("[MTA-833] Validate final manifests after export/transform/apply pipeline (tier0)", Label("tier0", "validate"), func() {
		appName := "multi-resource-app"
		namespace := appName
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		if scenario.SrcAppNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin offline validation test")
		}
		if scenario.TgtAppNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin offline validation test")
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

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-validate-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, scenario.TgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		runner.WorkDir = paths.TempDir
		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Verify output resource manifests exist for all expected kinds")
		outputResourcesDir := filepath.Join(paths.OutputDir, "resources", namespace)
		outputFiles, err := filepath.Glob(filepath.Join(outputResourcesDir, "*.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(outputFiles).NotTo(BeEmpty(), "expected output resource YAML files in %s", outputResourcesDir)

		// Verify each expected resource kind has at least one output file
		expectedKinds := []string{"Deployment", "Service", "ConfigMap", "Secret"}
		for _, kind := range expectedKinds {
			pattern := filepath.Join(outputResourcesDir, kind+"_*.yaml")
			matches, err := filepath.Glob(pattern)
			Expect(err).NotTo(HaveOccurred())
			Expect(matches).NotTo(BeEmpty(), "expected at least one %s file in %s", kind, outputResourcesDir)
		}
		log.Printf("Found %d output resource files in %s (verified all 4 kinds present)", len(outputFiles), outputResourcesDir)

		By("Run crane validate in live mode against target cluster")
		validateDir := filepath.Join(paths.TempDir, "validate")
		stdout, err := runner.Validate(ValidateOptions{
			Context:     scenario.TgtApp.Context,
			InputDir:    paths.OutputDir,
			ValidateDir: validateDir,
		})
		Expect(err).NotTo(HaveOccurred(), "crane validate should succeed with all compatible resources")
		log.Printf("Crane validate completed with exit code 0")
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

		By("Verify report mode is 'live'")
		Expect(report.Mode).To(Equal("live"), "expected mode='live' in report")
		log.Printf("Report mode: %s ✓", report.Mode)

		By("Verify clusterContext is set")
		Expect(report.ClusterContext).NotTo(BeEmpty(), "expected clusterContext to be set in live mode")
		Expect(report.ClusterContext).To(Equal(scenario.TgtApp.Context), "expected clusterContext to match target context")
		log.Printf("Cluster context: %s ✓", report.ClusterContext)

		By("Verify all 4 resource types are scanned")
		Expect(report.TotalScanned).To(BeNumerically(">=", 4), "expected at least 4 resources scanned (Deployment, Service, ConfigMap, Secret)")
		log.Printf("Total scanned: %d ✓", report.TotalScanned)

		By("Verify all resources are compatible")
		Expect(report.Compatible).To(Equal(report.TotalScanned), "expected all resources to be compatible")
		Expect(report.Incompatible).To(Equal(0), "expected 0 incompatible resources")
		log.Printf("Compatible: %d, Incompatible: %d ✓", report.Compatible, report.Incompatible)

		By("Verify expected resource types are present in results")
		// Map of expected resource kinds to their API versions
		// These are the 4 standard Kubernetes resources deployed by multi-resource-app
		expectedResources := map[string]string{
			"Deployment": "apps/v1",
			"Service":    "v1",
			"ConfigMap":  "v1",
			"Secret":     "v1",
		}

		// Track which expected resources were actually found in the report
		foundResources := make(map[string]bool)
		for _, result := range report.Results {
			log.Printf("Found resource: %s/%s (namespace: %s, status: %s, resourcePlural: %s)",
				result.APIVersion, result.Kind, result.Namespace, result.Status, result.ResourcePlural)

			// Check if this is one of our expected resources
			if expectedAPIVersion, expected := expectedResources[result.Kind]; expected {
				foundResources[result.Kind] = true

				// Verify API version matches expected
				Expect(result.APIVersion).To(Equal(expectedAPIVersion),
					"expected %s to have apiVersion %s", result.Kind, expectedAPIVersion)

				// Verify status is OK (compatible)
				Expect(result.Status).To(Equal(cranevalidate.StatusOK),
					"expected %s to have status OK", result.Kind)

				// Verify namespace is set for namespaced resources
				Expect(result.Namespace).To(Equal(namespace),
					"expected %s to be in namespace %s", result.Kind, namespace)

				// Verify resourcePlural is set (required field)
				Expect(result.ResourcePlural).NotTo(BeEmpty(),
					"expected %s to have resourcePlural set", result.Kind)
			}
		}

		By("Verify all 4 expected resource types were found")
		var missingResources []string
		for kind := range expectedResources {
			if !foundResources[kind] {
				missingResources = append(missingResources, kind)
			}
		}
		Expect(missingResources).To(BeEmpty(),
			"expected to find all resources in validation results, missing: %v", missingResources)

		for kind := range expectedResources {
			log.Printf("✓ Found %s with correct apiVersion and status", kind)
		}

		By("Verify no failures directory was created")
		failuresDir := filepath.Join(validateDir, "failures")
		_, err = os.Stat(failuresDir)
		Expect(os.IsNotExist(err)).To(BeTrue(),
			"expected no failures/ directory for all compatible resources")
		log.Printf("No failures directory created ✓")

		By("Verify resourcePlural mappings are correct")
		// Map of expected resource kinds to their plural forms
		// This verifies crane validate correctly maps Kind to resourcePlural
		expectedPlurals := map[string]string{
			"Deployment": "deployments",
			"Service":    "services",
			"ConfigMap":  "configmaps",
			"Secret":     "secrets",
		}

		for _, result := range report.Results {
			if expectedPlural, ok := expectedPlurals[result.Kind]; ok {
				Expect(result.ResourcePlural).To(Equal(expectedPlural),
					"expected %s resourcePlural to be %s, got %s",
					result.Kind, expectedPlural, result.ResourcePlural)
			}
		}
		log.Printf("All resourcePlural mappings correct ✓")

		log.Printf("\n"+
			"========================================\n"+
			"VALIDATION SUCCESS\n"+
			"========================================\n"+
			"Mode: %s\n"+
			"Context: %s\n"+
			"Total Scanned: %d\n"+
			"Compatible: %d\n"+
			"Incompatible: %d\n"+
			"========================================\n",
			report.Mode, report.ClusterContext,
			report.TotalScanned, report.Compatible, report.Incompatible)
	})
})
