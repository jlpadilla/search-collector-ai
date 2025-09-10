package sender

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/jlpadilla/search-collector-ai/pkg/reconciler"
	"k8s.io/klog/v2"
)

// HTTPSender implements the Sender interface using HTTP requests
type HTTPSender struct {
	config      *SenderConfig
	reconciler  reconciler.Reconciler
	httpClient  *http.Client
	stats       *SenderStats
	
	// Control channels
	ctx      context.Context
	cancel   context.CancelFunc
	stopCh   chan struct{}
	
	// State management
	mu              sync.RWMutex
	running         bool
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
	
	// Configure HTTP client with timeouts
	httpClient := &http.Client{
		Timeout: config.IndexerTimeout,
		Transport: &http.Transport{
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			DisableCompression:  false,
			MaxIdleConnsPerHost: 5,
		},
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
		
		lastErr = fmt.Errorf("HTTP %d: %s", resp.StatusCode, errorMsg)
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
		s.stats.AverageLatencyMs = (s.stats.AverageLatencyMs*0.9) + (float64(latency.Nanoseconds())/1e6)*0.1
	}
	
	// Update average batch size
	batchSize := float64(len(event.AddResources) + len(event.UpdateResources) + len(event.DeleteResources))
	if s.stats.AverageBatchSize == 0 {
		s.stats.AverageBatchSize = batchSize
	} else {
		s.stats.AverageBatchSize = (s.stats.AverageBatchSize*0.9) + (batchSize)*0.1
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

// DefaultSenderConfig returns a default sender configuration
func DefaultSenderConfig() *SenderConfig {
	return &SenderConfig{
		IndexerURL:          "http://localhost:3010/sync",
		IndexerTimeout:      30 * time.Second,
		BatchSize:           100,
		BatchTimeout:        5 * time.Second,
		SendInterval:        10 * time.Second,
		MaxRetries:          3,
		RetryDelay:          1 * time.Second,
		RetryBackoff:        2.0,
		MaxResourcesPerSync: 1000,
		FailureThreshold:    5,
		BackoffDuration:     5 * time.Minute,
	}
}