package dailyreport

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

type textJob struct {
	run func(context.Context)
}

func applyTextAnalysisConcurrent(ctx context.Context, report *Report, client AnalysisClient, cfg AIAnalysisConfig) error {
	if report == nil {
		return nil
	}
	if client == nil {
		report.AnalysisError = "text analyzer is not configured"
		return nil
	}
	var errMu sync.Mutex
	setReportAnalysisError := func(errText string) {
		errText = strings.TrimSpace(errText)
		if errText == "" {
			return
		}
		errMu.Lock()
		defer errMu.Unlock()
		if report.AnalysisError == "" {
			report.AnalysisError = errText
		}
	}
	var mentionJobs []textJob
	for i := range report.Mentions {
		item := &report.Mentions[i]
		mentionJobs = append(mentionJobs, textJob{run: func(ctx context.Context) {
			item.Analysis = analyzeMention(ctx, client, *item)
			setReportAnalysisError(item.Analysis.Error)
		}})
	}
	runTextJobs(ctx, cfg.Concurrency, mentionJobs)
	if err := ctx.Err(); err != nil {
		report.AnalysisError = err.Error()
		return err
	}

	var jobs []textJob
	grouped := groupMentions(report.Mentions)
	report.GroupAnalyses = make([]GroupAnalysis, len(sortedMentionChats(grouped)))
	for idx, chatName := range sortedMentionChats(grouped) {
		idx := idx
		chatName := chatName
		items := append([]MentionItem(nil), grouped[chatName]...)
		jobs = append(jobs, textJob{run: func(ctx context.Context) {
			analysis := analyzeGroup(ctx, client, chatName, items)
			report.GroupAnalyses[idx] = analysis
			setReportAnalysisError(analysis.Error)
		}})
	}
	for i := range report.PrivateUpdates {
		update := &report.PrivateUpdates[i]
		jobs = append(jobs, textJob{run: func(ctx context.Context) {
			summary, needsReply, errText := analyzePrivate(ctx, client, *update)
			if summary != "" {
				update.Summary = summary
			}
			if needsReply != nil {
				update.NeedsReply = *needsReply
			}
			setReportAnalysisError(errText)
		}})
	}
	runTextJobs(ctx, cfg.Concurrency, jobs)
	if err := ctx.Err(); err != nil {
		report.AnalysisError = err.Error()
		return err
	}
	return nil
}

func runTextJobs(ctx context.Context, concurrency int, jobs []textJob) {
	runConcurrent(ctx, normalizeConcurrency(concurrency), len(jobs), func(index int) {
		jobs[index].run(ctx)
	})
}

func analyzeMention(ctx context.Context, client AnalysisClient, item MentionItem) AnalysisResult {
	payload := map[string]any{
		"evidence_id": item.EvidenceID,
		"chat_name":   item.ChatName,
		"sender":      displayName(item.SenderName, item.Sender),
		"time":        item.Time,
		"status":      item.Status,
		"content":     renderContentWithVision(item.Content, item.MediaRefs, item.ImageAnalysis, item.ImageAnalysisError),
		"before":      contextRows(item.Before),
		"after":       contextRows(item.After),
		"replies":     replyRows(item.Replies),
	}
	instruction := `只能输出 JSON：{"topic":"短主题","context":"基于上下文的判断","suggested_action":"建议动作","needs_reply":true,"evidence_ids":["mention-001"]}。evidence_ids 必须来自输入，不要编造证据。`
	raw, err := client.AnalyzeText(ctx, "微信日报 @ 消息解析", toJSON(payload), instruction)
	if err != nil {
		return AnalysisResult{Error: err.Error()}
	}
	var out AnalysisResult
	if err := decodeJSONObject(raw, &out); err != nil {
		return AnalysisResult{Error: "parse mention analysis failed: " + err.Error()}
	}
	allowed := []string{item.EvidenceID}
	for _, reply := range item.Replies {
		allowed = append(allowed, reply.EvidenceID)
	}
	out.EvidenceIDs = filterEvidenceIDs(out.EvidenceIDs, allowed)
	return out
}

