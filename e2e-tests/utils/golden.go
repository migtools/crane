package utils

import (
	"fmt"
	"os"
	"reflect"

	"gopkg.in/yaml.v3"
)

// CompareWithGoldenFile compares an actual YAML file with a golden file and returns differences.
// Returns a list of human-readable difference descriptions and any error encountered.
func CompareWithGoldenFile(actualPath, goldenPath string) ([]string, error) {
	actualData, err := os.ReadFile(actualPath)
	if err != nil {
		return nil, fmt.Errorf("reading actual file %s: %w", actualPath, err)
	}

	goldenData, err := os.ReadFile(goldenPath)
	if err != nil {
		return nil, fmt.Errorf("reading golden file %s: %w", goldenPath, err)
	}

	var actual, golden map[string]interface{}
	if err := yaml.Unmarshal(actualData, &actual); err != nil {
		return nil, fmt.Errorf("unmarshaling actual YAML: %w", err)
	}
	if err := yaml.Unmarshal(goldenData, &golden); err != nil {
		return nil, fmt.Errorf("unmarshaling golden YAML: %w", err)
	}

	diffs := compareObjects("", actual, golden)
	return diffs, nil
}

// compareObjects recursively compares two objects and returns differences
func compareObjects(path string, actual, golden interface{}) []string {
	var diffs []string

	// Handle nil cases
	if actual == nil && golden == nil {
		return nil
	}
	if actual == nil {
		return []string{fmt.Sprintf("missing field: %s", path)}
	}
	if golden == nil {
		return []string{fmt.Sprintf("unexpected field: %s", path)}
	}

	// Type mismatch
	if reflect.TypeOf(actual) != reflect.TypeOf(golden) {
		return []string{fmt.Sprintf("type mismatch at %s: expected %T, got %T", path, golden, actual)}
	}

	switch actualVal := actual.(type) {
	case map[string]interface{}:
		goldenVal := golden.(map[string]interface{})

		// Check for missing keys in actual
		for key := range goldenVal {
			if _, ok := actualVal[key]; !ok {
				keyPath := key
				if path != "" {
					keyPath = path + "." + key
				}
				diffs = append(diffs, fmt.Sprintf("missing field: %s", keyPath))
			}
		}

		// Check for unexpected keys and compare values
		for key, actualValue := range actualVal {
			keyPath := key
			if path != "" {
				keyPath = path + "." + key
			}

			goldenValue, ok := goldenVal[key]
			if !ok {
				diffs = append(diffs, fmt.Sprintf("unexpected field: %s", keyPath))
				continue
			}

			diffs = append(diffs, compareObjects(keyPath, actualValue, goldenValue)...)
		}

	case []interface{}:
		goldenVal := golden.([]interface{})

		if len(actualVal) != len(goldenVal) {
			return []string{fmt.Sprintf("array length mismatch at %s: expected %d, got %d", path, len(goldenVal), len(actualVal))}
		}

		for i := range actualVal {
			indexPath := fmt.Sprintf("%s[%d]", path, i)
			diffs = append(diffs, compareObjects(indexPath, actualVal[i], goldenVal[i])...)
		}

	default:
		// Compare scalar values
		if !reflect.DeepEqual(actual, golden) {
			return []string{fmt.Sprintf("value mismatch at %s: expected %v (%T), got %v (%T)", path, golden, golden, actual, actual)}
		}
	}

	return diffs
}
