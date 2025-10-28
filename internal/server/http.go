package server

import (
	"encoding/json"
	"net/http"

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
	state := h.limiter.DebugState()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(state)
}
