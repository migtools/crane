package e2e

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Migrate namespace with multiple secret types", func() {
	It("[MTA-842] all secret types are exported, transformed, and applied to target cluster", Label("tier1"), func() {
		namespace := "crane-secrets-test"

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
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}
		DeferCleanup(func() {
			By("Cleanup temp directory")
			if paths.TempDir != "" {
				log.Printf("Removing temp dir: %s\n", paths.TempDir)
				if err := os.RemoveAll(paths.TempDir); err != nil {
					log.Printf("cleanup: failed to remove temp dir %q: %v", paths.TempDir, err)
				}
			}
			By("Cleanup source namespace and temp dir")
			if _, err := kubectlSrc.Run("delete", "namespace", namespace, "--ignore-not-found=true"); err != nil {
				log.Printf("cleanup: failed to delete source namespace: %v", err)
			}
			By("Cleanup target namespace and temp dir")
			if _, err := kubectlTgt.Run("delete", "namespace", namespace, "--ignore-not-found=true"); err != nil {
				log.Printf("cleanup: failed to delete target namespace: %v", err)
			}
		})

		By("Create namespace on source")
		Expect(kubectlSrc.CreateNamespace(namespace)).NotTo(HaveOccurred())

		By("Create Opaque secret on source")
		Expect(kubectlSrc.Run("create", "secret", "generic", "opaque-secret", "--from-literal=key=value", "-n", namespace)).Error().NotTo(HaveOccurred())

		By("Generate TLS certificate and key")
		certFile := filepath.Join(paths.TempDir, "tls.crt")
		keyFile := filepath.Join(paths.TempDir, "tls.key")
		cmd := exec.Command("openssl", "req", "-x509", "-newkey", "rsa:2048", "-keyout", keyFile, "-out", certFile, "-days", "1", "-nodes", "-subj", "/CN=test")
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "openssl output: %s", out)

		By("Create TLS secret on source")
		Expect(kubectlSrc.Run("create", "secret", "tls", "tls-secret", "--cert="+certFile, "--key="+keyFile, "-n", namespace)).Error().NotTo(HaveOccurred())

		By("Create docker-registry secret on source")
		Expect(kubectlSrc.Run("create", "secret", "docker-registry", "docker-secret", "--docker-server=quay.io", "--docker-username=user", "--docker-password=pass", "-n", namespace)).Error().NotTo(HaveOccurred())

		runner := scenario.Crane
		runner.WorkDir = paths.TempDir

		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", namespace)
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", namespace)

		By("Verify all secret manifests are present in output directory")
		for _, secretName := range []string{"opaque-secret", "tls-secret", "docker-secret"} {
			glob := filepath.Join(paths.OutputDir, "resources", namespace, "Secret__*_"+secretName+".yaml")
			matches, err := filepath.Glob(glob)
			Expect(err).NotTo(HaveOccurred())
			Expect(matches).NotTo(BeEmpty(), "expected manifest for secret "+secretName+" in output directory")
			log.Printf("Secret manifest found in output: %s\n", matches)
		}

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		By("Verify Opaque secret is present on target with correct type")
		Expect(VerifySecret(kubectlTgt, namespace, "opaque-secret", "Opaque")).NotTo(HaveOccurred())

		By("Verify tls secret is present on target with correct type")
		Expect(VerifySecret(kubectlTgt, namespace, "tls-secret", "kubernetes.io/tls")).NotTo(HaveOccurred())

		By("Verify docker secret is present on target with correct type")
		Expect(VerifySecret(kubectlTgt, namespace, "docker-secret", "kubernetes.io/dockerconfigjson")).NotTo(HaveOccurred())

	})
})
