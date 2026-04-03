package export

import (
	"io"
	"sort"
	"testing"

	securityv1 "github.com/openshift/api/security/v1"
	"github.com/sirupsen/logrus"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func testLogger() logrus.FieldLogger {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return l
}

func clusterRoleBindingToUnstructured(t *testing.T, crb *rbacv1.ClusterRoleBinding) unstructured.Unstructured {
	t.Helper()
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(crb)
	if err != nil {
		t.Fatalf("ToUnstructured: %v", err)
	}
	return unstructured.Unstructured{Object: u}
}

// clusterRoleBindingUnstructuredThatFailsConversion yields an object that does not convert to rbacv1.ClusterRoleBinding.
func clusterRoleBindingUnstructuredThatFailsConversion() unstructured.Unstructured {
	return unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "rbac.authorization.k8s.io/v1",
			"kind":       "ClusterRoleBinding",
			"metadata":   map[string]interface{}{"name": "bad"},
			"subjects":   "not-a-list",
		},
	}
}

func clusterRoleToUnstructured(t *testing.T, cr *rbacv1.ClusterRole) unstructured.Unstructured {
	t.Helper()
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cr)
	if err != nil {
		t.Fatalf("ToUnstructured: %v", err)
	}
	out := unstructured.Unstructured{Object: u}
	// Typed objects often have no TypeMeta; ClusterScopedRbacHandler.accept matches on GVK.
	out.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind("ClusterRole"))
	return out
}

func configMapGroupResource(t *testing.T, ns, name string) *groupResource {
	t.Helper()
	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
	uobj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(cm)
	if err != nil {
		t.Fatalf("ToUnstructured: %v", err)
	}
	return &groupResource{
		APIGroup:        "",
		APIVersion:      "v1",
		APIGroupVersion: "v1",
		APIResource:     metav1.APIResource{Kind: "ConfigMap", Name: "configmaps"},
		objects:         &unstructured.UnstructuredList{Items: []unstructured.Unstructured{{Object: uobj}}},
	}
}

func serviceAccountGroupResource(t *testing.T, ns, name string) *groupResource {
	t.Helper()
	sa := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Namespace: ns, Name: name}}
	uobj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(sa)
	if err != nil {
		t.Fatalf("ToUnstructured: %v", err)
	}
	return &groupResource{
		APIGroup:        "",
		APIVersion:      "v1",
		APIGroupVersion: "v1",
		APIResource:     metav1.APIResource{Kind: "ServiceAccount", Name: "serviceaccounts"},
		objects:         &unstructured.UnstructuredList{Items: []unstructured.Unstructured{{Object: uobj}}},
	}
}

func clusterRoleBindingGroupResource(t *testing.T, crbs ...*rbacv1.ClusterRoleBinding) *groupResource {
	t.Helper()
	items := make([]unstructured.Unstructured, 0, len(crbs))
	for _, crb := range crbs {
		items = append(items, clusterRoleBindingToUnstructured(t, crb))
	}
	return &groupResource{
		APIGroup:        rbacv1.GroupName,
		APIVersion:      rbacv1.SchemeGroupVersion.Version,
		APIGroupVersion: rbacv1.SchemeGroupVersion.String(),
		APIResource: metav1.APIResource{
			Group: rbacv1.GroupName, Kind: "ClusterRoleBinding", Name: "clusterrolebindings",
		},
		objects: &unstructured.UnstructuredList{Items: items},
	}
}

func clusterRoleGroupResource(t *testing.T, crs ...*rbacv1.ClusterRole) *groupResource {
	t.Helper()
	items := make([]unstructured.Unstructured, 0, len(crs))
	for _, cr := range crs {
		items = append(items, clusterRoleToUnstructured(t, cr))
	}
	return &groupResource{
		APIGroup:        rbacv1.GroupName,
		APIVersion:      rbacv1.SchemeGroupVersion.Version,
		APIGroupVersion: rbacv1.SchemeGroupVersion.String(),
		APIResource: metav1.APIResource{
			Group: rbacv1.GroupName, Kind: "ClusterRole", Name: "clusterroles",
		},
		objects: &unstructured.UnstructuredList{Items: items},
	}
}

func kindNames(resources []*groupResource) (kinds, names []string) {
	for _, r := range resources {
		kinds = append(kinds, r.APIResource.Kind)
		switch r.APIResource.Kind {
		case "ClusterRoleBinding", "ClusterRole", "SecurityContextConstraints":
			for _, it := range r.objects.Items {
				names = append(names, r.APIResource.Kind+"/"+it.GetName())
			}
		default:
			for _, it := range r.objects.Items {
				names = append(names, r.APIResource.Kind+"/"+it.GetNamespace()+"/"+it.GetName())
			}
		}
	}
	sort.Strings(kinds)
	sort.Strings(names)
	return kinds, names
}