func analyzeGroup(ctx context.Context, client AnalysisClient, chatName string, items []MentionItem) GroupAnalysis {
	out := GroupAnalysis{ChatName: chatName}
	if len(items) > 0 {
		out.ChatID = items[0].ChatID
	}
	payloadItems := make([]map[string]any, 0, len(items))
	allowed := make([]string, 0, len(items))
	for _, item := range items {
		allowed = append(allowed, item.EvidenceID)
		payloadItems = append(payloadItems, map[string]any{
			"evidence_id":      item.EvidenceID,
			"time":             item.Time,
			"sender":           displayName(item.SenderName, item.Sender),
			"status":           item.Status,
			"content":          renderContentWithVision(item.Content, item.MediaRefs, item.ImageAnalysis, item.ImageAnalysisError),
			"analysis_topic":   item.Analysis.Topic,
			"analysis_context": item.Analysis.Context,
			"suggested_action": item.Analysis.SuggestedAction,
		})
	}
	payload := map[string]any{
		"chat_name": chatName,
		"mentions":  payloadItems,
	}
	instruction := `只能输出 JSON：{"summary":"群聊当日主题总结","risks":["风险"],"todos":["待办"],"evidence_ids":["mention-001"]}。只基于输入证据，evidence_ids 必须来自输入。`
	raw, err := client.AnalyzeText(ctx, "微信日报群聊级解析", toJSON(payload), instruction)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	if err := decodeJSONObject(raw, &out); err != nil {
		out.Error = "parse group analysis failed: " + err.Error()
		return out
	}
	if out.ChatName == "" {
		out.ChatName = chatName
	}
	if out.ChatID == "" && len(items) > 0 {
		out.ChatID = items[0].ChatID
	}
	out.EvidenceIDs = filterEvidenceIDs(out.EvidenceIDs, allowed)
	return out
}

func analyzePrivate(ctx context.Context, client AnalysisClient, update PrivateChatUpdate) (string, *bool, string) {
	payload := map[string]any{
		"chat_name":         update.ChatName,
		"total_messages":    update.TotalMessages,
		"incoming_messages": update.IncomingMessages,
		"self_messages":     update.SelfMessages,
		"needs_reply":       update.NeedsReply,
		"latest_message": map[string]any{
			"time":    update.LatestMessage.TimeText,
			"sender":  displaySenderName(update.LatestMessage),
			"content": renderContentWithVision(update.LatestMessage.Content, update.LatestMessage.MediaRefs, update.LatestMessage.ImageAnalysis, update.LatestMessage.ImageAnalysisError),
		},
	}
	instruction := `只能输出 JSON：{"summary":"一句话私聊更新","needs_reply":true,"suggested_action":"建议动作"}。只基于输入，不要编造。`
	raw, err := client.AnalyzeText(ctx, "微信日报私聊更新解析", toJSON(payload), instruction)
	if err != nil {
		return "", nil, err.Error()
	}
	var parsed struct {
		Summary         string `json:"summary"`
		NeedsReply      *bool  `json:"needs_reply"`
		SuggestedAction string `json:"suggested_action"`
	}
	if err := decodeJSONObject(raw, &parsed); err != nil {
		return "", nil, "parse private analysis failed: " + err.Error()
	}
	summary := strings.TrimSpace(parsed.Summary)
	if action := strings.TrimSpace(parsed.SuggestedAction); action != "" {
		if summary != "" {
			summary += "；"
		}
		summary += "建议：" + action
	}
	return summary, parsed.NeedsReply, ""
}

func contextRows(items []ContextMessage) []map[string]any {
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, map[string]any{
			"time":     item.Time,
			"sender":   displayName(item.SenderName, item.Sender),
			"is_self":  item.IsSelf,
			"position": item.Position,
			"content":  renderContentWithVision(item.Content, item.MediaRefs, item.ImageAnalysis, item.ImageAnalysisError),
		})
	}
	return rows
}

func replyRows(items []ReplyItem) []map[string]any {
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, map[string]any{
			"evidence_id": item.EvidenceID,
			"time":        item.Time,
			"content":     item.Content,
		})
	}
	return rows
}

func toJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func decodeJSONObject(raw string, out any) error {
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end > start {
		raw = raw[start : end+1]
	}
	if raw == "" {
		return fmt.Errorf("empty model response")
	}
	return json.Unmarshal([]byte(raw), out)
}

func filterEvidenceIDs(ids, allowed []string) []string {
	allowedSet := map[string]struct{}{}
	for _, id := range allowed {
		if strings.TrimSpace(id) != "" {
			allowedSet[id] = struct{}{}
		}
	}
	out := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if _, ok := allowedSet[id]; !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func normalizeConcurrency(n int) int {
	if n <= 0 {
		return 1
	}
	return n
}

func runConcurrent(ctx context.Context, workers, total int, fn func(int)) {
	if total <= 0 || fn == nil {
		return
	}
	if workers <= 0 {
		workers = 1
	}
	if workers > total {
		workers = total
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				if ctx.Err() != nil {
					continue
				}
				fn(index)
			}
		}()
	}
	for i := 0; i < total; i++ {
		if ctx.Err() != nil {
			break
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}
