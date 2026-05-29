package e2e

import (
	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Cluster-level export control", func() {
	It("[CA-5] Should produce no _cluster output for namespace-only workload", Label("tier0"), func() {
		appName := "simple-nginx-nopv"
		namespace := "simple-nginx-nopv"
		serviceName := "my-" + appName
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
		paths, err := NewScenarioPaths("crane-ca5-*")
		Expect(err).NotTo(HaveOccurred())
		runner := scenario.Crane

		DeferCleanup(func() {
			ScenarioCleanup(paths, srcApp, tgtApp, kubectlSrc, kubectlTgt, namespace)
		})

		By("Prepare source app")
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())

		By("Wait for source quiesce")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		By("Run crane export/transform/apply pipeline")
		Expect(RunPipeline(&runner, namespace, paths)).NotTo(HaveOccurred())

		By("Verify no cluster resources in export")
		Expect(AssertNoClusterResources(paths.ExportDir)).NotTo(HaveOccurred())

		By("Verify no cluster resources in output")
		Expect(AssertNoClusterResources(paths.OutputDir)).NotTo(HaveOccurred())

		By("Apply output to target")
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate")
		ScaleAndValidateTargetApp(kubectlTgt, tgtApp, namespace, appName)
	})

})
