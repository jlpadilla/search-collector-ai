package transformer

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	"github.com/jlpadilla/search-collector-ai/pkg/informer"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
)

// TransformConfigFile represents the configuration file for field extraction
type TransformConfigFile struct {
	// Global configuration
	Global GlobalTransformConfig `json:"global"`
	
	// Resource-specific configurations
	Resources map[string]ResourceTransformConfig `json:"resources"`
}

// GlobalTransformConfig contains global transformation settings
type GlobalTransformConfig struct {
	// Default fields to extract for all resources
	DefaultFields []string `json:"defaultFields"`
	
	// Whether to include labels by default
	IncludeLabels bool `json:"includeLabels"`
	
	// Whether to include annotations by default
	IncludeAnnotations bool `json:"includeAnnotations"`
	
	// Whether to discover relationships by default
	DiscoverRelationships bool `json:"discoverRelationships"`
}

// ResourceTransformConfig contains resource-specific transformation settings
type ResourceTransformConfig struct {
	// Additional fields to extract (dot notation)
	AdditionalFields []string `json:"additionalFields"`
	
	// Custom field mappings (rename fields)
	FieldMappings map[string]string `json:"fieldMappings"`
	
	// Fields to exclude from extraction
	ExcludeFields []string `json:"excludeFields"`
	
	// Override global settings
	IncludeLabels     *bool `json:"includeLabels,omitempty"`
	IncludeAnnotations *bool `json:"includeAnnotations,omitempty"`
	DiscoverRelationships *bool `json:"discoverRelationships,omitempty"`
	
	// Custom field extraction rules
	CustomFields []CustomFieldRule `json:"customFields"`
}

// CustomFieldRule defines a custom field extraction rule
type CustomFieldRule struct {
	// Name of the field in the output
	FieldName string `json:"fieldName"`
	
	// JSONPath expression to extract the value
	JSONPath string `json:"jsonPath"`
	
	// Type conversion (string, int, bool, float, array)
	Type string `json:"type"`
	
	// Default value if field is not found
	DefaultValue interface{} `json:"defaultValue,omitempty"`
	
	// Condition for when to apply this rule
	Condition *FieldCondition `json:"condition,omitempty"`
}

// FieldCondition defines a condition for field extraction
type FieldCondition struct {
	// Field to check
	Field string `json:"field"`
	
	// Operator (equals, not_equals, contains, exists, not_exists)
	Operator string `json:"operator"`
	
	// Value to compare against
	Value interface{} `json:"value"`
}

// ConfigurableTransformer extends the resource-specific transformer with configuration
type ConfigurableTransformer struct {
	*ResourceSpecificTransformer
	config     *TransformConfigFile
	jsonExtractor *JSONPathExtractor
}

// NewConfigurableTransformer creates a transformer with configuration file support
func NewConfigurableTransformer(baseConfig *TransformConfig, configPath string) (Transformer, error) {
	// Create base transformer
	baseTransformer := NewResourceSpecificTransformer(baseConfig).(*ResourceSpecificTransformer)
	
	// Load configuration file if provided
	var transformConfig *TransformConfigFile
	if configPath != "" && fileExists(configPath) {
		var err error
		transformConfig, err = loadTransformConfig(configPath)
		if err != nil {
			klog.Warningf("Failed to load transform config from %s: %v", configPath, err)
			transformConfig = getDefaultTransformConfig()
		} else {
			klog.Infof("Loaded transform configuration from %s", configPath)
		}
	} else {
		transformConfig = getDefaultTransformConfig()
	}
	
	return &ConfigurableTransformer{
		ResourceSpecificTransformer: baseTransformer,
		config:                     transformConfig,
		jsonExtractor:              NewJSONPathExtractor(),
	}, nil
}

