package e2e

import (
	"log"

	"github.com/konveyor/crane/e2e/config"
	. "github.com/konveyor/crane/e2e/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Stateless migration", func() {
	It("[MTC-329] nginx app quiesce pod and apply to target cluster", func() {
		appName := "simple-nginx-nopv"
		namespace := "simple-nginx-nopv"
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

		paths, err := NewScenarioPaths("crane-export-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			CleanupScenario(paths.TempDir, srcApp, tgtApp)
		})
		runner := scenario.Crane
		runner.WorkDir = paths.TempDir
		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, srcApp.Namespace, paths)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Apply rendered manifests to target")
		log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
		Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app")
		log.Printf("Scaling target deployment(s) with label app=%s to 1\n", appName)
		Expect(kubectlTgt.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())

		log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
		log.Printf("Target validation completed for app %s\n", tgtApp.Name)
	})

	// It("[MTC-129] No PVCs stage warning", func() {
	// 	appName := "simple-nginx-nopv"
	// 	namespace := "simple-nginx-nopv"
	// 	scenario := NewMigrationScenario(
	// 		appName,
	// 		namespace,
	// 		config.K8sDeployBin,
	// 		config.CraneBin,
	// 		config.SourceContext,
	// 		config.TargetContext,
	// 	)
	// 	srcApp := scenario.SrcApp
	// 	tgtApp := scenario.TgtApp
	// 	kubectlSrc := scenario.KubectlSrc
	// 	kubectlTgt := scenario.KubectlTgt
	// 	_ = kubectlTgt
	// 	By("Prepare source app")
	// 	log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
	// 	Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())
	// 	log.Printf("Source app %s prepared successfully\n", srcApp.Name)

	// 	paths, err := NewScenarioPaths("crane-export-*")
	// 	Expect(err).NotTo(HaveOccurred())
	// 	DeferCleanup(func() {
	// 		By("Cleanup source and target resources")
	// 		CleanupScenario(paths.TempDir, srcApp, tgtApp)
	// 	})
	// 	runner := scenario.Crane
	// 	runner.WorkDir = paths.TempDir
	// 	By("Run crane export/transform/apply pipeline")
	// 	Expect(RunCranePipelineWithChecks(runner, srcApp.Namespace, paths)).NotTo(HaveOccurred())

	// 	By("Transfer PVCs")
	// 	tgtIP, err := GetClusterNodeIP(tgtApp.Context)
	// 	Expect(err).NotTo(HaveOccurred())
	// 	pvcName := "simple-nginx-nopv"
	// 	opts := TransferPVCOptions{
	// 		SourceContext:   srcApp.Context,
	// 		TargetContext:   tgtApp.Context,
	// 		PVCName:         pvcName,
	// 		PVCNamespaceMap: fmt.Sprintf("%s:%s", srcApp.Namespace, srcApp.Namespace),
	// 		Endpoint:        "nginx-ingress",
	// 		IngressClass:    "nginx",
	// 		Subdomain:       fmt.Sprintf("%s.%s.%s.nip.io", pvcName, srcApp.Namespace, tgtIP),
	// 	}
	// 	By("Transfer non existent PVC and expect an error")
	// 	log.Printf("Transferring PVC %s to namespace %s on target cluster", pvcName, tgtApp.Namespace)
	// 	err = runner.TransferPVC(opts)
	// 	Expect(err).To(HaveOccurred()) // Expected to fail because there are no PVCs to transfer
	// 	Expect(err.Error()).To(ContainSubstring("unable to get source PVC"))
	// 	log.Printf("Transfer of non existent PVC failed as expected: %v", err)

	// 	By("Apply rendered manifests to target")
	// 	log.Printf("Applying rendered manifests on target namespace %s from %s\n", namespace, paths.OutputDir)
	// 	Expect(ApplyOutputToTarget(kubectlTgt, namespace, paths.OutputDir)).NotTo(HaveOccurred())

	// 	By("Scale target deployment and validate app")
	// 	log.Printf("Scaling target deployment(s) with label app=%s to 1\n", appName)
	// 	Expect(kubectlTgt.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())

	// 	log.Printf("Validating app %s on target cluster\n", tgtApp.Name)
	// 	Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
	// 	log.Printf("Target validation completed for app %s\n", tgtApp.Name)
	// })
})
