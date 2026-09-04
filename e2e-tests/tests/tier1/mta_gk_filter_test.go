package e2e

import (
	"log"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// TODO: rename this file to mta_<number>_gk_filter_test.go and prefix the It
// descriptions with [MTA-<number>] once the test plan / tracking issue for the
// Group/Kind export filter is created, to match the rest of the tier1 suite.
var _ = Describe("Crane export: filter resources by Group/Kind", func() {
	const (
		appName   = "simple-nginx-nopv"
		namespace = "gk-filter-test"
	)

	var (
		scenario   MigrationScenario
		srcApp     K8sDeployApp
		tgtApp     K8sDeployApp
		kubectlSrc KubectlRunner
		paths      ScenarioPaths
	)

	// resourceGlob builds a glob for exported manifests of a given Kind. Exported
	// files are named "<Kind>_<Group>_<Version>_<Namespace>_<Name>.yaml", so the
	// "<Kind>_" prefix reliably selects resources of that Kind.
	resourceGlob := func(kind string) string {
		return filepath.Join(paths.ExportDir, "resources", namespace, kind+"_*")
	}

	BeforeEach(func() {
		scenario = NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		srcApp = scenario.SrcApp
		tgtApp = scenario.TgtApp
		kubectlSrc = scenario.KubectlSrc

		By("Prepare source app with multiple resource types")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrc)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		configMapYAML := `apiVersion: v1
kind: ConfigMap
metadata:
  name: test-cm
  namespace: ` + namespace + `
data:
  key: value
`
		By("Create test ConfigMap")
		Expect(kubectlSrc.ApplyYAMLSpec(configMapYAML, namespace)).NotTo(HaveOccurred())

		secretYAML := `apiVersion: v1
kind: Secret
metadata:
  name: test-secret
  namespace: ` + namespace + `
type: Opaque
data:
  password: c2VjcmV0
`
		By("Create test Secret")
		Expect(kubectlSrc.ApplyYAMLSpec(secretYAML, namespace)).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		By("Cleanup source and target resources")
		// CleanupScenario removes the temp dir and the source/target apps
		// (including the ConfigMap and Secret created in the app namespace).
		if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
			log.Printf("cleanup: %v", err)
		}
	})

	It("should export only the included Group/Kinds with --include-gk", Label("tier1"), func() {
		var err error
		paths, err = NewScenarioPaths("crane-export-gk-include-*")
		Expect(err).NotTo(HaveOccurred())

		runner := scenario.Crane
		runner.WorkDir = paths.TempDir

		By("Export with --include-gk to only export Deployments and ConfigMaps")
		exportOpts := ExportOptions{
			Namespace: srcApp.Namespace,
			ExportDir: paths.ExportDir,
			ExtraArgs: []string{"--include-gk", "Deployment", "--include-gk", "ConfigMap"},
		}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}

		log.Printf("Running crane export with --include-gk Deployment,ConfigMap for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verify Deployments and ConfigMaps were exported")
		Expect(filepath.Glob(resourceGlob("Deployment"))).NotTo(BeEmpty(), "should have exported at least one Deployment")
		Expect(filepath.Glob(resourceGlob("ConfigMap"))).NotTo(BeEmpty(), "should have exported at least one ConfigMap")

		By("Verify Secrets and Services were not exported")
		Expect(filepath.Glob(resourceGlob("Secret"))).To(BeEmpty(), "should not export Secrets with --include-gk Deployment,ConfigMap")
		Expect(filepath.Glob(resourceGlob("Service"))).To(BeEmpty(), "should not export Services with --include-gk Deployment,ConfigMap")
	})

	It("should skip the excluded Group/Kinds with --exclude-gk", Label("tier1"), func() {
		var err error
		paths, err = NewScenarioPaths("crane-export-gk-exclude-*")
		Expect(err).NotTo(HaveOccurred())

		runner := scenario.Crane
		runner.WorkDir = paths.TempDir

		By("Export with --exclude-gk to skip Secrets")
		exportOpts := ExportOptions{
			Namespace: srcApp.Namespace,
			ExportDir: paths.ExportDir,
			ExtraArgs: []string{"--exclude-gk", "Secret"},
		}
		transformOpts := TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir}
		applyOpts := ApplyOptions{TransformDir: paths.TransformDir, OutputDir: paths.OutputDir}

		log.Printf("Running crane export with --exclude-gk Secret for namespace %s\n", srcApp.Namespace)
		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())

		By("Verify Secrets were excluded")
		Expect(filepath.Glob(resourceGlob("Secret"))).To(BeEmpty(), "should not export Secrets with --exclude-gk Secret")

		By("Verify other resources were still exported")
		deployments, err := filepath.Glob(resourceGlob("Deployment"))
		Expect(err).NotTo(HaveOccurred())
		configMaps, err := filepath.Glob(resourceGlob("ConfigMap"))
		Expect(err).NotTo(HaveOccurred())
		Expect(append(deployments, configMaps...)).NotTo(BeEmpty(), "should have exported non-Secret resources")
	})
})
