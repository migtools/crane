package e2e

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/yaml"
)

var _ = Describe("Instructions-file migration", func() {
	It("[MTA-828] should migrate a simple nginx app using transform --instructions-file", Label("tier0"), func() {
		appName := "simple-nginx-nopv"
		namespace := "simple-nginx-instructionsfile"
		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)

		if scenario.KubectlSrcNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin instructions file migration test")
		}
		if scenario.KubectlTgtNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin instructions file migration test")
		}

		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin
		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = srcApp.ExtraVars

		By("Grant ns admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, _, cleanup, err := SetupNamespaceAdminUsersForScenario(scenario, namespace)
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Delete test namespace on source and target (wait for completion)")
			for _, k := range []KubectlRunner{scenario.KubectlSrc, scenario.KubectlTgt} {
				if _, err := k.Run("delete", "namespace", namespace, "--ignore-not-found=true", "--wait=true"); err != nil {
					log.Printf("cleanup: failed to delete namespace %q on context %q: %v", namespace, k.Context, err)
				}
			}
		})
		DeferCleanup(cleanup)
		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-pipeline-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)

			}
		})
		runner.WorkDir = paths.TempDir
		By("Run crane export/transform/apply pipeline with instructions file")
		log.Printf("Running crane export for namespace %s\n", srcApp.Namespace)
		Expect(runner.Export(ExportOptions{Namespace: srcApp.Namespace, ExportDir: paths.ExportDir})).NotTo(HaveOccurred())
		log.Printf("Running crane transform --instructions-file for namespace %s\n", srcApp.Namespace)
		instructionsFile, err := utils.TestdataFilePath("basic-instructions-file.yaml")
		Expect(err).NotTo(HaveOccurred())
		Expect(runner.Transform(TransformOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			InstructionsFile: instructionsFile, Force: false})).NotTo(HaveOccurred())

		By("Assert instructions-file stages are present as stage-directories in transform dir")
		stageDirectories := []string{"10_KubernetesPlugin", "20_CustomStage"}
		for _, stageDir := range stageDirectories {
			dirPath := filepath.Join(paths.TransformDir, stageDir)
			_, err = os.Stat(dirPath)
			Expect(err).NotTo(HaveOccurred(), fmt.Sprintf("expected stage dir %q at %q to be present", stageDir, dirPath))
		}

		By("Verify transform directory structure")
		workDirPath := filepath.Join(paths.TransformDir, ".work")
		_, err = os.Stat(workDirPath)
		if err == nil {
			Fail(fmt.Sprintf("did not expect legacy transform work dir at %q, but it exists", workDirPath))
		}
		if !os.IsNotExist(err) {
			Fail(fmt.Sprintf(
				"unexpected error while checking legacy transform work dir at %q: err=%v type=%T",
				workDirPath, err, err,
			))
		}

		stageSubDirs := []string{"input", "output"}
		for _, stageDir := range stageDirectories {
			stagePath := filepath.Join(paths.TransformDir, stageDir)
			for _, subDir := range stageSubDirs {
				subDirPath := filepath.Join(stagePath, subDir)
				subDirInfo, statErr := os.Stat(subDirPath)
				Expect(statErr).NotTo(HaveOccurred(), fmt.Sprintf("expected stage subdir %q at %q to be present", subDir, subDirPath))
				Expect(subDirInfo.IsDir()).To(BeTrue(), fmt.Sprintf("expected stage subdir %q at %q to be a directory", subDir, subDirPath))

				yamlFileCount := 0
				walkErr := filepath.WalkDir(subDirPath, func(path string, d os.DirEntry, walkErr error) error {
					if walkErr != nil {
						return walkErr
					}
					if d.IsDir() {
						return nil
					}
					if strings.HasSuffix(d.Name(), ".yaml") {
						yamlFileCount++
					}
					return nil
				})
				Expect(walkErr).NotTo(HaveOccurred(), fmt.Sprintf("failed to walk yaml files in %q", subDirPath))
				Expect(yamlFileCount).To(BeNumerically(">", 0), fmt.Sprintf("expected yaml files in %q", subDirPath))
			}

			kustomizationPath := filepath.Join(stagePath, "kustomization.yaml")
			kustomizationBytes, readErr := os.ReadFile(kustomizationPath)
			Expect(readErr).NotTo(HaveOccurred(), fmt.Sprintf("expected kustomization file at %q", kustomizationPath))

			kustomization := map[string]any{}
			unmarshalErr := yaml.Unmarshal(kustomizationBytes, &kustomization)
			Expect(unmarshalErr).NotTo(HaveOccurred(), fmt.Sprintf("failed to parse kustomization yaml at %q", kustomizationPath))

			resourcesRaw, exists := kustomization["resources"]
			Expect(exists).To(BeTrue(), fmt.Sprintf("expected resources field in kustomization %q", kustomizationPath))

			resources, ok := resourcesRaw.([]any)
			Expect(ok).To(BeTrue(), fmt.Sprintf("expected resources in %q to be []any but got %T", kustomizationPath, resourcesRaw))
			Expect(resources).NotTo(BeEmpty(), fmt.Sprintf("expected resources list in %q to be non-empty", kustomizationPath))

			for i, resourceRaw := range resources {
				resourcePath, pathOK := resourceRaw.(string)
				Expect(pathOK).To(BeTrue(), fmt.Sprintf("expected resources[%d] in %q to be string but got %T", i, kustomizationPath, resourceRaw))
				Expect(resourcePath).To(HavePrefix("input/"), fmt.Sprintf("expected resources[%d]=%q in %q to reference input/ path", i, resourcePath, kustomizationPath))
				Expect(resourcePath).NotTo(ContainSubstring("resources/"), fmt.Sprintf("did not expect legacy resources/ path in resources[%d]=%q in %q", i, resourcePath, kustomizationPath))
			}
		}

		log.Printf("Running crane apply for namespace %s\n", srcApp.Namespace)
		Expect(runner.Apply(ApplyOptions{ExportDir: paths.ExportDir, TransformDir: paths.TransformDir,
			OutputDir: paths.OutputDir})).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Apply rendered manifests to target")
		Expect(ApplyOutputToTargetNonAdmin(scenario.KubectlTgtNonAdmin, paths.OutputDir)).NotTo(HaveOccurred())

		By("Scale target deployment and validate app on target")
		Expect(scenario.KubectlTgtNonAdmin.ScaleDeployment(namespace, appName, 1)).NotTo(HaveOccurred())

		Eventually(tgtApp.Validate, "2m", "10s").Should(Succeed())
	})
})
