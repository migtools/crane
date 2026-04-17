package e2e

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ConfigMap Migration", func() {
	It("[MTC-291] Should migrate a configmap with data intact as nonadmin user", Label("tier0"), func() {
		appName := "configmap"
		namespace := appName
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)

		if scenario.KubectlSrcNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin stateless migration test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin stateless migration test")
		}
		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin

		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		By("Grant ns admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Delete test namespace on source and target (best effort)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=false"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(cleanup)
		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-export-*")
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

		By("Verify the output directory ConfigMap has the correct data")
		pattern := filepath.Join(paths.OutputDir, "resources", namespace, "ConfigMap_*.yaml")
		matches, err := filepath.Glob(pattern)
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).NotTo(BeEmpty(), "expected at least one ConfigMap manifest")

		expectedData := `redis-config: "maxmemory 2mb  \nmaxmemory-policy allkeys-lru\n"`
		foundExpectedConfigMap := false
		for _, match := range matches {
			content, err := os.ReadFile(match)
			Expect(err).NotTo(HaveOccurred())
			if strings.Contains(string(content), expectedData) {
				foundExpectedConfigMap = true
			}
		}
		Expect(foundExpectedConfigMap).To(BeTrue(), "expected at least one ConfigMap manifest to contain redis-config data")
		log.Printf("Verified output dir contains ConfigMap manifest with redis-config data")
		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Validate the app on target")
		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())

		By("Validate the configmap is present on target and contains expected data")
		output, err := kubectlTgtNonAdmin.Run("get", "configmap", "redis-config", "-n", namespace, "-o", "yaml")
		Expect(err).NotTo(HaveOccurred())
		Expect(output).To(ContainSubstring(expectedData))
		log.Printf("Configmap redis-config is present on target and contains expected data\n")
	})
})
