package validate

import (
	"encoding/json"
	"fmt"
	"os"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// apiResourceListJSON matches the JSON structure of `kubectl api-resources -o json`.
type apiResourceListJSON struct {
	Kind       string                 `json:"kind"`
	APIVersion string                 `json:"apiVersion"`
	Resources  []apiResourceEntryJSON `json:"resources"`
}

// apiResourceEntryJSON is one resource entry in the kubectl JSON output.
// Group and version are separate fields; core resources omit group.
type apiResourceEntryJSON struct {
	Name       string   `json:"name"`
	Namespaced bool     `json:"namespaced"`
	Group      string   `json:"group,omitempty"`
	Version    string   `json:"version"`
	Kind       string   `json:"kind"`
	Verbs      []string `json:"verbs,omitempty"`
}

// ParseAPIResourcesJSON reads the JSON output of `kubectl api-resources -o json`
// and builds a DiscoveryIndex suitable for offline validation.
func ParseAPIResourcesJSON(path string) (DiscoveryIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading api-resources file %q: %w", path, err)
	}

	var resourceList apiResourceListJSON
	if err := json.Unmarshal(data, &resourceList); err != nil {
		return nil, fmt.Errorf("parsing api-resources JSON from %q: %w", path, err)
	}

	if len(resourceList.Resources) == 0 {
		return nil, fmt.Errorf("api-resources file %q contains no resources", path)
	}

	index := DiscoveryIndex{}
	for _, res := range resourceList.Resources {
		gv := res.Version
		if res.Group != "" {
			gv = res.Group + "/" + res.Version
		}

		if _, ok := index[gv]; !ok {
			index[gv] = map[string]discoveryEntry{}
		}
		index[gv][res.Kind] = discoveryEntry{
			Resource: metav1.APIResource{
				Name:       res.Name,
				Kind:       res.Kind,
				Namespaced: res.Namespaced,
			},
		}
	}
	return index, nil
}
