package ratelimiter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/distributed-rate-limiter/internal/pbcodec"
	"github.com/example/distributed-rate-limiter/pkg/natsutil"
)

// StatsRow represents a single row in the distributed statistics table.
type StatsRow struct {
	Key             string    `json:"key"`
	Source          string    `json:"source"`
	Allowed         int64     `json:"allowed"`
	Denied          int64     `json:"denied"`
	LastAllowedAt   time.Time `json:"last_allowed_at,omitempty"`
	LastDeniedAt    time.Time `json:"last_denied_at,omitempty"`
	LastRequestAt   time.Time `json:"last_request_at,omitempty"`
	RemainingTokens float64   `json:"remaining_tokens"`
}

type bucket struct {
	Capacity   int64
	Tokens     float64
	RefillRate float64
	LastRefill time.Time
	Version    uint64
}

type statsEntry struct {
	Key             string
	Source          string
	Allowed         int64
	Denied          int64
	LastAllowedAt   time.Time
	LastDeniedAt    time.Time
	LastRequestAt   time.Time
	RemainingTokens float64
	Version         uint64
}

type syncEvent struct {
	Key        string      `json:"key"`
	Tokens     float64     `json:"tokens"`
	Capacity   int64       `json:"capacity"`
	RefillRate float64     `json:"refill_rate"`
	LastRefill time.Time   `json:"last_refill"`
	Version    uint64      `json:"version"`
	Stats      *statsEvent `json:"stats,omitempty"`
}

type statsEvent struct {
	Source          string    `json:"source"`
	Allowed         int64     `json:"allowed"`
	Denied          int64     `json:"denied"`
	LastAllowedAt   time.Time `json:"last_allowed_at"`
	LastDeniedAt    time.Time `json:"last_denied_at"`
	LastRequestAt   time.Time `json:"last_request_at"`
	RemainingTokens float64   `json:"remaining_tokens"`
	Version         uint64    `json:"version"`
}

// RateLimiter encapsulates token-bucket state, distributed synchronisation, and
// request statistics.
type RateLimiter struct {
	mu      sync.RWMutex
	buckets map[string]*bucket
	stats   map[string]*statsEntry

	bus     natsutil.Bus
	subject string

	now func() time.Time

	logger   *log.Logger
	sequence uint64
}

// Option configures optional behaviour when constructing a RateLimiter.
type Option func(*RateLimiter)

// WithClock overrides the clock used for refill and statistics timestamps. It
// is primarily intended for testing.
func WithClock(fn func() time.Time) Option {
	return func(rl *RateLimiter) {
		if fn != nil {
			rl.now = fn
		}
	}
}

// WithLogger injects the logger used for structured decision logs.
func WithLogger(l *log.Logger) Option {
	return func(rl *RateLimiter) {
		rl.logger = l
	}
}

// New constructs a rate limiter instance. When a non-nil message bus is
// provided the limiter will publish local state transitions and apply updates
// received from other nodes to keep buckets and statistics in sync.
func New(bus natsutil.Bus, opts ...Option) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*bucket),
		stats:   make(map[string]*statsEntry),
		bus:     bus,
		subject: "ratelimiter.sync",
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(rl)
	}
	if bus != nil {
		if _, err := bus.Subscribe(rl.subject, rl.handleSync); err != nil {
			if rl.logger != nil {
				rl.logger.Printf("failed to subscribe to bus: %v", err)
			}
		}
	}
	return rl
}