func (t *ConfigurableTransformer) Transform(event *informer.ResourceEvent) (*TransformedResource, error) {
	// Start with base transformation
	transformed, err := t.ResourceSpecificTransformer.Transform(event)
	if err != nil {
		return nil, err
	}
	
	// Apply configuration-based transformations
	err = t.applyConfigurableTransformations(transformed, event)
	if err != nil {
		klog.V(4).Infof("Failed to apply configurable transformations for %s: %v", event.ResourceKey, err)
		// Don't fail the transformation, just log the error
	}
	
	return transformed, nil
}

// applyConfigurableTransformations applies configuration-based field extraction
func (t *ConfigurableTransformer) applyConfigurableTransformations(transformed *TransformedResource, event *informer.ResourceEvent) error {
	if transformed.Fields == nil {
		transformed.Fields = make(map[string]interface{})
	}
	
	// Get resource-specific config
	resourceConfig, exists := t.config.Resources[event.ResourceType]
	if !exists {
		// Use global config for unknown resources
		return t.applyGlobalConfig(transformed, event)
	}
	
	// Apply additional fields
	for _, fieldPath := range resourceConfig.AdditionalFields {
		value, err := t.jsonExtractor.Extract(event.Object, fieldPath)
		if err == nil && value != nil {
			// Use original field name or mapping
			fieldName := fieldPath
			if mappedName, hasmapping := resourceConfig.FieldMappings[fieldPath]; hasmapping {
				fieldName = mappedName
			}
			transformed.Fields[fieldName] = value
		}
	}
	
	// Apply custom field rules
	for _, rule := range resourceConfig.CustomFields {
		if t.evaluateCondition(rule.Condition, transformed, event) {
			value, err := t.extractCustomField(rule, event.Object)
			if err == nil {
				transformed.Fields[rule.FieldName] = value
			} else if rule.DefaultValue != nil {
				transformed.Fields[rule.FieldName] = rule.DefaultValue
			}
		}
	}
	
	// Remove excluded fields
	for _, excludeField := range resourceConfig.ExcludeFields {
		delete(transformed.Fields, excludeField)
	}
	
	// Apply field mappings (rename fields)
	for originalName, newName := range resourceConfig.FieldMappings {
		if value, exists := transformed.Fields[originalName]; exists {
			transformed.Fields[newName] = value
			delete(transformed.Fields, originalName)
		}
	}
	
	// Override global settings if specified
	if resourceConfig.IncludeLabels != nil {
		if *resourceConfig.IncludeLabels && transformed.Labels == nil {
			transformed.Labels = event.ObjectMeta.Labels
		} else if !*resourceConfig.IncludeLabels {
			transformed.Labels = nil
		}
	}
	
	if resourceConfig.IncludeAnnotations != nil {
		if *resourceConfig.IncludeAnnotations && transformed.Annotations == nil {
			transformed.Annotations = event.ObjectMeta.Annotations
		} else if !*resourceConfig.IncludeAnnotations {
			transformed.Annotations = nil
		}
	}
	
	return nil
}

// applyGlobalConfig applies global configuration settings
func (t *ConfigurableTransformer) applyGlobalConfig(transformed *TransformedResource, event *informer.ResourceEvent) error {
	// Apply default fields
	for _, fieldPath := range t.config.Global.DefaultFields {
		value, err := t.jsonExtractor.Extract(event.Object, fieldPath)
		if err == nil && value != nil {
			transformed.Fields[fieldPath] = value
		}
	}
	
	return nil
}

// evaluateCondition checks if a condition is met
func (t *ConfigurableTransformer) evaluateCondition(condition *FieldCondition, transformed *TransformedResource, event *informer.ResourceEvent) bool {
	if condition == nil {
		return true
	}
	
	// Get the field value to check
	var fieldValue interface{}
	if value, exists := transformed.Fields[condition.Field]; exists {
		fieldValue = value
	} else {
		// Try to extract from the object
		var err error
		fieldValue, err = t.jsonExtractor.Extract(event.Object, condition.Field)
		if err != nil {
			fieldValue = nil
		}
	}
	
	// Evaluate condition
	switch condition.Operator {
	case "exists":
		return fieldValue != nil
	case "not_exists":
		return fieldValue == nil
	case "equals":
		return fieldValue == condition.Value
	case "not_equals":
		return fieldValue != condition.Value
	case "contains":
		if str, ok := fieldValue.(string); ok {
			if substr, ok := condition.Value.(string); ok {
				return strings.Contains(str, substr)
			}
		}
		return false
	default:
		return true
	}
}

