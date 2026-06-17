package e2e

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	cranevalidate "github.com/konveyor/crane/internal/validate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// runValidateAndParseReport runs crane validate and parses the report
func runValidateAndParseReport(runner CraneRunner, inputDir string, validateDir string, apiSurfaceFile string, outputFormat string) (cranevalidate.ValidationReport, error) {
	By("Run crane validate with " + outputFormat + " output format")
	stdout, err := runner.Validate(ValidateOptions{
		InputDir:         inputDir,
		ValidateDir:      validateDir,
		APIResourcesFile: apiSurfaceFile,
		OutputFormat:     outputFormat,
	})
	if err != nil {
		return cranevalidate.ValidationReport{}, err
	}
	log.Printf("Validate %s stdout: %s", outputFormat, stdout)

	By("Verify " + outputFormat + " validation report exists")
	reportExt := "." + outputFormat
	reportPath := filepath.Join(validateDir, "report"+reportExt)
	Expect(reportPath).To(BeAnExistingFile(), "expected report%s at %s", reportExt, reportPath)

	By("Parse and verify " + outputFormat + " validation report")
	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		return cranevalidate.ValidationReport{}, err
	}

	var report cranevalidate.ValidationReport
	if outputFormat == "yaml" {
		err = yaml.Unmarshal(reportData, &report)
	} else {
		err = json.Unmarshal(reportData, &report)
	}
	if err != nil {
		return cranevalidate.ValidationReport{}, err
	}

	return report, nil
}

// verifyCompatibleResources validates that a report contains all expected compatible resources
func verifyCompatibleResources(report cranevalidate.ValidationReport, namespace string, validateDir string, outputFormat string) {
	By("Verify report shows offline mode for " + outputFormat + " output")
	Expect(report.Mode).To(Equal("offline"), "expected validation mode to be 'offline' in %s report", outputFormat)
	log.Printf("%s validation mode: %s", outputFormat, report.Mode)

	By("Verify all 4 resources were scanned in " + outputFormat + " report")
	Expect(report.TotalScanned).To(Equal(4), "expected exactly 4 resources scanned in %s report", outputFormat)
	Expect(report.Compatible).To(Equal(4), "expected all 4 resources to be compatible in %s report", outputFormat)
	Expect(report.Incompatible).To(Equal(0), "expected no incompatible resources in %s report", outputFormat)
	log.Printf("%s report - Total: %d, Compatible: %d, Incompatible: %d",
		outputFormat, report.TotalScanned, report.Compatible, report.Incompatible)

	By("Verify expected resource types are present in " + outputFormat + " results")
	expectedResources := map[string]string{
		"Deployment": "apps/v1",
		"Service":    "v1",
		"ConfigMap":  "v1",
		"Secret":     "v1",
	}

	foundResources := make(map[string]bool)
	for _, result := range report.Results {
		log.Printf("%s report resource: %s/%s (namespace: %s, status: %s)",
			outputFormat, result.APIVersion, result.Kind, result.Namespace, result.Status)

		if expectedAPIVersion, expected := expectedResources[result.Kind]; expected {
			foundResources[result.Kind] = true
			Expect(result.APIVersion).To(Equal(expectedAPIVersion),
				"expected %s to have apiVersion %s in %s report", result.Kind, expectedAPIVersion, outputFormat)
			Expect(result.Status).To(Equal(cranevalidate.StatusOK),
				"expected %s to have status OK in %s report", result.Kind, outputFormat)
			Expect(result.Namespace).To(Equal(namespace),
				"expected %s to be in namespace %s in %s report", result.Kind, namespace, outputFormat)
		}
	}

	By("Verify all 4 resource types were found in " + outputFormat + " report")
	var missingResources []string
	for kind := range expectedResources {
		if !foundResources[kind] {
			missingResources = append(missingResources, kind)
		}
	}
	Expect(missingResources).To(BeEmpty(),
		"expected to find all resources in %s validation results, missing: %v", outputFormat, missingResources)

	for kind := range expectedResources {
		log.Printf("✓ Found %s in %s report with correct apiVersion and status", kind, outputFormat)
	}

	By("Verify no failures directory was created")
	failuresDir := filepath.Join(validateDir, "failures")
	_, err := os.Stat(failuresDir)
	Expect(os.IsNotExist(err)).To(BeTrue(),
		"expected no failures/ directory for all compatible resources")
	log.Printf("No failures directory created")
}

