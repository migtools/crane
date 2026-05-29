package e2e

import (
	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster-level RBAC export", func() {
	It("[CA-1] Should export ClusterRole and ClusterRoleBinding for linked ServiceAccount", Label("cluster-admin"), func() {
		appName := "nginx-with-serviceaccount"
		namespace := "simple-nginx-nopv"
		serviceName := "my-" + appName
		saName := "nginx-sa"
		clusterRoleBindingName := "crane-e2e-pod-reader-binding"
		clusterRoleName := "crane-e2e-pod-reader"
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		srcApp := scenario.SrcApp
		tgtApp := scenario.TgtApp
		kubectlSrc := scenario.KubectlSrc
		kubectlTgt := scenario.KubectlTgt
		runner := scenario.Crane
		paths, err := NewScenarioPaths("crane-ca1-*")
		resourcesPatterns := []string{"ClusterRole_*.yaml", "ClusterRoleBinding_*.yaml"}
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			ClusterResourceCleanup(kubectlSrc, kubectlTgt, []ClusterResource{
				{Kind: "clusterrolebinding", Name: clusterRoleBindingName},
				{Kind: "clusterrole", Name: clusterRoleName},
			})
		})

		DeferCleanup(func() {
			ScenarioCleanup(paths, srcApp, tgtApp, kubectlSrc, kubectlTgt, namespace)
		})

		By("Prepare source app")
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())

		By("Create ClusterRole on source")
		_, err = scenario.KubectlSrc.Run("create", "clusterrole", clusterRoleName, "--verb=get,list,watch", "--resource=pods")
		Expect(err).NotTo(HaveOccurred())

		By("Create ClusterRoleBinding on source")
		crbErr := CrateAndValidateCrb(kubectlSrc, namespace, clusterRoleBindingName, clusterRoleName, &saName)
		Expect(crbErr).NotTo(HaveOccurred())

		By("Wait for source quiesce")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		By("Running Crane Pipeline")
		Expect(RunPipeline(&runner, namespace, paths)).NotTo(HaveOccurred())

		By("Verify no export failures")
		Expect(AssertNoExportFailures(paths.ExportDir, namespace)).NotTo(HaveOccurred())

		By("Validate cluster resources across pipeline stages")
		Expect(ValidatePipelineClusterResources(paths, namespace, resourcesPatterns, nil)).NotTo(HaveOccurred())

		By("Create namespace on tgt and Dry-run validate full output")
		_, err = kubectlTgt.Run("create", "namespace", namespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(kubectlTgt.ValidateApplyDir(paths.OutputDir)).NotTo(HaveOccurred())

		By("Apply output to target")
		validateErr := ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)
		Expect(validateErr).NotTo(HaveOccurred())

		By("Scale and validate target app")
		ScaleAndValidateTargetApp(kubectlTgt, tgtApp, namespace, appName)

		By("Verify cluster RBAC on target")
		Expect(ValidateClusterRBAC(kubectlTgt, namespace, []ExpectedClusterRoleBinding{
			{ClusterRoleBindingName: clusterRoleBindingName, ClusterRoleName: clusterRoleName, SubjectName: saName},
		})).NotTo(HaveOccurred())
	})

})
