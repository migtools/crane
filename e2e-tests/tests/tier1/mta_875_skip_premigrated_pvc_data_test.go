package e2e

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var pvcOrPVKindRegex = regexp.MustCompile(`(?m)^kind:\s*(PersistentVolume|PersistentVolumeClaim)\s*$`)

func md5sumFile(k KubectlRunner, namespace, pod, path string) (string, error) {
	out, err := k.Run("exec", pod, "-n", namespace, "--", "/bin/sh", "-c", fmt.Sprintf("md5sum %s | awk '{print $1}'", path))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

var _ = Describe("Skip PV migration when PV data was already migrated ahead of time", func() {
	It("[MTA-875] Skip PV migration when PV data was already migrated ahead of time", Label("tier1", "pvc-transfer"), func() {
		const testFileName = "testfile.txt"
		appName := "app-with-empty-pvc"
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

		srcApp.ExtraVars = map[string]any{
			"app_name":  appName,
			"add_data":  "true",
			"file_name": testFileName,
			"file_size": 10,
		}
		tgtApp.ExtraVars = map[string]any{
			"app_name": appName,
			"add_data": "true",
		}

		By("Deploy a source app with a PVC and seed it with known data")
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

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

		By("Get file MD5 checksum")
		srcMD5, err := md5sumFile(kubectlSrc, srcApp.Namespace, appName, "/data/"+testFileName)
		Expect(err).NotTo(HaveOccurred())
		Expect(srcMD5).NotTo(BeEmpty(), "expected to compute an MD5 checksum on source")
		log.Printf("MD5 checksum: %s\n", srcMD5)

		By("List PVCs in the namespace")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).To(HaveLen(1), "expected exactly one PVC in namespace %q", srcApp.Namespace)
		pvcName := pvcs[0].Name
		log.Printf("Found PVC %s in namespace %q\n", pvcName, srcApp.Namespace)

		By("Create target namespace")
		Expect(kubectlTgt.CreateNamespace(tgtApp.Namespace)).NotTo(HaveOccurred())

		By("Manually run crane transfer-pvc to migrate the PVC data to the target cluster ahead of the main app migration")
		runner := scenario.Crane
		runner.WorkDir = paths.TempDir
		tgtIP, err := GetClusterNodeIP(tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		transferOpts := TransferPVCOptions{
			SourceContext:   srcApp.Context,
			TargetContext:   tgtApp.Context,
			PVCName:         pvcName,
			PVCNamespaceMap: fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
			Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvcName, srcApp.Namespace, tgtIP),
		}
		Expect(runner.TransferPVC(transferOpts)).NotTo(HaveOccurred())

		By("Wait for transfer-pvc's ephemeral rsync pods to be fully removed")
		Eventually(func() (string, error) {
			return kubectlSrc.Run("get", "pods", "-n", srcApp.Namespace, "-o", "name")
		}, "60s", "2s").ShouldNot(ContainSubstring("rsync"))
		Eventually(func() (string, error) {
			return kubectlTgt.Run("get", "pods", "-n", tgtApp.Namespace, "-o", "name")
		}, "60s", "2s").ShouldNot(ContainSubstring("rsync"))

		By("Verify the destination PVC exists")
		tgtPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtPVCs).To(HaveLen(1), "expected the pre-migrated PVC to exist on target")

		By("Verify the destination PVC's data already matches source, isolated from the rest of the migration")
		const verifierPod = "mta-875-pvc-verifier"
		verifierPodYAML := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  containers:
  - name: verifier
    image: quay.io/openshifttest/alpine:multiarch
    command: ["sleep", "300"]
    volumeMounts:
    - name: data
      mountPath: /data
    securityContext:
      runAsNonRoot: true
      runAsUser: 1000
      allowPrivilegeEscalation: false
      seccompProfile:
        type: RuntimeDefault
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: %s
`, verifierPod, tgtApp.Namespace, pvcName)
		Expect(kubectlTgt.ApplyYAMLSpec(verifierPodYAML, tgtApp.Namespace)).NotTo(HaveOccurred())
		_, err = kubectlTgt.Run("wait", "--for=condition=Ready", "pod/"+verifierPod, "-n", tgtApp.Namespace, "--timeout=120s")
		Expect(err).NotTo(HaveOccurred())
		tgtMD5BeforeMigration, err := md5sumFile(kubectlTgt, tgtApp.Namespace, verifierPod, "/data/"+testFileName)
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtMD5BeforeMigration).To(Equal(srcMD5),
			"destination PVC data should already match source right after transfer-pvc, before the main migration runs")
		_, err = kubectlTgt.Run("delete", "pod", verifierPod, "-n", tgtApp.Namespace, "--wait=true")
		Expect(err).NotTo(HaveOccurred())

		By("Run crane export/transform/apply for the app's namespace (PVCs whited out as usual)")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verify the rendered manifests contain no PVC/PV: the already-migrated PVC is not re-transferred or re-created")
		outputFiles, err := utils.ListFilesRecursivelyAsList(paths.OutputDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(outputFiles).NotTo(BeEmpty())
		for _, f := range outputFiles {
			content, err := os.ReadFile(filepath.Join(paths.OutputDir, f))
			Expect(err).NotTo(HaveOccurred())
			Expect(pvcOrPVKindRegex.MatchString(string(content))).To(BeFalse(),
				"output file %s should not declare a PersistentVolume/PersistentVolumeClaim kind", f)
		}

		By("Deploy/scale up the app on the target cluster, reusing the pre-migrated PVC")
		Expect(ApplyOutputToTarget(kubectlTgt, tgtApp.Namespace, paths.OutputDir)).NotTo(HaveOccurred())
		Eventually(tgtApp.Validate, "5m", "10s").Should(Succeed())

		By("Verify the app reads the pre-migrated data with no data loss")
		tgtMD5, err := md5sumFile(kubectlTgt, tgtApp.Namespace, appName, "/data/"+testFileName)
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtMD5).To(Equal(srcMD5), "MD5 checksum on target should match source")
		log.Printf("Source and target MD5 checksums match: %s\n", srcMD5)
	})
})
