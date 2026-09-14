package e2e

import (
	"fmt"
	"log"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// rcloneInspectImage is used by a throwaway pod that reads the S3 bucket directly
// to prove that crane wrote only ciphertext.
const rcloneInspectImage = "rclone/rclone:latest"

// The marker is written into the source /test-data PVC as a known plaintext file.
// After an encrypted transfer neither its name (rclone crypt obfuscates file names)
// nor its content should be findable among the raw objects in the bucket.
const (
	encMarkerFile    = "/test-data/plaintext-marker.txt"
	encMarkerBase    = "plaintext-marker.txt"
	encMarkerContent = "CRANE-ENCRYPT-PLAINTEXT-MARKER-8f3a1c"
)

// sectionBetween returns the text between the first occurrence of start and the
// following occurrence of end (exclusive). Empty string if the markers are absent.
func sectionBetween(s, start, end string) string {
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	i += len(start)
	j := strings.Index(s[i:], end)
	if j < 0 {
		return ""
	}
	return s[i : i+j]
}

var _ = Describe("Encrypted indirect transfer (--rclone-config-file --encrypt)", func() {
	It("[MTA-914] Should migrate MySQL PVC data via encrypted S3 and store only ciphertext at rest",
		Label("tier1", "pvc-transfer", "indirect"), func() {

			if config.CloudStorage == "" || config.RcloneConfigFile == "" {
				Skip("encrypted indirect transfer requires --cloud-storage and --rclone-config-file")
			}

			const testDataFile = "/test-data/test1"
			appName := "mysql"
			namespace := "indirect-encrypted-mysql"

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

			isOCP := scenario.KubectlSrc.IsOpenShift()
			srcApp.ExtraVars = map[string]any{
				"non_admin_user": "true",
				"has_scc":        isOCP,
			}

			By("Grant namespace-admin permissions to the non-admin user on source and target")
			kubectlSrc, kubectlTgt, rbacCleanup, err := SetupActiveKubectlRunners(scenario, namespace)
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

			paths, err := NewScenarioPaths("crane-indirect-encrypted-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				By("Cleanup source and target resources")
				if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
					log.Printf("cleanup: %v", err)
				}
			})

			By("Deploy the source MySQL app (auto-seeds the authors table and /test-data/test1)")
			Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

			By("Capture the source test-data fingerprint and seed a known plaintext marker")
			srcPod, err := GetPodNameByLabel(kubectlSrc, srcApp.Namespace, "app="+appName)
			Expect(err).NotTo(HaveOccurred())
			Expect(srcPod).NotTo(BeEmpty(), "expected a running MySQL pod on source")
			Eventually(func() error {
				return waitForMySQLSocket(kubectlSrc, srcApp.Namespace, srcPod)
			}, "2m", "5s").Should(Succeed())

			_, err = kubectlSrc.Run("exec", srcPod, "-n", srcApp.Namespace, "--", "/bin/sh", "-c",
				fmt.Sprintf("printf '%%s' %q > %s", encMarkerContent, encMarkerFile))
			Expect(err).NotTo(HaveOccurred(), "failed to write marker file into the source /test-data PVC")

			srcMD5, err := md5OfFileInPod(kubectlSrc, srcApp.Namespace, srcPod, testDataFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(srcMD5).NotTo(BeEmpty(), "expected to compute an MD5 for the source test-data file")
			log.Printf("Source test-data MD5: %s", srcMD5)

			By("Quiesce the source app so the PVC copy is consistent and the volumes are released")
			Expect(kubectlSrc.ScaleDeploymentIfPresent(srcApp.Namespace, appName, 0)).NotTo(HaveOccurred())
			Eventually(func() (string, error) {
				out, err := kubectlSrc.Run("get", "pods", "-n", namespace, "-l", "app="+appName, "-o", "name")
				return strings.TrimSpace(StripKubectlWarnings(out)), err
			}, "90s", "3s").Should(BeEmpty())

			By("List the PVCs created by the source app")
			pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
			Expect(err).NotTo(HaveOccurred())
			Expect(pvcs).NotTo(BeEmpty(), "expected at least one PVC in namespace %q", srcApp.Namespace)
			log.Printf("Found %d PVCs in namespace %q", len(pvcs), srcApp.Namespace)

			By("Transfer each PVC in indirect mode with client-side encryption")
			runner := scenario.CraneNonAdmin
			runner.WorkDir = paths.TempDir
			for _, pvc := range pvcs {
				opts := TransferPVCOptions{
					SourceContext:    srcApp.Context,
					TargetContext:    tgtApp.Context,
					PVCName:          pvc.Name,
					PVCNamespaceMap:  fmt.Sprintf("%s:%s", srcApp.Namespace, tgtApp.Namespace),
					CloudStorage:     config.CloudStorage,
					RcloneConfigFile: config.RcloneConfigFile,
					Encrypt:          true,
					// Keep the encrypted objects at rest so the inspection step below
					// can prove they are ciphertext. Without this, crane cleans up the
					// bucket after a successful transfer and there is nothing to inspect.
					KeepCloudData: true,
				}
				log.Printf("Transferring PVC %s via encrypted indirect S3", pvc.Name)
				Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred(),
					"encrypted indirect transfer-pvc should succeed for PVC %q", pvc.Name)
			}

			By("Wait for the ephemeral indirect-transfer pods to be removed")
			Eventually(func() (string, error) {
				return kubectlSrc.Run("get", "pods", "-n", srcApp.Namespace, "-o", "name")
			}, "120s", "3s").ShouldNot(ContainSubstring("rclone"))
			Eventually(func() (string, error) {
				return kubectlTgt.Run("get", "pods", "-n", tgtApp.Namespace, "-o", "name")
			}, "120s", "3s").ShouldNot(ContainSubstring("rclone"))

			By("Verify all destination PVCs exist and contain data")
			tgtPVCs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
			Expect(err).NotTo(HaveOccurred())
			Expect(VerifyPVCsExistByName(pvcs, tgtPVCs)).NotTo(HaveOccurred())
			var dataPVC string
			for _, pvc := range tgtPVCs {
				Expect(VerifyPVCSchedulable(scenario.KubectlTgt.Context, tgtApp.Namespace, pvc.Name)).NotTo(HaveOccurred())
				mountPath := "/var/lib/mysql"
				if strings.Contains(pvc.Name, "data1") {
					mountPath = "/test-data"
					dataPVC = pvc.Name
				}
				Expect(VerifyPVCHasData(kubectlTgt, tgtApp.Namespace, pvc.Name, mountPath)).NotTo(HaveOccurred())
			}
			Expect(dataPVC).NotTo(BeEmpty(), "expected to find the migrated /test-data PVC (<app>-data1) on target")

			By("Verify the decrypted data on the target matches the source (encrypt->decrypt round-trip)")
			const verifierPod = "mta-914-verifier"
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
      mountPath: /test-data
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
`, verifierPod, tgtApp.Namespace, dataPVC)
			Expect(kubectlTgt.ApplyYAMLSpec(verifierPodYAML, tgtApp.Namespace)).NotTo(HaveOccurred())
			DeferCleanup(func() {
				if _, err := kubectlTgt.Run("delete", "pod", verifierPod, "-n", tgtApp.Namespace, "--ignore-not-found", "--wait=true"); err != nil {
					log.Printf("cleanup verifier pod %q: %v", verifierPod, err)
				}
			})
			_, err = kubectlTgt.Run("wait", "--for=condition=Ready", "pod/"+verifierPod, "-n", tgtApp.Namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())
			tgtMD5, err := md5OfFileInPod(kubectlTgt, tgtApp.Namespace, verifierPod, testDataFile)
			Expect(err).NotTo(HaveOccurred())
			Expect(tgtMD5).To(Equal(srcMD5), "decrypted target data should match the source test-data file")
			log.Printf("Round-trip MD5 match: %s", tgtMD5)

			// The real proof of encryption: read the objects crane left in the bucket
			// via the *plain* S3 remote (no crypt overlay) and confirm they are
			// ciphertext with obfuscated names. Without this, a passing round-trip
			// alone would not prove anything was actually encrypted.
			By("Inspect the raw objects at rest in S3 and verify they are ciphertext")
			const inspectSecret = "rclone-inspect-conf"
			const inspectPod = "mta-914-rclone-inspect"
			if _, err := kubectlTgt.Run("delete", "secret", inspectSecret, "-n", namespace, "--ignore-not-found=true"); err != nil {
				log.Printf("pre-clean of inspect secret: %v", err)
			}
			_, err = kubectlTgt.Run("create", "secret", "generic", inspectSecret,
				"-n", namespace, "--from-file=rclone.conf="+config.RcloneConfigFile)
			Expect(err).NotTo(HaveOccurred(), "failed to create rclone inspect Secret")

			inspectPodYAML := fmt.Sprintf(`
apiVersion: v1
kind: Pod
metadata:
  name: %s
  namespace: %s
spec:
  containers:
  - name: rclone
    image: %s
    command: ["/bin/sh", "-c", "sleep 600"]
    env:
    - name: HOME
      value: /tmp
    volumeMounts:
    - name: cfg
      mountPath: /cfg
      readOnly: true
    - name: tmp
      mountPath: /tmp
    securityContext:
      runAsNonRoot: true
      runAsUser: 1000
      allowPrivilegeEscalation: false
      seccompProfile:
        type: RuntimeDefault
  volumes:
  - name: cfg
    secret:
      secretName: %s
  - name: tmp
    emptyDir: {}
`, inspectPod, namespace, rcloneInspectImage, inspectSecret)
			Expect(kubectlTgt.ApplyYAMLSpec(inspectPodYAML, namespace)).NotTo(HaveOccurred())
			DeferCleanup(func() {
				if _, err := kubectlTgt.Run("delete", "pod", inspectPod, "-n", namespace, "--ignore-not-found", "--wait=true"); err != nil {
					log.Printf("cleanup inspect pod %q: %v", inspectPod, err)
				}
				if _, err := kubectlTgt.Run("delete", "secret", inspectSecret, "-n", namespace, "--ignore-not-found=true"); err != nil {
					log.Printf("cleanup inspect secret %q: %v", inspectSecret, err)
				}
			})
			_, err = kubectlTgt.Run("wait", "--for=condition=Ready", "pod/"+inspectPod, "-n", namespace, "--timeout=120s")
			Expect(err).NotTo(HaveOccurred())

			// crane's crypt remote roots at "<cloud-storage>/<src-namespace>/<src-pvc>";
			// the encrypted leaf objects live directly under it. Inspect the /test-data
			// PVC, where the seeded fingerprint and the plaintext marker live.
			remotePath := fmt.Sprintf("%s/%s/%s", config.CloudStorage, srcApp.Namespace, dataPVC)
			// We kept the encrypted objects at rest for inspection; purge them via the
			// still-running inspect pod so the bucket does not accumulate stale objects
			// across runs. Registered after the pod-delete cleanup, so it runs first.
			DeferCleanup(func() {
				purge := fmt.Sprintf("export HOME=/tmp; rclone --config /cfg/rclone.conf purge %q || true",
					fmt.Sprintf("%s/%s", config.CloudStorage, srcApp.Namespace))
				if _, err := kubectlTgt.Run("exec", inspectPod, "-n", namespace, "--", "/bin/sh", "-c", purge); err != nil {
					log.Printf("cleanup: failed to purge cloud objects at %q/%s: %v", config.CloudStorage, srcApp.Namespace, err)
				}
			})
			inspectScript := fmt.Sprintf(
				"set -e; export HOME=/tmp; rm -rf /tmp/raw; mkdir -p /tmp/raw; "+
					"rclone --config /cfg/rclone.conf copy %q /tmp/raw; "+
					"echo '===NAMES_START==='; ls -1A /tmp/raw; echo '===NAMES_END==='; "+
					"echo '===GREP_START==='; grep -ra %q /tmp/raw || true; echo '===GREP_END==='",
				remotePath, encMarkerContent)
			rawOut, err := kubectlTgt.Run("exec", inspectPod, "-n", namespace, "--", "/bin/sh", "-c", inspectScript)
			Expect(err).NotTo(HaveOccurred(), "failed to inspect raw bucket objects")
			out := StripKubectlWarnings(rawOut)
			log.Printf("Raw bucket inspection output:\n%s", out)

			names := sectionBetween(out, "===NAMES_START===", "===NAMES_END===")
			grepHits := sectionBetween(out, "===GREP_START===", "===GREP_END===")

			Expect(strings.TrimSpace(names)).NotTo(BeEmpty(),
				"expected encrypted objects to exist in the bucket after the transfer")
			Expect(names).NotTo(ContainSubstring(encMarkerBase),
				"object names at rest must be encrypted: the plaintext filename should not appear")
			Expect(strings.TrimSpace(grepHits)).To(BeEmpty(),
				"data at rest must be ciphertext: no object should contain the plaintext marker content")

			By("Verify crane cleaned up its temporary indirect-transfer Secrets on both clusters")
			for _, k := range []KubectlRunner{kubectlSrc, kubectlTgt} {
				leftover, err := k.Run("get", "secrets", "-n", namespace,
					"-l", "app.kubernetes.io/component=indirect-transfer", "-o", "name")
				Expect(err).NotTo(HaveOccurred())
				Expect(strings.TrimSpace(StripKubectlWarnings(leftover))).To(BeEmpty(),
					"crane should not leave its temporary rclone Secret behind on context %q", k.Context)
			}
		})
})
