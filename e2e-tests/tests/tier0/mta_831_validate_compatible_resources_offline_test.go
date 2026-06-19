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
)

var _ = Describe("Crane validate: all compatible standard resources in offline mode in JSON and YAML formats", func() {
	It("[MTA-831][MTA-848] Generate and validate crane validate report in JSON and YAML formats",
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

		// Table-driven validation for both JSON and YAML formats
		type formatTest struct {
			format    string
			dirSuffix string
			label     string
		}

		formats := []formatTest{
			{format: "json", dirSuffix: "validate", label: "JSON"},
			{format: "yaml", dirSuffix: "validate-yaml", label: "YAML"},
		}

		reports := make(map[string]cranevalidate.ValidationReport)

		for _, ft := range formats {
			By("Run crane validate in offline mode with output in " + ft.label + " format")
			validateDir := filepath.Join(paths.TempDir, ft.dirSuffix)

			// Run crane validate command
			stdout, err := runner.Validate(ValidateOptions{
				InputDir:         paths.OutputDir,
				ValidateDir:      validateDir,
				APIResourcesFile: apiSurfaceFile,
				OutputFormat:     ft.format,
			})
			Expect(err).NotTo(HaveOccurred(), "crane validate should succeed")
			log.Printf("Validate %s stdout: %s", ft.format, stdout)

			By("Parse " + ft.label + " validation report")
			var report cranevalidate.ValidationReport
			err = utils.ParseValidationReport(validateDir, ft.format, &report)
			Expect(err).NotTo(HaveOccurred(), "should parse %s report", ft.label)

			// Define expectations for this test
			expectations := utils.ValidationExpectations{
				ValidationReport: cranevalidate.ValidationReport{
					Mode:               "offline",
					APIResourcesSource: apiSurfaceFile,
					TotalScanned:       5,
					Compatible:         5,
					Incompatible:       0,
				},
				ExpectedResources: map[string]string{
					"Deployment":  "apps/v1",
					"Service":     "v1",
					"ConfigMap":   "v1",
					"Secret":      "v1",
					"RoleBinding": "rbac.authorization.k8s.io/v1",
				},
				ExpectedStatus:    cranevalidate.StatusOK,
				Namespace:         namespace,
				ExpectFailuresDir: false,
			}

			utils.VerifyValidateResults(report, validateDir, ft.label, expectations)

			log.Printf("\n"+
				"========================================\n"+
				"%s OUTPUT VALIDATION SUCCESS\n"+
				"========================================\n"+
				"Mode: %s\n"+
				"API Resources Source: %s\n"+
				"Total Scanned: %d\n"+
				"Compatible: %d\n"+
				"Incompatible: %d\n"+
				"========================================\n",
				ft.label, report.Mode, report.APIResourcesSource,
				report.TotalScanned, report.Compatible, report.Incompatible)

			reports[ft.label] = report
		}

		report := reports["JSON"]
		reportYAML := reports["YAML"]

		By("Verify JSON and YAML reports contain identical data")
		Expect(reportYAML.Mode).To(Equal(report.Mode), "JSON and YAML reports should have same mode")
		Expect(reportYAML.APIResourcesSource).To(Equal(report.APIResourcesSource), "JSON and YAML reports should have same apiResourcesSource")
		Expect(reportYAML.TotalScanned).To(Equal(report.TotalScanned), "JSON and YAML reports should have same totalScanned")
		Expect(reportYAML.Compatible).To(Equal(report.Compatible), "JSON and YAML reports should have same compatible count")
		Expect(reportYAML.Incompatible).To(Equal(report.Incompatible), "JSON and YAML reports should have same incompatible count")
		Expect(reportYAML.Results).To(HaveLen(len(report.Results)), "JSON and YAML reports should have same number of results")

		By("Verify each resource in JSON and YAML reports match")
		// Create maps for easier comparison
		jsonResults := make(map[string]cranevalidate.ValidationResult)
		for _, r := range report.Results {
			key := r.Kind + "/" + r.Namespace
			jsonResults[key] = r
		}

		yamlResults := make(map[string]cranevalidate.ValidationResult)
		for _, r := range reportYAML.Results {
			key := r.Kind + "/" + r.Namespace
			yamlResults[key] = r
		}

		// Verify same resources in both formats
		for key, jsonRes := range jsonResults {
			yamlRes, found := yamlResults[key]
			Expect(found).To(BeTrue(), "resource %s found in JSON but missing in YAML", key)
			Expect(yamlRes.APIVersion).To(Equal(jsonRes.APIVersion), "resource %s has different apiVersion in JSON vs YAML", key)
			Expect(yamlRes.Status).To(Equal(jsonRes.Status), "resource %s has different status in JSON vs YAML", key)
			Expect(yamlRes.ResourcePlural).To(Equal(jsonRes.ResourcePlural), "resource %s has different resourcePlural in JSON vs YAML", key)
		}

		// Verify no extra resources in YAML
		for key := range yamlResults {
			_, found := jsonResults[key]
			Expect(found).To(BeTrue(), "resource %s found in YAML but missing in JSON", key)
		}

		log.Printf("✅ JSON and YAML reports are identical!")
	})
})
