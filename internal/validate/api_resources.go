package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/sirupsen/logrus"
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
func ParseAPIResourcesJSON(path string, log logrus.FieldLogger) (DiscoveryIndex, error) {
	log.Debugf("Loading API resources from %s", path)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading api-resources file %q: %w", path, err)
	}
	log.Debugf("Read %d bytes from API resources file", len(data))

	var surface apiSurfaceJSON
	if err := json.Unmarshal(data, &surface); err != nil {
		return nil, fmt.Errorf("parsing api-resources JSON from %q: %w", path, err)
	}

	if len(surface.APIResourceLists) == 0 {
		return nil, fmt.Errorf("api-resources file %q contains no API resource lists", path)
	}
	log.Debugf("Parsed %d API resource lists from file", len(surface.APIResourceLists))

	index := DiscoveryIndex{}
	totalKinds := 0
	for _, list := range surface.APIResourceLists {
		gv := list.GroupVersion
		if gv == "" {
			log.Debugf("Skipping API resource list with empty groupVersion")
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
			totalKinds++
		}
	}

	if len(index) == 0 {
		return nil, fmt.Errorf("api-resources file %q contains no usable resources", path)
	}

	log.Debugf("Built offline discovery index: %d group-versions, %d kinds", len(index), totalKinds)
	return index, nil
}
