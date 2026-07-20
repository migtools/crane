package e2e

import (
	"log"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("CRD group filtering during export", func() {
	appName := "simple-nginx-nopv"
	namespace := "simple-nginx-nopv"
	serviceName := "my-" + appName
	It("[MTA-869A] Should skip CRD when --crd-skip-group matches", Label("tier0"), func() {
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

		paths, err := NewScenarioPaths("crane-ca10a-*")
		Expect(err).NotTo(HaveOccurred())
		crdYAML, err := utils.ReadTestdataFile("widget_crd.yaml")
		Expect(err).NotTo(HaveOccurred())
		crYAML, err := utils.ReadTestdataFile("widget_cr.yaml")
		Expect(err).NotTo(HaveOccurred())

		crd := CustomResourceDefinition{
			Name: "widgets.crane-e2e.example.com",
			YAML: crdYAML,
		}

		cr := CustomResource{
			Name:      "test-widget",
			Namespace: namespace,
			Kind:      "Widget",
			YAML:      crYAML,
			Resource:  "widgets",
		}
		excludedResource := []utils.ResourceMatch{
			{Kind: "CustomResourceDefinition", Name: crd.Name},
		}
		includedResource := []utils.ResourceMatch{
			{Kind: cr.Kind, Name: cr.Name, Scope: namespace},
		}
		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir,
			CRDSkipGroups: []string{"crane-e2e.example.com"}}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}

		DeferCleanup(func() {
			if err := ResourceCleanup([]KubectlRunner{kubectlSrc, kubectlTgt}, []Resource{cr, crd}); err != nil {
				log.Printf("Resources cleanup: %v", err)
			}
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("Scenario cleanup: %v", err)
			}
		})

		By("Deploying app on source cluster")
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())

		By("Creating Widget CRD on source")
		Expect(crd.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Waiting for CRD to be established")
		Expect(crd.WaitForEstablished(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating Widget custom resource in namespace")
		Expect(cr.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Waiting for source pods and endpoints to drain")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		By("Running crane export with --crd-skip-group, transform, apply")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verifying CRD is excluded from export")
		found, err := utils.AssertResourcesDontExist(filepath.Join(paths.ExportDir, "resources", namespace, "_cluster"), excludedResource)
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())

		By("Verifying Widget CR exists in namespace export directory")
		nameSpaceDir := filepath.Join(paths.ExportDir, "resources", namespace)
		found, err = utils.AssertResourcesExist(nameSpaceDir, includedResource)
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
	})

	It("[MTA-869b] Should include CRD when --crd-include-group matches", Label("tier0"), func() {
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

		paths, err := NewScenarioPaths("crane-ca10b-*")
		Expect(err).NotTo(HaveOccurred())
		crdYAML, err := utils.ReadTestdataFile("gadget_crd.yaml")
		Expect(err).NotTo(HaveOccurred())
		crYAML, err := utils.ReadTestdataFile("gadget_cr.yaml")
		Expect(err).NotTo(HaveOccurred())

		crd := CustomResourceDefinition{
			Name: "gadgets.crane-e2e.openshift.io",
			YAML: crdYAML,
		}
		cr := CustomResource{
			Name:      "test-gadget",
			Namespace: namespace,
			Kind:      "Gadget",
			YAML:      crYAML,
			Resource:  "gadgets",
		}
		tgtNameSpace := Namespace{Name: namespace}

		exportOpts := ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir,
			CRDIncludeGroups: []string{"crane-e2e.openshift.io"}}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}

		DeferCleanup(func() {
			if err := ResourceCleanup([]KubectlRunner{kubectlSrc, kubectlTgt}, []Resource{cr, crd, tgtNameSpace}); err != nil {
				log.Printf("Resources cleanup: %v", err)
			}
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("Scenario cleanup: %v", err)
			}
		})

		By("Deploying app on source cluster")
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())

		By("Creating Gadget CRD on source")
		Expect(crd.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Waiting for CRD to be established")
		Expect(crd.WaitForEstablished(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating Gadget custom resource in namespace")
		Expect(cr.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Waiting for source pods and endpoints to drain")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		By("Running crane export with --crd-include-group, transform, apply")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verifying CRD exists in export _cluster directory")
		exportClusterPath := filepath.Join(paths.ExportDir, "resources", namespace, "_cluster")
		found, err := utils.AssertResourcesExist(exportClusterPath, []utils.ResourceMatch{
			{Kind: "CustomResourceDefinition", Name: crd.Name}})
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())

		By("Verifying Gadget CR exists in namespace export directory")
		namespaceDir := filepath.Join(paths.ExportDir, "resources", namespace)
		found, err = utils.AssertResourcesExist(namespaceDir, []utils.ResourceMatch{
			{Kind: cr.Kind, Name: cr.Name, Scope: namespace}})
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())

		By("Creating namespace on target cluster")
		Expect(tgtNameSpace.Create(kubectlTgt)).NotTo(HaveOccurred())

		By("Applying cluster resources to target")
		Expect(kubectlTgt.ApplyDir(filepath.Join(paths.OutputDir, "resources", "_cluster"))).NotTo(HaveOccurred())

		By("Waiting for CRD to be established on target")
		Expect(crd.WaitForEstablished(kubectlTgt)).NotTo(HaveOccurred())

		By("Applying namespace resources to target")
		Expect(kubectlTgt.ApplyDir(filepath.Join(paths.OutputDir, "resources", namespace))).NotTo(HaveOccurred())

		By("Verifying Gadget CR exists on target")
		_, err = kubectlTgt.Run("get", "gadget", cr.Name, "-n", namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Verifying Gadget CR has correct spec values on target")
		color, err := kubectlTgt.Run("get", "gadget", cr.Name, "-n", namespace,
			"-o", "jsonpath={.spec.color}")
		Expect(err).NotTo(HaveOccurred())
		Expect(color).To(Equal("red"))

		By("Scaling target deployment and validating app")
		Expect(kubectlTgt.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
		Eventually(tgtApp.Validate, "5m", "10s").Should(Succeed())

	})

})
