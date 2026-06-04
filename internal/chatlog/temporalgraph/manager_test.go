package temporalgraph

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sjzar/chatlog/internal/chatlog/conf"
)

type testGraphConfig struct {
	workDir  string
	semantic conf.SemanticConfig
}

func (c testGraphConfig) GetWorkDir() string {
	return c.workDir
}

func (c testGraphConfig) GetSemanticConfig() *conf.SemanticConfig {
	cfg := c.semantic
	return &cfg
}

func TestShouldSkipGraphMessageContent(t *testing.T) {
	cases := []struct {
		name    string
		content string
		skip    bool
	}{
		{name: "image placeholder", content: "[图片]", skip: true},
		{name: "voice placeholder", content: "[语音]", skip: true},
		{name: "recall", content: `"张三" 撤回了一条消息`, skip: true},
		{name: "ack", content: "收到", skip: true},
		{name: "file placeholder", content: "[文件|跟踪表.xls]", skip: true},
		{name: "short but meaningful", content: "刘琼场大环境消毒", skip: false},
		{name: "business text", content: "客户A合同延期到下周处理", skip: false},
		{name: "mention only", content: "@张三 @李四", skip: true},
		{name: "mention with text", content: "@张三 请确认客户A合同", skip: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := shouldSkipGraphMessageContent(tc.content, 1); got != tc.skip {
				t.Fatalf("shouldSkipGraphMessageContent(%q) = %v, want %v", tc.content, got, tc.skip)
			}
		})
	}
}

func TestNormalizeExtractionCanonicalizesAliasPredicateAndTime(t *testing.T) {
	base := time.Date(2026, 4, 25, 10, 0, 0, 0, time.Local)
	rec := SourceRecord{
		Talker: "room@chatroom", TalkerName: "项目群", Sender: "wxid_abc", SenderName: "张三",
		Content: "张三明天负责客户A合同处理", EventTime: base,
		Participants: []GraphParticipant{
			{UserName: "wxid_abc", DisplayName: "张三", Kind: "person", Aliases: []string{"张总"}},
			{UserName: "cust-a", DisplayName: "客户A", Kind: "customer"},
		},
	}
	ext := normalizeExtraction(Extraction{
		Entities: []ExtractedEntity{{Name: "张总", Type: "person", Confidence: 0.9}},
		Relations: []ExtractedRelation{{
			Subject: "张总", Predicate: "负责处理", Object: "cust-a", TimeText: "明天",
			Status: "active", ChangeType: "observed", Confidence: 0.8, SupportScore: 0.9, Evidence: "张三明天负责客户A合同处理",
		}},
		Facts: []ExtractedFact{{
			Statement: "张三明天负责客户A合同处理", TimeText: "明天",
			Status: "active", ChangeType: "observed", Confidence: 0.8, SupportScore: 0.9,
		}},
	}, rec)
	if len(ext.Relations) != 1 {
		t.Fatalf("expected one relation, got %d", len(ext.Relations))
	}
	rel := ext.Relations[0]
	if rel.Subject != "张三" || rel.Object != "客户A" {
		t.Fatalf("relation aliases not canonicalized: %#v", rel)
	}
	if rel.Predicate != "responsible_for" || rel.CanonicalPredicate != "responsible_for" {
		t.Fatalf("predicate not canonicalized: %#v", rel)
	}
	wantTime := time.Date(2026, 4, 26, 0, 0, 0, 0, time.Local).Unix()
	if rel.ValidFrom != wantTime || ext.Facts[0].ValidFrom != wantTime {
		t.Fatalf("relative time not resolved: rel=%d fact=%d want=%d", rel.ValidFrom, ext.Facts[0].ValidFrom, wantTime)
	}
}

func TestDecodeExtractionHandlesWrappedMarkdownAndExtraText(t *testing.T) {
	raw := "下面是结果：\n```json\n{\"extraction\":{\"entities\":[{\"name\":\"张美华\",\"type\":\"person\",\"aliases\":[],\"canonical_name\":\"张美华\",\"confidence\":1}],\"relations\":[],\"events\":[],\"facts\":[]}}\n```\n已完成"
	ext, err := decodeExtraction(raw)
	if err != nil {
		t.Fatalf("decodeExtraction returned error: %v", err)
	}
	if len(ext.Entities) != 1 || ext.Entities[0].Name != "张美华" {
		t.Fatalf("unexpected extraction: %#v", ext)
	}
}

func TestDecodeExtractionIgnoresBracesInsideStrings(t *testing.T) {
	raw := `{"entities":[],"relations":[],"events":[],"facts":[{"statement":"客户备注 {VIP} 已确认","confidence":0.8}]}`
	ext, err := decodeExtraction(raw)
	if err != nil {
		t.Fatalf("decodeExtraction returned error: %v", err)
	}
	if len(ext.Facts) != 1 || ext.Facts[0].Statement != "客户备注 {VIP} 已确认" {
		t.Fatalf("unexpected facts: %#v", ext.Facts)
	}
}

