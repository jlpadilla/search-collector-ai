package handler

import (
	"github.com/jlpadilla/search-collector-ai/pkg/informer"
	"github.com/jlpadilla/search-collector-ai/pkg/transformer"
	"k8s.io/klog/v2"
)

// TransformHandler handles events from informers and processes them through transformers
type TransformHandler struct {
	transformer transformer.Transformer
	// TODO: Add channel to send transformed data to reconciler
}

// NewTransformHandler creates a new transform handler
func NewTransformHandler(t transformer.Transformer) *TransformHandler {
	return &TransformHandler{
		transformer: t,
	}
}

// OnAdd handles resource addition events
func (h *TransformHandler) OnAdd(event *informer.ResourceEvent) error {
	klog.Infof("Received ADD event for %s: %s", event.ResourceType, event.ResourceKey)
	
	transformed, err := h.transformer.Transform(event)
	if err != nil {
		klog.Errorf("Failed to transform ADD event for %s: %v", event.ResourceKey, err)
		return err
	}
	
	klog.Infof("Transformed ADD event for %s: extracted %d fields", event.ResourceKey, len(transformed.Fields))
	
	// TODO: Send to reconciler
	_ = transformed
	
	return nil
}

// OnUpdate handles resource update events
func (h *TransformHandler) OnUpdate(oldEvent, newEvent *informer.ResourceEvent) error {
	if newEvent == nil {
		return nil
	}
	
	klog.Infof("Received UPDATE event for %s: %s", newEvent.ResourceType, newEvent.ResourceKey)
	
	transformed, err := h.transformer.Transform(newEvent)
	if err != nil {
		klog.Errorf("Failed to transform UPDATE event for %s: %v", newEvent.ResourceKey, err)
		return err
	}
	
	klog.Infof("Transformed UPDATE event for %s: extracted %d fields", newEvent.ResourceKey, len(transformed.Fields))
	
	// TODO: Send to reconciler
	_ = transformed
	
	return nil
}

// OnDelete handles resource deletion events
func (h *TransformHandler) OnDelete(event *informer.ResourceEvent) error {
	klog.Infof("Received DELETE event for %s: %s", event.ResourceType, event.ResourceKey)
	
	// For deletions, we might not need full transformation
	// Just need to know what to remove from the search index
	klog.Infof("Processed DELETE event for %s: ready for index removal", event.ResourceKey)
	
	// TODO: Send deletion signal to reconciler
	
	return nil
}

// OnError handles errors from the informer
func (h *TransformHandler) OnError(err error) {
	klog.Errorf("Informer error: %v", err)
}