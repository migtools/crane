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
		appName := "nginx-with-serviceaccount"
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
		forigenNamespace := Namespace{Name: "forigen-name-space"}

		foreignSA := ServiceAccount{Name: "forigen-nginx-sa", Namespace: forigenNamespace.Name}
		forigenSubject := "--serviceaccount=" + forigenNamespace.Name + ":" + foreignSA.Name
		foreignCRB := ClusterRoleBinding{Name: "forigen-crb", ClusterRoleName: cr.Name, Subject: forigenSubject}

		relatedSa := ServiceAccount{Name: "nginx-sa", Namespace: namespace}
		testSubject := "--serviceaccount=" + namespace + ":" + relatedSa.Name
		testCRB := ClusterRoleBinding{Name: "test-crb", ClusterRoleName: cr.Name, Subject: testSubject}

		DeferCleanup(func() {
			if err := ResourceCleanup([]KubectlRunner{kubectlSrc, kubectlTgt}, []Resource{
				cr, foreignSA, foreignCRB, relatedSa, testCRB, forigenNamespace}); err != nil {
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

		By("Creating foreign namespace on source")
		Expect(forigenNamespace.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ServiceAccount in foreign namespace")
		Expect(foreignSA.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRoleBinding referencing app's ServiceAccount")
		Expect(testCRB.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRoleBinding referencing foreign namespace ServiceAccount")
		Expect(foreignCRB.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Waiting for source pods and endpoints to drain")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		By("Running crane export, transform, apply")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verifying out-of-scope resources are not in export _cluster directory")
		exportClusterPath := filepath.Join(paths.ExportDir, "resources", namespace, "_cluster")
		found, err := utils.AssertClusterResourcesExist(exportClusterPath, []utils.ClusterResourceMatch{
			{Kind: "ClusterRoleBinding", Name: foreignCRB.Name},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeFalse())

		By("Verifying linked ClusterRole and ClusterRoleBinding exist in export, transform, and output")
		found, err = utils.AssertClusterResourcesExist(exportClusterPath, []utils.ClusterResourceMatch{
			{Kind: "ClusterRoleBinding", Name: testCRB.Name},
			{Kind: "ClusterRole", Name: cr.Name},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
	})
})
