package ratelimiter

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/example/distributed-rate-limiter/internal/pbcodec"
	"github.com/example/distributed-rate-limiter/internal/storage"
)

// StatsRow exposes aggregated counters for each key + source.
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
	capacity   int64
	tokens     float64
	refillRate float64
	lastRefill time.Time
}

type statsEntry struct {
	Key           string
	Source        string
	Allowed       int64
	Denied        int64
	LastAllowedAt time.Time
	LastDeniedAt  time.Time
	LastRequestAt time.Time
	Remaining     float64
}

// RateLimiter enforces configured limits using an in-memory token bucket per key.
type RateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	stats   map[string]*statsEntry

	store storage.ConfigStore

	now     func() time.Time
	logger  *log.Logger
	verbose bool
}

// Option configures optional behaviour when constructing a RateLimiter.
type Option func(*RateLimiter)

// WithClock overrides the clock used for refill calculations.
func WithClock(fn func() time.Time) Option {
	return func(rl *RateLimiter) {
		if fn != nil {
			rl.now = fn
		}
	}
}

// WithLogger injects the logger used for verbose traces.
func WithLogger(l *log.Logger) Option {
	return func(rl *RateLimiter) {
		rl.logger = l
	}
}

// WithVerboseLogging toggles decision logging.
func WithVerboseLogging(enabled bool) Option {
	return func(rl *RateLimiter) {
		rl.verbose = enabled
	}
}

// WithConfigStore sets the persistence layer for rate limit definitions.
func WithConfigStore(store storage.ConfigStore) Option {
	return func(rl *RateLimiter) {
		if store != nil {
			rl.store = store
		}
	}
}

// New constructs a rate limiter instance backed by the provided config store.
func New(opts ...Option) *RateLimiter {
	rl := &RateLimiter{
		buckets: make(map[string]*bucket),
		stats:   make(map[string]*statsEntry),
		store:   storage.NewMemoryConfigStore(),
		now:     time.Now,
	}
	for _, opt := range opts {
		opt(rl)
	}
	return rl
}

// ConfigureLimit persists a new rate limit definition.
func (r *RateLimiter) ConfigureLimit(ctx context.Context, cfg storage.RateLimitConfig) error {
	if strings.TrimSpace(cfg.Key) == "" {
		return fmt.Errorf("key is required")
	}
	if cfg.MaxTokens <= 0 {
		cfg.MaxTokens = 1
	}
	if cfg.RefillRate < 0 {
		cfg.RefillRate = 0
	}
	return r.store.Set(ctx, cfg)
}

// Allow evaluates a request using the configured bucket for the key.
func (r *RateLimiter) Allow(ctx context.Context, req *pbcodec.AllowRequest) (*pbcodec.AllowResponse, error) {
	if req == nil {
		return nil, fmt.Errorf("request is required")
	}
	key := strings.TrimSpace(req.Key)
	if key == "" {
		return &pbcodec.AllowResponse{Allowed: false, Message: "key is required"}, nil
	}

	cfg, err := r.store.Get(ctx, key)
	if err != nil {
		return &pbcodec.AllowResponse{Allowed: false, Message: "rate limit not configured"}, nil
	}

	tokens := req.Tokens
	if tokens <= 0 {
		tokens = 1
	}

	now := r.now().UTC()

	r.mu.Lock()
	defer r.mu.Unlock()

	b := r.ensureBucketLocked(key, cfg, now)
	allowed := r.consumeTokens(b, float64(tokens), now)

	entry := r.ensureStatsLocked(key, normaliseSource(req.Source))
	entry.LastRequestAt = now
	entry.Remaining = b.tokens
	if allowed {
		entry.Allowed++
		entry.LastAllowedAt = now
	} else {
		entry.Denied++
		entry.LastDeniedAt = now
	}

	if r.verbose && r.logger != nil {
		state, _ := json.Marshal(map[string]any{
			"key":         key,
			"allowed":     allowed,
			"tokens":      tokens,
			"remaining":   b.tokens,
			"capacity":    b.capacity,
			"refill_rate": b.refillRate,
		})
		r.logger.Printf("allow decision: %s", string(state))
	}

	res := &pbcodec.AllowResponse{
		Allowed:         allowed,
		RemainingTokens: int64(math.Round(b.tokens)),
		Message:         "",
		AllowedHits:     entry.Allowed,
		DeniedHits:      entry.Denied,
		Algorithm:       "token_bucket",
	}
	if !entry.LastAllowedAt.IsZero() {
		res.LastAllowedAt = entry.LastAllowedAt.Format(time.RFC3339Nano)
	}
	if !entry.LastDeniedAt.IsZero() {
		res.LastDeniedAt = entry.LastDeniedAt.Format(time.RFC3339Nano)
	}
	if !allowed {
		res.Message = "rate limit exceeded"
	}
	return res, nil
}

