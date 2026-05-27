package dailyreport

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sjzar/chatlog/internal/model"
	"github.com/sjzar/chatlog/internal/wechatdb"
)

type mockDB struct {
	sessions  []*model.Session
	chatrooms []*model.ChatRoom
	messages  map[string][]*model.Message
}

func (m mockDB) GetSessions(key string, limit, offset int) (*wechatdb.GetSessionsResp, error) {
	return &wechatdb.GetSessionsResp{Items: m.sessions}, nil
}

func (m mockDB) GetChatRooms(key string, limit, offset int) (*wechatdb.GetChatRoomsResp, error) {
	return &wechatdb.GetChatRoomsResp{Items: m.chatrooms}, nil
}

func (m mockDB) GetMessages(start, end time.Time, talker string, sender string, keyword string, limit, offset int) ([]*model.Message, error) {
	items := m.messages[talker]
	out := make([]*model.Message, 0, len(items))
	for _, item := range items {
		if item.Time.Before(start) || item.Time.After(end) {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func TestGenerateDailyReportWithMockDB(t *testing.T) {
	loc, _ := time.LoadLocation(DefaultTimezone)
	base := time.Date(2026, 5, 27, 9, 0, 0, 0, loc)
	db := mockDB{
		sessions: []*model.Session{
			{UserName: "room@chatroom", NickName: "项目群"},
			{UserName: "wxid_private", NickName: "张三"},
			{UserName: "gh_public", NickName: "公众号"},
		},
		chatrooms: []*model.ChatRoom{{Name: "room@chatroom", NickName: "项目群"}},
		messages: map[string][]*model.Message{
			"room@chatroom": {
				modelMsg("room@chatroom", "项目群", "wxid_a", "张三", base, 1, false, "@caomz 看下"),
				modelMsg("room@chatroom", "项目群", "me", "我", base.Add(time.Minute), 2, true, "收到"),
			},
			"wxid_private": {
				modelMsg("wxid_private", "张三", "wxid_private", "张三", base.Add(2*time.Hour), 3, false, "今晚发我"),
			},
			"gh_public": {
				modelMsg("gh_public", "公众号", "gh_public", "公众号", base.Add(3*time.Hour), 4, false, "推送"),
			},
		},
	}
	report, err := GenerateDailyReport(context.Background(), db, ReportOptions{
		Date:           "2026-05-27",
		Mention:        "caomz",
		IncludePrivate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Overview.GroupMentionCount != 1 || report.Overview.RepliedCount != 1 {
		t.Fatalf("unexpected mention counts: %+v", report.Overview)
	}
	if len(report.PrivateUpdates) != 1 || report.PrivateUpdates[0].ChatID != "wxid_private" {
		t.Fatalf("unexpected private updates: %+v", report.PrivateUpdates)
	}
	if len(report.Evidence) != 2 {
		t.Fatalf("expected mention and reply evidence, got %d", len(report.Evidence))
	}
}

func TestGenerateDailyReportCollectsImageRefs(t *testing.T) {
	loc, _ := time.LoadLocation(DefaultTimezone)
	base := time.Date(2026, 5, 26, 9, 0, 0, 0, loc)
	db := mockDB{
		sessions:  []*model.Session{{UserName: "room@chatroom", NickName: "项目群"}},
		chatrooms: []*model.ChatRoom{{Name: "room@chatroom", NickName: "项目群"}},
		messages: map[string][]*model.Message{
			"room@chatroom": {
				modelMsg("room@chatroom", "项目群", "wxid_a", "张三", base, 1, false, "前文"),
				imageModelMsg("room@chatroom", "项目群", "wxid_a", "张三", base.Add(time.Minute), 2),
				modelMsg("room@chatroom", "项目群", "wxid_a", "张三", base.Add(2*time.Minute), 3, false, "@caomz 看图"),
			},
		},
	}
	report, err := GenerateDailyReport(context.Background(), db, ReportOptions{
		Date:           "2026-05-26",
		Mention:        "caomz",
		BeforeCount:    2,
		IncludePrivate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Mentions) != 1 || len(report.Mentions[0].Before) != 2 {
		t.Fatalf("unexpected mentions: %+v", report.Mentions)
	}
	if got := report.Mentions[0].Before[1].MediaRefs; len(got) == 0 || got[0].Key != "abc123" {
		t.Fatalf("expected image media ref, got %+v", got)
	}
}

func modelMsg(talker, talkerName, sender, senderName string, tm time.Time, seq int64, self bool, content string) *model.Message {
	return &model.Message{
		Seq:        seq,
		ID:         seq,
		Time:       tm,
		Talker:     talker,
		TalkerName: talkerName,
		IsChatRoom: strings.HasSuffix(talker, "@chatroom"),
		Sender:     sender,
		SenderName: senderName,
		IsSelf:     self,
		Type:       model.MessageTypeText,
		Content:    content,
	}
}

func imageModelMsg(talker, talkerName, sender, senderName string, tm time.Time, seq int64) *model.Message {
	msg := modelMsg(talker, talkerName, sender, senderName, tm, seq, false, "")
	msg.Type = model.MessageTypeImage
	msg.Content = ""
	msg.Contents = map[string]any{
		"md5":  "abc123",
		"path": "msg/attach/a/b/Img/abc123.dat",
	}
	return msg
}
