package reconciler

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/jlpadilla/search-collector-ai/pkg/transformer"
	"k8s.io/klog/v2"
)

// MemoryReconciler implements an in-memory reconciler with state management
type MemoryReconciler struct {
	config *ReconcilerConfig
	
	// State storage
	mu        sync.RWMutex
	resources map[string]*ResourceState // key: resourceKey
	
	// Statistics
	stats *ReconcilerStats
	
	// Control channels
	ctx        context.Context
	cancel     context.CancelFunc
	changeCh   chan *StateChange
	
	// Background processes
	wg sync.WaitGroup
}

// NewMemoryReconciler creates a new in-memory reconciler
func NewMemoryReconciler(config *ReconcilerConfig) *MemoryReconciler {
	if config == nil {
		config = DefaultReconcilerConfig()
	}
	
	ctx, cancel := context.WithCancel(context.Background())
	
	return &MemoryReconciler{
		config:    config,
		resources: make(map[string]*ResourceState),
		stats: &ReconcilerStats{
			ResourcesByType: make(map[string]int),
		},
		ctx:      ctx,
		cancel:   cancel,
		changeCh: make(chan *StateChange, config.ChangeBatchSize*2), // Buffer for batching
	}
}

// Start starts the reconciler background processes
func (r *MemoryReconciler) Start() error {
	klog.Info("Starting memory reconciler...")
	
	// Start change processing goroutine
	r.wg.Add(1)
	go r.processChanges()
	
	// Start cleanup goroutine
	if r.config.CleanupInterval > 0 {
		r.wg.Add(1)
		go r.runCleanup()
	}
	
	// Start metrics collection if enabled
	if r.config.EnableMetrics && r.config.MetricsInterval > 0 {
		r.wg.Add(1)
		go r.collectMetrics()
	}
	
	klog.Info("Memory reconciler started")
	return nil
}

// Stop stops the reconciler
func (r *MemoryReconciler) Stop() {
	klog.Info("Stopping memory reconciler...")
	r.cancel()
	close(r.changeCh)
	r.wg.Wait()
	klog.Info("Memory reconciler stopped")
}

// ApplyChange applies a state change to the reconciler
func (r *MemoryReconciler) ApplyChange(change *StateChange) error {
	if change == nil {
		return fmt.Errorf("change cannot be nil")
	}
	
	// Send change to processing channel
	select {
	case r.changeCh <- change:
		return nil
	case <-r.ctx.Done():
		return fmt.Errorf("reconciler is stopping")
	default:
		// Channel is full, apply change synchronously
		return r.applyChangeSync(change)
	}
}

// processChanges processes state changes in batches
func (r *MemoryReconciler) processChanges() {
	defer r.wg.Done()
	
	ticker := time.NewTicker(r.config.ChangeBatchTimeout)
	defer ticker.Stop()
	
	var batch []*StateChange
	
	for {
		select {
		case change, ok := <-r.changeCh:
			if !ok {
				// Channel closed, process remaining batch
				if len(batch) > 0 {
					r.applyChangeBatch(batch)
				}
				return
			}
			
			batch = append(batch, change)
			
			// Process batch if it's full
			if len(batch) >= r.config.ChangeBatchSize {
				r.applyChangeBatch(batch)
				batch = batch[:0] // Reset batch
			}
			
		case <-ticker.C:
			// Process batch on timeout
			if len(batch) > 0 {
				r.applyChangeBatch(batch)
				batch = batch[:0] // Reset batch
			}
			
		case <-r.ctx.Done():
			// Process remaining batch before shutdown
			if len(batch) > 0 {
				r.applyChangeBatch(batch)
			}
			return
		}
	}
}

