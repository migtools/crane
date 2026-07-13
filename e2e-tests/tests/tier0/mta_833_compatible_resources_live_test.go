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
	"sigs.k8s.io/yaml"
)

func runValidateAndParseReportLive(runner CraneRunner, inputDir, validateDir, targetContext, format string) (cranevalidate.ValidationReport, error) {
	stdout, err := runner.Validate(ValidateOptions{
		Context:      targetContext,
		InputDir:     inputDir,
		ValidateDir:  validateDir,
		OutputFormat: format,
	})
	if err != nil {
		return cranevalidate.ValidationReport{}, err
	}
	log.Printf("Validate (%s) stdout: %s", format, stdout)

	reportPath := filepath.Join(validateDir, "report."+format)
	Expect(reportPath).To(BeAnExistingFile(), "expected report.%s at %s", format, reportPath)

	reportData, err := os.ReadFile(reportPath)
	if err != nil {
		return cranevalidate.ValidationReport{}, err
	}

	var report cranevalidate.ValidationReport
	if format == "yaml" {
		err = yaml.Unmarshal(reportData, &report)
	} else {
		err = json.Unmarshal(reportData, &report)
	}
	return report, err
}

func verifyCompatibleLiveReport(report cranevalidate.ValidationReport, namespace, targetContext string) {
	Expect(report.Mode).To(Equal("live"), "expected mode='live' in report")
	log.Printf("Report mode: %s", report.Mode)

	Expect(report.ClusterContext).NotTo(BeEmpty(), "expected clusterContext to be set in live mode")
	Expect(report.ClusterContext).To(Equal(targetContext), "expected clusterContext to match target context")
	log.Printf("Cluster context: %s", report.ClusterContext)

	Expect(report.TotalScanned).To(BeNumerically(">=", 4), "expected at least 4 resources scanned")
	log.Printf("Total scanned: %d", report.TotalScanned)

	Expect(report.Compatible).To(Equal(report.TotalScanned), "expected all resources to be compatible")
	Expect(report.Incompatible).To(Equal(0), "expected 0 incompatible resources")
	log.Printf("Compatible: %d, Incompatible: %d", report.Compatible, report.Incompatible)

	expectedResources := map[string]string{
		"Deployment": "apps/v1",
		"Service":    "v1",
		"ConfigMap":  "v1",
		"Secret":     "v1",
	}
	expectedPlurals := map[string]string{
		"Deployment": "deployments",
		"Service":    "services",
		"ConfigMap":  "configmaps",
		"Secret":     "secrets",
	}

	foundResources := make(map[string]bool)
	for _, result := range report.Results {
		log.Printf("Found resource: %s/%s (namespace: %s, status: %s, resourcePlural: %s)",
			result.APIVersion, result.Kind, result.Namespace, result.Status, result.ResourcePlural)

		if expectedAPIVersion, expected := expectedResources[result.Kind]; expected {
			foundResources[result.Kind] = true
			Expect(result.APIVersion).To(Equal(expectedAPIVersion),
				"expected %s to have apiVersion %s", result.Kind, expectedAPIVersion)
			Expect(result.Status).To(Equal(cranevalidate.StatusOK),
				"expected %s to have status OK", result.Kind)
			Expect(result.Namespace).To(Equal(namespace),
				"expected %s to be in namespace %s", result.Kind, namespace)
			Expect(result.ResourcePlural).To(Equal(expectedPlurals[result.Kind]),
				"expected %s resourcePlural to be %s", result.Kind, expectedPlurals[result.Kind])
		}
	}

	var missingResources []string
	for kind := range expectedResources {
		if !foundResources[kind] {
			missingResources = append(missingResources, kind)
		}
	}
	Expect(missingResources).To(BeEmpty(),
		"expected to find all resources in validation results, missing: %v", missingResources)

	for kind := range expectedResources {
		log.Printf("Found %s with correct apiVersion, status, and resourcePlural", kind)
	}
}

