package export

import (
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// GroupKind represents a Group/Kind pair for resource filtering.
type GroupKind struct {
	Group string
	Kind  string
}

// ParseGroupKind parses a string in "Kind" or "Group/Kind" format.
// "Kind" matches any group; "Group/Kind" matches a specific group.
func ParseGroupKind(s string) (GroupKind, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return GroupKind{}, fmt.Errorf("empty group/kind specification")
	}

	parts := strings.Split(s, "/")
	switch len(parts) {
	case 1:
		// "Kind" - matches any group
		return GroupKind{Group: "", Kind: parts[0]}, nil
	case 2:
		// "Group/Kind"
		group := strings.TrimSpace(parts[0])
		kind := strings.TrimSpace(parts[1])
		if kind == "" {
			return GroupKind{}, fmt.Errorf("invalid group/kind format %q: kind cannot be empty", s)
		}
		return GroupKind{Group: group, Kind: kind}, nil
	default:
		return GroupKind{}, fmt.Errorf("invalid group/kind format %q (expected \"Kind\" or \"Group/Kind\")", s)
	}
}

// Matches returns true if the given group and kind match this GroupKind.
// If gk.Group is empty, it matches any group with the same kind.
func (gk GroupKind) Matches(group, kind string) bool {
	if gk.Kind != kind {
		return false
	}
	// If Group is empty, match any group
	if gk.Group == "" {
		return true
	}
	return gk.Group == group
}

// String returns the string representation of the GroupKind.
func (gk GroupKind) String() string {
	if gk.Group == "" {
		return gk.Kind
	}
	return fmt.Sprintf("%s/%s", gk.Group, gk.Kind)
}

// GKFilter filters resources by Group/Kind using include or exclude semantics.
type GKFilter struct {
	includeList []GroupKind
	excludeList []GroupKind
}

// NewGKFilter creates a new GKFilter from include and exclude string slices.
func NewGKFilter(include, exclude []string) (*GKFilter, error) {
	if len(include) > 0 && len(exclude) > 0 {
		return nil, fmt.Errorf("cannot use both --include-gk and --exclude-gk")
	}

	f := &GKFilter{}

	for _, s := range include {
		gk, err := ParseGroupKind(s)
		if err != nil {
			return nil, fmt.Errorf("invalid --include-gk %q: %w", s, err)
		}
		f.includeList = append(f.includeList, gk)
	}

	for _, s := range exclude {
		gk, err := ParseGroupKind(s)
		if err != nil {
			return nil, fmt.Errorf("invalid --exclude-gk %q: %w", s, err)
		}
		f.excludeList = append(f.excludeList, gk)
	}

	return f, nil
}

// ShouldInclude returns true if the resource should be included based on the filter.
// Cluster-scoped resources (ClusterRole, ClusterRoleBinding, SCC) are excluded from
// GK filtering to preserve dependency graph resolution handled by ClusterScopeHandler.
func (f *GKFilter) ShouldInclude(gv schema.GroupVersion, resource metav1.APIResource) bool {
	if f == nil {
		return true
	}

	// Skip GK filtering for cluster-scoped admitted resources
	// These are handled by ClusterScopeHandler which maintains dependency graph
	// (e.g., ClusterRole depends on ClusterRoleBinding relationships)
	if !resource.Namespaced && isClusterScopedResource(gv.Group, resource.Kind) {
		return true
	}

	group := gv.Group
	kind := resource.Kind

	// If include list is set, resource must match at least one entry
	if len(f.includeList) > 0 {
		for _, gk := range f.includeList {
			if gk.Matches(group, kind) {
				return true
			}
		}
		return false
	}

	// If exclude list is set, resource must not match any entry
	if len(f.excludeList) > 0 {
		for _, gk := range f.excludeList {
			if gk.Matches(group, kind) {
				return false
			}
		}
		return true
	}

	// No filter set, include everything
	return true
}

// IsEmpty returns true if the filter has no include or exclude rules.
func (f *GKFilter) IsEmpty() bool {
	return f == nil || (len(f.includeList) == 0 && len(f.excludeList) == 0)
}
