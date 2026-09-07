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
	It("[MTA-819] Converted Shipwright Build runs successfully end-to-end", func() {
		appName := "buildconfig-test"
		namespace := "buildconfig-test"

		// BuildConfigs to validate in this single migration
		buildConfigs := []struct {
			name        string
			description string
		}{
			{"webapp-docker", "Git source with Docker strategy and buildArgs"},
			{"api-s2i", "Git source with Source (S2I) strategy and environment variables"},
			{"docker-envvars", "Inline Dockerfile with Docker strategy and environment variables"},
		}

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

		By("Deploy buildconfig-test app (contains 3 BuildConfigs: Git+Docker, S2I, Dockerfile)")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully (all BuildConfigs deployed)\n", srcApp.Name)

		By("Verify all BuildConfigs exist on source cluster")
		for _, bc := range buildConfigs {
			out, err := kubectlSrcNonAdmin.Run("get", "buildconfig", bc.name, "-n", namespace, "-o", "name")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(ContainSubstring(fmt.Sprintf("buildconfig.build.openshift.io/%s", bc.name)))
			log.Printf("BuildConfig %s exists (%s)\n", bc.name, bc.description)
		}

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
		log.Printf("Crane pipeline completed - all BuildConfigs converted in single migration\n")

		By("Compare all generated Builds against golden files")
		buildFilePattern := filepath.Join(paths.OutputDir, "resources", namespace, "Build_shipwright.io_*.yaml")
		buildMatches, err := filepath.Glob(buildFilePattern)
		Expect(err).NotTo(HaveOccurred())
		Expect(buildMatches).To(HaveLen(3), "Expected 3 Build YAML files to be generated")

		for _, bc := range buildConfigs {
			// Find the specific Build for this BuildConfig
			var actualBuildPath string
			for _, buildPath := range buildMatches {
				if strings.Contains(buildPath, bc.name) {
					actualBuildPath = buildPath
					break
				}
			}
			Expect(actualBuildPath).NotTo(BeEmpty(), fmt.Sprintf("Build YAML for %s should be generated", bc.name))

			// Golden file path (relative to tier0 test directory - go up 2 levels to crane root)
			goldenFilePath := filepath.Join("../../testdata/buildconfig-test/golden", fmt.Sprintf("%s-golden.yaml", bc.name))

			// Compare with golden file
			diffs, err := CompareWithGoldenFile(actualBuildPath, goldenFilePath)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("Failed to compare %s with golden file", bc.name))

			if len(diffs) > 0 {
				diffMsg := fmt.Sprintf("Build for %s differs from golden file:\n", bc.name)
				for _, diff := range diffs {
					diffMsg += fmt.Sprintf("  - %s\n", diff)
				}
				Fail(diffMsg)
			}
			log.Printf("✓ Build for %s matches golden file perfectly\n", bc.name)
		}

		By("Apply rendered manifests to target cluster")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Verify all Builds exist on target cluster")
		for _, bc := range buildConfigs {
			out, err := kubectlTgtNonAdmin.Run("get", "build.shipwright.io", bc.name, "-n", namespace, "-o", "jsonpath={.kind}")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("Build"))
			log.Printf("✓ Build %s exists on target cluster\n", bc.name)
		}

		By("Create BuildRun for each Build to verify end-to-end execution")
		buildRunNames := make(map[string]string)
		for _, bc := range buildConfigs {
			buildRunName := fmt.Sprintf("%s-run-1", bc.name)
			buildRunNames[bc.name] = buildRunName

			buildRunYAML := fmt.Sprintf(`apiVersion: shipwright.io/v1beta1
kind: BuildRun
metadata:
  name: %s
  namespace: %s
spec:
  build:
    name: %s
`, buildRunName, namespace, bc.name)

			log.Printf("Creating BuildRun %s for Build %s\n", buildRunName, bc.name)
			err := kubectlTgtNonAdmin.ApplyYAMLSpec(buildRunYAML, namespace)
			Expect(err).NotTo(HaveOccurred())
		}

		By("Wait for all BuildRuns to complete")
		for bcName, buildRunName := range buildRunNames {
			log.Printf("Waiting for BuildRun %s to complete (timeout: 5 minutes)...\n", buildRunName)

			// Wait for BuildRun to complete (either Succeeded or Failed)
			// Use kubectl wait with timeout
			Eventually(func() bool {
				out, err := kubectlTgtNonAdmin.Run("get", "buildrun", buildRunName, "-n", namespace, "-o", "jsonpath={.status.conditions[?(@.type=='Succeeded')].status}")
				if err != nil {
					log.Printf("BuildRun %s not ready yet: %v\n", buildRunName, err)
					return false
				}
				// Check if Succeeded condition is True
				if out == "True" {
					log.Printf("✓ BuildRun %s completed successfully\n", buildRunName)
					return true
				}
				// Check if it failed
				reason, _ := kubectlTgtNonAdmin.Run("get", "buildrun", buildRunName, "-n", namespace, "-o", "jsonpath={.status.conditions[?(@.type=='Succeeded')].reason}")
				if reason != "" && reason != "Running" && reason != "Pending" {
					log.Printf("BuildRun %s status: %s (reason: %s)\n", buildRunName, out, reason)
				}
				return false
			}, "5m", "10s").Should(BeTrue(), fmt.Sprintf("BuildRun %s should complete within 5 minutes", buildRunName))

			// Verify the BuildRun succeeded
			out, err := kubectlTgtNonAdmin.Run("get", "buildrun", buildRunName, "-n", namespace, "-o", "jsonpath={.status.conditions[?(@.type=='Succeeded')].status}")
			Expect(err).NotTo(HaveOccurred())
			Expect(out).To(Equal("True"), fmt.Sprintf("BuildRun %s should have Succeeded=True", buildRunName))

			reason, err := kubectlTgtNonAdmin.Run("get", "buildrun", buildRunName, "-n", namespace, "-o", "jsonpath={.status.conditions[?(@.type=='Succeeded')].reason}")
			Expect(err).NotTo(HaveOccurred())
			log.Printf("✓ BuildRun %s completed with status: Succeeded=True, Reason=%s\n", buildRunName, reason)

			// Log the output image
			outputImage, _ := kubectlTgtNonAdmin.Run("get", "buildrun", buildRunName, "-n", namespace, "-o", "jsonpath={.status.output.digest}")
			if outputImage != "" {
				log.Printf("  Image digest for %s: %s\n", bcName, outputImage)
			}
		}

		By("Test completed successfully - All BuildConfigs converted and BuildRuns executed end-to-end")
		log.Printf("MTA-819: Successfully converted and executed 3 BuildConfig types (Git+Docker, S2I, Dockerfile) end-to-end\n")
	})
})
