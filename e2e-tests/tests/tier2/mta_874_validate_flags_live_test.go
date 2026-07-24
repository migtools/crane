package e2e

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/e2e-tests/config"
	. "github.com/konveyor/crane/e2e-tests/framework"
	"github.com/konveyor/crane/e2e-tests/utils"
	cranevalidate "github.com/konveyor/crane/internal/validate"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type liveFlagTestFixture struct {
	tempDir       string
	inputDir      string
	validateDir   string
	runner        CraneRunner
	targetContext string
}

func setupLiveFlagTestFixture(tempPrefix, targetContext string) liveFlagTestFixture {
	tempDir, err := os.MkdirTemp("", tempPrefix)
	Expect(err).NotTo(HaveOccurred())
	DeferCleanup(func() {
		Expect(os.RemoveAll(tempDir)).To(Succeed())
	})

	inputDir := filepath.Join(tempDir, "input")
	validateDir := filepath.Join(tempDir, "validate")
	Expect(os.MkdirAll(inputDir, 0o755)).NotTo(HaveOccurred())

	goldenOutputDir, err := utils.GoldenManifestsDir("simple-nginx-nopv", "output")
	Expect(err).NotTo(HaveOccurred())
	sourceManifestPath := filepath.Join(goldenOutputDir, "output.yaml")
	Expect(sourceManifestPath).To(BeAnExistingFile())

	manifestData, err := os.ReadFile(sourceManifestPath)
	Expect(err).NotTo(HaveOccurred())
	Expect(os.WriteFile(filepath.Join(inputDir, "output.yaml"), manifestData, 0o644)).NotTo(HaveOccurred())

	return liveFlagTestFixture{
		tempDir:     tempDir,
		inputDir:    inputDir,
		validateDir: validateDir,
		runner: CraneRunner{
			Bin:     config.CraneBin,
			WorkDir: tempDir,
		},
		targetContext: targetContext,
	}
}

const validAPIResourcesJSON = `{"apiResourceLists":[{"groupVersion":"v1","resources":[{"name":"pods","namespaced":true,"kind":"Pod","verbs":["get","list"]}]}]}`

