package e2e

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
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

		srcApp := scenario.SrcAppNonAdmin
		tgtApp := scenario.TgtAppNonAdmin
		runner := scenario.CraneNonAdmin
		srcApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}
		tgtApp.ExtraVars = map[string]any{
			"non_admin_user": "true",
		}

		By("Grant ns admin permissions to nonadmin user on source and target")
		kubectlSrcNonAdmin, _, cleanup, err := SetupActiveKubectlRunners(scenario, namespace)
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

		// Define test cases for malformed JSON scenarios
		type malformedJSONTestCase struct {
			name              string
			fileContent       interface{} // string for raw content, []byte for binary, or map for marshaled JSON
			validateDirSuffix string
			expectError       bool
			errorSubstrings   []string
		}

		testCases := []malformedJSONTestCase{
			{
				name: "Invalid JSON syntax",
				fileContent: `{
			"resources": [
				{"apiVersion": "v1", "kind": "Pod"
			]
		}`, // Missing closing brace for Pod object
				validateDirSuffix: "malformed-syntax",
				expectError:       true,
				errorSubstrings:   []string{"invalid character"},
			},
			{
				name:              "Empty JSON file",
				fileContent:       "",
				validateDirSuffix: "empty-json",
				expectError:       true,
				errorSubstrings:   []string{"unexpected end of JSON input"},
			},
			{
				name: "Valid JSON but incorrect structure",
				fileContent: map[string]interface{}{
					"wrong_field": "value",
					"resources":   "should_be_array_not_string",
				},
				validateDirSuffix: "wrong-structure",
				expectError:       true,
				errorSubstrings:   []string{"contains no API resource lists"},
			},
			{
				name:              "Non-JSON content",
				fileContent:       "This is plain text, not JSON",
				validateDirSuffix: "non-json",
				expectError:       true,
				errorSubstrings:   []string{"invalid character"},
			},
			{
				name: "Truncated/incomplete JSON",
				fileContent: `{
			"resources": [
				{"apiVersion": "v1", "kind": "Pod", "name": "test"`,
				validateDirSuffix: "truncated",
				expectError:       true,
				errorSubstrings:   []string{"unexpected end of JSON input"},
			},
			{
				name: "Array at root instead of object",
				fileContent: `[
			{"apiVersion": "v1", "kind": "Pod"},
			{"apiVersion": "apps/v1", "kind": "Deployment"}
		]`,
				validateDirSuffix: "array-root",
				expectError:       true,
				errorSubstrings:   []string{"cannot unmarshal array"},
			},
			{
				name: "Mixed valid and invalid entries",
				fileContent: `{
			"resources": [
				{"apiVersion": "v1", "kind": "Pod"},
				{"apiVersion": "broken, "kind": "Deployment"},
				{"apiVersion": "v1", "kind": "Service"}
			]
		}`,
				validateDirSuffix: "mixed",
				expectError:       true,
				errorSubstrings:   []string{"invalid character"},
			},
			{
				name:              "Binary data with non-UTF8 bytes",
				fileContent:       []byte{0x7B, 0x22, 0x72, 0x65, 0xFF, 0xFE, 0x00, 0x01, 0x80, 0x90},
				validateDirSuffix: "binary",
				expectError:       true,
				errorSubstrings:   []string{"invalid character"},
			},
		}

		// Execute test cases in a loop
		for i, tc := range testCases {
			testNum := i + 1
			log.Printf("\n========================================")
			By(fmt.Sprintf("▶️ Test Case %d: %s", testNum, tc.name))

			// Wrap test case in a function to recover from panics and continue with remaining cases
			func() {
				defer GinkgoRecover()

				// Create test file with appropriate content
				testFile := filepath.Join(paths.TempDir, tc.validateDirSuffix+".json")
				switch content := tc.fileContent.(type) {
				case string:
					Expect(os.WriteFile(testFile, []byte(content), 0644)).To(Succeed())
				case []byte:
					Expect(os.WriteFile(testFile, content, 0644)).To(Succeed())
				case map[string]interface{}:
					jsonBytes, err := json.Marshal(content)
					Expect(err).NotTo(HaveOccurred())
					Expect(os.WriteFile(testFile, jsonBytes, 0644)).To(Succeed())
				default:
					Fail(fmt.Sprintf("unsupported fileContent type %T for test case %q", content, tc.name))
				}

				// Run crane validate
				validateDir := filepath.Join(paths.TempDir, "validate-"+tc.validateDirSuffix)
				stdout, err := runner.Validate(ValidateOptions{
					InputDir:         filepath.Join(paths.OutputDir, "resources", namespace),
					ValidateDir:      validateDir,
					APIResourcesFile: testFile,
				})

				// Verify error expectation
				if tc.expectError {
					Expect(err).To(HaveOccurred(), "crane validate should fail with "+tc.name)
					log.Printf("Validate output: %s", stdout)
					log.Printf("Validate error: %v", err)

					// Verify error message contains expected substrings
					// Unwrap to get the root cause error, not just the wrapper
					rootErr := err
					for {
						unwrapped := errors.Unwrap(rootErr)
						if unwrapped == nil {
							break
						}
						rootErr = unwrapped
					}
					rootErrMsg := rootErr.Error()
					matchers := make([]types.GomegaMatcher, len(tc.errorSubstrings))
					for idx, substr := range tc.errorSubstrings {
						matchers[idx] = ContainSubstring(substr)
					}
					Expect(rootErrMsg).To(Or(matchers...), "root error message should indicate expected issue")

					// Verify validation report was not created
					reportPath := filepath.Join(validateDir, "report.json")
					Expect(reportPath).NotTo(BeAnExistingFile(), "report.json should not be created with malformed JSON")
				} else {
					// For test cases expecting successful validation
					Expect(err).NotTo(HaveOccurred(), "crane validate should succeed with "+tc.name)
					log.Printf("Validate output: %s", stdout)

					// Verify validation report was created successfully
					reportPath := filepath.Join(validateDir, "report.json")
					Expect(reportPath).To(BeAnExistingFile(), "report.json should be created for successful validation")
				}

				log.Printf("✅ Test Case %d: Successfully validated error handling for %s", testNum, tc.name)
			}()
		}
		log.Printf("\n========================================")
		log.Printf("✅ MTA-860: All malformed API surface file scenarios validated successfully")
	})
})
