package kustomize

import (
	"fmt"
	"strings"

	"sigs.k8s.io/yaml"
)

// protectedFields are kustomization keys that must never be overridden by a
// user-provided fragment, since changing them would break the stage pipeline.
var protectedFields = map[string]bool{
	"apiVersion": true,
	"kind":       true,
}

// listMergeFields are keys whose fragment values are appended to the generated
// values instead of replacing them.
var listMergeFields = map[string]bool{
	"resources": true,
	"patches":   true,
}

// ParseFragment parses an inline kustomize fragment (YAML or JSON, since JSON is
// a subset of YAML) into a generic map. It rejects empty input and any fragment
// whose root is not a mapping (e.g. a list or a scalar).
func ParseFragment(raw string) (map[string]interface{}, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, fmt.Errorf("kustomize fragment is empty")
	}
	var probe interface{}
	if err := yaml.Unmarshal([]byte(raw), &probe); err != nil {
		return nil, fmt.Errorf("invalid kustomize fragment: %w", err)
	}
	out, ok := probe.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("kustomize fragment must be a mapping")
	}
	return out, nil
}

// MergeFragment merges a user-provided kustomize fragment into the generated
// kustomization.yaml bytes and returns the merged YAML.
//
// Merge rules:
//   - "resources" and "patches": fragment entries are appended to the generated
//     entries. Resources are de-duplicated by value.
//   - "apiVersion" and "kind": kept from the generated base, fragment values are
//     ignored.
//   - any other key: the fragment value replaces the generated value.
//
// An empty fragment returns the base unchanged.
func MergeFragment(base []byte, fragment map[string]interface{}) ([]byte, error) {
	if len(fragment) == 0 {
		return base, nil
	}

	merged := map[string]interface{}{}
	if err := yaml.Unmarshal(base, &merged); err != nil {
		return nil, fmt.Errorf("failed to parse generated kustomization.yaml: %w", err)
	}

	for key, val := range fragment {
		if protectedFields[key] {
			continue
		}
		if listMergeFields[key] {
			mergedList, err := appendList(key, merged[key], val)
			if err != nil {
				return nil, err
			}
			merged[key] = mergedList
			continue
		}
		merged[key] = val
	}

	out, err := yaml.Marshal(merged)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal merged kustomization.yaml: %w", err)
	}
	return out, nil
}

// appendList appends the fragment list to the base list. For "resources" the
// result is de-duplicated by string value, preserving base-then-fragment order.
func appendList(key string, base, fragment interface{}) (interface{}, error) {
	baseList, err := toList(key, base)
	if err != nil {
		return nil, err
	}
	fragList, err := toList(key, fragment)
	if err != nil {
		return nil, err
	}

	result := make([]interface{}, 0, len(baseList)+len(fragList))
	result = append(result, baseList...)

	if key == "resources" {
		seen := make(map[string]bool, len(baseList))
		for _, item := range baseList {
			if s, ok := item.(string); ok {
				seen[s] = true
			}
		}
		for _, item := range fragList {
			if s, ok := item.(string); ok {
				if seen[s] {
					continue
				}
				seen[s] = true
			}
			result = append(result, item)
		}
		return result, nil
	}

	result = append(result, fragList...)
	return result, nil
}

// toList coerces a value into a list, treating nil as an empty list and
// rejecting non-list values with a descriptive error.
func toList(key string, v interface{}) ([]interface{}, error) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]interface{})
	if !ok {
		return nil, fmt.Errorf("kustomize fragment field %q must be a list", key)
	}
	return list, nil
}
