package e2e

import (
	"log"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	cranevalidate "github.com/konveyor/crane/internal/validate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Crane validate: all compatible standard resources in live mode in JSON and YAML formats", func() {
	It("[MTA-833][MTA-865] Generate and validate crane validate report in JSON and YAML formats",
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
			applyOpts := ApplyOptions{TransformDir: paths.TransformDir,
				OutputDir: paths.OutputDir}
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

			By("Verify output resource manifests exist for all expected kinds")
			outputResourcesDir := filepath.Join(paths.OutputDir, "resources", namespace)
			outputFiles, err := filepath.Glob(filepath.Join(outputResourcesDir, "*.yaml"))
			Expect(err).NotTo(HaveOccurred())
			Expect(outputFiles).NotTo(BeEmpty(), "expected output resource YAML files in %s", outputResourcesDir)

			expectedKinds := []string{"Deployment", "Service", "ConfigMap", "Secret", "RoleBinding"}
			for _, kind := range expectedKinds {
				pattern := filepath.Join(outputResourcesDir, kind+"_*.yaml")
				matches, err := filepath.Glob(pattern)
				Expect(err).NotTo(HaveOccurred())
				Expect(matches).NotTo(BeEmpty(), "expected at least one %s file in %s", kind, outputResourcesDir)
			}
			log.Printf("Found %d output resource files in %s (verified all 5 kinds present)", len(outputFiles), outputResourcesDir)

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
				log.Printf("\n"+
					"========================================\n"+
					"RUNNING CRANE VALIDATE IN LIVE MODE WITH OUTPUT IN %s FORMAT\n"+
					"========================================",
					ft.label)

				// By("RUNNING CRANE VALIDATE IN LIVE MODE WITH OUTPUT IN " + ft.label + " format")
				validateDir := filepath.Join(paths.TempDir, ft.dirSuffix)

				stdout, err := runner.Validate(ValidateOptions{
					Context:      scenario.TgtApp.Context,
					InputDir:     paths.OutputDir,
					ValidateDir:  validateDir,
					OutputFormat: ft.format,
				})
				Expect(err).NotTo(HaveOccurred(), "crane validate should succeed")
				log.Printf("Validate %s stdout: %s", ft.format, stdout)

				By("Parse " + ft.label + " validation report")
				var report cranevalidate.ValidationReport
				err = utils.ParseValidationReport(validateDir, ft.format, &report)
				Expect(err).NotTo(HaveOccurred(), "should parse %s report", ft.label)

				expectations := utils.ValidationExpectations{
					ValidationReport: cranevalidate.ValidationReport{
						Mode:           "live",
						ClusterContext: scenario.TgtApp.Context,
						TotalScanned:   5,
						Compatible:     5,
						Incompatible:   0,
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
					"Cluster Context: %s\n"+
					"Total Scanned: %d\n"+
					"Compatible: %d\n"+
					"Incompatible: %d\n"+
					"========================================\n",
					ft.label, report.Mode, report.ClusterContext,
					report.TotalScanned, report.Compatible, report.Incompatible)

				reports[ft.label] = report
			}

			report := reports["JSON"]
			reportYAML := reports["YAML"]

			By("Verify JSON and YAML reports contain identical data")
			Expect(reportYAML.Mode).To(Equal(report.Mode), "JSON and YAML reports should have same mode")
			Expect(reportYAML.ClusterContext).To(Equal(report.ClusterContext), "JSON and YAML reports should have same clusterContext")
			Expect(reportYAML.TotalScanned).To(Equal(report.TotalScanned), "JSON and YAML reports should have same totalScanned")
			Expect(reportYAML.Compatible).To(Equal(report.Compatible), "JSON and YAML reports should have same compatible count")
			Expect(reportYAML.Incompatible).To(Equal(report.Incompatible), "JSON and YAML reports should have same incompatible count")
			Expect(reportYAML.Results).To(HaveLen(len(report.Results)), "JSON and YAML reports should have same number of results")

			By("Verify each resource in JSON and YAML reports match")
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

			for key, jsonRes := range jsonResults {
				yamlRes, found := yamlResults[key]
				Expect(found).To(BeTrue(), "resource %s found in JSON but missing in YAML", key)
				Expect(yamlRes.APIVersion).To(Equal(jsonRes.APIVersion), "resource %s has different apiVersion in JSON vs YAML", key)
				Expect(yamlRes.Status).To(Equal(jsonRes.Status), "resource %s has different status in JSON vs YAML", key)
				Expect(yamlRes.ResourcePlural).To(Equal(jsonRes.ResourcePlural), "resource %s has different resourcePlural in JSON vs YAML", key)
			}

			for key := range yamlResults {
				_, found := jsonResults[key]
				Expect(found).To(BeTrue(), "resource %s found in YAML but missing in JSON", key)
			}

			log.Printf("JSON and YAML reports are identical!")
		})
})
