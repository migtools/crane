package e2e

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BuildConfig to Shipwright conversion", func() {
	// Table-driven test for multiple BuildConfig scenarios
	DescribeTable("[MTA-819] should convert BuildConfig to Shipwright Build correctly",
		func(bcName, description string) {
			appName := "buildconfig-test"
			namespace := "buildconfig-test"

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
			tgtApp.ExtraVars = map[string]any{
				"non_admin_user": "true",
			}

			By("Grant namespace admin permissions to nonadmin user on source and target")
			kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupActiveKubectlRunners(scenario, namespace)
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				By("Delete test namespace on source and target")
				for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
					if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=60s"); err != nil {
						log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
					}
				}
			})
			DeferCleanup(cleanup)

			paths, err := NewScenarioPaths("crane-export-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				By("Cleanup temporary directories")
				if err := CleanupScenario(paths.TempDir, scenario.SrcAppNonAdmin, scenario.TgtAppNonAdmin); err != nil {
					log.Printf("cleanup: %v", err)
				}
			})

			By(fmt.Sprintf("Deploy buildconfig-test app (deploys %s BuildConfig)", bcName))
			log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
			Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
			log.Printf("Source app %s prepared successfully (BuildConfigs deployed)\n", srcApp.Name)

			By(fmt.Sprintf("Verify %s BuildConfig exists", bcName))
			out, err := kubectlSrcNonAdmin.Run("get", "buildconfig", bcName, "-n", namespace, "-o", "name")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(ContainSubstring(fmt.Sprintf("buildconfig.build.openshift.io/%s", bcName)))
			log.Printf("BuildConfig %s exists in namespace %s\n", bcName, namespace)

			By("Run crane export/transform/apply pipeline with BuildConfig plugin")
			exportOpts := ExportOptions{
				Namespace: namespace,
				ExportDir: paths.ExportDir,
			}
			transformOpts := TransformOptions{
				ExportDir:    paths.ExportDir,
				TransformDir: paths.TransformDir,
				PluginDir:    config.PluginDir,
			}
			applyOpts := ApplyOptions{
				TransformDir: paths.TransformDir,
				OutputDir:    paths.OutputDir,
			}

			runner.WorkDir = paths.TempDir
			log.Printf("Running crane pipeline for namespace %s with plugin directory: %s\n", namespace, config.PluginDir)
			Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
			log.Printf("Crane pipeline completed for namespace %s\n", namespace)

			By(fmt.Sprintf("Compare generated Build against golden file for %s", bcName))
			// Find generated Build YAML
			buildFilePattern := filepath.Join(paths.OutputDir, "resources", namespace, "Build_shipwright.io_*.yaml")
			buildMatches, err := filepath.Glob(buildFilePattern)
			Expect(err).NotTo(HaveOccurred())

			// Find the specific Build for this test case
			var actualBuildPath string
			for _, buildPath := range buildMatches {
				if strings.Contains(buildPath, bcName) {
					actualBuildPath = buildPath
					break
				}
			}
			Expect(actualBuildPath).NotTo(BeEmpty(), fmt.Sprintf("Build YAML for %s should be generated", bcName))

			// Golden file path (relative to tier0 test directory - go up 2 levels to crane root)
			goldenFilePath := filepath.Join("../../testdata/buildconfig-test/golden", fmt.Sprintf("%s-golden.yaml", bcName))

			// Compare with golden file
			diffs, err := CompareWithGoldenFile(actualBuildPath, goldenFilePath)
			Expect(err).NotTo(HaveOccurred(), "Failed to compare with golden file")

			if len(diffs) > 0 {
				diffMsg := fmt.Sprintf("Build for %s differs from golden file:\n", bcName)
				for _, diff := range diffs {
					diffMsg += fmt.Sprintf("  - %s\n", diff)
				}
				Fail(diffMsg)
			}
			log.Printf("✓ Build for %s matches golden file perfectly\n", bcName)

			By("Apply rendered manifests to target cluster")
			log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
			Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

			By(fmt.Sprintf("Verify %s Build exists on target cluster", bcName))
			out, err = kubectlTgtNonAdmin.Run("get", "build", bcName, "-n", namespace, "-o", "jsonpath={.kind}")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("Build"))
			log.Printf("Build %s exists on target cluster\n", bcName)

			By("Test completed successfully - BuildConfig converted to Shipwright Build")
			log.Printf("MTA-819: %s BuildConfig to Shipwright conversion validated\n", bcName)
		},

		// Test cases - one entry per BuildConfig
		Entry("Git + Docker + buildArgs", "webapp-docker", "Git source with Docker strategy and buildArgs"),
		Entry("Git + S2I + env vars", "api-s2i", "Git source with Source (S2I) strategy and environment variables"),
		Entry("Dockerfile + Docker + env vars", "docker-envvars", "Inline Dockerfile with Docker strategy and environment variables"),
	)
})
