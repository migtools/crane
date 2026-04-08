package export

import (
	"fmt"
	"strings"

	securityv1 "github.com/openshift/api/security/v1"
	"github.com/sirupsen/logrus"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type ClusterScopeHandler struct {
}

type admittedResource struct {
	APIgroup string
	Kind     string
}

var admittedClusterScopeResources = []admittedResource{
	{Kind: "ClusterRoleBinding", APIgroup: "rbac.authorization.k8s.io"},
	{Kind: "ClusterRole", APIgroup: "rbac.authorization.k8s.io"},
	{Kind: "SecurityContextConstraints", APIgroup: "security.openshift.io"},
}

func NewClusterScopeHandler() *ClusterScopeHandler {
	clusterScopeHandler := &ClusterScopeHandler{}

	return clusterScopeHandler
}

func isClusterScopedResource(apiGroup string, kind string) bool {
	for _, admitted := range admittedClusterScopeResources {
		if admitted.Kind == kind && admitted.APIgroup == apiGroup {
			return true
		}
	}
	return false
}

func (c *ClusterScopeHandler) filterRbacResources(resources []*groupResource, log logrus.FieldLogger) []*groupResource {
	log.Debug("Looking for ServiceAccount resources")

	handler := NewClusterScopedRbacHandler(log)
	var filteredResources []*groupResource
	for _, r := range resources {
		kind := r.APIResource.Kind
		if kind == "ServiceAccount" {
			for _, obj := range r.objects.Items {
				log.Debugf("Adding ServiceAccount %s in namespace %s", obj.GetName(), obj.GetNamespace())
				handler.serviceAccounts = append(handler.serviceAccounts, obj)
			}
		}
		if isClusterScopedResource(r.APIGroup, kind) {
			log.Debugf("Adding %d Cluster resource of type %s", len(r.objects.Items), kind)
			handler.clusterResources[kind] = r
		} else {
			filteredResources = append(filteredResources, r)
		}
	}

	if len(handler.clusterResources) == 0 {
		log.Debug("No cluster-scoped resources were collected, nothing to filter")
		return filteredResources
	}

	acceptedCounts := map[string]int{}
	for _, k := range admittedClusterScopeResources {
		filtered, ok := handler.filteredResourcesOfKind(k)
		if ok && len(filtered.objects.Items) > 0 {
			filteredResources = append(filteredResources, filtered)
			acceptedCounts[k.Kind] = len(filtered.objects.Items)
		}
	}

	if len(acceptedCounts) > 0 {
		var parts []string
		for kind, count := range acceptedCounts {
			parts = append(parts, fmt.Sprintf("%d %s", count, kind))
		}
		log.Infof("Cluster-scoped resources exported to _cluster/ directory: %s",
			strings.Join(parts, ", "))
	} else {
		log.Info("No matching cluster-scoped resources found; _cluster/ directory will be empty")
	}

	return filteredResources
}

type ClusterScopedRbacHandler struct {
	log              logrus.FieldLogger
	readyToFilter    bool
	serviceAccounts  []unstructured.Unstructured
	clusterResources map[string]*groupResource

	filteredClusterRoleBindings *groupResource
}

func NewClusterScopedRbacHandler(log logrus.FieldLogger) *ClusterScopedRbacHandler {
	handler := &ClusterScopedRbacHandler{log: log, readyToFilter: false}
	handler.clusterResources = make(map[string]*groupResource)
	return handler
}

// serviceAccountsGroupPrefix is the OpenShift/Kubernetes group that contains all
// ServiceAccounts in a namespace (used in ClusterRoleBinding subjects and SCC groups).
const serviceAccountsGroupPrefix = "system:serviceaccounts:"

// exportedSANamespaces returns distinct namespaces of exported ServiceAccounts.
func (c *ClusterScopedRbacHandler) exportedSANamespaces() map[string]struct{} {
	m := make(map[string]struct{})
	for _, sa := range c.serviceAccounts {
		if ns := sa.GetNamespace(); ns != "" {
			m[ns] = struct{}{}
		}
	}
	return m
}

// groupMatchesExportedSANamespaces is true when groupName is exactly
// system:serviceaccounts:<ns> for some exported SA namespace. Broad groups
// (e.g. system:authenticated) are rejected.
func groupMatchesExportedSANamespaces(groupName string, nsSet map[string]struct{}) bool {
	if len(nsSet) == 0 {
		return false
	}
	if !strings.HasPrefix(groupName, serviceAccountsGroupPrefix) {
		return false
	}
	ns := strings.TrimPrefix(groupName, serviceAccountsGroupPrefix)
	if ns == "" {
		return false
	}
	_, ok := nsSet[ns]
	return ok
}

// parseServiceAccountUserSubject parses user principal strings of the form
// system:serviceaccount:<namespace>:<serviceaccountname>.
func parseServiceAccountUserSubject(userName string) (namespace, saName string, ok bool) {
	parts := strings.Split(userName, ":")
	if len(parts) != 4 || parts[0] != "system" || parts[1] != "serviceaccount" {
		return "", "", false
	}
	if parts[2] == "" || parts[3] == "" {
		return "", "", false
	}
	return parts[2], parts[3], true
}

func (c *ClusterScopedRbacHandler) prepareForFiltering() {
	c.readyToFilter = true
	c.log.Infof("Preparing matching ClusterRoleBindings")

	clusterRoleBindings, ok := c.clusterResources["ClusterRoleBinding"]
	if ok {
		c.filteredClusterRoleBindings = &groupResource{
			APIGroup:        clusterRoleBindings.APIGroup,
			APIVersion:      clusterRoleBindings.APIVersion,
			APIGroupVersion: clusterRoleBindings.APIGroupVersion,
			APIResource:     clusterRoleBindings.APIResource,
		}
		filteredClusterRoleBindings := unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}
		for _, crb := range clusterRoleBindings.objects.Items {
			if c.acceptClusterRoleBinding(crb) {
				filteredClusterRoleBindings.Items = append(filteredClusterRoleBindings.Items, crb)
				c.log.Infof("Found matching %s %s", crb.GetKind(), crb.GetName())
			}
		}
		c.filteredClusterRoleBindings.objects = &filteredClusterRoleBindings
		return
	}

	c.filteredClusterRoleBindings = &groupResource{
		APIGroup:        "NA",
		APIVersion:      "NA",
		APIGroupVersion: "NA",
		APIResource:     metav1.APIResource{},
	}
	c.filteredClusterRoleBindings.objects = &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}
	c.log.Error("Failed to collect cluster-scoped resources; check the failures/ directory for details")
}

