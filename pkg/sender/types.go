package sender

import (
	"context"
	"time"
)

// Resource - Describes a resource (node) for the search indexer
type Resource struct {
	Kind           string                 `json:"kind,omitempty"`
	UID            string                 `json:"uid,omitempty"`
	ResourceString string                 `json:"resourceString,omitempty"`
	Properties     map[string]interface{} `json:"properties,omitempty"`
}

// Edge - Describes a relationship between resources
type Edge struct {
	SourceUID  string `json:"sourceUID,omitempty"`
	DestUID    string `json:"destUID,omitempty"`
	EdgeType   string `json:"edgeType,omitempty"`
	SourceKind string `json:"sourceKind,omitempty"`
	DestKind   string `json:"destKind,omitempty"`
}

// DeleteResourceEvent - Describes a resource deletion event
type DeleteResourceEvent struct {
	UID  string `json:"uid,omitempty"`
	Kind string `json:"kind,omitempty"`
}

// SyncEvent - Object sent by the collector with the resources to change
type SyncEvent struct {
	AddResources    []Resource             `json:"addResources,omitempty"`
	UpdateResources []Resource             `json:"updateResources,omitempty"`
	DeleteResources []DeleteResourceEvent  `json:"deleteResources,omitempty"`
	AddEdges        []Edge                 `json:"addEdges,omitempty"`
	DeleteEdges     []Edge                 `json:"deleteEdges,omitempty"`
}

// Sender defines the interface for sending sync events to the search indexer
type Sender interface {
	// SendSyncEvent sends a sync event to the search indexer
	SendSyncEvent(ctx context.Context, event *SyncEvent) error
	
	// ProcessChanges processes changed resources from the reconciler and sends them
	ProcessChanges(ctx context.Context) error
	
	// Start starts the sender background processes
	Start() error
	
	// Stop stops the sender
	Stop()
	
	// GetStats returns sender statistics
	GetStats() *SenderStats
}

// SenderConfig holds configuration for the sender
type SenderConfig struct {
	// Search indexer configuration
	IndexerURL     string        `json:"indexerURL"`
	IndexerTimeout time.Duration `json:"indexerTimeout"`
	
	// Authentication (if needed)
	APIKey   string `json:"apiKey,omitempty"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
	
	// Batch processing
	BatchSize      int           `json:"batchSize"`      // Maximum resources per batch
	BatchTimeout   time.Duration `json:"batchTimeout"`   // Maximum time to wait for batch
	SendInterval   time.Duration `json:"sendInterval"`   // How often to check for changes
	
	// Retry configuration
	MaxRetries     int           `json:"maxRetries"`
	RetryDelay     time.Duration `json:"retryDelay"`
	RetryBackoff   float64       `json:"retryBackoff"`   // Exponential backoff multiplier
	
	// Resource limits
	MaxResourcesPerSync int `json:"maxResourcesPerSync"` // Maximum resources in a single sync event
	
	// Error handling
	FailureThreshold int           `json:"failureThreshold"` // Number of failures before backing off
	BackoffDuration  time.Duration `json:"backoffDuration"`  // How long to back off after failures
}

// SenderStats provides statistics about the sender
type SenderStats struct {
	// Sync statistics
	TotalSyncEvents   int64     `json:"totalSyncEvents"`
	SuccessfulSyncs   int64     `json:"successfulSyncs"`
	FailedSyncs       int64     `json:"failedSyncs"`
	LastSyncTime      time.Time `json:"lastSyncTime"`
	LastSuccessTime   time.Time `json:"lastSuccessTime"`
	LastFailureTime   time.Time `json:"lastFailureTime"`
	
	// Resource statistics
	ResourcesSent     int64 `json:"resourcesSent"`
	ResourcesAdded    int64 `json:"resourcesAdded"`
	ResourcesUpdated  int64 `json:"resourcesUpdated"`
	ResourcesDeleted  int64 `json:"resourcesDeleted"`
	EdgesSent         int64 `json:"edgesSent"`
	EdgesAdded        int64 `json:"edgesAdded"`
	EdgesDeleted      int64 `json:"edgesDeleted"`
	
	// Performance statistics
	AverageLatencyMs  float64 `json:"averageLatencyMs"`
	AverageBatchSize  float64 `json:"averageBatchSize"`
	
	// Error statistics
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	LastError          string `json:"lastError,omitempty"`
	
	// Current state
	IsHealthy        bool      `json:"isHealthy"`
	CurrentBackoff   bool      `json:"currentBackoff"`
	NextSendTime     time.Time `json:"nextSendTime"`
	PendingResources int       `json:"pendingResources"`
}

// ResourceConversionContext holds context for resource conversion
type ResourceConversionContext struct {
	ClusterName   string            `json:"clusterName,omitempty"`
	ClusterUID    string            `json:"clusterUID,omitempty"`
	ExtraLabels   map[string]string `json:"extraLabels,omitempty"`
	FieldMappings map[string]string `json:"fieldMappings,omitempty"`
}