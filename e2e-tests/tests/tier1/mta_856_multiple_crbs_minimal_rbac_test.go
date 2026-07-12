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

var _ = Describe("Cluster-level RBAC export", func() {
	It("[MTA-856] Should export ClusterRole and ClusterRoleBinding for linked ServiceAccount", Label("tier1"), func() {
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
		paths, err := NewScenarioPaths("crane-*")
		Expect(err).NotTo(HaveOccurred())

		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}

		firstSa := ServiceAccount{Name: "first-sa", Namespace: namespace}
		secondSa := ServiceAccount{Name: "second-sa", Namespace: namespace}
		firstSubject := "--serviceaccount=" + namespace + ":" + firstSa.Name
		secondSubject := "--serviceaccount=" + namespace + ":" + secondSa.Name

		cr := ClusterRole{Name: "crane-cr", Verb: "get,list,watch,create,update,delete", Resource: "pods"}
		crb1 := ClusterRoleBinding{Name: "first-crb", ClusterRoleName: cr.Name, Subject: firstSubject}
		crb2 := ClusterRoleBinding{Name: "second-crb", ClusterRoleName: cr.Name, Subject: secondSubject}
		tgtNamespace := Namespace{Name: namespace}

		DeferCleanup(func() {
			if err := ResourceCleanup([]KubectlRunner{kubectlSrc, kubectlTgt}, []Resource{cr, crb1, crb2, firstSa, secondSa, tgtNamespace}); err != nil {
				log.Printf("Resources cleanup: %v", err)
			}
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("Scenario cleanup: %v", err)
			}
		})

		By("Deploying app on source cluster")
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())

		By("Creating first Service-account on src namespace")
		Expect(firstSa.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating second Service-account on src namespace")
		Expect(secondSa.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRole on source cluster")
		Expect(cr.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating first ClusterRoleBinding referencing first ServiceAccount")
		Expect(crb1.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating second ClusterRoleBinding referencing second ServiceAccount")
		Expect(crb2.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Waiting for source pods and endpoints to drain")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		By("Running crane export, transform, apply")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verifying no resources failed to export")
		failuresDir := filepath.Join(paths.ExportDir, "failures", namespace)
		hasFiles, _, err := utils.HasFilesRecursively(failuresDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasFiles).To(BeFalse())

		By("Create Namespace On target cluster")
		Expect(tgtNamespace.Create(kubectlTgt)).NotTo(HaveOccurred())

		By("Dry-run applying output manifests on target")
		Expect(kubectlTgt.ValidateApplyDir(paths.OutputDir)).NotTo(HaveOccurred())

		By("Applying migrated manifests to target cluster")
		Expect(ApplyOutputToTarget(kubectlTgt, tgtNamespace.Name, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scaling target deployment and validating app")
		Expect(kubectlTgt.ScaleDeployment(tgtNamespace.Name, appName, 1)).NotTo(HaveOccurred())
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())

		By("Verifying both ClusterRoleBindings on target reference correct ClusterRole and ServiceAccounts")
		Expect(ValidateClusterRBAC(kubectlTgt, []ExpectedClusterRoleBinding{
			{ClusterRoleBindingName: crb1.Name, ClusterRoleName: crb1.ClusterRoleName, SubjectName: firstSa.Name},
			{ClusterRoleBindingName: crb2.Name, ClusterRoleName: crb2.ClusterRoleName, SubjectName: secondSa.Name},
		})).NotTo(HaveOccurred())
	})
})