func (c *ClusterScopedRbacHandler) filteredResourcesOfKind(resource admittedResource) (*groupResource, bool) {
	if !c.readyToFilter {
		c.prepareForFiltering()
	}

	kind := resource.Kind
	clusterGroupResource, ok := c.clusterResources[kind]
	if ok {
		if kind == "ClusterRoleBinding" {
			return c.filteredClusterRoleBindings, true
		}

		filtered := make([]unstructured.Unstructured, 0)
		initialLen := len(clusterGroupResource.objects.Items)
		c.log.Infof("Filtering for kind %s (%d found)", kind, initialLen)
		for _, r := range clusterGroupResource.objects.Items {
			if c.accept(kind, r) {
				filtered = append(filtered, r)
			}
		}
		clusterGroupResource.objects.Items = filtered
		c.log.Infof("After filtering %d remained out of %d", len(clusterGroupResource.objects.Items), initialLen)
	}
	return clusterGroupResource, ok
}

func (c *ClusterScopedRbacHandler) accept(kind string, clusterResource unstructured.Unstructured) bool {
	c.log.Debugf("Checking acceptance for %s of kind %s", clusterResource.GetName(), kind)
	if clusterResource.GroupVersionKind().Kind == "ClusterRole" {
		return c.acceptClusterRole(clusterResource)
	} else if clusterResource.GroupVersionKind().Kind == "SecurityContextConstraints" {
		return c.acceptSecurityContextConstraints(clusterResource)
	}
	return false
}

