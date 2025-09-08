package config

import (
	"time"

	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

// Config holds the application configuration
type Config struct {
	// Kubernetes client configuration
	KubeConfig    string `json:"kubeconfig,omitempty"`
	InCluster     bool   `json:"inCluster"`
	
	// Resource discovery configuration
	UseDiscovery     bool             `json:"useDiscovery"`     // Whether to use discovery to find all resources
	Resources        []ResourceConfig `json:"resources"`        // Manually configured resources
	DiscoveredOnly   bool             `json:"discoveredOnly"`   // If true, only use discovered resources
	
	// Informer configuration
	BufferSize   int           `json:"bufferSize"`
	ResyncPeriod time.Duration `json:"resyncPeriod"`
	
	// Transformer configuration
	ExtractFields         []string `json:"extractFields"`
	DiscoverRelationships bool     `json:"discoverRelationships"`
	IncludeLabels         bool     `json:"includeLabels"`
	IncludeAnnotations    bool     `json:"includeAnnotations"`
	
	// Search indexer configuration
	IndexerURL string `json:"indexerURL"`
	
	// Logging
	LogLevel int `json:"logLevel"`
}

// ResourceConfig defines which resources to watch
type ResourceConfig struct {
	Group         string `json:"group"`
	Version       string `json:"version"`
	Resource      string `json:"resource"`
	Namespace     string `json:"namespace,omitempty"`
	LabelSelector string `json:"labelSelector,omitempty"`
	FieldSelector string `json:"fieldSelector,omitempty"`
}

// ToGVR converts ResourceConfig to GroupVersionResource
func (rc *ResourceConfig) ToGVR() schema.GroupVersionResource {
	return schema.GroupVersionResource{
		Group:    rc.Group,
		Version:  rc.Version,
		Resource: rc.Resource,
	}
}

// DefaultConfig returns a default configuration
func DefaultConfig() *Config {
	return &Config{
		InCluster:             false,
		UseDiscovery:          true,  // Enable discovery by default
		DiscoveredOnly:        false, // Use both discovered and manual resources
		BufferSize:            1000,
		ResyncPeriod:          10 * time.Minute,
		DiscoverRelationships: true,
		IncludeLabels:         true,
		IncludeAnnotations:    false,
		LogLevel:              2,
		Resources: []ResourceConfig{
			// Core resources - these will be supplemented by discovery
			{Group: "", Version: "v1", Resource: "pods"},
			{Group: "", Version: "v1", Resource: "services"},
			{Group: "", Version: "v1", Resource: "configmaps"},
			{Group: "", Version: "v1", Resource: "secrets"},
			{Group: "", Version: "v1", Resource: "persistentvolumes"},
			{Group: "", Version: "v1", Resource: "persistentvolumeclaims"},
			{Group: "", Version: "v1", Resource: "nodes"},
			{Group: "", Version: "v1", Resource: "namespaces"},
			
			// Apps resources
			{Group: "apps", Version: "v1", Resource: "deployments"},
			{Group: "apps", Version: "v1", Resource: "replicasets"},
			{Group: "apps", Version: "v1", Resource: "daemonsets"},
			{Group: "apps", Version: "v1", Resource: "statefulsets"},
			
			// Extensions/Networking
			{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
			{Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies"},
		},
		ExtractFields: []string{
			"metadata.name",
			"metadata.namespace",
			"metadata.labels",
			"spec.selector",
			"status.phase",
			"status.conditions",
		},
	}
}

// GetKubernetesConfig creates a Kubernetes client configuration
func (c *Config) GetKubernetesConfig() (*rest.Config, error) {
	if c.InCluster {
		klog.Info("Using in-cluster configuration")
		return rest.InClusterConfig()
	}
	
	if c.KubeConfig != "" {
		klog.Infof("Using kubeconfig from: %s", c.KubeConfig)
		return clientcmd.BuildConfigFromFlags("", c.KubeConfig)
	}
	
	klog.Info("Using default kubeconfig")
	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
}