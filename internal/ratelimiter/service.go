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
const (
	strategyTokenBucket     = "token_bucket"
	strategyLeakyBucket     = "leaky_bucket"
	strategySlidingWindow   = "sliding_window"
	defaultStrategyReason   = "balanced throughput"
	burstStrategyReason     = "absorbing burst"
	sustainedStrategyReason = "sustained flow smoothing"
)

type StatsRow struct {
	Key             string    `json:"key"`
	Source          string    `json:"source"`
	Allowed         int64     `json:"allowed"`
	Denied          int64     `json:"denied"`
	LastAllowedAt   time.Time `json:"last_allowed_at,omitempty"`
	LastDeniedAt    time.Time `json:"last_denied_at,omitempty"`
	LastRequestAt   time.Time `json:"last_request_at,omitempty"`
	RemainingTokens float64   `json:"remaining_tokens"`
	Algorithm       string    `json:"algorithm"`
	StrategyReason  string    `json:"strategy_reason"`
	BurstScore      float64   `json:"burst_score"`
	SustainScore    float64   `json:"sustain_score"`
	MeanInterval    float64   `json:"mean_interval"`
	IntervalStdDev  float64   `json:"interval_stddev"`
}

type bucket struct {
	Capacity       int64
	Tokens         float64
	RefillRate     float64
	LastRefill     time.Time
	Version        uint64
	Algorithm      string
	StrategyReason string
	Backlog        float64
	LastLeak       time.Time
	WindowCount    int64
	WindowStart    time.Time
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
	Algorithm       string
	StrategyReason  string
	MeanInterval    float64
	IntervalVar     float64
	BurstScore      float64
	SustainScore    float64
	LastSwitch      time.Time
}

type syncEvent struct {
	Key            string      `json:"key"`
	Tokens         float64     `json:"tokens"`
	Capacity       int64       `json:"capacity"`
	RefillRate     float64     `json:"refill_rate"`
	LastRefill     time.Time   `json:"last_refill"`
	Version        uint64      `json:"version"`
	Algorithm      string      `json:"algorithm"`
	StrategyReason string      `json:"strategy_reason"`
	Backlog        float64     `json:"backlog"`
	LastLeak       time.Time   `json:"last_leak"`
	WindowCount    int64       `json:"window_count"`
	WindowStart    time.Time   `json:"window_start"`
	Stats          *statsEvent `json:"stats,omitempty"`
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
	Algorithm       string    `json:"algorithm"`
	StrategyReason  string    `json:"strategy_reason"`
	MeanInterval    float64   `json:"mean_interval"`
	IntervalVar     float64   `json:"interval_variance"`
	BurstScore      float64   `json:"burst_score"`
	SustainScore    float64   `json:"sustain_score"`
	LastSwitch      time.Time `json:"last_switch"`
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
	entry := r.ensureStatsLocked(req.Key, source)

	if !entry.LastRequestAt.IsZero() {
		interval := now.Sub(entry.LastRequestAt)
		r.updateFlowMetrics(entry, interval)
	}
	entry.LastRequestAt = now

	r.applyStrategyLocked(b, entry, now)

	allowed, remaining := r.applyAlgorithmLocked(b, float64(tokens), now)

	entry.RemainingTokens = remaining
	entry.Algorithm = b.Algorithm
	entry.StrategyReason = b.StrategyReason
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

	remainingInt := int64(math.Floor(remaining))
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
		Algorithm:       b.Algorithm,
		StrategyReason:  b.StrategyReason,
	}

	evt := &syncEvent{
		Key:            req.Key,
		Tokens:         b.Tokens,
		Capacity:       b.Capacity,
		RefillRate:     b.RefillRate,
		LastRefill:     b.LastRefill,
		Version:        seq,
		Algorithm:      b.Algorithm,
		StrategyReason: b.StrategyReason,
		Backlog:        b.Backlog,
		LastLeak:       b.LastLeak,
		WindowCount:    b.WindowCount,
		WindowStart:    b.WindowStart,
		Stats: &statsEvent{
			Source:          source,
			Allowed:         entry.Allowed,
			Denied:          entry.Denied,
			LastAllowedAt:   entry.LastAllowedAt,
			LastDeniedAt:    entry.LastDeniedAt,
			LastRequestAt:   entry.LastRequestAt,
			RemainingTokens: entry.RemainingTokens,
			Version:         entry.Version,
			Algorithm:       entry.Algorithm,
			StrategyReason:  entry.StrategyReason,
			MeanInterval:    entry.MeanInterval,
			IntervalVar:     entry.IntervalVar,
			BurstScore:      entry.BurstScore,
			SustainScore:    entry.SustainScore,
			LastSwitch:      entry.LastSwitch,
		},
	}
	r.mu.Unlock()

	r.broadcast(evt)
	r.logDecision(req.Key, source, tokens, capacity, float64(refill), evt, res)

	return res, nil
}

