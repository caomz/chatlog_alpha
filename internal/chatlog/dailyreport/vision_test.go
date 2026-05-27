package dailyreport

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type mockImageResolver struct {
	calls int
}

func (m *mockImageResolver) ResolveImage(ctx context.Context, ref MediaRef) ([]byte, string, string, error) {
	m.calls++
	return []byte{0xff, 0xd8, 0xff, 0xdb, 0x00, 0x43, 0x00}, "image/jpeg", ref.Key, nil
}

type mockVisionClient struct {
	calls int
	err   error
	reply string
}

func (m *mockVisionClient) AnalyzeImage(ctx context.Context, prompt string, imageData []byte, mimeType string) (string, error) {
	m.calls++
	if m.err != nil {
		return "", m.err
	}
	if m.reply != "" {
		return m.reply, nil
	}
	return "截图里显示 Redis 超时告警", nil
}

func (m *mockVisionClient) AnalyzeText(ctx context.Context, title, data, instruction string) (string, error) {
	switch title {
	case "微信日报 @ 消息解析":
		return `{"topic":"Redis超时","context":"上下文显示有人询问接口超时","suggested_action":"回复排查进展","needs_reply":true,"evidence_ids":["mention-001"]}`, nil
	case "微信日报群聊级解析":
		return `{"summary":"项目群集中讨论 Redis 超时","risks":["接口继续超时"],"todos":["确认 Redis 连接池"],"evidence_ids":["mention-001"]}`, nil
	case "微信日报私聊更新解析":
		return `{"summary":"对方催脚本","needs_reply":true,"suggested_action":"发送巡检脚本"}`, nil
	default:
		return "{}", nil
	}
}

func TestApplyVisionWritesAnalysisAndUsesCache(t *testing.T) {
	report := &Report{
		Date: "2026-05-26",
		Mentions: []MentionItem{{
			ChatName: "项目群",
			Content:  "@caomz 看图",
			Before: []ContextMessage{{
				Content:   "[图片]",
				MediaRefs: []MediaRef{{Type: "image", Key: "image-1"}},
			}, {
				Content:   "[图片]",
				MediaRefs: []MediaRef{{Type: "image", Key: "image-1"}},
			}},
		}},
	}
	resolver := &mockImageResolver{}
	client := &mockVisionClient{}
	cfg := VisionConfig{Enabled: true, MaxImages: 5, Provider: "mock", Model: "vision"}
	if err := ApplyVision(context.Background(), report, resolver, client, cfg); err != nil {
		t.Fatal(err)
	}
	if report.Mentions[0].Before[0].ImageAnalysis == "" {
		t.Fatalf("expected image analysis")
	}
	md, err := RenderMarkdown(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "截图里显示 Redis 超时告警") {
		t.Fatalf("markdown missing vision result: %s", md)
	}

	if client.calls != 1 {
		t.Fatalf("expected duplicate image to use in-memory cache, got %d calls", client.calls)
	}
}

func TestApplyVisionTreatsMissingImageResponseAsError(t *testing.T) {
	report := &Report{
		Date: "2026-05-26",
		Mentions: []MentionItem{{
			ChatName: "项目群",
			Before: []ContextMessage{{
				Content:   "[图片]",
				MediaRefs: []MediaRef{{Type: "image", Key: "image-1"}},
			}},
		}},
	}
	resolver := &mockImageResolver{}
	client := &mockVisionClient{reply: "未收到图片，无法识别内容，请上传图片后再试。"}
	cfg := VisionConfig{Enabled: true, MaxImages: 5, Provider: "mock", Model: "vision"}
	if err := ApplyVision(context.Background(), report, resolver, client, cfg); err != nil {
		t.Fatal(err)
	}
	msg := report.Mentions[0].Before[0]
	if msg.ImageAnalysis != "" {
		t.Fatalf("expected missing-image response not to be accepted, got %q", msg.ImageAnalysis)
	}
	if !strings.Contains(msg.ImageAnalysisError, "did not receive image payload") {
		t.Fatalf("expected image payload error, got %q", msg.ImageAnalysisError)
	}
}

