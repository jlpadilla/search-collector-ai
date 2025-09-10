package transformer

import (
	"testing"
	"time"

	"github.com/jlpadilla/search-collector-ai/pkg/informer"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestTransformerBackwardCompatibility(t *testing.T) {
	// Test default configuration (no field mapping)
	config := &TransformConfig{
		ExtractFields:         []string{"metadata.uid", "metadata.name", "spec.replicas"},
		IncludeLabels:         false,
		IncludeAnnotations:    false,
		DiscoverRelationships: false,
		FieldMapping: FieldMappingConfig{
			Type: "none", // Explicitly no mapping
		},
	}

	transformer := NewBaseTransformer(config)
	
	// Create test pod
	pod := createTestPod()
	event := createTestEvent(pod)

	// Transform the resource
	transformed, err := transformer.Transform(event)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Verify original field names are preserved (backward compatibility)
	expectedFields := map[string]interface{}{
		"metadata.uid":  types.UID("test-uid-12345"),
		"metadata.name": "test-pod",
		"spec.replicas": nil, // Pod doesn't have replicas
	}

	for fieldName, expectedValue := range expectedFields {
		actualValue := transformed.Fields[fieldName]
		if actualValue != expectedValue {
			t.Errorf("Field %s: expected %v, got %v", fieldName, expectedValue, actualValue)
		}
	}
}

func TestTransformerWithMetadataPrefixRemoval(t *testing.T) {
	// Test metadata prefix removal
	config := &TransformConfig{
		ExtractFields:         []string{"metadata.uid", "metadata.name", "metadata.namespace", "spec.containers"},
		IncludeLabels:         false,
		IncludeAnnotations:    false,
		DiscoverRelationships: false,
		FieldMapping: FieldMappingConfig{
			Type: "metadata-prefix-removal",
		},
	}

	transformer := NewBaseTransformer(config)
	
	pod := createTestPod()
	event := createTestEvent(pod)

	transformed, err := transformer.Transform(event)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Verify metadata fields are renamed
	if _, exists := transformed.Fields["uid"]; !exists {
		t.Error("Expected 'uid' field not found")
	}
	if _, exists := transformed.Fields["name"]; !exists {
		t.Error("Expected 'name' field not found")
	}
	if _, exists := transformed.Fields["namespace"]; !exists {
		t.Error("Expected 'namespace' field not found")
	}

	// Verify non-metadata fields are unchanged
	if _, exists := transformed.Fields["spec.containers"]; !exists {
		t.Error("Expected 'spec.containers' field not found")
	}

	// Verify old metadata fields are NOT present
	if _, exists := transformed.Fields["metadata.uid"]; exists {
		t.Error("Old 'metadata.uid' field should not exist")
	}
	if _, exists := transformed.Fields["metadata.name"]; exists {
		t.Error("Old 'metadata.name' field should not exist")
	}
}

func TestTransformerWithCustomMapping(t *testing.T) {
	// Test custom field mapping
	config := &TransformConfig{
		ExtractFields:         []string{"metadata.uid", "metadata.name", "spec.containers"},
		IncludeLabels:         false,
		IncludeAnnotations:    false,
		DiscoverRelationships: false,
		FieldMapping: FieldMappingConfig{
			Type: "custom",
			CustomMappings: map[string]string{
				"metadata.uid":    "resource_id",
				"metadata.name":   "resource_name",
				"spec.containers": "container_list",
			},
		},
	}

	transformer := NewBaseTransformer(config)
	
	pod := createTestPod()
	event := createTestEvent(pod)

	transformed, err := transformer.Transform(event)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Verify custom mappings
	expectedMappings := map[string]string{
		"resource_id":    "uid should be mapped to resource_id",
		"resource_name":  "name should be mapped to resource_name",
		"container_list": "containers should be mapped to container_list",
	}

	for expectedField, description := range expectedMappings {
		if _, exists := transformed.Fields[expectedField]; !exists {
			t.Errorf("%s: field '%s' not found", description, expectedField)
		}
	}

	// Verify original fields are NOT present
	originalFields := []string{"metadata.uid", "metadata.name", "spec.containers"}
	for _, originalField := range originalFields {
		if _, exists := transformed.Fields[originalField]; exists {
			t.Errorf("Original field '%s' should not exist after custom mapping", originalField)
		}
	}
}

func TestTransformerWithBackwardCompatibilityFlag(t *testing.T) {
	// Test EnableMetadataPrefixRemoval flag for backward compatibility
	config := &TransformConfig{
		ExtractFields:         []string{"metadata.uid", "metadata.name", "spec.containers"},
		IncludeLabels:         false,
		IncludeAnnotations:    false,
		DiscoverRelationships: false,
		FieldMapping: FieldMappingConfig{
			Type:                        "none",
			EnableMetadataPrefixRemoval: true,
		},
	}

	transformer := NewBaseTransformer(config)
	
	pod := createTestPod()
	event := createTestEvent(pod)

	transformed, err := transformer.Transform(event)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Should behave like metadata-prefix-removal
	if _, exists := transformed.Fields["uid"]; !exists {
		t.Error("Expected 'uid' field not found with backward compatibility flag")
	}
	if _, exists := transformed.Fields["spec.containers"]; !exists {
		t.Error("Expected 'spec.containers' field not found")
	}
}

func TestTransformerWithChainedMapping(t *testing.T) {
	// Test chaining custom mapping with metadata prefix removal
	config := &TransformConfig{
		ExtractFields:         []string{"metadata.uid", "metadata.name", "spec.containers"},
		IncludeLabels:         false,
		IncludeAnnotations:    false,
		DiscoverRelationships: false,
		FieldMapping: FieldMappingConfig{
			Type: "custom",
			CustomMappings: map[string]string{
				"spec.containers": "container_list",
			},
			EnableMetadataPrefixRemoval: true,
		},
	}

	transformer := NewBaseTransformer(config)
	
	pod := createTestPod()
	event := createTestEvent(pod)

	transformed, err := transformer.Transform(event)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Should apply both custom mapping AND metadata prefix removal
	expectedFields := []string{"uid", "name", "container_list"}
	for _, expectedField := range expectedFields {
		if _, exists := transformed.Fields[expectedField]; !exists {
			t.Errorf("Expected field '%s' not found in chained mapping", expectedField)
		}
	}

	// Verify original fields are gone
	unexpectedFields := []string{"metadata.uid", "metadata.name", "spec.containers"}
	for _, unexpectedField := range unexpectedFields {
		if _, exists := transformed.Fields[unexpectedField]; exists {
			t.Errorf("Unexpected field '%s' found after chained mapping", unexpectedField)
		}
	}
}

func TestTransformerWithNoFieldExtraction(t *testing.T) {
	// Test transformer with no field extraction
	config := &TransformConfig{
		ExtractFields:         []string{}, // No fields to extract
		IncludeLabels:         true,
		IncludeAnnotations:    false,
		DiscoverRelationships: false,
		FieldMapping: FieldMappingConfig{
			Type: "metadata-prefix-removal",
		},
	}

	transformer := NewBaseTransformer(config)
	
	pod := createTestPod()
	event := createTestEvent(pod)

	transformed, err := transformer.Transform(event)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// Should still work with empty fields
	if transformed.Fields != nil && len(transformed.Fields) > 0 {
		t.Errorf("Expected no extracted fields, got %+v", transformed.Fields)
	}

	// But labels should still be included
	if transformed.Labels == nil || transformed.Labels["app"] != "test" {
		t.Error("Labels should be included even with no field extraction")
	}
}

// Helper functions

func createTestPod() *v1.Pod {
	return &v1.Pod{
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
}

func createTestEvent(pod *v1.Pod) *informer.ResourceEvent {
	return &informer.ResourceEvent{
		Type:         informer.EventTypeAdded,
		Object:       pod,
		ObjectMeta:   pod.ObjectMeta,
		ResourceKey:  "default/test-pod",
		ResourceType: "pods",
		APIVersion:   "v1",
	}
}

func TestTransformerErrorHandling(t *testing.T) {
	config := &TransformConfig{
		ExtractFields:         []string{"metadata.uid"},
		IncludeLabels:         false,
		IncludeAnnotations:    false,
		DiscoverRelationships: false,
		FieldMapping: FieldMappingConfig{
			Type: "metadata-prefix-removal",
		},
	}

	transformer := NewBaseTransformer(config)

	// Test with nil event
	_, err := transformer.Transform(nil)
	if err == nil {
		t.Error("Expected error for nil event")
	}

	// Test with nil object
	event := &informer.ResourceEvent{
		Type:         informer.EventTypeAdded,
		Object:       nil,
		ResourceKey:  "test/test",
		ResourceType: "pods",
		APIVersion:   "v1",
	}

	_, err = transformer.Transform(event)
	if err == nil {
		t.Error("Expected error for nil object")
	}
}