// AllowFromBytes decodes a binary request and executes Allow.
func (r *RateLimiter) AllowFromBytes(ctx context.Context, payload []byte) ([]byte, error) {
	var req pbcodec.AllowRequest
	if err := pbcodec.UnmarshalAllowRequest(payload, &req); err != nil {
		return nil, err
	}
	res, err := r.Allow(ctx, &req)
	if err != nil {
		return nil, err
	}
	return pbcodec.MarshalAllowResponse(res), nil
}

func (r *RateLimiter) ensureBucketLocked(key string, cfg *storage.RateLimitConfig, now time.Time) *bucket {
	b, ok := r.buckets[key]
	if !ok {
		b = &bucket{capacity: cfg.MaxTokens, tokens: float64(cfg.MaxTokens), refillRate: cfg.RefillRate, lastRefill: now}
		r.buckets[key] = b
		return b
	}
	if b.capacity != cfg.MaxTokens {
		b.capacity = cfg.MaxTokens
		if b.tokens > float64(b.capacity) {
			b.tokens = float64(b.capacity)
		}
	}
	b.refillRate = cfg.RefillRate
	if now.After(b.lastRefill) && b.refillRate > 0 {
		elapsed := now.Sub(b.lastRefill).Seconds()
		b.tokens += elapsed * b.refillRate
		if b.tokens > float64(b.capacity) {
			b.tokens = float64(b.capacity)
		}
		b.lastRefill = now
	}
	return b
}

func (r *RateLimiter) consumeTokens(b *bucket, tokens float64, now time.Time) bool {
	if b.tokens >= tokens {
		b.tokens -= tokens
		return true
	}
	// allow one last refill check to see if underflow can be recovered
	if b.refillRate > 0 && now.After(b.lastRefill) {
		elapsed := now.Sub(b.lastRefill).Seconds()
		b.tokens += elapsed * b.refillRate
		if b.tokens > float64(b.capacity) {
			b.tokens = float64(b.capacity)
		}
		b.lastRefill = now
	}
	if b.tokens >= tokens {
		b.tokens -= tokens
		return true
	}
	return false
}

func (r *RateLimiter) ensureStatsLocked(key, source string) *statsEntry {
	if source == "" {
		source = "unknown"
	}
	k := fmt.Sprintf("%s|%s", key, source)
	entry, ok := r.stats[k]
	if !ok {
		entry = &statsEntry{Key: key, Source: source}
		r.stats[k] = entry
	}
	return entry
}

// StatsTable returns current counters by key and source.
func (r *RateLimiter) StatsTable() []StatsRow {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows := make([]StatsRow, 0, len(r.stats))
	for _, s := range r.stats {
		rows = append(rows, StatsRow{
			Key:             s.Key,
			Source:          s.Source,
			Allowed:         s.Allowed,
			Denied:          s.Denied,
			LastAllowedAt:   s.LastAllowedAt,
			LastDeniedAt:    s.LastDeniedAt,
			LastRequestAt:   s.LastRequestAt,
			RemainingTokens: s.Remaining,
		})
	}
	return rows
}

// DebugState exposes the buckets for debugging endpoints.
func (r *RateLimiter) DebugState() map[string]*bucket {
	r.mu.Lock()
	defer r.mu.Unlock()
	snapshot := make(map[string]*bucket, len(r.buckets))
	for k, v := range r.buckets {
		copy := *v
		snapshot[k] = &copy
	}
	return snapshot
}

func normaliseSource(src string) string {
	return strings.TrimSpace(src)
}
