package transformer

import (
	"fmt"

	"k8s.io/klog/v2"
)

// TransformerFactory is a function that creates a transformer
type TransformerFactory func(config *TransformConfig) Transformer

// TransformerRegistry manages different transformer implementations
type TransformerRegistry struct {
	factories map[string]TransformerFactory
}

// NewTransformerRegistry creates a new transformer registry
func NewTransformerRegistry() *TransformerRegistry {
	registry := &TransformerRegistry{
		factories: make(map[string]TransformerFactory),
	}
	
	// Register built-in transformers
	registry.registerBuiltinTransformers()
	
	return registry
}

// registerBuiltinTransformers registers the built-in transformer types
func (r *TransformerRegistry) registerBuiltinTransformers() {
	// Base transformer - minimal field extraction
	r.Register("base", func(config *TransformConfig) Transformer {
		return NewBaseTransformer(config)
	})
	
	// Resource-specific transformer - kubectl get equivalent fields
	r.Register("resource-specific", func(config *TransformConfig) Transformer {
		return NewResourceSpecificTransformer(config)
	})
	
	// Configurable transformer - supports configuration files
	r.Register("configurable", func(config *TransformConfig) Transformer {
		transformer, err := NewConfigurableTransformer(config, config.ConfigFile)
		if err != nil {
			klog.Errorf("Failed to create configurable transformer: %v", err)
			// Fall back to resource-specific transformer
			return NewResourceSpecificTransformer(config)
		}
		return transformer
	})
	
	// Default transformer (alias for configurable)
	r.Register("default", r.factories["configurable"])
}

// Register adds a new transformer factory to the registry
func (r *TransformerRegistry) Register(name string, factory TransformerFactory) {
	r.factories[name] = factory
	klog.V(4).Infof("Registered transformer: %s", name)
}

// Create creates a transformer by name
func (r *TransformerRegistry) Create(name string, config *TransformConfig) (Transformer, error) {
	factory, exists := r.factories[name]
	if !exists {
		return nil, fmt.Errorf("transformer not found: %s", name)
	}
	
	return factory(config), nil
}

// List returns the names of all registered transformers
func (r *TransformerRegistry) List() []string {
	var names []string
	for name := range r.factories {
		names = append(names, name)
	}
	return names
}

// GetDefault returns the default transformer
func (r *TransformerRegistry) GetDefault(config *TransformConfig) Transformer {
	if transformer, err := r.Create("configurable", config); err == nil {
		return transformer
	}
	
	// Fall back to resource-specific if configurable fails
	klog.Warning("Failed to create configurable transformer, falling back to resource-specific")
	return NewResourceSpecificTransformer(config)
}

// Global registry instance
var defaultRegistry = NewTransformerRegistry()

// RegisterTransformer registers a transformer in the global registry
func RegisterTransformer(name string, factory TransformerFactory) {
	defaultRegistry.Register(name, factory)
}

// CreateTransformer creates a transformer from the global registry
func CreateTransformer(name string, config *TransformConfig) (Transformer, error) {
	return defaultRegistry.Create(name, config)
}

// GetDefaultTransformer returns a default transformer from the global registry
func GetDefaultTransformer(config *TransformConfig) Transformer {
	return defaultRegistry.GetDefault(config)
}

// ListTransformers returns all available transformer names
func ListTransformers() []string {
	return defaultRegistry.List()
}