package dailyreport

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

func NormalizeOptions(opts ReportOptions) ReportOptions {
	opts.Date = strings.TrimSpace(opts.Date)
	if opts.Date == "" {
		opts.Date = "today"
	}
	opts.Mention = strings.TrimSpace(opts.Mention)
	if opts.Mention == "" {
		opts.Mention = DefaultMention
	}
	opts.Timezone = strings.TrimSpace(opts.Timezone)
	if opts.Timezone == "" {
		opts.Timezone = DefaultTimezone
	}
	if opts.BeforeCount < 0 {
		opts.BeforeCount = 0
	}
	if opts.AfterCount < 0 {
		opts.AfterCount = 0
	}
	if opts.BeforeCount == 0 {
		opts.BeforeCount = DefaultBeforeCount
	}
	if opts.AfterCount == 0 {
		opts.AfterCount = DefaultAfterCount
	}
	if opts.ReplyWindowMinutes <= 0 {
		opts.ReplyWindowMinutes = DefaultReplyWindowMinute
	}
	if opts.MaxImages <= 0 {
		opts.MaxImages = DefaultMaxImages
	}
	if opts.AnalysisConcurrency < 0 {
		opts.AnalysisConcurrency = 0
	}
	opts.MentionAliases = NormalizeAliases(opts.Mention, opts.MentionAliases)
	return opts
}

func NormalizeAliases(mention string, aliases []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(aliases)+2)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		key := strings.ToLower(s)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, s)
	}
	add(mention)
	add("@" + strings.TrimPrefix(strings.TrimSpace(mention), "@"))
	for _, alias := range aliases {
		add(alias)
		if !strings.HasPrefix(strings.TrimSpace(alias), "@") {
			add("@" + alias)
		}
	}
	return out
}

func DetectMention(content string, aliases []string) bool {
	content = strings.TrimSpace(content)
	if content == "" {
		return false
	}
	lower := strings.ToLower(content)
	noAtSpace := compactAtSpace(lower)
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		a := strings.ToLower(alias)
		if strings.HasPrefix(a, "@") && (containsMentionToken(lower, a) || containsMentionToken(noAtSpace, compactAtSpace(a))) {
			return true
		}
		if !strings.HasPrefix(a, "@") && (containsKeywordToken(lower, a) || containsKeywordToken(noAtSpace, compactAtSpace(a))) {
			return true
		}
	}
	return false
}

func containsMentionToken(content, token string) bool {
	if token == "" {
		return false
	}
	for start := 0; start <= len(content)-len(token); {
		idx := strings.Index(content[start:], token)
		if idx < 0 {
			return false
		}
		pos := start + idx
		end := pos + len(token)
		if end >= len(content) || !isASCIIWordByte(content[end]) {
			return true
		}
		start = end
	}
	return false
}

func containsKeywordToken(content, token string) bool {
	if token == "" {
		return false
	}
	for start := 0; start <= len(content)-len(token); {
		idx := strings.Index(content[start:], token)
		if idx < 0 {
			return false
		}
		pos := start + idx
		end := pos + len(token)
		leftOK := pos == 0 || !isASCIIWordByte(content[pos-1])
		rightOK := end >= len(content) || !isASCIIWordByte(content[end])
		if leftOK && rightOK {
			return true
		}
		start = end
	}
	return false
}

func isASCIIWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '_' || b == '-' || b == '.'
}

