package e2e

import (
	"log"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Namespace-admin cluster-level migration", func() {
	It("[MTA-866] Should migrate namespace-only workload as namespace-admin without split apply", Label("tier0"), func() {
		appName := "simple-nginx-nopv"
		namespace := "simple-nginx-nopv"
		serviceName := "my-" + appName
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		srcAppNonAdmin := scenario.SrcAppNonAdmin
		tgtAppNonAdmin := scenario.TgtAppNonAdmin

		srcAppNonAdmin.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtAppNonAdmin.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		runner := scenario.CraneNonAdmin
		paths, err := NewScenarioPaths("crane-*")
		Expect(err).NotTo(HaveOccurred())

		exportOpts := ExportOptions{Namespace: srcAppNonAdmin.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}

		By("Granting namespace-admin permissions to non-admin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, rbacCleanup, err := SetupActiveKubectlRunners(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			if err := CleanupScenario(paths.TempDir, srcAppNonAdmin, tgtAppNonAdmin); err != nil {
				log.Printf("Scenario cleanup: %v", err)
			}
		})

		DeferCleanup(rbacCleanup)

		By("Deploying namespace-only app as namespace-admin on source cluster")
		err = PrepareSourceApp(srcAppNonAdmin, kubectlSrcNonAdmin)
		Expect(err).NotTo(HaveOccurred())

		By("Waiting for source pods and endpoints to drain")
		WaitForSourceQuiesce(kubectlSrcNonAdmin, namespace, "app="+appName, serviceName)

		By("Running crane export, transform, apply as namespace-admin")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verifying no cluster resources was migrated")
		clusterDir := filepath.Join(paths.OutputDir, "resources", "_cluster")
		//no cluster resources-> no _cluster folder created ->we expect an error.
		Expect(clusterDir).NotTo(BeADirectory())

		By("Applying namespace resources to target as namespace-admin")
		Expect(kubectlTgtNonAdmin.ApplyDir(paths.OutputDir)).NotTo(HaveOccurred())

		By("Scaling target deployment and validating app")
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
		Eventually(tgtAppNonAdmin.Validate, "2m", "10s").Should(Succeed())

	})

})
