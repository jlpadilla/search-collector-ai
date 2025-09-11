package transformer

import (
	"strings"
)

// FieldMapper defines the interface for transforming field names
type FieldMapper interface {
	// MapFields transforms the field names in the given fields map
	MapFields(fields map[string]interface{}) map[string]interface{}
	
	// MapFieldName transforms a single field name
	MapFieldName(fieldPath string) string
}

// NoOpFieldMapper doesn't transform field names (maintains backward compatibility)
type NoOpFieldMapper struct{}

func (m *NoOpFieldMapper) MapFields(fields map[string]interface{}) map[string]interface{} {
	return fields
}

func (m *NoOpFieldMapper) MapFieldName(fieldPath string) string {
	return fieldPath
}

// MetadataPrefixMapper removes "metadata." prefix from field names for cleaner indexing
type MetadataPrefixMapper struct{}

func (m *MetadataPrefixMapper) MapFields(fields map[string]interface{}) map[string]interface{} {
	if fields == nil {
		return fields
	}
	
	result := make(map[string]interface{})
	for fieldPath, value := range fields {
		mappedName := m.MapFieldName(fieldPath)
		
		// Special handling for apiVersion field - split into apigroup and apiversion
		if (fieldPath == "apiVersion" || fieldPath == "TypeMeta.APIVersion") && value != nil {
			if apiVersionStr, ok := value.(string); ok {
				apiGroup, apiVersion := splitAPIVersion(apiVersionStr)
				result["apigroup"] = apiGroup
				result["apiversion"] = apiVersion
				continue
			}
		}
		
		result[mappedName] = value
	}
	
	return result
}

func (m *MetadataPrefixMapper) MapFieldName(fieldPath string) string {
	if strings.HasPrefix(fieldPath, "metadata.") {
		return strings.TrimPrefix(fieldPath, "metadata.")
	}
	// Map TypeMeta.Kind to just "kind" for cleaner indexing
	if fieldPath == "TypeMeta.Kind" {
		return "kind"
	}
	return fieldPath
}

// ConfigurableFieldMapper allows custom field mapping rules
type ConfigurableFieldMapper struct {
	mappings map[string]string // fieldPath -> mappedName
}

func NewConfigurableFieldMapper(mappings map[string]string) *ConfigurableFieldMapper {
	return &ConfigurableFieldMapper{
		mappings: mappings,
	}
}

func (m *ConfigurableFieldMapper) MapFields(fields map[string]interface{}) map[string]interface{} {
	if fields == nil {
		return fields
	}
	
	result := make(map[string]interface{})
	for fieldPath, value := range fields {
		mappedName := m.MapFieldName(fieldPath)
		result[mappedName] = value
	}
	
	return result
}

func (m *ConfigurableFieldMapper) MapFieldName(fieldPath string) string {
	if mappedName, exists := m.mappings[fieldPath]; exists {
		return mappedName
	}
	return fieldPath
}

// ChainedFieldMapper allows chaining multiple field mappers
type ChainedFieldMapper struct {
	mappers []FieldMapper
}

func NewChainedFieldMapper(mappers ...FieldMapper) *ChainedFieldMapper {
	return &ChainedFieldMapper{
		mappers: mappers,
	}
}

func (m *ChainedFieldMapper) MapFields(fields map[string]interface{}) map[string]interface{} {
	result := fields
	for _, mapper := range m.mappers {
		result = mapper.MapFields(result)
	}
	return result
}

func (m *ChainedFieldMapper) MapFieldName(fieldPath string) string {
	result := fieldPath
	for _, mapper := range m.mappers {
		result = mapper.MapFieldName(result)
	}
	return result
}

// splitAPIVersion splits a Kubernetes apiVersion into group and version components
// Examples:
//   - "v1" -> ("", "v1") for core resources
//   - "apps/v1" -> ("apps", "v1") for apps group
//   - "networking.k8s.io/v1" -> ("networking.k8s.io", "v1") for networking group
func splitAPIVersion(apiVersion string) (group, version string) {
	if apiVersion == "" {
		return "", ""
	}
	
	parts := strings.Split(apiVersion, "/")
	if len(parts) == 1 {
		// Core API resources like "v1"
		return "", parts[0]
	} else if len(parts) == 2 {
		// Group API resources like "apps/v1"
		return parts[0], parts[1]
	}
	
	// Fallback for unexpected format
	return "", apiVersion
}