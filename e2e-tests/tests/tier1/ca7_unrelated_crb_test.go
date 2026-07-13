package e2e

import (
	"log"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster-level export filtering", func() {
	It("[CA-7] Should not export CRB with subject from another namespace", Label("cluster-admin"), func() {
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
		srcApp := scenario.SrcApp
		tgtApp := scenario.TgtApp
		kubectlSrc := scenario.KubectlSrc
		kubectlTgt := scenario.KubectlTgt
		runner := scenario.Crane
		paths, err := NewScenarioPaths("crane-ca7-*")
		Expect(err).NotTo(HaveOccurred())

		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}

		cr := ClusterRole{Name: "crane-cr", Verb: "get,list,watch", Resource: "pods", Label: "app=" + appName}
		unrelatedNamespace := Namespace{Name: "unrelated-name-space"}

		unrelatedSA := ServiceAccount{Name: "unrelated-nginx-sa", Namespace: unrelatedNamespace.Name}
		unrelatedSubject := "--serviceaccount=" + unrelatedNamespace.Name + ":" + unrelatedSA.Name
		unrelatedCRB := ClusterRoleBinding{Name: "unrelated-crb", ClusterRoleName: cr.Name, Subject: unrelatedSubject}

		relatedSa := ServiceAccount{Name: "nginx-sa", Namespace: namespace}
		testSubject := "--serviceaccount=" + namespace + ":" + relatedSa.Name
		testCRB := ClusterRoleBinding{Name: "test-crb", ClusterRoleName: cr.Name, Subject: testSubject}

		DeferCleanup(func() {
			if err := ResourceCleanup([]KubectlRunner{kubectlSrc, kubectlTgt}, []Resource{
				cr, unrelatedSA, unrelatedCRB, relatedSa, testCRB, unrelatedNamespace}); err != nil {
				log.Printf("Resources cleanup: %v", err)
			}
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("Scenario cleanup: %v", err)
			}
		})

		By("Deploying app with ServiceAccount on source cluster")
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRole on source")
		Expect(cr.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating unrelated namespace on source")
		Expect(unrelatedNamespace.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ServiceAccount in unrelated namespace")
		Expect(unrelatedSA.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRoleBinding referencing foreign namespace ServiceAccount")
		Expect(unrelatedCRB.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating related ServiceAccount in app namespace")
		Expect(relatedSa.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRoleBinding referencing app's ServiceAccount")
		Expect(testCRB.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Waiting for source pods and endpoints to drain")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		By("Running crane export, transform, apply")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verifying out-of-scope resources are not in export _cluster directory")
		exportClusterPath := filepath.Join(paths.ExportDir, "resources", namespace, "_cluster")
		found, err := utils.AssertResourcesExist(exportClusterPath, []utils.ResourceMatch{
			{Kind: "ClusterRoleBinding", Name: unrelatedCRB.Name},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeFalse())

		By("Verifying linked ClusterRole and ClusterRoleBinding exist in export, transform, and output")
		found, err = utils.AssertResourcesExist(exportClusterPath, []utils.ResourceMatch{
			{Kind: "ClusterRoleBinding", Name: testCRB.Name},
			{Kind: "ClusterRole", Name: cr.Name},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
	})
})
