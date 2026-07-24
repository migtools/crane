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
	It("[MTA-871]Should migrate workload with one CR and two CRBs using split apply", Label("tier0"), func() {
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
		NonAdminRunner := scenario.CraneNonAdmin
		adminRunner := scenario.Crane

		exportOpts := ExportOptions{Namespace: srcAppNonAdmin.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}
		cr := ClusterRole{Name: "crane-cluster-role", Verb: "get,list,watch", Resource: "pods"}
		firstCrb := ClusterRoleBinding{Name: "first-crb", ClusterRoleName: cr.Name}
		secondCrb := ClusterRoleBinding{Name: "second-crb", ClusterRoleName: cr.Name}
		firstSa := ServiceAccount{Name: "first-nginx-sa", Namespace: namespace}
		secondSa := ServiceAccount{Name: "second-nginx-sa", Namespace: namespace}
		clusterResourcesMatch := []utils.ResourceMatch{
			{Kind: "ClusterRoleBinding", Name: firstCrb.Name},
			{Kind: "ClusterRoleBinding", Name: secondCrb.Name},
			{Kind: "ClusterRole", Name: cr.Name},
		}
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
			if err := ResourceCleanup(
				[]KubectlRunner{kubectlSrc, kubectlTgt}, []Resource{firstCrb, secondCrb, cr, firstSa, secondSa}); err != nil {
				log.Printf("Resources cleanup: %v", err)
			}
			if err := CleanupScenario(paths.TempDir, srcAppNonAdmin, tgtAppNonAdmin); err != nil {
				log.Printf("Scenario cleanup: %v", err)
			}

		})

		By("Deploying app as namespace-admin on source cluster")
		Expect(PrepareSourceApp(srcAppNonAdmin, kubectlSrcNonAdmin)).NotTo(HaveOccurred())

		By("Creating first Service-Account on namespace")
		Expect(firstSa.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating second Service-Account on namespace")
		Expect(secondSa.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRole")
		Expect(cr.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating the first crb ClusterRoleBinding")
		Expect(firstCrb.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating the second crb ClusterRoleBinding")
		Expect(secondCrb.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("first crb: Bind Relevant Service-Account to cluster role")
		Expect(firstCrb.AddSubject(kubectlSrc, firstSa)).NotTo(HaveOccurred())

		By("second crb: Bind Relevant Service-Account to cluster role")
		Expect(secondCrb.AddSubject(kubectlSrc, secondSa)).NotTo(HaveOccurred())

		By("Waiting for source pods and endpoints to drain")
		WaitForSourceQuiesce(kubectlSrcNonAdmin, namespace, "app="+appName, serviceName)

		By("Namespace admin phase: Running crane export, transform, apply as namespace-admin")
		Expect(RunCranePipelineWithChecks(NonAdminRunner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

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
		allPresented, err := utils.AssertResourcesExist(filepath.Join(paths.OutputDir, "resources", "_cluster"), clusterResourcesMatch)
		Expect(err).NotTo(HaveOccurred())
		Expect(allPresented).To(BeTrue())

		By("Cluster admin phase: Applying namespace resources to target as namespace-admin")
		Expect(kubectlTgt.ApplyDir(filepath.Join(paths.OutputDir, "resources", "_cluster"))).NotTo(HaveOccurred())

		By("Scaling target deployment and validating app")
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
		Eventually(tgtAppNonAdmin.Validate, "5m", "10s").Should(Succeed())

	})

})
