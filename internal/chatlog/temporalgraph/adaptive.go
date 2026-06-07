package temporalgraph

import (
	"strings"
	"sync"
	"time"
)

// keyHealthTracker tracks a short rolling window of upstream Chat errors that
// signal key-pool pressure. It is consumed by adaptiveMaxWorkers to decide
// whether the temporal graph worker pool should hold 5-way concurrency, drop
// to a safer mid-tier, or fall back to single-key recovery mode.
//
// The tracker is intentionally read from manager.processOne and written from
// SetError paths so tests can drive it deterministically without going through
// the real MiniMax client.
type keyHealthTracker struct {
	mu sync.Mutex

	// windowDuration is the rolling window used to count "spike" buckets.
	windowDuration time.Duration
	// recoveryGrace is the quiet time required before degraded/critical can
	// climb back toward stable.
	recoveryGrace time.Duration

	// spikeBuckets names any error bucket that should count toward a "key
	// pressure" spike. Sensitive and decode buckets are deliberately
	// excluded so we never throttle throughput to hide content safety or
	// parser issues — those have to be solved with prompt/parser changes,
	// not worker scaling.
	spikeBuckets []string

	// spikeDowngradeAt is the number of spike observations inside the
	// window that triggers a downgrade from stable to degraded.
	spikeDowngradeAt int
	// spikeCriticalAt is the number of spike observations that escalates
	// degraded into critical (workers=1).
	spikeCriticalAt int

	events []keyHealthEvent
	last   time.Time
}

type keyHealthEvent struct {
	at     time.Time
	bucket string
}

// newKeyHealthTracker returns a tracker configured for runtime defaults.
// Values are conservative: a handful of timeouts in 60s should not move the
// pool below 3, but a sustained 8+ should not let the pool run at 5 either.
//
// recoveryGrace is intentionally larger than windowDuration so the cap can
// stay at a lower tier briefly even after the rolling window has cleared,
// which prevents oscillation when spikes arrive in clusters spaced just
// outside the 60s window.
func newKeyHealthTracker() *keyHealthTracker {
	return &keyHealthTracker{
		windowDuration:    60 * time.Second,
		recoveryGrace:     90 * time.Second,
		spikeDowngradeAt:  3,
		spikeCriticalAt:   8,
		spikeBuckets: []string{
			"minimax_rate_limited",
			"minimax_timeout",
			"minimax_before_request_timeout",
		},
	}
}

// Observe records an upstream error bucket. Empty buckets and non-spike
// buckets are ignored so callers can pass classifyMiniMaxErrorBucket output
// without filtering first.
func (t *keyHealthTracker) Observe(bucket string, at time.Time) {
	if t == nil {
		return
	}
	bucket = strings.TrimSpace(bucket)
	if bucket == "" {
		return
	}
	if !t.isSpikeBucket(bucket) {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.events = append(t.events, keyHealthEvent{at: at, bucket: bucket})
	t.last = at
}

// QuietDuration returns the time since the most recent spike observation.
// Stable windows (QuietDuration >= recoveryGrace) let the adaptive logic
// climb back to configured concurrency.
func (t *keyHealthTracker) QuietDuration(now time.Time) time.Duration {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.last.IsZero() {
		return now.Sub(time.Time{})
	}
	return now.Sub(t.last)
}

// SpikeCount returns how many spike observations are inside the rolling
// window ending at now. Expired events are pruned on read so the slice
// never grows unbounded across long uptimes.
func (t *keyHealthTracker) SpikeCount(now time.Time) int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	cutoff := now.Add(-t.windowDuration)
	kept := t.events[:0]
	count := 0
	for _, ev := range t.events {
		if ev.at.After(cutoff) || ev.at.Equal(cutoff) {
			kept = append(kept, ev)
			count++
		}
	}
	t.events = kept
	return count
}

// adaptiveLevel describes how aggressive the worker pool should be. The
// numeric "max workers" cap is computed by adaptiveMaxWorkers which clamps
// the cap to the configured key count and the global maxGraphWorkers limit.
type adaptiveLevel int

const (
	adaptiveStable   adaptiveLevel = iota // cap = stableCap (5)
	adaptiveDegraded                      // cap = degradedCap (3)
	adaptiveCritical                      // cap = criticalCap (1)
)

const (
	adaptiveStableCap   = 5
	adaptiveDegradedCap = 3
	adaptiveCriticalCap = 1
)

// currentLevel decides the current adaptive level based on the spike count
// in the rolling window and the recovery grace relative to the most recent
// spike observation. Recovery is asymmetric: moving to a lower tier is
// instant so we never run 5 workers through a throttled key pool, but
// moving to a higher tier is gated by recoveryGrace so we don't oscillate
// during a brief quiet patch.
//
// The rolling window itself trims spikes older than windowDuration. Once
// the spike count drops below spikeDowngradeAt we are technically eligible
// to climb, but the cap stays at the previous lower tier for an additional
// recoveryGrace period so a single quiet second does not bounce us back to
// stable. Only after BOTH "no spikes in the rolling window" AND "we have
// been quiet for at least recoveryGrace" do we return to adaptiveStable.
func (t *keyHealthTracker) currentLevel(now time.Time, previous adaptiveLevel) adaptiveLevel {
	if t == nil {
		return adaptiveStable
	}
	count := t.SpikeCount(now)
	if count >= t.spikeCriticalAt {
		return adaptiveCritical
	}
	if count >= t.spikeDowngradeAt {
		return adaptiveDegraded
	}
	if previous == adaptiveDegraded || previous == adaptiveCritical {
		// No new spikes in the rolling window. We need an additional quiet
		// period of recoveryGrace after the most recent spike before we
		// climb back up.
		if t.QuietDuration(now) < t.recoveryGrace {
			return previous
		}
	}
	return adaptiveStable
}

func (t *keyHealthTracker) isSpikeBucket(bucket string) bool {
	for _, b := range t.spikeBuckets {
		if b == bucket {
			return true
		}
	}
	return false
}

// adaptiveMaxWorkers returns the effective worker cap for the current tick,
// clamped to both the configured key count and the global maxGraphWorkers
// ceiling. configuredKeys==0 means "no key pool signal available" so we
// fall back to 1 to avoid spinning up workers without any key to use.
//
// userSetWorkers is the value the operator persisted via SetWorkers (or
// defaulted in NewManager). The adaptive cap will never exceed that
// user-set ceiling — it only narrows it.
//
// previous is the level from the prior tick. It is used to gate recovery
// so that the cap only climbs back up after recoveryGrace has elapsed
// without new spike observations. Downgrades are always immediate.
func adaptiveMaxWorkers(t *keyHealthTracker, configuredKeys, userSetWorkers int, previous adaptiveLevel, now time.Time) (int, adaptiveLevel) {
	level := adaptiveStable
	if t != nil {
		level = t.currentLevel(now, previous)
	}
	cap := adaptiveStableCap
	switch level {
	case adaptiveCritical:
		cap = adaptiveCriticalCap
	case adaptiveDegraded:
		cap = adaptiveDegradedCap
	}
	// Upper bound protection: never exceed the smaller of the configured key
	// count, the global maxGraphWorkers, and the user-requested workers
	// (which is itself already clamped to maxGraphWorkers inside SetWorkers).
	upper := configuredKeys
	if upper <= 0 || upper > maxGraphWorkers {
		upper = maxGraphWorkers
	}
	if userSetWorkers > 0 && userSetWorkers < upper {
		upper = userSetWorkers
	}
	if cap > upper {
		cap = upper
	}
	if cap < 1 {
		cap = 1
	}
	return cap, level
}
