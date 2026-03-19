package e2e

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Crane migration e2e stateless app", func() {
	It("exports , transforms , renders manifests and applies to target cluster", func() {
		tempDir, err := os.MkdirTemp("", "crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		runner := CraneRunner{
			Bin:           craneBin,
			SourceContext: sourceContext,
			WorkDir:       tempDir,
		}

		exportDir := tempDir + "/export"
		transformDir := tempDir + "/transform"
		outputDir := tempDir + "/output"
		GinkgoWriter.Println("export temp dir:", exportDir)
		GinkgoWriter.Println("transform temp dir:", transformDir)
		GinkgoWriter.Println("output temp dir:", outputDir)
		ns := "simple-nginx-nopv"
		srcApp := K8sDeployApp{
			Name:      "simple-nginx-nopv",
			Namespace: ns,
			Bin:       k8sdeployBin,
			Context:   sourceContext,
		}
		Expect(srcApp.Deploy()).NotTo(HaveOccurred())
		Expect(srcApp.Validate()).NotTo(HaveOccurred())
		Expect(runner.Export(ns, exportDir)).NotTo(HaveOccurred())
		entries, err := os.ReadDir(tempDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).NotTo(BeEmpty(), "expected crane export to produce files in temp dir")
		Expect(runner.Transform(exportDir, transformDir)).NotTo(HaveOccurred())
		entries, err = os.ReadDir(transformDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).NotTo(BeEmpty(), "expected crane transform to produce files in transform dir")

		Expect(runner.Apply(exportDir, transformDir, outputDir)).NotTo(HaveOccurred())
		entries, err = os.ReadDir(outputDir)
		Expect(err).NotTo(HaveOccurred())
		Expect(entries).NotTo(BeEmpty(), "expected crane apply to produce files in output dir")

		kubectl := KubectlRunner{
			Bin:     "kubectl",
			Context: targetContext,
		}
		Expect(kubectl.CreateNamespace(ns)).NotTo(HaveOccurred())
		Expect(kubectl.ApplyDir(outputDir)).NotTo(HaveOccurred())

		tgtApp := K8sDeployApp{
			Name:      "simple-nginx-nopv",
			Namespace: ns,
			Bin:       k8sdeployBin,
			Context:   targetContext,
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
