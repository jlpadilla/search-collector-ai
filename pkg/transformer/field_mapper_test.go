package transformer

import (
	"reflect"
	"testing"
)

func TestNoOpFieldMapper(t *testing.T) {
	mapper := &NoOpFieldMapper{}
	
	// Test MapFieldName
	testCases := []struct {
		input    string
		expected string
	}{
		{"metadata.uid", "metadata.uid"},
		{"metadata.name", "metadata.name"},
		{"spec.containers", "spec.containers"},
		{"", ""},
	}
	
	for _, tc := range testCases {
		result := mapper.MapFieldName(tc.input)
		if result != tc.expected {
			t.Errorf("MapFieldName(%s) = %s, expected %s", tc.input, result, tc.expected)
		}
	}
	
	// Test MapFields
	fields := map[string]interface{}{
		"metadata.uid":  "test-uid",
		"metadata.name": "test-name",
		"spec.replicas": 3,
	}
	
	result := mapper.MapFields(fields)
	if !reflect.DeepEqual(result, fields) {
		t.Errorf("MapFields should return unchanged fields, got %+v", result)
	}
	
	// Test nil fields
	if mapper.MapFields(nil) != nil {
		t.Error("MapFields(nil) should return nil")
	}
}

func TestMetadataPrefixMapper(t *testing.T) {
	mapper := &MetadataPrefixMapper{}
	
	// Test MapFieldName
	testCases := []struct {
		input    string
		expected string
	}{
		{"metadata.uid", "uid"},
		{"metadata.name", "name"},
		{"metadata.namespace", "namespace"},
		{"metadata.creationTimestamp", "creationTimestamp"},
		{"metadata.labels", "labels"},
		{"TypeMeta.Kind", "kind"},
		{"spec.containers", "spec.containers"},
		{"status.phase", "status.phase"},
		{"", ""},
		{"metadata.", ""},
		{"notmetadata.field", "notmetadata.field"},
	}
	
	for _, tc := range testCases {
		result := mapper.MapFieldName(tc.input)
		if result != tc.expected {
			t.Errorf("MapFieldName(%s) = %s, expected %s", tc.input, result, tc.expected)
		}
	}
	
	// Test MapFields
	fields := map[string]interface{}{
		"metadata.uid":      "test-uid",
		"metadata.name":     "test-name",
		"metadata.namespace": "test-ns",
		"TypeMeta.Kind":     "Pod",
		"spec.replicas":     3,
		"status.phase":      "Running",
	}
	
	expected := map[string]interface{}{
		"uid":           "test-uid",
		"name":          "test-name",
		"namespace":     "test-ns",
		"kind":          "Pod",
		"spec.replicas": 3,
		"status.phase":  "Running",
	}
	
	result := mapper.MapFields(fields)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("MapFields result mismatch.\nExpected: %+v\nActual: %+v", expected, result)
	}
	
	// Test nil fields
	if mapper.MapFields(nil) != nil {
		t.Error("MapFields(nil) should return nil")
	}
}

func TestConfigurableFieldMapper(t *testing.T) {
	mappings := map[string]string{
		"metadata.uid":   "resource_id",
		"metadata.name":  "resource_name",
		"spec.replicas":  "desired_count",
	}
	
	mapper := NewConfigurableFieldMapper(mappings)
	
	// Test MapFieldName
	testCases := []struct {
		input    string
		expected string
	}{
		{"metadata.uid", "resource_id"},
		{"metadata.name", "resource_name"},
		{"spec.replicas", "desired_count"},
		{"metadata.namespace", "metadata.namespace"}, // not in mappings
		{"status.phase", "status.phase"}, // not in mappings
	}
	
	for _, tc := range testCases {
		result := mapper.MapFieldName(tc.input)
		if result != tc.expected {
			t.Errorf("MapFieldName(%s) = %s, expected %s", tc.input, result, tc.expected)
		}
	}
	
	// Test MapFields
	fields := map[string]interface{}{
		"metadata.uid":       "test-uid",
		"metadata.name":      "test-name",
		"metadata.namespace": "test-ns",
		"spec.replicas":      3,
		"status.phase":       "Running",
	}
	
	expected := map[string]interface{}{
		"resource_id":        "test-uid",
		"resource_name":      "test-name",
		"metadata.namespace": "test-ns", // not mapped
		"desired_count":      3,
		"status.phase":       "Running", // not mapped
	}
	
	result := mapper.MapFields(fields)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("MapFields result mismatch.\nExpected: %+v\nActual: %+v", expected, result)
	}
}

func TestChainedFieldMapper(t *testing.T) {
	// Create custom mapper that maps spec.replicas -> desired_count
	customMappings := map[string]string{
		"spec.replicas": "desired_count",
	}
	customMapper := NewConfigurableFieldMapper(customMappings)
	
	// Chain with metadata prefix mapper
	chained := NewChainedFieldMapper(customMapper, &MetadataPrefixMapper{})
	
	// Test MapFieldName - should apply both transformations
	testCases := []struct {
		input    string
		expected string
	}{
		{"metadata.uid", "uid"},              // only metadata prefix removal
		{"spec.replicas", "desired_count"},   // only custom mapping
		{"metadata.name", "name"},            // only metadata prefix removal
		{"status.phase", "status.phase"},     // no transformation
	}
	
	for _, tc := range testCases {
		result := chained.MapFieldName(tc.input)
		if result != tc.expected {
			t.Errorf("MapFieldName(%s) = %s, expected %s", tc.input, result, tc.expected)
		}
	}
	
	// Test MapFields
	fields := map[string]interface{}{
		"metadata.uid":   "test-uid",
		"metadata.name":  "test-name",
		"spec.replicas":  3,
		"status.phase":   "Running",
	}
	
	expected := map[string]interface{}{
		"uid":            "test-uid",
		"name":           "test-name",
		"desired_count":  3,
		"status.phase":   "Running",
	}
	
	result := chained.MapFields(fields)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("MapFields result mismatch.\nExpected: %+v\nActual: %+v", expected, result)
	}
}

func TestFieldMapperEdgeCases(t *testing.T) {
	mapper := &MetadataPrefixMapper{}
	
	// Test empty map
	result := mapper.MapFields(map[string]interface{}{})
	if result == nil || len(result) != 0 {
		t.Error("MapFields on empty map should return empty map")
	}
	
	// Test map with nil values
	fields := map[string]interface{}{
		"metadata.uid":  nil,
		"metadata.name": "",
		"spec.replicas": 0,
	}
	
	expected := map[string]interface{}{
		"uid":           nil,
		"name":          "",
		"spec.replicas": 0,
	}
	
	result = mapper.MapFields(fields)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("MapFields with nil values result mismatch.\nExpected: %+v\nActual: %+v", expected, result)
	}
	
	// Test complex values
	complexFields := map[string]interface{}{
		"metadata.labels": map[string]string{"app": "test"},
		"spec.containers": []interface{}{
			map[string]interface{}{"name": "container1"},
		},
	}
	
	expectedComplex := map[string]interface{}{
		"labels": map[string]string{"app": "test"},
		"spec.containers": []interface{}{
			map[string]interface{}{"name": "container1"},
		},
	}
	
	result = mapper.MapFields(complexFields)
	if !reflect.DeepEqual(result, expectedComplex) {
		t.Errorf("MapFields with complex values result mismatch.\nExpected: %+v\nActual: %+v", expectedComplex, result)
	}
}