// Allow evaluates a request against the configured bucket and updates
// distributed statistics.
func (r *RateLimiter) Allow(ctx context.Context, req *pbcodec.AllowRequest) (*pbcodec.AllowResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}
	if strings.TrimSpace(req.Key) == "" {
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
		refill = 0
	}
	source := normaliseSource(req.Source)

	now := r.now().UTC()

	r.mu.Lock()
	b := r.ensureBucketLocked(req.Key, capacity, float64(refill), now)

	allowed := false
	if b.Tokens >= float64(tokens) {
		b.Tokens -= float64(tokens)
		allowed = true
	}
	if b.Tokens < 0 {
		b.Tokens = 0
	}

	entry := r.ensureStatsLocked(req.Key, source)
	entry.LastRequestAt = now
	entry.RemainingTokens = b.Tokens
	if allowed {
		entry.Allowed++
		entry.LastAllowedAt = now
	} else {
		entry.Denied++
		entry.LastDeniedAt = now
	}
	r.sequence++
	seq := r.sequence
	b.Version = seq
	entry.Version = seq

	remainingInt := int64(math.Floor(b.Tokens))
	if remainingInt < 0 {
		remainingInt = 0
	}

	res := &pbcodec.AllowResponse{
		Allowed:         allowed,
		RemainingTokens: remainingInt,
		Message:         outcomeMessage(allowed),
		AllowedHits:     entry.Allowed,
		DeniedHits:      entry.Denied,
		LastAllowedAt:   formatTimestamp(entry.LastAllowedAt),
		LastDeniedAt:    formatTimestamp(entry.LastDeniedAt),
	}

	evt := &syncEvent{
		Key:        req.Key,
		Tokens:     b.Tokens,
		Capacity:   b.Capacity,
		RefillRate: b.RefillRate,
		LastRefill: b.LastRefill,
		Version:    seq,
		Stats: &statsEvent{
			Source:          source,
			Allowed:         entry.Allowed,
			Denied:          entry.Denied,
			LastAllowedAt:   entry.LastAllowedAt,
			LastDeniedAt:    entry.LastDeniedAt,
			LastRequestAt:   entry.LastRequestAt,
			RemainingTokens: entry.RemainingTokens,
			Version:         entry.Version,
		},
	}
	r.mu.Unlock()

	r.broadcast(evt)
	r.logDecision(req.Key, source, tokens, capacity, float64(refill), evt, res)

	return res, nil
}

// StatsTable returns a sorted snapshot of per key/source statistics.
func (r *RateLimiter) StatsTable() []StatsRow {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := make([]StatsRow, 0, len(r.stats))
	for _, entry := range r.stats {
		rows = append(rows, StatsRow{
			Key:             entry.Key,
			Source:          entry.Source,
			Allowed:         entry.Allowed,
			Denied:          entry.Denied,
			LastAllowedAt:   entry.LastAllowedAt,
			LastDeniedAt:    entry.LastDeniedAt,
			LastRequestAt:   entry.LastRequestAt,
			RemainingTokens: entry.RemainingTokens,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Key == rows[j].Key {
			return rows[i].Source < rows[j].Source
		}
		return rows[i].Key < rows[j].Key
	})
	return rows
}

// DebugState exposes bucket state for diagnostics and testing.
func (r *RateLimiter) DebugState() map[string]map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()
	state := make(map[string]map[string]interface{}, len(r.buckets))
	for key, b := range r.buckets {
		state[key] = map[string]interface{}{
			"capacity":    b.Capacity,
			"tokens":      b.Tokens,
			"refill_rate": b.RefillRate,
			"last_refill": b.LastRefill.Format(time.RFC3339Nano),
			"version":     b.Version,
		}
	}
	return state
}

// AllowFromBytes decodes a protobuf payload, applies the decision, and returns
// the encoded response. It is used by the gRPC transport layer.
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

func (r *RateLimiter) ensureBucketLocked(key string, capacity int64, refill float64, now time.Time) *bucket {
	b, ok := r.buckets[key]
	if !ok {
		b = &bucket{
			Capacity:   capacity,
			Tokens:     float64(capacity),
			RefillRate: refill,
			LastRefill: now,
		}
		r.buckets[key] = b
		return b
	}
	if capacity != b.Capacity {
		b.Capacity = capacity
		if b.Tokens > float64(b.Capacity) {
			b.Tokens = float64(b.Capacity)
		}
	}
	if refill != b.RefillRate {
		b.RefillRate = refill
	}
	if now.After(b.LastRefill) {
		elapsed := now.Sub(b.LastRefill).Seconds()
		if elapsed > 0 && b.RefillRate > 0 {
			b.Tokens += elapsed * b.RefillRate
			if b.Tokens > float64(b.Capacity) {
				b.Tokens = float64(b.Capacity)
			}
		}
		b.LastRefill = now
	}
	return b
}

