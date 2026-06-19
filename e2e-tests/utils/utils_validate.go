// Package utils provides utilities for e2e tests.
//
// This file (utils_validate.go) contains Ginkgo/Gomega-specific test helpers
// for validating crane validate command results. Unlike the framework-agnostic
// utilities in utils.go (which return errors), these helpers use Ginkgo's Expect()
// and By() for more convenient test assertions.
//
// These helpers are designed to be used across all test tiers (tier0, tier1, tier2+)
// but are ONLY compatible with Ginkgo/Gomega test suites.
package utils

import (
	"log"
	"os"
	"path/filepath"

	"github.com/konveyor/crane/internal/validate"
	. "github.com/onsi/ginkgo/v2"   //nolint:revive,staticcheck // Ginkgo conventionally uses dot imports
	. "github.com/onsi/gomega"      //nolint:revive,staticcheck // Gomega conventionally uses dot imports
)

// ValidationExpectations holds the expected values for validating a crane validate report.
//
// It embeds validate.ValidationReport to reuse standard fields (Mode, APIResourcesSource, TotalScanned, etc.)
// and adds test-specific fields for verification.
//
// NOTE: This struct is designed to be used with VerifyValidateResults(), which is a Ginkgo-specific helper.
type ValidationExpectations struct {
	validate.ValidationReport                 // Embedded: Mode, APIResourcesSource, TotalScanned, Compatible, Incompatible
	ExpectedResources         map[string]string            // Map of Kind -> APIVersion for expected resources (simpler than Results)
	ExpectedStatus            validate.ValidationStatus // Expected status for resources (e.g., StatusOK)
	Namespace                 string                       // Expected namespace for resources
	ExpectFailuresDir         bool                         // Whether to expect a failures/ directory
}

// VerifyValidateResults verifies a crane validate report against expected values using Ginkgo assertions.
//
// IMPORTANT: This is a Ginkgo/Gomega-specific test helper. It uses Expect() and By() internally,
// so it can ONLY be called from within Ginkgo test specs (It, BeforeEach, etc.).
// For framework-agnostic validation, use ParseValidationReport() instead and write your own assertions.
//
// This helper can be used by any validate test in any mode (offline or live mode).
//
// Parameters:
//   - report: The parsed ValidationReport from crane validate
//   - validateDir: The directory where validate output was written
//   - outputFormat: The format label (e.g., "JSON", "YAML") for logging
//   - expectations: ValidationExpectations struct with expected values
//
// Example usage:
//
//	expectations := utils.ValidationExpectations{
//		ValidationReport: validate.ValidationReport{
//			Mode:               "offline",
//			APIResourcesSource: apiSurfaceFile,
//			TotalScanned:       5,
//			Compatible:         5,
//			Incompatible:       0,
//		},
//		ExpectedResources: map[string]string{
//			"Deployment": "apps/v1",
//			"Service":    "v1",
//		},
//		ExpectedStatus:    validate.StatusOK,
//		Namespace:         "my-namespace",
//		ExpectFailuresDir: false,
//	}
//	utils.VerifyValidateResults(report, validateDir, "JSON", expectations)
func VerifyValidateResults(report validate.ValidationReport, validateDir string,
	outputFormat string, expectations ValidationExpectations) {
	// Verify report mode
	By(outputFormat + " report: Verify report shows " + expectations.Mode + " mode")
	Expect(report.Mode).To(Equal(expectations.Mode), "expected validation mode to be '%s' in %s report", expectations.Mode, outputFormat)
	log.Printf("%s validation mode: %s", outputFormat, report.Mode)

	// Verify apiResourcesSource (for offline mode)
	if expectations.Mode == "offline" && expectations.APIResourcesSource != "" {
		By(outputFormat + " report: Verify apiResourcesSource is set to the captured API surface file")
		Expect(report.APIResourcesSource).NotTo(BeEmpty(), "expected apiResourcesSource to be set in offline mode")
		Expect(report.APIResourcesSource).To(Equal(expectations.APIResourcesSource), "expected apiResourcesSource to match API surface file path")
		log.Printf("API resources source: %s", report.APIResourcesSource)
	}

	// Verify resource counts
	By(outputFormat + " report: Verify resource count")
	Expect(report.TotalScanned).To(Equal(expectations.TotalScanned), "expected %d resources scanned in %s report", expectations.TotalScanned, outputFormat)
	Expect(report.Compatible).To(Equal(expectations.Compatible), "expected %d compatible resources in %s report", expectations.Compatible, outputFormat)
	Expect(report.Incompatible).To(Equal(expectations.Incompatible), "expected %d incompatible resources in %s report", expectations.Incompatible, outputFormat)
	log.Printf("%s report - Total: %d, Compatible: %d, Incompatible: %d",
		outputFormat, report.TotalScanned, report.Compatible, report.Incompatible)

	// Verify expected resources if provided
	if len(expectations.ExpectedResources) > 0 {
		By(outputFormat + " report: Verify resource API version, namespace, status")
		foundResources := make(map[string]bool)
		for _, result := range report.Results {
			if expectedAPIVersion, expected := expectations.ExpectedResources[result.Kind]; expected {
				foundResources[result.Kind] = true
				Expect(result.APIVersion).To(Equal(expectedAPIVersion),
					"expected %s to have apiVersion %s in %s report", result.Kind, expectedAPIVersion, outputFormat)
				Expect(result.Status).To(Equal(expectations.ExpectedStatus),
					"expected %s to have status %s in %s report", result.Kind, expectations.ExpectedStatus, outputFormat)
				if expectations.Namespace != "" {
					Expect(result.Namespace).To(Equal(expectations.Namespace),
						"expected %s to be in namespace %s in %s report", result.Kind, expectations.Namespace, outputFormat)
				}
			}
			log.Printf("✓ %s report: Found %s with apiVersion %s in namespace %s with status %s",
				outputFormat, result.Kind, result.APIVersion, result.Namespace, result.Status)
		}

		By(outputFormat + " report: Verify all expected resources were found")
		var missingResources []string
		for kind := range expectations.ExpectedResources {
			if !foundResources[kind] {
				missingResources = append(missingResources, kind)
			}
		}
		Expect(missingResources).To(BeEmpty(),
			"expected to find all resources in %s validation results, missing: %v", outputFormat, missingResources)
	}

	// Verify failures directory
	By(outputFormat + " output: Verify failures directory expectation")
	failuresDir := filepath.Join(validateDir, "failures")
	_, err := os.Stat(failuresDir)
	if expectations.ExpectFailuresDir {
		Expect(err).NotTo(HaveOccurred(), "expected failures/ directory to exist")
		log.Printf("Failures directory created as expected")
	} else {
		Expect(os.IsNotExist(err)).To(BeTrue(), "expected no failures/ directory")
		log.Printf("No failures directory created")
	}
}
