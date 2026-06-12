package temporalgraph

import (
	"fmt"
	"strings"
	"time"
)

// RenderDigestMarkdown renders a DigestResult into Obsidian-friendly Markdown
// with YAML frontmatter and 7 fixed H2 sections.
// The function is pure and does not import daily report or other heavy dependencies.
func RenderDigestMarkdown(result DigestResult) string {
	var sb strings.Builder

	// YAML frontmatter
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("date: %s\n", result.StartTime.Format("2006-01-02")))
	sb.WriteString(fmt.Sprintf("window_start: %s\n", result.StartTime.Format("2006-01-02T15:04:05Z07:00")))
	sb.WriteString(fmt.Sprintf("window_end: %s\n", result.EndTime.Format("2006-01-02T15:04:05Z07:00")))
	sb.WriteString("source: chatlog temporal graph\n")
	sb.WriteString("tags: [chatlog, graph-digest]\n")
	sb.WriteString("---\n\n")

	// H1 title with window dates
	startStr := result.StartTime.Format("2006-01-02")
	endStr := result.EndTime.Format("2006-01-02")
	sb.WriteString(fmt.Sprintf("# Graph Digest: %s to %s\n\n", startStr, endStr))

	// Section 1: 一句话结论
	sb.WriteString("## 一句话结论\n")
	oneLiner := summaryOneLiner(result)
	sb.WriteString(fmt.Sprintf("%s\n\n", oneLiner))

	// Section 2: 核心主题
	sb.WriteString("## 核心主题\n")
	if result.TopEntities == nil || len(result.TopEntities) == 0 {
		sb.WriteString("(需开启 summary 生成)\n\n")
	} else {
		sb.WriteString("(需开启 summary 生成)\n\n")
	}

	// Section 3: 关键实体
	sb.WriteString("## 关键实体\n")
	if len(result.TopEntities) > 0 {
		for _, entity := range result.TopEntities {
			sb.WriteString(fmt.Sprintf("- **%s**: %d 次提及\n", entity.Name, entity.Count))
		}
	} else {
		sb.WriteString("无关键实体。\n")
	}
	sb.WriteString("\n")

	// Section 4: 事件时间线
	sb.WriteString("## 事件时间线\n")
	if len(result.EventTimeline) > 0 {
		for _, event := range result.EventTimeline {
			timeStr := time.Unix(event.Time, 0).Format("2006-01-02 15:04")
			sb.WriteString(fmt.Sprintf("- **%s**: %s\n", timeStr, event.Title))
		}
	} else {
		sb.WriteString("无事件。\n")
	}
	sb.WriteString("\n")

	// Section 5: 新增事实与关系
	sb.WriteString("## 新增事实与关系\n")
	sb.WriteString(fmt.Sprintf("- 事实: %d 条\n", result.FactCount))
	sb.WriteString(fmt.Sprintf("- 关系: %d 条\n", result.RelationCount))
	sb.WriteString("\n")

	// Section 6: 可复用沉淀
	sb.WriteString("## 可复用沉淀\n")
	sb.WriteString("(需开启 summary 生成)\n\n")

	// Section 7: 后续跟进
	sb.WriteString("## 后续跟进\n")
	sb.WriteString("(需开启 summary 生成)\n\n")

	return sb.String()
}

// summaryOneLiner generates a template summary based on result counts.
func summaryOneLiner(result DigestResult) string {
	entityCount := len(result.TopEntities)
	eventCount := len(result.EventTimeline)
	totalCount := result.FactCount + result.RelationCount

	if entityCount == 0 && eventCount == 0 && totalCount == 0 {
		return "本时间段无数据。"
	}

	var parts []string
	if entityCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 个关键实体", entityCount))
	}
	if eventCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 个事件", eventCount))
	}
	if totalCount > 0 {
		parts = append(parts, fmt.Sprintf("%d 条事实关系", totalCount))
	}

	return fmt.Sprintf("本时间段共涉及 %s。", strings.Join(parts, "、"))
}
