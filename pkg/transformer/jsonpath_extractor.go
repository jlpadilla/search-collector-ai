package transformer

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"k8s.io/apimachinery/pkg/runtime"
)

// JSONPathExtractor extracts values from Kubernetes objects using JSONPath expressions
type JSONPathExtractor struct {
}

// NewJSONPathExtractor creates a new JSONPath extractor
func NewJSONPathExtractor() *JSONPathExtractor {
	return &JSONPathExtractor{}
}

// Extract extracts a value from a Kubernetes object using a JSONPath-like expression
func (e *JSONPathExtractor) Extract(obj runtime.Object, path string) (interface{}, error) {
	if obj == nil {
		return nil, fmt.Errorf("object is nil")
	}
	
	// Convert object to map for easier traversal
	objMap, err := e.objectToMap(obj)
	if err != nil {
		return nil, fmt.Errorf("failed to convert object to map: %w", err)
	}
	
	// Parse and evaluate the path
	return e.evaluatePath(objMap, path)
}

// objectToMap converts a runtime.Object to a map[string]interface{}
func (e *JSONPathExtractor) objectToMap(obj runtime.Object) (map[string]interface{}, error) {
	// Marshal to JSON and back to get a clean map representation
	jsonBytes, err := json.Marshal(obj)
	if err != nil {
		return nil, err
	}
	
	var objMap map[string]interface{}
	err = json.Unmarshal(jsonBytes, &objMap)
	if err != nil {
		return nil, err
	}
	
	return objMap, nil
}

// evaluatePath evaluates a JSONPath-like expression
func (e *JSONPathExtractor) evaluatePath(data interface{}, path string) (interface{}, error) {
	if path == "" {
		return data, nil
	}
	
	// Handle array length extraction
	if strings.HasSuffix(path, ".length") || strings.HasSuffix(path, ".count") {
		arrayPath := strings.TrimSuffix(strings.TrimSuffix(path, ".length"), ".count")
		arrayValue, err := e.evaluatePath(data, arrayPath)
		if err != nil {
			return nil, err
		}
		return e.getArrayLength(arrayValue), nil
	}
	
	// Split path into segments
	segments := strings.Split(path, ".")
	current := data
	
	for i, segment := range segments {
		if segment == "" {
			continue
		}
		
		// Handle array access with index [n]
		if strings.Contains(segment, "[") && strings.Contains(segment, "]") {
			current = e.handleArrayAccess(current, segment)
			if current == nil {
				return nil, fmt.Errorf("failed to access array element: %s", segment)
			}
			continue
		}
		
		// Handle filter expressions [?(...)]
		if strings.Contains(segment, "[?") {
			filtered, err := e.handleFilterExpression(current, segment)
			if err != nil {
				return nil, err
			}
			current = filtered
			continue
		}
		
		// Regular field access
		switch v := current.(type) {
		case map[string]interface{}:
			if value, exists := v[segment]; exists {
				current = value
			} else {
				// Try case-insensitive lookup
				found := false
				for key, value := range v {
					if strings.EqualFold(key, segment) {
						current = value
						found = true
						break
					}
				}
				if !found {
					return nil, fmt.Errorf("field not found: %s at segment %d", segment, i)
				}
			}
		case []interface{}:
			// If we have an array and a field name, apply to all elements
			var results []interface{}
			for _, item := range v {
				if remainingPath := strings.Join(segments[i:], "."); remainingPath != "" {
					if result, err := e.evaluatePath(item, remainingPath); err == nil {
						results = append(results, result)
					}
				}
			}
			if len(results) > 0 {
				if len(results) == 1 {
					return results[0], nil
				}
				return results, nil
			}
			return nil, fmt.Errorf("cannot access field %s on array", segment)
		default:
			return nil, fmt.Errorf("cannot access field %s on type %T", segment, current)
		}
	}
	
	return current, nil
}

