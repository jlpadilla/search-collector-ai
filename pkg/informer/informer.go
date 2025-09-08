package informer

import (
	"context"
	"fmt"
	"sync"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

// streamingInformer implements ResourceInformer with memory optimization
type streamingInformer struct {
	client   dynamic.Interface
	gvr      schema.GroupVersionResource
	config   *InformerConfig
	handlers []EventHandler
	running  bool
	stopCh   chan struct{}
	mu       sync.RWMutex
	eventsCh chan *ResourceEvent
}

// NewStreamingInformer creates a new memory-optimized informer for a specific resource type
func NewStreamingInformer(
	client dynamic.Interface,
	gvr schema.GroupVersionResource,
	config *InformerConfig,
) ResourceInformer {
	if config.BufferSize == 0 {
		config.BufferSize = 1000 // default buffer size
	}
	
	return &streamingInformer{
		client:   client,
		gvr:      gvr,
		config:   config,
		handlers: make([]EventHandler, 0),
		stopCh:   make(chan struct{}),
		eventsCh: make(chan *ResourceEvent, config.BufferSize),
	}
}

func (s *streamingInformer) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return fmt.Errorf("informer is already running")
	}
	s.running = true
	s.mu.Unlock()

	// Start event processing goroutine
	go s.processEvents(ctx)
	
	// Start watching
	go s.watch(ctx)
	
	return nil
}

func (s *streamingInformer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	
	if !s.running {
		return
	}
	
	s.running = false
	close(s.stopCh)
	close(s.eventsCh)
}

func (s *streamingInformer) AddEventHandler(handler EventHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers = append(s.handlers, handler)
}

func (s *streamingInformer) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

// watch implements the core watching logic without local caching
func (s *streamingInformer) watch(ctx context.Context) {
	var resourceVersion string
	
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		default:
			// Get list options
			listOptions := s.buildListOptions()
			listOptions.ResourceVersion = resourceVersion
			listOptions.Watch = true
			
			// Start watching
			watcher, err := s.getResourceInterface().Watch(ctx, listOptions)
			if err != nil {
				klog.Errorf("Failed to start watch for %s: %v", s.gvr.String(), err)
				s.notifyError(fmt.Errorf("failed to start watch: %w", err))
				time.Sleep(5 * time.Second) // backoff
				continue
			}
			
			// Process watch events
			s.processWatchEvents(watcher, &resourceVersion)
			watcher.Stop()
		}
	}
}

func (s *streamingInformer) processWatchEvents(watcher watch.Interface, resourceVersion *string) {
	defer watcher.Stop()
	
	for {
		select {
		case <-s.stopCh:
			return
		case event, ok := <-watcher.ResultChan():
			if !ok {
				// Channel closed, restart watch
				return
			}
			
			if event.Type == watch.Error {
				klog.Errorf("Watch error for %s: %v", s.gvr.String(), event.Object)
				s.notifyError(fmt.Errorf("watch error: %v", event.Object))
				return
			}
			
			// Update resource version for next watch
			if obj, ok := event.Object.(metav1.Object); ok {
				*resourceVersion = obj.GetResourceVersion()
			}
			
			// Convert to our event format and send to channel
			resourceEvent := s.convertWatchEvent(event)
			if resourceEvent != nil {
				select {
				case s.eventsCh <- resourceEvent:
				case <-s.stopCh:
					return
				default:
					// Channel full, drop event or implement backpressure
					klog.Warningf("Event channel full for %s, dropping event for %s", s.gvr.String(), event.Object)
					s.notifyError(fmt.Errorf("event channel full, dropping event"))
				}
			}
		}
	}
}

func (s *streamingInformer) processEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case event, ok := <-s.eventsCh:
			if !ok {
				return
			}
			s.dispatchEvent(event)
		}
	}
}

func (s *streamingInformer) convertWatchEvent(event watch.Event) *ResourceEvent {
	if event.Object == nil {
		return nil
	}
	
	obj, ok := event.Object.(metav1.Object)
	if !ok {
		return nil
	}
	
	resourceKey, err := cache.MetaNamespaceKeyFunc(obj)
	if err != nil || resourceKey == "" {
		resourceKey = obj.GetName()
	}
	
	resourceEvent := &ResourceEvent{
		Type:         EventType(event.Type),
		ResourceKey:  resourceKey,
		ResourceType: s.gvr.Resource,
		APIVersion:   s.gvr.GroupVersion().String(),
		Object:       event.Object.(runtime.Object),
		ObjectMeta: metav1.ObjectMeta{
			Name:      obj.GetName(),
			Namespace: obj.GetNamespace(),
			Labels:    obj.GetLabels(),
			Annotations: obj.GetAnnotations(),
			CreationTimestamp: obj.GetCreationTimestamp(),
			ResourceVersion: obj.GetResourceVersion(),
			UID: obj.GetUID(),
		},
	}
	
	return resourceEvent
}

func (s *streamingInformer) dispatchEvent(event *ResourceEvent) {
	s.mu.RLock()
	handlers := make([]EventHandler, len(s.handlers))
	copy(handlers, s.handlers)
	s.mu.RUnlock()
	
	for _, handler := range handlers {
		switch event.Type {
		case EventTypeAdded:
			if err := handler.OnAdd(event); err != nil {
				klog.Errorf("Handler error on add for %s: %v", event.ResourceKey, err)
				s.notifyError(fmt.Errorf("handler error on add: %w", err))
			}
		case EventTypeModified:
			// For updates, we don't have the old object to save memory
			// Pass nil for old event, handlers should handle this case
			if err := handler.OnUpdate(nil, event); err != nil {
				klog.Errorf("Handler error on update for %s: %v", event.ResourceKey, err)
				s.notifyError(fmt.Errorf("handler error on update: %w", err))
			}
		case EventTypeDeleted:
			if err := handler.OnDelete(event); err != nil {
				klog.Errorf("Handler error on delete for %s: %v", event.ResourceKey, err)
				s.notifyError(fmt.Errorf("handler error on delete: %w", err))
			}
		}
	}
}

func (s *streamingInformer) notifyError(err error) {
	s.mu.RLock()
	handlers := make([]EventHandler, len(s.handlers))
	copy(handlers, s.handlers)
	s.mu.RUnlock()
	
	for _, handler := range handlers {
		handler.OnError(err)
	}
}

func (s *streamingInformer) buildListOptions() metav1.ListOptions {
	opts := metav1.ListOptions{}
	
	if s.config.FieldSelector != "" {
		opts.FieldSelector = s.config.FieldSelector
	}
	
	if s.config.LabelSelector != "" {
		opts.LabelSelector = s.config.LabelSelector
	}
	
	return opts
}

func (s *streamingInformer) getResourceInterface() dynamic.ResourceInterface {
	if s.config.Namespace != "" {
		return s.client.Resource(s.gvr).Namespace(s.config.Namespace)
	}
	return s.client.Resource(s.gvr)
}

