package e2e

import (
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
)

var _ = Describe("Namespace-admin cluster-level migration", func() {
	It("[MTA-853] Should migrate workload with split apply: namespace-admin + cluster-admin", Label("namespace-admin"), func() {
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
		clusterRoleBindingName := "crane-e2e-pod-reader-binding"
		clusterRoleName := "crane-e2e-pod-reader"
		deniedResources := []string{"clusterroles.yaml", "clusterrolebindings.yaml"}
		if scenario.KubectlSrcNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin role migration test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin role migration test")
		}
		srcAppNonAdmin := scenario.SrcAppNonAdmin
		tgtAppNonAdmin := scenario.TgtAppNonAdmin

		srcAppNonAdmin.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtAppNonAdmin.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		kubectlSrc := scenario.KubectlSrc
		kubectlTgt := scenario.KubectlTgt
		paths, err := NewScenarioPaths("crane-na1-*")
		runner := scenario.CraneNonAdmin

		exportOpts := ExportOptions{Namespace: srcAppNonAdmin.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}
		Expect(err).NotTo(HaveOccurred())

		crb := ClusterRoleBinding{Name: clusterRoleBindingName, ClusterRoleName: clusterRoleName}
		cr := ClusterRole{Name: clusterRoleName, Verb: "get,list,watch", Resource: "pods"}

		By("Granting namespace-admin permissions to non-admin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, rbacCleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(rbacCleanup)
		DeferCleanup(func() {
			ResourceCleanup([]KubectlRunner{kubectlSrc, kubectlTgt}, []Resource{crb, cr})
			CleanupScenario(paths.TempDir, srcAppNonAdmin, tgtAppNonAdmin)

		})

		By("Deploying app as namespace-admin on source cluster")
		err = PrepareSourceApp(srcAppNonAdmin, kubectlSrcNonAdmin)
		Expect(err).NotTo(HaveOccurred())

		By("Creating ClusterRole")
		Expect(cr.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRoleBinding")
		Expect(crb.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Waiting for source pods and endpoints to drain")
		WaitForSourceQuiesce(kubectlSrcNonAdmin, namespace, "app="+appName, serviceName)

		By("Running crane export, transform, apply as namespace-admin")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verifying cluster resources failed to export (expected for namespace-admin)")
		Expect(utils.AssertFilesExist(filepath.Join(paths.ExportDir, "failures"), deniedResources)).To(HaveOccurred())

		By("Verifying no cluster resources in output _cluster directory")
		Expect(utils.AssertNoKindsInOutput(paths.OutputDir, []string{"ClusterRole", "ClusterRoleBinding"})).NotTo(HaveOccurred())

		By("Applying namespace resources to target as namespace-admin")
		Expect(kubectlTgt.ApplyDir(filepath.Join(paths.OutputDir, "resources", namespace))).NotTo(HaveOccurred())

		By("Scaling target deployment and validating app")
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
		Eventually(tgtAppNonAdmin.Validate, "2m", "10s").Should(gomega.Succeed())

	})

})
