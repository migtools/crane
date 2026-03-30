package export

import (
	"context"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// builtinK8sAPIGroups lists in-tree Kubernetes API groups that are not backed by
// user-defined CRDs. Extend when new first-party groups appear.
var builtinK8sAPIGroups = map[string]struct{}{
	"": {},
	"apps": {},
	"batch": {},
	"autoscaling": {},
	"policy": {},
	"networking.k8s.io": {},
	"rbac.authorization.k8s.io": {},
	"storage.k8s.io": {},
	"admissionregistration.k8s.io": {},
	"certificates.k8s.io": {},
	"coordination.k8s.io": {},
	"discovery.k8s.io": {},
	"events.k8s.io": {},
	"flowcontrol.apiserver.k8s.io": {},
	"node.k8s.io": {},
	"scheduling.k8s.io": {},
	"apiextensions.k8s.io": {},
	"apiregistration.k8s.io": {},
	"resource.k8s.io": {},
	"authentication.k8s.io": {},
	"authorization.k8s.io": {},
	"extensions": {},
	"metrics.k8s.io": {},
	"imagepolicy.k8s.io": {},
	"internal.apiserver.k8s.io": {},
	// OpenShift / OLM adjacent (not *.openshift.io suffix)
	"operators.coreos.com":            {},
	"packages.operators.coreos.com":   {},
	"monitoring.coreos.com":           {},
}

func isBuiltinAPIGroup(group string) bool {
	if _, ok := builtinK8sAPIGroups[group]; ok {
		return true
	}
	if strings.HasSuffix(group, ".openshift.io") {
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

// collectRelatedCRDs returns synthetic groupResource rows for CRDs backing custom
// API types that appear in resources (deduplicated by plural.group). Built-in API
// groups are skipped. Failed GETs are returned as groupResourceError entries for
// the same failures directory as list errors.
func collectRelatedCRDs(resources []*groupResource, dynamicClient dynamic.Interface, log logrus.FieldLogger) ([]*groupResource, []*groupResourceError) {
	seen := make(map[string]struct{})
	for _, g := range resources {
		if g == nil || g.objects == nil || len(g.objects.Items) == 0 {
			continue
		}
		if isBuiltinAPIGroup(g.APIGroup) {
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