// applyChangeBatch applies a batch of changes
func (r *MemoryReconciler) applyChangeBatch(batch []*StateChange) {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	for _, change := range batch {
		err := r.applyChangeLocked(change)
		if err != nil {
			klog.Errorf("Failed to apply change for %s: %v", change.ResourceKey, err)
		}
	}
	
	// Update stats after batch processing
	r.updateStatsLocked()
	
	if len(batch) > 1 {
		klog.V(4).Infof("Processed batch of %d changes", len(batch))
	}
}

// applyChangeSync applies a change synchronously
func (r *MemoryReconciler) applyChangeSync(change *StateChange) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	err := r.applyChangeLocked(change)
	if err == nil {
		r.updateStatsLocked()
	}
	return err
}

// applyChangeLocked applies a change while holding the lock
func (r *MemoryReconciler) applyChangeLocked(change *StateChange) error {
	now := time.Now()
	
	switch change.Type {
	case StateChangeAdd:
		return r.applyAddLocked(change, now)
	case StateChangeUpdate:
		return r.applyUpdateLocked(change, now)
	case StateChangeDelete:
		return r.applyDeleteLocked(change, now)
	default:
		return fmt.Errorf("unknown change type: %s", change.Type)
	}
}

// applyAddLocked handles resource addition
func (r *MemoryReconciler) applyAddLocked(change *StateChange, now time.Time) error {
	if change.TransformedResource == nil {
		return fmt.Errorf("transformed resource is required for add operation")
	}
	
	// Check if resource already exists
	if existing, exists := r.resources[change.ResourceKey]; exists {
		if existing.IsDeleted {
			// Resource was deleted, treat as update
			klog.V(4).Infof("Re-adding previously deleted resource: %s", change.ResourceKey)
			return r.applyUpdateLocked(change, now)
		}
		// Resource already exists, treat as update
		klog.V(4).Infof("Resource already exists, treating add as update: %s", change.ResourceKey)
		return r.applyUpdateLocked(change, now)
	}
	
	// Create new resource state
	state := &ResourceState{
		ResourceKey:         change.ResourceKey,
		ResourceType:        change.ResourceType,
		TransformedResource: change.TransformedResource,
		FirstSeen:           now,
		LastUpdated:         now,
		UpdateCount:         1,
		Generation:          change.Generation,
		IsDeleted:           false,
		HasChanges:          true, // New resource has changes
	}
	
	r.resources[change.ResourceKey] = state
	
	klog.V(4).Infof("Added resource to state: %s", change.ResourceKey)
	return nil
}

// applyUpdateLocked handles resource updates
func (r *MemoryReconciler) applyUpdateLocked(change *StateChange, now time.Time) error {
	if change.TransformedResource == nil {
		return fmt.Errorf("transformed resource is required for update operation")
	}
	
	existing, exists := r.resources[change.ResourceKey]
	if !exists {
		// Resource doesn't exist, treat as add
		klog.V(4).Infof("Resource doesn't exist, treating update as add: %s", change.ResourceKey)
		return r.applyAddLocked(change, now)
	}
	
	// Check if this is actually a change
	if r.hasResourceChanged(existing.TransformedResource, change.TransformedResource) {
		// Update the resource state
		existing.TransformedResource = change.TransformedResource
		existing.LastUpdated = now
		existing.UpdateCount++
		existing.Generation = change.Generation
		existing.HasChanges = true // Mark as having changes
		existing.IsDeleted = false // Clear deleted flag if it was set
		existing.DeletedAt = nil
		
		klog.V(4).Infof("Updated resource in state: %s (generation: %d)", change.ResourceKey, change.Generation)
	} else {
		klog.V(6).Infof("No changes detected for resource: %s", change.ResourceKey)
	}
	
	return nil
}