// handleArrayAccess handles array access like containers[0] or containers[*]
func (e *JSONPathExtractor) handleArrayAccess(data interface{}, segment string) interface{} {
	// Extract field name and index
	parts := strings.SplitN(segment, "[", 2)
	if len(parts) != 2 {
		return nil
	}
	
	fieldName := parts[0]
	indexPart := strings.TrimSuffix(parts[1], "]")
	
	// Get the field first
	var array interface{}
	if fieldName != "" {
		switch v := data.(type) {
		case map[string]interface{}:
			if value, exists := v[fieldName]; exists {
				array = value
			} else {
				return nil
			}
		default:
			return nil
		}
	} else {
		array = data
	}
	
	// Handle array indexing
	switch arr := array.(type) {
	case []interface{}:
		if indexPart == "*" {
			// Return all elements
			return arr
		}
		
		// Parse index
		index, err := strconv.Atoi(indexPart)
		if err != nil {
			return nil
		}
		
		// Handle negative indices
		if index < 0 {
			index = len(arr) + index
		}
		
		if index >= 0 && index < len(arr) {
			return arr[index]
		}
		return nil
	default:
		return nil
	}
}

// handleFilterExpression handles filter expressions like [?(@.type=='Ready')]
func (e *JSONPathExtractor) handleFilterExpression(data interface{}, segment string) (interface{}, error) {
	// Extract field name and filter
	parts := strings.SplitN(segment, "[?", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid filter expression: %s", segment)
	}
	
	fieldName := parts[0]
	filterExpr := strings.TrimSuffix(parts[1], "]")
	
	// Get the field first
	var array interface{}
	if fieldName != "" {
		switch v := data.(type) {
		case map[string]interface{}:
			if value, exists := v[fieldName]; exists {
				array = value
			} else {
				return nil, fmt.Errorf("field not found: %s", fieldName)
			}
		default:
			return nil, fmt.Errorf("cannot access field %s on type %T", fieldName, data)
		}
	} else {
		array = data
	}
	
	// Apply filter to array
	switch arr := array.(type) {
	case []interface{}:
		var filtered []interface{}
		for _, item := range arr {
			if e.evaluateFilter(item, filterExpr) {
				filtered = append(filtered, item)
			}
		}
		return filtered, nil
	default:
		return nil, fmt.Errorf("cannot apply filter to non-array type: %T", array)
	}
}

// evaluateFilter evaluates a filter expression like (@.type=='Ready')
func (e *JSONPathExtractor) evaluateFilter(item interface{}, filterExpr string) bool {
	// Simple filter parsing - supports basic comparisons
	// Remove parentheses and @. prefix
	expr := strings.Trim(filterExpr, "()")
	expr = strings.TrimPrefix(expr, "@.")
	
	// Parse comparison operators
	operators := []string{"==", "!=", "=", "<", ">", "<=", ">="}
	for _, op := range operators {
		if strings.Contains(expr, op) {
			parts := strings.SplitN(expr, op, 2)
			if len(parts) == 2 {
				field := strings.TrimSpace(parts[0])
				expectedValue := strings.Trim(strings.TrimSpace(parts[1]), "'\"")
				
				// Get field value from item
				fieldValue, err := e.evaluatePath(item, field)
				if err != nil {
					return false
				}
				
				// Convert field value to string for comparison
				fieldStr := fmt.Sprintf("%v", fieldValue)
				
				// Evaluate comparison
				switch op {
				case "==", "=":
					return fieldStr == expectedValue
				case "!=":
					return fieldStr != expectedValue
				case "<":
					return fieldStr < expectedValue
				case ">":
					return fieldStr > expectedValue
				case "<=":
					return fieldStr <= expectedValue
				case ">=":
					return fieldStr >= expectedValue
				}
			}
		}
	}
	
	return false
}

// getArrayLength returns the length of an array or slice
func (e *JSONPathExtractor) getArrayLength(data interface{}) int {
	if data == nil {
		return 0
	}
	
	switch v := data.(type) {
	case []interface{}:
		return len(v)
	case map[string]interface{}:
		return len(v)
	default:
		// Use reflection for other slice types
		rv := reflect.ValueOf(data)
		if rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
			return rv.Len()
		}
		return 0
	}
}