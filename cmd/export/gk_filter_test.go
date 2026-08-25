package export

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestParseGroupKind(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    GroupKind
		wantErr bool
	}{
		{
			name:  "Kind only",
			input: "Deployment",
			want:  GroupKind{Group: "", Kind: "Deployment"},
		},
		{
			name:  "Group/Kind",
			input: "apps/Deployment",
			want:  GroupKind{Group: "apps", Kind: "Deployment", GroupSpecified: true},
		},
		{
			name:  "Core group with slash",
			input: "/Pod",
			want:  GroupKind{Group: "", Kind: "Pod", GroupSpecified: true},
		},
		{
			name:  "With whitespace",
			input: "  ConfigMap  ",
			want:  GroupKind{Group: "", Kind: "ConfigMap"},
		},
		{
			name:    "Empty string",
			input:   "",
			wantErr: true,
		},
		{
			name:    "Too many slashes",
			input:   "apps/v1/Deployment",
			wantErr: true,
		},
		{
			name:    "Whitespace only",
			input:   "   ",
			wantErr: true,
		},
		{
			name:    "Empty Kind with group",
			input:   "apps/",
			wantErr: true,
		},
		{
			name:    "Empty Kind with slash",
			input:   "/",
			wantErr: true,
		},
		{
			name:    "Empty Kind with whitespace",
			input:   "apps/  ",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseGroupKind(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseGroupKind() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err == nil && got != tt.want {
				t.Errorf("ParseGroupKind() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGroupKind_Matches(t *testing.T) {
	tests := []struct {
		name  string
		gk    GroupKind
		group string
		kind  string
		want  bool
	}{
		{
			name:  "Kind-only filter matches core group",
			gk:    GroupKind{Group: "", Kind: "Pod"},
			group: "",
			kind:  "Pod",
			want:  true,
		},
		{
			name:  "Kind-only filter matches apps group",
			gk:    GroupKind{Group: "", Kind: "Deployment"},
			group: "apps",
			kind:  "Deployment",
			want:  true,
		},
		{
			name:  "Kind-only filter matches any group",
			gk:    GroupKind{Group: "", Kind: "Secret"},
			group: "custom.io",
			kind:  "Secret",
			want:  true,
		},
		{
			name:  "Group/Kind filter matches exact group",
			gk:    GroupKind{Group: "apps", Kind: "Deployment", GroupSpecified: true},
			group: "apps",
			kind:  "Deployment",
			want:  true,
		},
		{
			name:  "Group/Kind filter does not match different group",
			gk:    GroupKind{Group: "apps", Kind: "Deployment", GroupSpecified: true},
			group: "batch",
			kind:  "Deployment",
			want:  false,
		},
		{
			name:  "Group/Kind filter does not match different kind",
			gk:    GroupKind{Group: "apps", Kind: "Deployment", GroupSpecified: true},
			group: "apps",
			kind:  "StatefulSet",
			want:  false,
		},
		{
			name:  "Kind mismatch",
			gk:    GroupKind{Group: "", Kind: "Pod"},
			group: "",
			kind:  "Service",
			want:  false,
		},
		{
			name:  "Core-group filter (/Pod) matches core Pod",
			gk:    GroupKind{Group: "", Kind: "Pod", GroupSpecified: true},
			group: "",
			kind:  "Pod",
			want:  true,
		},
		{
			name:  "Core-group filter (/Pod) does not match custom group Pod",
			gk:    GroupKind{Group: "", Kind: "Pod", GroupSpecified: true},
			group: "example.io",
			kind:  "Pod",
			want:  false,
		},
		{
			name:  "Kind-only filter (Pod) matches custom group Pod",
			gk:    GroupKind{Group: "", Kind: "Pod", GroupSpecified: false},
			group: "example.io",
			kind:  "Pod",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.gk.Matches(tt.group, tt.kind); got != tt.want {
				t.Errorf("GroupKind.Matches() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGroupKind_String(t *testing.T) {
	tests := []struct {
		name string
		gk   GroupKind
		want string
	}{
		{
			name: "Kind only",
			gk:   GroupKind{Group: "", Kind: "Pod"},
			want: "Pod",
		},
		{
			name: "Group/Kind",
			gk:   GroupKind{Group: "apps", Kind: "Deployment"},
			want: "apps/Deployment",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.gk.String(); got != tt.want {
				t.Errorf("GroupKind.String() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNewGKFilter(t *testing.T) {
	tests := []struct {
		name    string
		include []string
		exclude []string
		wantErr bool
	}{
		{
			name:    "Empty filter",
			include: nil,
			exclude: nil,
			wantErr: false,
		},
		{
			name:    "Include only",
			include: []string{"Deployment", "apps/StatefulSet"},
			exclude: nil,
			wantErr: false,
		},
		{
			name:    "Exclude only",
			include: nil,
			exclude: []string{"Event", "Secret"},
			wantErr: false,
		},
		{
			name:    "Both include and exclude",
			include: []string{"Deployment"},
			exclude: []string{"Event"},
			wantErr: true,
		},
		{
			name:    "Invalid include format",
			include: []string{"apps/v1/Deployment"},
			exclude: nil,
			wantErr: true,
		},
		{
			name:    "Invalid exclude format",
			include: nil,
			exclude: []string{""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewGKFilter(tt.include, tt.exclude)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewGKFilter() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestGKFilter_ShouldInclude(t *testing.T) {
	tests := []struct {
		name     string
		include  []string
		exclude  []string
		group    string
		kind     string
		expected bool
	}{
		{
			name:     "Empty filter includes everything",
			include:  nil,
			exclude:  nil,
			group:    "apps",
			kind:     "Deployment",
			expected: true,
		},
		{
			name:     "Include list matches Kind only",
			include:  []string{"Deployment"},
			exclude:  nil,
			group:    "apps",
			kind:     "Deployment",
			expected: true,
		},
		{
			name:     "Include list matches Group/Kind",
			include:  []string{"apps/Deployment"},
			exclude:  nil,
			group:    "apps",
			kind:     "Deployment",
			expected: true,
		},
		{
			name:     "Include list does not match",
			include:  []string{"Deployment"},
			exclude:  nil,
			group:    "",
			kind:     "Pod",
			expected: false,
		},
		{
			name:     "Include list does not match group",
			include:  []string{"apps/Deployment"},
			exclude:  nil,
			group:    "batch",
			kind:     "Deployment",
			expected: false,
		},
		{
			name:     "Exclude list excludes Event",
			include:  nil,
			exclude:  []string{"Event"},
			group:    "",
			kind:     "Event",
			expected: false,
		},
		{
			name:     "Exclude list includes non-excluded",
			include:  nil,
			exclude:  []string{"Event"},
			group:    "",
			kind:     "Pod",
			expected: true,
		},
		{
			name:     "Exclude list with Group/Kind",
			include:  nil,
			exclude:  []string{"apps/Deployment"},
			group:    "apps",
			kind:     "Deployment",
			expected: false,
		},
		{
			name:     "Exclude list with Group/Kind allows different group",
			include:  nil,
			exclude:  []string{"apps/Deployment"},
			group:    "batch",
			kind:     "Deployment",
			expected: true,
		},
		{
			name:     "Multiple include entries",
			include:  []string{"Deployment", "ConfigMap", "Secret"},
			exclude:  nil,
			group:    "",
			kind:     "ConfigMap",
			expected: true,
		},
		{
			name:     "Multiple include entries no match",
			include:  []string{"Deployment", "ConfigMap", "Secret"},
			exclude:  nil,
			group:    "",
			kind:     "Pod",
			expected: false,
		},
		{
			name:     "Include /Pod matches core Pod",
			include:  []string{"/Pod"},
			exclude:  nil,
			group:    "",
			kind:     "Pod",
			expected: true,
		},
		{
			name:     "Include /Pod does not match custom group Pod",
			include:  []string{"/Pod"},
			exclude:  nil,
			group:    "example.io",
			kind:     "Pod",
			expected: false,
		},
		{
			name:     "Include Pod (no group) matches custom group Pod",
			include:  []string{"Pod"},
			exclude:  nil,
			group:    "example.io",
			kind:     "Pod",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := NewGKFilter(tt.include, tt.exclude)
			if err != nil {
				t.Fatalf("NewGKFilter() error = %v", err)
			}

			gv := schema.GroupVersion{Group: tt.group, Version: "v1"}
			resource := metav1.APIResource{Kind: tt.kind}

			got := filter.ShouldInclude(gv, resource)
			if got != tt.expected {
				t.Errorf("GKFilter.ShouldInclude(%s, %s) = %v, want %v", tt.group, tt.kind, got, tt.expected)
			}
		})
	}
}

func TestGKFilter_IsEmpty(t *testing.T) {
	tests := []struct {
		name    string
		include []string
		exclude []string
		want    bool
	}{
		{
			name:    "Nil filter",
			include: nil,
			exclude: nil,
			want:    true,
		},
		{
			name:    "Empty slices",
			include: []string{},
			exclude: []string{},
			want:    true,
		},
		{
			name:    "Has include",
			include: []string{"Deployment"},
			exclude: nil,
			want:    false,
		},
		{
			name:    "Has exclude",
			include: nil,
			exclude: []string{"Event"},
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := NewGKFilter(tt.include, tt.exclude)
			if err != nil {
				t.Fatalf("NewGKFilter() error = %v", err)
			}
			if got := filter.IsEmpty(); got != tt.want {
				t.Errorf("GKFilter.IsEmpty() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGKFilter_SkipsClusterScopedResources(t *testing.T) {
	tests := []struct {
		name        string
		include     []string
		exclude     []string
		group       string
		kind        string
		namespaced  bool
		expected    bool
		description string
	}{
		{
			name:        "ClusterRole excluded but still included (cluster-scoped)",
			include:     nil,
			exclude:     []string{"ClusterRole"},
			group:       "rbac.authorization.k8s.io",
			kind:        "ClusterRole",
			namespaced:  false,
			expected:    true,
			description: "Cluster-scoped admitted resources bypass GK filter",
		},
		{
			name:        "ClusterRoleBinding in include-only mode still included",
			include:     []string{"Deployment"},
			exclude:     nil,
			group:       "rbac.authorization.k8s.io",
			kind:        "ClusterRoleBinding",
			namespaced:  false,
			expected:    true,
			description: "ClusterRoleBinding bypasses whitelist filter",
		},
		{
			name:        "SecurityContextConstraints excluded but still included",
			include:     nil,
			exclude:     []string{"SecurityContextConstraints"},
			group:       "security.openshift.io",
			kind:        "SecurityContextConstraints",
			namespaced:  false,
			expected:    true,
			description: "SCC bypasses GK filter",
		},
		{
			name:        "Namespace (cluster-scoped but not admitted) respects exclude filter",
			include:     nil,
			exclude:     []string{"Namespace"},
			group:       "",
			kind:        "Namespace",
			namespaced:  false,
			expected:    false,
			description: "Non-admitted cluster resources are filtered normally",
		},
		{
			name:        "Namespaced Role respects exclude filter",
			include:     nil,
			exclude:     []string{"Role"},
			group:       "rbac.authorization.k8s.io",
			kind:        "Role",
			namespaced:  true,
			expected:    false,
			description: "Namespaced resources are filtered normally",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filter, err := NewGKFilter(tt.include, tt.exclude)
			if err != nil {
				t.Fatalf("NewGKFilter() error = %v", err)
			}

			gv := schema.GroupVersion{Group: tt.group, Version: "v1"}
			resource := metav1.APIResource{Kind: tt.kind, Namespaced: tt.namespaced}

			got := filter.ShouldInclude(gv, resource)
			if got != tt.expected {
				t.Errorf("%s: GKFilter.ShouldInclude() = %v, want %v", tt.description, got, tt.expected)
			}
		})
	}
}
