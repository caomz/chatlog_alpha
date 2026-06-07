package temporalgraph

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sjzar/chatlog/internal/chatlog/conf"
	"github.com/sjzar/chatlog/internal/chatlog/semantic"
)

// fakeInvoker is a deterministic ChatInvoker that returns canned responses
// keyed by cfg.ChatModel. It records every call so tests can inspect
// routing/usage.
type fakeInvoker struct {
	responses map[string][]string // model -> sequence of responses
	errs      map[string]error    // model -> terminal error (returns instead of responses)
	calls     []fakeCall
}

type fakeCall struct {
	Model string
	Stage string
}

func newFakeInvoker() *fakeInvoker {
	return &fakeInvoker{
		responses: map[string][]string{},
		errs:      map[string]error{},
	}
}

func (f *fakeInvoker) Chat(ctx context.Context, cfg conf.SemanticConfig, messages []semantic.ChatMessage) (string, error) {
	model := cfg.ChatModel
	if err, ok := f.errs[model]; ok {
		return "", err
	}
	queue := f.responses[model]
	if len(queue) == 0 {
		return "", nil
	}
	// Detect a decode-retry by looking at the user message content.
	stage := "first"
	for _, m := range messages {
		if strings.Contains(m.Content, "上一次输出不是合法 JSON") {
			stage = "retry"
		}
	}
	f.calls = append(f.calls, fakeCall{Model: model, Stage: stage})
	out := queue[0]
	f.responses[model] = queue[1:]
	return out, nil
}

// validExtractionJSON returns a minimal, schema-valid JSON extraction.
const validExtractionJSON = `{"entities":[{"name":"T-Alpha","type":"person","confidence":0.9}],"relations":[],"events":[],"facts":[{"statement":"A 报告 B","confidence":0.8}]}`

// validExtractionRichJSON includes entities, facts, events, relations.
const validExtractionRichJSON = `{
  "entities":[
    {"name":"T-Alpha","type":"person","confidence":0.9},
    {"name":"ProjectAtlas","type":"project","confidence":0.8}
  ],
  "relations":[
    {"subject":"T-Alpha","predicate":"responsible_for","object":"ProjectAtlas","time_text":"下周","change_type":"created","status":"active","confidence":0.7,"evidence":"ProjectAtlas 计划"}
  ],
  "events":[
    {"event_type":"meeting","title":"评审","summary":"开产品评审","time_text":"明天下午三点","actors":["T-Alpha","T-Beta"],"targets":[],"confidence":0.7,"evidence":"明天下午三点开产品评审"}
  ],
  "facts":[
    {"statement":"明天下午三点开产品评审","time_text":"明天下午三点","change_type":"observed","status":"active","confidence":0.7,"evidence":"明天下午三点开产品评审"}
  ]
}`

func TestBenchmarkRunnerRequiresNonNilInvoker(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for nil invoker")
		}
	}()
	NewBenchmarkRunner(nil)
}

