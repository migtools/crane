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
	It("[MTA-819] Converted Shipwright Build runs successfully end-to-end - Docker strategy (Git source)", func() {
		appName := "buildconfig-docker-git"
		namespace := "buildconfig-docker-git"
		buildConfigName := "webapp-docker"

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

		By("Deploy buildconfig-docker-git app (Docker strategy with Git source)")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		By("Verify BuildConfig exists on source cluster")
		out, err := kubectlSrcNonAdmin.Run("get", "buildconfig", buildConfigName, "-n", namespace, "-o", "name")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(ContainSubstring(fmt.Sprintf("buildconfig.build.openshift.io/%s", buildConfigName)))
		log.Printf("✓ BuildConfig %s exists (Docker strategy with Git source)\n", buildConfigName)

		By("Run crane export/transform/apply pipeline with BuildConfig plugin")
		exportOpts := ExportOptions{
			Namespace: namespace,
			ExportDir: paths.ExportDir,
		}
		transformOpts := TransformOptions{
			ExportDir:     paths.ExportDir,
			TransformDir:  paths.TransformDir,
			PluginDir:     config.PluginDir,
			OptionalFlags: `{"insecure-registries":"image-registry.openshift-image-registry.svc:5000"}`,
		}
		applyOpts := ApplyOptions{
			TransformDir: paths.TransformDir,
			OutputDir:    paths.OutputDir,
		}

		runner.WorkDir = paths.TempDir
		log.Printf("Running crane pipeline for namespace %s with plugin directory: %s\n", namespace, config.PluginDir)
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed - BuildConfig converted successfully\n")

		By("Compare generated Build against golden file")
		actualBuildPath := filepath.Join(paths.OutputDir, "resources", namespace, fmt.Sprintf("Build_shipwright.io_v1beta1_%s_%s.yaml", namespace, buildConfigName))
		goldenFilePath := filepath.Join("../../testdata/buildconfig-docker-git/golden", "webapp-docker-golden.yaml")

		diffs, err := CompareWithGoldenFile(actualBuildPath, goldenFilePath)
		Expect(err).NotTo(HaveOccurred(), "Failed to compare Build with golden file")

		if len(diffs) > 0 {
			diffMsg := fmt.Sprintf("Build for %s differs from golden file:\n", buildConfigName)
			for _, diff := range diffs {
				diffMsg += fmt.Sprintf("  - %s\n", diff)
			}
			Fail(diffMsg)
		}
		log.Printf("✓ Build matches golden file perfectly\n")

		By("Apply rendered manifests to target cluster")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Verify Build exists on target cluster")
		out, err = kubectlTgtNonAdmin.Run("get", "build.shipwright.io", buildConfigName, "-n", namespace, "-o", "jsonpath={.kind}")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal("Build"))
		log.Printf("✓ Build %s exists on target cluster\n", buildConfigName)

		By("Create BuildRun to verify end-to-end execution")
		buildRunName := fmt.Sprintf("%s-run-1", buildConfigName)
		buildRunYAML := fmt.Sprintf(`apiVersion: shipwright.io/v1beta1
kind: BuildRun
metadata:
  name: %s
  namespace: %s
spec:
  build:
    name: %s
  serviceAccount: builder
`, buildRunName, namespace, buildConfigName)

		log.Printf("Creating BuildRun %s for Build %s\n", buildRunName, buildConfigName)
		_, err = kubectlTgtNonAdmin.RunWithStdin(buildRunYAML, "apply", "-f", "-", "-n", namespace, "--validate=false")
		Expect(err).NotTo(HaveOccurred())

		By("Wait for BuildRun to complete")
		log.Printf("Waiting for BuildRun %s to complete (timeout: 5 minutes)...\n", buildRunName)

		Eventually(func() bool {
			out, err := kubectlTgtNonAdmin.Run("get", "buildrun", buildRunName, "-n", namespace, "-o", "jsonpath={.status.conditions[?(@.type=='Succeeded')].status}")
			if err != nil {
				log.Printf("BuildRun %s not ready yet: %v\n", buildRunName, err)
				return false
			}
			if out == "True" {
				log.Printf("✓ BuildRun %s completed successfully\n", buildRunName)
				return true
			}
			reason, _ := kubectlTgtNonAdmin.Run("get", "buildrun", buildRunName, "-n", namespace, "-o", "jsonpath={.status.conditions[?(@.type=='Succeeded')].reason}")
			if reason != "" && reason != "Running" && reason != "Pending" {
				log.Printf("BuildRun %s status: %s (reason: %s)\n", buildRunName, out, reason)
			}
			return false
		}, "5m", "10s").Should(BeTrue(), fmt.Sprintf("BuildRun %s should complete within 5 minutes", buildRunName))

		By("Verify BuildRun succeeded")
		out, err = kubectlTgtNonAdmin.Run("get", "buildrun", buildRunName, "-n", namespace, "-o", "jsonpath={.status.conditions[?(@.type=='Succeeded')].status}")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal("True"), fmt.Sprintf("BuildRun %s should have Succeeded=True", buildRunName))

		reason, err := kubectlTgtNonAdmin.Run("get", "buildrun", buildRunName, "-n", namespace, "-o", "jsonpath={.status.conditions[?(@.type=='Succeeded')].reason}")
		Expect(err).NotTo(HaveOccurred())
		log.Printf("✓ BuildRun %s completed with status: Succeeded=True, Reason=%s\n", buildRunName, reason)

		outputImage, _ := kubectlTgtNonAdmin.Run("get", "buildrun", buildRunName, "-n", namespace, "-o", "jsonpath={.status.output.digest}")
		if outputImage != "" {
			log.Printf("  Image digest: %s\n", outputImage)
		}

		By("Test completed successfully - BuildConfig converted and BuildRun executed end-to-end")
		log.Printf("MTA-819: Successfully converted and executed Docker BuildConfig (Git source) end-to-end\n")
	})
})
