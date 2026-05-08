package e2e

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
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
		DeferCleanup(func() {
			if migratedNamespaceCleanup != nil {
				migratedNamespaceCleanup()
			}
			scenarioCleanup()
		})
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

		kustomization := map[string]any{}
		err = yaml.Unmarshal(baseKustomizationBytes, &kustomization)
		Expect(err).NotTo(HaveOccurred(), "failed to parse custom stage kustomization")

		kustomization["namespace"] = targetNamespace

		commonLabels := map[string]any{}
		if existing, ok := kustomization["commonLabels"]; ok {
			switch labels := existing.(type) {
			case map[string]any:
				for key, value := range labels {
					commonLabels[key] = value
				}
			case map[any]any:
				for key, value := range labels {
					keyStr, keyOk := key.(string)
					if !keyOk {
						continue
					}
					commonLabels[keyStr] = value
				}
			default:
				Fail(fmt.Sprintf("invalid commonLabels type in %s: %T", kustomizationPath, existing))
			}
		}
		commonLabels["migrated-with"] = "crane"
		kustomization["commonLabels"] = commonLabels

		kustomization["images"] = []map[string]string{
			{
				"name":    "quay.io/migqe/nginx-unprivileged",
				"newName": "quay.io/migqe/nginx-unprivileged-crane",
				"newTag":  "latest",
			},
		}

		updatedKustomization, err := yaml.Marshal(kustomization)
		Expect(err).NotTo(HaveOccurred(), "failed to render updated custom stage kustomization")
		err = os.WriteFile(kustomizationPath, updatedKustomization, 0o644)
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
		Expect(outputStr).To(ContainSubstring("quay.io/migqe/nginx-unprivileged-crane:latest"))

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", targetNamespace, paths.OutputDir)
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app in migrated namespace")
		log.Printf("Scaling target deployment(s) with label app=%s to 1 in namespace %s\n", appName, targetNamespace)
		Expect(kubectlTgtNonAdmin.ScaleDeployment(targetNamespace, appName, 1)).NotTo(HaveOccurred())

		By("Verify deployment and service labels, namespace, and migrated image on target")
		expectedImage := "quay.io/migqe/nginx-unprivileged-crane:latest"
		Eventually(func() error {
			out, err := kubectlTgtNonAdmin.Run(
				"get", "deployment", appName+"-deployment",
				"-n", targetNamespace,
				"-o", `jsonpath={.metadata.name}{"|"}{.metadata.namespace}{"|"}{.metadata.labels['migrated-with']}{"|"}{.spec.template.spec.containers[0].image}`,
			)
			if err != nil {
				return err
			}
			out = strings.TrimSpace(StripKubectlWarnings(out))
			if out == "" {
				return fmt.Errorf("deployment %q not found in namespace %q", appName+"-deployment", targetNamespace)
			}
			parts := strings.Split(out, "|")
			if len(parts) != 4 {
				return fmt.Errorf("unexpected deployment jsonpath output: %q", out)
			}
			if parts[1] != targetNamespace {
				return fmt.Errorf("deployment %q is in namespace %q, expected %q", parts[0], parts[1], targetNamespace)
			}
			if parts[2] != "crane" {
				return fmt.Errorf("deployment %q label migrated-with=%q, expected crane", parts[0], parts[2])
			}
			if parts[3] != expectedImage {
				return fmt.Errorf("deployment %q image=%q, expected %q", parts[0], parts[3], expectedImage)
			}
			return nil
		}, "2m", "10s").Should(Succeed())

		Eventually(func() error {
			out, err := kubectlTgtNonAdmin.Run(
				"get", "service", serviceName,
				"-n", targetNamespace,
				"-o", `jsonpath={.metadata.name}{"|"}{.metadata.namespace}{"|"}{.metadata.labels['migrated-with']}`,
			)
			if err != nil {
				return err
			}
			out = strings.TrimSpace(StripKubectlWarnings(out))
			if out == "" {
				return fmt.Errorf("service %q not found in namespace %q", serviceName, targetNamespace)
			}
			parts := strings.Split(out, "|")
			if len(parts) != 3 {
				return fmt.Errorf("unexpected service jsonpath output: %q", out)
			}
			if parts[1] != targetNamespace {
				return fmt.Errorf("service %q is in namespace %q, expected %q", parts[0], parts[1], targetNamespace)
			}
			if parts[2] != "crane" {
				return fmt.Errorf("service %q label migrated-with=%q, expected crane", parts[0], parts[2])
			}
			return nil
		}, "2m", "10s").Should(Succeed())

		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)
	})
})
