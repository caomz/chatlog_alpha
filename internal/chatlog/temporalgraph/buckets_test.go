package temporalgraph

import (
	"testing"
	"time"

	"github.com/sjzar/chatlog/internal/chatlog/conf"
)

// TestClassifyFailedErrorCoversAllHABuckets locks the public bucket
// vocabulary. Every name listed in the HA-004 AC must be reachable by
// ClassifyFailedError so /api/v1/graph/status can render a stable
// summary. Adding a new bucket requires updating this test in lockstep.
func TestClassifyFailedErrorCoversAllHABuckets(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		// AC1 recoverable classes
		{name: "config_error_exact", input: "chat model is not configured", want: "config_error"},
		{name: "config_error_semantic_missing", input: "semantic config missing", want: "config_error"},
		// AC1 non-retryable classes
		{name: "sensitive_input_1026_message", input: "minimax chat error: input new_sensitive (1026)", want: "sensitive_input_1026"},
		{name: "sensitive_input_1026_token", input: "minimax_sensitive_1026", want: "sensitive_input_1026"},
		{name: "sensitive_output_1027_message", input: "minimax chat error: output new_sensitive (1027)", want: "sensitive_output_1027"},
		{name: "sensitive_output_1027_token", input: "minimax_sensitive_1027", want: "sensitive_output_1027"},
		// AC1 other known classes
		{name: "json_decode_error_extraction", input: "decode graph extraction failed: no json object found", want: "json_decode_error"},
		{name: "json_decode_error_verification", input: "decode graph verification failed: invalid JSON", want: "json_decode_error"},
		{name: "json_decode_error_model_response", input: "decode model response failed: <nil>", want: "json_decode_error"},
		{name: "empty_graph", input: "temporal graph extraction produced no reliable graph results", want: "empty_graph"},
		{name: "network_timeout_context_deadline", input: "minimax chat failed: context deadline exceeded (Client.Timeout)", want: "network_timeout"},
		{name: "network_timeout_eof", input: "unexpected EOF", want: "network_timeout"},
		{name: "before_request_timeout", input: "minimax chat failed before request: i/o timeout", want: "before_request_timeout"},
		{name: "rate_limited_429", input: "minimax chat failed: model http 429", want: "rate_limited"},
		{name: "rate_limited_token", input: "minimax_rate_limited", want: "rate_limited"},
		{name: "auth_error_401", input: "minimax chat failed: model http 401", want: "auth_error"},
		{name: "auth_error_403", input: "minimax chat failed: model http 403", want: "auth_error"},
		{name: "non_retryable_422", input: "minimax chat failed: model http 422", want: "non_retryable_request"},
		// AC1 negative: unknown error stays unclassified
		{name: "unclassified_empty", input: "", want: ""},
		{name: "unclassified_unknown", input: "totally unrelated telemetry dump", want: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyFailedError(tc.input); got != tc.want {
				t.Fatalf("ClassifyFailedError(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestAllFailedBucketNamesIsStableAndComplete(t *testing.T) {
	want := []string{
		"config_error",
		"network_timeout",
		"before_request_timeout",
		"rate_limited",
		"auth_error",
		"json_decode_error",
		"empty_graph",
		"sensitive_input_1026",
		"sensitive_output_1027",
		"non_retryable_request",
	}
	got := AllFailedBucketNames()
	if len(got) != len(want) {
		t.Fatalf("AllFailedBucketNames length = %d, want %d (%v)", len(got), len(want), got)
	}
	for i, name := range want {
		if got[i] != name {
			t.Fatalf("AllFailedBucketNames[%d] = %q, want %q", i, got[i], name)
		}
	}
}

// TestRequeueFailedSourcesByBucketRequeuesOnlyMatchingBucket ensures
// the per-bucket requeue path is precise — the AC requires that
// /api/v1/graph/resume moves only the 4 recoverable buckets and leaves
// non-recoverable classes (notably sensitive_1026/1027) untouched.
func TestRequeueFailedSourcesByBucketRequeuesOnlyMatchingBucket(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	cases := []struct {
		name      string
		sourceID  string
		errText   string
		expectPen int
		expectFai int
	}{
		{name: "config_error", sourceID: "biz:cfg", errText: "chat model is not configured"},
		{name: "network_timeout", sourceID: "biz:net", errText: "minimax chat failed: context deadline exceeded"},
		{name: "before_request_timeout", sourceID: "biz:before", errText: "minimax chat failed before request: i/o timeout"},
		{name: "rate_limited", sourceID: "biz:rl", errText: "minimax chat failed: model http 429"},
		{name: "auth_error", sourceID: "biz:auth", errText: "minimax chat failed: model http 401"},
		{name: "json_decode_error", sourceID: "biz:dec", errText: "decode graph extraction failed: no json object found"},
		{name: "sensitive_input_1026", sourceID: "biz:s1026", errText: "minimax chat error: input new_sensitive (1026)"},
		{name: "sensitive_output_1027", sourceID: "biz:s1027", errText: "minimax chat error: output new_sensitive (1027)"},
		{name: "non_retryable_request", sourceID: "biz:422", errText: "minimax chat failed: model http 422"},
		{name: "empty_graph", sourceID: "biz:empty", errText: "temporal graph extraction produced no reliable graph results"},
		{name: "unclassified", sourceID: "biz:unknown", errText: "no bucket tokens match this"},
	}
	ids := make(map[string]int64, len(cases))
	for _, c := range cases {
		id, _, err := store.UpsertSource(SourceRecord{
			SourceID: c.sourceID, SourceType: "business", EventType: "ticket",
			Content: "fixture", EventTime: time.Now(),
		})
		if err != nil {
			t.Fatalf("UpsertSource(%s) error = %v", c.sourceID, err)
		}
		ids[c.sourceID] = id
		if err := store.MarkSource(id, "failed", c.errText); err != nil {
			t.Fatalf("MarkSource(%s) error = %v", c.sourceID, err)
		}
	}

	// Requeue only the rate_limited bucket — everything else must
	// stay in failed.
	n, err := store.RequeueFailedSourcesByBucket("rate_limited")
	if err != nil {
		t.Fatalf("RequeueFailedSourcesByBucket(rate_limited) error = %v", err)
	}
	if n != 1 {
		t.Fatalf("RequeueFailedSourcesByBucket(rate_limited) affected %d, want 1", n)
	}
	// Unknown buckets must be a no-op.
	n, err = store.RequeueFailedSourcesByBucket("does_not_exist")
	if err != nil {
		t.Fatalf("RequeueFailedSourcesByBucket(does_not_exist) error = %v", err)
	}
	if n != 0 {
		t.Fatalf("RequeueFailedSourcesByBucket(does_not_exist) affected %d, want 0", n)
	}

	st := store.Status(false, false, "")
	if st.Pending != 1 {
		t.Fatalf("expected 1 pending after requeue, got %d", st.Pending)
	}
	if st.Failed != len(cases)-1 {
		t.Fatalf("expected %d failed to remain, got %d", len(cases)-1, st.Failed)
	}
	// sensitive_1026/1027 must remain failed (AC3).
	for _, sensitive := range []string{"biz:s1026", "biz:s1027"} {
		var status string
		if err := store.db.QueryRow(`SELECT status FROM graph_source_records WHERE id=?`, ids[sensitive]).Scan(&status); err != nil {
			t.Fatalf("read status for %s: %v", sensitive, err)
		}
		if status != "failed" {
			t.Fatalf("%s expected to remain failed, got %q", sensitive, status)
		}
	}
}

func TestRequeueFailedSourcesByBucketIsCaseInsensitive(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	id, _, err := store.UpsertSource(SourceRecord{
		SourceID: "biz:case", SourceType: "business", EventType: "ticket",
		Content: "fixture", EventTime: time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	if err := store.MarkSource(id, "failed", "MODEL HTTP 429 from upstream"); err != nil {
		t.Fatalf("MarkSource() error = %v", err)
	}
	n, err := store.RequeueFailedSourcesByBucket("rate_limited")
	if err != nil {
		t.Fatalf("RequeueFailedSourcesByBucket() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("RequeueFailedSourcesByBucket affected %d, want 1 (case-insensitive match)", n)
	}
}

func TestFailedBucketCountsReportsAllBuckets(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	fixtures := []struct {
		sourceID string
		errText  string
	}{
		{"biz:1", "chat model is not configured"},
		{"biz:2", "chat model is not configured"},
		{"biz:3", "minimax chat failed: context deadline exceeded"},
		{"biz:4", "minimax chat failed before request: i/o timeout"},
		{"biz:5", "minimax chat failed: model http 429"},
		{"biz:6", "minimax chat error: input new_sensitive (1026)"},
		{"biz:7", "minimax chat error: output new_sensitive (1027)"},
		{"biz:8", "decode graph extraction failed: no json object found"},
		{"biz:9", "minimax chat failed: model http 401"},
		{"biz:10", "minimax chat failed: model http 422"},
		{"biz:11", "temporal graph extraction produced no reliable graph results"},
		{"biz:12", "some random text that classifies nowhere"},
	}
	for _, c := range fixtures {
		id, _, err := store.UpsertSource(SourceRecord{
			SourceID: c.sourceID, SourceType: "business", EventType: "ticket",
			Content: "fixture", EventTime: time.Now(),
		})
		if err != nil {
			t.Fatalf("UpsertSource(%s) error = %v", c.sourceID, err)
		}
		if err := store.MarkSource(id, "failed", c.errText); err != nil {
			t.Fatalf("MarkSource(%s) error = %v", c.sourceID, err)
		}
	}

	counts, err := store.FailedBucketCounts()
	if err != nil {
		t.Fatalf("FailedBucketCounts() error = %v", err)
	}
	// Every known bucket must be present.
	for _, name := range AllFailedBucketNames() {
		if _, ok := counts[name]; !ok {
			t.Fatalf("FailedBucketCounts missing bucket %q", name)
		}
	}
	if counts["config_error"] != 2 {
		t.Fatalf("config_error = %d, want 2", counts["config_error"])
	}
	if counts["network_timeout"] != 1 {
		t.Fatalf("network_timeout = %d, want 1", counts["network_timeout"])
	}
	if counts["before_request_timeout"] != 1 {
		t.Fatalf("before_request_timeout = %d, want 1", counts["before_request_timeout"])
	}
	if counts["rate_limited"] != 1 {
		t.Fatalf("rate_limited = %d, want 1", counts["rate_limited"])
	}
	if counts["sensitive_input_1026"] != 1 {
		t.Fatalf("sensitive_input_1026 = %d, want 1", counts["sensitive_input_1026"])
	}
	if counts["sensitive_output_1027"] != 1 {
		t.Fatalf("sensitive_output_1027 = %d, want 1", counts["sensitive_output_1027"])
	}
	if counts["json_decode_error"] != 1 {
		t.Fatalf("json_decode_error = %d, want 1", counts["json_decode_error"])
	}
	if counts["auth_error"] != 1 {
		t.Fatalf("auth_error = %d, want 1", counts["auth_error"])
	}
	if counts["non_retryable_request"] != 1 {
		t.Fatalf("non_retryable_request = %d, want 1", counts["non_retryable_request"])
	}
	if counts["empty_graph"] != 1 {
		t.Fatalf("empty_graph = %d, want 1", counts["empty_graph"])
	}
	if counts["unclassified"] != 1 {
		t.Fatalf("unclassified = %d, want 1", counts["unclassified"])
	}
	if totalFailed := counts["config_error"] + counts["network_timeout"] +
		counts["before_request_timeout"] + counts["rate_limited"] +
		counts["auth_error"] + counts["json_decode_error"] +
		counts["sensitive_input_1026"] + counts["sensitive_output_1027"] +
		counts["non_retryable_request"] + counts["empty_graph"] +
		counts["unclassified"]; totalFailed != 12 {
		t.Fatalf("sum of buckets = %d, want 12", totalFailed)
	}
}

// TestManagerRequeueRecoverableOnlyMovesRecoverableBuckets exercises the
// manager-level invariant required by HA-004 AC2 + AC3: a Resume() call
// must requeue config_error, network_timeout, before_request_timeout
// and rate_limited, while keeping sensitive_1026/1027 and the other
// non-recoverable classes in failed.
func TestManagerRequeueRecoverableOnlyMovesRecoverableBuckets(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	fixtures := []struct {
		sourceID  string
		errText   string
		expectRec bool
	}{
		{sourceID: "biz:cfg-recover", errText: "chat model is not configured", expectRec: true},
		{sourceID: "biz:net-recover", errText: "minimax chat failed: context deadline exceeded", expectRec: true},
		{sourceID: "biz:before-recover", errText: "minimax chat failed before request: i/o timeout", expectRec: true},
		{sourceID: "biz:rl-recover", errText: "minimax chat failed: model http 429", expectRec: true},
		{sourceID: "biz:auth-stays", errText: "minimax chat failed: model http 401", expectRec: false},
		{sourceID: "biz:dec-stays", errText: "decode graph extraction failed: no json object found", expectRec: false},
		{sourceID: "biz:s1026-stays", errText: "minimax chat error: input new_sensitive (1026)", expectRec: false},
		{sourceID: "biz:s1027-stays", errText: "minimax chat error: output new_sensitive (1027)", expectRec: false},
		{sourceID: "biz:422-stays", errText: "minimax chat failed: model http 422", expectRec: false},
		{sourceID: "biz:empty-stays", errText: "temporal graph extraction produced no reliable graph results", expectRec: false},
	}
	statusByID := make(map[string]int64, len(fixtures))
	for _, c := range fixtures {
		id, _, err := store.UpsertSource(SourceRecord{
			SourceID: c.sourceID, SourceType: "business", EventType: "ticket",
			Content: "fixture", EventTime: time.Now(),
		})
		if err != nil {
			t.Fatalf("UpsertSource(%s) error = %v", c.sourceID, err)
		}
		statusByID[c.sourceID] = id
		if err := store.MarkSource(id, "failed", c.errText); err != nil {
			t.Fatalf("MarkSource(%s) error = %v", c.sourceID, err)
		}
	}

	m := &Manager{
		conf: testGraphConfig{semantic: conf.NormalizeSemanticConfig(conf.SemanticConfig{
			Enabled:      true,
			ChatProvider: conf.ProviderMMX,
			ChatModel:    conf.DefaultMMXChat,
		})},
		store: store,
	}

	if err := m.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}

	// Re-check status of every fixture source to confirm class membership.
	for _, c := range fixtures {
		var status string
		if err := store.db.QueryRow(`SELECT status FROM graph_source_records WHERE id=?`, statusByID[c.sourceID]).Scan(&status); err != nil {
			t.Fatalf("read status for %s: %v", c.sourceID, err)
		}
		want := "failed"
		if c.expectRec {
			want = "pending"
		}
		if status != want {
			t.Fatalf("%s (err=%q) status = %q, want %q", c.sourceID, c.errText, status, want)
		}
	}
	// Top-level counts must match the 4-recoverable / 6-non-recoverable split.
	st := store.Status(false, false, "")
	if st.Pending != 4 {
		t.Fatalf("Pending = %d, want 4 (recoverable buckets)", st.Pending)
	}
	if st.Failed != 6 {
		t.Fatalf("Failed = %d, want 6 (non-recoverable buckets)", st.Failed)
	}
	// Buckets summary must report the same distribution without exposing
	// any source payload.
	counts, err := store.FailedBucketCounts()
	if err != nil {
		t.Fatalf("FailedBucketCounts() error = %v", err)
	}
	if counts["sensitive_input_1026"] != 1 || counts["sensitive_output_1027"] != 1 {
		t.Fatalf("sensitive buckets should stay at 1 each, got s1026=%d s1027=%d",
			counts["sensitive_input_1026"], counts["sensitive_output_1027"])
	}
}
