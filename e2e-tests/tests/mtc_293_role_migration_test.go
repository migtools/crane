package e2e

import (
	"log"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Role and RoleBinding migration", func() {
	It("[BUG #266][MTC-293] Should migrate a project with Role and RoleBinding as namespace-admin user", Label("BUG #266", "tier0"), func() {
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

		if scenario.KubectlSrcNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin role migration test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin role migration test")
		}

		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin

		srcApp.ExtraVars = map[string]string{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = map[string]string{
			"non_admin_user": "true",
		}

		By("Grant ns admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Delete test namespace on source and target (best effort)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=false"); err != nil {
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

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		runner.WorkDir = paths.TempDir

		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", namespace)
		Expect(RunCranePipelineWithChecks(runner, namespace, paths)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", namespace)

		By("Verify Role manifest is present in output directory")
		rolePattern := filepath.Join(paths.OutputDir, "resources", namespace, "Role_*.yaml")
		roleMatches, err := filepath.Glob(rolePattern)
		Expect(err).NotTo(HaveOccurred())
		Expect(roleMatches).NotTo(BeEmpty(), "expected Role manifest in output dir")
		log.Printf("Role manifests in output: %v\n", roleMatches)

		By("Verify RoleBinding manifest is present in output directory")
		rbPattern := filepath.Join(paths.OutputDir, "resources", namespace, "RoleBinding_*.yaml")
		rbMatches, err := filepath.Glob(rbPattern)
		Expect(err).NotTo(HaveOccurred())
		Expect(rbMatches).NotTo(BeEmpty(), "expected RoleBinding manifest in output dir")
		log.Printf("RoleBinding manifests in output: %v\n", rbMatches)

		// TODO: remove once https://github.com/migtools/crane/issues/266 is fixed
		// NOTE: kubectl apply -f processes files alphabetically. RoleBinding sorts before
		// Role, so pod-reader-binding fails on a fresh namespace because pod-reader does
		// not yet exist. We skip the dry-run validation on the first pass and call ApplyDir
		// directly so the Role lands before the second pass retries the RoleBinding.
		// See: https://github.com/migtools/crane/issues/266
		By("Apply rendered manifests to target (first pass)")
		log.Printf("First apply pass — skipping dry-run, ordering failure for RoleBinding expected\n")
		_ = kubectlTgtNonAdmin.ApplyDir(paths.OutputDir)

		By("Apply rendered manifests to target (second pass — resolves ordering issue)")
		log.Printf("Second apply pass for namespace %s\n", namespace)
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