func BuildMentionItems(messages []ChatMessage, opts ReportOptions) []MentionItem {
	opts = NormalizeOptions(opts)
	msgs := sortedMessages(messages)
	items := make([]MentionItem, 0)
	for i, msg := range msgs {
		if !DetectMention(msg.Content, opts.MentionAliases) {
			continue
		}
		item := MentionItem{
			EvidenceID:         evidenceID("mention", len(items)+1),
			ChatID:             msg.ChatID,
			ChatName:           displayChatName(msg),
			Sender:             msg.Sender,
			SenderName:         displaySenderName(msg),
			Seq:                msg.Seq,
			MsgID:              msg.MsgID,
			Timestamp:          messageTimestamp(msg),
			Time:               messageTimeText(msg),
			Content:            msg.Content,
			MediaRefs:          append([]MediaRef(nil), msg.MediaRefs...),
			ImageAnalysis:      msg.ImageAnalysis,
			ImageAnalysisError: msg.ImageAnalysisError,
			Status:             StatusPending,
			Before:             contextSlice(msgs, i-opts.BeforeCount, i, "before"),
			After:              contextSlice(msgs, i+1, i+1+opts.AfterCount, "after"),
		}
		windowEnd := messageTime(msg).Add(time.Duration(opts.ReplyWindowMinutes) * time.Minute)
		for j := i + 1; j < len(msgs); j++ {
			next := msgs[j]
			if messageTime(next).After(windowEnd) {
				break
			}
			if DetectMention(next.Content, opts.MentionAliases) {
				break
			}
			if next.IsSelf {
				item.Replies = append(item.Replies, ReplyItem{
					EvidenceID: evidenceID("reply", len(item.Replies)+1),
					Seq:        next.Seq,
					MsgID:      next.MsgID,
					Timestamp:  messageTimestamp(next),
					Time:       messageTimeText(next),
					Content:    next.Content,
				})
			}
		}
		if len(item.Replies) > 0 {
			item.Status = StatusReplied
		}
		items = append(items, item)
	}
	return items
}

func BuildPrivateChatUpdates(messagesByChat map[string][]ChatMessage, opts ReportOptions) []PrivateChatUpdate {
	_ = NormalizeOptions(opts)
	updates := make([]PrivateChatUpdate, 0, len(messagesByChat))
	for chatID, messages := range messagesByChat {
		msgs := sortedMessages(messages)
		if len(msgs) == 0 {
			continue
		}
		update := PrivateChatUpdate{
			ChatID:        chatID,
			ChatName:      displayChatName(msgs[0]),
			TotalMessages: len(msgs),
			LatestMessage: msgs[len(msgs)-1],
		}
		for _, msg := range msgs {
			if msg.IsSelf {
				update.SelfMessages++
			} else {
				update.IncomingMessages++
			}
		}
		update.NeedsReply = update.IncomingMessages > 0 && !update.LatestMessage.IsSelf
		updates = append(updates, update)
	}
	sort.Slice(updates, func(i, j int) bool {
		ti := messageTimestamp(updates[i].LatestMessage)
		tj := messageTimestamp(updates[j].LatestMessage)
		if ti == tj {
			return updates[i].ChatID < updates[j].ChatID
		}
		return ti > tj
	})
	return updates
}

func BuildTodos(mentions []MentionItem, updates []PrivateChatUpdate) []TodoItem {
	todos := make([]TodoItem, 0)
	for _, item := range mentions {
		if item.Status != StatusPending {
			continue
		}
		sender := item.SenderName
		if sender == "" {
			sender = item.Sender
		}
		todos = append(todos, TodoItem{
			Text:       fmt.Sprintf("回复 %s 的 @ 消息：%s", sender, compactContent(item.Content, 80)),
			EvidenceID: item.EvidenceID,
			ChatID:     item.ChatID,
			ChatName:   item.ChatName,
		})
	}
	for _, update := range updates {
		if !update.NeedsReply {
			continue
		}
		todos = append(todos, TodoItem{
			Text:     fmt.Sprintf("回复私聊 %s：%s", update.ChatName, compactContent(update.LatestMessage.Content, 80)),
			ChatID:   update.ChatID,
			ChatName: update.ChatName,
		})
	}
	return todos
}

func BuildEvidence(mentions []MentionItem) []EvidenceItem {
	out := make([]EvidenceItem, 0)
	for i, item := range mentions {
		mentionID := evidenceID("mention", i+1)
		out = append(out, EvidenceItem{
			ID:                 mentionID,
			Type:               "mention",
			ChatID:             item.ChatID,
			ChatName:           item.ChatName,
			Sender:             item.Sender,
			SenderName:         item.SenderName,
			Seq:                item.Seq,
			MsgID:              item.MsgID,
			Timestamp:          item.Timestamp,
			Time:               item.Time,
			Content:            item.Content,
			MediaRefs:          append([]MediaRef(nil), item.MediaRefs...),
			ImageAnalysis:      item.ImageAnalysis,
			ImageAnalysisError: item.ImageAnalysisError,
			IsSelf:             false,
		})
		mentions[i].EvidenceID = mentionID
		for j, reply := range item.Replies {
			replyID := evidenceID("reply", len(out)+1)
			out = append(out, EvidenceItem{
				ID:        replyID,
				Type:      "reply",
				ChatID:    item.ChatID,
				ChatName:  item.ChatName,
				Seq:       reply.Seq,
				MsgID:     reply.MsgID,
				Timestamp: reply.Timestamp,
				Time:      reply.Time,
				Content:   reply.Content,
				IsSelf:    true,
			})
			mentions[i].Replies[j].EvidenceID = replyID
		}
	}
	return out
}

