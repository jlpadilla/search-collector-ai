package transformer

import (
	"fmt"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
)

// FieldExtractor extracts specific fields from Kubernetes objects to reduce memory usage
type FieldExtractor struct {
	fields []string
}

// NewFieldExtractor creates a new field extractor
func NewFieldExtractor(fields []string) *FieldExtractor {
	return &FieldExtractor{
		fields: fields,
	}
}

// Extract extracts specified fields from a Kubernetes object
func (fe *FieldExtractor) Extract(obj runtime.Object) map[string]interface{} {
	if len(fe.fields) == 0 {
		return nil
	}
	
	result := make(map[string]interface{})
	objValue := reflect.ValueOf(obj)
	
	// Handle pointer
	if objValue.Kind() == reflect.Ptr {
		objValue = objValue.Elem()
	}
	
	if !objValue.IsValid() || objValue.Kind() != reflect.Struct {
		return result
	}
	
	for _, fieldPath := range fe.fields {
		value := fe.extractFieldByPath(objValue, fieldPath)
		if value != nil {
			// Remove "metadata." prefix from field names for cleaner indexing
			indexKey := fe.getIndexKey(fieldPath)
			result[indexKey] = value
		}
	}
	
	return result
}

// extractFieldByPath extracts a field value using dot notation (e.g., "spec.containers.0.image")
func (fe *FieldExtractor) extractFieldByPath(objValue reflect.Value, fieldPath string) interface{} {
	parts := strings.Split(fieldPath, ".")
	current := objValue
	
	for _, part := range parts {
		current = fe.getFieldValue(current, part)
		if !current.IsValid() {
			return nil
		}
	}
	
	if current.IsValid() && current.CanInterface() {
		return current.Interface()
	}
	
	return nil
}

// getFieldValue gets a field value by name, handling both struct fields and map keys
func (fe *FieldExtractor) getFieldValue(val reflect.Value, fieldName string) reflect.Value {
	if !val.IsValid() {
		return reflect.Value{}
	}
	
	switch val.Kind() {
	case reflect.Struct:
		// Try to find field by name (case-insensitive)
		valType := val.Type()
		for i := 0; i < val.NumField(); i++ {
			field := valType.Field(i)
			if strings.EqualFold(field.Name, fieldName) {
				return val.Field(i)
			}
			// Also check JSON tags
			if jsonTag := field.Tag.Get("json"); jsonTag != "" {
				jsonName := strings.Split(jsonTag, ",")[0]
				if strings.EqualFold(jsonName, fieldName) {
					return val.Field(i)
				}
			}
		}
		
	case reflect.Map:
		// Handle map access
		for _, key := range val.MapKeys() {
			if key.String() == fieldName {
				return val.MapIndex(key)
			}
		}
		
	case reflect.Slice, reflect.Array:
		// Handle array/slice index access
		if idx, err := parseInt(fieldName); err == nil && idx < val.Len() {
			return val.Index(idx)
		}
		
	case reflect.Ptr, reflect.Interface:
		// Dereference pointer/interface
		if !val.IsNil() {
			return fe.getFieldValue(val.Elem(), fieldName)
		}
	}
	
	return reflect.Value{}
}

// parseInt parses a string to int, returns -1 on error
func parseInt(s string) (int, error) {
	var result int
	for _, ch := range s {
		if ch < '0' || ch > '9' {
			return -1, fmt.Errorf("not a number")
		}
		result = result*10 + int(ch-'0')
	}
	return result, nil
}

// getIndexKey converts field paths to index keys, removing "metadata." prefix for cleaner indexing
func (fe *FieldExtractor) getIndexKey(fieldPath string) string {
	// Remove "metadata." prefix from field names for cleaner indexing
	if strings.HasPrefix(fieldPath, "metadata.") {
		return strings.TrimPrefix(fieldPath, "metadata.")
	}
	return fieldPath
}