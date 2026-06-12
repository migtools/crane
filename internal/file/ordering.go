package file

import (
	"fmt"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// ResourceOrder defines the application order for Kubernetes resources.
// Lower numbers are applied first to ensure dependencies exist before dependents.
// This ordering follows kubectl's resource ordering for apply operations.
var ResourceOrder = map[string]int{
	// Cluster-wide resources
	"Namespace":                   10,
	"CustomResourceDefinition":    20,
	"StorageClass":                30,
	"PersistentVolume":            40,
	"ClusterRole":                 50,
	"ClusterRoleBinding":          60,
	"PriorityClass":               70,
	"IngressClass":                80,
	"RuntimeClass":                90,
	"VolumeSnapshotClass":         100,
	"CSIDriver":                   110,
	"CSINode":                     120,
	"ValidatingWebhookConfiguration": 130,
	"MutatingWebhookConfiguration":   140,

	// Namespace-scoped configuration resources
	"ResourceQuota":      200,
	"LimitRange":         210,
	"ServiceAccount":     220,
	"Secret":             230,
	"ConfigMap":          240,
	"PersistentVolumeClaim": 250,

	// RBAC resources (must come after ServiceAccount)
	"Role":        300,
	"RoleBinding": 310,

	// Workload resources
	"Pod":                320,
	"ReplicaSet":         330,
	"Deployment":         340,
	"StatefulSet":        350,
	"DaemonSet":          360,
	"Job":                370,
	"CronJob":            380,
	"ReplicationController": 390,

	// Service and networking
	"Service":            400,
	"Endpoints":          410,
	"EndpointSlice":      420,
	"Ingress":            430,
	"NetworkPolicy":      440,
	"PodDisruptionBudget": 450,

	// Autoscaling
	"HorizontalPodAutoscaler": 500,
	"VerticalPodAutoscaler":   510,

	// OpenShift-specific resources
	"Route":              600,
	"BuildConfig":        610,
	"Build":              620,
	"DeploymentConfig":   630,
	"ImageStream":        640,
	"ImageStreamTag":     650,
	"Template":           660,
	"SecurityContextConstraints": 670,

	// Default for unknown types
	"_default": 1000,
}

// GetResourceOrder returns the application order for a given resource kind.
// Resources with lower order values should be applied before those with higher values.
func GetResourceOrder(kind string) int {
	if order, exists := ResourceOrder[kind]; exists {
		return order
	}
	return ResourceOrder["_default"]
}

// GetOrderedResourceFilename returns a filename with an order prefix for dependency-aware application.
// Format: NNN_Kind_group_version_namespace_name.yaml
// where NNN is a 3-digit order number (e.g., "050_Role_rbac.authorization.k8s.io_v1_default_pod-reader.yaml")
func GetOrderedResourceFilename(obj unstructured.Unstructured) string {
	order := GetResourceOrder(obj.GetKind())
	baseFilename := GetResourceFilename(obj)
	return fmt.Sprintf("%03d_%s", order, baseFilename)
}
