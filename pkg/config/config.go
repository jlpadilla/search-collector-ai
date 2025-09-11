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
	KubeConfig string `json:"kubeconfig,omitempty"`
	InCluster  bool   `json:"inCluster"`

	// Resource discovery configuration
	UseDiscovery   bool             `json:"useDiscovery"`   // Whether to use discovery to find all resources
	Resources      []ResourceConfig `json:"resources"`      // Manually configured resources
	DiscoveredOnly bool             `json:"discoveredOnly"` // If true, only use discovered resources

	// Informer configuration
	BufferSize   int           `json:"bufferSize"`
	ResyncPeriod time.Duration `json:"resyncPeriod"`

	// Transformer configuration
	TransformerType       string   `json:"transformerType"`     // Type of transformer to use
	TransformConfigFile   string   `json:"transformConfigFile"` // Path to transformer config file
	ExtractFields         []string `json:"extractFields"`
	DiscoverRelationships bool     `json:"discoverRelationships"`
	IncludeLabels         bool     `json:"includeLabels"`
	IncludeAnnotations    bool     `json:"includeAnnotations"`

	// Reconciler configuration
	ReconcilerCleanupInterval   time.Duration `json:"reconcilerCleanupInterval"`
	ReconcilerDeletedRetention  time.Duration `json:"reconcilerDeletedRetention"`
	ReconcilerMemoryThresholdMB float64       `json:"reconcilerMemoryThresholdMB"`

	// Search indexer configuration
	IndexerURL     string        `json:"indexerURL"`
	IndexerTimeout time.Duration `json:"indexerTimeout"`
	IndexerAPIKey  string        `json:"indexerAPIKey,omitempty"`

	// TLS configuration for search indexer
	TLSInsecureSkipVerify bool   `json:"tlsInsecureSkipVerify,omitempty"`
	TLSCACertFile         string `json:"tlsCACertFile,omitempty"`
	TLSClientCertFile     string `json:"tlsClientCertFile,omitempty"`
	TLSClientKeyFile      string `json:"tlsClientKeyFile,omitempty"`
	TLSServerName         string `json:"tlsServerName,omitempty"`
	
	// Search indexer specific configuration
	OverwriteState bool `json:"overwriteState,omitempty"`

	// Sender configuration
	SenderBatchSize           int           `json:"senderBatchSize"`
	SenderBatchTimeout        time.Duration `json:"senderBatchTimeout"`
	SenderSendInterval        time.Duration `json:"senderSendInterval"`
	SenderMaxRetries          int           `json:"senderMaxRetries"`
	SenderRetryDelay          time.Duration `json:"senderRetryDelay"`
	SenderMaxResourcesPerSync int           `json:"senderMaxResourcesPerSync"`

	// Status server configuration
	StatusServerEnabled bool   `json:"statusServerEnabled"`
	StatusServerAddr    string `json:"statusServerAddr"`

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
		InCluster:                   false,
		UseDiscovery:                true,  // Enable discovery by default
		DiscoveredOnly:              false, // Use both discovered and manual resources
		BufferSize:                  1000,
		ResyncPeriod:                10 * time.Minute,
		TransformerType:             "configurable",           // Use configurable transformer by default
		TransformConfigFile:         "configs/transform.json", // Default config file path
		DiscoverRelationships:       true,
		IncludeLabels:               true,
		IncludeAnnotations:          false,
		ReconcilerCleanupInterval:   10 * time.Minute,
		ReconcilerDeletedRetention:  1 * time.Hour,
		ReconcilerMemoryThresholdMB: 512.0,
		IndexerURL:                  "https://localhost:3010/aggregator/clusters/local-ai/sync",
		IndexerTimeout:              30 * time.Second,
		TLSInsecureSkipVerify:       true,
		SenderBatchSize:             100,
		SenderBatchTimeout:          5 * time.Second,
		SenderSendInterval:          10 * time.Second,
		SenderMaxRetries:            3,
		SenderRetryDelay:            1 * time.Second,
		SenderMaxResourcesPerSync:   1000,
		StatusServerEnabled:         true,
		StatusServerAddr:            ":8080",
		LogLevel:                    2,
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
			"TypeMeta.Kind",
			"metadata.uid",
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
