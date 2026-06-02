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

var _ = Describe("Validate alternative GV suggestion [Live Mode]", func() {
	It("[MTA-829] should suggest apps/v1 for extensions/v1beta1 Deployment as namespace-admin", Label("tier1"), func() {
		appName := "simple-nginx-nopv"
		namespace := "simple-nginx-nopv-alt-gv"

		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)

		if scenario.SrcAppNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin stateless migration test")
		}
		if scenario.TgtAppNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin stateless migration test")
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
		paths, err := NewScenarioPaths("crane-pipeline-*")
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

		By("Mutate deployment apiversion to extensions/v1beta1")
		deploymentPattern := filepath.Join(paths.OutputDir, "resources", namespace, "Deployment_*.yaml")
		matches, err := filepath.Glob(deploymentPattern)
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).NotTo(BeEmpty(), "expected at least one Deployment manifest in output")

		deploymentPath := matches[0]
		deploymentBytes, err := os.ReadFile(deploymentPath)
		Expect(err).NotTo(HaveOccurred())

		mutatedDeployment := strings.Replace(string(deploymentBytes), "apiVersion: apps/v1", "apiVersion: extensions/v1beta1", 1) // Replace apiVersion
		Expect(mutatedDeployment).NotTo(Equal(string(deploymentBytes)), "expected to replace Deployment apiVersion")
		Expect(os.WriteFile(deploymentPath, []byte(mutatedDeployment), 0o644)).NotTo(HaveOccurred())

		By("Run crane validate against target context")
		stdout, err := runner.Validate(ValidateOptions{
			Context:     scenario.KubectlTgtNonAdmin.Context,
			InputDir:    filepath.Join(paths.OutputDir, "resources", namespace),
			ValidateDir: paths.ValidateDir,
		})

		Expect(err).To(HaveOccurred(), "validate should fail for deprecated Deployment apiVersion")
		Expect(stdout).To(ContainSubstring("available as apps/v1"))

		By("Assert report.json includes incompatible deployment with suggestion")
		reportPath := filepath.Join(paths.ValidateDir, "report.json")
		reportBytes, err := os.ReadFile(reportPath)
		Expect(err).NotTo(HaveOccurred())
		report := string(reportBytes)
		Expect(report).To(ContainSubstring(`"apiVersion": "extensions/v1beta1"`))
		Expect(report).To(ContainSubstring(`"kind": "Deployment"`))
		Expect(report).To(ContainSubstring(`"status": "Incompatible"`))
		Expect(report).To(ContainSubstring(`"suggestion": "available as apps/v1"`))

		By("Assert failures directory contains deployment failure with suggestion")
		failurePattern := filepath.Join(paths.ValidateDir, "failures", "Deployment_extensions_v1beta1_*.yaml")
		failureMatches, err := filepath.Glob(failurePattern)
		Expect(err).NotTo(HaveOccurred())
		Expect(failureMatches).NotTo(BeEmpty(), "expected at least one validation failure file")

		failureBytes, err := os.ReadFile(failureMatches[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(string(failureBytes)).To(ContainSubstring("suggestion: available as apps/v1"))
	})
})
