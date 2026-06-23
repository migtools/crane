package e2e

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("HPA migration", func() {
	It("[MTA-837] HPA is exported, transformed, and applied to target cluster", Label("tier0"), func() {
		appName := "nginx-with-hpa"
		namespace := appName
		deploymentName := appName + "-deployment"
		hpaName := appName + "-hpa"
		serviceName := "my-" + appName

		const (
			hpaMinReplicas    = 1
			hpaMaxReplicas    = 5
			hpaCPUUtilization = 50
		)

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

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		By("Verify HPA exists on source before export")
		hpaOut, err := kubectlSrc.Run("get", "hpa", hpaName, "-n", namespace, "-o", "json")
		Expect(err).NotTo(HaveOccurred(), "HPA should exist on source cluster")
		log.Printf("HPA %s found on source cluster\n", hpaName)

		var hpaSrc map[string]any
		Expect(json.Unmarshal([]byte(hpaOut), &hpaSrc)).NotTo(HaveOccurred())

		paths, err := NewScenarioPaths("crane-export-*")
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		runner := scenario.Crane
		runner.WorkDir = paths.TempDir

		By("Wait for source quiesce to stabilize before export")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Verify HPA manifest is present in output directory")
		hpaGlob := filepath.Join(paths.OutputDir, "resources", namespace, "HorizontalPodAutoscaler_*.yaml")
		hpaMatches, err := filepath.Glob(hpaGlob)
		Expect(err).NotTo(HaveOccurred())
		Expect(hpaMatches).NotTo(BeEmpty(), "expected HPA manifest in output directory")
		log.Printf("HPA manifest found in output: %v\n", hpaMatches)

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app")
		log.Printf("Scaling target deployment %s to 1\n", deploymentName)
		Expect(kubectlTgt.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())

		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)

		By("Verify HPA is present on target cluster")
		hpaJson, err := kubectlTgt.Run("get", "hpa", hpaName, "-n", namespace, "-o", "json")
		Expect(err).NotTo(HaveOccurred(), "HPA should be present on target cluster")
		log.Printf("HPA %s found on target cluster\n", hpaName)

		var hpaTgt map[string]any
		Expect(json.Unmarshal([]byte(hpaJson), &hpaTgt)).NotTo(HaveOccurred())

		By("Verify HPA scaleTargetRef references the migrated Deployment by name")
		spec, ok := hpaTgt["spec"].(map[string]any)
		Expect(ok).To(BeTrue(), "HPA spec should be a map")

		scaleTargetRef, ok := spec["scaleTargetRef"].(map[string]any)
		Expect(ok).To(BeTrue(), "HPA scaleTargetRef should be a map")
		Expect(scaleTargetRef["name"]).To(Equal(deploymentName),
			"HPA scaleTargetRef.name should match the migrated Deployment name")
		Expect(scaleTargetRef["kind"]).To(Equal("Deployment"),
			"HPA scaleTargetRef.kind should be Deployment")
		log.Printf("HPA scaleTargetRef correctly references Deployment %s\n", deploymentName)

		By("Verify HPA min/max replicas match source values")
		minReplicas, err := utils.ToInt64(spec["minReplicas"])
		Expect(err).NotTo(HaveOccurred())
		Expect(minReplicas).To(Equal(int64(hpaMinReplicas)),
			fmt.Sprintf("HPA minReplicas should be %d", hpaMinReplicas))

		maxReplicas, err := utils.ToInt64(spec["maxReplicas"])
		Expect(err).NotTo(HaveOccurred())
		Expect(maxReplicas).To(Equal(int64(hpaMaxReplicas)),
			fmt.Sprintf("HPA maxReplicas should be %d", hpaMaxReplicas))
		log.Printf("HPA min/max replicas verified: min=%d max=%d\n", minReplicas, maxReplicas)

		By("Verify HPA CPU utilization target matches source value")
		cpuTarget := utils.ExtractCPUAverageUtilization(spec)
		Expect(cpuTarget).To(Equal(int64(hpaCPUUtilization)),
			fmt.Sprintf("HPA CPU averageUtilization should be %d", hpaCPUUtilization))
		log.Printf("HPA CPU utilization target verified: %d%%\n", cpuTarget)
	})
})
