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
	It("[MTA-859] Should export CRB with Group subject referencing namespace ServiceAccounts", Label("tier1"), func() {
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
		paths, err := NewScenarioPaths("crane-ca4c-*")
		Expect(err).NotTo(HaveOccurred())

		sa := ServiceAccount{Name: "crane-sa", Namespace: namespace}
		subjectName := "system:serviceaccounts:" + namespace
		subject := "--group=" + subjectName

		cr := ClusterRole{Name: "crane-cr-subject-group", Verb: "get,list,watch,create,update,delete", Resource: "pods"}
		crb := ClusterRoleBinding{Name: "crane-crb-subject-group", ClusterRoleName: cr.Name, Subject: subject}
		tgtNamespace := Namespace{Name: namespace}

		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}

		DeferCleanup(func() {
			if err := ResourceCleanup([]KubectlRunner{kubectlSrc, kubectlTgt}, []Resource{cr, crb}); err != nil {
				log.Printf("Resources cleanup: %v", err)
			}
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("Scenario cleanup: %v", err)
			}
		})

		By("Deploying app with ServiceAccount on source cluster")
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())

		By("Creating Creating first Service-account source")
		Expect(sa.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRole with pod read permissions")
		Expect(cr.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRoleBinding with Group subject for namespace ServiceAccounts")
		Expect(crb.Create(kubectlSrc)).NotTo(HaveOccurred())

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
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scaling target deployment and validating app")
		Expect(kubectlTgt.ScaleDeployment(tgtNamespace.Name, appName, 1)).NotTo(HaveOccurred())
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())

		By("Verifying ClusterRoleBinding on target references correct ClusterRole and Group subject")
		Expect(ValidateClusterRBAC(kubectlTgt, []ExpectedClusterRoleBinding{
			{ClusterRoleBindingName: crb.Name, ClusterRoleName: crb.ClusterRoleName, SubjectName: subjectName},
		})).NotTo(HaveOccurred())
	})

})
