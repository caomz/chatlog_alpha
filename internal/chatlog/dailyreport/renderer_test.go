package dailyreport

import (
	"strings"
	"testing"
	"time"
)

func TestRenderMarkdownContainsEvidenceIndex(t *testing.T) {
	base := time.Date(2026, 5, 27, 9, 0, 0, 0, time.Local)
	mentions := BuildMentionItems([]ChatMessage{
		msg(base, 1, "zhangsan", false, "@caomz 这个接口会超时吗"),
		msg(base.Add(time.Minute), 2, "me", true, "我看下 Redis"),
	}, ReportOptions{Mention: "caomz", ReplyWindowMinutes: 60})
	report := &Report{
		Date:     "2026-05-27",
		Timezone: DefaultTimezone,
		Options:  NormalizeOptions(ReportOptions{Mention: "caomz"}),
		Mentions: mentions,
	}
	report.Evidence = BuildEvidence(report.Mentions)
	report.Todos = BuildTodos(report.Mentions, nil)
	fillOverview(report)
	md, err := RenderMarkdown(report)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"# 微信日报 - 2026-05-27", "## 6. 原始证据索引", "mention-001", "reply-002"} {
		if !strings.Contains(md, want) {
			t.Fatalf("markdown missing %q:\n%s", want, md)
		}
	}
}

func TestRenderJSONStable(t *testing.T) {
	report := &Report{
		Date:     "2026-05-27",
		Timezone: DefaultTimezone,
		Options:  NormalizeOptions(ReportOptions{Mention: "caomz"}),
	}
	out, err := RenderJSON(report)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"date": "2026-05-27"`) {
		t.Fatalf("json missing date: %s", string(out))
	}
}
