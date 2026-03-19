package e2e

import (
	"os"

	"github.com/konveyor/crane/e2e/config"
	. "github.com/konveyor/crane/e2e/framework"
	"github.com/konveyor/crane/e2e/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

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
		GinkgoWriter.Printf("Deploying app on source cluster: %s\n", srcApp.Name)

		Expect(srcApp.Deploy()).NotTo(HaveOccurred())
		GinkgoWriter.Printf("Validating app on source cluster: %s\n", srcApp.Name)
		Expect(srcApp.Validate()).NotTo(HaveOccurred())

		GinkgoWriter.Printf("Scaling down deployment %s on source cluster to 0\n", deploymentName)
		kubectl := KubectlRunner{
			Bin:     "kubectl",
			Context: config.SourceContext,
		}
		Expect(kubectl.ScaleDeployment(srcApp.Namespace, deploymentName, 0)).NotTo(HaveOccurred())
		GinkgoWriter.Printf("Deployment %s scaled down succesfully\n", deploymentName)
		GinkgoWriter.Printf("Exporting app from source cluster: %s\n", srcApp.Name)
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
		GinkgoWriter.Printf("Exporting app to: %s\n", exportDir)
		Expect(runner.Export(srcApp.Namespace, exportDir)).NotTo(HaveOccurred())
		exportFiles, err := utils.ListFilesRecursively(exportDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(exportFiles).NotTo(ContainSubstring("(no files)"), "expected crane export to produce files in export dir")
		GinkgoWriter.Printf("Exported files:\n%s\n", exportFiles)
		GinkgoWriter.Printf("Transforming app from: %s\n", exportDir)
		Expect(runner.Transform(exportDir, transformDir)).NotTo(HaveOccurred())
		transformFiles, err := utils.ListFilesRecursively(transformDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(transformFiles).NotTo(ContainSubstring("(no files)"), "expected crane transform to produce files in transform dir")
		GinkgoWriter.Printf("Transformed files:\n%s\n", transformFiles)
		Expect(runner.Apply(exportDir, transformDir, outputDir)).NotTo(HaveOccurred())
		outputFiles, err := utils.ListFilesRecursively(outputDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(outputFiles).NotTo(ContainSubstring("(no files)"), "expected crane apply to produce files in output dir")
		GinkgoWriter.Printf("Rendered output files:\n%s\n", outputFiles)
		GinkgoWriter.Printf("Applying app to target cluster: %s\n", outputDir)

		kubectl = KubectlRunner{
			Bin:     "kubectl",
			Context: config.TargetContext,
		}
		Expect(kubectl.CreateNamespace(srcApp.Namespace)).NotTo(HaveOccurred())
		Expect(kubectl.ApplyDir(outputDir)).NotTo(HaveOccurred())

		GinkgoWriter.Printf("Scaling deployment %s to 1", deploymentName)

		Expect(kubectl.ScaleDeployment(namespace, deploymentName, 1)).NotTo(HaveOccurred())
		tgtApp := K8sDeployApp{
			Name:      "simple-nginx-nopv",
			Namespace: srcApp.Namespace,
			Bin:       config.K8sDeployBin,
			Context:   config.TargetContext,
		}
		Eventually(func() error {
			return tgtApp.Validate()
		}, "2m", "10s").Should(Succeed())

		DeferCleanup(func() {
			_ = os.RemoveAll(tempDir)
			_ = srcApp.Cleanup()
			_ = tgtApp.Cleanup()
		})
	})
})