func securityContextConstraintsToUnstructured(t *testing.T, scc *securityv1.SecurityContextConstraints) unstructured.Unstructured {
	t.Helper()
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(scc)
	if err != nil {
		t.Fatalf("ToUnstructured: %v", err)
	}
	out := unstructured.Unstructured{Object: u}
	out.SetGroupVersionKind(securityv1.SchemeGroupVersion.WithKind("SecurityContextConstraints"))
	return out
}

func sccGroupResource(t *testing.T, sccs ...*securityv1.SecurityContextConstraints) *groupResource {
	t.Helper()
	items := make([]unstructured.Unstructured, 0, len(sccs))
	for _, scc := range sccs {
		items = append(items, securityContextConstraintsToUnstructured(t, scc))
	}
	return &groupResource{
		APIGroup:        securityv1.GroupName,
		APIVersion:      securityv1.SchemeGroupVersion.Version,
		APIGroupVersion: securityv1.SchemeGroupVersion.String(),
		APIResource: metav1.APIResource{
			Group: securityv1.GroupName, Kind: "SecurityContextConstraints", Name: "securitycontextconstraints",
		},
		objects: &unstructured.UnstructuredList{Items: items},
	}
}

func TestParseServiceAccountUserSubject(t *testing.T) {
	tests := []struct {
		user     string
		wantNS   string
		wantSA   string
		wantOK   bool
	}{
		{"system:serviceaccount:my-ns:app-sa", "my-ns", "app-sa", true},
		{"system:serviceaccount:my-ns:", "", "", false},
		{"system:serviceaccount::app-sa", "", "", false},
		{"user:alice", "", "", false},
		{"system:authenticated", "", "", false},
		{"system:serviceaccounts:my-ns", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.user, func(t *testing.T) {
			ns, sa, ok := parseServiceAccountUserSubject(tt.user)
			if ok != tt.wantOK || ns != tt.wantNS || sa != tt.wantSA {
				t.Fatalf("parseServiceAccountUserSubject(%q) = (%q,%q,%v), want (%q,%q,%v)",
					tt.user, ns, sa, ok, tt.wantNS, tt.wantSA, tt.wantOK)
			}
		})
	}
}

func TestGroupMatchesExportedSANamespaces(t *testing.T) {
	nsSet := map[string]struct{}{"my-ns": {}, "other": {}}
	tests := []struct {
		group string
		want  bool
	}{
		{"system:serviceaccounts:my-ns", true},
		{"system:serviceaccounts:other", true},
		{"system:serviceaccounts:foreign", false},
		{"system:authenticated", false},
		{"system:serviceaccounts:", false},
		{"", false},
	}
	for _, tt := range tests {
		if got := groupMatchesExportedSANamespaces(tt.group, nsSet); got != tt.want {
			t.Errorf("groupMatchesExportedSANamespaces(%q) = %v, want %v", tt.group, got, tt.want)
		}
	}
	if groupMatchesExportedSANamespaces("system:serviceaccounts:my-ns", nil) {
		t.Error("empty nsSet should not match")
	}
}

func TestIsClusterScopedResource(t *testing.T) {
	tests := []struct {
		group, kind string
		want        bool
	}{
		{rbacv1.GroupName, "ClusterRoleBinding", true},
		{rbacv1.GroupName, "ClusterRole", true},
		{securityv1.GroupName, "SecurityContextConstraints", true},
		{rbacv1.GroupName, "Role", false},
		{"wrong.group", "ClusterRole", false},
		{"", "ClusterRole", false},
		{rbacv1.GroupName, "UnknownKind", false},
	}
	for _, tt := range tests {
		key := tt.group + "/" + tt.kind
		t.Run(key, func(t *testing.T) {
			if got := isClusterScopedResource(tt.group, tt.kind); got != tt.want {
				t.Fatalf("isClusterScopedResource(%q,%q) = %v, want %v", tt.group, tt.kind, got, tt.want)
			}
		})
	}
}

func TestExportedSANamespaces_skipsEmptyNamespace(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	s1 := unstructured.Unstructured{}
	s1.SetName("no-ns-sa")
	h.serviceAccounts = []unstructured.Unstructured{s1}
	s2 := unstructured.Unstructured{}
	s2.SetNamespace("has-ns")
	s2.SetName("x")
	h.serviceAccounts = append(h.serviceAccounts, s2)
	m := h.exportedSANamespaces()
	if len(m) != 1 {
		t.Fatalf("want 1 namespace (empty ns ignored), got %#v", m)
	}
	if _, ok := m["has-ns"]; !ok {
		t.Fatalf("expected only has-ns, got %#v", m)
	}
}