var _ = Describe("Crane validate: all compatible standard resources in offline mode", func() {
	It("[MTA-831] Generate and validate crane validate report in JSON format",
		Label("tier0", "validate"), func() {
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

		paths, err := NewScenarioPaths("crane-validate-offline-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		runner.WorkDir = paths.TempDir
		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, srcApp.Namespace, paths)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Capture API surface from target cluster")
		captureScript, err := utils.CaptureAPISurfaceScriptPath()
		Expect(err).NotTo(HaveOccurred(), "failed to locate capture-api-surface.sh script")
		log.Printf("Capture script verified at: %s", captureScript)
		apiSurfaceFile := filepath.Join(paths.TempDir, "api-surface.json")

		chmodCmd := exec.Command("chmod", "+x", captureScript)
		if chmodOut, err := chmodCmd.CombinedOutput(); err != nil {
			log.Printf("chmod failed (continuing): %v, output: %s", err, string(chmodOut))
		}

		captureCmd := exec.Command("bash", captureScript, "--context", scenario.KubectlTgtNonAdmin.Context, "-o", apiSurfaceFile)
		captureOut, err := captureCmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "failed to capture API surface: %s", string(captureOut))
		log.Printf("API surface captured to %s using context %s", apiSurfaceFile, scenario.KubectlTgtNonAdmin.Context)

		By("Verify API surface file exists and is valid JSON")
		Expect(apiSurfaceFile).To(BeAnExistingFile(), "expected API surface file at %s", apiSurfaceFile)
		apiSurfaceData, err := os.ReadFile(apiSurfaceFile)
		Expect(err).NotTo(HaveOccurred())
		var apiSurface map[string]interface{}
		err = json.Unmarshal(apiSurfaceData, &apiSurface)
		Expect(err).NotTo(HaveOccurred(), "API surface file should contain valid JSON")
		log.Printf("API surface file validated")

		// Automate MTA-848: Generate and validate crane validate report in YAML format
		By("Run crane validate in offline mode with output in JSON format")
		validateDir := filepath.Join(paths.TempDir, "validate")
		report, err := runValidateAndParseReport(runner, paths.OutputDir, validateDir, apiSurfaceFile, "json")
		Expect(err).NotTo(HaveOccurred(), "validate with JSON output should succeed for all compatible resources")
		log.Printf("Crane validate completed with output in JSON format")

		By("Verify apiResourcesSource is set to the captured API surface file")
		Expect(report.APIResourcesSource).NotTo(BeEmpty(), "expected apiResourcesSource to be set in offline mode")
		Expect(report.APIResourcesSource).To(Equal(apiSurfaceFile), "expected apiResourcesSource to match API surface file path")
		log.Printf("API resources source: %s", report.APIResourcesSource)

		verifyCompatibleResources(report, namespace, validateDir, "JSON")

		log.Printf("\n"+
			"========================================\n"+
			"JSON OUTPUT VALIDATION SUCCESS\n"+
			"========================================\n"+
			"Mode: %s\n"+
			"API Resources Source: %s\n"+
			"Total Scanned: %d\n"+
			"Compatible: %d\n"+
			"Incompatible: %d\n"+
			"========================================\n",
			report.Mode, report.APIResourcesSource,
			report.TotalScanned, report.Compatible, report.Incompatible)

		By("Run crane validate in offline mode with output in YAML format")
		validateDirYAML := filepath.Join(paths.TempDir, "validate-yaml")
		reportYAML, err := runValidateAndParseReport(runner, filepath.Join(paths.OutputDir, "resources", namespace), validateDirYAML, apiSurfaceFile, "yaml")
		Expect(err).NotTo(HaveOccurred(), "validate with YAML output should succeed for all compatible resources")
		verifyCompatibleResources(reportYAML, namespace, validateDirYAML, "YAML")

		log.Printf("\n"+
			"========================================\n"+
			"YAML OUTPUT VALIDATION SUCCESS\n"+
			"========================================\n"+
			"Mode: %s\n"+
			"API Resources Source: %s\n"+
			"Total Scanned: %d\n"+
			"Compatible: %d\n"+
			"Incompatible: %d\n"+
			"All 4 resource types verified in YAML output!\n"+
			"========================================\n",
			reportYAML.Mode, report.APIResourcesSource,
			reportYAML.TotalScanned, reportYAML.Compatible, reportYAML.Incompatible)
	})
})
