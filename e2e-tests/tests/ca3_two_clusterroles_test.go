package e2e

import (
	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster-level RBAC export", func() {
	It("[CA-3] Should export two ClusterRoles and two ClusterRoleBindings for one Deployment", Label("cluster-admin"), func() {
		appName := "nginx-with-serviceaccount"
		namespace := "simple-nginx-nopv"
		serviceName := "my-" + appName
		saName := "nginx-sa"
		readClusterRole := "crane-e2e-pod-reader"
		writeClusterRole := "crane-e2e-pod-writer"
		readClusterRoleBindingName := "reader-crane-e2e-pod-binding"
		writeClusterRoleBindingName := "writer-crane-e2e-pod-binding"
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
		paths, err := NewScenarioPaths("crane-ca3-*")
		runner := scenario.Crane
		resourcesPatterns := []string{"ClusterRole_*.yaml", "ClusterRoleBinding_*.yaml"}
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			ClusterResourceCleanup(kubectlSrc, kubectlTgt, []ClusterResource{
				{Kind: "clusterrolebinding", Name: readClusterRoleBindingName},
				{Kind: "clusterrolebinding", Name: writeClusterRoleBindingName},
				{Kind: "clusterrole", Name: readClusterRole},
				{Kind: "clusterrole", Name: writeClusterRole},
			})
		})

		DeferCleanup(func() {
			ScenarioCleanup(paths, srcApp, tgtApp, kubectlSrc, kubectlTgt, namespace)
		})

		By("Prepare source app")
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())

		By("Create Read ClusterRole on source")
		crErr := CrateCrAndValidate(kubectlSrc, "read", readClusterRole)
		Expect(crErr).NotTo(HaveOccurred())

		By("Create Write ClusterRole on source")
		crErr = CrateCrAndValidate(kubectlSrc, "write", writeClusterRole)
		Expect(crErr).NotTo(HaveOccurred())

		By("Create the read ClusterRoleBinding on source")
		crbErr := CrateAndValidateCrb(kubectlSrc, namespace, readClusterRoleBindingName, readClusterRole, &saName)
		Expect(crbErr).NotTo(HaveOccurred())

		By("Create the write ClusterRoleBinding on source")
		crbErr = CrateAndValidateCrb(kubectlSrc, namespace, writeClusterRoleBindingName, writeClusterRole, &saName)
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
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale and validate target app")
		ScaleAndValidateTargetApp(kubectlTgt, tgtApp, namespace, appName)

		By("Verify cluster RBAC on target")
		Expect(ValidateClusterRBAC(kubectlTgt, namespace, []ExpectedClusterRoleBinding{
			{ClusterRoleBindingName: readClusterRoleBindingName, ClusterRoleName: readClusterRole, SubjectName: saName},
			{ClusterRoleBindingName: writeClusterRoleBindingName, ClusterRoleName: writeClusterRole, SubjectName: saName},
		})).NotTo(HaveOccurred())
	})
})
