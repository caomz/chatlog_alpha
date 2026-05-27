package dailyreport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func RenderMarkdown(report *Report) (string, error) {
	if report == nil {
		return "", fmt.Errorf("report is nil")
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "# 微信日报 - %s\n\n", report.Date)
	b.WriteString("## 1. 今日总览\n\n")
	fmt.Fprintf(&b, "- 群聊 @%s：%d 条\n", strings.TrimPrefix(report.Options.Mention, "@"), report.Overview.GroupMentionCount)
	fmt.Fprintf(&b, "- 已回复：%d 条\n", report.Overview.RepliedCount)
	fmt.Fprintf(&b, "- 未回复 / 待处理：%d 条\n", report.Overview.PendingCount)
	fmt.Fprintf(&b, "- 私聊更新联系人：%d 个\n", report.Overview.PrivateChatCount)
	fmt.Fprintf(&b, "- 重要待办：%d 项\n\n", report.Overview.ImportantTodoCount)
	if strings.TrimSpace(report.AnalysisError) != "" {
		fmt.Fprintf(&b, "- AI 解析错误：%s\n\n", compactContent(report.AnalysisError, 120))
	}
	b.WriteString("---\n\n")

	writeGroupAnalysisBlock(&b, report.GroupAnalyses)

	b.WriteString("## 2. 群聊 @ 汇总\n\n")
	if len(report.Mentions) == 0 {
		b.WriteString("今天没有匹配到群聊 @ 消息。\n\n")
	} else {
		grouped := groupMentions(report.Mentions)
		for idx, chat := range sortedMentionChats(grouped) {
			fmt.Fprintf(&b, "### 2.%d %s\n\n", idx+1, chat)
			for j, item := range grouped[chat] {
				fmt.Fprintf(&b, "#### @消息 %d\n\n", j+1)
				fmt.Fprintf(&b, "- 时间：%s\n", item.Time)
				fmt.Fprintf(&b, "- 群聊：%s\n", item.ChatName)
				fmt.Fprintf(&b, "- 发送人：%s\n", displayName(item.SenderName, item.Sender))
				fmt.Fprintf(&b, "- 内容：%s\n", renderContentWithVision(item.Content, item.MediaRefs, item.ImageAnalysis, item.ImageAnalysisError))
				fmt.Fprintf(&b, "- 证据：%s\n\n", item.EvidenceID)
				writeMentionAnalysis(&b, item.Analysis)
				writeContextBlock(&b, "前文上下文", item.Before)
				if len(item.Replies) == 0 {
					b.WriteString("**你的回复：** 未回复\n\n")
				} else {
					b.WriteString("**你的回复：**\n\n")
					for _, reply := range item.Replies {
						fmt.Fprintf(&b, "- 时间：%s\n", reply.Time)
						fmt.Fprintf(&b, "- 内容：%s\n", reply.Content)
						fmt.Fprintf(&b, "- 证据：%s\n\n", reply.EvidenceID)
					}
				}
				writeContextBlock(&b, "后续上下文", item.After)
				status := "未回复"
				if item.Status == StatusReplied {
					status = "已回复"
				}
				fmt.Fprintf(&b, "**状态：%s**\n\n---\n\n", status)
			}
		}
	}

	b.WriteString("## 3. 未回复 / 待跟进 @\n\n")
	pending := 0
	for _, item := range report.Mentions {
		if item.Status != StatusPending {
			continue
		}
		pending++
		fmt.Fprintf(&b, "### 3.%d %s\n\n", pending, item.ChatName)
		fmt.Fprintf(&b, "- 时间：%s\n", item.Time)
		fmt.Fprintf(&b, "- 发送人：%s\n", displayName(item.SenderName, item.Sender))
		fmt.Fprintf(&b, "- 内容：%s\n", renderContentWithVision(item.Content, item.MediaRefs, item.ImageAnalysis, item.ImageAnalysisError))
		fmt.Fprintf(&b, "- 证据：%s\n\n", item.EvidenceID)
	}
	if pending == 0 {
		b.WriteString("没有未回复 @。\n\n")
	}
	b.WriteString("---\n\n")

	b.WriteString("## 4. 今日个人私聊更新\n\n")
	if len(report.PrivateUpdates) == 0 {
		b.WriteString("没有私聊更新，或本次未启用私聊统计。\n\n")
	} else {
		for i, update := range report.PrivateUpdates {
			fmt.Fprintf(&b, "### 4.%d %s\n\n", i+1, update.ChatName)
			fmt.Fprintf(&b, "- 新消息：%d 条\n", update.TotalMessages)
			fmt.Fprintf(&b, "- 对方消息：%d 条\n", update.IncomingMessages)
			fmt.Fprintf(&b, "- 你回复：%d 条\n", update.SelfMessages)
			fmt.Fprintf(&b, "- 最近一条：%s %s：%s\n", update.LatestMessage.TimeText, displaySenderName(update.LatestMessage), renderContentWithVision(update.LatestMessage.Content, update.LatestMessage.MediaRefs, update.LatestMessage.ImageAnalysis, update.LatestMessage.ImageAnalysisError))
			if update.NeedsReply {
				b.WriteString("- 状态：待回复\n")
			} else {
				b.WriteString("- 状态：无需立即回复\n")
			}
			if strings.TrimSpace(update.Summary) != "" {
				fmt.Fprintf(&b, "- AI 摘要：%s\n", update.Summary)
			}
			b.WriteString("\n")
		}
	}
	b.WriteString("---\n\n")

	b.WriteString("## 5. 今日待办\n\n")
	if len(report.Todos) == 0 {
		b.WriteString("没有自动识别到待办。\n\n")
	} else {
		for _, todo := range report.Todos {
			if todo.EvidenceID != "" {
				fmt.Fprintf(&b, "- [ ] %s（%s）\n", todo.Text, todo.EvidenceID)
			} else {
				fmt.Fprintf(&b, "- [ ] %s\n", todo.Text)
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")

	b.WriteString("## 6. 原始证据索引\n\n")
	b.WriteString("| 类型 | 群/联系人 | 时间 | Seq | 说明 |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, ev := range report.Evidence {
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %s |\n", ev.Type, ev.ChatName, shortClock(ev.Time), ev.Seq, ev.ID)
	}
	return b.String(), nil
}

func writeGroupAnalysisBlock(b *bytes.Buffer, analyses []GroupAnalysis) {
	if len(analyses) == 0 {
		return
	}
	b.WriteString("## AI 群聊解析\n\n")
	for i, item := range analyses {
		name := strings.TrimSpace(item.ChatName)
		if name == "" {
			name = item.ChatID
		}
		fmt.Fprintf(b, "### G.%d %s\n\n", i+1, name)
		if strings.TrimSpace(item.Error) != "" {
			fmt.Fprintf(b, "- 解析错误：%s\n\n", compactContent(item.Error, 120))
			continue
		}
		if strings.TrimSpace(item.Summary) != "" {
			fmt.Fprintf(b, "- 总结：%s\n", item.Summary)
		}
		if len(item.Risks) > 0 {
			fmt.Fprintf(b, "- 风险：%s\n", strings.Join(item.Risks, "；"))
		}
		if len(item.Todos) > 0 {
			fmt.Fprintf(b, "- 待办：%s\n", strings.Join(item.Todos, "；"))
		}
		if len(item.EvidenceIDs) > 0 {
			fmt.Fprintf(b, "- 证据：%s\n", strings.Join(item.EvidenceIDs, ", "))
		}
		b.WriteString("\n")
	}
	b.WriteString("---\n\n")
}

func writeMentionAnalysis(b *bytes.Buffer, analysis AnalysisResult) {
	if analysisEmpty(analysis) {
		return
	}
	b.WriteString("**AI 解析：**\n\n")
	if strings.TrimSpace(analysis.Error) != "" {
		fmt.Fprintf(b, "- 解析错误：%s\n\n", compactContent(analysis.Error, 120))
		return
	}
	if strings.TrimSpace(analysis.Topic) != "" {
		fmt.Fprintf(b, "- 主题：%s\n", analysis.Topic)
	}
	if strings.TrimSpace(analysis.Context) != "" {
		fmt.Fprintf(b, "- 判断：%s\n", analysis.Context)
	}
	if strings.TrimSpace(analysis.SuggestedAction) != "" {
		fmt.Fprintf(b, "- 建议动作：%s\n", analysis.SuggestedAction)
	}
	if analysis.NeedsReply != nil {
		if *analysis.NeedsReply {
			b.WriteString("- 是否需要回复：是\n")
		} else {
			b.WriteString("- 是否需要回复：否\n")
		}
	}
	if len(analysis.EvidenceIDs) > 0 {
		fmt.Fprintf(b, "- 证据：%s\n", strings.Join(analysis.EvidenceIDs, ", "))
	}
	b.WriteString("\n")
}

func analysisEmpty(analysis AnalysisResult) bool {
	return strings.TrimSpace(analysis.Topic) == "" &&
		strings.TrimSpace(analysis.Context) == "" &&
		strings.TrimSpace(analysis.SuggestedAction) == "" &&
		analysis.NeedsReply == nil &&
		len(analysis.EvidenceIDs) == 0 &&
		strings.TrimSpace(analysis.Error) == ""
}

func RenderJSON(report *Report) ([]byte, error) {
	return json.MarshalIndent(report, "", "  ")
}

func SaveReport(report *Report, outDir, format string) (SaveResult, error) {
	if report == nil {
		return SaveResult{}, fmt.Errorf("report is nil")
	}
	format = strings.ToLower(strings.TrimSpace(format))
	if format == "" {
		format = "both"
	}
	if format != "both" && format != "markdown" && format != "json" {
		return SaveResult{}, fmt.Errorf("unsupported format: %s", format)
	}
	if strings.TrimSpace(outDir) == "" {
		outDir = "reports"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return SaveResult{}, err
	}
	base := fmt.Sprintf("daily-%s", report.Date)
	result := SaveResult{}
	if format == "both" || format == "markdown" {
		md, err := RenderMarkdown(report)
		if err != nil {
			return SaveResult{}, err
		}
		result.MarkdownPath = filepath.Join(outDir, base+".md")
		if err := os.WriteFile(result.MarkdownPath, []byte(md), 0o600); err != nil {
			return SaveResult{}, err
		}
	}
	if format == "both" || format == "json" {
		js, err := RenderJSON(report)
		if err != nil {
			return SaveResult{}, err
		}
		result.JSONPath = filepath.Join(outDir, base+".json")
		if err := os.WriteFile(result.JSONPath, js, 0o600); err != nil {
			return SaveResult{}, err
		}
	}
	return result, nil
}

func writeContextBlock(b *bytes.Buffer, title string, items []ContextMessage) {
	fmt.Fprintf(b, "**%s：**\n\n", title)
	if len(items) == 0 {
		b.WriteString("无。\n\n")
		return
	}
	for i, item := range items {
		fmt.Fprintf(b, "%d. %s %s：%s\n", i+1, shortClock(item.Time), displayName(item.SenderName, item.Sender), renderContentWithVision(item.Content, item.MediaRefs, item.ImageAnalysis, item.ImageAnalysisError))
	}
	b.WriteString("\n")
}

func renderContentWithVision(content string, refs []MediaRef, analysis, analysisErr string) string {
	content = strings.TrimSpace(content)
	if len(refs) == 0 {
		return content
	}
	switch {
	case strings.TrimSpace(analysis) != "":
		return replaceImagePlaceholder(content, "[图片："+strings.TrimSpace(analysis)+"]")
	case strings.TrimSpace(analysisErr) != "":
		return replaceImagePlaceholder(content, "[图片未解析："+compactContent(analysisErr, 80)+"]")
	default:
		return replaceImagePlaceholder(content, "[图片未解析]")
	}
}

func replaceImagePlaceholder(content, replacement string) string {
	if content == "" {
		return replacement
	}
	if strings.Contains(content, "[图片]") {
		return strings.ReplaceAll(content, "[图片]", replacement)
	}
	return content + " " + replacement
}

func groupMentions(items []MentionItem) map[string][]MentionItem {
	out := make(map[string][]MentionItem)
	for _, item := range items {
		key := strings.TrimSpace(item.ChatName)
		if key == "" {
			key = item.ChatID
		}
		out[key] = append(out[key], item)
	}
	return out
}

func sortedMentionChats(grouped map[string][]MentionItem) []string {
	chats := make([]string, 0, len(grouped))
	for chat := range grouped {
		chats = append(chats, chat)
	}
	sort.Strings(chats)
	return chats
}

func displayName(name, fallback string) string {
	if strings.TrimSpace(name) != "" {
		return strings.TrimSpace(name)
	}
	return strings.TrimSpace(fallback)
}

func shortClock(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= len("2006-01-02 15:04:05") {
		return s[11:19]
	}
	return s
}
