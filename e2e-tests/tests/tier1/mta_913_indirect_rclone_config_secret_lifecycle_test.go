package e2e

import (
	"bytes"
	"fmt"
	"log"
	"os/exec"
	"strings"
	"sync"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const indirectTestFileName = "testfile.txt"

// syncBuffer is a goroutine-safe bytes.Buffer. os/exec copies a command's stdout
// into the buffer from a background goroutine, so a spec that reads the buffer
// while the watch is still running (as waitUntilReady does) must synchronize
// access to avoid a data race.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// secretWatch runs a background `kubectl get secret ... -w --output-watch-events`
// that records ADDED/DELETED events for the Secrets in a namespace. It lets a spec
// prove a Secret was actually created and then deleted during a transfer, rather
// than only checking that it is absent before and after (which would also pass if
// crane never created it). The watch is not filtered by name so that waitUntilReady
// can confirm the watch is live via a throwaway sentinel Secret; the create/delete
// assertions match on the specific Secret name instead.
type secretWatch struct {
	cmd       *exec.Cmd
	buf       *syncBuffer
	context   string
	k         KubectlRunner
	namespace string
	stopped   bool
}

func startSecretWatch(k KubectlRunner, namespace string) *secretWatch {
	args := []string{
		"get", "secret", "-n", namespace,
		"--output-watch-events", "-w",
		"-o", `jsonpath={.type} {.object.metadata.name}{"\n"}`,
	}
	if k.Context != "" {
		args = append(args, "--context", k.Context)
	}
	buf := &syncBuffer{}
	cmd := exec.Command(k.Bin, args...)
	cmd.Stdout = buf
	cmd.Stderr = buf
	Expect(cmd.Start()).To(Succeed(), "failed to start secret watch on context %q", k.Context)

	w := &secretWatch{cmd: cmd, buf: buf, context: k.Context, k: k, namespace: namespace}
	// Ensure the watch process never leaks even if the spec fails before output().
	DeferCleanup(w.kill)
	// Block until the watch is actually delivering events, so the caller can create
	// the temp Secret knowing its ADDED event will be captured.
	w.waitUntilReady()
	return w
}

// readySentinelName is a throwaway Secret used only to confirm the background
// watch is delivering events. Its name differs from crane's temp Secret.
const readySentinelName = "crane-e2e-watch-ready-sentinel"

// waitUntilReady blocks until the watch has observed an event, proving it is
// registered and streaming. It creates a throwaway sentinel Secret and waits to
// see its ADDED event in the watch output, then deletes it. This replaces a fixed
// sleep with a deterministic readiness handshake.
func (w *secretWatch) waitUntilReady() {
	GinkgoHelper()
	_, err := w.k.Run("create", "secret", "generic", readySentinelName, "-n", w.namespace)
	Expect(err).NotTo(HaveOccurred(),
		"failed to create readiness sentinel Secret on context %q", w.context)
	Eventually(w.buf.String, "30s", "200ms").Should(ContainSubstring("ADDED "+readySentinelName),
		"watch on context %q should observe the readiness sentinel Secret, proving the watch is live", w.context)
	if _, err := w.k.Run("delete", "secret", readySentinelName, "-n", w.namespace, "--ignore-not-found=true"); err != nil {
		log.Printf("delete readiness sentinel Secret %q on namespace %q: %v", readySentinelName, w.namespace, err)
	}
}

// kill terminates the watch process and waits for its output to be flushed. It is
// idempotent, so calling it from both output() and DeferCleanup is safe.
func (w *secretWatch) kill() {
	if w.stopped {
		return
	}
	w.stopped = true
	if w.cmd.Process != nil {
		_ = w.cmd.Process.Kill()
		_ = w.cmd.Wait() // waits for stdout/stderr copy goroutines, so buf is safe to read
	}
}

// output stops the watch and returns everything it captured.
func (w *secretWatch) output() string {
	w.kill()
	return w.buf.String()
}

// expectCreatedThenDeleted asserts the watch observed the Secret being created and
// then deleted, in that order, and that the Secret no longer exists.
func (w *secretWatch) expectCreatedThenDeleted(secretName string) {
	out := w.output()
	added := "ADDED " + secretName
	deleted := "DELETED " + secretName
	Expect(out).To(ContainSubstring(added),
		"watch on context %q should have observed the temp Secret being created; watch output:\n%s", w.context, out)
	Expect(out).To(ContainSubstring(deleted),
		"watch on context %q should have observed the temp Secret being deleted; watch output:\n%s", w.context, out)
	Expect(strings.Index(out, added)).To(BeNumerically("<", strings.Index(out, deleted)),
		"watch on context %q should observe ADDED before DELETED; watch output:\n%s", w.context, out)

	// Assert the Secret is actually gone
	secretOut, err := w.k.Run("get", "secret", secretName, "-n", w.namespace, "--ignore-not-found=true", "-o", "name")
	Expect(err).NotTo(HaveOccurred())
	Expect(strings.TrimSpace(secretOut)).To(BeEmpty(),
		"temp Secret %q should not exist on namespace %q", secretName, w.namespace)
}

// indirectApp holds the handles produced by deployIndirectApp for a single spec.
type indirectApp struct {
	scenario   MigrationScenario
	srcApp     K8sDeployApp
	tgtApp     K8sDeployApp
	kubectlSrc KubectlRunner
	kubectlTgt KubectlRunner
	pvcName    string
	tmpSecret  string
	srcMD5     string
	workDir    string
}

func deployIndirectApp(namespace, tempPrefix string) indirectApp {
	// "app-with-empty-pvc" is the base k8sdeploy template (its PVC starts empty);
	// add_data:true below seeds a known file into it, so the PVC is non-empty at
	// transfer time. That seeded data is what we checksum on source and verify on
	// target.
	appName := "app-with-empty-pvc"

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
		"app_name":       appName,
		"add_data":       "true",
		"file_name":      indirectTestFileName,
		"file_size":      10,
	}

	By("Grant namespace-admin permissions to the non-admin user on source and target")
	kubectlSrc, kubectlTgt, rbacCleanup, err := SetupActiveKubectlRunners(scenario, namespace)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(rbacCleanup)

	paths, err := NewScenarioPaths(tempPrefix)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		By("Cleanup source and target resources")
		if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
			log.Printf("cleanup: %v", err)
		}
	})
	DeferCleanup(func() {
		By("Delete test namespace on source and target (wait for completion)")
		for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
			if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
				log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
			}
		}
	})

	By("Deploy a source app with a PVC and seed it with known data")
	Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

	By("Get the source file MD5 checksum")
	srcMD5, err := MD5SumFile(kubectlSrc, srcApp.Namespace, appName, "/data/"+indirectTestFileName)
	Expect(err).NotTo(HaveOccurred())
	Expect(srcMD5).NotTo(BeEmpty(), "expected to compute an MD5 checksum on source")
	log.Printf("Source MD5 checksum: %s\n", srcMD5)

	By("List the PVC created by the source app")
	pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
	Expect(err).NotTo(HaveOccurred())
	Expect(pvcs).To(HaveLen(1), "expected exactly one PVC in namespace %q", srcApp.Namespace)
	pvcName := pvcs[0].Name
	log.Printf("Found PVC %s in namespace %q\n", pvcName, srcApp.Namespace)

	return indirectApp{
		scenario:   scenario,
		srcApp:     srcApp,
		tgtApp:     tgtApp,
		kubectlSrc: kubectlSrc,
		kubectlTgt: kubectlTgt,
		pvcName:    pvcName,
		// crane derives the temp Secret name from the source PVC name:
		//   crane-rclone-config-<getValidatedResourceName(sourcePVC)>
		// so the name is exactly crane-rclone-config-<pvcName>. crane creates this
		// Secret on BOTH the source and destination namespaces from the
		// --rclone-config-file contents, and deletes both when the command returns
		// (via deferred cleanup, on success or failure).
		tmpSecret: "crane-rclone-config-" + pvcName,
		srcMD5:    srcMD5,
		workDir:   paths.TempDir,
	}
}

