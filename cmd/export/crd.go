package export

import (
	"context"
	"fmt"
	"strings"

	"github.com/konveyor/crane-lib/apigroups"
	"github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

func normalizeGroupSet(groups []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, g := range groups {
		g = strings.TrimSpace(g)
		if g == "" {
			continue
		}
		out[g] = struct{}{}
	}
	return out
}

func shouldSkipCRDGroup(group string, includeSet, skipSet map[string]struct{}) bool {
	if _, ok := includeSet[group]; ok {
		return false
	}
	if apigroups.IsDefaultBuiltinAPIGroup(group) {
		return true
	}
	if _, ok := skipSet[group]; ok {
		return true
	}
	return false
}

var crdGVR = schema.GroupVersionResource{
	Group:    "apiextensions.k8s.io",
	Version:  "v1",
	Resource: "customresourcedefinitions",
}

// crdFailureAPIResourceName returns a unique writeErrors filename stem for a failed CRD GET
// (writeErrors uses APIResource.Name + ".yaml"; one file per CRD name).
func crdFailureAPIResourceName(crdName string) string {
	return "customresourcedefinition-" + strings.ReplaceAll(crdName, "/", "-")
}

// getOperatorManager checks if a CRD is managed by an operator.
// Returns the manager name if detected, empty string otherwise.
func getOperatorManager(obj *unstructured.Unstructured) string {
	labels := obj.GetLabels()
	annotations := obj.GetAnnotations()

	if v, ok := labels["olm.managed"]; ok && v == "true" {
		if name := labels["operators.coreos.com/managed-by"]; name != "" {
			return "operator " + name + " (OLM)"
		}
		return "OLM"
	}

	if v, ok := labels["app.kubernetes.io/managed-by"]; ok && v != "" {
		return v
	}

	for k := range annotations {
		if strings.HasPrefix(k, "operators.operatorframework.io/") {
			return "operator-framework"
		}
	}

	if refs := obj.GetOwnerReferences(); len(refs) > 0 {
		return refs[0].Kind + "/" + refs[0].Name
	}

	return ""
}

// collectRelatedCRDs returns synthetic groupResource rows for CRDs backing custom
// API types that appear in resources (deduplicated by plural.group). Built-in API
// groups are skipped. Failed GETs are returned as groupResourceError entries for
// the same failures directory as list errors.
func collectRelatedCRDs(resources []*groupResource, dynamicClient dynamic.Interface, log logrus.FieldLogger, userSkipGroups, userIncludeGroups []string) ([]*groupResource, []*groupResourceError) {
	skipSet := normalizeGroupSet(userSkipGroups)
	includeSet := normalizeGroupSet(userIncludeGroups)

	seen := make(map[string]struct{})
	for _, g := range resources {
		if g == nil || g.objects == nil || len(g.objects.Items) == 0 {
			continue
		}
		if shouldSkipCRDGroup(g.APIGroup, includeSet, skipSet) {
			continue
		}
		if strings.Contains(g.APIResource.Name, "/") {
			continue
		}
		name := fmt.Sprintf("%s.%s", g.APIResource.Name, g.APIGroup)
		seen[name] = struct{}{}
	}

	if len(seen) == 0 {
		return nil, nil
	}

	crdClient := dynamicClient.Resource(crdGVR)
	out := make([]*groupResource, 0, len(seen))
	var outErrs []*groupResourceError
	for crdName := range seen {
		obj, err := crdClient.Get(context.Background(), crdName, metav1.GetOptions{})
		if err != nil {
			switch {
			case apierrors.IsForbidden(err):
				log.Warnf("cannot get CustomResourceDefinition %q (forbidden); ensure get on customresourcedefinitions.apiextensions.k8s.io", crdName)
			case apierrors.IsNotFound(err):
				log.Debugf("CustomResourceDefinition %q not found (plural may not match CRD spec.names.plural)", crdName)
			default:
				log.Warnf("error getting CustomResourceDefinition %q: %v", crdName, err)
			}
			outErrs = append(outErrs, &groupResourceError{
				APIResource: metav1.APIResource{
					Name:         crdFailureAPIResourceName(crdName),
					SingularName: "customresourcedefinition",
					Namespaced:   false,
					Kind:         "CustomResourceDefinition",
					Verbs:        metav1.Verbs{"get", "list"},
				},
				Error: err,
			})
			continue
		}
		
		if manager := getOperatorManager(obj); manager != "" {
			log.Warnf("Skipping CRD %q — managed by %s; install the operator on the target cluster instead", crdName, manager)
			continue
		}

		log.Infof("exported CustomResourceDefinition %q for referenced custom resources", crdName)
		out = append(out, &groupResource{
			APIGroup:        crdGVR.Group,
			APIVersion:      crdGVR.Version,
			APIGroupVersion: schema.GroupVersion{Group: crdGVR.Group, Version: crdGVR.Version}.String(),
			APIResource: metav1.APIResource{
				Name:         "customresourcedefinitions",
				SingularName: "customresourcedefinition",
				Namespaced:   false,
				Kind:         "CustomResourceDefinition",
				Verbs:        metav1.Verbs{"get", "list"},
			},
			objects: &unstructured.UnstructuredList{
				Items: []unstructured.Unstructured{*obj},
			},
		})
	}
	return out, outErrs
}
