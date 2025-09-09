package reconciler

import (
	"time"

	"github.com/jlpadilla/search-collector-ai/pkg/transformer"
)

// ResourceState represents the current state of a resource in the reconciler
type ResourceState struct {
	// Resource identification
	ResourceKey  string `json:"resourceKey"`  // namespace/name format
	ResourceType string `json:"resourceType"` // e.g., "pods", "services"
	
	// Current transformed resource data
	TransformedResource *transformer.TransformedResource `json:"transformedResource"`
	
	// State metadata
	FirstSeen    time.Time `json:"firstSeen"`    // When resource was first discovered
	LastUpdated  time.Time `json:"lastUpdated"`  // When resource was last updated
	UpdateCount  int64     `json:"updateCount"`  // Number of times resource has been updated
	Generation   int64     `json:"generation"`   // Kubernetes resource generation
	
	// State tracking
	IsDeleted    bool      `json:"isDeleted"`    // True if resource is marked for deletion
	DeletedAt    *time.Time `json:"deletedAt"`   // When resource was deleted
	
	// Change tracking
	HasChanges   bool      `json:"hasChanges"`   // True if resource has pending changes for indexer
	LastSynced   time.Time `json:"lastSynced"`   // When resource was last synced to indexer
}

// StateChange represents a change to be applied to the state
type StateChange struct {
	// Change type
	Type StateChangeType `json:"type"`
	
	// Resource identification
	ResourceKey  string `json:"resourceKey"`
	ResourceType string `json:"resourceType"`
	
	// Data for add/update operations
	TransformedResource *transformer.TransformedResource `json:"transformedResource,omitempty"`
	
	// Metadata
	Timestamp  time.Time `json:"timestamp"`
	Generation int64     `json:"generation"`
}

// StateChangeType represents the type of state change
type StateChangeType string

const (
	StateChangeAdd    StateChangeType = "ADD"
	StateChangeUpdate StateChangeType = "UPDATE"
	StateChangeDelete StateChangeType = "DELETE"
)

// Reconciler manages the state of all resources and merges changes
type Reconciler interface {
	// ApplyChange applies a state change to the reconciler
	ApplyChange(change *StateChange) error
	
	// GetResource retrieves the current state of a resource
	GetResource(resourceKey string) (*ResourceState, bool)
	
	// ListResources returns all resources, optionally filtered by type
	ListResources(resourceType string) []*ResourceState
	
	// GetChangedResources returns resources that have pending changes for the indexer
	GetChangedResources() []*ResourceState
	
	// MarkSynced marks resources as synced to the indexer
	MarkSynced(resourceKeys []string) error
	
	// GetStats returns reconciler statistics
	GetStats() *ReconcilerStats
	
	// Cleanup removes old deleted resources from state
	Cleanup(olderThan time.Duration) int
	
	// Start starts the reconciler background processes
	Start() error
	
	// Stop stops the reconciler
	Stop()
}

// ReconcilerStats provides statistics about the reconciler state
type ReconcilerStats struct {
	// Resource counts
	TotalResources   int `json:"totalResources"`
	ActiveResources  int `json:"activeResources"`
	DeletedResources int `json:"deletedResources"`
	ChangedResources int `json:"changedResources"`
	
	// Resource type breakdown
	ResourcesByType map[string]int `json:"resourcesByType"`
	
	// Update statistics
	TotalUpdates     int64     `json:"totalUpdates"`
	LastUpdateTime   time.Time `json:"lastUpdateTime"`
	
	// Memory usage (approximate)
	EstimatedMemoryMB float64 `json:"estimatedMemoryMB"`
}

// ReconcilerConfig holds configuration for the reconciler
type ReconcilerConfig struct {
	// Cleanup configuration
	CleanupInterval    time.Duration `json:"cleanupInterval"`    // How often to run cleanup
	DeletedRetention   time.Duration `json:"deletedRetention"`   // How long to keep deleted resources
	
	// Memory management
	MaxResources       int     `json:"maxResources"`       // Maximum number of resources to keep in memory
	MemoryThresholdMB  float64 `json:"memoryThresholdMB"`  // Memory threshold for cleanup
	
	// Change batching
	ChangeBatchSize    int           `json:"changeBatchSize"`    // Maximum changes to batch
	ChangeBatchTimeout time.Duration `json:"changeBatchTimeout"` // Timeout for batching changes
	
	// Metrics and monitoring
	EnableMetrics      bool `json:"enableMetrics"`      // Enable metrics collection
	MetricsInterval    time.Duration `json:"metricsInterval"`    // Metrics collection interval
}