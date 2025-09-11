package status

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/jlpadilla/search-collector-ai/pkg/reconciler"
	"github.com/jlpadilla/search-collector-ai/pkg/sender"
	"k8s.io/klog/v2"
)

// StatusHandler provides HTTP endpoints for status and statistics
type StatusHandler struct {
	reconciler reconciler.Reconciler
	sender     sender.Sender
	startTime  time.Time
}

// NewStatusHandler creates a new status handler
func NewStatusHandler(r reconciler.Reconciler, s sender.Sender) *StatusHandler {
	return &StatusHandler{
		reconciler: r,
		sender:     s,
		startTime:  time.Now(),
	}
}

// StatusResponse represents the overall system status
type StatusResponse struct {
	Status        string                      `json:"status"`
	Uptime        string                      `json:"uptime"`
	StartTime     time.Time                   `json:"startTime"`
	Reconciler    *reconciler.ReconcilerStats `json:"reconciler"`
	Sender        *sender.SenderStats         `json:"sender"`
	Version       string                      `json:"version,omitempty"`
	BuildDate     string                      `json:"buildDate,omitempty"`
	CommitHash    string                      `json:"commitHash,omitempty"`
}

// HealthHandler returns a simple health check
func (h *StatusHandler) HealthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	
	response := map[string]string{
		"status": "healthy",
		"time":   time.Now().Format(time.RFC3339),
	}
	
	json.NewEncoder(w).Encode(response)
}

// StatusHandler returns comprehensive system status
func (h *StatusHandler) StatusHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	// Get reconciler stats
	reconcilerStats := h.reconciler.GetStats()
	
	// Get sender stats
	var senderStats *sender.SenderStats
	if h.sender != nil {
		senderStats = h.sender.GetStats()
	}
	
	uptime := time.Since(h.startTime)
	
	response := &StatusResponse{
		Status:     "running",
		Uptime:     uptime.String(),
		StartTime:  h.startTime,
		Reconciler: reconcilerStats,
		Sender:     senderStats,
	}
	
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		klog.Errorf("Failed to encode status response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// ReconcilerStatsHandler returns detailed reconciler statistics
func (h *StatusHandler) ReconcilerStatsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	stats := h.reconciler.GetStats()
	
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		klog.Errorf("Failed to encode reconciler stats: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// ResourcesHandler returns list of resources (optionally filtered by type)
func (h *StatusHandler) ResourcesHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	resourceType := r.URL.Query().Get("type")
	onlyChanged := r.URL.Query().Get("changed") == "true"
	
	var resources interface{}
	
	if onlyChanged {
		resources = h.reconciler.GetChangedResources()
	} else {
		resources = h.reconciler.ListResources(resourceType)
	}
	
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resources); err != nil {
		klog.Errorf("Failed to encode resources response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// SenderStatsHandler returns detailed sender statistics
func (h *StatusHandler) SenderStatsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	
	if h.sender == nil {
		http.Error(w, "Sender not configured", http.StatusServiceUnavailable)
		return
	}
	
	stats := h.sender.GetStats()
	
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(stats); err != nil {
		klog.Errorf("Failed to encode sender stats: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// StartStatusServer starts an HTTP server for status endpoints
func StartStatusServer(addr string, handler *StatusHandler) error {
	mux := http.NewServeMux()
	
	// Register endpoints
	mux.HandleFunc("/health", handler.HealthHandler)
	mux.HandleFunc("/status", handler.StatusHandler)
	mux.HandleFunc("/reconciler/stats", handler.ReconcilerStatsHandler)
	mux.HandleFunc("/reconciler/resources", handler.ResourcesHandler)
	mux.HandleFunc("/sender/stats", handler.SenderStatsHandler)
	
	// Add a root handler that shows available endpoints
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		
		w.Header().Set("Content-Type", "application/json")
		endpoints := map[string]string{
			"/health":               "Health check endpoint",
			"/status":               "Overall system status and statistics",
			"/reconciler/stats":     "Detailed reconciler statistics",
			"/reconciler/resources": "List of managed resources (supports ?type=<type> and ?changed=true)",
			"/sender/stats":         "Detailed sender statistics and sync status",
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"service":   "Search Collector AI",
			"endpoints": endpoints,
		})
	})
	
	klog.Infof("Starting status server on %s", addr)
	
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
		// Add timeouts for security
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	
	return server.ListenAndServe()
}