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

var _ = Describe("Namespace-admin cluster-level migration", func() {
	It("[MTA-872] Should migrate CRD + CR with split apply: cluster-admin applies CRD, namespace-admin applies CR", Label("tier0"), func() {
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
		srcAppNonAdmin := scenario.SrcAppNonAdmin
		tgtAppNonAdmin := scenario.TgtAppNonAdmin

		srcAppNonAdmin.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtAppNonAdmin.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		kubectlSrc := scenario.KubectlSrc
		kubectlTgt := scenario.KubectlTgt
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
		tgtNameSpace := Namespace{Name: namespace}
		paths, err := NewScenarioPaths("crane-*")
		Expect(err).NotTo(HaveOccurred())

		runner := scenario.Crane

		exportOpts := ExportOptions{Namespace: srcAppNonAdmin.Namespace, ExportDir: paths.ExportDir}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir}

		By("Granting namespace-admin permissions to non-admin user on source and target")
		kubectlSrcNonAdmin, kubectlTgtNonAdmin, rbacCleanup, err := SetupActiveKubectlRunners(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())

		DeferCleanup(rbacCleanup)

		DeferCleanup(func() {
			if err := ResourceCleanup(
				[]KubectlRunner{kubectlSrc, kubectlTgt}, []Resource{cr, crd, tgtNameSpace}); err != nil {
				log.Printf("Resources cleanup: %v", err)
			}
			if err := CleanupScenario(paths.TempDir, srcAppNonAdmin, tgtAppNonAdmin); err != nil {
				log.Printf("Scenario cleanup: %v", err)
			}

		})
		By("Deploying app as namespace-admin on source cluster")
		err = PrepareSourceApp(srcAppNonAdmin, kubectlSrcNonAdmin)
		Expect(err).NotTo(HaveOccurred())

		By("Creating Widget CRD as cluster-admin")
		Expect(crd.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Waiting for CRD to be established")
		Expect(crd.WaitForEstablished(kubectlSrc)).NotTo(HaveOccurred())

		By("Creating Widget custom resource as cluster-admin")
		Expect(cr.Create(kubectlSrc)).NotTo(HaveOccurred())

		By("Waiting for source pods and endpoints to drain")
		WaitForSourceQuiesce(kubectlSrc, namespace, "app="+appName, serviceName)

		By("Running crane export, transform, apply as cluster-admin")
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verifying CRD exists in export _cluster directory")
		isCrdPresented, err := utils.AssertResourcesExist(filepath.Join(paths.ExportDir, "resources", namespace, "_cluster"),
			[]utils.ResourceMatch{
				{Kind: "CustomResourceDefinition", Name: crd.Name},
			})
		Expect(err).NotTo(HaveOccurred())
		Expect(isCrdPresented).To(BeTrue())

		By("Verifying Widget CR exists in namespace export directory")
		isCrPresented, err := utils.AssertResourcesExist(filepath.Join(paths.ExportDir, "resources", namespace),
			[]utils.ResourceMatch{
				{Kind: cr.Kind, Name: cr.Name, Scope: namespace},
			})
		Expect(err).NotTo(HaveOccurred())
		Expect(isCrPresented).To(BeTrue())

		By("Creating namespace on target cluster")
		Expect(tgtNameSpace.Create(kubectlTgt)).NotTo(HaveOccurred())

		By("Applying CRD to target as cluster-admin")
		Expect(kubectlTgt.ApplyDir(filepath.Join(paths.OutputDir, "resources", "_cluster"))).NotTo(HaveOccurred())

		By("Waiting for CRD to be established on target")
		Expect(crd.WaitForEstablished(kubectlTgt)).NotTo(HaveOccurred())

		By("Granting namespace-admin permission to manage widgets on target")
		_, err = kubectlTgt.Run("create", "role", "widget-admin", "-n", namespace,
			"--verb=*", "--resource=widgets.crane-e2e.example.com")
		Expect(err).NotTo(HaveOccurred())
		_, err = kubectlTgt.Run("create", "rolebinding", "widget-admin-binding", "-n", namespace,
			"--role=widget-admin", "--user=dev")
		Expect(err).NotTo(HaveOccurred())

		By("Applying namespace resources to target as namespace-admin")
		Expect(kubectlTgtNonAdmin.ApplyDir(filepath.Join(paths.OutputDir, "resources", namespace))).NotTo(HaveOccurred())

		By("Verifying Widget CR exists on target")
		_, err = kubectlTgtNonAdmin.Run("get", "widget", "test-widget", "-n", namespace)
		Expect(err).NotTo(HaveOccurred())

		By("Verifying Widget CR has correct spec values on target")
		Expect(cr.AssertField(kubectlTgtNonAdmin, "{.spec.color}", "blue")).NotTo(HaveOccurred())

		By("Scaling target deployment and validating app")
		Expect(kubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())
		Eventually(tgtAppNonAdmin.Validate, "2m", "10s").NotTo(HaveOccurred())

	})

})