func TestBenchmarkRunnerRunsAllFixtureCategories(t *testing.T) {
	inv := newFakeInvoker()
	// Both models always return valid rich JSON.
	for _, m := range []string{"MiniMax-M2.7", "m2.1"} {
		for i := 0; i < len(benchmarkFixtures()); i++ {
			inv.responses[m] = append(inv.responses[m], validExtractionRichJSON)
		}
	}
	runner := NewBenchmarkRunner(inv)
	metrics := runner.RunBenchmark(context.Background(), conf.SemanticConfig{ChatProvider: conf.ProviderMMX}, "MiniMax-M2.7")
	if metrics.SampleCount != len(benchmarkFixtures()) {
		t.Fatalf("sample count = %d, want %d", metrics.SampleCount, len(benchmarkFixtures()))
	}
	if metrics.JSONValidRate < 0.99 {
		t.Fatalf("json_valid_rate = %v, want ~1.0", metrics.JSONValidRate)
	}
	if metrics.NonEmptyGraphRate < 0.99 {
		t.Fatalf("non_empty_graph_rate = %v, want ~1.0", metrics.NonEmptyGraphRate)
	}
	if metrics.DecodeRetryRate != 0 {
		t.Fatalf("decode_retry_rate = %v, want 0", metrics.DecodeRetryRate)
	}
	if metrics.AvgLatencyMS < 0 {
		t.Fatalf("avg_latency_ms must be non-negative, got %v", metrics.AvgLatencyMS)
	}
	wantCategories := map[string]int{
		benchmarkCatShort:     1,
		benchmarkCatEllipsis:  1,
		benchmarkCatTime:      1,
		benchmarkCatMultiParty: 1,
		benchmarkCatBusiness:  1,
		benchmarkCatRelation:  1,
		benchmarkCatEmpty:     1,
	}
	for k, v := range wantCategories {
		if metrics.CategoryBreakdown[k] != v {
			t.Fatalf("category %q count = %d, want %d", k, metrics.CategoryBreakdown[k], v)
		}
	}
}

func TestBenchmarkRunnerCountsDecodeRetryWhenFirstInvalid(t *testing.T) {
	inv := newFakeInvoker()
	// All fixtures: first response is invalid (not JSON), retry is valid.
	for i := 0; i < len(benchmarkFixtures()); i++ {
		inv.responses["m2.1"] = append(inv.responses["m2.1"], "this is not JSON at all")
		inv.responses["m2.1"] = append(inv.responses["m2.1"], validExtractionJSON)
	}
	runner := NewBenchmarkRunner(inv)
	metrics := runner.RunBenchmark(context.Background(), conf.SemanticConfig{ChatProvider: conf.ProviderMMX}, "m2.1")
	if metrics.SampleCount != len(benchmarkFixtures()) {
		t.Fatalf("sample count = %d, want %d", metrics.SampleCount, len(benchmarkFixtures()))
	}
	if metrics.JSONValidRate < 0.99 {
		t.Fatalf("json_valid_rate = %v, want ~1.0 after retries", metrics.JSONValidRate)
	}
	if metrics.DecodeRetryRate < 0.99 {
		t.Fatalf("decode_retry_rate = %v, want ~1.0", metrics.DecodeRetryRate)
	}
}

func TestBenchmarkRunnerTreatsNetworkErrorAsInvalid(t *testing.T) {
	inv := newFakeInvoker()
	inv.errs["m2.1"] = errFakeNetwork
	runner := NewBenchmarkRunner(inv)
	metrics := runner.RunBenchmark(context.Background(), conf.SemanticConfig{ChatProvider: conf.ProviderMMX}, "m2.1")
	if metrics.JSONValidRate != 0 {
		t.Fatalf("json_valid_rate = %v, want 0 on network error", metrics.JSONValidRate)
	}
	if metrics.NonEmptyGraphRate != 0 {
		t.Fatalf("non_empty_graph_rate = %v, want 0 on network error", metrics.NonEmptyGraphRate)
	}
}

func TestBenchmarkRunnerFixtureCoversAllRequiredCategories(t *testing.T) {
	want := map[string]bool{
		benchmarkCatShort: false,
		benchmarkCatEllipsis: false,
		benchmarkCatTime: false,
		benchmarkCatMultiParty: false,
		benchmarkCatBusiness: false,
		benchmarkCatRelation: false,
		benchmarkCatEmpty: false,
	}
	for _, fx := range benchmarkFixtures() {
		if _, ok := want[fx.Category]; !ok {
			t.Fatalf("fixture category %q is not in the required allow-list", fx.Category)
		}
		want[fx.Category] = true
	}
	for k, seen := range want {
		if !seen {
			t.Fatalf("fixture category %q is required by AC#1 but missing", k)
		}
	}
}

