package validate

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	cranevalidate "github.com/konveyor/crane/internal/validate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Crane validate: all compatible standard resources in offline mode", func() {
	It("[MTA-826] Validate final manifests in offline mode using captured API surface (tier0)", Label("tier0", "validate"), func() {
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
		srcApp := scenario.SrcApp
		kubectlSrc := scenario.KubectlSrc

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-validate-offline-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup temp directory")
			if paths.TempDir != "" {
				log.Printf("Removing temp dir: %s\n", paths.TempDir)
				if err := os.RemoveAll(paths.TempDir); err != nil {
					log.Printf("cleanup: failed to remove temp dir %q: %v", paths.TempDir, err)
				}
			}
		})
		DeferCleanup(func() {
			By("Delete test namespace on source and target (wait for completion)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})

		By("Export manifests from source cluster")
		runner := scenario.CraneNonAdmin
		runner.WorkDir = paths.TempDir
		err = runner.Export(srcApp.Namespace, paths.ExportDir)
		Expect(err).NotTo(HaveOccurred(), "crane export failed")
		log.Printf("Crane export completed successfully")

		By("Verify exported manifests exist")
		exportedFiles, err := filepath.Glob(filepath.Join(paths.ExportDir, "*.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(exportedFiles).NotTo(BeEmpty(), "expected exported YAML files in %s", paths.ExportDir)
		log.Printf("Found %d exported files", len(exportedFiles))

		By("Transform exported manifests")
		err = runner.Transform(paths.ExportDir, paths.TransformDir)
		Expect(err).NotTo(HaveOccurred(), "crane transform failed")
		log.Printf("Crane transform completed successfully")

		By("Apply transformations to generate final manifests")
		err = runner.Apply(paths.ExportDir, paths.TransformDir, paths.OutputDir)
		Expect(err).NotTo(HaveOccurred(), "crane apply failed")
		log.Printf("Crane apply completed successfully")

		By("Verify output manifests exist")
		outputFiles, err := filepath.Glob(filepath.Join(paths.OutputDir, "*.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(outputFiles).NotTo(BeEmpty(), "expected output YAML files in %s", paths.OutputDir)
		log.Printf("Found %d output files", len(outputFiles))

		By("Capture API surface from target cluster")
		apiSurfaceFile := filepath.Join(paths.TempDir, "api-surface.json")
		captureScript := filepath.Join(config.CraneBin, "..", "..", "scripts", "capture-api-surface.sh")

		captureCmd := exec.Command("bash", captureScript, "--context", scenario.TgtApp.Context, "-o", apiSurfaceFile)
		captureOut, err := captureCmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "failed to capture API surface: %s", string(captureOut))
		log.Printf("API surface captured to %s", apiSurfaceFile)

		By("Verify API surface file exists and is valid JSON")
		Expect(apiSurfaceFile).To(BeAnExistingFile(), "expected API surface file at %s", apiSurfaceFile)
		apiSurfaceData, err := os.ReadFile(apiSurfaceFile)
		Expect(err).NotTo(HaveOccurred())
		var apiSurface map[string]interface{}
		err = json.Unmarshal(apiSurfaceData, &apiSurface)
		Expect(err).NotTo(HaveOccurred(), "API surface file should contain valid JSON")
		log.Printf("API surface file validated")

		By("Run crane validate in offline mode using captured API surface")
		validateDir := filepath.Join(paths.TempDir, "validate")
		stdout, err := runner.Validate(ValidateOptions{
			InputDir:         paths.OutputDir,
			ValidateDir:      validateDir,
			APIResourcesFile: apiSurfaceFile,
		})
		Expect(err).NotTo(HaveOccurred(), "crane validate should succeed in offline mode with all compatible resources")
		log.Printf("Crane validate completed in offline mode with exit code 0")
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

		By("Verify report mode is 'offline'")
		Expect(report.Mode).To(Equal("offline"), "expected mode='offline' in report")
		log.Printf("Report mode: %s ✓", report.Mode)

		By("Verify apiResourcesSource is set to the captured API surface file")
		Expect(report.APIResourcesSource).NotTo(BeEmpty(), "expected apiResourcesSource to be set in offline mode")
		Expect(report.APIResourcesSource).To(Equal(apiSurfaceFile), "expected apiResourcesSource to match API surface file path")
		log.Printf("API resources source: %s ✓", report.APIResourcesSource)

		By("Verify clusterContext is NOT set in offline mode")
		Expect(report.ClusterContext).To(BeEmpty(), "expected clusterContext to be empty in offline mode")
		log.Printf("Cluster context is empty (offline mode) ✓")

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
				Expect(result.Status).To(Equal("OK"),
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

		By("Verify offline mode works without cluster connectivity")
		log.Printf("Offline validation successfully completed without requiring cluster API calls during validation")

		log.Printf("\n" +
			"========================================\n" +
			"OFFLINE VALIDATION SUCCESS\n" +
			"========================================\n" +
			"Mode: %s\n" +
			"API Resources Source: %s\n" +
			"Total Scanned: %d\n" +
			"Compatible: %d\n" +
			"Incompatible: %d\n" +
			"========================================\n",
			report.Mode, report.APIResourcesSource,
			report.TotalScanned, report.Compatible, report.Incompatible)
	})
})
