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
	It("[MTA-852] Should export ClusterRole and ClusterRoleBinding for linked ServiceAccount", Label("tier0"), func() {
		appName := "simple-nginx-nopv"
		namespace := "simple-nginx-nopv"
		serviceName := "my-" + appName
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
		Expect(err).NotTo(HaveOccurred())
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}

		crb := ClusterRoleBinding{Name: clusterRoleBindingName, ClusterRoleName: clusterRoleName}
		cr := ClusterRole{Name: clusterRoleName, Verb: "get,list,watch", Resource: "pods"}
		sa := ServiceAccount{Name: "nginx-sa", Namespace: namespace}
		DeferCleanup(func() {
			if err := ResourceCleanup([]KubectlRunner{kubectlSrc, kubectlTgt}, []Resource{cr, crb, sa}); err != nil {
				log.Printf("Resources cleanup: %v", err)
			}

			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("Scenario cleanup: %v", err)
			}
		})

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())

		By("Creating Service-Account on namespace")
		Expect(sa.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRole")
		Expect(cr.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRoleBinding that references the app's ServiceAccount")
		Expect(crb.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Bind Relevent Service-Account to cluster role")
		Expect(crb.AddSubject(kubectlSrc, sa)).NotTo(HaveOccurred())

		By("Waiting for source pods and endpoints to drain")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		By("Running crane export, transform, apply")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verifying no resources failed to export")
		failuresDir := filepath.Join(paths.ExportDir, "failures", namespace)
		hasFiles, _, err := utils.HasFilesRecursively(failuresDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasFiles).To(BeFalse())

		By("Dry-run applying output manifests on target")
		Expect(kubectlTgt.CreateNamespace(namespace)).NotTo(HaveOccurred())
		Expect(kubectlTgt.ValidateApplyDir(paths.OutputDir)).NotTo(HaveOccurred())

		By("Applying migrated manifests to target cluster")
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scaling target deployment and validating app")
		Expect(kubectlTgt.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
		Eventually(tgtApp.Validate, "5m", "10s").Should(Succeed())

		By("Verifying ClusterRoleBinding on target references correct ClusterRole and ServiceAccount")
		Expect(ValidateClusterRBAC(kubectlTgt, []ExpectedClusterRoleBinding{
			{ClusterRoleBindingName: clusterRoleBindingName, ClusterRoleName: clusterRoleName, SubjectName: sa.Name},
		})).NotTo(HaveOccurred())
	})

})
