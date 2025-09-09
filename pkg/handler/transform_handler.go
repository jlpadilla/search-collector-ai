package handler

import (
	"time"

	"github.com/jlpadilla/search-collector-ai/pkg/informer"
	"github.com/jlpadilla/search-collector-ai/pkg/reconciler"
	"github.com/jlpadilla/search-collector-ai/pkg/transformer"
	"k8s.io/klog/v2"
)

// TransformHandler handles events from informers and processes them through transformers
type TransformHandler struct {
	transformer transformer.Transformer
	reconciler  reconciler.Reconciler
}

// NewTransformHandler creates a new transform handler
func NewTransformHandler(t transformer.Transformer, r reconciler.Reconciler) *TransformHandler {
	return &TransformHandler{
		transformer: t,
		reconciler:  r,
	}
}

// OnAdd handles resource addition events
func (h *TransformHandler) OnAdd(event *informer.ResourceEvent) error {
	// klog.Infof("Received ADD event for %s: %s", event.ResourceType, event.ResourceKey)

	transformed, err := h.transformer.Transform(event)
	if err != nil {
		klog.Errorf("Failed to transform ADD event for %s: %v", event.ResourceKey, err)
		return err
	}

	keys := make([]string, 0, len(transformed.Fields))
	for k := range transformed.Fields {
		keys = append(keys, k)
	}
	// klog.Infof("Transformed ADD event for %s: extracted %d fields. %v", event.ResourceKey, len(transformed.Fields), keys)

	// Send to reconciler
	change := &reconciler.StateChange{
		Type:                reconciler.StateChangeAdd,
		ResourceKey:         event.ResourceKey,
		ResourceType:        event.ResourceType,
		TransformedResource: transformed,
		Timestamp:           time.Now(),
		Generation:          event.ObjectMeta.Generation,
	}

	err = h.reconciler.ApplyChange(change)
	if err != nil {
		klog.Errorf("Failed to apply ADD change to reconciler for %s: %v", event.ResourceKey, err)
		return err
	}

	return nil
}

// OnUpdate handles resource update events
func (h *TransformHandler) OnUpdate(oldEvent, newEvent *informer.ResourceEvent) error {
	if newEvent == nil {
		return nil
	}

	// klog.Infof("Received UPDATE event for %s: %s", newEvent.ResourceType, newEvent.ResourceKey)

	transformed, err := h.transformer.Transform(newEvent)
	if err != nil {
		klog.Errorf("Failed to transform UPDATE event for %s: %v", newEvent.ResourceKey, err)
		return err
	}

	keys := make([]string, 0, len(transformed.Fields))
	for k := range transformed.Fields {
		keys = append(keys, k)
	}
	klog.Infof("Transformed UPDATE event for %s: extracted %d fields. %v", newEvent.ResourceKey, len(transformed.Fields), keys)

	// Send to reconciler
	change := &reconciler.StateChange{
		Type:                reconciler.StateChangeUpdate,
		ResourceKey:         newEvent.ResourceKey,
		ResourceType:        newEvent.ResourceType,
		TransformedResource: transformed,
		Timestamp:           time.Now(),
		Generation:          newEvent.ObjectMeta.Generation,
	}

	err = h.reconciler.ApplyChange(change)
	if err != nil {
		klog.Errorf("Failed to apply UPDATE change to reconciler for %s: %v", newEvent.ResourceKey, err)
		return err
	}

	return nil
}

// OnDelete handles resource deletion events
func (h *TransformHandler) OnDelete(event *informer.ResourceEvent) error {
	// klog.Infof("Received DELETE event for %s: %s", event.ResourceType, event.ResourceKey)

	// For deletions, we might not need full transformation
	// Just need to know what to remove from the search index
	klog.Infof("Processed DELETE event for %s: ready for index removal", event.ResourceKey)

	// Send deletion signal to reconciler
	change := &reconciler.StateChange{
		Type:         reconciler.StateChangeDelete,
		ResourceKey:  event.ResourceKey,
		ResourceType: event.ResourceType,
		Timestamp:    time.Now(),
		Generation:   event.ObjectMeta.Generation,
	}

	err := h.reconciler.ApplyChange(change)
	if err != nil {
		klog.Errorf("Failed to apply DELETE change to reconciler for %s: %v", event.ResourceKey, err)
		return err
	}

	return nil
}

// OnError handles errors from the informer
func (h *TransformHandler) OnError(err error) {
	klog.Errorf("Informer error: %v", err)
}