func (c *ClusterScopedRbacHandler) acceptClusterRoleBinding(clusterResource unstructured.Unstructured) bool {
	var crb rbacv1.ClusterRoleBinding
	err := runtime.DefaultUnstructuredConverter.
		FromUnstructured(clusterResource.Object, &crb)
	if err != nil {
		c.log.Warnf("Cannot convert to rbacv1.ClusterRoleBinding: %s", err)
		return false
	}
	nsSet := c.exportedSANamespaces()
	if len(nsSet) == 0 {
		return false
	}
	for _, s := range crb.Subjects {
		switch s.Kind {
		case rbacv1.ServiceAccountKind:
			if c.anyServiceAccountInNamespace(s.Namespace, s.Name) {
				c.log.Infof("Accepted %s of kind %s", clusterResource.GetName(), clusterResource.GetKind())
				return true
			}
		case rbacv1.GroupKind:
			if groupMatchesExportedSANamespaces(s.Name, nsSet) {
				c.log.Infof("Accepted %s of kind %s (match via Group %s)",
					clusterResource.GetName(), clusterResource.GetKind(), s.Name)
				return true
			}
		case rbacv1.UserKind:
			ns, saName, ok := parseServiceAccountUserSubject(s.Name)
			if ok && c.anyServiceAccountInNamespace(ns, saName) {
				c.log.Infof("Accepted %s of kind %s (match via User %s)",
					clusterResource.GetName(), clusterResource.GetKind(), s.Name)
				return true
			}
		}
	}
	return false
}

func (c *ClusterScopedRbacHandler) acceptClusterRole(clusterResource unstructured.Unstructured) bool {
	var cr rbacv1.ClusterRole
	err := runtime.DefaultUnstructuredConverter.
		FromUnstructured(clusterResource.Object, &cr)
	if err != nil {
		c.log.Warnf("Cannot convert to rbacv1.ClusterRole: %s", err)
	} else {
		for _, f := range c.filteredClusterRoleBindings.objects.Items {
			var crb rbacv1.ClusterRoleBinding
			err := runtime.DefaultUnstructuredConverter.
				FromUnstructured(f.Object, &crb)
			if err != nil {
				c.log.Warnf("Cannot convert to rbacv1.ClusterRoleBinding: %s", err)
			} else {
				if crb.RoleRef.Kind == "ClusterRole" && crb.RoleRef.Name == cr.Name {
					c.log.Infof("Accepted %s of kind %s", clusterResource.GetName(), clusterResource.GetKind())
					return true
				}
			}
		}
	}
	return false
}

func (c *ClusterScopedRbacHandler) acceptSecurityContextConstraints(clusterResource unstructured.Unstructured) bool {
	var scc securityv1.SecurityContextConstraints
	err := runtime.DefaultUnstructuredConverter.
		FromUnstructured(clusterResource.Object, &scc)
	if err != nil {
		c.log.Warnf("Cannot convert to securityv1.SecurityContextConstraints: %s", err)
		return false
	}

	for _, f := range c.filteredClusterRoleBindings.objects.Items {
		var crb rbacv1.ClusterRoleBinding
		err := runtime.DefaultUnstructuredConverter.
			FromUnstructured(f.Object, &crb)
		if err != nil {
			c.log.Warnf("Cannot convert to rbacv1.ClusterRoleBinding: %s", err)
			continue
		}

		if crb.RoleRef.Kind == "SecurityContextConstraints" && crb.RoleRef.Name == scc.Name {
			c.log.Infof("Accepted %s of kind %s", clusterResource.GetName(), clusterResource.GetKind())
			return true
		} else {
			sccSystemName := fmt.Sprintf("system:openshift:scc:%s", clusterResource.GetName())
			if crb.RoleRef.Kind == "ClusterRole" && crb.RoleRef.Name == sccSystemName {
				c.log.Infof("Accepted %s of kind %s (match via ClusterRoleBinding %s)",
					clusterResource.GetName(), clusterResource.GetKind(), crb.Name)
				return true
			}
		}
	}

	// Last option, look at the users field if it contains one of the exported serviceaccounts
	for _, u := range scc.Users {
		if ns, saName, ok := parseServiceAccountUserSubject(u); ok && c.anyServiceAccountInNamespace(ns, saName) {
			c.log.Infof("Accepted %s of kind %s (match via user %s)",
				clusterResource.GetName(), clusterResource.GetKind(), u)
			return true
		}
	}

	nsSet := c.exportedSANamespaces()
	for _, g := range scc.Groups {
		if groupMatchesExportedSANamespaces(g, nsSet) {
			c.log.Infof("Accepted %s of kind %s (match via group %s)",
				clusterResource.GetName(), clusterResource.GetKind(), g)
			return true
		}
	}

	return false
}

func (c *ClusterScopedRbacHandler) anyServiceAccountInNamespace(namespaceName string, serviceAccountName string) bool {
	c.log.Debugf("Looking for SA %s in %s", serviceAccountName, namespaceName)
	for _, sa := range c.serviceAccounts {
		if sa.GetName() == serviceAccountName && sa.GetNamespace() == namespaceName {
			return true
		}
	}
	return false
}
