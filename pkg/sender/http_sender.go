package sender

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/jlpadilla/search-collector-ai/pkg/reconciler"
	"k8s.io/klog/v2"
)

// HTTPSender implements the Sender interface using HTTP requests
type HTTPSender struct {
	config     *SenderConfig
	reconciler reconciler.Reconciler
	httpClient *http.Client
	stats      *SenderStats

	// Control channels
	ctx    context.Context
	cancel context.CancelFunc
	stopCh chan struct{}

	// State management
	mu                  sync.RWMutex
	running             bool
	consecutiveFailures int
	lastFailureTime     time.Time
	nextSendTime        time.Time

	// Background processes
	wg sync.WaitGroup
}

// NewHTTPSender creates a new HTTP sender
func NewHTTPSender(config *SenderConfig, r reconciler.Reconciler) *HTTPSender {
	if config == nil {
		config = DefaultSenderConfig()
	}

	ctx, cancel := context.WithCancel(context.Background())

	// Configure TLS settings
	tlsConfig, err := configureTLS(config)
	if err != nil {
		klog.Errorf("Failed to configure TLS: %v, using default TLS config", err)
		tlsConfig = &tls.Config{}
	}

	// Configure HTTP client with timeouts and TLS
	transport := &http.Transport{
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		DisableCompression:  false,
		MaxIdleConnsPerHost: 5,
		TLSClientConfig:     tlsConfig,
	}

	httpClient := &http.Client{
		Timeout:   config.IndexerTimeout,
		Transport: transport,
	}

	return &HTTPSender{
		config:     config,
		reconciler: r,
		httpClient: httpClient,
		stats: &SenderStats{
			IsHealthy: true,
		},
		ctx:    ctx,
		cancel: cancel,
		stopCh: make(chan struct{}),
	}
}

// Start starts the sender background processes
func (s *HTTPSender) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("sender is already running")
	}

	// Test connectivity to the search indexer (optional but helpful for debugging)
	if err := s.testConnection(); err != nil {
		klog.Warningf("Search indexer connectivity test failed: %v", err)
		klog.Warning("Continuing startup anyway - errors will be logged during sync attempts")
	}

	s.running = true
	s.nextSendTime = time.Now()

	// Start background processing
	s.wg.Add(1)
	go s.processLoop()

	klog.Info("HTTP sender started")
	return nil
}

// Stop stops the sender
func (s *HTTPSender) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	s.mu.Unlock()

	klog.Info("Stopping HTTP sender...")
	s.cancel()
	close(s.stopCh)
	s.wg.Wait()
	klog.Info("HTTP sender stopped")
}

// processLoop runs the main processing loop
func (s *HTTPSender) processLoop() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.SendInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if we should send based on backoff
			if time.Now().Before(s.nextSendTime) {
				continue
			}

			// Process changes
			if err := s.ProcessChanges(s.ctx); err != nil {
				klog.Errorf("Failed to process changes: %v", err)
			}

		case <-s.stopCh:
			return
		case <-s.ctx.Done():
			return
		}
	}
}

// ProcessChanges processes changed resources from the reconciler and sends them
func (s *HTTPSender) ProcessChanges(ctx context.Context) error {
	// Get changed resources from reconciler
	changedResources := s.reconciler.GetChangedResources()
	if len(changedResources) == 0 {
		klog.V(4).Info("No changed resources to send")
		return nil
	}

	klog.V(2).Infof("Processing %d changed resources", len(changedResources))

	// Convert to sync events (may create multiple events if too many resources)
	syncEvents, err := s.convertToSyncEvents(changedResources)
	if err != nil {
		return fmt.Errorf("failed to convert resources to sync events: %w", err)
	}

	// Send each sync event
	var sentResourceKeys []string
	for i, event := range syncEvents {
		klog.V(3).Infof("Sending sync event %d/%d with %d adds, %d updates, %d deletes",
			i+1, len(syncEvents),
			len(event.AddResources),
			len(event.UpdateResources),
			len(event.DeleteResources))

		if err := s.SendSyncEvent(ctx, event); err != nil {
			s.handleSendFailure(err)
			return fmt.Errorf("failed to send sync event %d: %w", i+1, err)
		}

		// Collect resource keys that were successfully sent
		for _, res := range event.AddResources {
			sentResourceKeys = append(sentResourceKeys, s.getResourceKeyFromUID(res.UID, changedResources))
		}
		for _, res := range event.UpdateResources {
			sentResourceKeys = append(sentResourceKeys, s.getResourceKeyFromUID(res.UID, changedResources))
		}
		for _, res := range event.DeleteResources {
			sentResourceKeys = append(sentResourceKeys, s.getResourceKeyFromUID(res.UID, changedResources))
		}
	}

	// Mark resources as synced in reconciler
	if len(sentResourceKeys) > 0 {
		if err := s.reconciler.MarkSynced(sentResourceKeys); err != nil {
			klog.Errorf("Failed to mark resources as synced: %v", err)
		} else {
			klog.V(2).Infof("Marked %d resources as synced", len(sentResourceKeys))
		}
	}

	s.handleSendSuccess(len(changedResources))
	return nil
}