func (r *RateLimiter) updateFlowMetrics(entry *statsEntry, interval time.Duration) {
	if interval <= 0 {
		interval = 1 * time.Millisecond
	}
	seconds := interval.Seconds()
	const alpha = 0.25
	if entry.MeanInterval == 0 {
		entry.MeanInterval = seconds
	} else {
		entry.MeanInterval = (1-alpha)*entry.MeanInterval + alpha*seconds
	}
	diff := seconds - entry.MeanInterval
	entry.IntervalVar = (1 - alpha) * (entry.IntervalVar + alpha*diff*diff)

	if seconds < 0.2 {
		entry.BurstScore = clamp01(entry.BurstScore*0.7 + 0.3)
	} else {
		entry.BurstScore = clamp01(entry.BurstScore * 0.7)
	}
	if seconds < 1 {
		entry.SustainScore = clamp01(entry.SustainScore*0.8 + 0.2)
	} else {
		entry.SustainScore = clamp01(entry.SustainScore * 0.7)
	}
	total := entry.Allowed + entry.Denied
	if total > 0 && entry.Denied > 0 {
		pressure := float64(entry.Denied) / float64(total)
		entry.BurstScore = clamp01(entry.BurstScore * (1 + pressure*0.2))
		entry.SustainScore = clamp01(entry.SustainScore * (1 - pressure*0.1))
	}
}

func (r *RateLimiter) applyStrategyLocked(b *bucket, entry *statsEntry, now time.Time) {
	if b.Algorithm == "" {
		b.Algorithm = strategyTokenBucket
		b.StrategyReason = defaultStrategyReason
		resetAlgorithmState(b, now)
	}
	if entry.Algorithm == "" {
		entry.Algorithm = b.Algorithm
		entry.StrategyReason = b.StrategyReason
	}

	if now.Sub(entry.LastSwitch) < 3*time.Second {
		return
	}

	variance := math.Sqrt(math.Max(entry.IntervalVar, 0))
	total := entry.Allowed + entry.Denied
	var denialRate float64
	if total > 0 {
		denialRate = float64(entry.Denied) / float64(total)
	}
	desired := strategyTokenBucket
	reason := defaultStrategyReason

	switch {
	case entry.BurstScore > 0.65 && (variance > 0.2 || denialRate > 0.3):
		desired = strategyLeakyBucket
		reason = burstStrategyReason
	case entry.SustainScore > 0.65 && variance < 0.15:
		desired = strategySlidingWindow
		reason = sustainedStrategyReason
	default:
		desired = strategyTokenBucket
		reason = defaultStrategyReason
	}

	if desired != b.Algorithm {
		b.Algorithm = desired
		b.StrategyReason = reason
		entry.LastSwitch = now
		resetAlgorithmState(b, now)
	} else {
		b.StrategyReason = reason
	}

	entry.Algorithm = b.Algorithm
	entry.StrategyReason = b.StrategyReason
}

func (r *RateLimiter) applyAlgorithmLocked(b *bucket, tokens float64, now time.Time) (bool, float64) {
	switch b.Algorithm {
	case strategyLeakyBucket:
		return applyLeakyBucket(b, tokens, now)
	case strategySlidingWindow:
		return applySlidingWindow(b, tokens, now)
	default:
		return applyTokenBucket(b, tokens)
	}
}

func applyTokenBucket(b *bucket, tokens float64) (bool, float64) {
	if b.Tokens >= tokens {
		b.Tokens -= tokens
		if b.Tokens < 0 {
			b.Tokens = 0
		}
		return true, b.Tokens
	}
	if b.Tokens < 0 {
		b.Tokens = 0
	}
	return false, b.Tokens
}

func applyLeakyBucket(b *bucket, tokens float64, now time.Time) (bool, float64) {
	if b.LastLeak.IsZero() {
		b.LastLeak = now
	}
	if now.After(b.LastLeak) {
		elapsed := now.Sub(b.LastLeak).Seconds()
		leaked := elapsed * b.RefillRate
		if leaked > 0 {
			b.Backlog -= leaked
			if b.Backlog < 0 {
				b.Backlog = 0
			}
			b.LastLeak = now
		}
	}

	projected := b.Backlog + tokens
	if projected <= float64(b.Capacity) {
		b.Backlog = projected
		remaining := float64(b.Capacity) - b.Backlog
		if remaining < 0 {
			remaining = 0
		}
		b.Tokens = remaining
		return true, remaining
	}

	remaining := float64(b.Capacity) - b.Backlog
	if remaining < 0 {
		remaining = 0
	}
	b.Tokens = remaining
	return false, remaining
}

func applySlidingWindow(b *bucket, tokens float64, now time.Time) (bool, float64) {
	window := strategyWindowDuration(b)
	if b.WindowStart.IsZero() {
		b.WindowStart = now
	}
	if now.Sub(b.WindowStart) >= window {
		b.WindowStart = now
		b.WindowCount = 0
	}
	if tokens <= 0 {
		remaining := float64(b.Capacity - b.WindowCount)
		if remaining < 0 {
			remaining = 0
		}
		b.Tokens = remaining
		return true, remaining
	}
	increment := int64(math.Ceil(tokens))
	if increment < 1 {
		increment = 1
	}
	if b.WindowCount+increment <= b.Capacity {
		b.WindowCount += increment
		remaining := float64(b.Capacity - b.WindowCount)
		if remaining < 0 {
			remaining = 0
		}
		b.Tokens = remaining
		return true, remaining
	}
	remaining := float64(b.Capacity - b.WindowCount)
	if remaining < 0 {
		remaining = 0
	}
	b.Tokens = remaining
	return false, remaining
}

