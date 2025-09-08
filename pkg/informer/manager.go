package informer

import (
	"context"
	"fmt"
	"sync"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

// Manager manages multiple resource informers
type Manager struct {
	client    dynamic.Interface
	informers map[schema.GroupVersionResource]ResourceInformer
	mu        sync.RWMutex
	running   bool
	stopCh    chan struct{}
}

// NewManager creates a new informer manager
func NewManager(client dynamic.Interface) *Manager {
	return &Manager{
		client:    client,
		informers: make(map[schema.GroupVersionResource]ResourceInformer),
		stopCh:    make(chan struct{}),
	}
}

// AddInformer adds an informer for a specific resource type
func (m *Manager) AddInformer(gvr schema.GroupVersionResource, config *InformerConfig, handler EventHandler) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.running {
		return fmt.Errorf("cannot add informer while manager is running")
	}
	
	// Create informer if it doesn't exist
	if _, exists := m.informers[gvr]; !exists {
		informer := NewStreamingInformer(m.client, gvr, config)
		m.informers[gvr] = informer
	}
	
	// Add event handler
	m.informers[gvr].AddEventHandler(handler)
	
	return nil
}

// Start starts all managed informers
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if m.running {
		return fmt.Errorf("manager is already running")
	}
	
	m.running = true
	
	// Start all informers
	for gvr, informer := range m.informers {
		if err := informer.Start(ctx); err != nil {
			// Stop already started informers
			m.stopInformers()
			m.running = false
			return fmt.Errorf("failed to start informer for %s: %w", gvr.String(), err)
		}
	}
	
	return nil
}

// Stop stops all managed informers
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	if !m.running {
		return
	}
	
	m.running = false
	m.stopInformers()
	close(m.stopCh)
}

func (m *Manager) stopInformers() {
	for _, informer := range m.informers {
		informer.Stop()
	}
}

// IsRunning returns true if the manager is running
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// GetInformer returns the informer for a specific resource type
func (m *Manager) GetInformer(gvr schema.GroupVersionResource) (ResourceInformer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	informer, exists := m.informers[gvr]
	return informer, exists
}