// SendSyncEvent sends a sync event to the search indexer
func (s *HTTPSender) SendSyncEvent(ctx context.Context, event *SyncEvent) error {
	startTime := time.Now()

	// Serialize the sync event
	jsonData, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal sync event: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", s.config.IndexerURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "search-collector-ai/1.0")
	
	// Set X-Overwrite-State header required by the search indexer
	if s.config.OverwriteState {
		req.Header.Set("X-Overwrite-State", "true")
	} else {
		req.Header.Set("X-Overwrite-State", "false")
	}

	// Log the request details for debugging
	klog.V(4).Infof("Sending sync event to %s with %d bytes payload", s.config.IndexerURL, len(jsonData))
	klog.V(5).Infof("Sync event payload: %s", string(jsonData))

	// Add authentication if configured
	if s.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	} else if s.config.Username != "" && s.config.Password != "" {
		req.SetBasicAuth(s.config.Username, s.config.Password)
	}

	// Send request with retry logic
	var lastErr error
	for attempt := 0; attempt <= s.config.MaxRetries; attempt++ {
		if attempt > 0 {
			// Calculate backoff delay
			backoffMultiplier := 1 << uint(attempt-1)
			delay := time.Duration(float64(s.config.RetryDelay) * float64(backoffMultiplier) * s.config.RetryBackoff)
			klog.V(3).Infof("Retrying sync event send after %v (attempt %d/%d)",
				delay, attempt+1, s.config.MaxRetries+1)

			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		// Execute request
		resp, err := s.httpClient.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("HTTP request failed: %w", err)
			klog.V(3).Infof("Attempt %d failed: %v", attempt+1, err)
			continue
		}

		// Check response status
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			resp.Body.Close()

			// Update statistics
			latency := time.Since(startTime)
			s.updateSuccessStats(event, latency)

			klog.V(3).Infof("Successfully sent sync event to %s (status: %d, latency: %v)",
				s.config.IndexerURL, resp.StatusCode, latency)
			return nil
		}

		// Read error response
		var errorMsg string
		if resp.Body != nil {
			buf := make([]byte, 1024)
			if n, readErr := resp.Body.Read(buf); readErr == nil && n > 0 {
				errorMsg = string(buf[:n])
			}
			resp.Body.Close()
		}

		// Provide detailed error information for debugging
		if resp.StatusCode == 404 {
			lastErr = fmt.Errorf("HTTP 404 Not Found: The search indexer endpoint '%s' was not found. Please verify: 1) The indexer service is running, 2) The URL is correct, 3) The endpoint path is correct. Error response: %s", s.config.IndexerURL, errorMsg)
			klog.Errorf("HTTP 404 - Search indexer endpoint not found: %s", s.config.IndexerURL)
			klog.Errorf("Please check: 1) Indexer service status, 2) URL correctness, 3) Network connectivity")
		} else if resp.StatusCode == 500 {
			lastErr = fmt.Errorf("HTTP 500 Internal Server Error: The search indexer encountered an internal error processing the request to '%s'. This usually indicates: 1) Invalid request payload format, 2) Database/storage issues, 3) Search indexer configuration problems. Error response: %s", s.config.IndexerURL, errorMsg)
			klog.Errorf("HTTP 500 - Search indexer internal error: %s", s.config.IndexerURL)
			klog.Errorf("Check indexer logs for details. Payload might be invalid or indexer has internal issues")
		} else {
			lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, errorMsg)
		}

		klog.V(3).Infof("Attempt %d failed with status %d: %s", attempt+1, resp.StatusCode, errorMsg)

		// Don't retry on client errors (4xx)
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			break
		}
	}

	return fmt.Errorf("failed to send sync event after %d attempts: %w", s.config.MaxRetries+1, lastErr)
}