func TestBenchmarkRunnerFixtureHasNoRealChatContent(t *testing.T) {
	for _, fx := range benchmarkFixtures() {
		if strings.Contains(fx.Source.Content, "wxid_") {
			t.Fatalf("fixture %s contains wxid_*, should be redacted", fx.Category)
		}
		if strings.Contains(fx.Source.Content, "13800") {
			t.Fatalf("fixture %s contains phone-like digits, should be redacted", fx.Category)
		}
	}
}

func TestBenchmarkRunnerFixtureHasVariedShape(t *testing.T) {
	short, ellipsis, time, multi, biz, rel, empty := 0, 0, 0, 0, 0, 0, 0
	for _, fx := range benchmarkFixtures() {
		switch fx.Category {
		case benchmarkCatShort:
			short++
		case benchmarkCatEllipsis:
			ellipsis++
		case benchmarkCatTime:
			time++
		case benchmarkCatMultiParty:
			multi++
		case benchmarkCatBusiness:
			biz++
		case benchmarkCatRelation:
			rel++
		case benchmarkCatEmpty:
			empty++
		}
	}
	if short == 0 || ellipsis == 0 || time == 0 || multi == 0 || biz == 0 || rel == 0 || empty == 0 {
		t.Fatalf("fixture missing required category: short=%d ellipsis=%d time=%d multi=%d biz=%d rel=%d empty=%d",
			short, ellipsis, time, multi, biz, rel, empty)
	}
}

func TestBenchmarkComparisonProducesRequiredDeltas(t *testing.T) {
	inv := newFakeInvoker()
	n := len(benchmarkFixtures())
	// Baseline: 2/7 invalid, 3/7 empty graph, 2/7 valid.
	baselineResponses := []string{
		validExtractionRichJSON,   // short
		validExtractionRichJSON,   // ellipsis
		"not json at all",         // time
		`{"entities":[],"relations":[],"events":[],"facts":[]}`, // multi-party
		`{"entities":[],"relations":[],"events":[],"facts":[]}`, // business
		`{"entities":[],"relations":[],"events":[],"facts":[]}`, // relation
		validExtractionJSON,       // empty
	}
	for _, r := range baselineResponses {
		inv.responses["MiniMax-M2.7"] = append(inv.responses["MiniMax-M2.7"], r)
	}
	// Candidate: 1/7 invalid, 1/7 empty graph, 5/7 valid (better than baseline).
	candidateResponses := []string{
		validExtractionRichJSON,
		validExtractionRichJSON,
		validExtractionRichJSON,
		validExtractionRichJSON,
		validExtractionRichJSON,
		"not json at all",
		`{"entities":[],"relations":[],"events":[],"facts":[]}`,
	}
	for _, r := range candidateResponses {
		inv.responses["m2.1"] = append(inv.responses["m2.1"], r)
	}
	// Retries for the invalid cases.
	inv.responses["MiniMax-M2.7"] = append(inv.responses["MiniMax-M2.7"], validExtractionJSON)
	inv.responses["m2.1"] = append(inv.responses["m2.1"], validExtractionJSON)
	_ = n

	runner := NewBenchmarkRunner(inv)
	baseline, candidate, cmp := runner.RunBenchmarkComparison(
		context.Background(),
		conf.SemanticConfig{ChatProvider: conf.ProviderMMX},
		"MiniMax-M2.7",
		"m2.1",
	)
	if baseline.SampleCount != len(benchmarkFixtures()) {
		t.Fatalf("baseline sample count = %d, want %d", baseline.SampleCount, len(benchmarkFixtures()))
	}
	if candidate.SampleCount != len(benchmarkFixtures()) {
		t.Fatalf("candidate sample count = %d, want %d", candidate.SampleCount, len(benchmarkFixtures()))
	}
	if cmp.BaselineModel != "MiniMax-M2.7" || cmp.CandidateModel != "m2.1" {
		t.Fatalf("comparison models = (%q, %q), want (MiniMax-M2.7, m2.1)", cmp.BaselineModel, cmp.CandidateModel)
	}
	if cmp.JSONValidRateDelta < 0 {
		t.Fatalf("candidate should have higher json_valid_rate (delta=%v)", cmp.JSONValidRateDelta)
	}
}

