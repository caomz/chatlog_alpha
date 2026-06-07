package temporalgraph

import (
	"strings"
	"testing"
	"time"

	"github.com/sjzar/chatlog/internal/chatlog/conf"
)

// TestAdaptiveMaxWorkersStableHealthyPool covers AC#1: when the key pool is
// healthy (no spike observations) and the operator requested 5 workers, the
// effective cap stays at 5.
func TestAdaptiveMaxWorkersStableHealthyPool(t *testing.T) {
	tracker := newKeyHealthTracker()
	cap, level := adaptiveMaxWorkers(tracker, 5, 5, adaptiveStable, time.Now())
	if cap != 5 {
		t.Fatalf("expected stable cap 5, got %d", cap)
	}
	if level != adaptiveStable {
		t.Fatalf("expected adaptiveStable, got %v", level)
	}
}

// TestAdaptiveMaxWorkersDowngradesOnRateLimitSpike covers AC#2: a handful of
// rate-limit observations inside the rolling window must drop the cap below
// 5 and be readable from the public Status surface.
func TestAdaptiveMaxWorkersDowngradesOnRateLimitSpike(t *testing.T) {
	tracker := newKeyHealthTracker()
	now := time.Now()
	for i := 0; i < 4; i++ {
		tracker.Observe("minimax_rate_limited", now.Add(time.Duration(i)*time.Second))
	}
	cap, level := adaptiveMaxWorkers(tracker, 5, 5, adaptiveStable, now.Add(10*time.Second))
	if cap != adaptiveDegradedCap {
		t.Fatalf("expected degraded cap %d, got %d (level=%v)", adaptiveDegradedCap, cap, level)
	}
	if level != adaptiveDegraded {
		t.Fatalf("expected adaptiveDegraded, got %v", level)
	}
}

// TestAdaptiveMaxWorkersDowngradesOnTimeoutSpike covers AC#2 for the timeout
// bucket: context deadline exceeded / before request observations also push
// the cap to the degraded tier.
func TestAdaptiveMaxWorkersDowngradesOnTimeoutSpike(t *testing.T) {
	tracker := newKeyHealthTracker()
	now := time.Now()
	for i := 0; i < 3; i++ {
		tracker.Observe("minimax_timeout", now.Add(time.Duration(i)*time.Second))
	}
	tracker.Observe("minimax_before_request_timeout", now.Add(5*time.Second))
	cap, level := adaptiveMaxWorkers(tracker, 5, 5, adaptiveStable, now.Add(10*time.Second))
	if cap != adaptiveDegradedCap {
		t.Fatalf("expected degraded cap %d on mixed timeout spike, got %d (level=%v)", adaptiveDegradedCap, cap, level)
	}
	if level != adaptiveDegraded {
		t.Fatalf("expected adaptiveDegraded, got %v", level)
	}
}

// TestAdaptiveMaxWorkersEscalatesToCriticalOnSustainedSpike covers the
// upper-half of AC#2: 8+ spike observations in 60s should clamp workers to 1.
func TestAdaptiveMaxWorkersEscalatesToCriticalOnSustainedSpike(t *testing.T) {
	tracker := newKeyHealthTracker()
	now := time.Now()
	for i := 0; i < 9; i++ {
		tracker.Observe("minimax_rate_limited", now.Add(time.Duration(i)*time.Second))
	}
	cap, level := adaptiveMaxWorkers(tracker, 5, 5, adaptiveStable, now.Add(15*time.Second))
	if cap != adaptiveCriticalCap {
		t.Fatalf("expected critical cap %d, got %d (level=%v)", adaptiveCriticalCap, cap, level)
	}
	if level != adaptiveCritical {
		t.Fatalf("expected adaptiveCritical, got %v", level)
	}
}

