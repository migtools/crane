package validate

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
)

// MatchOptions configures the target-cluster discovery used by MatchResults.
type MatchOptions struct {
	DiscoveryClient discovery.DiscoveryInterface
}

// MatchResults compares each ManifestEntry against the target cluster's
// discovery information and returns a report indicating which GVKs are
// compatible and which are not.
func MatchResults(entries []ManifestEntry, opts MatchOptions, log logrus.FieldLogger) (*ValidationReport, error) {
	index, err := buildDiscoveryIndex(opts.DiscoveryClient, log)
	if err != nil {
		return nil, err
	}

	kindIndex := buildKindIndex(index)

	results := make([]ValidationResult, 0, len(entries))
	for _, entry := range entries {
		result := matchEntry(entry, index)
		if result.Status == StatusIncompatible {
			addSuggestion(&result, entry, kindIndex)
		}
		results = append(results, result)
	}

	compatible, incompatible := 0, 0
	for _, r := range results {
		if r.Status == StatusOK {
			compatible++
		} else {
			incompatible++
		}
	}

	return &ValidationReport{
		Results:      results,
		TotalScanned: len(results),
		Compatible:   compatible,
		Incompatible: incompatible,
	}, nil
}

// discoveryEntry stores one APIResource with its group/version context.
type discoveryEntry struct {
	Resource metav1.APIResource
}

// buildDiscoveryIndex fetches all served group-versions from the target cluster
// and builds a two-level lookup: groupVersion -> kind -> discoveryEntry.
func buildDiscoveryIndex(client discovery.DiscoveryInterface, log logrus.FieldLogger) (map[string]map[string]discoveryEntry, error) {
	_, lists, err := client.ServerGroupsAndResources()
	if err != nil {
		if discovery.IsGroupDiscoveryFailedError(err) {
			if len(lists) == 0 {
				return nil, fmt.Errorf("discovery failed with no usable results: %w", err)
			}
			log.Warnf("partial discovery failure, continuing with available groups: %v", err)
		} else {
			return nil, fmt.Errorf("discovery failed: %w", err)
		}
	}

	index := map[string]map[string]discoveryEntry{}
	for _, list := range lists {
		gv := list.GroupVersion
		if _, ok := index[gv]; !ok {
			index[gv] = map[string]discoveryEntry{}
		}
		for _, res := range list.APIResources {
			if strings.Contains(res.Name, "/") {
				continue
			}
			index[gv][res.Kind] = discoveryEntry{Resource: res}
		}
	}
	return index, nil
}

// matchEntry checks a single ManifestEntry against the discovery index.
func matchEntry(entry ManifestEntry, index map[string]map[string]discoveryEntry) ValidationResult {
	result := ValidationResult{
		APIVersion: entry.APIVersion,
		Kind:       entry.Kind,
		Namespace:  entry.Namespace,
	}

	gvKey := entry.APIVersion
	kinds, gvFound := index[gvKey]
	if !gvFound {
		result.Status = StatusIncompatible
		result.Reason = fmt.Sprintf("API version %s not available on target cluster", entry.APIVersion)
		return result
	}

	de, kindFound := kinds[entry.Kind]
	if !kindFound {
		result.Status = StatusIncompatible
		result.Reason = fmt.Sprintf("kind %s not found in API version %s on target cluster", entry.Kind, entry.APIVersion)
		return result
	}

	result.Status = StatusOK
	result.ResourcePlural = de.Resource.Name
	return result
}

// buildKindIndex creates a reverse lookup: kind -> list of groupVersion strings
// that serve it. Used to suggest alternatives for incompatible resources.
func buildKindIndex(index map[string]map[string]discoveryEntry) map[string][]string {
	kindIdx := map[string][]string{}
	for gv, kinds := range index {
		for kind := range kinds {
			kindIdx[kind] = append(kindIdx[kind], gv)
		}
	}
	return kindIdx
}

// addSuggestion checks whether the same kind is available under a different
// apiVersion on the target and populates the Suggestion field.
func addSuggestion(result *ValidationResult, entry ManifestEntry, kindIndex map[string][]string) {
	alternatives := kindIndex[entry.Kind]
	if len(alternatives) == 0 {
		return
	}

	var available []string
	for _, gv := range alternatives {
		if gv != entry.APIVersion {
			available = append(available, gv)
		}
	}
	if len(available) == 0 {
		return
	}

	sort.Strings(available)
	suggestion := fmt.Sprintf("available as %s", strings.Join(available, ", "))
	result.Suggestion = suggestion
	result.Reason = fmt.Sprintf("%s (%s)", result.Reason, suggestion)
}

