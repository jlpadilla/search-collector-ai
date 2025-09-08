package informer

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

// EventType represents the type of Kubernetes event
type EventType string

const (
	EventTypeAdded    EventType = "ADDED"
	EventTypeModified EventType = "MODIFIED"
	EventTypeDeleted  EventType = "DELETED"
	EventTypeError    EventType = "ERROR"
)

// ResourceEvent represents a minimal event structure to reduce memory usage
type ResourceEvent struct {
	Type         EventType         `json:"type"`
	ResourceKey  string            `json:"resourceKey"`  // namespace/name format
	ResourceType string            `json:"resourceType"` // e.g., "Pod", "Service"
	APIVersion   string            `json:"apiVersion"`
	Object       runtime.Object    `json:"object"`     // Raw Kubernetes object
	ObjectMeta   metav1.ObjectMeta `json:"objectMeta"` // Metadata for quick access
}

// EventHandler defines the interface for handling resource events
type EventHandler interface {
	OnAdd(event *ResourceEvent) error
	OnUpdate(oldEvent, newEvent *ResourceEvent) error
	OnDelete(event *ResourceEvent) error
	OnError(err error)
}

// ResourceInformer defines the interface for a memory-optimized Kubernetes informer
type ResourceInformer interface {
	// Start begins watching for resource events
	Start(ctx context.Context) error

	// Stop stops the informer
	Stop()

	// AddEventHandler adds an event handler
	AddEventHandler(handler EventHandler)

	// IsRunning returns true if the informer is currently running
	IsRunning() bool
}

// InformerConfig holds configuration for the informer
type InformerConfig struct {
	// Namespace to watch (empty for all namespaces)
	Namespace string

	// ResyncPeriod defines how often to perform full resyncs
	ResyncPeriod *metav1.Duration

	// FieldSelectors for filtering resources
	FieldSelector string

	// LabelSelector for filtering resources
	LabelSelector string

	// BufferSize for the event channel
	BufferSize int
}
