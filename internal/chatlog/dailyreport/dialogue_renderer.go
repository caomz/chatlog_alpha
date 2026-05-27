package dailyreport

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func RenderDialogueAnalysis(report *Report) (string, error) {
	if report == nil {
		return "", fmt.Errorf("report is nil")
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "# 微信日报对话解析 - %s\n\n", report.Date)
	b.WriteString("## 1. 解析结论\n\n")
	fmt.Fprintf(&b, "- 群聊 @：%d 条\n", report.Overview.GroupMentionCount)
	fmt.Fprintf(&b, "- 已识别回复：%d 条\n", report.Overview.RepliedCount)
	fmt.Fprintf(&b, "- 待处理 @：%d 条\n", report.Overview.PendingCount)
	fmt.Fprintf(&b, "- 私聊更新：%d 个\n", report.Overview.PrivateChatCount)
	fmt.Fprintf(&b, "- 图片证据：%d 条\n\n", countImageRefs(report))
	if strings.TrimSpace(report.AnalysisError) != "" {
		fmt.Fprintf(&b, "- AI 解析错误：%s\n\n", compactContent(report.AnalysisError, 120))
	}

	if len(report.GroupAnalyses) > 0 {
		b.WriteString("## 2. 群聊 AI 总结\n\n")
		for i, item := range report.GroupAnalyses {
			fmt.Fprintf(&b, "### 2.%d %s\n\n", i+1, displayName(item.ChatName, item.ChatID))
			if item.Error != "" {
				fmt.Fprintf(&b, "- 解析错误：%s\n\n", compactContent(item.Error, 120))
				continue
			}
			if item.Summary != "" {
				fmt.Fprintf(&b, "- 总结：%s\n", item.Summary)
			}
			if len(item.Risks) > 0 {
				fmt.Fprintf(&b, "- 风险：%s\n", strings.Join(item.Risks, "；"))
			}
			if len(item.Todos) > 0 {
				fmt.Fprintf(&b, "- 待办：%s\n", strings.Join(item.Todos, "；"))
			}
			if len(item.EvidenceIDs) > 0 {
				fmt.Fprintf(&b, "- 证据：%s\n", strings.Join(item.EvidenceIDs, ", "))
			}
			b.WriteString("\n")
		}
	}

	grouped := groupMentions(report.Mentions)
	chats := sortedMentionChats(grouped)
	b.WriteString("## 3. 群聊 @ 对话解析\n\n")
	if len(chats) == 0 {
		b.WriteString("无群聊 @ 记录。\n\n")
	}
	for i, chat := range chats {
		fmt.Fprintf(&b, "### 3.%d %s\n\n", i+1, chat)
		for j, item := range grouped[chat] {
			fmt.Fprintf(&b, "#### @ %d：%s\n\n", j+1, inferTopic(mentionTexts(item)))
			status := "待处理"
			if item.Status == StatusReplied {
				status = "已回复"
			}
			fmt.Fprintf(&b, "- 状态：%s\n", status)
			fmt.Fprintf(&b, "- 时间：%s\n", item.Time)
			fmt.Fprintf(&b, "- 发起人：%s\n", displayName(item.SenderName, item.Sender))
			fmt.Fprintf(&b, "- 核心问题：%s\n", renderContentWithVision(item.Content, item.MediaRefs, item.ImageAnalysis, item.ImageAnalysisError))
			if !analysisEmpty(item.Analysis) {
				if item.Analysis.Topic != "" {
					fmt.Fprintf(&b, "- AI 主题：%s\n", item.Analysis.Topic)
				}
				if item.Analysis.Context != "" {
					fmt.Fprintf(&b, "- AI 判断：%s\n", item.Analysis.Context)
				}
				if item.Analysis.SuggestedAction != "" {
					fmt.Fprintf(&b, "- AI 建议：%s\n", item.Analysis.SuggestedAction)
				}
			}
			context := append([]ContextMessage{}, tailContext(item.Before, 3)...)
			context = append(context, headContext(item.After, 4)...)
			if len(context) > 0 {
				b.WriteString("- 对话脉络：\n")
				for _, msg := range context {
					fmt.Fprintf(&b, "  - %s %s：%s\n", shortClock(msg.Time), displayName(msg.SenderName, msg.Sender), renderContentWithVision(msg.Content, msg.MediaRefs, msg.ImageAnalysis, msg.ImageAnalysisError))
				}
			}
			if len(item.Replies) == 0 {
				b.WriteString("- 建议动作：需要回看上下文并补回复；若关键证据在图片里，以视觉摘要为准再确认原图。\n\n")
			} else {
				b.WriteString("- 我的回复：\n")
				for _, reply := range item.Replies {
					fmt.Fprintf(&b, "  - %s：%s\n", reply.Time, reply.Content)
				}
				b.WriteString("\n")
			}
		}
	}

	b.WriteString("## 4. 私聊更新解析\n\n")
	for i, update := range report.PrivateUpdates {
		msg := update.LatestMessage
		fmt.Fprintf(&b, "### 4.%d %s\n\n", i+1, update.ChatName)
		fmt.Fprintf(&b, "- 消息量：总 %d，对方 %d，我 %d\n", update.TotalMessages, update.IncomingMessages, update.SelfMessages)
		if update.NeedsReply {
			b.WriteString("- 状态：待回复\n")
		} else {
			b.WriteString("- 状态：已跟进/无需立即回复\n")
		}
		fmt.Fprintf(&b, "- 最新消息：%s\n", renderContentWithVision(msg.Content, msg.MediaRefs, msg.ImageAnalysis, msg.ImageAnalysisError))
		if update.Summary != "" {
			fmt.Fprintf(&b, "- AI 摘要：%s\n", update.Summary)
		}
		fmt.Fprintf(&b, "- 对话类型：%s\n\n", inferTopic([]string{msg.Content, msg.ImageAnalysis}))
	}

	b.WriteString("## 5. 待办合并\n\n")
	if len(report.Todos) == 0 {
		b.WriteString("无自动待办。\n")
		return b.String(), nil
	}
	for _, todo := range report.Todos {
		fmt.Fprintf(&b, "- [ ] %s\n", todo.Text)
	}
	return b.String(), nil
}

