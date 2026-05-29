package e2e

import (
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Namespace-admin cluster-level migration", func() {
	It("[NA-1] Should migrate workload with split apply: namespace-admin + cluster-admin", Label("namespace-admin"), func() {
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
		forbiddenResourcesPatterns := []string{"ClusterRole_*.yaml", "ClusterRoleBinding_*.yaml"}
		if scenario.KubectlSrcNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin role migration test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin role migration test")
		}
		srcAppNonAdmin, tgtAppNonAdmin := NonAdminApps(scenario)
		kubectlSrc := scenario.KubectlSrc
		kubectlTgt := scenario.KubectlTgt
		paths, err := NewScenarioPaths("crane-na1-*")
		runner := scenario.CraneNonAdmin
		Expect(err).NotTo(HaveOccurred())

		By("Grant namespace-admin permissions to non-admin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, rbacCleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(rbacCleanup)
		DeferCleanup(func() {
			ClusterResourceCleanup(kubectlSrc, kubectlTgt, []ClusterResource{
				{Kind: "clusterrolebinding", Name: clusterRoleBindingName},
				{Kind: "clusterrole", Name: clusterRoleName},
			})
		})
		DeferCleanup(func() {
			ScenarioCleanup(paths, srcAppNonAdmin, tgtAppNonAdmin, kubectlSrc, kubectlTgt, namespace)
		})

		By("Prepare source app")
		err = PrepareSourceApp(srcAppNonAdmin, kubectlSrcNonAdmin)
		Expect(err).NotTo(HaveOccurred())

		By("Create ClusterRole on source")
		Expect(CrateCrAndValidate(kubectlSrc, "read", clusterRoleName)).NotTo(HaveOccurred())

		By("Create ClusterRoleBinding on source")
		crbErr := CrateAndValidateCrb(kubectlSrc, namespace, clusterRoleBindingName, clusterRoleName, nil)
		Expect(crbErr).NotTo(HaveOccurred())

		By("Wait for source quiesce")
		WaitForSourceQuiesce(kubectlSrcNonAdmin, namespace, "app="+appName, serviceName)

		By("Running Crane Pipeline")
		Expect(RunPipeline(&runner, namespace, paths)).NotTo(HaveOccurred())

		By("Verify no export failures")
		err = ValidateDirResources(filepath.Join(paths.ExportDir, "failures"), forbiddenResourcesPatterns)
		//as namespace admin we dont have privilages to export cluster resource.
		//on that scenario export will create files on the failure folder stating what kind of resource didnt
		Expect(err).To(HaveOccurred())

		By("Apply namespace resources to target as namespace-admin")
		Expect(NonAdminApplyOutput(kubectlTgtNonAdmin, paths.OutputDir, namespace)).NotTo(HaveOccurred())

		By("Verify no cluster resources in present after the Namespace admin Applied")
		Expect(AssertNoClusterResources(filepath.Join(paths.OutputDir, "resources", "_cluster"))).NotTo(HaveOccurred())

		By("Apply cluster resources to target as cluster-admin")
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale and validate target app")
		ScaleAndValidateTargetApp(kubectlTgtNonAdmin, tgtAppNonAdmin, namespace, appName)

	})

})
