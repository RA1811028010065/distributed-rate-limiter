package server

import (
	"encoding/json"
	"net"
	"net/http"
	"strings"

	"github.com/example/distributed-rate-limiter/internal/pbcodec"
	"github.com/example/distributed-rate-limiter/internal/ratelimiter"
)

type HTTPServer struct {
	limiter *ratelimiter.RateLimiter
}

func NewHTTPServer(limiter *ratelimiter.RateLimiter) *HTTPServer {
	return &HTTPServer{limiter: limiter}
}

func (h *HTTPServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/allow", h.handleAllow)
	mux.HandleFunc("/api/v1/debug", h.handleDebug)
	mux.HandleFunc("/api/v1/stats", h.handleStats)
	mux.HandleFunc("/healthz", h.handleHealth)
	return mux
}

func (h *HTTPServer) handleAllow(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	var req pbcodec.AllowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if req.Source == "" {
		req.Source = inferSource(r)
	}
	res, err := h.limiter.Allow(r.Context(), &req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(res)
}

func (h *HTTPServer) handleDebug(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	state := map[string]interface{}{
		"buckets": h.limiter.DebugState(),
		"stats":   h.limiter.StatsTable(),
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}

func (h *HTTPServer) handleStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	rows := h.limiter.StatsTable()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(rows)
}

func (h *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func inferSource(r *http.Request) string {
	if values := r.Header.Get("X-Forwarded-For"); values != "" {
		parts := strings.Split(values, ",")
		if len(parts) > 0 {
			candidate := strings.TrimSpace(parts[0])
			if candidate != "" {
				return candidate
			}
		}
	}
	if value := r.Header.Get("X-Real-IP"); strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return ""
}
