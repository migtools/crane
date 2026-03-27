package transform

import (
	"sort"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// SortResourceFiles sorts resource filenames lexically for deterministic output
func SortResourceFiles(files []string) []string {
	sorted := make([]string, len(files))
	copy(sorted, files)
	sort.Strings(sorted)
	return sorted
}

// SortPatchFiles sorts patch filenames lexically for deterministic output
func SortPatchFiles(patches []string) []string {
	sorted := make([]string, len(patches))
	copy(sorted, patches)
	sort.Strings(sorted)
	return sorted
}

// PreserveCreationOrder preserves the original order of resources
// Resources should maintain their discovery order from the export phase
func PreserveCreationOrder(resources []unstructured.Unstructured) []unstructured.Unstructured {
	// No sorting - preserve input order
	return resources
}

// SortResourcesByNamespace sorts resources by namespace, then kind, then name
// This is used for deterministic ordering when grouping resources
func SortResourcesByNamespace(resources []unstructured.Unstructured) []unstructured.Unstructured {
	sorted := make([]unstructured.Unstructured, len(resources))
	copy(sorted, resources)

	sort.Slice(sorted, func(i, j int) bool {
		// First by namespace
		if sorted[i].GetNamespace() != sorted[j].GetNamespace() {
			return sorted[i].GetNamespace() < sorted[j].GetNamespace()
		}

		// Then by kind
		if sorted[i].GetKind() != sorted[j].GetKind() {
			return sorted[i].GetKind() < sorted[j].GetKind()
		}

		// Finally by name
		return sorted[i].GetName() < sorted[j].GetName()
	})

	return sorted
}

// GetSortedKeys returns sorted keys from a map for deterministic iteration
func GetSortedKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
