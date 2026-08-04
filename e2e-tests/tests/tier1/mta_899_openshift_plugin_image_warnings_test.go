package e2e

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// This test verifies the officially supported answer to crane#681 (crane does not
// migrate image content): the optional OpenShiftPlugin, once installed via
// `crane plugin-manager add OpenShiftPlugin`, warns during `crane transform` when
// it encounters an ImageStream. It is OpenShift-only (BuildConfig/ImageStream are
// OCP APIs) and is skipped on non-OpenShift clusters.
var _ = Describe("OpenShiftPlugin image resource warnings", func() {
	It("[MTA-899] warns about an ImageStream it does not migrate (crane#681)", Label("tier1", "openshift-plugin"), func() {
		appName := "dockerbuild"
		namespace := "image-resource-warnings"
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		srcApp := scenario.SrcApp
		kubectlSrc := scenario.KubectlSrc

		if !kubectlSrc.IsOpenShift() {
			Skip("BuildConfig/ImageStream are OpenShift-only APIs; skipping on non-OpenShift clusters")
		}

		paths, err := NewScenarioPaths("crane-image-warning-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup source resources")
			if err := srcApp.Cleanup(); err != nil {
				log.Printf("cleanup: %v", err)
			}
			if paths.TempDir != "" {
				if err := os.RemoveAll(paths.TempDir); err != nil {
					log.Printf("cleanup: failed to remove temp dir %q: %v", paths.TempDir, err)
				}
			}
		})

		By("Deploy a BuildConfig + ImageStream on the source cluster")
		Expect(srcApp.Deploy()).NotTo(HaveOccurred())

		pluginDir := filepath.Join(paths.TempDir, "plugins")
		Expect(os.MkdirAll(pluginDir, 0o755)).NotTo(HaveOccurred())

		By("Install OpenShiftPlugin into an isolated plugin dir")
		installCmd := exec.Command(config.CraneBin, "plugin-manager", "add", "OpenShiftPlugin", "--plugin-dir", pluginDir)
		installOut, err := installCmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "failed to install OpenShiftPlugin: %s", string(installOut))

		By("Run crane export")
		runner := scenario.Crane
		runner.WorkDir = paths.TempDir
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		Expect(runner.Export(exportOpts)).NotTo(HaveOccurred())

		By("Run crane transform with OpenShiftPlugin installed and verify it warns about the ImageStream")
		transformCmd := exec.Command(config.CraneBin, "transform",
			"--export-dir", paths.ExportDir,
			"--transform-dir", paths.TransformDir,
			"--plugin-dir", pluginDir,
		)
		transformCmd.Dir = paths.TempDir
		// Don't assert on the transform command's exit status here: the plugin emits
		// its warning to output regardless of whether the overall pipeline later
		// succeeds, and that warning — not end-to-end transform success, which is
		// covered elsewhere — is what this test verifies.
		transformOut, _ := transformCmd.CombinedOutput()

		output := string(transformOut)
		Expect(output).To(ContainSubstring(fmt.Sprintf("ImageStream '%s/centos' detected", namespace)),
			"expected OpenShiftPlugin to warn about the ImageStream it does not migrate:\n%s", output)
		Expect(output).To(ContainSubstring("images from internal registry are NOT migrated automatically"))
		Expect(output).To(ContainSubstring("use tools like skopeo"))
	})
})