func TestAcceptClusterRoleBinding_malformedUnstructured(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	sa := unstructured.Unstructured{}
	sa.SetName("app-sa")
	sa.SetNamespace("my-ns")
	h.serviceAccounts = []unstructured.Unstructured{sa}
	u := clusterRoleBindingUnstructuredThatFailsConversion()
	if h.acceptClusterRoleBinding(u) {
		t.Fatal("expected reject when CRB does not convert")
	}
}

func TestAcceptClusterRoleBinding_emptySubjects(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	sa := unstructured.Unstructured{}
	sa.SetName("app-sa")
	sa.SetNamespace("my-ns")
	h.serviceAccounts = []unstructured.Unstructured{sa}
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "empty-subjects"},
		Subjects:   nil,
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "r1"},
	}
	u := clusterRoleBindingToUnstructured(t, crb)
	if h.acceptClusterRoleBinding(u) {
		t.Fatal("expected reject with no subjects")
	}
}

func TestAcceptClusterRoleBinding_serviceAccountWrongNamespace(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	sa := unstructured.Unstructured{}
	sa.SetName("app-sa")
	sa.SetNamespace("my-ns")
	h.serviceAccounts = []unstructured.Unstructured{sa}
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "crb-wrong-ns"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Namespace: "other-ns", Name: "app-sa"},
		},
		RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "r1"},
	}
	u := clusterRoleBindingToUnstructured(t, crb)
	if h.acceptClusterRoleBinding(u) {
		t.Fatal("expected reject when ServiceAccount subject namespace does not match an exported SA")
	}
}

func TestAcceptClusterRoleBinding_unhandledSubjectKindIgnored(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	sa := unstructured.Unstructured{}
	sa.SetName("app-sa")
	sa.SetNamespace("my-ns")
	h.serviceAccounts = []unstructured.Unstructured{sa}
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "crb-unknown-subject"},
		Subjects: []rbacv1.Subject{
			{Kind: "SomeOtherKind", Name: "x"},
		},
		RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "r1"},
	}
	u := clusterRoleBindingToUnstructured(t, crb)
	if h.acceptClusterRoleBinding(u) {
		t.Fatal("expected reject when no subject matches ServiceAccount, Group, or User")
	}
}

func TestAccept_rejectsUnknownGroupVersionKind(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	u := unstructured.Unstructured{}
	u.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "Pod"})
	u.SetName("p1")
	if h.accept("ClusterRole", u) {
		t.Fatal("accept should return false when object GVK is not ClusterRole or SCC")
	}
}

func TestAcceptClusterRole_fromUnstructuredFails(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	h.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}},
	}
	u := unstructured.Unstructured{}
	u.SetGroupVersionKind(rbacv1.SchemeGroupVersion.WithKind("ClusterRole"))
	u.Object["metadata"] = "not-an-object"
	if h.acceptClusterRole(u) {
		t.Fatal("expected reject when ClusterRole does not convert")
	}
}

func TestAcceptClusterRole_skipsMalformedFilteredCRB(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	good := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "good-crb"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Namespace: "ns1", Name: "sa1"},
		},
		RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "linked-role"},
	}
	h.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			clusterRoleBindingUnstructuredThatFailsConversion(),
			clusterRoleBindingToUnstructured(t, good),
		}},
	}
	cr := clusterRoleToUnstructured(t, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "linked-role"}})
	if !h.acceptClusterRole(cr) {
		t.Fatal("expected accept after skipping CRB that fails conversion")
	}
}

func TestAcceptClusterRole_roleRefKindNotClusterRole(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "bind-role-not-cluster"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Namespace: "ns1", Name: "sa1"},
		},
		RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "Role", Name: "same-name"},
	}
	h.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			clusterRoleBindingToUnstructured(t, crb),
		}},
	}
	cr := clusterRoleToUnstructured(t, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "same-name"}})
	if h.acceptClusterRole(cr) {
		t.Fatal("expected reject when CRB RoleRef.Kind is not ClusterRole")
	}
}

func TestAcceptClusterRole_roleRefNameMismatch(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "bind"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Namespace: "ns1", Name: "sa1"},
		},
		RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "wanted"},
	}
	h.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			clusterRoleBindingToUnstructured(t, crb),
		}},
	}
	cr := clusterRoleToUnstructured(t, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "other"}})
	if h.acceptClusterRole(cr) {
		t.Fatal("expected reject when ClusterRole name does not match RoleRef.Name")
	}
}

