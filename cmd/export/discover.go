package export

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/yaml"
)

// groupResource contains the APIGroup and APIResource
type groupResource struct {
	APIGroup        string
	APIVersion      string
	APIGroupVersion string
	APIResource     metav1.APIResource
	objects         *unstructured.UnstructuredList
}

type groupResourceError struct {
	APIResource metav1.APIResource `json:",inline"`
	Error       error              `json:"error"`
}

func writeResources(resources []*groupResource, resourceDir string) []error {
	errs := []error{}
	for _, r := range resources {
		fmt.Printf("%s %s\n", r.APIResource.Name, r.APIGroupVersion)

		kind := r.APIResource.Kind

		if kind == "" {
			continue
		}

		for _, obj := range r.objects.Items {
			path := filepath.Join(resourceDir, getFilePath(obj))
			f, err := os.Create(path)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			objBytes, err := yaml.Marshal(f)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			_, err = f.Write(objBytes)
			if err != nil {
				errs = append(errs, err)
				continue
			}

			err = f.Close()
			if err != nil {
				errs = append(errs, err)
				continue
			}

		}
	}

	return errs
}

func writeErrors(errors []*groupResourceError, failuresDir string) []error {
	errs := []error{}
	for _, r := range errors {
		fmt.Printf("%s\n", r.APIResource.Name)

		kind := r.APIResource.Kind

		if kind == "" {
			continue
		}

		path := filepath.Join(failuresDir, r.APIResource.Name+".yaml")
		f, err := os.Create(path)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		errBytes, err := yaml.Marshal(&r)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		_, err = f.Write(errBytes)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		err = f.Close()
		if err != nil {
			errs = append(errs, err)
			continue
		}
	}

	return errs
}

func getFilePath(obj unstructured.Unstructured) string {
	namespace := obj.GetNamespace()
	if namespace == "" {
		namespace = "clusterscoped"
	}
	return strings.Join([]string{obj.GetKind(), namespace, obj.GetName()}, "_") + ".yaml"
}

func resourceToExtract(namespace string, dynamicClient dynamic.Interface, lists []*metav1.APIResourceList) ([]*groupResource, []*groupResourceError) {
	resources := []*groupResource{}
	errors := []*groupResourceError{}

	for _, list := range lists {
		if len(list.APIResources) == 0 {
			continue
		}
		gv, err := schema.ParseGroupVersion(list.GroupVersion)
		if err != nil {
			continue
		}
		for _, resource := range list.APIResources {
			if len(resource.Verbs) == 0 {
				continue
			}

			// TODO: alpatel: put this behing a flag
			if resource.Kind == "Event" {
				fmt.Printf("resource: %s.%s, skipping\n", gv.String(), resource.Kind)
				continue
			}

			if !resource.Namespaced {
				fmt.Printf("resource: %s.%s is clusterscoped, skipping\n", gv.String(), resource.Kind)
				continue
			}

			fmt.Printf("processing resource: %s.%s\n", gv.String(), resource.Kind)

			g := &groupResource{
				APIGroup:        gv.Group,
				APIVersion:      gv.Version,
				APIGroupVersion: gv.String(),
				APIResource:     resource,
			}

			objs, err := getObjects(g, namespace, dynamicClient)
			if err != nil {
				switch {
				case apierrors.IsForbidden(err):
					fmt.Printf("cannot list obj in namespace\n")
				case apierrors.IsMethodNotSupported(err):
					fmt.Printf("list method not supported on the gvr\n")
				case apierrors.IsNotFound(err):
					fmt.Printf("could not find the resource, most likely this is a virtual resource\n")
				default:
					fmt.Printf("error listing objects: %#v\n", err)
				}
				errors = append(errors, &groupResourceError{
					APIResource: resource,
					Error:       err,
				})
				continue
			}

			if len(objs.Items) > 0 {
				g.objects = objs
				fmt.Printf("more than one object found\n")
				resources = append(resources, g)
				continue
			}

			fmt.Printf("0 objects found, skipping\n")
		}
	}

	return resources, errors
}

func getObjects(g *groupResource, namespace string, d dynamic.Interface) (*unstructured.UnstructuredList, error) {
	c := d.Resource(schema.GroupVersionResource{
		Group:    g.APIGroup,
		Version:  g.APIVersion,
		Resource: g.APIResource.Name,
	})
	if g.APIResource.Namespaced {
		return c.Namespace(namespace).List(context.Background(), metav1.ListOptions{})
	}
	return &unstructured.UnstructuredList{}, nil
}
