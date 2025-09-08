package discovery

import (
	"fmt"
	"strings"

	"github.com/jlpadilla/search-collector-ai/pkg/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/klog/v2"
)

// ResourceDiscovery handles discovery of all available Kubernetes resources
type ResourceDiscovery struct {
	discoveryClient discovery.DiscoveryInterface
}

// NewResourceDiscovery creates a new resource discovery client
func NewResourceDiscovery(discoveryClient discovery.DiscoveryInterface) *ResourceDiscovery {
	return &ResourceDiscovery{
		discoveryClient: discoveryClient,
	}
}

// DiscoverAllResources discovers all available resources in the cluster
func (rd *ResourceDiscovery) DiscoverAllResources() ([]config.ResourceConfig, error) {
	klog.Info("Discovering all available resources in the cluster...")
	
	// Get all API groups and resources
	apiGroups, err := rd.discoveryClient.ServerGroups()
	if err != nil {
		return nil, fmt.Errorf("failed to get server groups: %w", err)
	}
	
	var resources []config.ResourceConfig
	
	// Process core group (empty group name)
	coreResources, err := rd.discoverGroupResources("", "v1")
	if err != nil {
		klog.Warningf("Failed to discover core resources: %v", err)
	} else {
		resources = append(resources, coreResources...)
	}
	
	// Process all other API groups
	for _, group := range apiGroups.Groups {
		if len(group.Versions) == 0 {
			continue
		}
		
		// Use the preferred version
		preferredVersion := group.PreferredVersion.Version
		groupResources, err := rd.discoverGroupResources(group.Name, preferredVersion)
		if err != nil {
			klog.Warningf("Failed to discover resources for group %s/%s: %v", group.Name, preferredVersion, err)
			continue
		}
		
		resources = append(resources, groupResources...)
	}
	
	// Filter out non-watchable resources
	watchableResources := rd.filterWatchableResources(resources)
	
	klog.Infof("Discovered %d total resources, %d are watchable", len(resources), len(watchableResources))
	
	return watchableResources, nil
}

// discoverGroupResources discovers resources for a specific API group and version
func (rd *ResourceDiscovery) discoverGroupResources(groupName, version string) ([]config.ResourceConfig, error) {
	var gv schema.GroupVersion
	if groupName == "" {
		gv = schema.GroupVersion{Group: "", Version: version}
	} else {
		gv = schema.GroupVersion{Group: groupName, Version: version}
	}
	
	resourceList, err := rd.discoveryClient.ServerResourcesForGroupVersion(gv.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get resources for %s: %w", gv.String(), err)
	}
	
	var resources []config.ResourceConfig
	
	for _, resource := range resourceList.APIResources {
		// Skip subresources (those with '/' in the name)
		if strings.Contains(resource.Name, "/") {
			continue
		}
		
		// Skip resources that don't support list and watch
		if !rd.supportsListAndWatch(resource.Verbs) {
			continue
		}
		
		resourceConfig := config.ResourceConfig{
			Group:    groupName,
			Version:  version,
			Resource: resource.Name,
		}
		
		resources = append(resources, resourceConfig)
		klog.V(4).Infof("Discovered resource: %s", resourceConfig.ToGVR().String())
	}
	
	return resources, nil
}

// supportsListAndWatch checks if a resource supports list and watch operations
func (rd *ResourceDiscovery) supportsListAndWatch(verbs metav1.Verbs) bool {
	haslist := false
	hasWatch := false
	
	for _, verb := range verbs {
		switch verb {
		case "list":
			haslist = true
		case "watch":
			hasWatch = true
		}
	}
	
	return haslist && hasWatch
}

// filterWatchableResources filters out resources that shouldn't be watched
func (rd *ResourceDiscovery) filterWatchableResources(resources []config.ResourceConfig) []config.ResourceConfig {
	var filtered []config.ResourceConfig
	
	// Resources to exclude from watching
	excludeResources := map[string]bool{
		// Events are too noisy and short-lived
		"events": true,
		
		// Leases generate too many update events
		"leases": true,
		
		// Metrics and monitoring resources
		"metrics": true,
		"metricses": true,
		
		// API discovery resources
		"apiservices": true,
		
		// Certificate signing requests (often transient)
		"certificatesigningrequests": true,
		
		// Token requests (security sensitive and transient)
		"tokenrequests": true,
		"tokenreviews": true,
		
		// Subject access reviews (transient)
		"subjectaccessreviews": true,
		"localsubjectaccessreviews": true,
		"selfsubjectaccessreviews": true,
		"selfsubjectrulesreviews": true,
		
		// Binding resources (internal)
		"bindings": true,
		
		// Node proxy (internal)
		"nodes/proxy": true,
		"services/proxy": true,
		"pods/proxy": true,
	}
	
	// Groups to exclude entirely
	excludeGroups := map[string]bool{
		"metrics.k8s.io":         true, // Metrics API
		"custom.metrics.k8s.io":  true, // Custom metrics
		"external.metrics.k8s.io": true, // External metrics
		"apiregistration.k8s.io": true, // API registration (internal)
	}
	
	for _, resource := range resources {
		// Skip excluded groups
		if excludeGroups[resource.Group] {
			klog.V(4).Infof("Excluding resource from group %s: %s", resource.Group, resource.Resource)
			continue
		}
		
		// Skip excluded resources
		if excludeResources[resource.Resource] {
			klog.V(4).Infof("Excluding resource: %s", resource.Resource)
			continue
		}
		
		// Skip resources with certain patterns
		if strings.HasSuffix(resource.Resource, "/status") ||
			strings.HasSuffix(resource.Resource, "/scale") ||
			strings.HasSuffix(resource.Resource, "/proxy") {
			klog.V(4).Infof("Excluding subresource: %s", resource.Resource)
			continue
		}
		
		filtered = append(filtered, resource)
	}
	
	return filtered
}