// expectTempSecretAbsent asserts crane's temp rclone Secret is absent in the
// given namespace on both the source and target clusters.
func expectTempSecretAbsent(app indirectApp, namespace, when string) {
	for _, k := range []KubectlRunner{app.kubectlSrc, app.kubectlTgt} {
		out, err := k.Run("get", "secret", app.tmpSecret, "-n", namespace, "--ignore-not-found=true", "-o", "name")
		Expect(err).NotTo(HaveOccurred())
		Expect(strings.TrimSpace(out)).To(BeEmpty(),
			"temp Secret %q should not exist %s on context %q", app.tmpSecret, when, k.Context)
	}
}

var _ = Describe("Verify lifecycle of Secret created from --rclone-config-file", func() {
	It("[MTA-913] Should create a rclone config Secret from --rclone-config-file and clean it up after a successful transfer",
		Label("tier1", "pvc-transfer", "indirect"), func() {

			if config.CloudStorage == "" || config.RcloneConfigFile == "" {
				Skip("indirect transfer not configured: requires --cloud-storage and --rclone-config-file")
			}

			namespace := "indirect-rclone-file-k8s"
			app := deployIndirectApp(namespace, "crane-indirect-rclone-file-*")

			By("Precondition: crane's temp rclone Secret does not exist on either cluster")
			expectTempSecretAbsent(app, namespace, "before transfer")

			By("Start watching the temp Secret on both clusters to observe its create/delete lifecycle")
			// startSecretWatch blocks until each watch is confirmed live, so the temp
			// Secret's ADDED event is guaranteed to be captured once crane creates it.
			watchSrc := startSecretWatch(app.kubectlSrc, namespace)
			watchTgt := startSecretWatch(app.kubectlTgt, namespace)

			By("Run crane transfer-pvc in indirect mode using --rclone-config-file")
			runner := app.scenario.CraneNonAdmin
			runner.WorkDir = app.workDir
			Expect(runner.TransferPVC(TransferPVCOptions{
				SourceContext:    app.srcApp.Context,
				TargetContext:    app.tgtApp.Context,
				PVCName:          app.pvcName,
				PVCNamespaceMap:  fmt.Sprintf("%s:%s", app.srcApp.Namespace, app.tgtApp.Namespace),
				CloudStorage:     config.CloudStorage,
				RcloneConfigFile: config.RcloneConfigFile,
			})).NotTo(HaveOccurred(), "indirect transfer-pvc should succeed with a valid --rclone-config-file")

			By("Verify the temp Secret was created and then deleted on both clusters")
			// crane creates and deletes the Secret synchronously within the
			// TransferPVC call above, so both events are already captured by now.
			watchSrc.expectCreatedThenDeleted(app.tmpSecret)
			watchTgt.expectCreatedThenDeleted(app.tmpSecret)

			By("Wait for the ephemeral indirect-transfer pods to be removed")
			Eventually(func() (string, error) {
				return app.kubectlSrc.Run("get", "pods", "-n", app.srcApp.Namespace, "-o", "name")
			}, "120s", "3s").ShouldNot(ContainSubstring("rclone"))
			Eventually(func() (string, error) {
				return app.kubectlTgt.Run("get", "pods", "-n", app.tgtApp.Namespace, "-o", "name")
			}, "120s", "3s").ShouldNot(ContainSubstring("rclone"))
		})

	It("[MTA-913] Should clean up the rclone config Secret on both clusters when the transfer fails",
		Label("tier1", "pvc-transfer", "indirect"), func() {

			if config.CloudStorage == "" || config.RcloneConfigFile == "" {
				Skip("indirect transfer not configured: requires --cloud-storage and --rclone-config-file")
			}

			namespace := "indirect-rclone-file-fail-k8s"
			app := deployIndirectApp(namespace, "crane-indirect-rclone-file-fail-*")

			By("Precondition: crane's temp rclone Secret does not exist on either cluster")
			expectTempSecretAbsent(app, namespace, "before transfer")

			By("Start watching the temp Secret on both clusters to observe its create/delete lifecycle")
			watchSrc := startSecretWatch(app.kubectlSrc, namespace)
			watchTgt := startSecretWatch(app.kubectlTgt, namespace)

			By("Run crane transfer-pvc with a valid config file but an unknown cloud-storage remote")
			runner := app.scenario.CraneNonAdmin
			runner.WorkDir = app.workDir
			// Pointing --cloud-storage at a remote that is not defined in the rclone
			// config makes that upload pod fail deterministically *after* the Secret
			// has been created, which is exactly the deferred cleanup-on-error path
			// we want to exercise.
			err := runner.TransferPVC(TransferPVCOptions{
				SourceContext:    app.srcApp.Context,
				TargetContext:    app.tgtApp.Context,
				PVCName:          app.pvcName,
				PVCNamespaceMap:  fmt.Sprintf("%s:%s", app.srcApp.Namespace, app.tgtApp.Namespace),
				CloudStorage:     "crane-e2e-nonexistent-remote:crane-indirect-e2e",
				RcloneConfigFile: config.RcloneConfigFile,
			})
			Expect(err).To(HaveOccurred(),
				"transfer should fail when --cloud-storage references a remote not defined in the rclone config")

			By("Verify the temp Secret was created and then deleted on both clusters despite the failure")
			// Proves crane's deferred cleanup runs on the error path: the Secret is
			// created (ADDED) and then removed (DELETED) even though the transfer
			// failed, so no credential Secret is leaked.
			watchSrc.expectCreatedThenDeleted(app.tmpSecret)
			watchTgt.expectCreatedThenDeleted(app.tmpSecret)
		})
})