// GetStats returns sender statistics
func (s *HTTPSender) GetStats() *SenderStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// Create a copy of stats
	statsCopy := *s.stats
	statsCopy.ConsecutiveFailures = s.consecutiveFailures
	statsCopy.CurrentBackoff = time.Now().Before(s.nextSendTime)
	statsCopy.NextSendTime = s.nextSendTime

	// Get pending resources count from reconciler
	changedResources := s.reconciler.GetChangedResources()
	statsCopy.PendingResources = len(changedResources)

	return &statsCopy
}

// handleSendSuccess updates statistics for successful sends
func (s *HTTPSender) handleSendSuccess(resourceCount int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.consecutiveFailures = 0
	s.stats.SuccessfulSyncs++
	s.stats.LastSuccessTime = time.Now()
	s.stats.LastSyncTime = time.Now()
	s.stats.IsHealthy = true
	s.nextSendTime = time.Now() // Allow immediate next send

	klog.V(2).Infof("Successfully sent %d resources to search indexer", resourceCount)
}

// handleSendFailure updates statistics for failed sends
func (s *HTTPSender) handleSendFailure(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.consecutiveFailures++
	s.stats.FailedSyncs++
	s.stats.LastFailureTime = time.Now()
	s.stats.LastSyncTime = time.Now()
	s.stats.LastError = err.Error()

	// Apply backoff if too many consecutive failures
	if s.consecutiveFailures >= s.config.FailureThreshold {
		s.nextSendTime = time.Now().Add(s.config.BackoffDuration)
		s.stats.IsHealthy = false
		klog.Warningf("Too many consecutive failures (%d), backing off until %v",
			s.consecutiveFailures, s.nextSendTime)
	}

	klog.Errorf("Failed to send to search indexer (failure %d): %v", s.consecutiveFailures, err)
}

// updateSuccessStats updates statistics for successful operations
func (s *HTTPSender) updateSuccessStats(event *SyncEvent, latency time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.stats.TotalSyncEvents++
	s.stats.ResourcesAdded += int64(len(event.AddResources))
	s.stats.ResourcesUpdated += int64(len(event.UpdateResources))
	s.stats.ResourcesDeleted += int64(len(event.DeleteResources))
	s.stats.ResourcesSent += int64(len(event.AddResources) + len(event.UpdateResources) + len(event.DeleteResources))
	s.stats.EdgesAdded += int64(len(event.AddEdges))
	s.stats.EdgesDeleted += int64(len(event.DeleteEdges))
	s.stats.EdgesSent += int64(len(event.AddEdges) + len(event.DeleteEdges))

	// Update average latency (simple moving average)
	if s.stats.AverageLatencyMs == 0 {
		s.stats.AverageLatencyMs = float64(latency.Nanoseconds()) / 1e6
	} else {
		s.stats.AverageLatencyMs = (s.stats.AverageLatencyMs * 0.9) + (float64(latency.Nanoseconds())/1e6)*0.1
	}

	// Update average batch size
	batchSize := float64(len(event.AddResources) + len(event.UpdateResources) + len(event.DeleteResources))
	if s.stats.AverageBatchSize == 0 {
		s.stats.AverageBatchSize = batchSize
	} else {
		s.stats.AverageBatchSize = (s.stats.AverageBatchSize * 0.9) + (batchSize)*0.1
	}
}

