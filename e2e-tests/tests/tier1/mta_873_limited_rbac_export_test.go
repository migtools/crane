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
	It("[MTA-873] Should export namespace resources and record failures under limited RBAC", Label("tier1"), func() {
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
		NonAdminrunner := scenario.CraneNonAdmin

		exportOpts := ExportOptions{Namespace: srcAppNonAdmin.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}
		cr := ClusterRole{Name: "crane-cluster-role", Verb: "get,list,watch", Resource: "pods"}
		firstCrb := ClusterRoleBinding{Name: "first-crb", ClusterRoleName: cr.Name}
		firstSa := ServiceAccount{Name: "first-nginx-sa", Namespace: namespace}

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
				[]KubectlRunner{kubectlSrc, kubectlTgt}, []Resource{firstCrb, cr, firstSa}); err != nil {
				log.Printf("Resources cleanup: %v", err)
			}
			if err := CleanupScenario(paths.TempDir, srcAppNonAdmin, tgtAppNonAdmin); err != nil {
				log.Printf("Scenario cleanup: %v", err)
			}

		})

		By("Deploying app as namespace-admin on source cluster")
		Expect(PrepareSourceApp(srcAppNonAdmin, kubectlSrcNonAdmin)).NotTo(HaveOccurred())

		By("Creating Service-Account on namespace")
		Expect(firstSa.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating ClusterRole")
		Expect(cr.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating the ClusterRoleBinding")
		Expect(firstCrb.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Bind Relevant Service-Account to cluster role")
		Expect(firstCrb.AddSubject(kubectlSrc, firstSa)).NotTo(HaveOccurred())

		By("Waiting for source pods and endpoints to drain")
		WaitForSourceQuiesce(kubectlSrcNonAdmin, namespace, "app="+appName, serviceName)

		By("Namespace admin: Running crane export, transform, apply as namespace-admin")
		Expect(RunCranePipelineWithChecks(NonAdminrunner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Namespace admin: Verifying expected cluster-resource failures for the current platform")
		Expect(utils.AssertFilesExist(filepath.Join(paths.ExportDir, "failures", namespace), deniedResources)).NotTo(HaveOccurred())

		By("Namespace admin: Verifying no cluster resources in output _cluster directory")
		Expect(utils.AssertNoKindsInOutput(paths.OutputDir, []string{"ClusterRole", "ClusterRoleBinding"})).NotTo(HaveOccurred())

		By("Namespace admin phase: Applying namespace resources to target as namespace-admin")
		Expect(kubectlTgtNonAdmin.ValidateApplyDir(paths.OutputDir)).NotTo(HaveOccurred())
	})

})
