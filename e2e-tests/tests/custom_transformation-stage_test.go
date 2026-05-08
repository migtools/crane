package e2e

import (
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Custom transformation stage", func() {
	It("[Custom transformation stage] nginx app quiesce pod and apply to target cluster", Label("tier0"), func() {
		appName := "simple-nginx-nopv"
		namespace := "simple-nginx-nopv"
		serviceName := "my-" + appName
		targetNamespace := "migrated-app"
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		if scenario.KubectlSrcNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin custom transformation stage test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin custom transformation stage test")
		}

		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		tgtApp.Namespace = targetNamespace
		runner := scenario.CraneNonAdmin

		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		By("Grant namespace admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, scenarioCleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		By("Grant namespace admin permissions to nonadmin user on migrated target namespace")
		_, migratedNamespaceCleanup, err := SetupNamespaceAdminUser(
			scenario.KubectlTgt,
			scenario.KubectlTgtNonAdmin.Context,
			targetNamespace,
		)
		if err != nil {
			scenarioCleanup()
		}
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(func() {
			By("Delete test namespace on source and target (wait for completion)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
			if _, err := scenario.KubectlTgt.Run("delete", "namespace", targetNamespace, "--ignore-not-found=true", "--wait=true"); err != nil {
				log.Printf("cleanup: failed to delete namespace %q on context %q: %v", targetNamespace, scenario.KubectlTgt.Context, err)
			}
		})
		DeferCleanup(func() {
			migratedNamespaceCleanup()
			scenarioCleanup()
		})

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

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
		By("Wait for source quiesce to stabilize before export")
		WaitForSourceQuiesce(kubectlSrcNonAdmin, namespace, "app="+appName, serviceName)

		log.Printf("Running crane export for namespace %s\n", srcApp.Namespace)
		Expect(runner.Export(srcApp.Namespace, paths.ExportDir)).NotTo(HaveOccurred())
		log.Printf("Running crane transform default stage for namespace %s\n", srcApp.Namespace)
		Expect(runner.Transform(paths.ExportDir, paths.TransformDir)).NotTo(HaveOccurred())
		log.Printf("Running crane transform --stage for namespace %s\n", srcApp.Namespace)
		Expect(runner.TransformStage(paths.ExportDir, paths.TransformDir, "50_CustomModifications")).NotTo(HaveOccurred())

		By("Update custom stage kustomization with namespace, labels, and image override")
		stageDir := filepath.Join(paths.TransformDir, "50_CustomModifications")
		kustomizationPath := filepath.Join(stageDir, "kustomization.yaml")

		_, err = os.Stat(kustomizationPath)
		Expect(err).NotTo(HaveOccurred(), "expected custom stage kustomization to exist")

		baseKustomizationBytes, err := os.ReadFile(kustomizationPath)
		Expect(err).NotTo(HaveOccurred())

		customPatch := `
namespace: migrated-app
commonLabels:
  migrated-with: crane
images:
- name: quay.io/migqe/nginx-unprivileged
  newName: quay.io/migqe/nginx-unprivileged
  newTag: 1.23-amd64
`

		updatedKustomization := strings.TrimSpace(string(baseKustomizationBytes)) + "\n" + strings.TrimSpace(customPatch) + "\n"
		err = os.WriteFile(kustomizationPath, []byte(updatedKustomization), 0o644)
		Expect(err).NotTo(HaveOccurred())

		By("Validate custom stage is a valid kustomize structure")
		_, err = kubectlSrcNonAdmin.Run("kustomize", stageDir)
		Expect(err).NotTo(HaveOccurred())

		log.Printf("Running crane apply for namespace %s\n", srcApp.Namespace)
		Expect(runner.Apply(paths.ExportDir, paths.TransformDir, paths.OutputDir)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Verify rendered output contains custom stage changes")
		outputYAMLPath := filepath.Join(paths.OutputDir, "output.yaml")
		outputBytes, err := os.ReadFile(outputYAMLPath)
		Expect(err).NotTo(HaveOccurred())
		outputStr := string(outputBytes)
		Expect(outputStr).To(ContainSubstring("namespace: migrated-app"))
		Expect(outputStr).To(ContainSubstring("migrated-with: crane"))
		Expect(outputStr).To(ContainSubstring("quay.io/migqe/nginx-unprivileged:1.23-amd64"))

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", targetNamespace, paths.OutputDir)
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app in migrated namespace")
		log.Printf("Scaling target deployment(s) with label app=%s to 1 in namespace %s\n", appName, targetNamespace)
		Expect(kubectlTgtNonAdmin.ScaleDeployment(targetNamespace, appName, 1)).NotTo(HaveOccurred())

		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)
	})
})
