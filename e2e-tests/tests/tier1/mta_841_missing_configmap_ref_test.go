package e2e

import (
	"log"
	"strings"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Missing ConfigMap reference migration", func() {
	It("[MTA-841] crane migrates deployment with missing ConfigMap reference intact", Label("tier1"), func() {
		namespace := "mta-841-missing-cm"
		missingCMName := "missing-configmap"
		scenario := NewMigrationScenario(
			namespace,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)

		kubectlSrc := scenario.KubectlSrc
		kubectlTgt := scenario.KubectlTgt

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts := ExportOptions{Namespace: namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}
		DeferCleanup(func() {
			By("Delete source and target namespaces")
			if _, err := kubectlSrc.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=60s"); err != nil {
				log.Printf("cleanup: %v", err)
			}
			if _, err := kubectlTgt.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true", "--timeout=60s"); err != nil {
				log.Printf("cleanup: %v", err)
			}

			By("Cleanup namespace and temp dir")
			if err := os.RemoveAll(paths.TempDir); err != nil {
				log.Printf("cleanup: failed to remove temp dir: %v", err)
			}
		})

		By("Create source namespace")
		Expect(kubectlSrc.CreateNamespace(namespace)).NotTo(HaveOccurred())
		log.Printf("Created source namespace %s\n", namespace)

		By("Apply Deployment with missing ConfigMap reference to source")
		deploymentYAML, err := utils.ReadTestdataFile("broken-ref-deployment.yaml")
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectlSrc.RunWithStdin(deploymentYAML, "apply", "-f", "-")
		Expect(err).NotTo(HaveOccurred())
		log.Printf("Applied deployment with broken ConfigMap ref %s\n", missingCMName)

		runner := scenario.Crane
		runner.WorkDir = paths.TempDir

		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", namespace)
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", namespace)

		By("Verify no export failures")
		failuresDir := filepath.Join(paths.ExportDir, "failures", namespace)
		hasFiles, _, err := utils.HasFilesRecursively(failuresDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(hasFiles).To(BeFalse(), "unexpected files in export failures directory")

		By("Verify Deployment manifest exists in output directory")
		deploymentManifest := filepath.Join(paths.OutputDir, "resources", namespace, "*Deployment*.yaml")
		matches, err := filepath.Glob(deploymentManifest)
		Expect(err).NotTo(HaveOccurred())
		Expect(matches).NotTo(BeEmpty(), "Expected at least one Deployment manifest")
		log.Printf("Deployment resource present in output dir: %s\n", matches[0])

		By("Verify broken ConfigMap reference is preserved in output YAML")
		content, err := os.ReadFile(matches[0])
		Expect(err).NotTo(HaveOccurred())
		Expect(string(content)).To(ContainSubstring("name: " + missingCMName))
		log.Printf("Verified broken ConfigMap ref %s is preserved in output YAML\n", missingCMName)

		By("Dry-run apply output manifests on target")
		Expect(kubectlTgt.CreateNamespace(namespace)).NotTo(HaveOccurred())
		Expect(kubectlTgt.ValidateApplyDir(paths.OutputDir)).NotTo(HaveOccurred())

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		By("Verify broken ConfigMap reference is preserved on target cluster")
		out, err := kubectlTgt.Run("get", "deployment", "broken-ref-deploy", "-n", namespace, "-o", "jsonpath={.spec.template.spec.containers[0].envFrom[0].configMapRef.name}")
		Expect(err).NotTo(HaveOccurred())
		Expect(out).To(Equal(missingCMName))
		log.Printf("Verified broken ConfigMap ref %s is present on target\n", missingCMName)

	})
})
