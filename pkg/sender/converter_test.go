package sender

import (
	"testing"

	"github.com/jlpadilla/search-collector-ai/pkg/reconciler"
	"github.com/jlpadilla/search-collector-ai/pkg/transformer"
)

func TestExtractUID(t *testing.T) {
	// Create a mock HTTP sender
	config := DefaultSenderConfig()
	sender := &HTTPSender{config: config}

	// Test case 1: UID exists in metadata.uid field
	tr1 := &transformer.TransformedResource{
		ResourceKey:  "default/test-pod",
		ResourceType: "pods",
		Name:         "test-pod",
		Namespace:    "default",
		Fields: map[string]interface{}{
			"metadata.uid":  "test-uid-12345",
			"metadata.name": "test-pod",
		},
	}

	uid1 := sender.extractUID(tr1)
	if uid1 != "test-uid-12345" {
		t.Errorf("Expected UID 'test-uid-12345', got '%s'", uid1)
	}

	// Test case 2: UID exists in uid field
	tr2 := &transformer.TransformedResource{
		ResourceKey:  "default/test-service",
		ResourceType: "services",
		Name:         "test-service",
		Namespace:    "default",
		Fields: map[string]interface{}{
			"uid":           "service-uid-67890",
			"metadata.name": "test-service",
		},
	}

	uid2 := sender.extractUID(tr2)
	if uid2 != "service-uid-67890" {
		t.Errorf("Expected UID 'service-uid-67890', got '%s'", uid2)
	}

	// Test case 3: No UID field, should generate fallback
	tr3 := &transformer.TransformedResource{
		ResourceKey:  "default/test-configmap",
		ResourceType: "configmaps",
		Name:         "test-configmap",
		Namespace:    "default",
		Fields: map[string]interface{}{
			"metadata.name": "test-configmap",
		},
	}

	uid3 := sender.extractUID(tr3)
	expectedFallback := "fallback-configmaps-default/test-configmap"
	if uid3 != expectedFallback {
		t.Errorf("Expected fallback UID '%s', got '%s'", expectedFallback, uid3)
	}

	// Test case 4: No UID and no resource key, should use last resort
	tr4 := &transformer.TransformedResource{
		ResourceType: "secrets",
		Name:         "test-secret",
		Namespace:    "default",
		Fields: map[string]interface{}{
			"metadata.name": "test-secret",
		},
	}

	uid4 := sender.extractUID(tr4)
	expectedLastResort := "lastresort-secrets-test-secret"
	if uid4 != expectedLastResort {
		t.Errorf("Expected last resort UID '%s', got '%s'", expectedLastResort, uid4)
	}

	t.Logf("All UID extraction tests passed")
}

func TestConvertToResource(t *testing.T) {
	// Create a mock HTTP sender
	config := DefaultSenderConfig()
	sender := &HTTPSender{config: config}

	// Test converting a resource with proper UID
	resourceState := &reconciler.ResourceState{
		ResourceKey: "default/test-pod",
		TransformedResource: &transformer.TransformedResource{
			ResourceKey:  "default/test-pod",
			ResourceType: "pods",
			Name:         "test-pod",
			Namespace:    "default",
			Fields: map[string]interface{}{
				"metadata.uid":    "test-uid-12345",
				"metadata.name":   "test-pod",
				"status.phase":    "Running",
				"spec.nodeName":   "worker-1",
			},
			Labels: map[string]string{
				"app": "test",
			},
		},
		UpdateCount: 1, // First update, so should be treated as "add"
	}

	resource := sender.convertToResource(resourceState)
	if resource == nil {
		t.Fatal("convertToResource returned nil")
	}

	if resource.UID != "test-uid-12345" {
		t.Errorf("Expected UID 'test-uid-12345', got '%s'", resource.UID)
	}

	if resource.Kind != "Pod" {
		t.Errorf("Expected Kind 'Pod', got '%s'", resource.Kind)
	}

	if resource.ResourceString != "default/test-pod" {
		t.Errorf("Expected ResourceString 'default/test-pod', got '%s'", resource.ResourceString)
	}

	// Check properties
	if resource.Properties == nil {
		t.Fatal("Properties map is nil")
	}

	if resource.Properties["status_phase"] != "Running" {
		t.Errorf("Expected status_phase 'Running', got '%v'", resource.Properties["status_phase"])
	}

	t.Logf("Resource conversion test passed: %+v", resource)
}

func TestConvertToDeleteEvent(t *testing.T) {
	// Create a mock HTTP sender
	config := DefaultSenderConfig()
	sender := &HTTPSender{config: config}

	// Test converting a deleted resource
	resourceState := &reconciler.ResourceState{
		ResourceKey: "default/deleted-pod",
		TransformedResource: &transformer.TransformedResource{
			ResourceKey:  "default/deleted-pod",
			ResourceType: "pods",
			Name:         "deleted-pod",
			Namespace:    "default",
			Fields: map[string]interface{}{
				"metadata.uid":  "deleted-uid-98765",
				"metadata.name": "deleted-pod",
			},
		},
		IsDeleted: true,
	}

	deleteEvent := sender.convertToDeleteEvent(resourceState)
	if deleteEvent == nil {
		t.Fatal("convertToDeleteEvent returned nil")
	}

	if deleteEvent.UID != "deleted-uid-98765" {
		t.Errorf("Expected UID 'deleted-uid-98765', got '%s'", deleteEvent.UID)
	}

	if deleteEvent.Kind != "Pod" {
		t.Errorf("Expected Kind 'Pod', got '%s'", deleteEvent.Kind)
	}

	t.Logf("Delete event conversion test passed: %+v", deleteEvent)
}