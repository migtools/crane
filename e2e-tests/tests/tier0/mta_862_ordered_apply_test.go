package e2e

import (
	"log"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Ordered apply for Role and RoleBinding migration", func() {
	It("[BUG #266][MTA-862] Should apply ordered output manifests to a fresh namespace in a single pass", Label("BUG #266", "tier0"), func() {
		appName := "simple-nginx-nopv"
		namespace := appName

		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)

		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin

		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		By("Grant ns admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupActiveKubectlRunners(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Delete test namespace on source and target (wait for completion)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(cleanup)

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		By("Create ServiceAccount on source")
		log.Printf("Creating ServiceAccount test-sa in namespace %s\n", namespace)
		_, err = kubectlSrcNonAdmin.Run("create", "serviceaccount", "test-sa", "-n", namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Create Role on source")
		log.Printf("Creating Role pod-reader in namespace %s\n", namespace)
		_, err = kubectlSrcNonAdmin.Run(
			"create", "role", "pod-reader",
			"--verb=get,list,watch",
			"--resource=pods",
			"-n", namespace,
		)
		Expect(err).NotTo(HaveOccurred())

		By("Create RoleBinding on source")
		log.Printf("Creating RoleBinding pod-reader-binding in namespace %s\n", namespace)
		_, err = kubectlSrcNonAdmin.Run(
			"create", "rolebinding", "pod-reader-binding",
			"--role=pod-reader",
			"--serviceaccount="+namespace+":test-sa",
			"-n", namespace,
		)
		Expect(err).NotTo(HaveOccurred())

		By("Verify source RBAC resources exist")
		_, err = kubectlSrcNonAdmin.Run("get", "role", "pod-reader", "-n", namespace)
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectlSrcNonAdmin.Run("get", "rolebinding", "pod-reader-binding", "-n", namespace)
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectlSrcNonAdmin.Run("get", "serviceaccount", "test-sa", "-n", namespace)
		Expect(err).NotTo(HaveOccurred())

		paths, err := NewScenarioPaths("crane-ordered-apply-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts := ExportOptions{Namespace: namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{
			ExportDir:    paths.ExportDir,
			TransformDir: paths.TransformDir,
			OutputDir:    paths.OutputDir,
			Ordered:      true,
		}
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		runner.WorkDir = paths.TempDir

		By("Run crane export/transform/apply --ordered pipeline")
		log.Printf("Running crane pipeline (ordered) for namespace %s\n", namespace)
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", namespace)

		By("Verify Role manifest has an ordering prefix that sorts before RoleBinding")
		rolePattern := filepath.Join(paths.OutputDir, "resources", namespace, "300_Role_*.yaml")
		roleMatches, err := filepath.Glob(rolePattern)
		Expect(err).NotTo(HaveOccurred())
		Expect(roleMatches).NotTo(BeEmpty(), "expected ordered Role manifest (300_Role_*) in output dir")
		log.Printf("Ordered Role manifests in output: %v\n", roleMatches)

		By("Verify RoleBinding manifest has an ordering prefix that sorts after Role")
		rbPattern := filepath.Join(paths.OutputDir, "resources", namespace, "310_RoleBinding_*.yaml")
		rbMatches, err := filepath.Glob(rbPattern)
		Expect(err).NotTo(HaveOccurred())
		Expect(rbMatches).NotTo(BeEmpty(), "expected ordered RoleBinding manifest (310_RoleBinding_*) in output dir")
		log.Printf("Ordered RoleBinding manifests in output: %v\n", rbMatches)

		By("Apply rendered manifests to target in a single pass (ordering must resolve the dependency)")
		log.Printf("Single apply pass for namespace %s using ordered output\n", namespace)
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app is running")
		log.Printf("Scaling target deployment(s) with label app=%s to 1\n", appName)
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())

		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target app validation completed for %s\n", tgtApp.Name)

		By("Verify Role exists on target with correct rules")
		roleOut, err := scenario.KubectlTgt.Run(
			"get", "role", "pod-reader",
			"-n", namespace,
			"-o", "jsonpath={.rules[0].verbs}",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(roleOut).To(ContainSubstring("get"))
		Expect(roleOut).To(ContainSubstring("list"))
		Expect(roleOut).To(ContainSubstring("watch"))
		log.Printf("Role pod-reader rules on target: %s\n", roleOut)

		By("Verify RoleBinding exists on target with correct roleRef and subject")
		rbRoleRef, err := scenario.KubectlTgt.Run(
			"get", "rolebinding", "pod-reader-binding",
			"-n", namespace,
			"-o", "jsonpath={.roleRef.name}",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(rbRoleRef).To(Equal("pod-reader"))

		rbSubject, err := scenario.KubectlTgt.Run(
			"get", "rolebinding", "pod-reader-binding",
			"-n", namespace,
			"-o", "jsonpath={.subjects[0].name}",
		)
		Expect(err).NotTo(HaveOccurred())
		Expect(rbSubject).To(Equal("test-sa"))
		log.Printf("RoleBinding pod-reader-binding on target: roleRef=%s subject=%s\n", rbRoleRef, rbSubject)

		By("Verify ServiceAccount test-sa exists on target")
		_, err = scenario.KubectlTgt.Run("get", "serviceaccount", "test-sa", "-n", namespace)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("ServiceAccount test-sa verified on target\n")
	})
})
