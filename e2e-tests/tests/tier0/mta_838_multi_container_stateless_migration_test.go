package e2e

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Multi-container pod migration", func() {
	It("[MTA-838] nginx+sidecar deployment migrates to target with all containers intact as namespace-admin user", Label("tier0"), func() {
		appName := "sidecar-app"
		namespace := appName
		deploymentName := appName + "-deployment"

		const (
			mainContainerName    = "main"
			sidecarContainerName = "sidecar"
			mainImage            = "quay.io/migqe/nginx-unprivileged:1.23"
			sidecarImage         = "busybox:latest"
		)

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
		runner := scenario.CraneNonAdmin

		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		By("Grant ns admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, cleanup, err := SetupActiveKubectlRunners(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Delete test namespace on source and target (wait for completion)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(cleanup)

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		By("Verify both containers exist on source before export")
		deploymentOut, err := kubectlSrcNonAdmin.Run("get", "deployment", deploymentName, "-n", namespace, "-o", "json")
		Expect(err).NotTo(HaveOccurred(), "should be able to get deployment from source")
		log.Printf("deployment %s found on source cluster\n", deploymentName)

		var deploymentSrc map[string]any
		Expect(json.Unmarshal([]byte(deploymentOut), &deploymentSrc)).NotTo(HaveOccurred())
		spec, ok := deploymentSrc["spec"].(map[string]any)
		Expect(ok).To(BeTrue(), "spec should be a map")
		template, ok := spec["template"].(map[string]any)
		Expect(ok).To(BeTrue(), "template should be a map")
		podSpec, ok := template["spec"].(map[string]any)
		Expect(ok).To(BeTrue(), "podSpec should be a map")
		containers, ok := podSpec["containers"].([]any)
		Expect(ok).To(BeTrue(), "containers should be a slice")
		Expect(containers).To(HaveLen(2), "source deployment should have 2 containers")
		log.Printf("Source deployment has %d containers\n", len(containers))

		runner.WorkDir = paths.TempDir

		By("Wait for source quiesce to stabilize before export")
		WaitForSourceQuiesce(kubectlSrcNonAdmin, namespace, "app="+appName, "my-"+appName)

		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Verify deployment manifest is present in output directory")
		deploymentGlob := filepath.Join(paths.OutputDir, "resources", namespace, "Deployment_*.yaml")
		deploymentMatches, err := filepath.Glob(deploymentGlob)
		Expect(err).NotTo(HaveOccurred())
		Expect(deploymentMatches).NotTo(BeEmpty(), "expected deployment manifest in output directory")
		log.Printf("deployment manifest found in output: %v\n", deploymentMatches)

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
		Expect(ApplyOutputToTargetNonAdmin(kubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app")
		log.Printf("Scaling target deployment %s to 1\n", deploymentName)
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())

		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "5m", "10s").Should(Succeed())
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)

		By("Verify both containers are present on target deployment")
		deploymentJson, err := kubectlTgtNonAdmin.Run("get", "deployment", deploymentName, "-n", namespace, "-o", "json")
		Expect(err).NotTo(HaveOccurred(), "should be able to get deployment from target")

		var deploymentTgt map[string]any
		Expect(json.Unmarshal([]byte(deploymentJson), &deploymentTgt)).NotTo(HaveOccurred())
		tgtSpec, ok := deploymentTgt["spec"].(map[string]any)
		Expect(ok).To(BeTrue(), "spec should be a map")
		tgtTemplate, ok := tgtSpec["template"].(map[string]any)
		Expect(ok).To(BeTrue(), "template should be a map")
		tgtPodSpec, ok := tgtTemplate["spec"].(map[string]any)
		Expect(ok).To(BeTrue(), "podSpec should be a map")
		tgtContainers, ok := tgtPodSpec["containers"].([]any)
		Expect(ok).To(BeTrue(), "containers should be a slice")
		Expect(tgtContainers).To(HaveLen(2), "target deployment should have 2 containers")
		log.Printf("target deployment has %d containers\n", len(tgtContainers))

		containersByName := make(map[string]map[string]any)
		for _, c := range tgtContainers {
			container, ok := c.(map[string]any)
			if !ok {
				continue
			}
			name, _ := container["name"].(string)
			containersByName[name] = container
		}

		By("Verify main container name and image match source")
		mainContainer, ok := containersByName[mainContainerName]
		Expect(ok).To(BeTrue(), "main container should be present on target")
		Expect(mainContainer["image"]).To(Equal(mainImage), fmt.Sprintf("main container image should be %s", mainImage))
		log.Printf("Main container verified: name=%s image=%s\n", mainContainerName, mainImage)

		By("Verify sidecar container name and image match source")
		sidecarContainer, ok := containersByName[sidecarContainerName]
		Expect(ok).To(BeTrue(), "sidecar container should be present on target")
		Expect(sidecarContainer["image"]).To(Equal(sidecarImage), fmt.Sprintf("sidecar container image should be %s", sidecarImage))
		log.Printf("sidecar container verified: name=%s image=%s\n", sidecarContainerName, sidecarImage)
	})
})
