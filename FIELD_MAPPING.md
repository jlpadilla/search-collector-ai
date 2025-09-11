# Field Mapping Configuration

The Search Collector AI now supports configurable field mapping to transform field names during indexing. This allows for cleaner field names in the search index while maintaining backward compatibility.

## Configuration Options

Field mapping is configured through the `FieldMapping` section of the `TransformConfig`:

```go
FieldMapping: FieldMappingConfig{
    Type: "metadata-prefix-removal", // or "none", "custom"
    EnableMetadataPrefixRemoval: false, // backward compatibility flag
    CustomMappings: map[string]string{
        "metadata.uid": "resource_id",
        "spec.replicas": "desired_count",
    },
}
```

## Mapping Types

### 1. None (Default)
**Type:** `"none"`
**Behavior:** No field mapping is applied. Field names remain exactly as configured.

```go
FieldMapping: FieldMappingConfig{
    Type: "none",
}
```

**Example:**
- `metadata.uid` → `metadata.uid`
- `metadata.name` → `metadata.name`
- `spec.containers` → `spec.containers`

### 2. Metadata Prefix Removal
**Type:** `"metadata-prefix-removal"`
**Behavior:** Removes "metadata." prefix from field names for cleaner indexing.

```go
FieldMapping: FieldMappingConfig{
    Type: "metadata-prefix-removal",
}
```

**Example:**
- `metadata.uid` → `uid`
- `metadata.name` → `name`
- `metadata.namespace` → `namespace`
- `spec.containers` → `spec.containers` (unchanged)

### 3. Custom Mapping
**Type:** `"custom"`
**Behavior:** Applies user-defined field name mappings.

```go
FieldMapping: FieldMappingConfig{
    Type: "custom",
    CustomMappings: map[string]string{
        "metadata.uid":    "resource_id",
        "metadata.name":   "resource_name",
        "spec.replicas":   "desired_count",
        "status.phase":    "current_phase",
    },
}
```

**Example:**
- `metadata.uid` → `resource_id`
- `metadata.name` → `resource_name`
- `spec.replicas` → `desired_count`
- `status.phase` → `current_phase`
- `metadata.namespace` → `metadata.namespace` (not in mappings, unchanged)

## Chained Mapping

You can combine custom mappings with metadata prefix removal:

```go
FieldMapping: FieldMappingConfig{
    Type: "custom",
    EnableMetadataPrefixRemoval: true,
    CustomMappings: map[string]string{
        "spec.replicas": "desired_count",
    },
}
```

This applies custom mappings first, then metadata prefix removal:
- `metadata.uid` → `uid` (metadata prefix removed)
- `metadata.name` → `name` (metadata prefix removed)
- `spec.replicas` → `desired_count` (custom mapping)
- `spec.containers` → `spec.containers` (unchanged)

## Backward Compatibility

The default configuration ensures **complete backward compatibility**:

```go
FieldMapping: FieldMappingConfig{
    Type: "none", // No field mapping by default
}
```

For users who want to opt-in to metadata prefix removal for existing configurations:

```go
FieldMapping: FieldMappingConfig{
    Type: "none",
    EnableMetadataPrefixRemoval: true, // Enables metadata prefix removal
}
```

## Configuration Examples

### Example 1: Clean Metadata Fields
Perfect for new deployments wanting cleaner field names:

```json
{
  "transformerType": "base",
  "extractFields": [
    "metadata.uid",
    "metadata.name", 
    "metadata.namespace",
    "metadata.labels",
    "spec.containers",
    "status.phase"
  ],
  "fieldMapping": {
    "type": "metadata-prefix-removal"
  }
}
```

**Result:** `uid`, `name`, `namespace`, `labels`, `spec.containers`, `status.phase`

### Example 2: Custom Business Logic Fields
For organizations with specific naming conventions:

```json
{
  "transformerType": "base",
  "extractFields": [
    "metadata.uid",
    "metadata.name",
    "spec.replicas",
    "status.readyReplicas"
  ],
  "fieldMapping": {
    "type": "custom",
    "customMappings": {
      "metadata.uid": "resource_id",
      "metadata.name": "service_name",
      "spec.replicas": "desired_instances",
      "status.readyReplicas": "available_instances"
    }
  }
}
```

**Result:** `resource_id`, `service_name`, `desired_instances`, `available_instances`

### Example 3: Hybrid Approach
Combines custom mappings with metadata cleanup:

```json
{
  "transformerType": "base", 
  "extractFields": [
    "metadata.uid",
    "metadata.name",
    "metadata.namespace",
    "spec.replicas",
    "spec.containers"
  ],
  "fieldMapping": {
    "type": "custom",
    "enableMetadataPrefixRemoval": true,
    "customMappings": {
      "spec.replicas": "instance_count",
      "spec.containers": "container_specs"
    }
  }
}
```

**Result:** `uid`, `name`, `namespace`, `instance_count`, `container_specs`

## Migration Guide

### Existing Users (No Changes Required)
The default behavior preserves all existing field names. No configuration changes are needed.

### Users Wanting Cleaner Fields
To enable metadata prefix removal for existing configurations:

1. Add field mapping configuration:
```go
FieldMapping: FieldMappingConfig{
    Type: "metadata-prefix-removal",
}
```

2. Update any downstream systems that reference metadata fields:
   - `metadata.uid` → `uid`
   - `metadata.name` → `name`
   - etc.

## Testing

Comprehensive test coverage ensures reliability:

```bash
# Run field mapping tests
go test ./pkg/transformer/ -v -run TestFieldMapper

# Run integration tests  
go test ./pkg/transformer/ -v -run TestTransformer

# Test backward compatibility
go test ./pkg/transformer/ -v -run TestTransformerBackwardCompatibility
```

## Performance Impact

Field mapping has minimal performance impact:
- **NoOp Mapper:** Zero overhead
- **Metadata Prefix Mapper:** Simple string operations
- **Custom Mapper:** Map lookup per field
- **Memory:** No additional memory allocation for field values

Field mapping occurs after extraction but before indexing, maintaining the separation of concerns in the architecture.