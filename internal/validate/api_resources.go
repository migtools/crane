package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// apiSurfaceJSON is the wrapper format produced by the capture-api-surface.sh script.
// It contains an array of native Kubernetes APIResourceList objects, one per
// served group-version — the same data ServerGroupsAndResources() returns.
type apiSurfaceJSON struct {
	APIResourceLists []metav1.APIResourceList `json:"apiResourceLists"`
}

// ParseAPIResourcesJSON reads the JSON output of capture-api-surface.sh
// and builds a DiscoveryIndex suitable for offline validation.
// The file format is: {"apiResourceLists": [ <APIResourceList>, ... ]}
// where each APIResourceList has a groupVersion field and a resources array.
func ParseAPIResourcesJSON(path string) (DiscoveryIndex, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading api-resources file %q: %w", path, err)
	}

	var surface apiSurfaceJSON
	if err := json.Unmarshal(data, &surface); err != nil {
		return nil, fmt.Errorf("parsing api-resources JSON from %q: %w", path, err)
	}

	if len(surface.APIResourceLists) == 0 {
		return nil, fmt.Errorf("api-resources file %q contains no API resource lists", path)
	}

	index := DiscoveryIndex{}
	for _, list := range surface.APIResourceLists {
		gv := list.GroupVersion
		if gv == "" {
			continue
		}
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

	if len(index) == 0 {
		return nil, fmt.Errorf("api-resources file %q contains no usable resources", path)
	}

	return index, nil
}
