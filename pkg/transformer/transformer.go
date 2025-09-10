package transformer

import (
	"fmt"
	"time"

	"github.com/jlpadilla/search-collector-ai/pkg/informer"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

// baseTransformer provides common transformation functionality
type baseTransformer struct {
	config         *TransformConfig
	fieldExtractor *FieldExtractor
	fieldMapper    FieldMapper
}

// NewBaseTransformer creates a new base transformer
func NewBaseTransformer(config *TransformConfig) Transformer {
	// Create field mapper based on configuration
	var fieldMapper FieldMapper
	switch config.FieldMapping.Type {
	case "metadata-prefix-removal":
		fieldMapper = &MetadataPrefixMapper{}
	case "custom":
		fieldMapper = NewConfigurableFieldMapper(config.FieldMapping.CustomMappings)
	case "none":
		fallthrough
	default:
		fieldMapper = &NoOpFieldMapper{}
	}
	
	// Handle backward compatibility for metadata prefix removal
	if config.FieldMapping.EnableMetadataPrefixRemoval {
		if config.FieldMapping.Type == "custom" {
			// Chain custom mappings with metadata prefix removal
			fieldMapper = NewChainedFieldMapper(
				NewConfigurableFieldMapper(config.FieldMapping.CustomMappings),
				&MetadataPrefixMapper{},
			)
		} else if config.FieldMapping.Type == "none" || config.FieldMapping.Type == "" {
			fieldMapper = &MetadataPrefixMapper{}
		}
	}

	return &baseTransformer{
		config:         config,
		fieldExtractor: NewFieldExtractor(config.ExtractFields),
		fieldMapper:    fieldMapper,
	}
}

func (t *baseTransformer) Transform(event *informer.ResourceEvent) (*TransformedResource, error) {
	if event == nil {
		return nil, fmt.Errorf("event is nil")
	}
	if event.Object == nil {
		return nil, fmt.Errorf("event object is nil")
	}
	
	// Extract basic metadata
	meta := event.ObjectMeta
	
	// Extract fields and apply field mapping
	extractedFields := t.fieldExtractor.Extract(event.Object)
	mappedFields := t.fieldMapper.MapFields(extractedFields)
	
	transformed := &TransformedResource{
		ResourceKey:  event.ResourceKey,
		ResourceType: event.ResourceType,
		APIVersion:   event.APIVersion,
		Namespace:    meta.Namespace,
		Name:         meta.Name,
		Fields:       mappedFields,
	}
	
	// Add labels if configured
	if t.config.IncludeLabels && meta.Labels != nil {
		transformed.Labels = meta.Labels
	}
	
	// Add annotations if configured
	if t.config.IncludeAnnotations && meta.Annotations != nil {
		transformed.Annotations = meta.Annotations
	}
	
	// Add timestamp information
	if !meta.CreationTimestamp.IsZero() {
		transformed.CreatedAt = meta.CreationTimestamp.Format(time.RFC3339)
	}
	
	// For updates, use the current time as updated timestamp
	if event.Type == informer.EventTypeModified {
		transformed.UpdatedAt = time.Now().Format(time.RFC3339)
	}
	
	// Discover relationships if configured
	if t.config.DiscoverRelationships {
		relationships, err := t.DiscoverRelationships(transformed, event.Object)
		if err != nil {
			// Log error but don't fail the transformation
			klog.Warningf("Failed to discover relationships for %s: %v", event.ResourceKey, err)
		} else {
			transformed.Relationships = relationships
		}
	}
	
	return transformed, nil
}

func (t *baseTransformer) DiscoverRelationships(resource *TransformedResource, obj runtime.Object) ([]ResourceRelationship, error) {
	var relationships []ResourceRelationship
	
	// Get object metadata
	metaObj, ok := obj.(metav1.Object)
	if !ok {
		return relationships, nil
	}
	
	// Check for owner references
	for _, ownerRef := range metaObj.GetOwnerReferences() {
		relationships = append(relationships, ResourceRelationship{
			Type:       "owner",
			TargetKey:  fmt.Sprintf("%s/%s", resource.Namespace, ownerRef.Name),
			TargetType: ownerRef.Kind,
			TargetName: ownerRef.Name,
			Namespace:  resource.Namespace,
		})
	}
	
	// Resource-specific relationship discovery can be implemented here
	// For example, for Pods we might discover:
	// - Services that select this pod
	// - ConfigMaps and Secrets referenced by the pod
	// - PersistentVolumeClaims used by the pod
	
	return relationships, nil
}

func (t *baseTransformer) GetSupportedTypes() []string {
	// Base transformer supports all types
	return []string{"*"}
}