func resetAlgorithmState(b *bucket, now time.Time) {
	switch b.Algorithm {
	case strategyLeakyBucket:
		if b.Tokens > float64(b.Capacity) {
			b.Tokens = float64(b.Capacity)
		}
		b.Backlog = float64(b.Capacity) - b.Tokens
		if b.Backlog < 0 {
			b.Backlog = 0
		}
		b.LastLeak = now
		b.WindowCount = 0
		b.WindowStart = now
	case strategySlidingWindow:
		if b.Tokens > float64(b.Capacity) {
			b.Tokens = float64(b.Capacity)
		}
		b.WindowStart = now
		b.WindowCount = 0
		b.Backlog = 0
		b.LastLeak = now
	default:
		if b.Tokens > float64(b.Capacity) {
			b.Tokens = float64(b.Capacity)
		}
		b.LastRefill = now
		b.LastLeak = now
		b.Backlog = 0
		b.WindowCount = 0
		b.WindowStart = now
	}
}

func clamp01(v float64) float64 {
	switch {
	case v < 0:
		return 0
	case v > 1:
		return 1
	default:
		return v
	}
}

func strategyWindowDuration(b *bucket) time.Duration {
	base := time.Second
	if b.RefillRate > 0 {
		ratio := float64(b.Capacity) / b.RefillRate
		candidate := time.Duration(ratio * float64(time.Second))
		if candidate < 200*time.Millisecond {
			candidate = 200 * time.Millisecond
		}
		if candidate > 5*time.Second {
			candidate = 5 * time.Second
		}
		base = candidate
	}
	return base
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
			Algorithm:       entry.Algorithm,
			StrategyReason:  entry.StrategyReason,
			BurstScore:      entry.BurstScore,
			SustainScore:    entry.SustainScore,
			MeanInterval:    entry.MeanInterval,
			IntervalStdDev:  math.Sqrt(math.Max(entry.IntervalVar, 0)),
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
			"capacity":        b.Capacity,
			"tokens":          b.Tokens,
			"refill_rate":     b.RefillRate,
			"last_refill":     b.LastRefill.Format(time.RFC3339Nano),
			"version":         b.Version,
			"algorithm":       b.Algorithm,
			"strategy_reason": b.StrategyReason,
			"backlog":         b.Backlog,
			"last_leak":       b.LastLeak.Format(time.RFC3339Nano),
			"window_count":    b.WindowCount,
			"window_start":    b.WindowStart.Format(time.RFC3339Nano),
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
			Capacity:       capacity,
			Tokens:         float64(capacity),
			RefillRate:     refill,
			LastRefill:     now,
			Algorithm:      strategyTokenBucket,
			StrategyReason: defaultStrategyReason,
			LastLeak:       now,
			WindowStart:    now,
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
	switch b.Algorithm {
	case strategyLeakyBucket:
		applyLeakyBucket(b, 0, now)
	case strategySlidingWindow:
		applySlidingWindow(b, 0, now)
	default:
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
		entry = &statsEntry{Key: key, Source: source, Algorithm: strategyTokenBucket, StrategyReason: defaultStrategyReason}
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
		if evt.Algorithm != "" {
			existing.Algorithm = evt.Algorithm
		}
		if evt.StrategyReason != "" {
			existing.StrategyReason = evt.StrategyReason
		}
		existing.Backlog = evt.Backlog
		if !evt.LastLeak.IsZero() {
			existing.LastLeak = evt.LastLeak
		}
		existing.WindowCount = evt.WindowCount
		if !evt.WindowStart.IsZero() {
			existing.WindowStart = evt.WindowStart
		}
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
			if evt.Stats.Algorithm != "" {
				entry.Algorithm = evt.Stats.Algorithm
			}
			if evt.Stats.StrategyReason != "" {
				entry.StrategyReason = evt.Stats.StrategyReason
			}
			if evt.Stats.MeanInterval != 0 {
				entry.MeanInterval = evt.Stats.MeanInterval
			}
			entry.IntervalVar = evt.Stats.IntervalVar
			entry.BurstScore = evt.Stats.BurstScore
			entry.SustainScore = evt.Stats.SustainScore
			if !evt.Stats.LastSwitch.IsZero() {
				entry.LastSwitch = evt.Stats.LastSwitch
			}
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
		"decision=%s key=%s source=%s tokens=%d capacity=%d refill_rate=%.2f remaining=%.2f allowed_hits=%d denied_hits=%d version=%d algorithm=%s reason=%q",
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
		evt.Algorithm,
		evt.StrategyReason,
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
