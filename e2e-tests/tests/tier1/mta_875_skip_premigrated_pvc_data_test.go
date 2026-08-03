package e2e

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func getPodName(k KubectlRunner, namespace, label string) (string, error) {
	out, err := k.Run("get", "pods", "-n", namespace, "-l", "app="+label, "-o", "jsonpath={.items[0].metadata.name}")
	if err != nil {
		return "", err
	}
	name := strings.TrimSpace(out)
	if name == "" {
		return "", fmt.Errorf("no pod found in namespace %q with label app=%s", namespace, label)
	}
	return name, nil
}

func readPodFile(k KubectlRunner, namespace, pod, path string) (string, error) {
	return k.Run("exec", pod, "-n", namespace, "--", "cat", path)
}

func writePodFile(k KubectlRunner, namespace, pod, path, content string) error {
	_, err := k.RunWithStdin(content, "exec", pod, "-n", namespace, "-i", "--", "sh", "-c", "cat > "+path)
	return err
}

func curlStatusCode(k KubectlRunner, namespace, pod string) (string, error) {
	return k.Run("exec", pod, "-n", namespace, "--", "sh", "-c", "curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/")
}

var _ = Describe("Skip PV migration when PV data was already migrated ahead of time", func() {
	It("[MTA-875] Skip PV migration when PV data was already migrated ahead of time", Label("tier1", "pvc-transfer"), func() {
		const podLabel = "nginx"
		appName := "nginxpv"
		namespace := "mta-875-skip-pv"
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

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		By("Deploy nginx app with two PVCs (html, logs)")
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())
		srcPod, err := getPodName(kubectlSrc, srcApp.Namespace, podLabel)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("pod %s is ready\n", srcPod)

		By("Generate traffic/data (a 403 on empty html dir, then a real page)")
		firstCode, err := curlStatusCode(kubectlSrc, srcApp.Namespace, srcPod)
		Expect(err).NotTo(HaveOccurred())
		Expect(firstCode).To(Equal("403"), "expected a 403 for the empty html dir")
		Expect(writePodFile(kubectlSrc, srcApp.Namespace, srcPod, "/usr/share/nginx/html/index.html", "<h1>HELLO WORLD</h1>\n")).NotTo(HaveOccurred())
		secondCode, err := curlStatusCode(kubectlSrc, srcApp.Namespace, srcPod)
		Expect(err).NotTo(HaveOccurred())
		Expect(secondCode).To(Equal("200"), "expected a 200 once index.html exists")

		By("Extract data to be pre-copied to target ahead of migration")
		srcIndexHTML, err := readPodFile(kubectlSrc, srcApp.Namespace, srcPod, "/usr/share/nginx/html/index.html")
		Expect(err).NotTo(HaveOccurred())
		srcAccessLog, err := readPodFile(kubectlSrc, srcApp.Namespace, srcPod, "/var/log/nginx/access.log")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.Count(srcAccessLog, "403")).To(BeNumerically(">=", 1), "expected at least one 403 in source access.log")
		srcErrorLog, err := readPodFile(kubectlSrc, srcApp.Namespace, srcPod, "/var/log/nginx/error.log")
		Expect(err).NotTo(HaveOccurred())

		By("Import PV data ahead of time via a temporary instance of the same app")
		Expect(tgtApp.Deploy()).NotTo(HaveOccurred())
		Expect(tgtApp.Validate()).NotTo(HaveOccurred())
		tgtSeedPod, err := getPodName(kubectlTgt, tgtApp.Namespace, podLabel)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("seeding pre-existing PVC data via temporary pod %s\n", tgtSeedPod)
		Expect(writePodFile(kubectlTgt, tgtApp.Namespace, tgtSeedPod, "/usr/share/nginx/html/index.html", srcIndexHTML+"\n")).NotTo(HaveOccurred())
		Expect(writePodFile(kubectlTgt, tgtApp.Namespace, tgtSeedPod, "/var/log/nginx/access.log", srcAccessLog+"\n")).NotTo(HaveOccurred())
		Expect(writePodFile(kubectlTgt, tgtApp.Namespace, tgtSeedPod, "/var/log/nginx/error.log", srcErrorLog+"\n")).NotTo(HaveOccurred())

		By("Remove the temporary app, leaving the pre-populated PVCs bound")
		_, err = kubectlTgt.Run("delete", "deployment", "nginx-deployment", "-n", tgtApp.Namespace, "--wait=true")
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectlTgt.Run("delete", "service", "my-nginx", "-n", tgtApp.Namespace)
		Expect(err).NotTo(HaveOccurred())
		tgtPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtPVCs).To(HaveLen(2), "expected the two pre-populated PVCs to remain after removing the temporary app")

		By("Run crane export/transform/apply pipeline (no transfer-pvc: PV migration is skipped)")
		runner := scenario.Crane
		runner.WorkDir = paths.TempDir
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verify rendered manifests contain no PVC/PV: crane's default pipeline never migrates PV data")
		outputFiles, err := utils.ListFilesRecursivelyAsList(paths.OutputDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(outputFiles).NotTo(BeEmpty())
		for _, f := range outputFiles {
			Expect(f).NotTo(ContainSubstring("PersistentVolume"), "output should not contain a PVC/PV manifest file: %s", f)
			content, err := os.ReadFile(filepath.Join(paths.OutputDir, f))
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).NotTo(ContainSubstring("PersistentVolume"), "output file %s should not reference a PVC/PV", f)
		}

		By("Apply rendered manifests; the new pod must bind to the pre-existing, pre-populated PVCs")
		Expect(ApplyOutputToTarget(kubectlTgt, tgtApp.Namespace, paths.OutputDir)).NotTo(HaveOccurred())
		Eventually(tgtApp.Validate, "5m", "10s").Should(Succeed())
		tgtPod, err := getPodName(kubectlTgt, tgtApp.Namespace, podLabel)
		Expect(err).NotTo(HaveOccurred())
		log.Printf("migrated pod %s is ready\n", tgtPod)

		By("Verify the migrated app is functioning correctly using the pre-existing data, without a PV data transfer")
		tgtIndexHTML, err := readPodFile(kubectlTgt, tgtApp.Namespace, tgtPod, "/usr/share/nginx/html/index.html")
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtIndexHTML).To(Equal(srcIndexHTML), "index.html should be the pre-copied source content")
		tgtAccessLog, err := readPodFile(kubectlTgt, tgtApp.Namespace, tgtPod, "/var/log/nginx/access.log")
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtAccessLog).To(ContainSubstring(srcAccessLog), "access.log should still contain the pre-copied source content")
		tgtErrorLog, err := readPodFile(kubectlTgt, tgtApp.Namespace, tgtPod, "/var/log/nginx/error.log")
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtErrorLog).To(ContainSubstring(srcErrorLog), "error.log should still contain the pre-copied source content")

		By("Final smoke test: the migrated app serves the pre-existing page correctly")
		finalCode, err := curlStatusCode(kubectlTgt, tgtApp.Namespace, tgtPod)
		Expect(err).NotTo(HaveOccurred())
		Expect(finalCode).To(Equal("200"))
	})
})