func TestAcceptSecurityContextConstraints_fromUnstructuredFails(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	h.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}},
	}
	u := unstructured.Unstructured{}
	u.SetGroupVersionKind(securityv1.SchemeGroupVersion.WithKind("SecurityContextConstraints"))
	u.Object["metadata"] = "invalid"
	if h.acceptSecurityContextConstraints(u) {
		t.Fatal("expected reject when SCC does not convert")
	}
}

func TestAcceptSecurityContextConstraints_malformedUsersIgnoredAcceptsViaGroup(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	sa := unstructured.Unstructured{}
	sa.SetName("app-sa")
	sa.SetNamespace("my-ns")
	h.serviceAccounts = []unstructured.Unstructured{sa}
	h.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}},
	}
	scc := &securityv1.SecurityContextConstraints{
		ObjectMeta: metav1.ObjectMeta{Name: "scc-mixed"},
		Users:      []string{"not-a-serviceaccount-user", "system:serviceaccount:my-ns:wrong-sa"},
		Groups:     []string{"system:serviceaccounts:my-ns"},
	}
	u := securityContextConstraintsToUnstructured(t, scc)
	if !h.acceptSecurityContextConstraints(u) {
		t.Fatal("expected SCC accepted via Groups after Users entries do not match")
	}
}

func TestFilteredResourcesOfKind_prepareWithoutClusterRoleBindingUsesNAPlaceholder(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	h.clusterResources["ClusterRole"] = clusterRoleGroupResource(t, &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "orphan-cr"},
	})
	_, ok := h.filteredResourcesOfKind(admittedClusterScopeResources[1])
	if !ok {
		t.Fatal("expected ClusterRole in clusterResources")
	}
	if h.filteredClusterRoleBindings == nil || h.filteredClusterRoleBindings.APIGroup != "NA" {
		t.Fatalf("expected NA placeholder filtered CRB holder, got %#v", h.filteredClusterRoleBindings)
	}
	if h.filteredClusterRoleBindings.objects == nil || len(h.filteredClusterRoleBindings.objects.Items) != 0 {
		t.Fatal("expected empty filtered CRB list when no ClusterRoleBinding was collected")
	}
}

func TestFilterRbacResources_nonAdmittedAPIGroupPassesResourceThrough(t *testing.T) {
	h := NewClusterScopeHandler()
	gr := clusterRoleGroupResource(t, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "leaked"}})
	gr.APIGroup = "example.com"
	gr.APIGroupVersion = "example.com/v1"
	out := h.filterRbacResources([]*groupResource{gr}, testLogger())
	if len(out) != 1 || out[0].APIResource.Kind != "ClusterRole" {
		t.Fatalf("expected cluster-shaped resource with non-admitted APIGroup to pass through unfiltered, got %#v", out)
	}
}

func TestFilterRbacResources_onlyClusterRoleNoBindingDropped(t *testing.T) {
	h := NewClusterScopeHandler()
	cr := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "only-cr"}}
	out := h.filterRbacResources([]*groupResource{clusterRoleGroupResource(t, cr)}, testLogger())
	if len(out) != 0 {
		t.Fatalf("expected no output (CR not linked without CRB), got %d resources", len(out))
	}
}

func TestFilterRbacResources_clusterScopedNoneAcceptedKeepsPassthroughOnly(t *testing.T) {
	h := NewClusterScopeHandler()
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "no-match"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Namespace: "other", Name: "sa"},
		},
		RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "r1"},
	}
	cr := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "r1"}}
	in := []*groupResource{
		serviceAccountGroupResource(t, "my-ns", "app-sa"),
		clusterRoleBindingGroupResource(t, crb),
		clusterRoleGroupResource(t, cr),
	}
	out := h.filterRbacResources(in, testLogger())
	if len(out) != 1 || out[0].APIResource.Kind != "ServiceAccount" {
		t.Fatalf("expected only ServiceAccount kept, got %d resources kinds=%v", len(out), resourceKindsList(out))
	}
}

func resourceKindsList(resources []*groupResource) []string {
	var ks []string
	for _, r := range resources {
		ks = append(ks, r.APIResource.Kind)
	}
	return ks
}