func SaveDialogueAnalysis(report *Report, outDir string) (string, error) {
	if strings.TrimSpace(outDir) == "" {
		outDir = "reports"
	}
	md, err := RenderDialogueAnalysis(report)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(outDir, fmt.Sprintf("daily-%s-dialogue-analysis.md", report.Date))
	if err := os.WriteFile(path, []byte(md), 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func mentionTexts(item MentionItem) []string {
	texts := []string{item.Content, item.ImageAnalysis}
	for _, msg := range item.Before {
		texts = append(texts, msg.Content, msg.ImageAnalysis)
	}
	for _, msg := range item.After {
		texts = append(texts, msg.Content, msg.ImageAnalysis)
	}
	return texts
}

func inferTopic(texts []string) string {
	joined := strings.Join(texts, " ")
	candidates := map[string][]string{
		"日报/报告/复盘": {"日报", "报告", "复盘", "总结", "分析"},
		"工具/配置/环境": {"配置", "环境", "安装", "启动", "报错", "接口", "服务", "权限"},
		"内容发布/素材":  {"小红书", "公众号", "视频", "封面", "文案", "发布", "选题", "素材"},
		"项目协作/交付":  {"项目", "需求", "进度", "方案", "确认", "交付", "上线", "代码", "测试"},
		"日程/沟通确认":  {"今天", "明天", "晚上", "下午", "上午", "时间", "会议", "确认"},
		"问题求助/处理":  {"看看", "看下", "帮我", "怎么", "为什么", "问题", "处理", "解决"},
	}
	type scored struct {
		name  string
		score int
	}
	var scores []scored
	for name, words := range candidates {
		score := 0
		for _, word := range words {
			score += strings.Count(joined, word)
		}
		if score > 0 {
			scores = append(scores, scored{name: name, score: score})
		}
	}
	sort.Slice(scores, func(i, j int) bool {
		if scores[i].score == scores[j].score {
			return scores[i].name < scores[j].name
		}
		return scores[i].score > scores[j].score
	})
	if len(scores) == 0 {
		return "未明确主题"
	}
	return scores[0].name
}

func headContext(items []ContextMessage, n int) []ContextMessage {
	if len(items) <= n {
		return items
	}
	return items[:n]
}

func tailContext(items []ContextMessage, n int) []ContextMessage {
	if len(items) <= n {
		return items
	}
	return items[len(items)-n:]
}

func countImageRefs(report *Report) int {
	count := 0
	for _, item := range report.Mentions {
		count += len(item.MediaRefs)
		for _, msg := range item.Before {
			count += len(msg.MediaRefs)
		}
		for _, msg := range item.After {
			count += len(msg.MediaRefs)
		}
	}
	for _, update := range report.PrivateUpdates {
		count += len(update.LatestMessage.MediaRefs)
	}
	return count
}
