package sender

import (
	"fmt"
	"strings"

	"github.com/jlpadilla/search-collector-ai/pkg/reconciler"
	"github.com/jlpadilla/search-collector-ai/pkg/transformer"
	"k8s.io/klog/v2"
)

// convertToSyncEvents converts reconciler resource states to sync events
func (s *HTTPSender) convertToSyncEvents(resources []*reconciler.ResourceState) ([]*SyncEvent, error) {
	if len(resources) == 0 {
		return nil, nil
	}
	
	var syncEvents []*SyncEvent
	currentEvent := &SyncEvent{}
	currentResourceCount := 0
	
	for _, resourceState := range resources {
		// Convert resource state to appropriate sync event components
		if resourceState.IsDeleted {
			// Handle deleted resources
			deleteEvent := s.convertToDeleteEvent(resourceState)
			if deleteEvent != nil {
				currentEvent.DeleteResources = append(currentEvent.DeleteResources, *deleteEvent)
				currentResourceCount++
			}
		} else {
			// Handle added/updated resources
			resource := s.convertToResource(resourceState)
			if resource != nil {
				// Determine if this is an add or update based on update count
				if resourceState.UpdateCount <= 1 {
					currentEvent.AddResources = append(currentEvent.AddResources, *resource)
				} else {
					currentEvent.UpdateResources = append(currentEvent.UpdateResources, *resource)
				}
				currentResourceCount++
				
				// Add relationships as edges
				edges := s.convertRelationshipsToEdges(resourceState.TransformedResource)
				currentEvent.AddEdges = append(currentEvent.AddEdges, edges...)
			}
		}
		
		// Check if we need to create a new sync event (batch size limit)
		if currentResourceCount >= s.config.MaxResourcesPerSync {
			syncEvents = append(syncEvents, currentEvent)
			currentEvent = &SyncEvent{}
			currentResourceCount = 0
		}
	}
	
	// Add the last event if it has any resources
	if currentResourceCount > 0 {
		syncEvents = append(syncEvents, currentEvent)
	}
	
	// If no events were created, return an empty event
	if len(syncEvents) == 0 {
		syncEvents = append(syncEvents, &SyncEvent{})
	}
	
	return syncEvents, nil
}

// convertToResource converts a reconciler resource state to a Resource for the sync event
func (s *HTTPSender) convertToResource(resourceState *reconciler.ResourceState) *Resource {
	if resourceState.TransformedResource == nil {
		klog.V(4).Infof("Skipping resource %s: no transformed resource data", resourceState.ResourceKey)
		return nil
	}
	
	tr := resourceState.TransformedResource
	
	// Get UID from various sources
	uid := s.extractUID(tr)
	if uid == "" {
		klog.Errorf("Resource %s has no UID and fallback generation failed, skipping", resourceState.ResourceKey)
		return nil
	}
	
	// Build resource string (typically namespace/name format)
	resourceString := s.buildResourceString(tr)
	
	// Build properties map by combining fields, labels, and metadata
	properties := s.buildProperties(tr)
	
	resource := &Resource{
		Kind:           s.normalizeKind(tr.ResourceType),
		UID:            uid,
		ResourceString: resourceString,
		Properties:     properties,
	}
	
	klog.V(4).Infof("Converted resource %s (kind: %s, uid: %s) with %d properties",
		resourceState.ResourceKey, resource.Kind, resource.UID, len(resource.Properties))
	
	return resource
}

// convertToDeleteEvent converts a deleted resource state to a DeleteResourceEvent
func (s *HTTPSender) convertToDeleteEvent(resourceState *reconciler.ResourceState) *DeleteResourceEvent {
	if resourceState.TransformedResource == nil {
		klog.V(4).Infof("Skipping deleted resource %s: no transformed resource data", resourceState.ResourceKey)
		return nil
	}
	
	tr := resourceState.TransformedResource
	uid := s.extractUID(tr)
	if uid == "" {
		klog.Errorf("Deleted resource %s has no UID and fallback generation failed, skipping", resourceState.ResourceKey)
		return nil
	}
	
	deleteEvent := &DeleteResourceEvent{
		UID:  uid,
		Kind: s.normalizeKind(tr.ResourceType),
	}
	
	klog.V(4).Infof("Converted delete event for resource %s (kind: %s, uid: %s)",
		resourceState.ResourceKey, deleteEvent.Kind, deleteEvent.UID)
	
	return deleteEvent
}

// convertRelationshipsToEdges converts transformer relationships to edges
func (s *HTTPSender) convertRelationshipsToEdges(tr *transformer.TransformedResource) []Edge {
	if tr == nil || len(tr.Relationships) == 0 {
		return nil
	}
	
	var edges []Edge
	sourceUID := s.extractUID(tr)
	if sourceUID == "" {
		return nil
	}
	
	sourceKind := s.normalizeKind(tr.ResourceType)
	
	for _, rel := range tr.Relationships {
		// Extract target UID from relationship
		targetUID := s.extractTargetUID(rel)
		if targetUID == "" {
			continue
		}
		
		edge := Edge{
			SourceUID:  sourceUID,
			DestUID:    targetUID,
			EdgeType:   rel.Type,
			SourceKind: sourceKind,
			DestKind:   s.normalizeKind(rel.TargetType),
		}
		
		edges = append(edges, edge)
	}
	
	return edges
}