func TestAcceptClusterRoleBinding_Subjects(t *testing.T) {
	roleRef := rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "role1"}

	t.Run("ServiceAccount subject", func(t *testing.T) {
		h := NewClusterScopedRbacHandler(testLogger())
		sa := unstructured.Unstructured{}
		sa.SetName("app-sa")
		sa.SetNamespace("my-ns")
		h.serviceAccounts = []unstructured.Unstructured{sa}

		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "crb-sa"},
			Subjects: []rbacv1.Subject{
				{Kind: rbacv1.ServiceAccountKind, Namespace: "my-ns", Name: "app-sa"},
			},
			RoleRef: roleRef,
		}
		u := clusterRoleBindingToUnstructured(t, crb)
		if !h.acceptClusterRoleBinding(u) {
			t.Fatal("expected accept for ServiceAccount subject")
		}
	})

	t.Run("Group system:serviceaccounts namespace", func(t *testing.T) {
		h := NewClusterScopedRbacHandler(testLogger())
		sa := unstructured.Unstructured{}
		sa.SetName("any-sa")
		sa.SetNamespace("my-ns")
		h.serviceAccounts = []unstructured.Unstructured{sa}

		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "crb-group"},
			Subjects: []rbacv1.Subject{
				{Kind: rbacv1.GroupKind, APIGroup: rbacv1.GroupName, Name: "system:serviceaccounts:my-ns"},
			},
			RoleRef: roleRef,
		}
		u := clusterRoleBindingToUnstructured(t, crb)
		if !h.acceptClusterRoleBinding(u) {
			t.Fatal("expected accept for Group subject matching exported namespace")
		}
	})

	t.Run("Group other namespace rejected", func(t *testing.T) {
		h := NewClusterScopedRbacHandler(testLogger())
		sa := unstructured.Unstructured{}
		sa.SetName("app-sa")
		sa.SetNamespace("my-ns")
		h.serviceAccounts = []unstructured.Unstructured{sa}

		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "crb-wrong-ns"},
			Subjects: []rbacv1.Subject{
				{Kind: rbacv1.GroupKind, APIGroup: rbacv1.GroupName, Name: "system:serviceaccounts:foreign"},
			},
			RoleRef: roleRef,
		}
		u := clusterRoleBindingToUnstructured(t, crb)
		if h.acceptClusterRoleBinding(u) {
			t.Fatal("expected reject for Group in non-exported namespace")
		}
	})

	t.Run("Group system authenticated rejected", func(t *testing.T) {
		h := NewClusterScopedRbacHandler(testLogger())
		sa := unstructured.Unstructured{}
		sa.SetName("app-sa")
		sa.SetNamespace("my-ns")
		h.serviceAccounts = []unstructured.Unstructured{sa}

		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "crb-auth"},
			Subjects: []rbacv1.Subject{
				{Kind: rbacv1.GroupKind, APIGroup: rbacv1.GroupName, Name: "system:authenticated"},
			},
			RoleRef: roleRef,
		}
		u := clusterRoleBindingToUnstructured(t, crb)
		if h.acceptClusterRoleBinding(u) {
			t.Fatal("expected reject for system:authenticated")
		}
	})

	t.Run("User system serviceaccount string", func(t *testing.T) {
		h := NewClusterScopedRbacHandler(testLogger())
		sa := unstructured.Unstructured{}
		sa.SetName("app-sa")
		sa.SetNamespace("my-ns")
		h.serviceAccounts = []unstructured.Unstructured{sa}

		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "crb-user"},
			Subjects: []rbacv1.Subject{
				{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: "system:serviceaccount:my-ns:app-sa"},
			},
			RoleRef: roleRef,
		}
		u := clusterRoleBindingToUnstructured(t, crb)
		if !h.acceptClusterRoleBinding(u) {
			t.Fatal("expected accept for User subject with SA principal string")
		}
	})

	t.Run("User malformed rejected", func(t *testing.T) {
		h := NewClusterScopedRbacHandler(testLogger())
		sa := unstructured.Unstructured{}
		sa.SetName("app-sa")
		sa.SetNamespace("my-ns")
		h.serviceAccounts = []unstructured.Unstructured{sa}

		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "crb-bad-user"},
			Subjects: []rbacv1.Subject{
				{Kind: rbacv1.UserKind, APIGroup: rbacv1.GroupName, Name: "not-a-sa-string"},
			},
			RoleRef: roleRef,
		}
		u := clusterRoleBindingToUnstructured(t, crb)
		if h.acceptClusterRoleBinding(u) {
			t.Fatal("expected reject for malformed User subject")
		}
	})

	t.Run("no exported SAs", func(t *testing.T) {
		h := NewClusterScopedRbacHandler(testLogger())
		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{Name: "crb"},
			Subjects: []rbacv1.Subject{
				{Kind: rbacv1.GroupKind, APIGroup: rbacv1.GroupName, Name: "system:serviceaccounts:my-ns"},
			},
			RoleRef: roleRef,
		}
		u := clusterRoleBindingToUnstructured(t, crb)
		if h.acceptClusterRoleBinding(u) {
			t.Fatal("expected reject with no exported service accounts")
		}
	})
}