// TestAdaptiveMaxWorkersRecoversAfterQuietWindow covers AC#3: after the
// rolling window has cleared (no spikes in the last 60s) and the recovery
// grace has elapsed since the last observation, the cap climbs back to
// stable even if the operator never changed SetWorkers.
func TestAdaptiveMaxWorkersRecoversAfterQuietWindow(t *testing.T) {
	tracker := newKeyHealthTracker()
	// Place 3 spikes far enough in the past that they will be out of the
	// rolling window for both the mid and later ticks. The last spike
	// anchor is what recovery grace counts from.
	spikeAt := time.Now().Add(-75 * time.Second)
	for i := 0; i < 3; i++ {
		tracker.Observe("minimax_rate_limited", spikeAt.Add(time.Duration(i)*time.Second))
	}
	// First tick immediately: spikes are out-of-window but quiet duration
	// is < recoveryGrace → cap must stay degraded.
	cap, level := adaptiveMaxWorkers(tracker, 5, 5, adaptiveDegraded, time.Now())
	if cap != adaptiveDegradedCap {
		t.Fatalf("expected cap to stay degraded inside recovery grace, got %d (level=%v)", cap, level)
	}
	if level != adaptiveDegraded {
		t.Fatalf("expected adaptiveDegraded inside recovery grace, got %v", level)
	}
	// Second tick: spikes still out of window AND recovery grace has fully
	// elapsed since the last observation → cap must climb back to stable.
	laterTick := spikeAt.Add(tracker.recoveryGrace + 5*time.Second)
	cap, level = adaptiveMaxWorkers(tracker, 5, 5, adaptiveDegraded, laterTick)
	if cap != 5 {
		t.Fatalf("expected recovery to stable cap 5, got %d (level=%v)", cap, level)
	}
	if level != adaptiveStable {
		t.Fatalf("expected adaptiveStable after recovery, got %v", level)
	}
}

// TestAdaptiveMaxWorkersUpperBoundRespectsConfiguredKeyCount covers AC#4:
// the effective cap must never exceed the configured MiniMax key count.
func TestAdaptiveMaxWorkersUpperBoundRespectsConfiguredKeyCount(t *testing.T) {
	tracker := newKeyHealthTracker()
	// Stable state, but only 3 keys configured → cap must clamp to 3.
	cap, level := adaptiveMaxWorkers(tracker, 3, 5, adaptiveStable, time.Now())
	if cap != 3 {
		t.Fatalf("expected cap clamped to configured key count 3, got %d", cap)
	}
	if level != adaptiveStable {
		t.Fatalf("expected adaptiveStable when only bounded by keys, got %v", level)
	}
}

// TestAdaptiveMaxWorkersUpperBoundRespectsUserSetWorkers covers AC#4 from
// the operator side: when SetWorkers was called with a value below the
// configured key count, the cap must not exceed that operator ceiling.
func TestAdaptiveMaxWorkersUpperBoundRespectsUserSetWorkers(t *testing.T) {
	tracker := newKeyHealthTracker()
	// User requested 2 workers explicitly → cap must clamp to 2 even with
	// 5 healthy keys available.
	cap, _ := adaptiveMaxWorkers(tracker, 5, 2, adaptiveStable, time.Now())
	if cap != 2 {
		t.Fatalf("expected cap to respect user-set 2 workers, got %d", cap)
	}
}

// TestAdaptiveMaxWorkersUpperBoundRespectsMaxGraphWorkers covers AC#4 for
// the global ceiling: the cap must never exceed maxGraphWorkers (12) even
// if a future operator asks for 50.
func TestAdaptiveMaxWorkersUpperBoundRespectsMaxGraphWorkers(t *testing.T) {
	tracker := newKeyHealthTracker()
	cap, _ := adaptiveMaxWorkers(tracker, 50, 50, adaptiveStable, time.Now())
	if cap > maxGraphWorkers {
		t.Fatalf("expected cap <= maxGraphWorkers(%d), got %d", maxGraphWorkers, cap)
	}
	// Also verify the smaller user-set ceiling trumps the key count.
	cap, _ = adaptiveMaxWorkers(tracker, 0, 50, adaptiveStable, time.Now())
	if cap > maxGraphWorkers {
		t.Fatalf("expected cap <= maxGraphWorkers(%d) when keys=0, got %d", maxGraphWorkers, cap)
	}
}

