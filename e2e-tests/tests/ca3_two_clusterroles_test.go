package e2e

import (
	"log"

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
		subject := "--serviceaccount=" + namespace + ":" + saName
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

		paths, err := NewScenarioPaths("crane-ca3-*")
		Expect(err).NotTo(HaveOccurred())

		readCR := ClusterRole{Name: readClusterRole, Permission: "read"}
		writeCR := ClusterRole{Name: writeClusterRole, Permission: "write"}
		readCRB := ClusterRoleBinding{Name: readClusterRoleBindingName, ClusterRoleName: readClusterRole, Subject: subject}
		writeCRB := ClusterRoleBinding{Name: writeClusterRoleBindingName, ClusterRoleName: writeClusterRole, Subject: subject}

		DeferCleanup(func() {
			ResourceCleanup([]KubectlRunner{kubectlSrc, kubectlTgt}, []Resource{
				readCR, writeCR, readCRB, writeCRB,
			})
		})

		DeferCleanup(func() {
			CleanupScenario(paths.TempDir, srcApp, tgtApp)
		})

		By("Deploying app with ServiceAccount on source cluster")
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRole with pod read permissions")
		Expect(readCR.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRole with pod write permissions")
		Expect(writeCR.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRoleBinding for read ClusterRole")
		Expect(readCRB.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRoleBinding for write ClusterRole")
		Expect(writeCRB.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Waiting for source pods and endpoints to drain")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		By("Running crane export, transform, apply")
		Expect(RunCranePipelineWithChecks(runner, namespace, paths)).NotTo(HaveOccurred())

		By("Create namespace on target, dryrun validate and Applying migrated manifests to target cluster")
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app")
		log.Printf("Scaling target deployment(s) with label app=%s to 1\n", appName)
		Expect(kubectlTgt.ScaleDeployment(tgtApp.Namespace, appName, 1)).NotTo(HaveOccurred())

		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)

		By("Verifying both ClusterRoleBindings on target reference correct ClusterRoles and ServiceAccount")
		Expect(ValidateClusterRBAC(kubectlTgt, namespace, []ExpectedClusterRoleBinding{
			{ClusterRoleBindingName: readClusterRoleBindingName, ClusterRoleName: readClusterRole, SubjectName: saName},
			{ClusterRoleBindingName: writeClusterRoleBindingName, ClusterRoleName: writeClusterRole, SubjectName: saName},
		})).NotTo(HaveOccurred())
	})
})
