package transformer

import (
	"testing"
	"time"

	"github.com/jlpadilla/search-collector-ai/pkg/informer"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

func TestFieldExtractorMetadataRenaming(t *testing.T) {
	// Create a field extractor with metadata fields
	fields := []string{"metadata.uid", "metadata.name", "metadata.namespace", "metadata.creationTimestamp"}
	extractor := NewFieldExtractor(fields)

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
			CreationTimestamp: metav1.Time{Time: time.Now()},
		},
	}

	// Extract fields
	extracted := extractor.Extract(pod)
	if extracted == nil {
		t.Fatal("Extracted fields is nil")
	}

	// Debug: print all extracted fields
	t.Logf("Extracted fields: %+v", extracted)

	// Verify that metadata fields are renamed (without "metadata." prefix)
	expectedFields := map[string]bool{
		"uid":               true,
		"name":              true,
		"namespace":         true,
		"creationTimestamp": true,
	}

	for expectedField := range expectedFields {
		if _, exists := extracted[expectedField]; !exists {
			t.Errorf("Expected field '%s' not found in extracted fields", expectedField)
		}
	}

	// Verify that the old metadata. prefixed fields are NOT present
	unexpectedFields := []string{"metadata.uid", "metadata.name", "metadata.namespace", "metadata.creationTimestamp"}
	for _, unexpectedField := range unexpectedFields {
		if _, exists := extracted[unexpectedField]; exists {
			t.Errorf("Unexpected field '%s' found in extracted fields (should be renamed)", unexpectedField)
		}
	}

	// Verify actual values
	if uid := extracted["uid"]; uid == nil || string(uid.(types.UID)) != "test-uid-12345" {
		t.Errorf("Expected UID 'test-uid-12345', got '%v' (type: %T)", extracted["uid"], extracted["uid"])
	}

	if nameStr, ok := extracted["name"].(string); !ok || nameStr != "test-pod" {
		t.Errorf("Expected name 'test-pod', got '%v' (type: %T)", extracted["name"], extracted["name"])
	}

	if nsStr, ok := extracted["namespace"].(string); !ok || nsStr != "default" {
		t.Errorf("Expected namespace 'default', got '%v' (type: %T)", extracted["namespace"], extracted["namespace"])
	}

	t.Logf("Field extraction test passed - metadata fields renamed correctly")
}

func TestTransformerIntegrationWithRenamedFields(t *testing.T) {
	// Create a transformer config with metadata fields
	config := &TransformConfig{
		ExtractFields:         []string{"metadata.uid", "metadata.name", "metadata.namespace"},
		IncludeLabels:         true,
		IncludeAnnotations:    false,
		DiscoverRelationships: false,
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

	// Verify transformed resource has renamed fields
	if transformed.Fields == nil {
		t.Fatal("Fields map is nil")
	}

	// Check that renamed fields are present
	expectedFields := []string{"uid", "name", "namespace"}
	for _, field := range expectedFields {
		if _, exists := transformed.Fields[field]; !exists {
			t.Errorf("Expected renamed field '%s' not found in transformed fields", field)
		}
	}

	// Verify actual values
	if uid := transformed.Fields["uid"]; uid == nil || string(uid.(types.UID)) != "test-uid-12345" {
		t.Errorf("Expected UID 'test-uid-12345', got '%v' (type: %T)", transformed.Fields["uid"], transformed.Fields["uid"])
	}

	if nameStr, ok := transformed.Fields["name"].(string); !ok || nameStr != "test-pod" {
		t.Errorf("Expected name 'test-pod', got '%v' (type: %T)", transformed.Fields["name"], transformed.Fields["name"])
	}

	if nsStr, ok := transformed.Fields["namespace"].(string); !ok || nsStr != "default" {
		t.Errorf("Expected namespace 'default', got '%v' (type: %T)", transformed.Fields["namespace"], transformed.Fields["namespace"])
	}

	t.Logf("Transformer integration test passed - metadata fields renamed correctly")
}

func TestGetIndexKey(t *testing.T) {
	extractor := NewFieldExtractor([]string{})

	testCases := []struct {
		input    string
		expected string
	}{
		{"metadata.uid", "uid"},
		{"metadata.name", "name"},
		{"metadata.namespace", "namespace"},
		{"metadata.creationTimestamp", "creationTimestamp"},
		{"metadata.labels", "labels"},
		{"spec.containers", "spec.containers"},
		{"status.phase", "status.phase"},
		{"kind", "kind"},
	}

	for _, tc := range testCases {
		result := extractor.getIndexKey(tc.input)
		if result != tc.expected {
			t.Errorf("getIndexKey(%s) = %s, expected %s", tc.input, result, tc.expected)
		}
	}

	t.Logf("getIndexKey test passed")
}