// TestAdaptiveMaxWorkersIgnoresNonSpikeBuckets makes sure sensitive, decode,
// and config buckets do NOT trigger worker downgrades — they are prompt or
// parser problems, not key-pool pressure.
func TestAdaptiveMaxWorkersIgnoresNonSpikeBuckets(t *testing.T) {
	tracker := newKeyHealthTracker()
	now := time.Now()
	// Push 20 non-spike observations to make sure none of them move the cap.
	for i := 0; i < 20; i++ {
		tracker.Observe("minimax_sensitive_1026", now.Add(time.Duration(i)*time.Second))
		tracker.Observe("minimax_decode_error", now.Add(time.Duration(i)*time.Second))
		tracker.Observe("minimax_auth_error", now.Add(time.Duration(i)*time.Second))
		tracker.Observe("minimax_config_error", now.Add(time.Duration(i)*time.Second))
	}
	cap, level := adaptiveMaxWorkers(tracker, 5, 5, adaptiveStable, now.Add(25*time.Second))
	if cap != 5 {
		t.Fatalf("non-spike buckets must not throttle workers, got cap=%d", cap)
	}
	if level != adaptiveStable {
		t.Fatalf("non-spike buckets must keep level=stable, got %v", level)
	}
}

// TestAdaptiveMaxWorkersPrunesExpiredEvents ensures the tracker does not
// grow unbounded across long uptimes: events older than windowDuration
// must drop out of the spike count.
func TestAdaptiveMaxWorkersPrunesExpiredEvents(t *testing.T) {
	tracker := newKeyHealthTracker()
	now := time.Now()
	// Plant spikes older than the window.
	for i := 0; i < 5; i++ {
		tracker.Observe("minimax_rate_limited", now.Add(-2*time.Minute-time.Duration(i)*time.Second))
	}
	// After 2 minutes have passed, the cap must not be affected.
	cap, level := adaptiveMaxWorkers(tracker, 5, 5, adaptiveStable, now)
	if cap != 5 {
		t.Fatalf("expired events must not affect cap, got %d (level=%v)", cap, level)
	}
	if level != adaptiveStable {
		t.Fatalf("expired events must not downgrade level, got %v", level)
	}
}

// TestKeyHealthTrackerSpikeCountKeepsOnlyWindow makes the rolling-window
// pruning behavior explicit and easy to audit.
func TestKeyHealthTrackerSpikeCountKeepsOnlyWindow(t *testing.T) {
	tracker := newKeyHealthTracker()
	now := time.Now()
	// 2 events in-window, 3 events out-of-window.
	tracker.Observe("minimax_rate_limited", now.Add(-10*time.Second))
	tracker.Observe("minimax_rate_limited", now.Add(-5*time.Second))
	tracker.Observe("minimax_rate_limited", now.Add(-90*time.Second))
	tracker.Observe("minimax_rate_limited", now.Add(-2*time.Minute))
	tracker.Observe("minimax_rate_limited", now.Add(-3*time.Minute))
	if got := tracker.SpikeCount(now); got != 2 {
		t.Fatalf("SpikeCount = %d, want 2 (only in-window events)", got)
	}
}

// TestBucketFromErrorClassifiesSpikeTokens pins the substring contract used
// by the processOne failure path: future upstream error string changes
// should surface here first.
func TestBucketFromErrorClassifiesSpikeTokens(t *testing.T) {
	cases := []struct {
		name   string
		errMsg string
		want   string
	}{
		{"rate limit 429", "minimax chat failed: model http 429: rate limit", "minimax_rate_limited"},
		{"context deadline", "minimax chat failed: context deadline exceeded", "minimax_timeout"},
		{"before request", "minimax chat failed before request: timeout", "minimax_before_request_timeout"},
		{"empty", "", ""},
		{"nil-like", "some other error", ""},
		{"sensitive is not spike", "minimax output new_sensitive (1027) blocked", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var err error
			if tc.errMsg != "" {
				err = &bucketTestError{msg: tc.errMsg}
			}
			if got := bucketFromError(err); got != tc.want {
				t.Fatalf("bucketFromError(%q) = %q, want %q", tc.errMsg, got, tc.want)
			}
		})
	}
}