var _ = Describe("Crane validate: all compatible standard resources in live mode", func() {
	It("[MTA-833][MTA-865] Validate JSON and YAML reports after export/transform/apply pipeline (tier0)", Label("tier0", "validate"), func() {
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
		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin
		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = srcApp.ExtraVars

		By("Grant ns admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, _, cleanup, err := SetupActiveKubectlRunners(scenario, namespace)
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

		expectedKinds := []string{"Deployment", "Service", "ConfigMap", "Secret"}
		for _, kind := range expectedKinds {
			pattern := filepath.Join(outputResourcesDir, kind+"_*.yaml")
			matches, err := filepath.Glob(pattern)
			Expect(err).NotTo(HaveOccurred())
			Expect(matches).NotTo(BeEmpty(), "expected at least one %s file in %s", kind, outputResourcesDir)
		}
		log.Printf("Found %d output resource files in %s (verified all 4 kinds present)", len(outputFiles), outputResourcesDir)

		cases := []struct {
			format      string
			label       string
			validateDir string
		}{
			{format: "json", label: "JSON", validateDir: filepath.Join(paths.TempDir, "validate")},
			{format: "yaml", label: "YAML", validateDir: filepath.Join(paths.TempDir, "validate-yaml")},
		}

		reports := make(map[string]cranevalidate.ValidationReport)

		for _, tc := range cases {
			By("Run crane validate in live mode with output in " + tc.label + " format")
			report, err := runValidateAndParseReportLive(runner, paths.OutputDir, tc.validateDir, scenario.TgtApp.Context, tc.format)
			Expect(err).NotTo(HaveOccurred(), "failed to run validate or parse %s report", tc.label)
			reports[tc.format] = report

			By("Verify " + tc.label + " report content")
			verifyCompatibleLiveReport(report, namespace, scenario.TgtApp.Context)

			By("Verify no failures directory was created for " + tc.label + " run")
			failuresDir := filepath.Join(tc.validateDir, "failures")
			_, statErr := os.Stat(failuresDir)
			Expect(os.IsNotExist(statErr)).To(BeTrue(),
				"expected no failures/ directory for all compatible resources (%s)", tc.label)
			log.Printf("No failures directory created for %s run", tc.label)
		}

		By("Compare JSON and YAML reports for equivalence")
		jsonReport := reports["json"]
		yamlReport := reports["yaml"]

		Expect(yamlReport.Mode).To(Equal(jsonReport.Mode), "Mode mismatch between JSON and YAML reports")
		Expect(yamlReport.ClusterContext).To(Equal(jsonReport.ClusterContext), "ClusterContext mismatch between JSON and YAML reports")
		Expect(yamlReport.TotalScanned).To(Equal(jsonReport.TotalScanned), "TotalScanned mismatch between JSON and YAML reports")
		Expect(yamlReport.Compatible).To(Equal(jsonReport.Compatible), "Compatible mismatch between JSON and YAML reports")
		Expect(yamlReport.Incompatible).To(Equal(jsonReport.Incompatible), "Incompatible mismatch between JSON and YAML reports")
		log.Printf("Report summary fields match between JSON and YAML")

		Expect(len(yamlReport.Results)).To(Equal(len(jsonReport.Results)),
			"Results count mismatch: JSON has %d, YAML has %d", len(jsonReport.Results), len(yamlReport.Results))

		yamlResultsByKey := make(map[string]cranevalidate.ValidationResult)
		for _, r := range yamlReport.Results {
			key := r.APIVersion + "/" + r.Kind + "/" + r.Namespace
			yamlResultsByKey[key] = r
		}

		for _, jr := range jsonReport.Results {
			key := jr.APIVersion + "/" + jr.Kind + "/" + jr.Namespace
			yr, found := yamlResultsByKey[key]
			Expect(found).To(BeTrue(), "YAML report missing result for %s", key)
			Expect(yr.Status).To(Equal(jr.Status), "Status mismatch for %s", key)
			Expect(yr.ResourcePlural).To(Equal(jr.ResourcePlural), "ResourcePlural mismatch for %s", key)
			Expect(yr.Reason).To(Equal(jr.Reason), "Reason mismatch for %s", key)
			Expect(yr.Suggestion).To(Equal(jr.Suggestion), "Suggestion mismatch for %s", key)
		}
		log.Printf("All %d results match between JSON and YAML reports", len(jsonReport.Results))

		log.Printf("\n"+
			"========================================\n"+
			"VALIDATION SUCCESS\n"+
			"========================================\n"+
			"Mode: %s\n"+
			"Context: %s\n"+
			"Total Scanned: %d\n"+
			"Compatible: %d\n"+
			"Incompatible: %d\n"+
			"Results compared: %d\n"+
			"========================================\n",
			jsonReport.Mode, jsonReport.ClusterContext,
			jsonReport.TotalScanned, jsonReport.Compatible, jsonReport.Incompatible,
			len(jsonReport.Results))
	})
})
