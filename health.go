// File: health.go
// Health check endpoint for Render deployment

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"runtime"
	"time"
)

// HealthResponse represents the health check response
type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp string `json:"timestamp"`
	BotName   string `json:"bot_name,omitempty"`
	Uptime    string `json:"uptime,omitempty"`
	Version   string `json:"version"`
}

// startTime is the bot start time
var startTime = time.Now()

// startHealthServer starts the health check HTTP server
func startHealthServer() {
	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/healthz", healthHandler)
	http.HandleFunc("/ready", readyHandler)
	http.HandleFunc("/", rootHandler)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	addr := ":" + port
	log.Printf("Health server starting on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Printf("Health server error: %v", err)
	}
}

// healthHandler handles /health requests
func healthHandler(w http.ResponseWriter, r *http.Request) {
	response := HealthResponse{
		Status:    "healthy",
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Version:   "1.0.0",
	}

	if bot != nil {
		response.BotName = bot.Self.UserName
	}

	uptime := time.Since(startTime)
	response.Uptime = uptime.Round(time.Second).String()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

// readyHandler handles /ready requests (kubernetes readiness probe)
func readyHandler(w http.ResponseWriter, r *http.Request) {
	ready := true
	status := "ready"

	if bot == nil {
		ready = false
		status = "bot_not_initialized"
	}

	w.Header().Set("Content-Type", "application/json")
	if ready {
		w.WriteHeader(http.StatusOK)
	} else {
		w.WriteHeader(http.StatusServiceUnavailable)
	}
	json.NewEncoder(w).Encode(map[string]string{"status": status})
}

// rootHandler handles the root path
func rootHandler(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	info := map[string]interface{}{
		"bot":        "Telegram Video Downloader",
		"version":    "1.0.0",
		"status":     "running",
		"uptime":     time.Since(startTime).Round(time.Second).String(),
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"endpoints": []string{
			"/health - Health check",
			"/ready - Kubernetes readiness probe",
		},
		"memory": getMemoryStats(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(info)
}

// getMemoryStats returns current memory statistics
func getMemoryStats() map[string]interface{} {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	return map[string]interface{}{
		"alloc":      formatBytes(int64(m.Alloc)),
		"total_alloc": formatBytes(int64(m.TotalAlloc)),
		"sys":        formatBytes(int64(m.Sys)),
		"num_gc":     m.NumGC,
	}
}

// formatBytes formats bytes into human readable string
func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
