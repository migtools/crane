package export

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sirupsen/logrus"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/pager"
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

func writeResources(resources []*groupResource, resourceDir string, log logrus.FieldLogger) []error {
	errs := []error{}
	for _, r := range resources {
		log.Infof("Writing objects of resource: %s to the output directory\n", r.APIResource.Name)

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

			objBytes, err := yaml.Marshal(obj.Object)
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

func writeErrors(errors []*groupResourceError, failuresDir string, log logrus.FieldLogger) []error {
	errs := []error{}
	for _, r := range errors {
		log.Debugf("Writing error for resource %s, error: %#v\n", r.APIResource.Name, r.Error)

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

func resourceToExtract(namespace string, dynamicClient dynamic.Interface, lists []*metav1.APIResourceList, log logrus.FieldLogger, iterateByGet bool) ([]*groupResource, []*groupResourceError) {
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
				log.Debugf("skipping extracting events\n")
				continue
			}

			if !resource.Namespaced {
				log.Debugf("resource: %s.%s is clusterscoped, skipping\n", gv.String(), resource.Kind)
				continue
			}

			log.Debugf("processing resource: %s.%s\n", gv.String(), resource.Kind)

			g := &groupResource{
				APIGroup:        gv.Group,
				APIVersion:      gv.Version,
				APIGroupVersion: gv.String(),
				APIResource:     resource,
			}

			objs, err := getObjects(g, namespace, dynamicClient, log, iterateByGet)
			if err != nil {
				switch {
				case apierrors.IsForbidden(err):
					log.Errorf("cannot list obj in namespace for groupVersion %s, kind: %s\n", g.APIGroupVersion, g.APIResource.Kind)
				case apierrors.IsMethodNotSupported(err):
					log.Errorf("list method not supported on the groupVersion %s, kind: %s\n", g.APIGroupVersion, g.APIResource.Kind)
				case apierrors.IsNotFound(err):
					log.Errorf("could not find the resource, most likely this is a virtual resource, groupVersion %s, kind: %s\n", g.APIGroupVersion, g.APIResource.Kind)
				default:
					log.Errorf("error listing objects: %#v, groupVersion %s, kind: %s\n", err, g.APIGroupVersion, g.APIResource.Kind)
				}
				errors = append(errors, &groupResourceError{resource, err})
				continue
			}

			if len(objs.Items) > 0 {
				g.objects = objs
				log.Infof("adding resource: %s to the list of GVRs to be extracted", resource.Name)
				resources = append(resources, g)
				continue
			}

			log.Debugf("0 objects found, for resource %s, skipping\n", resource.Name)
		}
	}

	return resources, errors
}

func getObjects(g *groupResource, namespace string, d dynamic.Interface, logger logrus.FieldLogger, iterateByGet bool) (*unstructured.UnstructuredList, error) {
	c := d.Resource(schema.GroupVersionResource{
		Group:    g.APIGroup,
		Version:  g.APIVersion,
		Resource: g.APIResource.Name,
	})
	if !g.APIResource.Namespaced {
		return &unstructured.UnstructuredList{}, nil
	}

	p := pager.New(func(ctx context.Context, opts metav1.ListOptions) (runtime.Object, error) {
		return c.Namespace(namespace).List(context.Background(), opts)
	})

	list, _, err := p.List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	if iterateByGet && (g.APIResource.Name == "imagestreamtags" || g.APIResource.Name == "imagetags") {
		logger.Infof("iterating through %v by get, this could take some time", g.APIResource.Name)
		unstructuredList, err := iterateItemsByGet(c, g, list, namespace, logger)
		if err != nil {
			return nil, err
		}
		return unstructuredList, nil
	}
	return iterateItemsInList(list, g, logger)
}

func iterateItemsByGet(c dynamic.NamespaceableResourceInterface, g *groupResource, list runtime.Object, namespace string, logger logrus.FieldLogger) (*unstructured.UnstructuredList, error) {
	unstructuredList := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}
	err := meta.EachListItem(list, func(object runtime.Object) error {
		u, ok := object.(*unstructured.Unstructured)
		if !ok {
			// TODO: explore aggregating all the errors here instead of terminating the loop
			logger.Errorf("expected unstructured.Unstructured but got %T for groupResource %s and object: %#v\n", g, object)
			return fmt.Errorf("expected *unstructured.Unstructured but got %T", u)
		}
		obj, err := c.Namespace(namespace).Get(context.TODO(), u.GetName(), metav1.GetOptions{})
		if err != nil {
			return err
		}
		unstructuredList.Items = append(unstructuredList.Items, *obj)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("unable to process the list for group: %s, kind: %s", g.APIGroup, g.APIResource.Kind)
	}
	return unstructuredList, nil
}

func iterateItemsInList(list runtime.Object, g *groupResource, logger logrus.FieldLogger) (*unstructured.UnstructuredList, error) {
	unstructuredList := &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}}
	err := meta.EachListItem(list, func(object runtime.Object) error {
		u, ok := object.(*unstructured.Unstructured)
		if !ok {
			// TODO: explore aggregating all the errors here instead of terminating the loop
			logger.Errorf("expected unstructured.Unstructured but got %T for groupResource %s and object: %#v\n", g, object)
			return fmt.Errorf("expected *unstructured.Unstructured but got %T", u)
		}
		unstructuredList.Items = append(unstructuredList.Items, *u)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("unable to process the list for group: %s, kind: %s", g.APIGroup, g.APIResource.Kind)
	}
	return unstructuredList, nil
}