func ResolveDateRange(dateExpr, timezone string, now time.Time) (string, time.Time, time.Time, error) {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		timezone = DefaultTimezone
	}
	loc, err := time.LoadLocation(timezone)
	if err != nil {
		return "", time.Time{}, time.Time{}, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	dateExpr = strings.TrimSpace(dateExpr)
	if dateExpr == "" || strings.EqualFold(dateExpr, "today") {
		now = now.In(loc)
		dateExpr = now.Format("2006-01-02")
	}
	day, err := time.ParseInLocation("2006-01-02", dateExpr, loc)
	if err != nil {
		return "", time.Time{}, time.Time{}, err
	}
	return day.Format("2006-01-02"), day, day.Add(24 * time.Hour).Add(-time.Nanosecond), nil
}

func sortedMessages(messages []ChatMessage) []ChatMessage {
	out := append([]ChatMessage(nil), messages...)
	sort.SliceStable(out, func(i, j int) bool {
		ti, tj := messageTimestamp(out[i]), messageTimestamp(out[j])
		if ti == tj {
			return out[i].Seq < out[j].Seq
		}
		return ti < tj
	})
	return out
}

func contextSlice(messages []ChatMessage, start, end int, position string) []ContextMessage {
	if start < 0 {
		start = 0
	}
	if end > len(messages) {
		end = len(messages)
	}
	if start >= end {
		return nil
	}
	out := make([]ContextMessage, 0, end-start)
	for _, msg := range messages[start:end] {
		out = append(out, ContextMessage{
			Seq:                msg.Seq,
			MsgID:              msg.MsgID,
			Timestamp:          messageTimestamp(msg),
			Time:               messageTimeText(msg),
			Sender:             msg.Sender,
			SenderName:         displaySenderName(msg),
			Content:            msg.Content,
			MediaRefs:          append([]MediaRef(nil), msg.MediaRefs...),
			ImageAnalysis:      msg.ImageAnalysis,
			ImageAnalysisError: msg.ImageAnalysisError,
			IsSelf:             msg.IsSelf,
			Position:           position,
		})
	}
	return out
}

func compactAtSpace(s string) string {
	for strings.Contains(s, "@ ") || strings.Contains(s, "@\t") {
		s = strings.ReplaceAll(s, "@ ", "@")
		s = strings.ReplaceAll(s, "@\t", "@")
	}
	return s
}

func compactContent(s string, limit int) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if limit > 0 && len([]rune(s)) > limit {
		r := []rune(s)
		return string(r[:limit]) + "..."
	}
	return s
}

func evidenceID(prefix string, n int) string {
	return fmt.Sprintf("%s-%03d", prefix, n)
}

func displayChatName(msg ChatMessage) string {
	if strings.TrimSpace(msg.ChatName) != "" {
		return strings.TrimSpace(msg.ChatName)
	}
	return strings.TrimSpace(msg.ChatID)
}

func displaySenderName(msg ChatMessage) string {
	if msg.IsSelf {
		return "我"
	}
	if strings.TrimSpace(msg.SenderName) != "" {
		return strings.TrimSpace(msg.SenderName)
	}
	return strings.TrimSpace(msg.Sender)
}

func messageTimestamp(msg ChatMessage) int64 {
	if msg.Timestamp > 0 {
		return msg.Timestamp
	}
	if !msg.Time.IsZero() {
		return msg.Time.Unix()
	}
	return 0
}

func messageTime(msg ChatMessage) time.Time {
	if !msg.Time.IsZero() {
		return msg.Time
	}
	if msg.Timestamp > 0 {
		return time.Unix(msg.Timestamp, 0)
	}
	return time.Time{}
}

func messageTimeText(msg ChatMessage) string {
	if strings.TrimSpace(msg.TimeText) != "" {
		return strings.TrimSpace(msg.TimeText)
	}
	tm := messageTime(msg)
	if tm.IsZero() {
		return ""
	}
	return tm.Format("2006-01-02 15:04:05")
}
