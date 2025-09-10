package sender

import (
	"net/http"
	"testing"
	"time"

	"github.com/jlpadilla/search-collector-ai/pkg/reconciler"
)

func TestConfigureTLS(t *testing.T) {
	// Test case 1: Default secure configuration
	config1 := &SenderConfig{
		TLSInsecureSkipVerify: false,
	}

	tlsConfig1, err := configureTLS(config1)
	if err != nil {
		t.Fatalf("configureTLS failed: %v", err)
	}

	if tlsConfig1.InsecureSkipVerify {
		t.Error("Expected InsecureSkipVerify to be false")
	}

	// Test case 2: Insecure configuration (for testing)
	config2 := &SenderConfig{
		TLSInsecureSkipVerify: true,
	}

	tlsConfig2, err := configureTLS(config2)
	if err != nil {
		t.Fatalf("configureTLS failed: %v", err)
	}

	if !tlsConfig2.InsecureSkipVerify {
		t.Error("Expected InsecureSkipVerify to be true")
	}

	// Test case 3: Custom server name
	config3 := &SenderConfig{
		TLSInsecureSkipVerify: false,
		TLSServerName:         "search-indexer.example.com",
	}

	tlsConfig3, err := configureTLS(config3)
	if err != nil {
		t.Fatalf("configureTLS failed: %v", err)
	}

	if tlsConfig3.ServerName != "search-indexer.example.com" {
		t.Errorf("Expected ServerName 'search-indexer.example.com', got '%s'", tlsConfig3.ServerName)
	}

	// Test case 4: Invalid CA certificate file
	config4 := &SenderConfig{
		TLSCACertFile: "/nonexistent/ca.crt",
	}

	_, err = configureTLS(config4)
	if err == nil {
		t.Error("Expected error for nonexistent CA certificate file")
	}

	t.Logf("TLS configuration tests passed")
}

func TestHTTPSenderTLSIntegration(t *testing.T) {
	// Test creating HTTP sender with HTTPS configuration
	config := &SenderConfig{
		IndexerURL:            "https://localhost:3010/sync",
		TLSInsecureSkipVerify: true, // For testing only
	}

	mockReconciler := &MockReconciler{}
	sender := NewHTTPSender(config, mockReconciler)

	if sender == nil {
		t.Fatal("NewHTTPSender returned nil")
	}

	if sender.config.IndexerURL != "https://localhost:3010/sync" {
		t.Errorf("Expected HTTPS URL, got '%s'", sender.config.IndexerURL)
	}

	// Verify that the HTTP client was configured with TLS
	transport, ok := sender.httpClient.Transport.(*http.Transport)
	if ok && transport.TLSClientConfig != nil {
		// If we can cast to http.Transport and TLS config exists, that means TLS is configured
		t.Logf("TLS is properly configured: InsecureSkipVerify=%v", transport.TLSClientConfig.InsecureSkipVerify)
	} else {
		t.Error("TLS configuration not found in HTTP transport")
	}

	t.Logf("HTTPS sender integration test passed")
}

// MockReconciler for testing
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

func TestDefaultSenderConfigHTTPS(t *testing.T) {
	config := DefaultSenderConfig()

	if config.IndexerURL != "https://localhost:3010/sync" {
		t.Errorf("Expected HTTPS URL by default, got '%s'", config.IndexerURL)
	}

	if config.TLSInsecureSkipVerify {
		t.Error("Expected TLSInsecureSkipVerify to be false by default (secure)")
	}

	t.Logf("Default HTTPS configuration test passed")
}