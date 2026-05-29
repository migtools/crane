package e2e

import (
	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster-level RBAC export", func() {
	It("[CA-2] Should export ClusterRole and ClusterRoleBinding for linked ServiceAccount", Label("cluster-admin"), func() {
		appName := "nginx-with-serviceaccount"
		namespace := "simple-nginx-nopv"
		serviceName := "my-" + appName
		firstSa := "nginx-sa"
		secondSa := "nginx-sa-2"
		clusterRoleName := "crane-e2e-pod-reader"
		firstClusterRoleBindingName := "crane-e2e-pod-reader-binding-1"
		secondClusterRoleBindingName := "crane-e2e-pod-reader-binding-2"
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
		paths, err := NewScenarioPaths("crane-ca2-*")

		resourcesPatterns := []string{"ClusterRole_*.yaml", "ClusterRoleBinding_*.yaml"}
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			ClusterResourceCleanup(kubectlSrc, kubectlTgt, []ClusterResource{
				{Kind: "clusterrolebinding", Name: firstClusterRoleBindingName},
				{Kind: "clusterrolebinding", Name: secondClusterRoleBindingName},
				{Kind: "clusterrole", Name: clusterRoleName},
			})
		})

		DeferCleanup(func() {
			ScenarioCleanup(paths, srcApp, tgtApp, kubectlSrc, kubectlTgt, namespace)
		})

		By("Prepare source app")
		prepareSrcErr := PrepareSourceApp(srcApp, kubectlSrc)
		Expect(prepareSrcErr).NotTo(HaveOccurred())

		By("Create ServiceAccount on source")
		_, err = kubectlSrc.Run("create", "serviceaccount", secondSa, "-n", namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Create ClusterRole on source")
		crErr := CrateCrAndValidate(kubectlSrc, "read", clusterRoleName)
		Expect(crErr).NotTo(HaveOccurred())

		By("Create the first ClusterRoleBinding on source")
		crbErr := CrateAndValidateCrb(kubectlSrc, namespace, firstClusterRoleBindingName, clusterRoleName, &firstSa)
		Expect(crbErr).NotTo(HaveOccurred())

		By("Create the second ClusterRoleBinding on source")
		crbErr = CrateAndValidateCrb(kubectlSrc, namespace, secondClusterRoleBindingName, clusterRoleName, &secondSa)
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
		Expect(CreateNamespaceAndDryRun(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		By("Apply output to target")
		validateErr := ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)
		Expect(validateErr).NotTo(HaveOccurred())

		By("Scale and validate target app")
		ScaleAndValidateTargetApp(kubectlTgt, tgtApp, namespace, appName)

		By("Verify cluster RBAC on target")
		Expect(ValidateClusterRBAC(kubectlTgt, namespace, []ExpectedClusterRoleBinding{
			{ClusterRoleBindingName: firstClusterRoleBindingName, ClusterRoleName: clusterRoleName, SubjectName: firstSa},
			{ClusterRoleBindingName: secondClusterRoleBindingName, ClusterRoleName: clusterRoleName, SubjectName: secondSa},
		})).NotTo(HaveOccurred())
	})
})
