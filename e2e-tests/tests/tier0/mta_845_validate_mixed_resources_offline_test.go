package e2e

import (
	"encoding/json"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	cranevalidate "github.com/konveyor/crane/internal/validate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Crane validate: mixed compatible and incompatible resources in offline mode", func() {
	It("[MTA-845] Crane should identify compatible and incompatible resources in offline mode as namespace-admin (tier0)", Label("tier0", "validate"), func() {
		appName := "multi-resource-app"
		namespace := "mixed-resources-offline"

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

		paths, err := NewScenarioPaths("crane-validate-mixed-offline-*")
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

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		runner.WorkDir = paths.TempDir
		By("Run crane export/transform pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Mutate Deployment to deprecated extensions/v1beta1 API version")
		deploymentPattern := filepath.Join(paths.OutputDir, "resources", namespace, "Deployment_*.yaml")
		deploymentMatches, err := filepath.Glob(deploymentPattern)
		Expect(err).NotTo(HaveOccurred())
		Expect(deploymentMatches).NotTo(BeEmpty(), "expected at least one Deployment manifest")

		deploymentPath := deploymentMatches[0]
		deploymentBytes, err := os.ReadFile(deploymentPath)
		Expect(err).NotTo(HaveOccurred())

		mutatedDeployment := strings.Replace(string(deploymentBytes), "apiVersion: apps/v1", "apiVersion: extensions/v1beta1", 1)
		Expect(mutatedDeployment).NotTo(Equal(string(deploymentBytes)), "expected to replace Deployment apiVersion")
		Expect(os.WriteFile(deploymentPath, []byte(mutatedDeployment), 0o644)).NotTo(HaveOccurred())
		log.Printf("Mutated Deployment to extensions/v1beta1 at %s", deploymentPath)

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

		By("Run crane validate in offline mode using captured API surface")
		validateDir := filepath.Join(paths.TempDir, "validate")
		stdout, err := runner.Validate(ValidateOptions{
			InputDir:         filepath.Join(paths.OutputDir, "resources", namespace),
			ValidateDir:      validateDir,
			APIResourcesFile: apiSurfaceFile,
		})

		Expect(err).To(HaveOccurred(), "validate should fail when incompatible resources are present")
		Expect(err.Error()).To(Or(
			ContainSubstring("incompatible"),
			ContainSubstring("Incompatible"),
		), "error message should indicate incompatible resources")
		Expect(stdout).To(ContainSubstring("Mode: offline"), "expected offline mode in output")
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

		By("Verify report shows offline mode")
		Expect(report.Mode).To(Equal("offline"), "expected validation mode to be 'offline'")
		log.Printf("Validation mode: %s", report.Mode)

		By("Verify apiResourcesSource is set to the captured API surface file")
		Expect(report.APIResourcesSource).NotTo(BeEmpty(), "expected apiResourcesSource to be set in offline mode")
		Expect(report.APIResourcesSource).To(Equal(apiSurfaceFile), "expected apiResourcesSource to match API surface file path")
		log.Printf("API resources source: %s", report.APIResourcesSource)

		By("Verify report contains mixed results: both compatible and incompatible resources")
		Expect(report.TotalScanned).To(BeNumerically(">=", 4), "expected at least 4 resources scanned")
		Expect(report.Compatible).To(BeNumerically(">", 0), "expected some compatible resources")
		Expect(report.Incompatible).To(Equal(1), "expected exactly 1 incompatible resource (Deployment)")
		Expect(report.Compatible+report.Incompatible).To(Equal(report.TotalScanned),
			"expected Compatible + Incompatible to equal TotalScanned (found %d + %d != %d)",
			report.Compatible, report.Incompatible, report.TotalScanned)
		log.Printf("Total: %d, Compatible: %d, Incompatible: %d", report.TotalScanned, report.Compatible, report.Incompatible)

		By("Verify compatible resources (Service, ConfigMap, Secret) have status OK")
		compatibleResources := map[string]string{
			"Service":   "v1",
			"ConfigMap": "v1",
			"Secret":    "v1",
		}

		foundCompatible := make(map[string]bool)
		for _, result := range report.Results {
			log.Printf("Resource: %s/%s (namespace: %s, status: %s)",
				result.APIVersion, result.Kind, result.Namespace, result.Status)

			if expectedAPIVersion, expected := compatibleResources[result.Kind]; expected {
				foundCompatible[result.Kind] = true
				Expect(result.APIVersion).To(Equal(expectedAPIVersion),
					"expected %s to have apiVersion %s", result.Kind, expectedAPIVersion)
				Expect(result.Status).To(Equal(cranevalidate.StatusOK),
					"expected %s to have status OK", result.Kind)
				Expect(result.Namespace).To(Equal(namespace),
					"expected %s to be in namespace %s", result.Kind, namespace)
			}
		}

		By("Verify all compatible resource types were found")
		for kind := range compatibleResources {
			Expect(foundCompatible[kind]).To(BeTrue(), "expected to find compatible %s in report", kind)
			log.Printf("Found compatible %s with status OK", kind)
		}

		By("Verify incompatible Deployment has correct status and suggestion")
		foundIncompatibleDeployment := false
		for _, result := range report.Results {
			if result.Kind == "Deployment" && result.APIVersion == "extensions/v1beta1" {
				foundIncompatibleDeployment = true
				Expect(result.Status).To(Equal(cranevalidate.StatusIncompatible),
					"expected Deployment to have status Incompatible")
				Expect(result.Suggestion).To(ContainSubstring("available as apps/v1"),
					"expected suggestion to mention apps/v1")
				Expect(result.Namespace).To(Equal(namespace),
					"expected Deployment to be in namespace %s", namespace)
				log.Printf("Found incompatible Deployment with suggestion: %s", result.Suggestion)
			}
		}
		Expect(foundIncompatibleDeployment).To(BeTrue(), "expected to find incompatible Deployment in report")

		By("Verify failures directory contains only the incompatible Deployment")
		failuresDir := filepath.Join(validateDir, "failures")
		Expect(failuresDir).To(BeADirectory(), "expected failures/ directory to exist")

		failureFiles, err := filepath.Glob(filepath.Join(failuresDir, "*.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(failureFiles).To(HaveLen(1), "expected exactly 1 failure file for the incompatible Deployment")

		failureBytes, err := os.ReadFile(failureFiles[0])
		Expect(err).NotTo(HaveOccurred())
		failureContent := string(failureBytes)
		Expect(failureContent).To(ContainSubstring("apiVersion: extensions/v1beta1"),
			"failure file should contain the deprecated apiVersion")
		Expect(failureContent).To(ContainSubstring("kind: Deployment"),
			"failure file should be for a Deployment")
		Expect(failureContent).To(ContainSubstring("suggestion: available as apps/v1"),
			"failure file should include the suggestion")
		log.Printf("Failure file created at: %s", failureFiles[0])

		By("Verify stdout contains suggestion for incompatible resource")
		Expect(stdout).To(ContainSubstring("available as apps/v1"),
			"stdout should contain suggestion for Deployment")

		log.Printf("\n"+
			"========================================\n"+
			"MIXED RESOURCES OFFLINE VALIDATION SUCCESS\n"+
			"========================================\n"+
			"Mode: %s\n"+
			"API Resources Source: %s\n"+
			"Total Scanned: %d\n"+
			"Compatible: %d (Service, ConfigMap, Secret)\n"+
			"Incompatible: %d (Deployment extensions/v1beta1)\n"+
			"========================================\n",
			report.Mode,
			report.APIResourcesSource,
			report.TotalScanned,
			report.Compatible,
			report.Incompatible)
	})
})
