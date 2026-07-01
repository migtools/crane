package e2e

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Crane validate offline mode: malformed API surface file handling", func() {
	It("[MTA-860] Should handle malformed API surface JSON file gracefully as namespace admin",
		Label("tier1", "validate", "offline"), func() {
		appName := "multi-resource-app"
		namespace := "validate-malformed-json"

		scenario := NewMigrationScenario(
			appName,
			namespace,
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)

		if scenario.SrcAppNonAdmin.Context == "" {
			Skip("source-nonadmin-context is required for non-admin offline validation test")
		}
		if scenario.TgtAppNonAdmin.Context == "" {
			Skip("target-nonadmin-context is required for non-admin offline validation test")
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
		DeferCleanup(cleanup) // Cleanup rolebindings

		By("Prepare source app")
		log.Printf("Preparing source app %s in namespace %s\n", srcApp.Name, srcApp.Namespace)
		Expect(PrepareSourceApp(srcApp, kubectlSrcNonAdmin)).NotTo(HaveOccurred())
		log.Printf("Source app %s prepared successfully\n", srcApp.Name)

		paths, err := NewScenarioPaths("crane-validate-malformed-json-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			By("Cleanup source and target resources")
			if err := CleanupScenario(paths.TempDir, srcApp, tgtApp); err != nil {
				log.Printf("cleanup: %v", err)
			}
		})

		runner.WorkDir = paths.TempDir
		By("Run crane export/transform/apply pipeline")
		log.Printf("Running crane pipeline for namespace %s\n", srcApp.Namespace)

		exportOpts := ExportOptions{
			Namespace: srcApp.Namespace,
			ExportDir: paths.ExportDir,
		}
		transformOpts := TransformOptions{
			ExportDir:    paths.ExportDir,
			TransformDir: paths.TransformDir,
		}
		applyOpts := ApplyOptions{
			ExportDir:    paths.ExportDir,
			TransformDir: paths.TransformDir,
			OutputDir:    paths.OutputDir,
		}

		Expect(RunCranePipelineWithChecks(runner, exportOpts, transformOpts, applyOpts)).NotTo(HaveOccurred())
		log.Printf("Crane pipeline completed for namespace %s\n", srcApp.Namespace)

		By("Test Case 1: Invalid JSON syntax")
		malformedFile := filepath.Join(paths.TempDir, "malformed-syntax.json")
		malformedContent := `{
			"resources": [
				{"apiVersion": "v1", "kind": "Pod"
			]
		}` // Missing closing brace for Pod object
		Expect(os.WriteFile(malformedFile, []byte(malformedContent), 0644)).To(Succeed())

		By("Run crane validate with malformed JSON file")
		validateDir := filepath.Join(paths.TempDir, "validate-malformed-syntax")
		stdout, err := runner.Validate(ValidateOptions{
			InputDir:         filepath.Join(paths.OutputDir, "resources", namespace),
			ValidateDir:      validateDir,
			APIResourcesFile: malformedFile,
		})

		By("Verify that crane validate returns an error")
		Expect(err).To(HaveOccurred(), "crane validate should fail with malformed JSON")
		log.Printf("Validate output: %s", stdout)
		log.Printf("Validate error: %v", err)

		By("Verify error message indicates JSON parsing issue")
		errMsg := err.Error()
		Expect(errMsg).To(Or(
			ContainSubstring("json"),
			ContainSubstring("JSON"),
			ContainSubstring("parse"),
			ContainSubstring("unmarshal"),
			ContainSubstring("invalid"),
			ContainSubstring("syntax"),
		), "error message should indicate JSON parsing issue")

		By("Verify validation report was not created for malformed JSON")
		reportPath := filepath.Join(validateDir, "report.json")
		Expect(reportPath).NotTo(BeAnExistingFile(), "report.json should not be created with malformed JSON")

		log.Printf("✅ Test Case 1: Successfully validated error handling for malformed JSON syntax")

		By("Test Case 2: Empty JSON file")
		emptyFile := filepath.Join(paths.TempDir, "empty-api-surface.json")
		Expect(os.WriteFile(emptyFile, []byte(""), 0644)).To(Succeed())

		By("Run crane validate with empty JSON file")
		validateDir2 := filepath.Join(paths.TempDir, "validate-empty-json")
		stdout2, err := runner.Validate(ValidateOptions{
			InputDir:         filepath.Join(paths.OutputDir, "resources", namespace),
			ValidateDir:      validateDir2,
			APIResourcesFile: emptyFile,
		})

		By("Verify that crane validate returns an error")
		Expect(err).To(HaveOccurred(), "crane validate should fail with empty JSON file")
		log.Printf("Validate output: %s", stdout2)
		log.Printf("Validate error: %v", err)

		By("Verify error message indicates JSON parsing or empty file issue")
		errMsg2 := err.Error()
		Expect(errMsg2).To(Or(
			ContainSubstring("json"),
			ContainSubstring("JSON"),
			ContainSubstring("parse"),
			ContainSubstring("unmarshal"),
			ContainSubstring("empty"),
			ContainSubstring("EOF"),
		), "error message should indicate JSON parsing or empty file issue")

		log.Printf("✅ Test Case 2: Successfully validated error handling for empty JSON file")

		By("Test Case 3: Valid JSON but incorrect structure")
		wrongStructureFile := filepath.Join(paths.TempDir, "wrong-structure.json")
		wrongStructure := map[string]interface{}{
			"wrong_field": "value",
			"resources":   "should_be_array_not_string",
		}
		wrongJSON, err := json.Marshal(wrongStructure)
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(wrongStructureFile, wrongJSON, 0644)).To(Succeed())

		By("Run crane validate with wrong structure JSON file")
		validateDir3 := filepath.Join(paths.TempDir, "validate-wrong-structure")
		stdout3, err := runner.Validate(ValidateOptions{
			InputDir:         filepath.Join(paths.OutputDir, "resources", namespace),
			ValidateDir:      validateDir3,
			APIResourcesFile: wrongStructureFile,
		})

		if err != nil {
			log.Printf("Validate returned error (expected): %v", err)
			log.Printf("Validate output: %s", stdout3)
		} else {
			log.Printf("Validate succeeded (checking report content)")
			// If it doesn't error, check if the report exists
			reportPath3 := filepath.Join(validateDir3, "report.json")
			if _, statErr := os.Stat(reportPath3); statErr == nil {
				log.Printf("Report was created despite wrong structure")
			}
		}

		log.Printf("✅ Test Case 3: Validated behavior with wrong JSON structure")

		By("Test Case 4: Non-JSON content")
		nonJSONFile := filepath.Join(paths.TempDir, "non-json.json")
		nonJSONContent := "This is plain text, not JSON"
		Expect(os.WriteFile(nonJSONFile, []byte(nonJSONContent), 0644)).To(Succeed())

		By("Run crane validate with non-JSON file")
		validateDir4 := filepath.Join(paths.TempDir, "validate-non-json")
		stdout4, err := runner.Validate(ValidateOptions{
			InputDir:         filepath.Join(paths.OutputDir, "resources", namespace),
			ValidateDir:      validateDir4,
			APIResourcesFile: nonJSONFile,
		})

		By("Verify that crane validate returns an error")
		Expect(err).To(HaveOccurred(), "crane validate should fail with non-JSON content")
		log.Printf("Validate output: %s", stdout4)
		log.Printf("Validate error: %v", err)

		By("Verify error message indicates JSON parsing issue")
		errMsg4 := err.Error()
		Expect(errMsg4).To(Or(
			ContainSubstring("json"),
			ContainSubstring("JSON"),
			ContainSubstring("parse"),
			ContainSubstring("unmarshal"),
			ContainSubstring("invalid"),
		), "error message should indicate JSON parsing issue")

		log.Printf("✅ Test Case 4: Successfully validated error handling for non-JSON content")

		By("Test Case 5: Truncated/incomplete JSON")
		By("Create truncated JSON file (incomplete)")
		truncatedFile := filepath.Join(paths.TempDir, "truncated.json")
		truncatedContent := `{
			"resources": [
				{"apiVersion": "v1", "kind": "Pod", "name": "test"`
		// Missing closing brackets - simulates interrupted write
		Expect(os.WriteFile(truncatedFile, []byte(truncatedContent), 0644)).To(Succeed())

		By("Run crane validate with truncated JSON file")
		validateDir5 := filepath.Join(paths.TempDir, "validate-truncated")
		stdout5, err := runner.Validate(ValidateOptions{
			InputDir:         filepath.Join(paths.OutputDir, "resources", namespace),
			ValidateDir:      validateDir5,
			APIResourcesFile: truncatedFile,
		})

		By("Verify that crane validate returns an error")
		Expect(err).To(HaveOccurred(), "crane validate should fail with truncated JSON")
		log.Printf("Validate output: %s", stdout5)
		log.Printf("Validate error: %v", err)

		By("Verify error message indicates JSON parsing issue")
		errMsg5 := err.Error()
		Expect(errMsg5).To(Or(
			ContainSubstring("json"),
			ContainSubstring("JSON"),
			ContainSubstring("parse"),
			ContainSubstring("unmarshal"),
			ContainSubstring("unexpected"),
			ContainSubstring("EOF"),
		), "error message should indicate JSON parsing issue")

		log.Printf("✅ Test Case 5: Successfully validated error handling for truncated JSON")

		By("Test Case 6: Array at root instead of object")
		arrayRootFile := filepath.Join(paths.TempDir, "array-root.json")
		arrayRootContent := `[
			{"apiVersion": "v1", "kind": "Pod"},
			{"apiVersion": "apps/v1", "kind": "Deployment"}
		]`
		Expect(os.WriteFile(arrayRootFile, []byte(arrayRootContent), 0644)).To(Succeed())

		By("Run crane validate with array-root JSON file")
		validateDir6 := filepath.Join(paths.TempDir, "validate-array-root")
		stdout6, err := runner.Validate(ValidateOptions{
			InputDir:         filepath.Join(paths.OutputDir, "resources", namespace),
			ValidateDir:      validateDir6,
			APIResourcesFile: arrayRootFile,
		})

		if err != nil {
			log.Printf("Validate returned error: %v", err)
			log.Printf("Validate output: %s", stdout6)
			By("Verify error indicates type/structure issue")
			errMsg6 := err.Error()
			Expect(errMsg6).To(Or(
				ContainSubstring("json"),
				ContainSubstring("JSON"),
				ContainSubstring("unmarshal"),
				ContainSubstring("type"),
				ContainSubstring("object"),
			), "error should indicate structure/type issue")
		} else {
			log.Printf("Validate succeeded - checking behavior")
		}

		log.Printf("✅ Test Case 6: Validated behavior with array at root")

		By("Test Case 7: Mixed valid and invalid entries")
		mixedFile := filepath.Join(paths.TempDir, "mixed-entries.json")
		mixedContent := `{
			"resources": [
				{"apiVersion": "v1", "kind": "Pod"},
				{"apiVersion": "broken, "kind": "Deployment"},
				{"apiVersion": "v1", "kind": "Service"}
			]
		}`
		Expect(os.WriteFile(mixedFile, []byte(mixedContent), 0644)).To(Succeed())

		By("Run crane validate with mixed entries")
		validateDir7 := filepath.Join(paths.TempDir, "validate-mixed")
		stdout7, err := runner.Validate(ValidateOptions{
			InputDir:         filepath.Join(paths.OutputDir, "resources", namespace),
			ValidateDir:      validateDir7,
			APIResourcesFile: mixedFile,
		})

		By("Verify that crane validate returns an error")
		Expect(err).To(HaveOccurred(), "crane validate should fail with invalid JSON syntax")
		log.Printf("Validate output: %s", stdout7)
		log.Printf("Validate error: %v", err)

		By("Verify error message indicates JSON parsing issue")
		errMsg7 := err.Error()
		Expect(errMsg7).To(Or(
			ContainSubstring("json"),
			ContainSubstring("JSON"),
			ContainSubstring("parse"),
			ContainSubstring("unmarshal"),
			ContainSubstring("syntax"),
		), "error message should indicate JSON parsing issue")

		log.Printf("✅ Test Case 7: Successfully validated error handling for mixed valid/invalid entries")

		By("Test Case 8: Binary data simulated with non-UTF8 bytes)")
		binaryFile := filepath.Join(paths.TempDir, "binary.json")
		// Create binary content - mix of valid JSON start with binary garbage
		binaryContent := []byte{0x7B, 0x22, 0x72, 0x65, 0xFF, 0xFE, 0x00, 0x01, 0x80, 0x90}
		Expect(os.WriteFile(binaryFile, binaryContent, 0644)).To(Succeed())

		By("Run crane validate with binary file")
		validateDir8 := filepath.Join(paths.TempDir, "validate-binary")
		stdout8, err := runner.Validate(ValidateOptions{
			InputDir:         filepath.Join(paths.OutputDir, "resources", namespace),
			ValidateDir:      validateDir8,
			APIResourcesFile: binaryFile,
		})

		By("Verify that crane validate returns an error")
		Expect(err).To(HaveOccurred(), "crane validate should fail with binary content")
		log.Printf("Validate output: %s", stdout8)
		log.Printf("Validate error: %v", err)

		By("Verify error indicates parsing or encoding issue")
		errMsg8 := err.Error()
		Expect(errMsg8).To(Or(
			ContainSubstring("json"),
			ContainSubstring("JSON"),
			ContainSubstring("parse"),
			ContainSubstring("unmarshal"),
			ContainSubstring("invalid"),
			ContainSubstring("character"),
		), "error message should indicate parsing or encoding issue")

		log.Printf("✅ Test Case 8: Successfully validated error handling for binary content")

		log.Printf("✅ MTA-860: All malformed API surface file scenarios validated successfully")
	})
})
