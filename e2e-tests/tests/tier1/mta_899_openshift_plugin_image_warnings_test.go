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

var _ = Describe("Image resource warnings for content crane does not migrate", func() {
	It("[MTA-899] warns when an exported app contains an OCP image reference (crane#681)", Label("tier1"), func() {
		appName := "app-with-empty-pvc"
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

		By("Deploy a simple app on the source cluster")
		Expect(PrepareSourceAppNoQuiesce(srcApp)).NotTo(HaveOccurred())

		paths, err := NewScenarioPaths("crane-image-warning-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup source app and temp dir")
			if err := srcApp.Cleanup(); err != nil {
				log.Printf("cleanup: failed to remove app: %v", err)
			}
			if paths.TempDir != "" {
				if err := os.RemoveAll(paths.TempDir); err != nil {
					log.Printf("cleanup: failed to remove temp dir %q: %v", paths.TempDir, err)
				}
			}
		})

		By("Run crane export")
		exportOut, err := exec.Command(config.CraneBin, "export",
			"--context", srcApp.Context,
			"--namespace", srcApp.Namespace,
			"--export-dir", paths.ExportDir,
		).CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "crane export failed: %s", string(exportOut))

		By("Simulate the app containing an OCP image reference (ImageStream)")
		imageStreamYAML := fmt.Sprintf(`
apiVersion: image.openshift.io/v1
kind: ImageStream
metadata:
  name: demo-app
  namespace: %s
spec:
  lookupPolicy:
    local: false
`, namespace)
		resourceDir := filepath.Join(paths.ExportDir, "resources", namespace)
		Expect(os.WriteFile(
			filepath.Join(resourceDir, "ImageStream_image.openshift.io_v1_demo-app.yaml"),
			[]byte(imageStreamYAML), 0o644,
		)).NotTo(HaveOccurred())

		By("Install OpenShiftPlugin into an isolated plugin dir")
		pluginDir := filepath.Join(paths.TempDir, "plugins")
		Expect(os.MkdirAll(pluginDir, 0o755)).NotTo(HaveOccurred())
		installOut, err := exec.Command(config.CraneBin, "plugin-manager", "add", "OpenShiftPlugin", "--plugin-dir", pluginDir).CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "failed to install OpenShiftPlugin: %s", string(installOut))

		By("Run crane transform and verify it warns about the ImageStream")
		transformOut, _ := exec.Command(config.CraneBin, "transform",
			"--export-dir", paths.ExportDir,
			"--transform-dir", paths.TransformDir,
			"--plugin-dir", pluginDir,
		).CombinedOutput()

		output := string(transformOut)
		Expect(output).To(ContainSubstring(fmt.Sprintf("ImageStream '%s/demo-app' detected", namespace)),
			"expected OpenShiftPlugin to warn about the ImageStream it does not migrate:\n%s", output)
		Expect(output).To(ContainSubstring("images from internal registry are NOT migrated automatically"))
	})
})