var _ = Describe("Crane validate: flag behavior (live mode)", func() {
	var targetContext string

	BeforeEach(func() {
		scenario := NewMigrationScenario(
			"validate-flags-live",
			"validate-flags-live",
			config.K8sDeployBin,
			config.CraneBin,
			config.SourceContext,
			config.TargetContext,
		)
		targetContext = scenario.KubectlTgt.Context
		if targetContext == "" {
			Skip("target context not configured, skipping live mode flag tests")
		}
	})

	// Automates: https://github.com/migtools/crane/issues/431
	It("[MTA-874] --overwrite should fail when validate-dir already exists without the flag ", Label("tier2", "validate"), func() {
		fixture := setupLiveFlagTestFixture("crane-validate-live-overwrite-err-*", targetContext)

		By("Run validate to populate the validate directory")
		_, err := fixture.runner.Validate(ValidateOptions{
			Context:      fixture.targetContext,
			InputDir:     fixture.inputDir,
			ValidateDir:  fixture.validateDir,
			OutputFormat: "json",
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(filepath.Join(fixture.validateDir, "report.json")).To(BeAnExistingFile())
		log.Printf("First validate run succeeded, validate dir populated")

		By("Run validate again without --overwrite")
		_, err = fixture.runner.Validate(ValidateOptions{
			Context:      fixture.targetContext,
			InputDir:     fixture.inputDir,
			ValidateDir:  fixture.validateDir,
			OutputFormat: "json",
		})
		Expect(err).To(HaveOccurred(), "expected error when validate-dir already exists")
		Expect(err.Error()).To(ContainSubstring("already exists"),
			"error should mention directory already exists")
		log.Printf("Second validate run correctly failed: %v", err)
	})

	It("[MTA-874] --overwrite should succeed when validate-dir already exists ", Label("tier2", "validate"), func() {
		fixture := setupLiveFlagTestFixture("crane-validate-live-overwrite-ok-*", targetContext)

		By("Run validate to populate the validate directory")
		_, err := fixture.runner.Validate(ValidateOptions{
			Context:      fixture.targetContext,
			InputDir:     fixture.inputDir,
			ValidateDir:  fixture.validateDir,
			OutputFormat: "json",
		})
		Expect(err).NotTo(HaveOccurred())
		log.Printf("First validate run succeeded")

		By("Run validate again with --overwrite")
		_, err = fixture.runner.Validate(ValidateOptions{
			Context:      fixture.targetContext,
			InputDir:     fixture.inputDir,
			ValidateDir:  fixture.validateDir,
			OutputFormat: "json",
			ExtraArgs:    []string{"--overwrite"},
		})
		Expect(err).NotTo(HaveOccurred(), "validate with --overwrite should succeed")
		log.Printf("Second validate run with --overwrite succeeded")

		By("Verify report.json exists and is valid after overwrite")
		reportPath := filepath.Join(fixture.validateDir, "report.json")
		Expect(reportPath).To(BeAnExistingFile())

		reportData, err := os.ReadFile(reportPath)
		Expect(err).NotTo(HaveOccurred())

		var report cranevalidate.ValidationReport
		Expect(json.Unmarshal(reportData, &report)).To(Succeed(), "report.json should be valid JSON")
		Expect(report.Mode).To(Equal("live"))
		Expect(report.TotalScanned).To(BeNumerically(">", 0), "report should contain scanned resources")
		log.Printf("Report after overwrite: mode=%s, scanned=%d, compatible=%d",
			report.Mode, report.TotalScanned, report.Compatible)
	})

	It("[MTA-874] --input-dir should default to 'output' when omitted ", Label("tier2", "validate"), func() {
		tempDir, err := os.MkdirTemp("", "crane-validate-live-default-input-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(os.RemoveAll(tempDir)).To(Succeed())
		})

		By("Create 'output' directory in workdir with golden manifests")
		defaultInputDir := filepath.Join(tempDir, "output")
		Expect(os.MkdirAll(defaultInputDir, 0o755)).NotTo(HaveOccurred())

		goldenOutputDir, err := utils.GoldenManifestsDir("simple-nginx-nopv", "output")
		Expect(err).NotTo(HaveOccurred())
		manifestData, err := os.ReadFile(filepath.Join(goldenOutputDir, "output.yaml"))
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(filepath.Join(defaultInputDir, "output.yaml"), manifestData, 0o644)).NotTo(HaveOccurred())

		validateDir := filepath.Join(tempDir, "validate-result")
		runner := CraneRunner{Bin: config.CraneBin, WorkDir: tempDir}

		By("Run validate without specifying --input-dir (should default to 'output')")
		_, err = runner.Validate(ValidateOptions{
			Context:      targetContext,
			ValidateDir:  validateDir,
			OutputFormat: "json",
		})
		Expect(err).NotTo(HaveOccurred(), "validate should succeed using default input-dir 'output'")

		reportPath := filepath.Join(validateDir, "report.json")
		Expect(reportPath).To(BeAnExistingFile())

		reportData, err := os.ReadFile(reportPath)
		Expect(err).NotTo(HaveOccurred())

		var report cranevalidate.ValidationReport
		Expect(json.Unmarshal(reportData, &report)).To(Succeed())
		Expect(report.TotalScanned).To(BeNumerically(">", 0), "should have scanned resources from default 'output' dir")
		Expect(report.Mode).To(Equal("live"))
		log.Printf("Default --input-dir (live): scanned %d resources from 'output' directory", report.TotalScanned)
	})

	It("[MTA-874] --validate-dir should default to 'validate' when omitted ", Label("tier2", "validate"), func() {
		fixture := setupLiveFlagTestFixture("crane-validate-live-default-validatedir-*", targetContext)

		By("Run validate without specifying --validate-dir (should default to 'validate')")
		_, err := fixture.runner.Validate(ValidateOptions{
			Context:      fixture.targetContext,
			InputDir:     fixture.inputDir,
			OutputFormat: "json",
		})
		Expect(err).NotTo(HaveOccurred(), "validate should succeed using default validate-dir 'validate'")

		By("Verify report.json is created in <workdir>/validate/")
		defaultReportPath := filepath.Join(fixture.tempDir, "validate", "report.json")
		Expect(defaultReportPath).To(BeAnExistingFile(),
			"expected report.json at default validate dir: %s", defaultReportPath)
		log.Printf("Default --validate-dir (live): report created at %s", defaultReportPath)
	})

	It("[MTA-874] --output should reject invalid format ", Label("tier2", "validate"), func() {
		fixture := setupLiveFlagTestFixture("crane-validate-live-invalid-output-*", targetContext)

		By("Run validate with --output=xml")
		_, err := fixture.runner.Validate(ValidateOptions{
			Context:      fixture.targetContext,
			InputDir:     fixture.inputDir,
			ValidateDir:  fixture.validateDir,
			OutputFormat: "xml",
		})
		Expect(err).To(HaveOccurred(), "validate should fail for unsupported output format")
		Expect(err.Error()).To(SatisfyAll(
			ContainSubstring("output"),
			ContainSubstring("xml"),
			ContainSubstring("must be"),
		), "error should name the invalid format and list supported formats")
		log.Printf("Invalid --output=xml correctly rejected : %v", err)
	})

	It("[MTA-874] --output should default to json when omitted", Label("tier2", "validate"), func() {
		fixture := setupLiveFlagTestFixture("crane-validate-live-default-output-*", targetContext)

		By("Run validate without specifying --output")
		_, err := fixture.runner.Validate(ValidateOptions{
			Context:     fixture.targetContext,
			InputDir:    fixture.inputDir,
			ValidateDir: fixture.validateDir,
		})
		Expect(err).NotTo(HaveOccurred(), "validate should succeed with default output format")

		By("Verify report.json exists")
		jsonReportPath := filepath.Join(fixture.validateDir, "report.json")
		Expect(jsonReportPath).To(BeAnExistingFile(), "expected report.json at %s", jsonReportPath)

		reportData, err := os.ReadFile(jsonReportPath)
		Expect(err).NotTo(HaveOccurred())

		var report cranevalidate.ValidationReport
		Expect(json.Unmarshal(reportData, &report)).To(Succeed(), "report.json should be valid JSON")
		Expect(report.Mode).To(Equal("live"))
		Expect(report.TotalScanned).To(BeNumerically(">", 0))
		log.Printf("Default --output (live): report.json created with %d scanned resources", report.TotalScanned)

		By("Verify report.yaml does NOT exist")
		yamlReportPath := filepath.Join(fixture.validateDir, "report.yaml")
		Expect(yamlReportPath).NotTo(BeAnExistingFile(),
			"report.yaml should not exist when --output defaults to json")
	})

	It("[MTA-874] --input-dir should fail when path does not exist ", Label("tier2", "validate"), func() {
		fixture := setupLiveFlagTestFixture("crane-validate-live-nonexistent-input-*", targetContext)

		nonExistentDir := filepath.Join(fixture.tempDir, "does-not-exist")

		By("Run validate with non-existent --input-dir")
		_, err := fixture.runner.Validate(ValidateOptions{
			Context:      fixture.targetContext,
			InputDir:     nonExistentDir,
			ValidateDir:  fixture.validateDir,
			OutputFormat: "json",
		})
		Expect(err).To(HaveOccurred(), "validate should fail when input-dir does not exist")
		Expect(err.Error()).To(SatisfyAny(
			ContainSubstring(nonExistentDir),
			ContainSubstring("input-dir"),
			ContainSubstring("no such file or directory"),
		), "error should identify the missing path or flag")
		log.Printf("Non-existent --input-dir correctly failed : %v", err)
	})

	// Automates: https://github.com/migtools/crane/issues/428
	It("[MTA-874] --input-dir should handle empty directory with no YAML files ", Label("tier2", "validate"), func() {
		tempDir, err := os.MkdirTemp("", "crane-validate-live-empty-input-*")
		Expect(err).NotTo(HaveOccurred())
		DeferCleanup(func() {
			Expect(os.RemoveAll(tempDir)).To(Succeed())
		})

		emptyInputDir := filepath.Join(tempDir, "empty-input")
		validateDir := filepath.Join(tempDir, "validate")
		Expect(os.MkdirAll(emptyInputDir, 0o755)).NotTo(HaveOccurred())

		runner := CraneRunner{Bin: config.CraneBin, WorkDir: tempDir}

		By("Run validate with empty --input-dir")
		_, err = runner.Validate(ValidateOptions{
			Context:      targetContext,
			InputDir:     emptyInputDir,
			ValidateDir:  validateDir,
			OutputFormat: "json",
		})
		Expect(err).To(HaveOccurred(), "validate should fail when input dir contains no manifests")
		Expect(err.Error()).To(ContainSubstring("no manifests found"),
			"error should indicate no manifests were found in the empty input dir")
		log.Printf("Empty --input-dir correctly rejected : %v", err)
	})

	// Automates: https://github.com/migtools/crane/issues/432
	It("[MTA-874] --output should accept uppercase format ", Label("tier2", "validate"), func() {
		fixture := setupLiveFlagTestFixture("crane-validate-live-output-case-*", targetContext)

		By("Run validate with --output=JSON (uppercase)")
		_, err := fixture.runner.Validate(ValidateOptions{
			Context:      fixture.targetContext,
			InputDir:     fixture.inputDir,
			ValidateDir:  fixture.validateDir,
			OutputFormat: "JSON",
		})
		Expect(err).NotTo(HaveOccurred(), "validate should accept uppercase output format")

		By("Verify report.json exists")
		reportPath := filepath.Join(fixture.validateDir, "report.json")
		Expect(reportPath).To(BeAnExistingFile(), "expected report.json at %s", reportPath)
		log.Printf("Uppercase --output=JSON correctly accepted")
	})

	// Automates: https://github.com/migtools/crane/issues/429
	DescribeTable("[MTA-874] --api-resources should be mutually exclusive with kubeconfig flags",
		func(flagName, flagValue string) {
			fixture := setupLiveFlagTestFixture("crane-validate-live-mutual-excl-*", targetContext)

			apiResourcesFile := filepath.Join(fixture.tempDir, "api-resources.json")
			Expect(os.WriteFile(apiResourcesFile, []byte(validAPIResourcesJSON), 0o644)).NotTo(HaveOccurred())

			By("Run validate with both --api-resources and --" + flagName)
			_, err := fixture.runner.Validate(ValidateOptions{
				InputDir:         fixture.inputDir,
				ValidateDir:      fixture.validateDir,
				OutputFormat:     "json",
				APIResourcesFile: apiResourcesFile,
				ExtraArgs:        []string{"--" + flagName, flagValue},
			})
			Expect(err).To(HaveOccurred(), "validate should fail when both --api-resources and --%s are provided", flagName)
			Expect(err.Error()).To(SatisfyAll(
				ContainSubstring("api-resources"),
				ContainSubstring(flagName),
				ContainSubstring("mutually exclusive"),
			), "error should name both flags and state they are mutually exclusive")
			log.Printf("--api-resources and --%s mutual exclusivity enforced: %v", flagName, err)
		},
		Entry("[MTA-874] --context", Label("tier2", "validate"), "context", "fake-context"),
		Entry("[MTA-874] --kubeconfig", Label("tier2", "validate"), "kubeconfig", "/tmp/fake-kubeconfig"),
		Entry("[MTA-874] --server", Label("tier2", "validate"), "server", "https://fake-server:6443"),
		Entry("[MTA-874] --token", Label("tier2", "validate"), "token", "fake-bearer-token"),
		Entry("[MTA-874] --cluster", Label("tier2", "validate"), "cluster", "fake-cluster"),
		Entry("[MTA-874] --user", Label("tier2", "validate"), "user", "fake-user"),
	)

	It("[MTA-874] --overwrite should succeed on first run with no pre-existing validate-dir ", Label("tier2", "validate"), func() {
		fixture := setupLiveFlagTestFixture("crane-validate-live-overwrite-first-run-*", targetContext)

		By("Run validate with --overwrite on a fresh validate-dir")
		_, err := fixture.runner.Validate(ValidateOptions{
			Context:      fixture.targetContext,
			InputDir:     fixture.inputDir,
			ValidateDir:  fixture.validateDir,
			OutputFormat: "json",
			ExtraArgs:    []string{"--overwrite"},
		})
		Expect(err).NotTo(HaveOccurred(), "validate with --overwrite should succeed even when validate-dir does not exist yet")

		By("Verify report.json exists")
		reportPath := filepath.Join(fixture.validateDir, "report.json")
		Expect(reportPath).To(BeAnExistingFile(), "expected report.json at %s", reportPath)
		log.Printf("--overwrite on first run succeeded ")
	})
})
