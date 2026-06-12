package temporalgraph

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sjzar/chatlog/internal/chatlog/conf"
	"github.com/sjzar/chatlog/internal/chatlog/semantic"
)

func TestDigestNormalWindow(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()

	mgr := &Manager{store: store}

	// Synthesize test events with known actors/targets.
	now := time.Now()
	windowStart := now.Add(-1 * time.Hour)

	// Event 1: actors=[alice, bob], targets=[charlie]
	actorsJSON1, _ := json.Marshal([]string{"alice", "bob"})
	targetsJSON1, _ := json.Marshal([]string{"charlie"})
	_, err = store.db.Exec(`
INSERT INTO graph_events (event_time, title, summary, actors_json, targets_json, source_record_id, created_at, confidence)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, windowStart.Unix()+100, "Event 1", "Summary 1", string(actorsJSON1), string(targetsJSON1), 1, time.Now().Unix(), 0.9)
	if err != nil {
		t.Fatalf("Insert event 1 failed: %v", err)
	}

	// Event 2: actors=[alice, charlie], targets=[dave]
	actorsJSON2, _ := json.Marshal([]string{"alice", "charlie"})
	targetsJSON2, _ := json.Marshal([]string{"dave"})
	_, err = store.db.Exec(`
INSERT INTO graph_events (event_time, title, summary, actors_json, targets_json, source_record_id, created_at, confidence)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, windowStart.Unix()+200, "Event 2", "Summary 2", string(actorsJSON2), string(targetsJSON2), 2, time.Now().Unix(), 0.8)
	if err != nil {
		t.Fatalf("Insert event 2 failed: %v", err)
	}

	// Insert some facts and relations in the window.
	_, err = store.db.Exec(`
INSERT INTO graph_facts (fact_key, statement, canonical_statement, valid_from, source_record_id, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
	`, "fact1", "Alice is a person", "alice is a person", windowStart.Unix()+150, 1, time.Now().Unix())
	if err != nil {
		t.Fatalf("Insert fact failed: %v", err)
	}

	_, err = store.db.Exec(`
INSERT INTO graph_relations (subject_entity_id, object_entity_id, predicate, valid_from, last_seen, updated_at)
VALUES (?, ?, ?, ?, ?, ?)
	`, 1, 2, "knows", windowStart.Unix()+175, windowStart.Unix()+175, time.Now().Unix())
	if err != nil {
		t.Fatalf("Insert relation failed: %v", err)
	}

	// Digest the window.
	result, err := mgr.Digest(windowStart, now.Add(1*time.Hour), DigestOptions{
		MaxEntities:   20,
		MaxEvents:     50,
		MaxEventsScan: 2000,
	})
	if err != nil {
		t.Fatalf("Digest failed: %v", err)
	}

	// Verify no truncation.
	if result.Truncated {
		t.Errorf("Expected Truncated=false, got true")
	}

	// Verify entity counts: alice=2, bob=1, charlie=2, dave=1.
	entityMap := make(map[string]int)
	for _, e := range result.TopEntities {
		entityMap[e.Name] = e.Count
	}

	expectedCounts := map[string]int{"alice": 2, "bob": 1, "charlie": 2, "dave": 1}
	for entity, expectedCount := range expectedCounts {
		if got := entityMap[entity]; got != expectedCount {
			t.Errorf("Entity %q: expected count %d, got %d", entity, expectedCount, got)
		}
	}

	// Verify fact and relation counts.
	if result.FactCount < 1 {
		t.Errorf("Expected FactCount >= 1, got %d", result.FactCount)
	}
	if result.RelationCount < 1 {
		t.Errorf("Expected RelationCount >= 1, got %d", result.RelationCount)
	}
}

func TestDigestEmptyWindow(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()

	mgr := &Manager{store: store}

	// Query an empty time window.
	result, err := mgr.Digest(time.Now().Add(-24*time.Hour), time.Now().Add(-23*time.Hour), DigestOptions{})
	if err != nil {
		t.Fatalf("Digest on empty window failed: %v", err)
	}

	// Verify zero result.
	if len(result.TopEntities) != 0 {
		t.Errorf("Expected empty TopEntities, got %d", len(result.TopEntities))
	}
	if len(result.EventTimeline) != 0 {
		t.Errorf("Expected empty EventTimeline, got %d", len(result.EventTimeline))
	}
	if result.FactCount != 0 {
		t.Errorf("Expected FactCount=0, got %d", result.FactCount)
	}
	if result.RelationCount != 0 {
		t.Errorf("Expected RelationCount=0, got %d", result.RelationCount)
	}
	if result.SourceCount != 0 {
		t.Errorf("Expected SourceCount=0, got %d", result.SourceCount)
	}
}

