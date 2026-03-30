package export

import (
	"io"
	"testing"

	securityv1 "github.com/openshift/api/security/v1"
	"github.com/sirupsen/logrus"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
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

func securityContextConstraintsToUnstructured(t *testing.T, scc *securityv1.SecurityContextConstraints) unstructured.Unstructured {
	t.Helper()
	u, err := runtime.DefaultUnstructuredConverter.ToUnstructured(scc)
	if err != nil {
		t.Fatalf("ToUnstructured: %v", err)
	}
	return unstructured.Unstructured{Object: u}
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