func TestAcceptSecurityContextConstraints_Groups(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	sa := unstructured.Unstructured{}
	sa.SetName("app-sa")
	sa.SetNamespace("my-ns")
	h.serviceAccounts = []unstructured.Unstructured{sa}
	h.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}},
	}

	scc := &securityv1.SecurityContextConstraints{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-scc"},
		Groups:     []string{"system:serviceaccounts:my-ns"},
		Users:      nil,
	}
	u := securityContextConstraintsToUnstructured(t, scc)
	if !h.acceptSecurityContextConstraints(u) {
		t.Fatal("expected SCC accepted via groups only")
	}
}

func TestAcceptSecurityContextConstraints_GroupWrongNamespace(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	sa := unstructured.Unstructured{}
	sa.SetName("app-sa")
	sa.SetNamespace("my-ns")
	h.serviceAccounts = []unstructured.Unstructured{sa}
	h.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}},
	}

	scc := &securityv1.SecurityContextConstraints{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-scc"},
		Groups:     []string{"system:serviceaccounts:other-ns"},
		Users:      nil,
	}
	u := securityContextConstraintsToUnstructured(t, scc)
	if h.acceptSecurityContextConstraints(u) {
		t.Fatal("expected SCC rejected when group namespace does not match exported SAs")
	}
}

func TestAcceptSecurityContextConstraints_Users(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	sa := unstructured.Unstructured{}
	sa.SetName("app-sa")
	sa.SetNamespace("my-ns")
	h.serviceAccounts = []unstructured.Unstructured{sa}
	h.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}},
	}
	scc := &securityv1.SecurityContextConstraints{
		ObjectMeta: metav1.ObjectMeta{Name: "scc-users"},
		Users:      []string{"system:serviceaccount:my-ns:app-sa"},
		Groups:     nil,
	}
	u := securityContextConstraintsToUnstructured(t, scc)
	if !h.acceptSecurityContextConstraints(u) {
		t.Fatal("expected SCC accepted via Users list")
	}
}

func TestAcceptSecurityContextConstraints_UsersWrongServiceAccount(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	sa := unstructured.Unstructured{}
	sa.SetName("app-sa")
	sa.SetNamespace("my-ns")
	h.serviceAccounts = []unstructured.Unstructured{sa}
	h.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}},
	}
	scc := &securityv1.SecurityContextConstraints{
		ObjectMeta: metav1.ObjectMeta{Name: "scc-users"},
		Users:      []string{"system:serviceaccount:other-ns:app-sa"},
		Groups:     nil,
	}
	u := securityContextConstraintsToUnstructured(t, scc)
	if h.acceptSecurityContextConstraints(u) {
		t.Fatal("expected SCC rejected when Users entry does not match an exported SA")
	}
}

func TestAcceptSecurityContextConstraints_viaClusterRoleBindingRoleRefSCC(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "crb-use-scc"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Namespace: "ns1", Name: "sa1"},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: securityv1.GroupName,
			Kind:     "SecurityContextConstraints",
			Name:     "custom-scc",
		},
	}
	h.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			clusterRoleBindingToUnstructured(t, crb),
		}},
	}
	scc := &securityv1.SecurityContextConstraints{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-scc"},
	}
	u := securityContextConstraintsToUnstructured(t, scc)
	if !h.acceptSecurityContextConstraints(u) {
		t.Fatal("expected SCC accepted when a filtered CRB references it by RoleRef")
	}
}

func TestAcceptSecurityContextConstraints_viaSystemClusterRoleName(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "crb-scc-system-role"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Namespace: "ns1", Name: "sa1"},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "system:openshift:scc:restricted",
		},
	}
	h.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			clusterRoleBindingToUnstructured(t, crb),
		}},
	}
	scc := &securityv1.SecurityContextConstraints{
		ObjectMeta: metav1.ObjectMeta{Name: "restricted"},
	}
	u := securityContextConstraintsToUnstructured(t, scc)
	if !h.acceptSecurityContextConstraints(u) {
		t.Fatal("expected SCC accepted via system:openshift:scc:<name> ClusterRole RoleRef")
	}
}

func TestAcceptSecurityContextConstraints_skipsMalformedCRBThenMatches(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	good := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "good"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Namespace: "ns1", Name: "sa1"},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: securityv1.GroupName,
			Kind:     "SecurityContextConstraints",
			Name:     "linked-scc",
		},
	}
	h.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			clusterRoleBindingUnstructuredThatFailsConversion(),
			clusterRoleBindingToUnstructured(t, good),
		}},
	}
	scc := &securityv1.SecurityContextConstraints{ObjectMeta: metav1.ObjectMeta{Name: "linked-scc"}}
	u := securityContextConstraintsToUnstructured(t, scc)
	if !h.acceptSecurityContextConstraints(u) {
		t.Fatal("expected SCC accepted after skipping CRB that does not convert")
	}
}

