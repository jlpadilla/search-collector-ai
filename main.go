package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jlpadilla/search-collector-ai/pkg/config"
	"github.com/jlpadilla/search-collector-ai/pkg/discovery"
	"github.com/jlpadilla/search-collector-ai/pkg/handler"
	"github.com/jlpadilla/search-collector-ai/pkg/informer"
	"github.com/jlpadilla/search-collector-ai/pkg/reconciler"
	"github.com/jlpadilla/search-collector-ai/pkg/status"
	"github.com/jlpadilla/search-collector-ai/pkg/transformer"
	k8sdiscovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
)

func main() {
	// Parse command line flags
	klog.InitFlags(nil)
	flag.Parse()
	
	fmt.Println("Search Collector AI starting...")
	klog.Info("Initializing Search Collector AI")
	
	// Load configuration
	cfg := config.DefaultConfig()
	
	// Set up Kubernetes client
	restConfig, err := cfg.GetKubernetesConfig()
	if err != nil {
		klog.Fatalf("Failed to get Kubernetes config: %v", err)
	}
	
	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		klog.Fatalf("Failed to create dynamic client: %v", err)
	}
	
	// Create discovery client
	discoveryClient, err := k8sdiscovery.NewDiscoveryClientForConfig(restConfig)
	if err != nil {
		klog.Fatalf("Failed to create discovery client: %v", err)
	}
	
	// Create transformer
	transformConfig := &transformer.TransformConfig{
		TransformerType:       cfg.TransformerType,
		ConfigFile:            cfg.TransformConfigFile,
		ExtractFields:         cfg.ExtractFields,
		DiscoverRelationships: cfg.DiscoverRelationships,
		IncludeLabels:         cfg.IncludeLabels,
		IncludeAnnotations:    cfg.IncludeAnnotations,
	}
	
	// Create transformer using registry
	t, err := transformer.CreateTransformer(cfg.TransformerType, transformConfig)
	if err != nil {
		klog.Warningf("Failed to create %s transformer: %v, using default", cfg.TransformerType, err)
		t = transformer.GetDefaultTransformer(transformConfig)
	}
	klog.Infof("Using transformer type: %s", cfg.TransformerType)
	
	// Create reconciler
	reconcilerConfig := reconciler.DefaultReconcilerConfig()
	reconcilerConfig.CleanupInterval = cfg.ReconcilerCleanupInterval
	reconcilerConfig.DeletedRetention = cfg.ReconcilerDeletedRetention
	reconcilerConfig.MemoryThresholdMB = cfg.ReconcilerMemoryThresholdMB
	r := reconciler.NewMemoryReconciler(reconcilerConfig)
	
	// Start reconciler
	if err := r.Start(); err != nil {
		klog.Fatalf("Failed to start reconciler: %v", err)
	}
	defer r.Stop()
	
	// Create event handler
	eventHandler := handler.NewTransformHandler(t, r)
	
	// Create informer manager
	manager := informer.NewManager(dynamicClient)
	
	// Discover resources if enabled
	var resourcesToWatch []config.ResourceConfig
	if cfg.UseDiscovery {
		resourceDiscovery := discovery.NewResourceDiscovery(discoveryClient)
		discoveredResources, err := resourceDiscovery.DiscoverAllResources()
		if err != nil {
			klog.Errorf("Failed to discover resources: %v", err)
			if cfg.DiscoveredOnly {
				klog.Fatalf("Discovery failed and DiscoveredOnly is true")
			}
			// Fall back to configured resources
			resourcesToWatch = cfg.Resources
		} else {
			if cfg.DiscoveredOnly {
				resourcesToWatch = discoveredResources
			} else {
				// Merge discovered and configured resources
				resourcesToWatch = mergeResources(cfg.Resources, discoveredResources)
			}
		}
	} else {
		resourcesToWatch = cfg.Resources
	}
	
	klog.Infof("Watching %d resource types", len(resourcesToWatch))
	
	// Add informers for each resource type to watch
	for _, resource := range resourcesToWatch {
		informerConfig := &informer.InformerConfig{
			Namespace:     resource.Namespace,
			FieldSelector: resource.FieldSelector,
			LabelSelector: resource.LabelSelector,
			BufferSize:    cfg.BufferSize,
		}
		
		gvr := resource.ToGVR()
		if err := manager.AddInformer(gvr, informerConfig, eventHandler); err != nil {
			klog.Errorf("Failed to add informer for %s: %v", gvr.String(), err)
			continue
		}
		
		klog.Infof("Added informer for resource: %s", gvr.String())
	}
	
	// Set up context with cancellation
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	
	// Set up signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	
	// Start status server if enabled
	if cfg.StatusServerEnabled {
		statusHandler := status.NewStatusHandler(r)
		go func() {
			klog.Infof("Status server available at http://localhost%s", cfg.StatusServerAddr)
			if err := status.StartStatusServer(cfg.StatusServerAddr, statusHandler); err != nil {
				klog.Errorf("Status server failed: %v", err)
			}
		}()
	}
	
	// Start the informer manager
	klog.Info("Starting informer manager...")
	if err := manager.Start(ctx); err != nil {
		klog.Fatalf("Failed to start informer manager: %v", err)
	}
	
	klog.Info("Search Collector AI is running. Press Ctrl+C to stop.")
	if cfg.StatusServerEnabled {
		klog.Infof("Status endpoint: http://localhost%s/status", cfg.StatusServerAddr)
	}
	
	// Wait for shutdown signal
	<-sigCh
	klog.Info("Shutdown signal received, stopping...")
	
	// Cancel context to stop informers
	cancel()
	
	// Stop the manager
	manager.Stop()
	
	// Give some time for graceful shutdown
	time.Sleep(2 * time.Second)
	
	klog.Info("Search Collector AI stopped")
}

// mergeResources merges configured and discovered resources, avoiding duplicates
func mergeResources(configured, discovered []config.ResourceConfig) []config.ResourceConfig {
	resourceMap := make(map[string]config.ResourceConfig)
	
	// Add all configured resources first
	for _, resource := range configured {
		key := fmt.Sprintf("%s/%s/%s", resource.Group, resource.Version, resource.Resource)
		resourceMap[key] = resource
	}
	
	// Add discovered resources, but don't override configured ones
	for _, resource := range discovered {
		key := fmt.Sprintf("%s/%s/%s", resource.Group, resource.Version, resource.Resource)
		if _, exists := resourceMap[key]; !exists {
			resourceMap[key] = resource
		}
	}
	
	// Convert back to slice
	var merged []config.ResourceConfig
	for _, resource := range resourceMap {
		merged = append(merged, resource)
	}
	
	return merged
}