// extractCustomField extracts a custom field using JSONPath
func (t *ConfigurableTransformer) extractCustomField(rule CustomFieldRule, obj runtime.Object) (interface{}, error) {
	value, err := t.jsonExtractor.Extract(obj, rule.JSONPath)
	if err != nil {
		return nil, err
	}
	
	// Apply type conversion
	return t.convertType(value, rule.Type), nil
}

// convertType converts a value to the specified type
func (t *ConfigurableTransformer) convertType(value interface{}, targetType string) interface{} {
	if value == nil {
		return nil
	}
	
	switch targetType {
	case "string":
		return fmt.Sprintf("%v", value)
	case "int":
		if i, ok := value.(int); ok {
			return i
		}
		if f, ok := value.(float64); ok {
			return int(f)
		}
		return 0
	case "bool":
		if b, ok := value.(bool); ok {
			return b
		}
		return false
	case "float":
		if f, ok := value.(float64); ok {
			return f
		}
		if i, ok := value.(int); ok {
			return float64(i)
		}
		return 0.0
	default:
		return value
	}
}

// loadTransformConfig loads configuration from a file
func loadTransformConfig(configPath string) (*TransformConfigFile, error) {
	data, err := ioutil.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	
	var config TransformConfigFile
	err = json.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}
	
	return &config, nil
}

// getDefaultTransformConfig returns a default configuration
func getDefaultTransformConfig() *TransformConfigFile {
	return &TransformConfigFile{
		Global: GlobalTransformConfig{
			DefaultFields: []string{
				"metadata.name",
				"metadata.namespace",
				"metadata.creationTimestamp",
			},
			IncludeLabels:         true,
			IncludeAnnotations:    false,
			DiscoverRelationships: true,
		},
		Resources: make(map[string]ResourceTransformConfig),
	}
}

// fileExists checks if a file exists
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// SaveDefaultConfig saves a default configuration file for reference
func SaveDefaultConfig(configPath string) error {
	config := &TransformConfigFile{
		Global: GlobalTransformConfig{
			DefaultFields: []string{
				"metadata.name",
				"metadata.namespace",
				"metadata.creationTimestamp",
				"metadata.labels",
			},
			IncludeLabels:         true,
			IncludeAnnotations:    false,
			DiscoverRelationships: true,
		},
		Resources: map[string]ResourceTransformConfig{
			"pods": {
				AdditionalFields: []string{
					"spec.nodeName",
					"status.phase",
					"status.podIP",
					"status.hostIP",
				},
				CustomFields: []CustomFieldRule{
					{
						FieldName: "container_count",
						JSONPath:  "spec.containers",
						Type:      "int",
					},
					{
						FieldName: "ready_condition",
						JSONPath:  "status.conditions[?(@.type=='Ready')].status",
						Type:      "string",
					},
				},
				FieldMappings: map[string]string{
					"spec.nodeName": "node",
					"status.phase":  "pod_status",
				},
			},
			"services": {
				AdditionalFields: []string{
					"spec.type",
					"spec.clusterIP",
					"spec.ports",
				},
				CustomFields: []CustomFieldRule{
					{
						FieldName: "port_count",
						JSONPath:  "spec.ports",
						Type:      "int",
					},
				},
			},
		},
	}
	
	// Create directory if it doesn't exist
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	
	err = ioutil.WriteFile(configPath, data, 0644)
	if err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	
	return nil
}