// extractUID extracts the UID from various sources in the transformed resource
func (s *HTTPSender) extractUID(tr *transformer.TransformedResource) string {
	// Try to get UID from fields first
	if tr.Fields != nil {
		if uid, exists := tr.Fields["metadata.uid"]; exists {
			if uidStr, ok := uid.(string); ok && uidStr != "" {
				return uidStr
			}
		}
		if uid, exists := tr.Fields["uid"]; exists {
			if uidStr, ok := uid.(string); ok && uidStr != "" {
				return uidStr
			}
		}
	}
	
	// Generate a fallback UID if none exists - this should rarely happen with the improved transformer
	// but provides a safety net to prevent the converter from failing
	if tr.ResourceKey != "" {
		klog.V(4).Infof("Generating fallback UID for resource %s (resourceKey: %s)", tr.Name, tr.ResourceKey)
		return fmt.Sprintf("fallback-%s-%s", tr.ResourceType, tr.ResourceKey)
	}
	
	// Last resort: use resource type and name
	if tr.Name != "" {
		klog.V(4).Infof("Generating last-resort UID for resource %s", tr.Name)
		return fmt.Sprintf("lastresort-%s-%s", tr.ResourceType, tr.Name)
	}
	
	return ""
}

// extractTargetUID extracts the target UID from a relationship
func (s *HTTPSender) extractTargetUID(rel transformer.ResourceRelationship) string {
	// For now, we'll use a combination of namespace, name, and type to generate a pseudo-UID
	// In a real implementation, you might need to look up the actual UID from the reconciler
	if rel.TargetName != "" {
		if rel.Namespace != "" {
			return fmt.Sprintf("%s/%s/%s", rel.Namespace, rel.TargetName, rel.TargetType)
		}
		return fmt.Sprintf("%s/%s", rel.TargetName, rel.TargetType)
	}
	return rel.TargetKey
}

// buildResourceString creates a resource string for the search indexer
func (s *HTTPSender) buildResourceString(tr *transformer.TransformedResource) string {
	if tr.Namespace != "" && tr.Name != "" {
		return fmt.Sprintf("%s/%s", tr.Namespace, tr.Name)
	}
	if tr.Name != "" {
		return tr.Name
	}
	return tr.ResourceKey
}

// buildProperties builds the properties map from transformed resource data
func (s *HTTPSender) buildProperties(tr *transformer.TransformedResource) map[string]interface{} {
	properties := make(map[string]interface{})
	
	// Add basic metadata
	if tr.Name != "" {
		properties["name"] = tr.Name
	}
	if tr.Namespace != "" {
		properties["namespace"] = tr.Namespace
	}
	if tr.APIVersion != "" {
		properties["apiVersion"] = tr.APIVersion
	}
	if tr.CreatedAt != "" {
		properties["createdAt"] = tr.CreatedAt
	}
	if tr.UpdatedAt != "" {
		properties["updatedAt"] = tr.UpdatedAt
	}
	
	// Add all fields from the transformer
	if tr.Fields != nil {
		for key, value := range tr.Fields {
			// Flatten nested field names (replace dots with underscores for indexing)
			flatKey := strings.ReplaceAll(key, ".", "_")
			properties[flatKey] = value
		}
	}
	
	// Add labels with a prefix to avoid conflicts
	if tr.Labels != nil && len(tr.Labels) > 0 {
		for key, value := range tr.Labels {
			properties["label_"+key] = value
		}
		// Also add all labels as a single field for search
		properties["labels"] = tr.Labels
	}
	
	// Add annotations with a prefix (if they exist and are not too large)
	if tr.Annotations != nil && len(tr.Annotations) > 0 {
		// Only include smaller annotations to avoid bloating the index
		filteredAnnotations := make(map[string]string)
		for key, value := range tr.Annotations {
			if len(value) < 1000 { // Arbitrary limit
				filteredAnnotations[key] = value
			}
		}
		if len(filteredAnnotations) > 0 {
			properties["annotations"] = filteredAnnotations
		}
	}
	
	// Add relationship count for search/filtering
	if len(tr.Relationships) > 0 {
		properties["relationship_count"] = len(tr.Relationships)
		
		// Add relationship types for search
		relTypes := make([]string, 0, len(tr.Relationships))
		for _, rel := range tr.Relationships {
			relTypes = append(relTypes, rel.Type)
		}
		properties["relationship_types"] = relTypes
	}
	
	return properties
}

// normalizeKind normalizes Kubernetes resource kinds for consistency
func (s *HTTPSender) normalizeKind(resourceType string) string {
	// Convert resource type (plural, lowercase) to Kind (singular, PascalCase)
	kindMap := map[string]string{
		"pods":                     "Pod",
		"services":                 "Service",
		"deployments":              "Deployment",
		"replicasets":              "ReplicaSet",
		"daemonsets":               "DaemonSet",
		"statefulsets":             "StatefulSet",
		"configmaps":               "ConfigMap",
		"secrets":                  "Secret",
		"persistentvolumes":        "PersistentVolume",
		"persistentvolumeclaims":   "PersistentVolumeClaim",
		"nodes":                    "Node",
		"namespaces":               "Namespace",
		"ingresses":                "Ingress",
		"networkpolicies":          "NetworkPolicy",
		"customresourcedefinitions": "CustomResourceDefinition",
		"roles":                    "Role",
		"rolebindings":             "RoleBinding",
		"clusterroles":             "ClusterRole",
		"clusterrolebindings":      "ClusterRoleBinding",
		"serviceaccounts":          "ServiceAccount",
	}
	
	if kind, exists := kindMap[resourceType]; exists {
		return kind
	}
	
	// For unknown types, try to convert to PascalCase
	if resourceType != "" {
		// Remove 's' from the end if it exists (simple pluralization)
		singular := strings.TrimSuffix(resourceType, "s")
		// Capitalize first letter
		if len(singular) > 0 {
			return strings.ToUpper(singular[:1]) + singular[1:]
		}
	}
	
	return resourceType
}