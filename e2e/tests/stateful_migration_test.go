package e2e

import (
	"fmt"
	"log"

	"github.com/konveyor/crane/e2e/config"
	. "github.com/konveyor/crane/e2e/framework"
	"github.com/konveyor/crane/e2e/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Stateful app migration", func() {
	It("[MTC-123] Migrate all of PVCs that are associated with quiesced resource", func() {
		appName := "redis"
		srcApp := K8sDeployApp{
			Name:      appName,
			Namespace: appName,
			Bin:       config.K8sDeployBin,
			Context:   config.SourceContext,
		}
		kubectlSrc := KubectlRunner{
			Bin:     "kubectl",
			Context: config.SourceContext,
		}
		tgtApp := K8sDeployApp{
			Name:      appName,
			Namespace: appName,
			Bin:       config.K8sDeployBin,
			Context:   config.TargetContext,
		}
		kubectlTgt := KubectlRunner{
			Bin:     "kubectl",
			Context: config.TargetContext,
		}

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)
		By("Scale src app to 0 replicas")
		Expect(kubectlSrc.ScaleDeployment(srcApp.Namespace, srcApp.Name, 0)).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			log.Println("Starting cleanup...")

			log.Printf("Cleaning source app: %s/%s\n", srcApp.Namespace, srcApp.Name)
			_ = srcApp.Cleanup()

			log.Printf("Cleaning target app: %s/%s\n", tgtApp.Namespace, tgtApp.Name)
			_ = tgtApp.Cleanup()
			log.Println("Cleanup completed.")
		})
		By("List pvcs in the namespace")
		pvcs, err := ListPVCs(srcApp.Namespace, "", srcApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(pvcs).NotTo(BeEmpty(), "expected at least one pvc in namespace %q", srcApp.Namespace)
		log.Printf("Found %d pvcs in namespace %q", len(pvcs), srcApp.Namespace)
		for _, pvc := range pvcs {
			log.Printf("Found pvc %s in namespace %q\n", pvc.Name, pvc.Namespace)
		}
		tempDir, err := utils.CreateTempDir("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		exportDir := tempDir + "/export"
		transformDir := tempDir + "/transform"
		outputDir := tempDir + "/output"
		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		runner := CraneRunner{
			Bin: config.CraneBin,
		}
		Expect(RunCranePipeline(runner, srcApp.Namespace, exportDir, transformDir, outputDir)).NotTo(HaveOccurred())
		expectAndPrintFiles("export", exportDir)
		expectAndPrintFiles("transform", transformDir)
		expectAndPrintFiles("output", outputDir)
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)
		By("Create namespace on target cluster")
		log.Printf("Creating ns %s on target cluster", tgtApp.Namespace)
		kubectlTgt.CreateNamespace(tgtApp.Namespace)

		By("Transfer PVCs")
		tgtIP, err := GetClusterNodeIP(config.TargetContext)
		Expect(err).NotTo(HaveOccurred())
		pvcName := pvcs[0].Name

		opts := TransferPVCOptions{
			SourceContext:   config.SourceContext,
			TargetContext:   config.TargetContext,
			PVCName:         pvcName,
			PVCNamespaceMap: fmt.Sprintf("%s:%s", srcApp.Namespace, srcApp.Namespace),
			Endpoint:        "nginx-ingress",
			IngressClass:    "nginx",
			Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvcName, srcApp.Namespace, tgtIP),
		}

		Expect(runner.TransferPVC(opts)).NotTo(HaveOccurred())
		By("List pvcs on target cluster")
		tgtpvcs, err := ListPVCs(tgtApp.Namespace, "", tgtApp.Context)
		Expect(err).NotTo(HaveOccurred())
		Expect(tgtpvcs).NotTo(BeEmpty(), "expected at least one pvc in namespace %q", tgtApp.Namespace)

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", tgtApp.Namespace, outputDir)
		Expect(ApplyOutputToTarget(kubectlTgt, tgtApp.Namespace, outputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app")
		log.Printf("Scaling target deployment(s) with label app=%s to 1\n", appName)
		Expect(kubectlTgt.ScaleDeployment(tgtApp.Namespace, appName, 1)).NotTo(HaveOccurred())

		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)

	})
})