func TestApplyAIAnalysisWritesMentionGroupAndPrivateSummary(t *testing.T) {
	base := time.Date(2026, 5, 26, 9, 0, 0, 0, time.Local)
	mentions := BuildMentionItems([]ChatMessage{
		msg(base, 1, "zhangsan", false, "@caomz Redis 超时看下"),
	}, ReportOptions{Mention: "caomz"})
	report := &Report{
		Date: "2026-05-26",
		Options: NormalizeOptions(ReportOptions{
			Mention:        "caomz",
			IncludePrivate: true,
		}),
		Mentions: mentions,
		PrivateUpdates: []PrivateChatUpdate{{
			ChatID:           "wxid_a",
			ChatName:         "张三",
			TotalMessages:    1,
			IncomingMessages: 1,
			LatestMessage:    privateMsg("wxid_a", "张三", base, 2, false, "脚本发我下"),
			NeedsReply:       true,
		}},
	}
	report.Evidence = BuildEvidence(report.Mentions)
	client := &mockVisionClient{}
	err := ApplyAIAnalysis(context.Background(), report, nil, client, AIAnalysisConfig{Summary: true, Concurrency: 2})
	if err != nil {
		t.Fatal(err)
	}
	if report.Mentions[0].Analysis.Topic != "Redis超时" {
		t.Fatalf("mention analysis = %+v", report.Mentions[0].Analysis)
	}
	if len(report.GroupAnalyses) != 1 || !strings.Contains(report.GroupAnalyses[0].Summary, "Redis") {
		t.Fatalf("group analysis = %+v", report.GroupAnalyses)
	}
	if !strings.Contains(report.PrivateUpdates[0].Summary, "发送巡检脚本") {
		t.Fatalf("private summary = %q", report.PrivateUpdates[0].Summary)
	}
}

func TestApplyVisionRunsRequestsConcurrently(t *testing.T) {
	base := time.Date(2026, 5, 26, 9, 0, 0, 0, time.Local)
	report := &Report{Date: "2026-05-26"}
	for i := 0; i < 3; i++ {
		report.Mentions = append(report.Mentions, MentionItem{
			ChatName: "项目群",
			Time:     base.Add(time.Duration(i) * time.Minute).Format("2006-01-02 15:04:05"),
			Before: []ContextMessage{{
				Content:   "[图片]",
				MediaRefs: []MediaRef{{Type: "image", Key: fmt.Sprintf("image-%d", i)}},
			}},
		})
	}
	resolver := &mockImageResolver{}
	client := &slowVisionClient{}
	start := time.Now()
	err := ApplyAIAnalysis(context.Background(), report, resolver, client, AIAnalysisConfig{
		Vision:      VisionConfig{Enabled: true, MaxImages: 3, Provider: "mock", Model: "vision"},
		Concurrency: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed >= 120*time.Millisecond {
		t.Fatalf("vision calls did not run concurrently enough, elapsed=%v", elapsed)
	}
	if client.maxActive.Load() < 2 {
		t.Fatalf("expected concurrent calls, maxActive=%d", client.maxActive.Load())
	}
}

type slowVisionClient struct {
	active    atomic.Int32
	maxActive atomic.Int32
}

func (c *slowVisionClient) AnalyzeImage(ctx context.Context, prompt string, imageData []byte, mimeType string) (string, error) {
	now := c.active.Add(1)
	for {
		max := c.maxActive.Load()
		if now <= max || c.maxActive.CompareAndSwap(max, now) {
			break
		}
	}
	time.Sleep(50 * time.Millisecond)
	c.active.Add(-1)
	return "并发图片摘要", nil
}

func (c *slowVisionClient) AnalyzeText(ctx context.Context, title, data, instruction string) (string, error) {
	return "{}", nil
}
