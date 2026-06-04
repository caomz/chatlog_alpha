package temporalgraph

import (
	"testing"
	"time"
)

func TestApplyExtractionAndQuery(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	src := SourceRecord{
		SourceID:   "msg:1",
		SourceType: "message",
		EventType:  "message_1",
		TalkerName: "项目群",
		SenderName: "张三",
		Content:    "张三确认客户A的合同延期到下周处理",
		EventTime:  time.Date(2026, 4, 25, 10, 0, 0, 0, time.Local),
	}
	id, _, err := store.UpsertSource(src)
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	src.ID = id
	ext := Extraction{
		Entities: []ExtractedEntity{
			{Name: "张三", Type: "person", Confidence: 0.9},
			{Name: "客户A", Type: "customer", Confidence: 0.9},
		},
		Relations: []ExtractedRelation{
			{Subject: "张三", Predicate: "确认", Object: "客户A", Status: "active", ChangeType: "observed", Confidence: 0.8, Evidence: "张三确认客户A的合同延期到下周处理"},
		},
		Events: []ExtractedEvent{
			{EventType: "contract", Title: "合同延期确认", Summary: "客户A合同延期到下周处理", Actors: []string{"张三"}, Targets: []string{"客户A"}, Confidence: 0.8},
		},
		Facts: []ExtractedFact{
			{Statement: "客户A的合同延期到下周处理", ChangeType: "observed", Status: "active", Confidence: 0.8},
		},
	}
	if err := store.ApplyExtraction(src, ext); err != nil {
		t.Fatalf("ApplyExtraction() error = %v", err)
	}

	result, err := store.Query("合同", "", "", time.Time{}, time.Time{}, 20)
	if err != nil {
		t.Fatalf("Query() error = %v", err)
	}
	if len(result.Events) == 0 {
		t.Fatalf("expected event")
	}
	if len(result.Facts) == 0 {
		t.Fatalf("expected fact")
	}
	allResult, err := store.Query("", "", "", time.Time{}, time.Time{}, 20)
	if err != nil {
		t.Fatalf("Query(all) error = %v", err)
	}
	if len(allResult.Entities) == 0 {
		t.Fatalf("expected entities")
	}
}

func TestSourceUpsertRequeuesChangedContent(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	src := SourceRecord{SourceID: "biz:1", SourceType: "business", EventType: "order", Content: "初始内容", EventTime: time.Now()}
	id, _, err := store.UpsertSource(src)
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	src.ID = id
	if err := store.ApplyExtraction(src, Extraction{
		Entities: []ExtractedEntity{{Name: "初始内容", Type: "topic", Confidence: 0.8}},
		Facts:    []ExtractedFact{{Statement: "初始内容", ChangeType: "observed", Status: "active", Confidence: 0.8}},
	}); err != nil {
		t.Fatalf("ApplyExtraction() error = %v", err)
	}
	if st := store.Status(false, false, ""); st.Pending != 0 || st.Processed != 1 {
		t.Fatalf("unexpected status after done: pending=%d processed=%d", st.Pending, st.Processed)
	}

	src.Content = "变更后的内容"
	if _, _, err := store.UpsertSource(src); err != nil {
		t.Fatalf("UpsertSource(changed) error = %v", err)
	}
	if st := store.Status(false, false, ""); st.Pending != 1 {
		t.Fatalf("changed source should be pending, got %d", st.Pending)
	}
}

func TestClaimPendingSourcesMarksProcessingAndReset(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	for i := 0; i < 3; i++ {
		_, _, err := store.UpsertSource(SourceRecord{
			SourceID:   "msg:claim:" + string(rune('0'+i)),
			SourceType: "message",
			EventType:  "message_1",
			Content:    "待抽取内容",
			EventTime:  time.Now().Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("UpsertSource() error = %v", err)
		}
	}

	items, err := store.ClaimPendingSources(2)
	if err != nil {
		t.Fatalf("ClaimPendingSources() error = %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 claimed items, got %d", len(items))
	}
	st := store.Status(false, false, "")
	if st.Pending != 1 || st.Processing != 2 {
		t.Fatalf("unexpected status after claim: pending=%d processing=%d", st.Pending, st.Processing)
	}

	if err := store.ResetProcessingSources(); err != nil {
		t.Fatalf("ResetProcessingSources() error = %v", err)
	}
	st = store.Status(false, false, "")
	if st.Pending != 3 || st.Processing != 0 {
		t.Fatalf("unexpected status after reset: pending=%d processing=%d", st.Pending, st.Processing)
	}
}

func TestRequeueFailedSourcesByErrorOnlyRecoversMatchingFailures(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	matchingID, _, err := store.UpsertSource(SourceRecord{
		SourceID:   "msg:failed:matching",
		SourceType: "message",
		EventType:  "message_1",
		Content:    "配置恢复后应重试",
		EventTime:  time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertSource(matching) error = %v", err)
	}
	otherID, _, err := store.UpsertSource(SourceRecord{
		SourceID:   "msg:failed:other",
		SourceType: "message",
		EventType:  "message_1",
		Content:    "真实模型错误不应自动重试",
		EventTime:  time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertSource(other) error = %v", err)
	}
	if err := store.MarkSource(matchingID, "failed", "chat model is not configured"); err != nil {
		t.Fatalf("MarkSource(matching) error = %v", err)
	}
	if err := store.MarkSource(otherID, "failed", "minimax chat failed after 3 round(s)"); err != nil {
		t.Fatalf("MarkSource(other) error = %v", err)
	}

	n, err := store.RequeueFailedSourcesByError("chat model is not configured")
	if err != nil {
		t.Fatalf("RequeueFailedSourcesByError() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("RequeueFailedSourcesByError affected %d rows, want 1", n)
	}
	st := store.Status(false, false, "")
	if st.Pending != 1 || st.Failed != 1 {
		t.Fatalf("unexpected status after requeue: pending=%d failed=%d", st.Pending, st.Failed)
	}
}

func TestRequeueFailedSourcesByErrorContainingRecoversTimeouts(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	timeoutID, _, err := store.UpsertSource(SourceRecord{
		SourceID:   "msg:timeout",
		SourceType: "message",
		EventType:  "message_1",
		Content:    "超时后应重试",
		EventTime:  time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertSource(timeout) error = %v", err)
	}
	decodeID, _, err := store.UpsertSource(SourceRecord{
		SourceID:   "msg:decode",
		SourceType: "message",
		EventType:  "message_1",
		Content:    "解析错误不应按 timeout 规则重试",
		EventTime:  time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertSource(decode) error = %v", err)
	}
	if err := store.MarkSource(timeoutID, "failed", `minimax chat failed after 3 round(s): Post "https://api.minimaxi.com/v1/chat/completions": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`); err != nil {
		t.Fatalf("MarkSource(timeout) error = %v", err)
	}
	if err := store.MarkSource(decodeID, "failed", `decode graph extraction failed: no json object found; raw=`); err != nil {
		t.Fatalf("MarkSource(decode) error = %v", err)
	}

	n, err := store.RequeueFailedSourcesByErrorContaining(recoverableGraphTimeoutErrorTokens)
	if err != nil {
		t.Fatalf("RequeueFailedSourcesByErrorContaining() error = %v", err)
	}
	if n != 1 {
		t.Fatalf("RequeueFailedSourcesByErrorContaining affected %d rows, want 1", n)
	}
	st := store.Status(false, false, "")
	if st.Pending != 1 || st.Failed != 1 {
		t.Fatalf("unexpected status after timeout requeue: pending=%d failed=%d", st.Pending, st.Failed)
	}
}
