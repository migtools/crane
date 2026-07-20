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
	It("[MTA-868] Should export only labeled workload and its RBAC with --label-selector", Label("tier0"), func() {
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
		paths, err := NewScenarioPaths("crane-ca8-*")
		Expect(err).NotTo(HaveOccurred())

		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir,
			LabelSelector: "app=" + appName}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}

		inScopeSA := ServiceAccount{Name: "nginx-sa", Namespace: namespace, Label: "app=simple-nginx-nopv"}
		outOfScopeSA := ServiceAccount{Name: "out-of-scope-sa", Namespace: namespace, Label: "app=outScopedApp"}

		inScopeCR := ClusterRole{Name: "in-scope-cr", Verb: "get,list,watch", Resource: "pods", Label: "app=" + appName}
		outOfScopeCR := ClusterRole{Name: "out-scope-cr", Verb: "get,list,watch,create,update,delete", Resource: "pods", Label: "app=outScopedApp"}

		inScopeSubject := "--serviceaccount=" + namespace + ":" + inScopeSA.Name
		outScopeSubject := "--serviceaccount=" + namespace + ":" + outOfScopeSA.Name

		inScopeBinding := ClusterRoleBinding{Name: "in-scope-crb", ClusterRoleName: inScopeCR.Name, Subject: inScopeSubject, Label: "app=" + appName}
		outOfScopeBinding := ClusterRoleBinding{Name: "out-scope-crb", ClusterRoleName: outOfScopeCR.Name, Subject: outScopeSubject, Label: "app=outScopedApp"}

		outOfScopeResources := []utils.ResourceMatch{
			{Kind: "ClusterRoleBinding", Name: outOfScopeBinding.Name},
			{Kind: "ClusterRole", Name: outOfScopeCR.Name},
		}
		inScopeResources := []utils.ResourceMatch{
			{Kind: "ClusterRoleBinding", Name: inScopeBinding.Name},
			{Kind: "ClusterRole", Name: inScopeCR.Name},
		}
		DeferCleanup(func() {
			if err := ResourceCleanup([]KubectlRunner{kubectlSrc, kubectlTgt}, []Resource{
				inScopeBinding, outOfScopeBinding, inScopeCR, outOfScopeCR, inScopeSA, outOfScopeSA}); err != nil {
				log.Printf("Resources cleanup: %v", err)
			}
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("Scenario cleanup: %v", err)
			}
		})

		By("Deploying app on source cluster")
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())

		By("Creating in-scope ServiceAccount with matching label")
		Expect(inScopeSA.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating out-of-scope ServiceAccount with different label")
		Expect(outOfScopeSA.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating in-scope ClusterRole with matching label")
		Expect(inScopeCR.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating out-of-scope ClusterRole with different label")
		Expect(outOfScopeCR.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating in-scope ClusterRoleBinding with matching label")
		Expect(inScopeBinding.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating out-of-scope ClusterRoleBinding with different label")
		Expect(outOfScopeBinding.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Waiting for source pods and endpoints to drain")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		By("Running crane export with label-selector, transform, apply")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verifying out-of-scope resources are not in export _cluster directory")
		exportClusterPath := filepath.Join(paths.ExportDir, "resources", namespace, "_cluster")
		allExcluded, err := utils.AssertResourcesDontExist(exportClusterPath, outOfScopeResources)
		Expect(err).NotTo(HaveOccurred())
		Expect(allExcluded).To(BeTrue())

		By("Verifying in-scope ClusterRole and ClusterRoleBinding exist after export")
		allFound, err := utils.AssertResourcesExist(exportClusterPath, inScopeResources)
		Expect(err).NotTo(HaveOccurred())
		Expect(allFound).To(BeTrue())
	})
})
