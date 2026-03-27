package export

import (
	"bytes"
	"strings"
	"testing"

	securityv1 "github.com/openshift/api/security/v1"
	"github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
)

func TestSecurityContextConstraintsWarning(t *testing.T) {
	// Create a test logger that captures output
	var logOutput bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&logOutput)
	logger.SetLevel(logrus.InfoLevel) // Set to Info to capture both info and warning messages

	// Create a test SecurityContextConstraints
	scc := &securityv1.SecurityContextConstraints{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "security.openshift.io/v1",
			Kind:       "SecurityContextConstraints",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-scc",
		},
		Users: []string{"system:serviceaccount:test-namespace:test-sa"},
	}

	// Convert to unstructured
	sccObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(scc)
	if err != nil {
		t.Fatalf("Failed to convert SCC to unstructured: %v", err)
	}

	sccUnstructured := &unstructured.Unstructured{Object: sccObj}
	sccUnstructured.SetGroupVersionKind(securityv1.GroupVersion.WithKind("SecurityContextConstraints"))

	// Create a test service account
	saObj := &unstructured.Unstructured{}
	saObj.SetAPIVersion("v1")
	saObj.SetKind("ServiceAccount")
	saObj.SetName("test-sa")
	saObj.SetNamespace("test-namespace")

	// Create handler with test logger
	handler := NewClusterScopedRbacHandler(logger)
	handler.serviceAccounts = []unstructured.Unstructured{*saObj}
	handler.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}},
	}

	// Test the acceptance function
	result := handler.acceptSecurityContextConstraints(*sccUnstructured)

	// Check that the function returns true (SCC is accepted)
	if !result {
		t.Errorf("Expected acceptSecurityContextConstraints to return true, got false")
	}

	// Check that warning message is logged
	logContents := logOutput.String()
	t.Logf("Log output: %s", logContents) // Show the actual log output for verification
	
	if !strings.Contains(logContents, "WARNING: SecurityContextConstraints 'test-scc' requires elevated privileges") {
		t.Errorf("Expected warning message about SecurityContextConstraints privileges, got: %s", logContents)
	}

	// Check that the warning mentions destination cluster and OpenShift
	if !strings.Contains(logContents, "destination cluster") {
		t.Errorf("Expected warning to mention 'destination cluster', got: %s", logContents)
	}

	if !strings.Contains(logContents, "OpenShift") {
		t.Errorf("Expected warning to mention 'OpenShift', got: %s", logContents)
	}
}

func TestSecurityContextConstraintsNoWarningWhenNotAccepted(t *testing.T) {
	// Create a test logger that captures output
	var logOutput bytes.Buffer
	logger := logrus.New()
	logger.SetOutput(&logOutput)
	logger.SetLevel(logrus.WarnLevel)

	// Create a test SecurityContextConstraints that won't be accepted
	scc := &securityv1.SecurityContextConstraints{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "security.openshift.io/v1",
			Kind:       "SecurityContextConstraints",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-scc",
		},
		Users: []string{"system:serviceaccount:other-namespace:other-sa"},
	}

	// Convert to unstructured
	sccObj, err := runtime.DefaultUnstructuredConverter.ToUnstructured(scc)
	if err != nil {
		t.Fatalf("Failed to convert SCC to unstructured: %v", err)
	}

	sccUnstructured := &unstructured.Unstructured{Object: sccObj}
	sccUnstructured.SetGroupVersionKind(securityv1.GroupVersion.WithKind("SecurityContextConstraints"))

	// Create a test service account with different name/namespace
	saObj := &unstructured.Unstructured{}
	saObj.SetAPIVersion("v1")
	saObj.SetKind("ServiceAccount")
	saObj.SetName("test-sa")
	saObj.SetNamespace("test-namespace")

	// Create handler with test logger
	handler := NewClusterScopedRbacHandler(logger)
	handler.serviceAccounts = []unstructured.Unstructured{*saObj}
	handler.filteredClusterRoleBindings = &groupResource{
		objects: &unstructured.UnstructuredList{Items: []unstructured.Unstructured{}},
	}

	// Test the acceptance function
	result := handler.acceptSecurityContextConstraints(*sccUnstructured)

	// Check that the function returns false (SCC is not accepted)
	if result {
		t.Errorf("Expected acceptSecurityContextConstraints to return false, got true")
	}

	// Check that no warning message is logged since SCC was not accepted
	logContents := logOutput.String()
	if strings.Contains(logContents, "WARNING: SecurityContextConstraints") {
		t.Errorf("Expected no warning message when SCC is not accepted, but got: %s", logContents)
	}
}