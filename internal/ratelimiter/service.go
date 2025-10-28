package ratelimiter

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/example/distributed-rate-limiter/internal/pbcodec"
	"github.com/example/distributed-rate-limiter/pkg/natsutil"
)

type bucket struct {
	Capacity   int64
	Tokens     float64
	RefillRate float64
	LastRefill time.Time
}

type RateLimiter struct {
	mu      sync.RWMutex
	buckets map[string]*bucket
	bus     natsutil.Bus
	subject string
}

type syncEvent struct {
	Key        string    `json:"key"`
	Tokens     float64   `json:"tokens"`
	Capacity   int64     `json:"capacity"`
	RefillRate float64   `json:"refill_rate"`
	LastRefill time.Time `json:"last_refill"`
}

func New(bus natsutil.Bus) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*bucket),
		bus:     bus,
		subject: "ratelimiter.sync",
	}
	if bus != nil {
		bus.Subscribe(rl.subject, rl.handleSync)
	}
	return rl
}

func (r *RateLimiter) Allow(ctx context.Context, req *pbcodec.AllowRequest) (*pbcodec.AllowResponse, error) {
	if req.Key == "" {
		return &pbcodec.AllowResponse{Allowed: false, Message: "key is required"}, nil
	}
	tokens := req.Tokens
	if tokens <= 0 {
		tokens = 1
	}
	capacity := req.MaxTokens
	if capacity <= 0 {
		capacity = 10
	}
	refill := req.RefillRate
	if refill < 0 {
		refill = 1
	}

	r.mu.Lock()
	b, ok := r.buckets[req.Key]
	if !ok {
		b = &bucket{Capacity: capacity, Tokens: float64(capacity), RefillRate: float64(refill), LastRefill: time.Now()}
		r.buckets[req.Key] = b
	} else {
		if capacity != b.Capacity {
			b.Capacity = capacity
			if b.Tokens > float64(b.Capacity) {
				b.Tokens = float64(b.Capacity)
			}
		}
		if float64(refill) != b.RefillRate {
			b.RefillRate = float64(refill)
		}
	}
	now := time.Now()
	elapsed := now.Sub(b.LastRefill).Seconds()
	if elapsed > 0 {
		b.Tokens += elapsed * b.RefillRate
		if b.Tokens > float64(b.Capacity) {
			b.Tokens = float64(b.Capacity)
		}
		b.LastRefill = now
	}
	allowed := false
	if b.Tokens >= float64(tokens) {
		b.Tokens -= float64(tokens)
		allowed = true
	}
	remaining := int64(b.Tokens)
	if remaining < 0 {
		remaining = 0
	}
	updated := &syncEvent{
		Key:        req.Key,
		Tokens:     b.Tokens,
		Capacity:   b.Capacity,
		RefillRate: b.RefillRate,
		LastRefill: b.LastRefill,
	}
	r.mu.Unlock()

	if allowed {
		r.broadcast(updated)
		return &pbcodec.AllowResponse{Allowed: true, RemainingTokens: remaining, Message: "allowed"}, nil
	}
	return &pbcodec.AllowResponse{Allowed: false, RemainingTokens: remaining, Message: "rate limit exceeded"}, nil
}

func (r *RateLimiter) broadcast(evt *syncEvent) {
	if r.bus == nil {
		return
	}
	data, err := json.Marshal(evt)
	if err != nil {
		return
	}
	r.bus.Publish(r.subject, data)
}

func (r *RateLimiter) handleSync(msg []byte) {
	var evt syncEvent
	if err := json.Unmarshal(msg, &evt); err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	b, ok := r.buckets[evt.Key]
	if !ok {
		b = &bucket{}
		r.buckets[evt.Key] = b
	}
	b.Capacity = evt.Capacity
	b.Tokens = evt.Tokens
	b.RefillRate = evt.RefillRate
	b.LastRefill = evt.LastRefill
}

func (r *RateLimiter) DebugState() map[string]map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]map[string]interface{})
	for key, b := range r.buckets {
		out[key] = map[string]interface{}{
			"capacity":    b.Capacity,
			"tokens":      b.Tokens,
			"refill_rate": b.RefillRate,
			"last_refill": b.LastRefill.Format(time.RFC3339Nano),
		}
	}
	return out
}

func (r *RateLimiter) AllowFromBytes(ctx context.Context, payload []byte) ([]byte, error) {
	var req pbcodec.AllowRequest
	if err := pbcodec.UnmarshalAllowRequest(payload, &req); err != nil {
		return nil, fmt.Errorf("decode request: %w", err)
	}
	res, err := r.Allow(ctx, &req)
	if err != nil {
		return nil, err
	}
	return pbcodec.MarshalAllowResponse(res), nil
}