func TestAcceptSecurityContextConstraints_roleRefSCCNameMismatch(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "crb"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Namespace: "ns1", Name: "sa1"},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: securityv1.GroupName,
			Kind:     "SecurityContextConstraints",
			Name:     "wanted-scc",
		},
	}
	h.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{
			clusterRoleBindingToUnstructured(t, crb),
		}},
	}
	scc := &securityv1.SecurityContextConstraints{ObjectMeta: metav1.ObjectMeta{Name: "other-scc"}}
	u := securityContextConstraintsToUnstructured(t, scc)
	if h.acceptSecurityContextConstraints(u) {
		t.Fatal("expected reject when CRB RoleRef name does not match SCC metadata name")
	}
}

func TestFilterRbacResources_keepsSecurityContextConstraintsViaRoleRef(t *testing.T) {
	h := NewClusterScopeHandler()
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "crb-scc"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Namespace: "my-ns", Name: "app-sa"},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: securityv1.GroupName,
			Kind:     "SecurityContextConstraints",
			Name:     "export-scc",
		},
	}
	scc := &securityv1.SecurityContextConstraints{ObjectMeta: metav1.ObjectMeta{Name: "export-scc"}}
	in := []*groupResource{
		serviceAccountGroupResource(t, "my-ns", "app-sa"),
		clusterRoleBindingGroupResource(t, crb),
		sccGroupResource(t, scc),
	}
	out := h.filterRbacResources(in, testLogger())
	if len(out) != 3 {
		t.Fatalf("want SA + CRB + SCC, got %d", len(out))
	}
	if !containsSorted(kindNamesOnly(out), "SecurityContextConstraints/export-scc") {
		t.Fatalf("missing SCC: %v", kindNamesOnly(out))
	}
}

func TestFilterRbacResources_keepsSecurityContextConstraintsViaSystemClusterRole(t *testing.T) {
	h := NewClusterScopeHandler()
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "crb-sys"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Namespace: "my-ns", Name: "app-sa"},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "system:openshift:scc:custom-scc",
		},
	}
	scc := &securityv1.SecurityContextConstraints{ObjectMeta: metav1.ObjectMeta{Name: "custom-scc"}}
	in := []*groupResource{
		serviceAccountGroupResource(t, "my-ns", "app-sa"),
		clusterRoleBindingGroupResource(t, crb),
		sccGroupResource(t, scc),
	}
	out := h.filterRbacResources(in, testLogger())
	if len(out) != 3 {
		t.Fatalf("want SA + CRB + SCC, got %d", len(out))
	}
}

func TestFilterRbacResources_keepsSecurityContextConstraintsViaUsersOnly(t *testing.T) {
	h := NewClusterScopeHandler()
	scc := &securityv1.SecurityContextConstraints{
		ObjectMeta: metav1.ObjectMeta{Name: "scc-by-user"},
		Users:      []string{"system:serviceaccount:my-ns:app-sa"},
	}
	in := []*groupResource{
		serviceAccountGroupResource(t, "my-ns", "app-sa"),
		sccGroupResource(t, scc),
	}
	out := h.filterRbacResources(in, testLogger())
	if len(out) != 2 {
		t.Fatalf("want SA + SCC, got %d", len(out))
	}
	if !containsSorted(kindNamesOnly(out), "SecurityContextConstraints/scc-by-user") {
		t.Fatalf("missing SCC: %v", kindNamesOnly(out))
	}
}

func kindNamesOnly(resources []*groupResource) []string {
	_, names := kindNames(resources)
	return names
}

func TestFilterRbacResources_namespaceResourcesUnchanged(t *testing.T) {
	h := NewClusterScopeHandler()
	cm := configMapGroupResource(t, "ns1", "cm1")
	out := h.filterRbacResources([]*groupResource{cm}, testLogger())
	if len(out) != 1 || out[0].objects.Items[0].GetName() != "cm1" {
		t.Fatalf("got %#v", out)
	}
}

func TestFilterRbacResources_dropsUnmatchedClusterRBAC(t *testing.T) {
	h := NewClusterScopeHandler()
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "orphan-crb"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Namespace: "missing-ns", Name: "sa"},
		},
		RoleRef: rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "any-role"},
	}
	cr := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "any-role"}}
	out := h.filterRbacResources([]*groupResource{
		clusterRoleBindingGroupResource(t, crb),
		clusterRoleGroupResource(t, cr),
	}, testLogger())
	if len(out) != 0 {
		t.Fatalf("expected no resources (no exported SAs, CRB rejected, CR not linked), got %d", len(out))
	}
}

