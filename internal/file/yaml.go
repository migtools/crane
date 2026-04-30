package file

import (
	"fmt"
	"io"
	"strings"

	yamlv3 "gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// ParseMultiDocYAML parses multi-document YAML into unstructured Kubernetes resources
func ParseMultiDocYAML(yamlBytes []byte) ([]unstructured.Unstructured, error) {
	var resources []unstructured.Unstructured

	// Use yaml.v3 Decoder to properly handle multi-document YAML streams
	decoder := yamlv3.NewDecoder(strings.NewReader(string(yamlBytes)))

	for {
		var doc any
		err := decoder.Decode(&doc)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to decode YAML document: %w", err)
		}

		// Skip empty documents
		if doc == nil {
			continue
		}

		// Convert the decoded document back to YAML bytes, then to JSON
		docBytes, err := yamlv3.Marshal(doc)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal YAML document: %w", err)
		}

		// Convert YAML to JSON
		jsonData, err := yaml.YAMLToJSON(docBytes)
		if err != nil {
			return nil, fmt.Errorf("failed to convert YAML to JSON: %w", err)
		}

		// Unmarshal into unstructured
		u := unstructured.Unstructured{}
		if err := u.UnmarshalJSON(jsonData); err != nil {
			return nil, fmt.Errorf("failed to unmarshal resource: %w", err)
		}

		resources = append(resources, u)
	}

	return resources, nil
}
