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
		kubectlSrc := KubectlRunner{
			Bin:     "kubectl",
			Context: config.SourceContext,
		}

		kubectlTgt := KubectlRunner{
			Bin:     "kubectl",
			Context: config.TargetContext,
		}

		tgtApp := K8sDeployApp{
			Name:      "simple-nginx-nopv",
			Namespace: srcApp.Namespace,
			Bin:       config.K8sDeployBin,
			Context:   config.TargetContext,
		}
		By("Prepare source app")
		GinkgoWriter.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrc, deploymentName)).NotTo(HaveOccurred())
		GinkgoWriter.Printf("Source app %s prepared successfully\n", srcApp.Name)

		tempDir, err := utils.CreateTempDir("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		exportDir := tempDir + "/export"
		transformDir := tempDir + "/transform"
		outputDir := tempDir + "/output"
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			GinkgoWriter.Println("Starting cleanup...")

			GinkgoWriter.Printf("Removing temp dir: %s\n", tempDir)
			_ = os.RemoveAll(tempDir)

			GinkgoWriter.Printf("Cleaning source app: %s/%s\n", srcApp.Namespace, srcApp.Name)
			_ = srcApp.Cleanup()

			GinkgoWriter.Printf("Cleaning target app: %s/%s\n", tgtApp.Namespace, tgtApp.Name)
			_ = tgtApp.Cleanup()

			GinkgoWriter.Println("Cleanup completed.")
		})
		runner := CraneRunner{
			Bin:           config.CraneBin,
			SourceContext: config.SourceContext,
			WorkDir:       tempDir,
		}
		By("Run crane export/transform/apply pipeline")
		GinkgoWriter.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipeline(runner, srcApp.Namespace, exportDir, transformDir, outputDir)).NotTo(HaveOccurred())
		expectAndPrintFiles("export", exportDir)
		expectAndPrintFiles("transform", transformDir)
		expectAndPrintFiles("output", outputDir)
		GinkgoWriter.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Apply rendered manifests to target")
		GinkgoWriter.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, outputDir)
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, outputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app")
		GinkgoWriter.Printf("Scaling target deployment %s to 1\n", deploymentName)
		Expect(kubectlTgt.ScaleDeployment(namespace, deploymentName, 1)).NotTo(HaveOccurred())

		GinkgoWriter.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		GinkgoWriter.Printf("Target validation completed for app %s\n", tgtApp.Name)

	})
})
