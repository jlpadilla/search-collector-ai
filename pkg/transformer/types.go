package transformer

import (
	"github.com/jlpadilla/search-collector-ai/pkg/informer"
	"k8s.io/apimachinery/pkg/runtime"
)

// TransformedResource represents a resource after transformation for search indexing
type TransformedResource struct {
	// Basic resource identification
	ResourceKey  string `json:"resourceKey"`  // namespace/name format
	ResourceType string `json:"resourceType"` // e.g., "Pod", "Service"
	APIVersion   string `json:"apiVersion"`
	Namespace    string `json:"namespace,omitempty"`
	Name         string `json:"name"`
	
	// Extracted fields for search indexing
	Fields map[string]interface{} `json:"fields"`
	
	// Relationships to other resources
	Relationships []ResourceRelationship `json:"relationships,omitempty"`
	
	// Labels and annotations for search
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	
	// Timestamp information
	CreatedAt string `json:"createdAt,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

// ResourceRelationship represents a relationship between resources
type ResourceRelationship struct {
	Type         string `json:"type"`         // e.g., "owner", "service", "configmap"
	TargetKey    string `json:"targetKey"`    // target resource key
	TargetType   string `json:"targetType"`   // target resource type
	TargetName   string `json:"targetName"`   // target resource name
	Namespace    string `json:"namespace,omitempty"`
}

// Transformer defines the interface for transforming Kubernetes resources
type Transformer interface {
	// Transform converts a raw Kubernetes resource event into a transformed resource
	Transform(event *informer.ResourceEvent) (*TransformedResource, error)
	
	// DiscoverRelationships finds relationships to other resources
	DiscoverRelationships(resource *TransformedResource, obj runtime.Object) ([]ResourceRelationship, error)
	
	// GetSupportedTypes returns the resource types this transformer supports
	GetSupportedTypes() []string
}

// TransformConfig holds configuration for transformers
type TransformConfig struct {
	// Transformer type to use (base, resource-specific, configurable)
	TransformerType string `json:"transformerType"`
	
	// Path to configuration file (for configurable transformer)
	ConfigFile string `json:"configFile"`
	
	// Fields to extract from resources (using dot notation)
	ExtractFields []string `json:"extractFields"`
	
	// Whether to discover relationships
	DiscoverRelationships bool `json:"discoverRelationships"`
	
	// Whether to include labels in output
	IncludeLabels bool `json:"includeLabels"`
	
	// Whether to include annotations in output
	IncludeAnnotations bool `json:"includeAnnotations"`
	
	// Field mapping configuration
	FieldMapping FieldMappingConfig `json:"fieldMapping"`
}

// FieldMappingConfig configures how field names are transformed
type FieldMappingConfig struct {
	// Type of field mapping: "none", "metadata-prefix-removal", "custom"
	Type string `json:"type"`
	
	// Custom mappings for "custom" type (fieldPath -> mappedName)
	CustomMappings map[string]string `json:"customMappings,omitempty"`
	
	// Whether to enable metadata prefix removal (for backward compatibility)
	EnableMetadataPrefixRemoval bool `json:"enableMetadataPrefixRemoval"`
}