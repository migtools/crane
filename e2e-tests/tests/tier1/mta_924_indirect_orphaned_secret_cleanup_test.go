package e2e

import (
	"fmt"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// staleRcloneConf is deliberately invalid rclone config. It is non-empty (so it
// passes crane's key-presence validation of the Secret), but it is not usable by
// rclone. It stands in for whatever a crashed prior run happened to leave behind
// in the orphaned Secret. Because it is unusable, a transfer that succeeds proves
// crane overwrote it with the real --rclone-config-file contents rather than
// reusing the orphan as-is.
const staleRcloneConf = `# stale rclone config left behind by a simulated crashed crane run
this-is-not-a-valid-rclone-remote
`

// seedOrphanedRcloneSecret pre-creates crane's temp rclone Secret on the given
// cluster/namespace with crane's own labels and deliberately-invalid data. This
// simulates the state after a crane run is killed mid-transfer (SIGKILL / crash):
// the deferred cleanup never runs, so the crane-rclone-config-<pvc> Secret is
// orphaned. crane sets no ownerReferences and installs no finalizer, so nothing
// reclaims it automatically; only a later successful run touching the same source
// PVC adopts and deletes it.
func seedOrphanedRcloneSecret(k KubectlRunner, namespace, secretName, pvcName string) {
	GinkgoHelper()
	manifest := fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: %s
  namespace: %s
  labels:
    app.kubernetes.io/name: crane
    app.kubernetes.io/component: indirect-transfer
    app.kubernetes.io/managed-by: crane
    app.konveyor.io/created-for-pvc: %s
stringData:
  rclone.conf: |
%s
`, secretName, namespace, pvcName, indentForYAML(staleRcloneConf, "    "))

	out, err := k.RunWithStdin(manifest, "apply", "-f", "-", "-n", namespace)
	Expect(err).NotTo(HaveOccurred(),
		"failed to seed orphaned rclone Secret %q on context %q: %s", secretName, k.Context, out)
}

// indentForYAML indents every line of s by prefix so it can be embedded under a
// YAML block scalar (the `rclone.conf: |` key above).
func indentForYAML(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = prefix + l
	}
	return strings.Join(lines, "\n")
}

// expectTempSecretPresent asserts crane's temp rclone Secret exists in the given
// namespace on both the source and target clusters. It is the precondition
// counterpart to expectTempSecretAbsent (defined in the MTA-913 lifecycle test):
// here we are proving the orphan is really there before the transfer, so that the
// post-transfer absence check genuinely demonstrates cleanup.
func expectTempSecretPresent(app indirectApp, namespace, when string) {
	GinkgoHelper()
	for _, k := range []KubectlRunner{app.kubectlSrc, app.kubectlTgt} {
		out, err := k.Run("get", "secret", app.tmpSecret, "-n", namespace, "--ignore-not-found=true", "-o", "name")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).NotTo(BeEmpty(),
			"seeded orphan Secret %q should exist %s on context %q", app.tmpSecret, when, k.Context)
	}
}

var _ = Describe("Clean up an orphaned rclone config Secret left by a crashed transfer", func() {
	It("[MTA-924] Should adopt and delete an orphaned rclone config Secret on the next successful transfer",
		Label("tier1", "pvc-transfer", "indirect"), func() {

			if config.CloudStorage == "" || config.RcloneConfigFile == "" {
				Skip("indirect transfer not configured: requires --cloud-storage and --rclone-config-file")
			}

			namespace := "indirect-orphan-secret-k8s"
			app := deployIndirectApp(namespace, "crane-indirect-orphan-secret-*")

			By("Simulate a crashed prior run: seed an orphaned temp rclone Secret on both clusters")
			// crane has no signal handler, finalizer, or owner-reference GC, so a
			// SIGKILL before its deferred cleanup runs leaves this Secret behind.
			// We reproduce that end state directly: the exact deterministic name
			// (crane-rclone-config-<pvc>), crane's labels, and unusable data.
			seedOrphanedRcloneSecret(app.kubectlSrc, namespace, app.tmpSecret, app.pvcName)
			seedOrphanedRcloneSecret(app.kubectlTgt, namespace, app.tmpSecret, app.pvcName)

			By("Precondition: the orphaned Secret exists on both clusters before the transfer")
			expectTempSecretPresent(app, namespace, "before transfer")

			By("Run crane transfer-pvc in indirect mode using --rclone-config-file")
			runner := app.scenario.CraneNonAdmin
			runner.WorkDir = app.workDir
			targetStorageClass, err := DefaultStorageClassName(app.scenario.KubectlTgt.Context)
			Expect(err).NotTo(HaveOccurred())
			// The transfer succeeding is the proof of adoption: crane hits
			// AlreadyExists on the pre-seeded Secret, overwrites its data with the
			// real --rclone-config-file contents (Get+Update in place), and uses it.
			// If crane had instead errored on the existing Secret, or reused the
			// invalid seeded data, this transfer would fail.
			Expect(runner.TransferPVC(TransferPVCOptions{
				SourceContext:    app.srcApp.Context,
				TargetContext:    app.tgtApp.Context,
				PVCName:          app.pvcName,
				PVCNamespaceMap:  fmt.Sprintf("%s:%s", app.srcApp.Namespace, app.tgtApp.Namespace),
				DestStorageClass: targetStorageClass,
				CloudStorage:     config.CloudStorage,
				RcloneConfigFile: config.RcloneConfigFile,
			})).NotTo(HaveOccurred(),
				"indirect transfer should adopt the orphaned Secret and succeed, not fail on the pre-existing Secret")

			By("Verify the orphaned Secret was cleaned up on both clusters after the transfer")
			// The whole point: the orphan left by the simulated crash is gone once a
			// subsequent successful run completes, so no credential Secret is leaked.
			expectTempSecretAbsent(app, namespace, "after a successful transfer that adopted the orphan")

			By("Wait for the ephemeral indirect-transfer pods to be removed")
			Eventually(func() (string, error) {
				return app.kubectlSrc.Run("get", "pods", "-n", app.srcApp.Namespace, "-o", "name")
			}, "120s", "3s").ShouldNot(ContainSubstring("rclone"))
			Eventually(func() (string, error) {
				return app.kubectlTgt.Run("get", "pods", "-n", app.tgtApp.Namespace, "-o", "name")
			}, "120s", "3s").ShouldNot(ContainSubstring("rclone"))
		})
})
