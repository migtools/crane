package e2e

import (
	"os"

	"github.com/konveyor/crane/e2e/config"
	. "github.com/konveyor/crane/e2e/framework"
	"github.com/konveyor/crane/e2e/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func expectAndPrintFiles(stage, dir string) {
	GinkgoHelper()
	hasFiles, files, err := utils.HasFilesRecursively(dir)
	Expect(err).NotTo(HaveOccurred())
	Expect(hasFiles).To(BeTrue(), "expected crane %s to produce files in %s", stage, dir)
	GinkgoWriter.Printf("%s files:\n%s\n", stage, files)
}

func runCranePipeline(runner CraneRunner, namespace, exportDir, transformDir, outputDir string) {
	GinkgoHelper()
	GinkgoWriter.Printf("Running crane pipeline for namespace %s\n", namespace)

	GinkgoWriter.Printf("Export app to : %s\n", exportDir)
	Expect(runner.Export(namespace, exportDir)).NotTo(HaveOccurred())
	expectAndPrintFiles("export", exportDir)

	GinkgoWriter.Printf("Transforming app from: %s\n", exportDir)
	Expect(runner.Transform(exportDir, transformDir)).NotTo(HaveOccurred())
	expectAndPrintFiles("transform", transformDir)

	Expect(runner.Apply(exportDir, transformDir, outputDir)).NotTo(HaveOccurred())
	expectAndPrintFiles("output", outputDir)
	GinkgoWriter.Printf("Crane pipeline completed for namespace %s\n", namespace)
}

func prepareSourceApp(srcApp K8sDeployApp, kubectlSrc KubectlRunner, deploymentName string) {
	GinkgoHelper()
	GinkgoWriter.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
	Expect(srcApp.Deploy()).NotTo(HaveOccurred())
	Expect(srcApp.Validate()).NotTo(HaveOccurred())
	GinkgoWriter.Printf("Scaling down deployment %s on source cluster to 0\n", deploymentName)
	Expect(kubectlSrc.ScaleDeployment(srcApp.Namespace, deploymentName, 0)).NotTo(HaveOccurred())
	GinkgoWriter.Printf("Source app %s prepared successfully\n", srcApp.Name)
}

func applyAndVerifyOnTarget(kubectlTgt KubectlRunner, tgtApp K8sDeployApp, namespace, deploymentName, outputDir string) {
	GinkgoHelper()
	GinkgoWriter.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, outputDir)
	Expect(kubectlTgt.CreateNamespace(namespace)).NotTo(HaveOccurred())
	Expect(kubectlTgt.ApplyDir(outputDir)).NotTo(HaveOccurred())
	GinkgoWriter.Printf("Scaling target deployment %s to 1\n", deploymentName)
	Expect(kubectlTgt.ScaleDeployment(namespace, deploymentName, 1)).NotTo(HaveOccurred())
	GinkgoWriter.Printf("Validating app %s on target cluster\n", tgtApp.Name)
	Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
	GinkgoWriter.Printf("Target validation completed for app %s\n", tgtApp.Name)
}

var _ = Describe("Stateless migration", func() {
	It("[MTC-329] nginx app quiesce pod and apply to target cluster", func() {
		appName := "simple-nginx-nopv"
		namespace := "simple-nginx-nopv"
		deploymentName := appName + "-deployment"
		srcApp := K8sDeployApp{
			Name:      appName,
			Namespace: namespace,
			Bin:       config.K8sDeployBin,
			Context:   config.SourceContext,
		}
		kubectl := KubectlRunner{
			Bin:     "kubectl",
			Context: config.SourceContext,
		}
		prepareSourceApp(srcApp, kubectl, deploymentName)
		tempDir, err := utils.CreateTempDir("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		exportDir := tempDir + "/export"
		transformDir := tempDir + "/transform"
		outputDir := tempDir + "/output"

		runner := CraneRunner{
			Bin:           config.CraneBin,
			SourceContext: config.SourceContext,
			WorkDir:       tempDir,
		}

		runCranePipeline(runner, srcApp.Namespace, exportDir, transformDir, outputDir)

		kubectl = KubectlRunner{
			Bin:     "kubectl",
			Context: config.TargetContext,
		}

		tgtApp := K8sDeployApp{
			Name:      "simple-nginx-nopv",
			Namespace: srcApp.Namespace,
			Bin:       config.K8sDeployBin,
			Context:   config.TargetContext,
		}

		applyAndVerifyOnTarget(kubectl, tgtApp, namespace, deploymentName, outputDir)
		DeferCleanup(func() {
			_ = os.RemoveAll(tempDir)
			_ = srcApp.Cleanup()
			_ = tgtApp.Cleanup()
		})
	})
})