func TestDigestTruncation(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()

	mgr := &Manager{store: store}

	// Insert more events than MaxEventsScan (5 events, MaxEventsScan=3).
	now := time.Now()
	windowStart := now.Add(-1 * time.Hour)

	for i := 0; i < 5; i++ {
		actors, _ := json.Marshal([]string{"entity_" + string(rune('A'+i))})
		targets, _ := json.Marshal([]string{"target_" + string(rune('1'+i))})
		_, err = store.db.Exec(`
INSERT INTO graph_events (event_time, title, summary, actors_json, targets_json, source_record_id, created_at, confidence)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		`, windowStart.Unix()+int64((i+1)*100), "Event "+string(rune('0'+i)), "Summary", string(actors), string(targets), int64(i+1), time.Now().Unix(), 0.9)
		if err != nil {
			t.Fatalf("Insert event %d failed: %v", i, err)
		}
	}

	// Digest with tight limits.
	result, err := mgr.Digest(windowStart, now, DigestOptions{
		MaxEntities:   2,
		MaxEvents:     2,
		MaxEventsScan: 3,
	})
	if err != nil {
		t.Fatalf("Digest failed: %v", err)
	}

	// Verify truncation flag.
	if !result.Truncated {
		t.Errorf("Expected Truncated=true, got false")
	}

	// Verify timeline limit.
	if len(result.EventTimeline) > 2 {
		t.Errorf("Expected EventTimeline len <= 2, got %d", len(result.EventTimeline))
	}

	// Verify entity limit.
	if len(result.TopEntities) > 2 {
		t.Errorf("Expected TopEntities len <= 2, got %d", len(result.TopEntities))
	}
}

func TestDigestStoreError(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}

	// Close the store to simulate a closed database.
	store.Close()

	mgr := &Manager{store: store}

	// Try to digest on a closed store.
	_, err = mgr.Digest(time.Now().Add(-1*time.Hour), time.Now(), DigestOptions{})
	if err == nil {
		t.Errorf("Expected error when querying closed store, got nil")
	}
}

// MockConfig implements Config for testing.
type MockConfig struct{}

func (m *MockConfig) GetWorkDir() string {
	return ""
}

func (m *MockConfig) GetSemanticConfig() *conf.SemanticConfig {
	return &conf.SemanticConfig{
		Enabled:         true,
		ChatProvider:    "test",
		ChatModel:       "test-model",
		ChatMaxTokens:   1000,
		ChatTemperature: 0.5,
	}
}

// MockChatInvoker is a test double for ChatInvoker.
type MockChatInvoker struct {
	CallCount int
	Response  string
	Error     error
}

func (m *MockChatInvoker) Chat(ctx context.Context, cfg conf.SemanticConfig, messages []semantic.ChatMessage) (string, error) {
	m.CallCount++
	if m.Error != nil {
		return "", m.Error
	}
	return m.Response, nil
}

func TestDigestSummaryDisabledNoCall(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()

	mgr := &Manager{store: store}
	mock := &MockChatInvoker{Response: "mocked summary"}

	now := time.Now()
	result, err := mgr.Digest(now.Add(-1*time.Hour), now, DigestOptions{
		EnableSummary: false,
		ChatInvoker:   mock,
	})
	if err != nil {
		t.Fatalf("Digest failed: %v", err)
	}

	if mock.CallCount != 0 {
		t.Errorf("Expected ChatInvoker not called when summary disabled, got %d calls", mock.CallCount)
	}
	if result.SummaryUsed {
		t.Errorf("Expected SummaryUsed=false, got true")
	}
}

func TestDigestSummaryEnabledWithSuccess(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()

	mgr := &Manager{store: store, conf: &MockConfig{}}
	expectedSummary := "This is a test summary."
	mock := &MockChatInvoker{Response: expectedSummary}

	now := time.Now()
	result, err := mgr.Digest(now.Add(-1*time.Hour), now, DigestOptions{
		EnableSummary: true,
		ChatInvoker:   mock,
	})
	if err != nil {
		t.Fatalf("Digest failed: %v", err)
	}

	if mock.CallCount != 1 {
		t.Errorf("Expected 1 ChatInvoker call, got %d", mock.CallCount)
	}
	if !result.SummaryUsed {
		t.Errorf("Expected SummaryUsed=true, got false")
	}
	if result.SummaryContent != expectedSummary {
		t.Errorf("Expected SummaryContent=%q, got %q", expectedSummary, result.SummaryContent)
	}
	if result.SummaryError != "" {
		t.Errorf("Expected SummaryError empty, got %q", result.SummaryError)
	}
}

func TestDigestSummaryEnabledWithError(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore failed: %v", err)
	}
	defer store.Close()

	mgr := &Manager{store: store, conf: &MockConfig{}}
	mock := &MockChatInvoker{Error: errors.New("chat timeout")}

	now := time.Now()
	result, err := mgr.Digest(now.Add(-1*time.Hour), now, DigestOptions{
		EnableSummary: true,
		ChatInvoker:   mock,
	})
	if err != nil {
		t.Fatalf("Digest should not fail on summary error, got: %v", err)
	}

	if !result.SummaryUsed {
		// Summary failed, so SummaryUsed should be false (graceful degradation).
		if result.SummaryUsed {
			t.Errorf("Expected SummaryUsed=false when summary error occurs, got true")
		}
	}
	if result.SummaryError == "" {
		t.Errorf("Expected SummaryError to be set, got empty")
	}
	if result.SummaryError != "chat_timeout" {
		t.Errorf("Expected SummaryError=chat_timeout, got %q", result.SummaryError)
	}
}

func TestErrorBucketForSummary(t *testing.T) {
	tests := []struct {
		input    error
		expected string
	}{
		{nil, ""},
		{errors.New("chat provider not configured"), "chat_provider_not_configured"},
		{errors.New("timeout error"), "chat_timeout"},
		{errors.New("context deadline exceeded"), "chat_timeout"},
		{errors.New("auth failed 401"), "chat_auth_error"},
		{errors.New("rate limit 429"), "chat_rate_limit"},
		{errors.New("unknown error"), "chat_error"},
	}

	for _, tt := range tests {
		got := errorBucketForSummary(tt.input)
		if got != tt.expected {
			t.Errorf("errorBucketForSummary(%v) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}