// applyDeleteLocked handles resource deletion
func (r *MemoryReconciler) applyDeleteLocked(change *StateChange, now time.Time) error {
	existing, exists := r.resources[change.ResourceKey]
	if !exists {
		klog.V(4).Infof("Attempted to delete non-existent resource: %s", change.ResourceKey)
		return nil // Not an error, resource might have been cleaned up
	}
	
	// Mark resource as deleted
	existing.IsDeleted = true
	existing.DeletedAt = &now
	existing.LastUpdated = now
	existing.HasChanges = true // Deletion is a change
	
	klog.V(4).Infof("Marked resource as deleted: %s", change.ResourceKey)
	return nil
}

// hasResourceChanged compares two transformed resources to detect changes
func (r *MemoryReconciler) hasResourceChanged(old, new *transformer.TransformedResource) bool {
	if old == nil && new == nil {
		return false
	}
	if old == nil || new == nil {
		return true
	}
	
	// Compare basic fields
	if old.ResourceKey != new.ResourceKey ||
		old.ResourceType != new.ResourceType ||
		old.APIVersion != new.APIVersion ||
		old.Name != new.Name ||
		old.Namespace != new.Namespace {
		return true
	}
	
	// Compare field count
	if len(old.Fields) != len(new.Fields) {
		return true
	}
	
	// Compare field values
	for key, oldValue := range old.Fields {
		if newValue, exists := new.Fields[key]; !exists || !deepEqual(oldValue, newValue) {
			return true
		}
	}
	
	// Compare relationships count
	if len(old.Relationships) != len(new.Relationships) {
		return true
	}
	
	// For performance, we do a simplified relationship comparison
	// In production, you might want more sophisticated comparison
	for i, oldRel := range old.Relationships {
		if i >= len(new.Relationships) ||
			oldRel.Type != new.Relationships[i].Type ||
			oldRel.TargetKey != new.Relationships[i].TargetKey {
			return true
		}
	}
	
	return false
}

// deepEqual performs a simple deep equality check
func deepEqual(a, b interface{}) bool {
	// Simple implementation - in production you might use reflect.DeepEqual
	// or a more sophisticated comparison
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// GetResource retrieves the current state of a resource
func (r *MemoryReconciler) GetResource(resourceKey string) (*ResourceState, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	state, exists := r.resources[resourceKey]
	if !exists {
		return nil, false
	}
	
	// Return a copy to prevent external modification
	stateCopy := *state
	return &stateCopy, true
}

// ListResources returns all resources, optionally filtered by type
func (r *MemoryReconciler) ListResources(resourceType string) []*ResourceState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	var result []*ResourceState
	
	for _, state := range r.resources {
		if resourceType == "" || state.ResourceType == resourceType {
			// Return a copy to prevent external modification
			stateCopy := *state
			result = append(result, &stateCopy)
		}
	}
	
	return result
}

// GetChangedResources returns resources that have pending changes for the indexer
func (r *MemoryReconciler) GetChangedResources() []*ResourceState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	var result []*ResourceState
	
	for _, state := range r.resources {
		if state.HasChanges {
			// Return a copy to prevent external modification
			stateCopy := *state
			result = append(result, &stateCopy)
		}
	}
	
	return result
}

// MarkSynced marks resources as synced to the indexer
func (r *MemoryReconciler) MarkSynced(resourceKeys []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	now := time.Now()
	
	for _, key := range resourceKeys {
		if state, exists := r.resources[key]; exists {
			state.HasChanges = false
			state.LastSynced = now
		}
	}
	
	klog.V(4).Infof("Marked %d resources as synced", len(resourceKeys))
	return nil
}

// GetStats returns reconciler statistics
func (r *MemoryReconciler) GetStats() *ReconcilerStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	
	// Return a copy of current stats
	statsCopy := *r.stats
	statsCopy.ResourcesByType = make(map[string]int)
	for k, v := range r.stats.ResourcesByType {
		statsCopy.ResourcesByType[k] = v
	}
	
	return &statsCopy
}

