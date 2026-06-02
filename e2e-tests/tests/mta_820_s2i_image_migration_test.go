package e2e

import (
	"fmt"
	"log"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("OCP image migration - S2I/BuildConfig-based image", func() {
	It("[MTA-820] Should migrate a S2I/BuildConfig-built image from source to target OCP cluster",
		Label("tier0", "ocp", "BUG crane-plugin-openshift#25", "BUG crane#331"),
		func() {
			// App: dockerbuild role — triggers a Dockerfile-strategy BuildConfig
			// that builds centos+httpd and pushes to an ImageStream named "centos".
			// This is the canonical S2I/internal-registry scenario for MTA-820.
			appName := "dockerbuild"
			namespace := appName

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
			runner := scenario.Crane

			srcApp.ExtraVars = map[string]any{
				"docker_image":       "centos",
				"docker_image_tag":   "7",
				"output_imagestream": "centos",
				"docker_file": `FROM quay.io/centos/centos:latest
RUN yum install -y httpd`,
			}
			tgtApp.ExtraVars = map[string]any{
				"docker_image":       "centos",
				"docker_image_tag":   "7",
				"output_imagestream": "centos",
			}

			By("Prepare source app (triggers BuildConfig, waits for build, populates ImageStream)")
			log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
			Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())
			log.Printf("Source app %s prepared successfully\n", srcApp.Name)

			paths, err := NewScenarioPaths("crane-export-ocp-s2i-*")
			Expect(err).NotTo(HaveOccurred())
			DeferCleanup(func() {
				By("Cleanup source and target resources")
				if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
					log.Printf("cleanup: %v", err)
				}
			})

			runner.WorkDir = paths.TempDir

			By("Run crane export (admin context — captures ImageStream + BuildConfig)")
			log.Printf("Running crane export for namespace %s\n", namespace)
			Expect(runner.Export(namespace, paths.ExportDir)).NotTo(HaveOccurred())
			log.Printf("crane export complete\n")

			By("Run crane transform --stage 10_KubernetesPlugin")
			Expect(runner.TransformStage(paths.ExportDir, paths.TransformDir, "10_KubernetesPlugin")).NotTo(HaveOccurred())

			// Workaround for crane#331: OpenShiftPlugin is not auto-staged and must be
			// invoked explicitly via --stage. Without this the plugin is silently skipped
			// and ImageStream references in the output still point to the source internal
			// registry, causing image pull failures on the target cluster.
			// TODO: remove explicit --stage once crane#331 is fixed.
			By("Run crane transform --stage 20_OpenShiftPlugin (workaround: crane#331)")
			Expect(runner.TransformStage(paths.ExportDir, paths.TransformDir, "20_OpenShiftPlugin")).NotTo(HaveOccurred())

			By("Run crane apply (renders output manifests)")
			Expect(runner.Apply(paths.ExportDir, paths.TransformDir, paths.OutputDir)).NotTo(HaveOccurred())
			log.Printf("crane apply complete, output dir: %s\n", paths.OutputDir)

			// Workaround for crane-plugin-openshift#25 / #26: crane apply emits
			// ImageTag_*.yaml and ImageStreamTag_*.yaml files that the API server
			// rejects on apply. Remove them before kubectl apply.
			// TODO: remove once crane-plugin-openshift#25 / #26 are fixed.
			By("Remove ImageTag and ImageStreamTag output files (workaround: crane-plugin-openshift#25, #26)")
			outputResourcesDir := filepath.Join(paths.OutputDir, "resources", namespace)
			Expect(utils.RemoveGlob(outputResourcesDir, "ImageTag_*.yaml")).NotTo(HaveOccurred())
			Expect(utils.RemoveGlob(outputResourcesDir, "ImageStreamTag_*.yaml")).NotTo(HaveOccurred())

			By("Resolve source and target OCP registry routes")
			srcRegistry, err := GetOCRegistryURL(config.SourceContext)
			Expect(err).NotTo(HaveOccurred())
			tgtRegistry, err := GetOCRegistryURL(config.TargetContext)
			Expect(err).NotTo(HaveOccurred())

			By("Generate skopeo sync YAML from export dir (crane skopeo-sync-gen)")
			syncYAMLPath := filepath.Join(paths.TempDir, "skopeo-sync-src.yaml")
			Expect(runner.SkopeoSyncGen(paths.ExportDir, srcRegistry, syncYAMLPath)).NotTo(HaveOccurred())
			log.Printf("skopeo-sync-gen YAML written to %s\n", syncYAMLPath)

			By("Retrieve source and target OCP registry tokens (oc whoami --show-token)")
			srcToken, err := GetOCToken(config.SourceContext)
			Expect(err).NotTo(HaveOccurred())
			tgtToken, err := GetOCToken(config.TargetContext)
			Expect(err).NotTo(HaveOccurred())

			By("Run skopeo sync: copy images from source to target registry")
			// Destination is <TGT_REGISTRY>/<namespace>.
			// --scoped must NOT be used — it produces wrong image paths against OCP.
			// TLS verify is disabled on both sides; OCP registry routes use self-signed certs.
			skopeo := SkopeoRunner{}
			syncOpts := SkopeoSyncOptions{
				SrcYAMLPath:  syncYAMLPath,
				DestRegistry: fmt.Sprintf("%s/%s", tgtRegistry, namespace),
				SrcCreds:     fmt.Sprintf("unused:%s", srcToken),
				DestCreds:    fmt.Sprintf("unused:%s", tgtToken),
			}
			Expect(skopeo.Sync(syncOpts)).NotTo(HaveOccurred())
			log.Printf("skopeo sync complete\n")

			By("Apply rendered manifests to target cluster")
			log.Printf("Applying output manifests to target namespace %s\n", namespace)
			Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

			By("Validate app on target cluster")
			log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
			Eventually(tgtApp.Validate, "5m", "15s").Should(Succeed())
			log.Printf("Target validation complete for app %s\n", tgtApp.Name)
		})
})