func TestFilterRbacResources_keepsLinkedClusterRoleBindingAndClusterRole(t *testing.T) {
	h := NewClusterScopeHandler()
	roleRef := rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "export-role"}
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "link-crb"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.ServiceAccountKind, Namespace: "my-ns", Name: "app-sa"},
		},
		RoleRef: roleRef,
	}
	crKeep := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "export-role"}}
	crDrop := &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "unrelated-role"}}
	in := []*groupResource{
		configMapGroupResource(t, "my-ns", "cm1"),
		serviceAccountGroupResource(t, "my-ns", "app-sa"),
		clusterRoleBindingGroupResource(t, crb),
		clusterRoleGroupResource(t, crKeep, crDrop),
	}
	out := h.filterRbacResources(in, testLogger())
	kinds, objNames := kindNames(out)
	wantKinds := []string{"ClusterRole", "ClusterRoleBinding", "ConfigMap", "ServiceAccount"}
	if len(kinds) != len(wantKinds) {
		t.Fatalf("kinds len: got %v", kinds)
	}
	for i := range wantKinds {
		if kinds[i] != wantKinds[i] {
			t.Fatalf("kinds: got %v want %v", kinds, wantKinds)
		}
	}
	if !containsSorted(objNames, "ClusterRoleBinding/link-crb") {
		t.Fatalf("missing CRB in %v", objNames)
	}
	if !containsSorted(objNames, "ClusterRole/export-role") {
		t.Fatalf("missing linked ClusterRole in %v", objNames)
	}
	if containsSorted(objNames, "ClusterRole/unrelated-role") {
		t.Fatalf("unrelated ClusterRole should be removed: %v", objNames)
	}
}

func TestFilterRbacResources_acceptsClusterRoleBindingViaGroupSubject(t *testing.T) {
	h := NewClusterScopeHandler()
	roleRef := rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "r1"}
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "crb-group"},
		Subjects: []rbacv1.Subject{
			{Kind: rbacv1.GroupKind, APIGroup: rbacv1.GroupName, Name: "system:serviceaccounts:ns1"},
		},
		RoleRef: roleRef,
	}
	in := []*groupResource{
		serviceAccountGroupResource(t, "ns1", "any-sa"),
		clusterRoleBindingGroupResource(t, crb),
		clusterRoleGroupResource(t, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "r1"}}),
	}
	out := h.filterRbacResources(in, testLogger())
	if len(out) != 3 {
		t.Fatalf("want SA + filtered CRB + filtered ClusterRole (3), got %d", len(out))
	}
}

func TestFilterRbacResources_filtersMultipleClusterRoleBindingsInOneList(t *testing.T) {
	h := NewClusterScopeHandler()
	roleRef := rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "r1"}
	crbGood := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "good"},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Namespace: "ns1", Name: "sa1"}},
		RoleRef:    roleRef,
	}
	crbBad := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "bad"},
		Subjects:   []rbacv1.Subject{{Kind: rbacv1.ServiceAccountKind, Namespace: "other", Name: "sa1"}},
		RoleRef:    roleRef,
	}
	in := []*groupResource{
		serviceAccountGroupResource(t, "ns1", "sa1"),
		clusterRoleBindingGroupResource(t, crbGood, crbBad),
		clusterRoleGroupResource(t, &rbacv1.ClusterRole{ObjectMeta: metav1.ObjectMeta{Name: "r1"}}),
	}
	out := h.filterRbacResources(in, testLogger())
	var crbOut *groupResource
	for _, r := range out {
		if r.APIResource.Kind == "ClusterRoleBinding" {
			crbOut = r
			break
		}
	}
	if crbOut == nil || len(crbOut.objects.Items) != 1 || crbOut.objects.Items[0].GetName() != "good" {
		t.Fatalf("expected single accepted CRB, got %#v", crbOut)
	}
}

func containsSorted(sortedNames []string, want string) bool {
	i := sort.SearchStrings(sortedNames, want)
	return i < len(sortedNames) && sortedNames[i] == want
}

func TestExportedSANamespaces(t *testing.T) {
	h := NewClusterScopedRbacHandler(testLogger())
	s1 := unstructured.Unstructured{}
	s1.SetNamespace("a")
	s1.SetName("x")
	s2 := unstructured.Unstructured{}
	s2.SetNamespace("b")
	s2.SetName("y")
	s3 := unstructured.Unstructured{}
	s3.SetNamespace("a")
	s3.SetName("z")
	h.serviceAccounts = []unstructured.Unstructured{s1, s2, s3}
	m := h.exportedSANamespaces()
	if len(m) != 2 {
		t.Fatalf("want 2 namespaces, got %d", len(m))
	}
	_, okA := m["a"]
	_, okB := m["b"]
	if !okA || !okB {
		t.Fatalf("missing keys: %#v", m)
	}
}