func TestEvaluateFastPathGatePassesWhenCandidateMeetsBaseline(t *testing.T) {
	dec := EvaluateFastPathGate(BenchmarkComparison{
		JSONValidRateDelta: 0.05,
		NonEmptyGraphDelta: 0.10,
	})
	if !dec.AllowFastPath {
		t.Fatalf("expected AllowFastPath=true, got %+v", dec)
	}
	if !dec.JSONValidPassed || !dec.NonEmptyPassed {
		t.Fatalf("expected both checks passed, got %+v", dec)
	}
}

func TestEvaluateFastPathGateRejectsOnJSONValidRegression(t *testing.T) {
	dec := EvaluateFastPathGate(BenchmarkComparison{
		JSONValidRateDelta: -0.05, // > 2pp below baseline
		NonEmptyGraphDelta: 0.10,
	})
	if dec.AllowFastPath {
		t.Fatalf("expected AllowFastPath=false, got %+v", dec)
	}
	if dec.JSONValidPassed {
		t.Fatalf("expected JSONValidPassed=false, got %+v", dec)
	}
	if !dec.NonEmptyPassed {
		t.Fatalf("expected NonEmptyPassed=true, got %+v", dec)
	}
}

func TestEvaluateFastPathGateRejectsOnNonEmptyRegression(t *testing.T) {
	dec := EvaluateFastPathGate(BenchmarkComparison{
		JSONValidRateDelta: 0.05,
		NonEmptyGraphDelta: -0.10, // > 5pp below baseline
	})
	if dec.AllowFastPath {
		t.Fatalf("expected AllowFastPath=false, got %+v", dec)
	}
	if dec.NonEmptyPassed {
		t.Fatalf("expected NonEmptyPassed=false, got %+v", dec)
	}
	if !dec.JSONValidPassed {
		t.Fatalf("expected JSONValidPassed=true, got %+v", dec)
	}
}

func TestEvaluateFastPathGateAtBoundaryAllows(t *testing.T) {
	// Exactly at the boundary is allowed.
	dec := EvaluateFastPathGate(BenchmarkComparison{
		JSONValidRateDelta: -JSONValidRateGap,
		NonEmptyGraphDelta: -NonEmptyGraphRateGap,
	})
	if !dec.AllowFastPath {
		t.Fatalf("expected AllowFastPath=true at boundary, got %+v", dec)
	}
}

func TestEvaluateFastPathGateSlightlyPastBoundaryRejects(t *testing.T) {
	dec := EvaluateFastPathGate(BenchmarkComparison{
		JSONValidRateDelta: -JSONValidRateGap - 0.005,
		NonEmptyGraphDelta: 0,
	})
	if dec.AllowFastPath {
		t.Fatalf("expected AllowFastPath=false when past boundary, got %+v", dec)
	}
}

func TestEvaluateFastPathGateReasonMentionsBothFailures(t *testing.T) {
	dec := EvaluateFastPathGate(BenchmarkComparison{
		JSONValidRateDelta: -0.10,
		NonEmptyGraphDelta: -0.20,
	})
	if dec.AllowFastPath {
		t.Fatal("expected rejection")
	}
	if !strings.Contains(dec.Reason, "json_valid_rate") {
		t.Fatalf("reason should mention json_valid_rate, got %q", dec.Reason)
	}
	if !strings.Contains(dec.Reason, "non_empty_graph_rate") {
		t.Fatalf("reason should mention non_empty_graph_rate, got %q", dec.Reason)
	}
}

func TestPickChatModelBaselineForKeySource(t *testing.T) {
	cfg := BulkRoutingConfig{
		BaselineModel:  "MiniMax-M2.7",
		FastPathModel:  "m2.1",
		AllowFastPath:  true,
	}
	if got := PickChatModel(cfg, true, false); got != "MiniMax-M2.7" {
		t.Fatalf("key source should use baseline, got %q", got)
	}
}

