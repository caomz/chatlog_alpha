package dailyreport

import (
	"context"
	"fmt"
	"time"

	"github.com/sjzar/chatlog/internal/model"
	"github.com/sjzar/chatlog/internal/wechatdb"
)

type DBSource interface {
	GetSessions(key string, limit, offset int) (*wechatdb.GetSessionsResp, error)
	GetChatRooms(key string, limit, offset int) (*wechatdb.GetChatRoomsResp, error)
	GetMessages(start, end time.Time, talker string, sender string, keyword string, limit, offset int) ([]*model.Message, error)
}

func GenerateDailyReport(ctx context.Context, db DBSource, opts ReportOptions) (*Report, error) {
	if db == nil {
		return nil, fmt.Errorf("db source is nil")
	}
	opts = NormalizeOptions(opts)
	date, start, end, err := ResolveDateRange(opts.Date, opts.Timezone, time.Now())
	if err != nil {
		return nil, err
	}
	opts.Date = date

	groupTalkers, privateTalkers, err := collectTalkers(db)
	if err != nil {
		return nil, err
	}

	report := &Report{
		Date:        date,
		Timezone:    opts.Timezone,
		GeneratedAt: time.Now().Format(time.RFC3339),
		Options:     opts,
		Mentions:    make([]MentionItem, 0),
	}

	for _, talker := range groupTalkers {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		msgs, err := db.GetMessages(start, end, talker, "", "", 0, 0)
		if err != nil {
			report.SkippedTalkerCount++
			continue
		}
		chatMessages := convertMessages(msgs)
		report.Overview.ScannedGroupCount++
		report.Overview.ScannedMessageCount += len(chatMessages)
		report.Mentions = append(report.Mentions, BuildMentionItems(chatMessages, opts)...)
	}

	if opts.IncludePrivate {
		messagesByChat := make(map[string][]ChatMessage)
		for _, talker := range privateTalkers {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if skipPrivateTalker(talker) {
				continue
			}
			msgs, err := db.GetMessages(start, end, talker, "", "", 0, 0)
			if err != nil {
				report.SkippedTalkerCount++
				continue
			}
			chatMessages := convertMessages(msgs)
			if len(chatMessages) == 0 {
				continue
			}
			report.Overview.ScannedPrivateCount++
			report.Overview.ScannedMessageCount += len(chatMessages)
			messagesByChat[talker] = chatMessages
		}
		report.PrivateUpdates = BuildPrivateChatUpdates(messagesByChat, opts)
	}

	for i := range report.Mentions {
		report.Mentions[i].EvidenceID = evidenceID("mention", i+1)
		for j := range report.Mentions[i].Replies {
			report.Mentions[i].Replies[j].EvidenceID = evidenceID("reply", len(report.Evidence)+j+1)
		}
	}
	report.Evidence = BuildEvidence(report.Mentions)
	report.Todos = BuildTodos(report.Mentions, report.PrivateUpdates)
	fillOverview(report)
	return report, nil
}

func fillOverview(report *Report) {
	report.Overview.GroupMentionCount = len(report.Mentions)
	for _, item := range report.Mentions {
		if item.Status == StatusReplied {
			report.Overview.RepliedCount++
		} else {
			report.Overview.PendingCount++
		}
	}
	report.Overview.PrivateChatCount = len(report.PrivateUpdates)
	report.Overview.ImportantTodoCount = len(report.Todos)
}
