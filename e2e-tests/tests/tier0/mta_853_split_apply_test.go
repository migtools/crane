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

var _ = Describe("Namespace-admin cluster-level migration", func() {
	It("[MTA-853] Should migrate workload with split apply: namespace-admin + cluster-admin", Label("tier0"), func() {
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
		clusterRoleBindingName := "crane-e2e-pod-reader-binding"
		clusterRoleName := "crane-e2e-pod-reader"
		srcAppNonAdmin := scenario.SrcAppNonAdmin
		tgtAppNonAdmin := scenario.TgtAppNonAdmin

		srcAppNonAdmin.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtAppNonAdmin.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		kubectlSrc := scenario.KubectlSrc
		kubectlTgt := scenario.KubectlTgt
		deniedResources := []string{"clusterrolebindings.yaml"}
		if !kubectlSrc.IsOpenShift() {
			deniedResources = append(deniedResources, "clusterroles.yaml")
		}
		paths, err := NewScenarioPaths("crane-na1-*")
		Expect(err).NotTo(HaveOccurred())
		runner := scenario.CraneNonAdmin
		adminRunner := scenario.Crane

		exportOpts := ExportOptions{Namespace: srcAppNonAdmin.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}

		crb := ClusterRoleBinding{Name: clusterRoleBindingName, ClusterRoleName: clusterRoleName}
		cr := ClusterRole{Name: clusterRoleName, Verb: "get,list,watch", Resource: "pods"}
		sa := ServiceAccount{Name: "nginx-sa", Namespace: namespace}

		By("Granting namespace-admin permissions to non-admin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, rbacCleanup, err := SetupActiveKubectlRunners(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			By("Delete test namespace on source and target (wait for completion)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(rbacCleanup)
		DeferCleanup(func() {
			if err := ResourceCleanup([]KubectlRunner{kubectlSrc, kubectlTgt}, []Resource{crb, cr, sa}); err != nil {
				log.Printf("Resources cleanup: %v", err)
			}
			if err := CleanupScenario(paths.TempDir, srcAppNonAdmin, tgtAppNonAdmin); err != nil {
				log.Printf("Scenario cleanup: %v", err)
			}

		})

		By("Deploying app as namespace-admin on source cluster")
		err = PrepareSourceApp(srcAppNonAdmin, kubectlSrcNonAdmin)
		Expect(err).NotTo(HaveOccurred())

		By("Creating Service-Account on namespace")
		Expect(sa.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRole")
		Expect(cr.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRoleBinding")
		Expect(crb.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Bind Relevant Service-Account to cluster role")
		Expect(crb.AddSubject(kubectlSrc, sa)).NotTo(HaveOccurred())

		By("Waiting for source pods and endpoints to drain")
		WaitForSourceQuiesce(kubectlSrcNonAdmin, namespace, "app="+appName, serviceName)

		By("Namespace admin phase: Running crane export, transform, apply as namespace-admin")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Namespace admin phase: Verifying expected cluster-resource failures for the current platform")
		Expect(utils.AssertFilesExist(filepath.Join(paths.ExportDir, "failures", namespace), deniedResources)).NotTo(HaveOccurred())

		By("Namespace admin phase: Verifying no cluster resources in output _cluster directory")
		Expect(utils.AssertNoKindsInOutput(paths.OutputDir, []string{"ClusterRole", "ClusterRoleBinding"})).NotTo(HaveOccurred())

		By("Namespace admin phase: Applying namespace resources to target as namespace-admin")
		Expect(kubectlTgtNonAdmin.ApplyDir(filepath.Join(paths.OutputDir, "resources", namespace))).NotTo(HaveOccurred())

		By("Cluster admin phase: Running crane export, transform, apply as cluster-admin")
		//we reuse the same setup so we need to override for the second pipeline run
		exportOpts.Overwrite = true
		transformOpts.Overwrite = true
		applyOpts.Overwrite = true
		Expect(RunCranePipelineWithChecks(adminRunner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Cluster admin phase: Verifying cluster resources in output _cluster directory after cluster Admin phase")
		Expect(utils.AssertKindsInOutput(paths.OutputDir, []string{"ClusterRole", "ClusterRoleBinding"})).NotTo(HaveOccurred())

		By("Cluster admin phase: Applying cluster resources to target")
		Expect(kubectlTgt.ApplyDir(filepath.Join(paths.OutputDir, "resources", "_cluster"))).NotTo(HaveOccurred())

		By("Scaling target deployment and validating app")
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
		Eventually(tgtAppNonAdmin.Validate, "5m", "10s").Should(Succeed())

		By("Verifying ClusterRoleBinding on target references correct ClusterRole and ServiceAccount")
		Expect(ValidateClusterRBAC(kubectlTgt, []ExpectedClusterRoleBinding{
			{ClusterRoleBindingName: clusterRoleBindingName, ClusterRoleName: clusterRoleName, SubjectName: sa.Name},
		})).NotTo(HaveOccurred())
	})

})
