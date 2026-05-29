package e2e

import (
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster-level export filtering", func() {
	It("[CA-6] Should not export orphan ClusterRole with no CRB linking it to exported SAs", Label("cluster-admin"), func() {
		appName := "nginx-with-serviceaccount"
		namespace := "simple-nginx-nopv"
		serviceName := "my-" + appName
		saName := "nginx-sa"
		readClusterRole := "crane-e2e-pod-reader"
		writeClusterRole := "crane-e2e-pod-writer"
		readClusterRoleBindingName := "reader-crane-e2e-pod-binding"
		relatedResources := []string{"ClusterRole_*" + readClusterRole + "*.yaml", "ClusterRoleBinding_*" +
			readClusterRoleBindingName + "*.yaml"}
		orphanResource := []string{"ClusterRole_" + writeClusterRole}
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
		paths, err := NewScenarioPaths("crane-ca6-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			ClusterResourceCleanup(kubectlSrc, kubectlTgt, []ClusterResource{
				{Kind: "clusterrolebinding", Name: readClusterRoleBindingName},
				{Kind: "clusterrole", Name: readClusterRole},
				{Kind: "clusterrole", Name: writeClusterRole},
			})
		})

		DeferCleanup(func() {
			ScenarioCleanup(paths, srcApp, tgtApp, kubectlSrc, kubectlTgt, namespace)
		})

		By("Prepare source app")
		prepareSrcErr := PrepareSourceApp(srcApp, kubectlSrc)
		Expect(prepareSrcErr).NotTo(HaveOccurred())

		By("Create Read ClusterRole on source")
		Expect(CrateCrAndValidate(kubectlSrc, "read", readClusterRole)).NotTo(HaveOccurred())

		By("Create Write ClusterRole on source")
		Expect(CrateCrAndValidate(kubectlSrc, "write", writeClusterRole)).NotTo(HaveOccurred())

		By("Create the read ClusterRoleBinding on source")
		crbErr := CrateAndValidateCrb(kubectlSrc, namespace, readClusterRoleBindingName, readClusterRole, &saName)
		Expect(crbErr).NotTo(HaveOccurred())

		By("Wait for source quiesce")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		By("Running Crane Pipeline")
		Expect(RunPipeline(&runner, namespace, paths)).NotTo(HaveOccurred())

		By("Verify that Orphan ClusterRole Failed to be exported")
		exportClusterPath := filepath.Join(paths.ExportDir, "resources", namespace, "_cluster")
		//we dont expect to find the orphan clusterRole on the export dir.
		Expect(ValidateDirResources(exportClusterPath, orphanResource)).To(HaveOccurred())

		By("Verify that Realted ClusterRole being be exported across all stages")
		Expect(ValidatePipelineClusterResources(paths, namespace, relatedResources, nil)).NotTo(HaveOccurred())
	})
})
