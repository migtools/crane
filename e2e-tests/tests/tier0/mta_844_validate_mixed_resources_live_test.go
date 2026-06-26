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

var _ = Describe("Crane validate: mixed compatible and incompatible resources in live mode", func() {
	It("[MTA-844] Crane should identify compatible and incompatible resources in live mode as namespace-admin (tier0)", Label("tier0", "validate"), func() {
		appName := "multi-resource-app"
		namespace := "mixed-resources-live"

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

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-validate-mixed-*")
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}
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
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Mutate Deployment to deprecated extensions/v1beta1 API version")
		deploymentPattern := filepath.Join(paths.OutputDir, "resources", namespace, "Deployment_*.yaml")
		deploymentMatches, err := filepath.Glob(deploymentPattern)
		Expect(err).NotTo(HaveOccurred())
		Expect(deploymentMatches).To(HaveLen(1), "expected exactly one Deployment manifest")

		deploymentPath := deploymentMatches[0]
		deploymentBytes, err := os.ReadFile(deploymentPath)
		Expect(err).NotTo(HaveOccurred())

		mutatedDeployment := strings.Replace(string(deploymentBytes), "apiVersion: apps/v1", "apiVersion: extensions/v1beta1", 1)
		Expect(mutatedDeployment).NotTo(Equal(string(deploymentBytes)), "expected to replace Deployment apiVersion")
		Expect(os.WriteFile(deploymentPath, []byte(mutatedDeployment), 0o644)).NotTo(HaveOccurred())
		log.Printf("Mutated Deployment to extensions/v1beta1 at %s", deploymentPath)

		By("Mutate ConfigMap to deprecated v1beta1 API version")
		configMapPattern := filepath.Join(paths.OutputDir, "resources", namespace, "ConfigMap_*.yaml")
		configMapMatches, err := filepath.Glob(configMapPattern)
		Expect(err).NotTo(HaveOccurred())
		Expect(configMapMatches).To(HaveLen(1), "expected exactly one ConfigMap manifest (kube-root-ca.crt is filtered out by transform)")

		appConfigMapPath := configMapMatches[0]

		configMapBytes, err := os.ReadFile(appConfigMapPath)
		Expect(err).NotTo(HaveOccurred())

		mutatedConfigMap := strings.Replace(string(configMapBytes), "apiVersion: v1", "apiVersion: v1beta1", 1)
		Expect(mutatedConfigMap).NotTo(Equal(string(configMapBytes)), "expected to replace ConfigMap apiVersion")
		Expect(os.WriteFile(appConfigMapPath, []byte(mutatedConfigMap), 0o644)).NotTo(HaveOccurred())
		log.Printf("Mutated ConfigMap to v1beta1 at %s", appConfigMapPath)

		By("Run crane validate in live mode against target context")
		validateDir := filepath.Join(paths.TempDir, "validate")
		stdout, err := runner.Validate(ValidateOptions{
			Context:      scenario.KubectlTgtNonAdmin.Context,
			InputDir:     filepath.Join(paths.OutputDir, "resources", namespace),
			ValidateDir:  validateDir,
			OutputFormat: "json",
		})

		Expect(err).To(HaveOccurred(), "validate should fail when incompatible resources are present")
		Expect(err.Error()).To(ContainSubstring("incompatible"))
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

		By("Verify report contains mixed results: both compatible and incompatible resources")
		Expect(report.TotalScanned).To(BeNumerically(">=", 4), "expected at least 4 resources scanned")
		Expect(report.Compatible).To((Equal(3)), "expected 3 compatible resources")
		Expect(report.Incompatible).To(Equal(2), "expected exactly 2 incompatible resources (Deployment and ConfigMap)")
		Expect(report.Compatible+report.Incompatible).To(Equal(report.TotalScanned),
			"expected Compatible + Incompatible to equal TotalScanned (found %d + %d != %d)",
			report.Compatible, report.Incompatible, report.TotalScanned)
		log.Printf("Total: %d, Compatible: %d, Incompatible: %d", report.TotalScanned, report.Compatible, report.Incompatible)

		By("Verify compatible resources (Service, Secret, RoleBinding) have status OK")
		compatibleResources := map[string]string{
			"Service":     "v1",
			"Secret":      "v1",
			"RoleBinding": "rbac.authorization.k8s.io/v1",
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

		By("Verify incompatible resources (Deployment and ConfigMap) have correct status and suggestions")
		incompatibleResources := map[string]struct {
			apiVersion string
			suggestion string
		}{
			"Deployment": {
				apiVersion: "extensions/v1beta1",
				suggestion: "available as apps/v1",
			},
			"ConfigMap": {
				apiVersion: "v1beta1",
				suggestion: "", // ConfigMap v1beta1 doesn't have a direct suggestion
			},
		}

		foundIncompatible := make(map[string]bool)
		for _, result := range report.Results {
			if expected, isIncompatible := incompatibleResources[result.Kind]; isIncompatible {
				if result.APIVersion == expected.apiVersion {
					foundIncompatible[result.Kind] = true
					Expect(result.Status).To(Equal(cranevalidate.StatusIncompatible),
						"expected %s to have status Incompatible", result.Kind)
					if expected.suggestion != "" {
						Expect(result.Suggestion).To(ContainSubstring(expected.suggestion),
							"expected suggestion to mention %s", expected.suggestion)
					}
					Expect(result.Namespace).To(Equal(namespace),
						"expected %s to be in namespace %s", result.Kind, namespace)
					log.Printf("Found incompatible %s (%s) with suggestion: %s", result.Kind, result.APIVersion, result.Suggestion)
				}
			}
		}

		for kind := range incompatibleResources {
			Expect(foundIncompatible[kind]).To(BeTrue(), "expected to find incompatible %s in report", kind)
		}

		By("Verify failures directory contains both incompatible resources")
		failuresDir := filepath.Join(validateDir, "failures")
		Expect(failuresDir).To(BeADirectory(), "expected failures/ directory to exist")

		failureFiles, err := filepath.Glob(filepath.Join(failuresDir, "*.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(failureFiles).To(HaveLen(2), "expected exactly 2 failure files for the incompatible resources")

		// Verify both failure files exist
		foundFailureKinds := make(map[string]bool)
		for _, failureFile := range failureFiles {
			failureBytes, err := os.ReadFile(failureFile)
			Expect(err).NotTo(HaveOccurred())
			failureContent := string(failureBytes)

			if strings.Contains(failureContent, "kind: Deployment") {
				foundFailureKinds["Deployment"] = true
				Expect(failureContent).To(ContainSubstring("apiVersion: extensions/v1beta1"),
					"Deployment failure file should contain the deprecated apiVersion")
				Expect(failureContent).To(ContainSubstring("suggestion: available as apps/v1"),
					"Deployment failure file should include the suggestion")
				log.Printf("Deployment failure file created at: %s", failureFile)
			} else if strings.Contains(failureContent, "kind: ConfigMap") {
				foundFailureKinds["ConfigMap"] = true
				Expect(failureContent).To(ContainSubstring("apiVersion: v1beta1"),
					"ConfigMap failure file should contain the deprecated apiVersion")
				log.Printf("ConfigMap failure file created at: %s", failureFile)
			}
		}

		Expect(foundFailureKinds["Deployment"]).To(BeTrue(), "expected to find Deployment failure file")
		Expect(foundFailureKinds["ConfigMap"]).To(BeTrue(), "expected to find ConfigMap failure file")

		By("Verify stdout contains suggestions for incompatible resources")
		Expect(stdout).To(ContainSubstring("available as apps/v1"),
			"stdout should contain suggestion for Deployment")

		log.Printf("\n"+
			"========================================\n"+
			"MIXED RESOURCES VALIDATION SUCCESS\n"+
			"========================================\n"+
			"Mode: %s\n"+
			"Total Scanned: %d\n"+
			"Compatible: %d (Service, Secret, RoleBinding)\n"+
			"Incompatible: %d (Deployment extensions/v1beta1, ConfigMap v1beta1)\n"+
			"========================================\n",
			report.Mode,
			report.TotalScanned,
			report.Compatible,
			report.Incompatible)
	})
})
