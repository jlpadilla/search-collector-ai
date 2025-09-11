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
	"github.com/jlpadilla/search-collector-ai/pkg/sender"
	"github.com/jlpadilla/search-collector-ai/pkg/status"
	"github.com/jlpadilla/search-collector-ai/pkg/transformer"
	k8sdiscovery "k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
)

func main() {
	// Parse command line flags
	var diagnose = flag.Bool("diagnose", false, "Run diagnostic tests and exit")
	klog.InitFlags(nil)
	flag.Parse()
	
	// Load configuration
	cfg := config.DefaultConfig()
	
	// Run diagnostics if requested
	if *diagnose {
		runDiagnostics(cfg)
		return
	}
	
	fmt.Println("Search Collector AI starting...")
	klog.Info("Initializing Search Collector AI")
	
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
		DiscoverRelationships: cfg.DiscoverRelationships,
		IncludeLabels:         cfg.IncludeLabels,
		IncludeAnnotations:    cfg.IncludeAnnotations,
		FieldMapping: transformer.FieldMappingConfig{
			Type: "none", // Default: no field mapping for backward compatibility
		},
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
	
	// Create sender
	senderConfig := &sender.SenderConfig{
		IndexerURL:            cfg.IndexerURL,
		IndexerTimeout:        cfg.IndexerTimeout,
		APIKey:                cfg.IndexerAPIKey,
		TLSInsecureSkipVerify: cfg.TLSInsecureSkipVerify,
		TLSCACertFile:         cfg.TLSCACertFile,
		TLSClientCertFile:     cfg.TLSClientCertFile,
		TLSClientKeyFile:      cfg.TLSClientKeyFile,
		TLSServerName:         cfg.TLSServerName,
		OverwriteState:        cfg.OverwriteState,
		BatchSize:             cfg.SenderBatchSize,
		BatchTimeout:          cfg.SenderBatchTimeout,
		SendInterval:          cfg.SenderSendInterval,
		MaxRetries:            cfg.SenderMaxRetries,
		RetryDelay:            cfg.SenderRetryDelay,
		RetryBackoff:          2.0,
		MaxResourcesPerSync:   cfg.SenderMaxResourcesPerSync,
		FailureThreshold:      5,
		BackoffDuration:       5 * time.Minute,
	}
	s := sender.NewHTTPSender(senderConfig, r)
	
	// Start sender
	if err := s.Start(); err != nil {
		klog.Fatalf("Failed to start sender: %v", err)
	}
	defer s.Stop()
	
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
		statusHandler := status.NewStatusHandler(r, s)
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

// runDiagnostics performs various diagnostic tests to help troubleshoot issues
func runDiagnostics(cfg *config.Config) {
	fmt.Println("🔍 Search Collector AI Diagnostics")
	fmt.Println("==================================")
	
	// Test 1: Configuration check
	fmt.Println("\n1. Configuration Check:")
	fmt.Printf("   📍 Indexer URL: %s\n", cfg.IndexerURL)
	fmt.Printf("   🔒 TLS Skip Verify: %v\n", cfg.TLSInsecureSkipVerify)
	fmt.Printf("   📜 TLS CA Cert: %s\n", getStringOrEmpty(cfg.TLSCACertFile))
	fmt.Printf("   🎫 API Key: %s\n", getMaskedString(cfg.IndexerAPIKey))
	fmt.Printf("   🔄 Overwrite State: %v\n", cfg.OverwriteState)
	
	if cfg.TLSInsecureSkipVerify {
		fmt.Println("   ⚠️  WARNING: TLS certificate verification is disabled")
	}
	
	// Test 2: Create sender and test connectivity
	fmt.Println("\n2. Search Indexer Connectivity Test:")
	
	// Create a mock reconciler for testing
	mockReconciler := &MockReconciler{}
	
	// Create sender configuration
	senderConfig := &sender.SenderConfig{
		IndexerURL:            cfg.IndexerURL,
		IndexerTimeout:        cfg.IndexerTimeout,
		APIKey:                cfg.IndexerAPIKey,
		TLSInsecureSkipVerify: cfg.TLSInsecureSkipVerify,
		TLSCACertFile:         cfg.TLSCACertFile,
		TLSClientCertFile:     cfg.TLSClientCertFile,
		TLSClientKeyFile:      cfg.TLSClientKeyFile,
		TLSServerName:         cfg.TLSServerName,
		OverwriteState:        cfg.OverwriteState,
		BatchSize:             cfg.SenderBatchSize,
		BatchTimeout:          cfg.SenderBatchTimeout,
		SendInterval:          cfg.SenderSendInterval,
		MaxRetries:            1, // Reduce retries for faster diagnosis
		RetryDelay:            cfg.SenderRetryDelay,
		RetryBackoff:          2.0,
		MaxResourcesPerSync:   cfg.SenderMaxResourcesPerSync,
		FailureThreshold:      5,
		BackoffDuration:       5 * time.Minute,
	}
	
	// Create and test HTTP sender
	httpSender := sender.NewHTTPSender(senderConfig, mockReconciler)
	
	fmt.Printf("   🌐 Testing connection to %s...\n", cfg.IndexerURL)
	
	// The testConnection will be called when we start the sender
	if err := httpSender.Start(); err != nil {
		fmt.Printf("   ❌ Sender startup failed: %v\n", err)
	} else {
		fmt.Println("   ✅ Sender started successfully")
		httpSender.Stop()
	}
	
	// Test 3: Test a simple sync event
	fmt.Println("\n3. Test Sync Event:")
	fmt.Println("   📤 Attempting to send test sync event...")
	
	// Create a new sender for the test
	testSender := sender.NewHTTPSender(senderConfig, mockReconciler)
	testSender.Start()
	defer testSender.Stop()
	
	// Create a minimal test sync event
	testEvent := &sender.SyncEvent{
		AddResources: []sender.Resource{
			{
				Kind:           "Pod",
				UID:            "test-uid-diagnostic",
				ResourceString: "diagnostic/test-pod",
				Properties: map[string]interface{}{
					"name":      "diagnostic-test",
					"namespace": "diagnostic",
				},
			},
		},
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	if err := testSender.SendSyncEvent(ctx, testEvent); err != nil {
		fmt.Printf("   ❌ Test sync failed: %v\n", err)
		
		// Provide specific guidance based on error
		if isConnectionError(err) {
			fmt.Println("\n💡 Troubleshooting suggestions:")
			fmt.Println("   • Check if search indexer service is running")
			fmt.Println("   • Verify the indexer URL is correct")
			fmt.Println("   • Test connectivity: curl -k " + cfg.IndexerURL)
		} else if is404Error(err) {
			fmt.Println("\n💡 HTTP 404 Error - Troubleshooting suggestions:")
			fmt.Println("   • Verify the endpoint path is correct")
			fmt.Println("   • Check search indexer API documentation")
			fmt.Println("   • Try: curl -k -X POST " + cfg.IndexerURL + " -H 'Content-Type: application/json' -d '{}'")
		} else if is500Error(err) {
			fmt.Println("\n💡 HTTP 500 Error - Troubleshooting suggestions:")
			fmt.Println("   • Check search indexer service logs for error details")
			fmt.Println("   • Verify the request payload format is correct")
			fmt.Println("   • Check indexer database/storage connectivity")
			fmt.Println("   • Ensure indexer has proper configuration and permissions")
		}
	} else {
		fmt.Println("   ✅ Test sync event sent successfully!")
	}
	
	// Test 4: Summary and recommendations
	fmt.Println("\n4. Summary and Recommendations:")
	fmt.Println("   📋 Diagnostic complete")
	
	if cfg.TLSInsecureSkipVerify {
		fmt.Println("   ⚠️  Security: Consider using proper TLS certificates in production")
	}
	
	fmt.Println("\n📖 For more troubleshooting help, see TROUBLESHOOTING.md")
	fmt.Println("🐛 Run with -v=4 for detailed debug logging")
}

// Helper functions for diagnostics
func getStringOrEmpty(s string) string {
	if s == "" {
		return "(not set)"
	}
	return s
}

func getMaskedString(s string) string {
	if s == "" {
		return "(not set)"
	}
	if len(s) <= 8 {
		return "***"
	}
	return s[:4] + "***" + s[len(s)-4:]
}

func isConnectionError(err error) bool {
	errStr := err.Error()
	return contains(errStr, "connection") || contains(errStr, "network") || contains(errStr, "timeout")
}

func is404Error(err error) bool {
	return contains(err.Error(), "404")
}

func is500Error(err error) bool {
	return contains(err.Error(), "500")
}

func contains(str, substr string) bool {
	return len(str) >= len(substr) && str[len(str)-len(substr):] == substr || 
		   (len(str) > len(substr) && findSubstring(str, substr))
}

func findSubstring(str, substr string) bool {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// MockReconciler for diagnostics
type MockReconciler struct{}

func (m *MockReconciler) ApplyChange(change *reconciler.StateChange) error { return nil }
func (m *MockReconciler) GetResource(resourceKey string) (*reconciler.ResourceState, bool) { return nil, false }
func (m *MockReconciler) ListResources(resourceType string) []*reconciler.ResourceState { return nil }
func (m *MockReconciler) GetChangedResources() []*reconciler.ResourceState { return nil }
func (m *MockReconciler) MarkSynced(resourceKeys []string) error { return nil }
func (m *MockReconciler) GetStats() *reconciler.ReconcilerStats { return nil }
func (m *MockReconciler) Cleanup(olderThan time.Duration) int { return 0 }
func (m *MockReconciler) Start() error { return nil }
func (m *MockReconciler) Stop() {}