// TestStatusReportsEffectiveWorkersAndAdaptiveLevel makes sure the
// /api/v1/graph/status JSON path exposes the adaptive signal so operators
// can confirm the cap is actually being applied at runtime.
func TestStatusReportsEffectiveWorkersAndAdaptiveLevel(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()
	m := &Manager{
		conf: testGraphConfig{semantic: conf.NormalizeSemanticConfig(conf.SemanticConfig{
			Enabled:      true,
			ChatProvider: conf.ProviderMMX,
			ChatModel:    conf.DefaultMMXChat,
		})},
		store:             store,
		workers:           5,
		lastEffectiveWorkers: 3,
		adaptiveLevel:     adaptiveDegraded,
		tracker:           newKeyHealthTracker(),
	}
	st := m.Status()
	if st.EffectiveWorkers != 3 {
		t.Fatalf("expected effective_workers=3, got %d", st.EffectiveWorkers)
	}
	if st.AdaptiveLevel != "degraded" {
		t.Fatalf("expected adaptive_level=degraded, got %q", st.AdaptiveLevel)
	}
	if st.Workers != 5 {
		t.Fatalf("expected user-set workers=5 to be reported separately, got %d", st.Workers)
	}
}

// TestStatusFallsBackToUserSetWhenAdaptiveNotYetPrimed covers the first
// /api/v1/graph/status call right after process startup, before any
// ProcessPending tick has run.
func TestStatusFallsBackToUserSetWhenAdaptiveNotYetPrimed(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()
	m := &Manager{
		conf: testGraphConfig{semantic: conf.NormalizeSemanticConfig(conf.SemanticConfig{
			Enabled:      true,
			ChatProvider: conf.ProviderMMX,
			ChatModel:    conf.DefaultMMXChat,
		})},
		store:   store,
		workers: 5,
		tracker: newKeyHealthTracker(),
	}
	st := m.Status()
	if st.EffectiveWorkers != 5 {
		t.Fatalf("expected fallback to user-set 5 when not yet primed, got %d", st.EffectiveWorkers)
	}
	if st.AdaptiveLevel != "stable" {
		t.Fatalf("expected stable fallback, got %q", st.AdaptiveLevel)
	}
}

// bucketTestError is a tiny error type that just carries a string, used so
// bucketFromError tests can drive the substring path with a known message.
type bucketTestError struct {
	msg string
}

func (e *bucketTestError) Error() string { return e.msg }

// Sanity check: the bucket strings we assert on are exactly the tokens the
// tracker also recognizes. If one drifts, this guard fails first.
func TestTrackerAndBucketFromErrorAgreeOnTokens(t *testing.T) {
	tracker := newKeyHealthTracker()
	for _, bucket := range adaptiveKeyErrorBuckets {
		tracker.Observe(bucket, time.Now())
	}
	if got := tracker.SpikeCount(time.Now()); got != len(adaptiveKeyErrorBuckets) {
		t.Fatalf("tracker spike count = %d, want %d", got, len(adaptiveKeyErrorBuckets))
	}
	// And each recognized upstream error must surface as one of those tokens
	// through bucketFromError.
	for _, want := range []string{
		"minimax chat failed: model http 429: limited",
		"minimax chat failed: context deadline exceeded",
		"minimax chat failed before request: lease timeout",
	} {
		bucket := bucketFromError(&bucketTestError{msg: want})
		if !strings.Contains(bucket, "minimax_") {
			t.Fatalf("bucketFromError(%q) = %q, expected a minimax_* spike token", want, bucket)
		}
	}
}
