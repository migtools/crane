package transform

import (
	"fmt"

	cranelib "github.com/konveyor/crane-lib/transform"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// WhiteoutReport represents a resource that was excluded from output
type WhiteoutReport struct {
	Resource   unstructured.Unstructured
	PluginName string
}

// IgnoredPatchReport represents a patch operation that was discarded
type IgnoredPatchReport struct {
	Resource      unstructured.Unstructured
	IgnoredOps    []cranelib.IgnoredOperation
	PluginName    string
	WinningPlugin string
}

// GenerateWhiteoutReport creates a YAML report of whiteouted resources
func GenerateWhiteoutReport(whiteouts []WhiteoutReport) ([]byte, error) {
	// Convert to crane-lib format (flat structure)
	libWhiteouts := make([]cranelib.WhiteoutReport, len(whiteouts))
	for i, w := range whiteouts {
		libWhiteouts[i] = cranelib.WhiteoutReport{
			APIVersion:  w.Resource.GetAPIVersion(),
			Kind:        w.Resource.GetKind(),
			Name:        w.Resource.GetName(),
			Namespace:   w.Resource.GetNamespace(),
			RequestedBy: []string{w.PluginName},
		}
	}

	// Sort for deterministic output
	cranelib.SortWhiteouts(libWhiteouts)

	// Convert JSON to YAML
	jsonBytes, err := cranelib.GenerateWhiteoutReport(libWhiteouts)
	if err != nil {
		return nil, err
	}

	yamlBytes, err := yaml.JSONToYAML(jsonBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to convert JSON to YAML: %w", err)
	}

	return yamlBytes, nil
}

// GenerateIgnoredPatchReport creates a YAML report of ignored patches
func GenerateIgnoredPatchReport(ignored []IgnoredPatchReport) ([]byte, error) {
	// Convert to crane-lib format (flat structure)
	var libIgnored []cranelib.IgnoredPatchReport

	for _, r := range ignored {
		// Create ResourceIdentity
		resourceID := cranelib.ResourceIdentity{
			APIVersion: r.Resource.GetAPIVersion(),
			Kind:       r.Resource.GetKind(),
			Name:       r.Resource.GetName(),
			Namespace:  r.Resource.GetNamespace(),
		}

		// Create one report per ignored operation
		for _, op := range r.IgnoredOps {
			// Extract path and operation type from the JSONPatch operation
			path := extractPath(op.Operation)
			operation := extractOp(op.Operation)

			libIgnored = append(libIgnored, cranelib.IgnoredPatchReport{
				Resource:       resourceID,
				Path:           path,
				Operation:      operation,
				SelectedPlugin: r.WinningPlugin,
				IgnoredPlugin:  r.PluginName,
				Reason:         op.Reason,
			})
		}
	}

	// Sort for deterministic output
	cranelib.SortIgnoredPatches(libIgnored)

	// Convert JSON to YAML
	jsonBytes, err := cranelib.GenerateIgnoredPatchReport(libIgnored)
	if err != nil {
		return nil, err
	}

	yamlBytes, err := yaml.JSONToYAML(jsonBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to convert JSON to YAML: %w", err)
	}

	return yamlBytes, nil
}
// extractPath extracts the "path" field from a JSONPatch Operation
func extractPath(operation interface{}) string {
	if opMap, ok := operation.(map[string]interface{}); ok {
		if p, exists := opMap["path"]; exists {
			return fmt.Sprintf("%v", p)
		}
	}
	return ""
}

// extractOp extracts the "op" field from a JSONPatch Operation
func extractOp(operation interface{}) string {
	if opMap, ok := operation.(map[string]interface{}); ok {
		if o, exists := opMap["op"]; exists {
			return fmt.Sprintf("%v", o)
		}
	}
	return ""
}
