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

var _ = Describe("Validate core group omission [Offline Mode]", func() {
	It("should use apiVersion v1 for core resources when API group name is omitted as namespace-admin", Label("tier1", "validate"), func() {
		appName := "multi-resource-app"
		namespace := "validate-offline-omission"
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

		DeferCleanup(cleanup)
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

		By("Capture target cluster api surface in a api-surface.json file")
		apiSurfaceFile := filepath.Join(paths.TempDir, "api-surface.json")
		captureScript, err := utils.CaptureAPISurfaceScriptPath()
		Expect(err).NotTo(HaveOccurred())
		captureCmd := exec.Command("bash", captureScript, "--context", scenario.KubectlTgtNonAdmin.Context, "-o", apiSurfaceFile)
		captureOut, err := captureCmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "failed to capture API surface %s", string(captureOut))
		By("Verify captured API surface file exists and is valid JSON")
		Expect(apiSurfaceFile).To(BeAnExistingFile(), "expected api surface file at %s", apiSurfaceFile)

		apiSurfaceData, err := os.ReadFile(apiSurfaceFile)
		Expect(err).NotTo(HaveOccurred())

		var apiSurface map[string]any
		err = json.Unmarshal(apiSurfaceData, &apiSurface)
		Expect(err).NotTo(HaveOccurred(), "api-surface.json should contain valid JSON")

		By("Run crane validate in offline mode using captured API surface")
		stdout, err := runner.Validate(ValidateOptions{
			InputDir:         filepath.Join(paths.OutputDir, "resources", namespace),
			ValidateDir:      paths.ValidateDir,
			APIResourcesFile: apiSurfaceFile,
		})
		Expect(err).NotTo(HaveOccurred(), "validate should pass for compatible resources in offline mode")
		Expect(stdout).To(ContainSubstring("Mode: offline"))

		By("Parse validation report")
		reportPath := filepath.Join(paths.ValidateDir, "report.json")
		reportData, err := os.ReadFile(reportPath)
		Expect(err).NotTo(HaveOccurred())

		var report cranevalidate.ValidationReport
		err = json.Unmarshal(reportData, &report)
		Expect(err).NotTo(HaveOccurred(), "failed to parse report.json")

		By("Verify core resources use apiVersion v1 (group omitted)")
		coreKinds := map[string]bool{
			"Service":        true,
			"ConfigMap":      true,
			"Secret":         true,
			"ServiceAccount": true,
		}

		requiredCoreKinds := map[string]bool{
			"Service":   true,
			"ConfigMap": true,
			"Secret":    true,
		}

		foundCoreKinds := map[string]bool{}

		for _, result := range report.Results {
			if !coreKinds[result.Kind] {
				continue
			}

			foundCoreKinds[result.Kind] = true

			Expect(result.APIVersion).To(Equal("v1"), "expected core resource %s to use apiVersion v1", result.Kind)
			Expect(result.APIVersion).NotTo(ContainSubstring("/"), "expected core resource %s apiVersion to omit group", result.Kind)
			Expect(result.Status).To(Equal(cranevalidate.StatusOK), "expected core resource %s to be compatible", result.Kind)
		}

		By("Verify required core resources were present in validation results")
		for kind := range requiredCoreKinds {
			Expect(foundCoreKinds[kind]).To(BeTrue(), "expected core resource %s in validation results", kind)
		}

		By("Verify overall validation is fully compatible")
		Expect(report.Incompatible).To(Equal(0), "expected no incompatible resources")
		Expect(report.Compatible).To(BeNumerically(">=", len(requiredCoreKinds)), "expected compatible resources to include required core resources")

		By("Verify no failures directory is created")
		failuresDir := filepath.Join(paths.ValidateDir, "failures")
		_, err = os.Stat(failuresDir)
		Expect(os.IsNotExist(err)).To(BeTrue(), "expected no failures directory for compatible resources")
	})
})