func TestPickChatModelBaselineForDecodeError(t *testing.T) {
	cfg := BulkRoutingConfig{
		BaselineModel:  "MiniMax-M2.7",
		FastPathModel:  "m2.1",
		AllowFastPath:  true,
	}
	if got := PickChatModel(cfg, false, true); got != "MiniMax-M2.7" {
		t.Fatalf("decode error should use baseline, got %q", got)
	}
}

func TestPickChatModelFastPathOnlyWhenAllowed(t *testing.T) {
	cfg := BulkRoutingConfig{
		BaselineModel:  "MiniMax-M2.7",
		FastPathModel:  "m2.1",
		AllowFastPath:  false,
	}
	if got := PickChatModel(cfg, false, false); got != "MiniMax-M2.7" {
		t.Fatalf("disallowed fast path should fall back to baseline, got %q", got)
	}

	cfg.AllowFastPath = true
	if got := PickChatModel(cfg, false, false); got != "m2.1" {
		t.Fatalf("allowed fast path should use fast path model, got %q", got)
	}
}

func TestPickChatModelEmptyBaselineReturnsEmpty(t *testing.T) {
	if got := PickChatModel(BulkRoutingConfig{}, false, false); got != "" {
		t.Fatalf("empty baseline should return empty model, got %q", got)
	}
}

func TestBenchmarkMetricsHasAllRequiredFields(t *testing.T) {
	// Compile-time check via struct literal — if a field is renamed/removed,
	// this test fails to compile.
	metrics := BenchmarkMetrics{
		Model:              "m2.1",
		SampleCount:        1,
		JSONValidRate:      1.0,
		NonEmptyGraphRate:  0.5,
		EntityCountDelta:   0,
		FactCountDelta:     0,
		EventCountDelta:    0,
		RelationCountDelta: 0,
		DecodeRetryRate:    0,
		AvgLatencyMS:       12.3,
	}
	_ = metrics
}

func TestBenchmarkRunnerDoesNotPanicOnEmptyModel(t *testing.T) {
	inv := newFakeInvoker()
	runner := NewBenchmarkRunner(inv)
	// Empty model is normalized to provider default, so the runner still
	// proceeds but the invoker will simply not be called for the empty case.
	metrics := runner.RunBenchmark(context.Background(), conf.SemanticConfig{ChatProvider: conf.ProviderMMX}, "")
	if metrics.SampleCount != len(benchmarkFixtures()) {
		t.Fatalf("sample count = %d, want %d", metrics.SampleCount, len(benchmarkFixtures()))
	}
}

func TestBenchmarkRunnerTracksLatency(t *testing.T) {
	inv := newFakeInvoker()
	for i := 0; i < len(benchmarkFixtures()); i++ {
		inv.responses["m2.1"] = append(inv.responses["m2.1"], validExtractionJSON)
	}
	runner := NewBenchmarkRunner(inv)
	start := time.Now()
	metrics := runner.RunBenchmark(context.Background(), conf.SemanticConfig{ChatProvider: conf.ProviderMMX}, "m2.1")
	elapsed := time.Since(start)
	if metrics.AvgLatencyMS < 0 {
		t.Fatalf("avg_latency_ms must be non-negative, got %v", metrics.AvgLatencyMS)
	}
	// Sanity bound: the fake should be much faster than 1 second per call.
	if metrics.AvgLatencyMS > 1000 {
		t.Fatalf("avg_latency_ms suspiciously large: %v (elapsed=%v)", metrics.AvgLatencyMS, elapsed)
	}
}

// errFakeNetwork is a stand-in upstream error used by tests.
var errFakeNetwork = &fakeError{msg: "fake network error"}

type fakeError struct{ msg string }

func (e *fakeError) Error() string { return e.msg }
