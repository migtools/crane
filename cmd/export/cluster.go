package export

import (
	"fmt"
	"strings"

	authv1 "github.com/openshift/api/authorization/v1"
	securityv1 "github.com/openshift/api/security/v1"
	"github.com/sirupsen/logrus"
	"golang.org/x/exp/slices"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

type ClusterScopeHandler struct {
}

var admittedClusterScopeKinds = []string{"ClusterRole", "ClusterRoleBinding", "SecurityContextConstraints"}

func NewClusterScopeHandler() *ClusterScopeHandler {
	clusterScopeHandler := &ClusterScopeHandler{}

	return clusterScopeHandler
}

func (c *ClusterScopeHandler) filterRbacResources(resources []*groupResource, log logrus.FieldLogger) []*groupResource {
	log.Debug("Looking for ServiceAccount resources")

	handler := NewClusterScopedRbacHandler(log)
	for i := len(resources) - 1; i >= 0; i-- {
		r := resources[i]
		kind := r.APIResource.Kind
		if kind == "ServiceAccount" {
			for _, obj := range r.objects.Items {
				log.Debugf("Adding ServiceAccount %s in namespace %s", obj.GetName(), obj.GetNamespace())
				handler.serviceAccounts = append(handler.serviceAccounts, obj)
			}
		} else if slices.Contains(admittedClusterScopeKinds, kind) {
			log.Debugf("Adding %d Cluster resource of type %s", len(r.objects.Items), kind)
			handler.clusterResources[kind] = r
			resources = append(resources[:i], resources[i+1:]...)
		}
	}

	for _, k := range admittedClusterScopeKinds {
		filtered, ok := handler.filteredResourcesOfKind(k)
		if ok && len(filtered.objects.Items) > 0 {
			resources = append(resources, filtered)
		}
	}

	return resources
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
	}
}

func (c *ClusterScopedRbacHandler) filteredResourcesOfKind(kind string) (*groupResource, bool) {
	if !c.readyToFilter {
		c.prepareForFiltering()
	}

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
	var crb authv1.ClusterRoleBinding
	err := runtime.DefaultUnstructuredConverter.
		FromUnstructured(clusterResource.Object, &crb)
	if err != nil {
		c.log.Warnf("Cannot convert to authv1.ClusterRoleBinding: %s", err)
	} else {
		for _, s := range crb.Subjects {
			if s.Kind == "ServiceAccount" && c.anyServiceAccountInNamespace(s.Namespace, s.Name) {
				c.log.Infof("Accepted %s of kind %s", clusterResource.GetName(), clusterResource.GetKind())
				return true
			}
		}
	}
	return false
}

func (c *ClusterScopedRbacHandler) acceptClusterRole(clusterResource unstructured.Unstructured) bool {
	var cr authv1.ClusterRole
	err := runtime.DefaultUnstructuredConverter.
		FromUnstructured(clusterResource.Object, &cr)
	if err != nil {
		c.log.Warnf("Cannot convert to authv1.ClusterRole: %s", err)
	} else {
		for _, f := range c.filteredClusterRoleBindings.objects.Items {
			var crb authv1.ClusterRoleBinding
			err := runtime.DefaultUnstructuredConverter.
				FromUnstructured(f.Object, &crb)
			if err != nil {
				c.log.Warnf("Cannot convert to authv1.ClusterRoleBinding: %s", err)
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
	} else {
		for _, f := range c.filteredClusterRoleBindings.objects.Items {
			var crb authv1.ClusterRoleBinding
			err := runtime.DefaultUnstructuredConverter.
				FromUnstructured(f.Object, &crb)
			if err != nil {
				c.log.Warnf("Cannot convert to authv1.ClusterRoleBinding: %s", err)
			} else {
				if crb.RoleRef.Kind == "SecurityContextConstraints" && crb.RoleRef.Name == scc.Name {
					c.log.Infof("Accepted %s of kind %s", clusterResource.GetName(), clusterResource.GetKind())
					return true
				} else {
					sccSystemName := fmt.Sprintf("system:openshift:scc:%s", clusterResource.GetName())
					if crb.RoleRef.Kind == "ClusterRole" && crb.RoleRef.Name == sccSystemName {
						c.log.Infof("Accepted %s of kind %s (match wia ClusterRoleBinding %s)",
							clusterResource.GetName(), clusterResource.GetKind(), crb.Name)
						return true
					}
				}
			}
		}

		// Last option, look at the users field if it contains one of the exported serviceaccounts
		for _, u := range scc.Users {
			if strings.Contains(u, "system:serviceaccount:") && len(strings.Split(u, ":")) == 4 {
				namespaceName := strings.Split(u, ":")[2]
				serviceAccountName := strings.Split(u, ":")[3]
				if c.anyServiceAccountInNamespace(namespaceName, serviceAccountName) {
					c.log.Infof("Accepted %s of kind %s (match wia user %s)",
						clusterResource.GetName(), clusterResource.GetKind(), u)
					return true
				}
			}
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