func TestDecodeExtractionAcceptsEmptyExtraction(t *testing.T) {
	raw := `{"entities":[],"relations":[],"events":[],"facts":[]}`
	ext, err := decodeExtraction(raw)
	if err != nil {
		t.Fatalf("decodeExtraction returned error: %v", err)
	}
	if len(ext.Entities) != 0 || len(ext.Relations) != 0 || len(ext.Events) != 0 || len(ext.Facts) != 0 {
		t.Fatalf("expected empty extraction, got %#v", ext)
	}
}

func TestDecodeExtractionAcceptsFencedEmptyExtraction(t *testing.T) {
	raw := "```json\n{\"entities\":[],\"relations\":[],\"events\":[],\"facts\":[]}\n```"
	ext, err := decodeExtraction(raw)
	if err != nil {
		t.Fatalf("decodeExtraction returned error: %v", err)
	}
	if len(ext.Entities) != 0 || len(ext.Relations) != 0 || len(ext.Events) != 0 || len(ext.Facts) != 0 {
		t.Fatalf("expected empty extraction, got %#v", ext)
	}
}

func TestDecodeExtractionStripsThinkBlock(t *testing.T) {
	raw := "<think>{\"entities\":[{\"name\":\"错误\",\"type\":\"person\"}]}</think>\n```json\n{\"entities\":[{\"name\":\"段宏洋\",\"type\":\"person\",\"confidence\":0.9}],\"relations\":[],\"events\":[],\"facts\":[]}\n```"
	ext, err := decodeExtraction(raw)
	if err != nil {
		t.Fatalf("decodeExtraction returned error: %v", err)
	}
	if len(ext.Entities) != 1 || ext.Entities[0].Name != "段宏洋" {
		t.Fatalf("unexpected extraction: %#v", ext)
	}
}

func TestDecodeExtractionPrefersExtractionSchemaCandidate(t *testing.T) {
	raw := `分析 {"foo":"bar"} 后输出 {"entities":[{"name":"客户A","type":"customer","confidence":0.8}],"relations":[],"events":[],"facts":[]}`
	ext, err := decodeExtraction(raw)
	if err != nil {
		t.Fatalf("decodeExtraction returned error: %v", err)
	}
	if len(ext.Entities) != 1 || ext.Entities[0].Name != "客户A" {
		t.Fatalf("unexpected extraction: %#v", ext)
	}
}

func TestIsDatabaseNotReady(t *testing.T) {
	if !isDatabaseNotReady(errors.New("database not ready")) {
		t.Fatal("expected database not ready to match")
	}
	if isDatabaseNotReady(errors.New("database query failed")) {
		t.Fatal("unexpected database not ready match")
	}
}

func TestResumeRequeuesRecoverableChatConfigFailures(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	id, _, err := store.UpsertSource(SourceRecord{
		SourceID:   "biz:recoverable",
		SourceType: "business",
		EventType:  "ticket",
		Content:    "配置恢复后应重新抽取",
		EventTime:  time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	if err := store.MarkSource(id, "failed", errChatModelNotConfigured); err != nil {
		t.Fatalf("MarkSource() error = %v", err)
	}
	m := &Manager{
		conf: testGraphConfig{semantic: conf.NormalizeSemanticConfig(conf.SemanticConfig{
			Enabled:      true,
			ChatProvider: conf.ProviderMMX,
			ChatModel:    conf.DefaultMMXChat,
		})},
		store:   store,
		lastErr: errChatModelNotConfigured,
	}

	if err := m.Resume(); err != nil {
		t.Fatalf("Resume() error = %v", err)
	}
	st := store.Status(false, false, "")
	if st.Pending != 1 || st.Failed != 0 {
		t.Fatalf("unexpected status after resume: pending=%d failed=%d", st.Pending, st.Failed)
	}
	if m.lastErr != "" {
		t.Fatalf("recoverable lastErr was not cleared: %q", m.lastErr)
	}
}

func TestProcessPendingDoesNotAutoRequeueRecoverableFailures(t *testing.T) {
	store, err := OpenStore(t.TempDir())
	if err != nil {
		t.Fatalf("OpenStore() error = %v", err)
	}
	defer store.Close()

	id, _, err := store.UpsertSource(SourceRecord{
		SourceID:   "biz:no-auto-retry",
		SourceType: "business",
		EventType:  "ticket",
		Content:    "只有显式恢复才重试",
		EventTime:  time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertSource() error = %v", err)
	}
	if err := store.MarkSource(id, "failed", errChatModelNotConfigured); err != nil {
		t.Fatalf("MarkSource() error = %v", err)
	}
	m := &Manager{
		conf: testGraphConfig{semantic: conf.NormalizeSemanticConfig(conf.SemanticConfig{
			Enabled:      true,
			ChatProvider: conf.ProviderMMX,
			ChatModel:    conf.DefaultMMXChat,
		})},
		store:   store,
		lastErr: errChatModelNotConfigured,
	}

	m.ProcessPending(context.Background(), 10)
	st := store.Status(false, false, "")
	if st.Pending != 0 || st.Failed != 1 {
		t.Fatalf("unexpected status after process: pending=%d failed=%d", st.Pending, st.Failed)
	}
	if m.lastErr != "" {
		t.Fatalf("stale recoverable lastErr was not cleared: %q", m.lastErr)
	}
}