// updateStatsLocked updates statistics while holding the lock
func (r *MemoryReconciler) updateStatsLocked() {
	r.stats.TotalResources = len(r.resources)
	r.stats.ActiveResources = 0
	r.stats.DeletedResources = 0
	r.stats.ChangedResources = 0
	r.stats.LastUpdateTime = time.Now()
	
	// Reset resource type counts
	for k := range r.stats.ResourcesByType {
		r.stats.ResourcesByType[k] = 0
	}
	
	// Count resources by type and status
	for _, state := range r.resources {
		if state.IsDeleted {
			r.stats.DeletedResources++
		} else {
			r.stats.ActiveResources++
		}
		
		if state.HasChanges {
			r.stats.ChangedResources++
		}
		
		r.stats.ResourcesByType[state.ResourceType]++
	}
	
	// Estimate memory usage
	r.stats.EstimatedMemoryMB = r.estimateMemoryUsage()
}

// estimateMemoryUsage provides a rough estimate of memory usage
func (r *MemoryReconciler) estimateMemoryUsage() float64 {
	// Very rough estimation - each resource state is approximately 1-5KB
	avgResourceSize := 3.0 // KB
	totalSizeKB := float64(len(r.resources)) * avgResourceSize
	return totalSizeKB / 1024.0 // Convert to MB
}

// Cleanup removes old deleted resources from state
func (r *MemoryReconciler) Cleanup(olderThan time.Duration) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	
	cutoff := time.Now().Add(-olderThan)
	var toRemove []string
	
	for key, state := range r.resources {
		if state.IsDeleted && state.DeletedAt != nil && state.DeletedAt.Before(cutoff) {
			toRemove = append(toRemove, key)
		}
	}
	
	for _, key := range toRemove {
		delete(r.resources, key)
	}
	
	if len(toRemove) > 0 {
		klog.Infof("Cleaned up %d old deleted resources", len(toRemove))
		r.updateStatsLocked()
	}
	
	return len(toRemove)
}

// runCleanup runs periodic cleanup
func (r *MemoryReconciler) runCleanup() {
	defer r.wg.Done()
	
	ticker := time.NewTicker(r.config.CleanupInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			r.Cleanup(r.config.DeletedRetention)
		case <-r.ctx.Done():
			return
		}
	}
}

// collectMetrics runs periodic metrics collection
func (r *MemoryReconciler) collectMetrics() {
	defer r.wg.Done()
	
	ticker := time.NewTicker(r.config.MetricsInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			stats := r.GetStats()
			
			// Log metrics
			klog.V(2).Infof("Reconciler metrics: total=%d, active=%d, deleted=%d, changed=%d, memory=%.1fMB",
				stats.TotalResources,
				stats.ActiveResources,
				stats.DeletedResources,
				stats.ChangedResources,
				stats.EstimatedMemoryMB)
			
			// Check memory threshold
			if r.config.MemoryThresholdMB > 0 && stats.EstimatedMemoryMB > r.config.MemoryThresholdMB {
				klog.Warningf("Memory usage (%.1fMB) exceeds threshold (%.1fMB), running cleanup",
					stats.EstimatedMemoryMB, r.config.MemoryThresholdMB)
				r.Cleanup(r.config.DeletedRetention / 2) // More aggressive cleanup
			}
			
			// Force garbage collection if memory usage is high
			if stats.EstimatedMemoryMB > r.config.MemoryThresholdMB*1.5 {
				runtime.GC()
			}
			
		case <-r.ctx.Done():
			return
		}
	}
}

// DefaultReconcilerConfig returns a default reconciler configuration
func DefaultReconcilerConfig() *ReconcilerConfig {
	return &ReconcilerConfig{
		CleanupInterval:    10 * time.Minute,
		DeletedRetention:   1 * time.Hour,
		MaxResources:       100000,
		MemoryThresholdMB:  512.0,
		ChangeBatchSize:    100,
		ChangeBatchTimeout: 1 * time.Second,
		EnableMetrics:      true,
		MetricsInterval:    30 * time.Second,
	}
}