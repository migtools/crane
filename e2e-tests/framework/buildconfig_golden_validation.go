package framework

import (
	"fmt"
	"os"

	sigsyaml "sigs.k8s.io/yaml"
)

// CompareWithGoldenFile compares a Build YAML against an expected golden file.
// This provides comprehensive validation that catches missing fields, wrong values, and unexpected fields.
// Adapted from crane-plugin-buildconfig-to-shipwright/tests/framework/validation.go
//
// Returns a list of differences, or empty slice if they match.
//
// Example usage:
//
//	diffs, err := CompareWithGoldenFile(buildYAMLPath, goldenYAMLPath)
//	Expect(err).NotTo(HaveOccurred())
//	if len(diffs) > 0 {
//	    Fail(fmt.Sprintf("Build differs from golden file:\n  %s", strings.Join(diffs, "\n  ")))
//	}
func CompareWithGoldenFile(actualYAMLPath, goldenYAMLPath string) ([]string, error) {
	// Read actual Build YAML
	actualData, err := os.ReadFile(actualYAMLPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read actual YAML: %w", err)
	}

	// Read golden file
	goldenData, err := os.ReadFile(goldenYAMLPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read golden file: %w", err)
	}

	return CompareYAMLs(string(actualData), string(goldenData))
}

// CompareYAMLs compares two YAML strings and returns differences.
// This is the core comparison function - use this when you have YAML strings instead of files.
func CompareYAMLs(actualYAML, expectedYAML string) ([]string, error) {
	// Parse actual YAML
	var actualMap map[string]interface{}
	if err := sigsyaml.Unmarshal([]byte(actualYAML), &actualMap); err != nil {
		return nil, fmt.Errorf("failed to parse actual YAML: %w", err)
	}

	// Parse expected YAML
	var expectedMap map[string]interface{}
	if err := sigsyaml.Unmarshal([]byte(expectedYAML), &expectedMap); err != nil {
		return nil, fmt.Errorf("failed to parse expected YAML: %w", err)
	}

	// Compare and collect differences
	var diffs []string
	compareMapsForGolden("", expectedMap, actualMap, &diffs)

	return diffs, nil
}

// compareMapsForGolden recursively compares two maps and collects differences.
// Adapted from crane-plugin-buildconfig-to-shipwright validation logic.
func compareMapsForGolden(path string, expected, actual map[string]interface{}, diffs *[]string) {
	// Check for missing keys in actual (expected fields not present)
	for key := range expected {
		currentPath := key
		if path != "" {
			currentPath = path + "." + key
		}

		expectedVal, _ := expected[key]
		actualVal, exists := actual[key]

		if !exists {
			*diffs = append(*diffs, fmt.Sprintf("missing field: %s", currentPath))
			continue
		}

		// Compare values recursively
		compareValuesForGolden(currentPath, expectedVal, actualVal, diffs)
	}

	// Check for unexpected keys in actual (extra fields not in expected)
	for key := range actual {
		currentPath := key
		if path != "" {
			currentPath = path + "." + key
		}

		if _, exists := expected[key]; !exists {
			*diffs = append(*diffs, fmt.Sprintf("unexpected field: %s", currentPath))
		}
	}
}

// compareValuesForGolden compares two values recursively.
func compareValuesForGolden(path string, expected, actual interface{}, diffs *[]string) {
	switch expectedVal := expected.(type) {
	case map[string]interface{}:
		actualMap, ok := actual.(map[string]interface{})
		if !ok {
			*diffs = append(*diffs, fmt.Sprintf("%s: type mismatch (expected map, got %T)", path, actual))
			return
		}
		compareMapsForGolden(path, expectedVal, actualMap, diffs)

	case []interface{}:
		actualSlice, ok := actual.([]interface{})
		if !ok {
			*diffs = append(*diffs, fmt.Sprintf("%s: type mismatch (expected array, got %T)", path, actual))
			return
		}
		if len(expectedVal) != len(actualSlice) {
			*diffs = append(*diffs, fmt.Sprintf("%s: length mismatch (expected %d, got %d)", path, len(expectedVal), len(actualSlice)))
			return
		}
		for i := range expectedVal {
			compareValuesForGolden(fmt.Sprintf("%s[%d]", path, i), expectedVal[i], actualSlice[i], diffs)
		}

	default:
		// Compare scalar values
		if fmt.Sprintf("%v", expected) != fmt.Sprintf("%v", actual) {
			*diffs = append(*diffs, fmt.Sprintf("%s: expected '%v', got '%v'", path, expected, actual))
		}
	}
}
