package dailyreport

import (
	"testing"
	"time"
)

func TestDetectMentionAliases(t *testing.T) {
	aliases := NormalizeAliases("caomz", []string{"曹明哲"})
	cases := []string{
		"@caomz 这个接口看下",
		"@ caomz 这个接口看下",
		"caomz 这个接口看下",
		"@曹明哲 这个接口看下",
	}
	for _, tc := range cases {
		if !DetectMention(tc, aliases) {
			t.Fatalf("expected mention match for %q", tc)
		}
	}
	if DetectMention("caomzz 不是 at", aliases) || DetectMention("@caomzz 不是我", aliases) {
		t.Fatalf("unexpected mention match")
	}

	if DetectMention("noopencodefoo", []string{"opencode"}) {
		t.Fatalf("unexpected mention match in composite token")
	}
	if !DetectMention("我在opencode群里讨论问题", []string{"opencode"}) {
		t.Fatalf("unexpected mention match")
	}
}

func TestBuildMentionItemsPendingAndReplied(t *testing.T) {
	base := time.Date(2026, 5, 27, 9, 0, 0, 0, time.Local)
	messages := []ChatMessage{
		msg(base, 1, "a", false, "前文 1"),
		msg(base.Add(time.Minute), 2, "b", false, "@caomz 看下 Redis"),
		msg(base.Add(2*time.Minute), 3, "me", true, "我看下"),
		msg(base.Add(3*time.Minute), 4, "me", true, "连接池也查一下"),
		msg(base.Add(4*time.Minute), 5, "c", false, "@曹明哲 另一个问题"),
	}
	items := BuildMentionItems(messages, ReportOptions{
		Mention:            "caomz",
		MentionAliases:     []string{"曹明哲"},
		BeforeCount:        1,
		AfterCount:         1,
		ReplyWindowMinutes: 60,
	})
	if len(items) != 2 {
		t.Fatalf("expected 2 mention items, got %d", len(items))
	}
	if items[0].Status != StatusReplied || len(items[0].Replies) != 2 {
		t.Fatalf("expected first mention replied with 2 replies, got status=%s replies=%d", items[0].Status, len(items[0].Replies))
	}
	if len(items[0].Before) != 1 || len(items[0].After) != 1 {
		t.Fatalf("expected before/after context")
	}
	if items[1].Status != StatusPending {
		t.Fatalf("expected second mention pending, got %s", items[1].Status)
	}
}

func TestBuildPrivateChatUpdatesNeedsReply(t *testing.T) {
	base := time.Date(2026, 5, 27, 10, 0, 0, 0, time.Local)
	updates := BuildPrivateChatUpdates(map[string][]ChatMessage{
		"wxid_a": {
			privateMsg("wxid_a", "张三", base, 1, false, "今晚能发脚本吗"),
			privateMsg("wxid_a", "张三", base.Add(time.Minute), 2, true, "可以"),
			privateMsg("wxid_a", "张三", base.Add(2*time.Minute), 3, false, "那我等你"),
		},
	}, ReportOptions{})
	if len(updates) != 1 {
		t.Fatalf("expected 1 update, got %d", len(updates))
	}
	if !updates[0].NeedsReply {
		t.Fatalf("expected needs reply")
	}
	if updates[0].IncomingMessages != 2 || updates[0].SelfMessages != 1 {
		t.Fatalf("unexpected counts: %+v", updates[0])
	}
}

func msg(tm time.Time, seq int64, sender string, self bool, content string) ChatMessage {
	return ChatMessage{
		ChatID:     "room@chatroom",
		ChatName:   "项目群",
		IsChatRoom: true,
		Sender:     sender,
		Seq:        seq,
		MsgID:      seq,
		Timestamp:  tm.Unix(),
		Time:       tm,
		TimeText:   tm.Format("2006-01-02 15:04:05"),
		Content:    content,
		IsSelf:     self,
	}
}

func privateMsg(chatID, chatName string, tm time.Time, seq int64, self bool, content string) ChatMessage {
	return ChatMessage{
		ChatID:    chatID,
		ChatName:  chatName,
		Sender:    "wxid_sender",
		Seq:       seq,
		MsgID:     seq,
		Timestamp: tm.Unix(),
		Time:      tm,
		TimeText:  tm.Format("2006-01-02 15:04:05"),
		Content:   content,
		IsSelf:    self,
	}
}
