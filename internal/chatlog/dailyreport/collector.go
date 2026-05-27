package dailyreport

import (
	"strings"

	"github.com/sjzar/chatlog/internal/model"
)

func collectTalkers(db DBSource) ([]string, []string, error) {
	groupSet := map[string]struct{}{}
	privateSet := map[string]struct{}{}
	sessions, err := db.GetSessions("", 0, 0)
	if err != nil {
		return nil, nil, err
	}
	if sessions != nil {
		for _, sess := range sessions.Items {
			if sess == nil || strings.TrimSpace(sess.UserName) == "" {
				continue
			}
			talker := strings.TrimSpace(sess.UserName)
			if isGroupTalker(talker) {
				groupSet[talker] = struct{}{}
			} else {
				privateSet[talker] = struct{}{}
			}
		}
	}
	if rooms, err := db.GetChatRooms("", 0, 0); err == nil && rooms != nil {
		for _, room := range rooms.Items {
			if room == nil || strings.TrimSpace(room.Name) == "" {
				continue
			}
			groupSet[strings.TrimSpace(room.Name)] = struct{}{}
		}
	}
	return sortedKeys(groupSet), sortedKeys(privateSet), nil
}

func convertMessages(messages []*model.Message) []ChatMessage {
	out := make([]ChatMessage, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			content = strings.TrimSpace(msg.PlainTextContent())
		}
		chatName := strings.TrimSpace(msg.TalkerName)
		if chatName == "" {
			chatName = strings.TrimSpace(msg.Talker)
		}
		senderName := strings.TrimSpace(msg.SenderName)
		if msg.IsSelf {
			senderName = "我"
		}
		out = append(out, ChatMessage{
			ChatID:     strings.TrimSpace(msg.Talker),
			ChatName:   chatName,
			IsChatRoom: msg.IsChatRoom || isGroupTalker(msg.Talker),
			Sender:     strings.TrimSpace(msg.Sender),
			SenderName: senderName,
			Seq:        msg.Seq,
			MsgID:      msg.ID,
			Timestamp:  msg.Time.Unix(),
			Time:       msg.Time,
			TimeText:   msg.Time.Format("2006-01-02 15:04:05"),
			Content:    content,
			IsSelf:     msg.IsSelf,
			Type:       msg.Type,
			SubType:    msg.SubType,
			MediaRefs:  messageMediaRefs(msg),
		})
	}
	return out
}

func messageMediaRefs(msg *model.Message) []MediaRef {
	if msg == nil || msg.Contents == nil || msg.Type != model.MessageTypeImage {
		return nil
	}
	refs := make([]MediaRef, 0, 2)
	md5 := contentString(msg.Contents["md5"])
	path := contentString(msg.Contents["path"])
	if md5 != "" {
		refs = append(refs, MediaRef{Type: "image", Key: md5, Path: path})
	}
	if path != "" && path != md5 {
		refs = append(refs, MediaRef{Type: "image", Key: path, Path: path})
	}
	return refs
}

func contentString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case []byte:
		return strings.TrimSpace(string(x))
	default:
		return ""
	}
}

func isGroupTalker(talker string) bool {
	return strings.HasSuffix(strings.TrimSpace(talker), "@chatroom")
}

func skipPrivateTalker(talker string) bool {
	talker = strings.ToLower(strings.TrimSpace(talker))
	if talker == "" || isGroupTalker(talker) {
		return true
	}
	if strings.HasPrefix(talker, "gh_") || strings.Contains(talker, "@app") {
		return true
	}
	switch talker {
	case "weixin", "weixinreminder", "newsapp", "fmessage", "tmessage", "notification_messages", "floatbottle":
		return true
	default:
		return false
	}
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sortStrings(out)
	return out
}

func sortStrings(items []string) {
	for i := 1; i < len(items); i++ {
		for j := i; j > 0 && items[j] < items[j-1]; j-- {
			items[j], items[j-1] = items[j-1], items[j]
		}
	}
}
