package temporalgraph

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sjzar/chatlog/internal/chatlog/conf"
	"github.com/sjzar/chatlog/internal/chatlog/semantic"
)

// DigestOptions configures Digest aggregation behavior.
type DigestOptions struct {
	// MaxEntities limits the top entities list; defaults to 20.
	MaxEntities int
	// MaxEvents limits the event timeline; defaults to 50.
	MaxEvents int
	// MaxEventsScan limits the number of graph_events rows scanned; defaults to 2000.
	MaxEventsScan int
	// EnableSummary requests optional Chat provider summarization (at most 1 call).
	EnableSummary bool
	// ChatInvoker is used when EnableSummary is true; ignored otherwise.
	ChatInvoker ChatInvoker
}

// DigestEntity represents a top entity in the digest with its window-scoped mention count.
type DigestEntity struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// DigestEventItem represents a single event in the timeline.
type DigestEventItem struct {
	Time  int64  `json:"time"`
	Title string `json:"title"`
}

// DigestResult is the aggregated digest of a temporal graph window.
type DigestResult struct {
	// Time window
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`

	// Top entities by mention count in this window
	TopEntities []DigestEntity `json:"top_entities"`

	// Event timeline (time + title)
	EventTimeline []DigestEventItem `json:"event_timeline"`

	// Counts
	FactCount     int `json:"fact_count"`
	RelationCount int `json:"relation_count"`
	SourceCount   int `json:"source_count"`

	// Summary (optional, from Chat provider)
	SummaryContent string `json:"summary_content,omitempty"`
	SummaryError   string `json:"summary_error,omitempty"`
	SummaryUsed    bool   `json:"summary_used"`

	// Indicators
	Truncated bool `json:"truncated"`
}

// Digest returns a read-only aggregation of graph entities/events/facts over a time window.
// It scans graph_events within the window to tally entity mentions from actors/targets JSON.
// No Chat provider calls are made; no queue state is modified.
func (m *Manager) Digest(start, end time.Time, opts DigestOptions) (DigestResult, error) {
	if opts.MaxEntities <= 0 {
		opts.MaxEntities = 20
	}
	if opts.MaxEvents <= 0 {
		opts.MaxEvents = 50
	}
	if opts.MaxEventsScan <= 0 {
		opts.MaxEventsScan = 2000
	}

	if m.store == nil {
		return DigestResult{}, fmt.Errorf("graph store not initialized")
	}

	startTS := start.Unix()
	endTS := end.Unix()

	// Query events within the window, scanning up to MaxEventsScan rows.
	// Query one extra to detect truncation.
	rows, err := m.store.db.Query(`
SELECT event_time, title, actors_json, targets_json
FROM graph_events
WHERE event_time BETWEEN ? AND ?
ORDER BY event_time DESC
LIMIT ?
	`, startTS, endTS, opts.MaxEventsScan+1)
	if err != nil {
		return DigestResult{}, fmt.Errorf("failed to query graph events: %w", err)
	}
	defer rows.Close()

	// Count entities and collect timeline.
	entityCounts := make(map[string]int)
	var timeline []DigestEventItem
	eventCount := 0
	truncated := false

	for rows.Next() {
		var eventTime int64
		var title, actorsJSON, targetsJSON string

		if err := rows.Scan(&eventTime, &title, &actorsJSON, &targetsJSON); err != nil {
			return DigestResult{}, fmt.Errorf("failed to scan event: %w", err)
		}

		eventCount++
		if eventCount > opts.MaxEventsScan {
			truncated = true
			break
		}

		// Add to timeline (up to MaxEvents).
		if len(timeline) < opts.MaxEvents {
			timeline = append(timeline, DigestEventItem{
				Time:  eventTime,
				Title: title,
			})
		}

		// Tally entities from actors.
		var actors []string
		if actorsJSON != "" {
			_ = json.Unmarshal([]byte(actorsJSON), &actors)
		}
		for _, actor := range actors {
			if actor != "" {
				entityCounts[actor]++
			}
		}

		// Tally entities from targets.
		var targets []string
		if targetsJSON != "" {
			_ = json.Unmarshal([]byte(targetsJSON), &targets)
		}
		for _, target := range targets {
			if target != "" {
				entityCounts[target]++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return DigestResult{}, fmt.Errorf("error scanning events: %w", err)
	}

	// Build top entities list sorted by count.
	type entityCountPair struct {
		name  string
		count int
	}
	var entityList []entityCountPair
	for name, count := range entityCounts {
		entityList = append(entityList, entityCountPair{name, count})
	}
	sort.Slice(entityList, func(i, j int) bool {
		if entityList[i].count != entityList[j].count {
			return entityList[i].count > entityList[j].count
		}
		return entityList[i].name < entityList[j].name
	})

	topEntities := make([]DigestEntity, 0, opts.MaxEntities)
	for i, pair := range entityList {
		if i >= opts.MaxEntities {
			break
		}
		topEntities = append(topEntities, DigestEntity{
			Name:  pair.name,
			Count: pair.count,
		})
	}

	// Count facts in the window.
	var factCount int
	_ = m.store.db.QueryRow(`
SELECT COUNT(1) FROM graph_facts WHERE valid_from BETWEEN ? AND ?
	`, startTS, endTS).Scan(&factCount)

	// Count relations in the window.
	var relationCount int
	_ = m.store.db.QueryRow(`
SELECT COUNT(1) FROM graph_relations WHERE last_seen BETWEEN ? AND ?
	`, startTS, endTS).Scan(&relationCount)

	// Count sources in the window.
	var sourceCount int
	_ = m.store.db.QueryRow(`
SELECT COUNT(1) FROM graph_source_records WHERE event_time BETWEEN ? AND ?
	`, startTS, endTS).Scan(&sourceCount)

	result := DigestResult{
		StartTime:     start,
		EndTime:       end,
		TopEntities:   topEntities,
		EventTimeline: timeline,
		FactCount:     factCount,
		RelationCount: relationCount,
		SourceCount:   sourceCount,
		Truncated:     truncated,
		SummaryUsed:   false,
	}

	// Optionally invoke Chat provider for summarization.
	// Fall back to the manager's own semantic client when no invoker is injected (HTTP path).
	invoker := opts.ChatInvoker
	if opts.EnableSummary && invoker == nil && m.client != nil {
		invoker = m.client
	}
	if opts.EnableSummary && invoker != nil && m.conf != nil {
		semCfg := m.conf.GetSemanticConfig()
		if semCfg != nil {
			summaryContent, summaryErr := digestGenerateSummary(invoker, *semCfg, result)
			if summaryErr != nil {
				// Graceful degradation: log error class only, not prompt or output.
				result.SummaryError = errorBucketForSummary(summaryErr)
			} else {
				result.SummaryContent = summaryContent
				result.SummaryUsed = true
			}
		}
	}

	return result, nil
}

// digestGenerateSummary calls the Chat provider to generate a role-neutral summary.
// It constructs a prompt from the digest result and returns summary content or error.
func digestGenerateSummary(invoker ChatInvoker, cfg conf.SemanticConfig, result DigestResult) (string, error) {
	// Build role-neutral prompt from result data (no private content, no sender IDs).
	var prompt strings.Builder
	prompt.WriteString("请根据以下图谱信息生成一份简要综述。\n\n")

	// Summary details.
	prompt.WriteString(fmt.Sprintf("时间范围：%s 至 %s\n", result.StartTime.Format("2006-01-02"), result.EndTime.Format("2006-01-02")))

	// Key entities (role-neutral: just names and counts).
	if len(result.TopEntities) > 0 {
		prompt.WriteString("\n关键实体：\n")
		for i, entity := range result.TopEntities {
			if i >= 10 {
				break
			}
			prompt.WriteString(fmt.Sprintf("- %s (%d 次)\n", entity.Name, entity.Count))
		}
	}

	// Event summary (role-neutral: just counts).
	prompt.WriteString(fmt.Sprintf("\n事件数：%d，新增事实：%d，关系数：%d\n", len(result.EventTimeline), result.FactCount, result.RelationCount))

	// Role-neutral instruction.
	prompt.WriteString("\n请按以下几点生成摘要（勿涉及个人身份、敏感信息）：\n")
	prompt.WriteString("1. 核心主题与趋势\n")
	prompt.WriteString("2. 待跟进的关键项\n")

	// Call Chat provider.
	messages := []semantic.ChatMessage{
		{Role: "user", Content: prompt.String()},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	response, err := invoker.Chat(ctx, cfg, messages)
	if err != nil {
		return "", err
	}

	return response, nil
}

// errorBucketForSummary classifies a summary error into a bucket name (error class only, no details).
func errorBucketForSummary(err error) string {
	if err == nil {
		return ""
	}

	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "not configured"), strings.Contains(msg, "unavailable"):
		return "chat_provider_not_configured"
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline exceeded"):
		return "chat_timeout"
	case strings.Contains(msg, "auth"), strings.Contains(msg, "permission"), strings.Contains(msg, "401"):
		return "chat_auth_error"
	case strings.Contains(msg, "rate limit"), strings.Contains(msg, "429"):
		return "chat_rate_limit"
	default:
		return "chat_error"
	}
}
