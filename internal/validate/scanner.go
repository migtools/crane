package validate

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sirupsen/logrus"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/yaml"
)

// ScanOptions configures which directories to scan for Kubernetes manifests.
type ScanOptions struct {
	Dirs []string
}

// manifestMeta holds the minimal fields we unmarshal from each YAML document.
type manifestMeta struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Metadata   struct {
		Namespace string `json:"namespace"`
	} `json:"metadata"`
}

// ScanManifests walks the given directories, parses YAML/JSON manifests
// (including multi-document YAML), and returns deduplicated ManifestEntry
// values sorted by group/version/kind/namespace.
func ScanManifests(opts ScanOptions, log logrus.FieldLogger) ([]ManifestEntry, error) {
	index := map[string]*ManifestEntry{}

	for _, dir := range opts.Dirs {
		if err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				if d.Name() == "failures" {
					return filepath.SkipDir
				}
				return nil
			}
			ext := strings.ToLower(filepath.Ext(path))
			if ext != ".yaml" && ext != ".yml" && ext != ".json" {
				return nil
			}

			data, err := os.ReadFile(path)
			if err != nil {
				return fmt.Errorf("read %s: %w", path, err)
			}

			decoder := yaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
			for {
				var meta manifestMeta
				if err := decoder.Decode(&meta); err != nil {
					if err == io.EOF {
						break
					}
					log.Warnf("skipping unparseable document in %s: %v", path, err)
					break
				}
				if meta.APIVersion == "" || meta.Kind == "" {
					continue
				}

				gv, err := schema.ParseGroupVersion(meta.APIVersion)
				if err != nil {
					log.Warnf("skipping invalid apiVersion %q in %s: %v", meta.APIVersion, path, err)
					continue
				}

				key := fmt.Sprintf("%s/%s/%s/%s", gv.Group, gv.Version, meta.Kind, meta.Metadata.Namespace)
				if entry, ok := index[key]; ok {
					entry.SourceFiles = append(entry.SourceFiles, path)
				} else {
					index[key] = &ManifestEntry{
						APIVersion:  meta.APIVersion,
						Kind:        meta.Kind,
						Group:       gv.Group,
						Version:     gv.Version,
						Namespace:   meta.Metadata.Namespace,
						SourceFiles: []string{path},
					}
				}
			}
			return nil
		}); err != nil {
			return nil, fmt.Errorf("walking %s: %w", dir, err)
		}
	}

	entries := make([]ManifestEntry, 0, len(index))
	for _, e := range index {
		entries = append(entries, *e)
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.Group != b.Group {
			return a.Group < b.Group
		}
		if a.Version != b.Version {
			return a.Version < b.Version
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		return a.Namespace < b.Namespace
	})

	return entries, nil
}
