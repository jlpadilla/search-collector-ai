package transformer

import (
	"testing"
	"time"

	"github.com/jlpadilla/search-collector-ai/pkg/informer"
	"k8s.io/api/core/v1"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestTransformerUIDExtraction(t *testing.T) {
	// Create a test transformer config
	config := &TransformConfig{
		ExtractFields:         []string{"metadata.uid", "metadata.name", "metadata.namespace"},
		IncludeLabels:         true,
		IncludeAnnotations:    false,
		DiscoverRelationships: false,
		FieldMapping: FieldMappingConfig{
			Type: "none", // No field mapping, preserve raw values
		},
	}

	transformer := NewBaseTransformer(config)

	// Create a test pod
	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod",
			Namespace: "default",
			UID:       types.UID("test-uid-12345"),
			Labels: map[string]string{
				"app": "test",
			},
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "test-container",
					Image: "nginx:latest",
				},
			},
		},
	}

	// Create a resource event
	event := &informer.ResourceEvent{
		Type:         informer.EventTypeAdded,
		Object:       pod,
		ObjectMeta:   pod.ObjectMeta,
		ResourceKey:  "default/test-pod",
		ResourceType: "pods",
		APIVersion:   "v1",
	}

	// Transform the resource
	transformed, err := transformer.Transform(event)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Verify UID is extracted
	if transformed.Fields == nil {
		t.Fatal("Fields map is nil")
	}

	uid, exists := transformed.Fields["metadata.uid"]
	if !exists {
		t.Fatal("metadata.uid not found in fields")
	}

	uidValue, ok := uid.(types.UID)
	if !ok {
		t.Fatalf("UID is not types.UID: %T", uid)
	}

	if string(uidValue) != "test-uid-12345" {
		t.Fatalf("Expected UID 'test-uid-12345', got '%s'", string(uidValue))
	}

	// Verify other fields
	name, exists := transformed.Fields["metadata.name"]
	if !exists {
		t.Fatal("metadata.name not found in fields")
	}
	if name != "test-pod" {
		t.Fatalf("Expected name 'test-pod', got '%s'", name)
	}

	namespace, exists := transformed.Fields["metadata.namespace"]
	if !exists {
		t.Fatal("metadata.namespace not found in fields")
	}
	if namespace != "default" {
		t.Fatalf("Expected namespace 'default', got '%s'", namespace)
	}

	t.Logf("UID extraction test passed: UID=%s", string(uidValue))
}

func TestTransformerWithMissingUID(t *testing.T) {
	// Create a test transformer config
	config := &TransformConfig{
		ExtractFields:         []string{"metadata.uid", "metadata.name"},
		IncludeLabels:         false,
		IncludeAnnotations:    false,
		DiscoverRelationships: false,
		FieldMapping: FieldMappingConfig{
			Type: "none", // No field mapping, preserve raw values
		},
	}

	transformer := NewBaseTransformer(config)

	// Create a test pod with empty UID
	pod := &v1.Pod{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Pod",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-pod-no-uid",
			Namespace: "default",
			// No UID field set
		},
	}

	// Create a resource event
	event := &informer.ResourceEvent{
		Type:         informer.EventTypeAdded,
		Object:       pod,
		ObjectMeta:   pod.ObjectMeta,
		ResourceKey:  "default/test-pod-no-uid",
		ResourceType: "pods",
		APIVersion:   "v1",
	}

	// Transform the resource
	transformed, err := transformer.Transform(event)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Verify that UID field still exists but is empty or has a warning
	if transformed.Fields == nil {
		t.Fatal("Fields map is nil")
	}

	// The transformer should warn about missing UID but not fail
	t.Logf("Transform completed for resource without UID: %+v", transformed.Fields)
}

func TestTransformerAPIVersionSplitting(t *testing.T) {
	// Create a test transformer config with apiVersion splitting
	config := &TransformConfig{
		ExtractFields:         []string{"TypeMeta.APIVersion", "metadata.name", "metadata.namespace", "TypeMeta.Kind"},
		IncludeLabels:         false,
		IncludeAnnotations:    false,
		DiscoverRelationships: false,
		FieldMapping: FieldMappingConfig{
			Type: "metadata-prefix-removal", // Use metadata prefix mapper for apiVersion splitting
		},
	}

	transformer := NewBaseTransformer(config)

	// Create a test deployment with grouped API version
	deployment := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "default",
		},
	}

	// Create a resource event
	event := &informer.ResourceEvent{
		Type:         informer.EventTypeAdded,
		Object:       deployment,
		ObjectMeta:   deployment.ObjectMeta,
		ResourceKey:  "default/test-deployment",
		ResourceType: "deployments",
		APIVersion:   "apps/v1",
	}

	// Transform the resource
	transformed, err := transformer.Transform(event)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Verify apiVersion is split correctly
	if transformed.Fields == nil {
		t.Fatal("Fields map is nil")
	}

	// Check for apigroup
	apiGroup, exists := transformed.Fields["apigroup"]
	if !exists {
		t.Fatal("apigroup not found in fields")
	}
	if apiGroup != "apps" {
		t.Fatalf("Expected apigroup 'apps', got '%s'", apiGroup)
	}

	// Check for apiversion
	apiVersion, exists := transformed.Fields["apiversion"]
	if !exists {
		t.Fatal("apiversion not found in fields")
	}
	if apiVersion != "v1" {
		t.Fatalf("Expected apiversion 'v1', got '%s'", apiVersion)
	}

	// Check that Kind is properly mapped
	kind, exists := transformed.Fields["kind"]
	if !exists {
		t.Fatal("kind not found in fields")
	}
	if kind != "Deployment" {
		t.Fatalf("Expected kind 'Deployment', got '%s'", kind)
	}

	// Verify original apiVersion field is not present (replaced by split fields)
	if _, exists := transformed.Fields["apiVersion"]; exists {
		t.Error("Original apiVersion field should not be present after splitting")
	}

	t.Logf("API version splitting test passed: apigroup=%s, apiversion=%s, kind=%s", apiGroup, apiVersion, kind)
}