// getResourceKeyFromUID finds the resource key for a given UID
func (s *HTTPSender) getResourceKeyFromUID(uid string, resources []*reconciler.ResourceState) string {
	for _, resource := range resources {
		if resource.TransformedResource != nil {
			// Try to get UID from transformed resource fields
			if resourceUID, exists := resource.TransformedResource.Fields["metadata.uid"]; exists {
				if resourceUID == uid {
					return resource.ResourceKey
				}
			}
			// This was a fallback that's no longer needed since we're using fields
		}
	}
	return ""
}

// configureTLS configures TLS settings based on the sender configuration
func configureTLS(config *SenderConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		InsecureSkipVerify: config.TLSInsecureSkipVerify,
		// Add compatibility settings to avoid TLS handshake issues
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS13,
		// Enable cipher suites for compatibility
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		},
	}

	// Set server name for TLS verification if specified
	if config.TLSServerName != "" {
		tlsConfig.ServerName = config.TLSServerName
	}

	// Load custom CA certificate if specified
	if config.TLSCACertFile != "" {
		caCert, err := os.ReadFile(config.TLSCACertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate file %s: %w", config.TLSCACertFile, err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate from %s", config.TLSCACertFile)
		}
		tlsConfig.RootCAs = caCertPool
		klog.V(2).Infof("Loaded custom CA certificate from %s", config.TLSCACertFile)
	}

	// Load client certificate and key if specified
	if config.TLSClientCertFile != "" && config.TLSClientKeyFile != "" {
		clientCert, err := tls.LoadX509KeyPair(config.TLSClientCertFile, config.TLSClientKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate and key: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{clientCert}
		klog.V(2).Infof("Loaded client certificate from %s", config.TLSClientCertFile)
	}

	// Log TLS configuration for debugging
	if config.TLSInsecureSkipVerify {
		klog.Warning("TLS certificate verification is disabled - this is insecure for production use")
	} else {
		klog.V(2).Info("TLS certificate verification is enabled")
	}

	return tlsConfig, nil
}

// testConnection performs a connectivity test to the search indexer
func (s *HTTPSender) testConnection() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a simple HEAD request to test connectivity
	req, err := http.NewRequestWithContext(ctx, "HEAD", s.config.IndexerURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create test request: %w", err)
	}

	// Add authentication headers if configured
	if s.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+s.config.APIKey)
	} else if s.config.Username != "" && s.config.Password != "" {
		req.SetBasicAuth(s.config.Username, s.config.Password)
	}

	klog.V(2).Infof("Testing connectivity to search indexer: %s", s.config.IndexerURL)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("connection test failed: %w", err)
	}
	defer resp.Body.Close()

	// Log the response for debugging
	klog.V(2).Infof("Search indexer connectivity test response: %d %s", resp.StatusCode, resp.Status)

	// Don't fail on 404 or 405 (Method Not Allowed) as the endpoint might not support HEAD
	// but we've confirmed connectivity
	if resp.StatusCode == 404 {
		klog.Warning("Search indexer responded with 404 - the /sync endpoint may not exist or HEAD method not supported")
		klog.Warning("This might indicate a configuration issue with the indexer URL")
	} else if resp.StatusCode == 405 {
		klog.V(2).Info("Search indexer doesn't support HEAD method, but connectivity confirmed")
	} else if resp.StatusCode >= 400 {
		return fmt.Errorf("search indexer returned error status: %d %s", resp.StatusCode, resp.Status)
	}

	klog.V(2).Info("Search indexer connectivity test passed")
	return nil
}

// DefaultSenderConfig returns a default sender configuration
func DefaultSenderConfig() *SenderConfig {
	return &SenderConfig{
		IndexerURL:            "https://localhost:3010/aggregator/clusters/local-ai/sync",
		IndexerTimeout:        30 * time.Second,
		TLSInsecureSkipVerify: false, // Secure by default
		OverwriteState:        false, // Don't overwrite existing state by default
		BatchSize:             100,
		BatchTimeout:          5 * time.Second,
		SendInterval:          10 * time.Second,
		MaxRetries:            3,
		RetryDelay:            1 * time.Second,
		RetryBackoff:          2.0,
		MaxResourcesPerSync:   1000,
		FailureThreshold:      5,
		BackoffDuration:       5 * time.Minute,
	}
}