func (r *RateLimiter) ensureStatsLocked(key, source string) *statsEntry {
	if source == "" {
		source = "unknown"
	}
	composite := statsKey(key, source)
	entry, ok := r.stats[composite]
	if !ok {
		entry = &statsEntry{Key: key, Source: source}
		r.stats[composite] = entry
	}
	return entry
}

func (r *RateLimiter) broadcast(evt *syncEvent) {
	if r.bus == nil || evt == nil {
		return
	}
	data, err := json.Marshal(evt)
	if err != nil {
		if r.logger != nil {
			r.logger.Printf("failed to marshal sync event: %v", err)
		}
		return
	}
	if err := r.bus.Publish(r.subject, data); err != nil && r.logger != nil {
		r.logger.Printf("failed to publish sync event: %v", err)
	}
}

func (r *RateLimiter) handleSync(msg []byte) {
	var evt syncEvent
	if err := json.Unmarshal(msg, &evt); err != nil {
		if r.logger != nil {
			r.logger.Printf("failed to decode sync event: %v", err)
		}
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.buckets[evt.Key]
	if !ok {
		existing = &bucket{}
		r.buckets[evt.Key] = existing
	}
	if evt.Version >= existing.Version {
		existing.Capacity = evt.Capacity
		existing.Tokens = evt.Tokens
		existing.RefillRate = evt.RefillRate
		if !evt.LastRefill.IsZero() {
			existing.LastRefill = evt.LastRefill
		}
		existing.Version = evt.Version
	}

	if evt.Stats != nil && evt.Stats.Source != "" {
		composite := statsKey(evt.Key, evt.Stats.Source)
		entry, ok := r.stats[composite]
		if !ok {
			entry = &statsEntry{Key: evt.Key, Source: evt.Stats.Source}
			r.stats[composite] = entry
		}
		if evt.Stats.Version >= entry.Version {
			entry.Allowed = evt.Stats.Allowed
			entry.Denied = evt.Stats.Denied
			entry.LastAllowedAt = evt.Stats.LastAllowedAt
			entry.LastDeniedAt = evt.Stats.LastDeniedAt
			entry.LastRequestAt = evt.Stats.LastRequestAt
			entry.RemainingTokens = evt.Stats.RemainingTokens
			entry.Version = evt.Stats.Version
		}
	}

	if evt.Version > r.sequence {
		r.sequence = evt.Version
	}
}

func (r *RateLimiter) logDecision(key, source string, tokens, capacity int64, refill float64, evt *syncEvent, res *pbcodec.AllowResponse) {
	if r.logger == nil {
		return
	}
	outcome := "DENY"
	if res.Allowed {
		outcome = "ALLOW"
	}
	r.logger.Printf(
		"decision=%s key=%s source=%s tokens=%d capacity=%d refill_rate=%.2f remaining=%.2f allowed_hits=%d denied_hits=%d version=%d",
		outcome,
		key,
		source,
		tokens,
		capacity,
		refill,
		evt.Tokens,
		res.AllowedHits,
		res.DeniedHits,
		evt.Version,
	)
}

func normaliseSource(input string) string {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "unknown"
	}
	return trimmed
}

func statsKey(key, source string) string {
	return key + "|" + source
}

func outcomeMessage(allowed bool) string {
	if allowed {
		return "allowed"
	}
	return "rate limit exceeded"
}

func formatTimestamp(ts time.Time) string {
	if ts.IsZero() {
		return ""
	}
	return ts.UTC().Format(time.RFC3339Nano)
}
