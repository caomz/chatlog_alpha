package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sjzar/chatlog/internal/chatlog/temporalgraph"
	"github.com/sjzar/chatlog/internal/errors"
)

func (s *Service) requireGraph(c *gin.Context) *temporalgraph.Manager {
	if s.graph == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "temporal graph manager unavailable"})
		return nil
	}
	return s.graph
}

func (s *Service) handleGraphStatus(c *gin.Context) {
	g := s.requireGraph(c)
	if g == nil {
		return
	}
	writeByFormat(c, g.Status(), c.Query("format"))
}

func (s *Service) handleGraphConfigGet(c *gin.Context) {
	g := s.requireGraph(c)
	if g == nil {
		return
	}
	st := g.Status()
	writeByFormat(c, gin.H{
		"workers":         st.Workers,
		"enqueue_workers": st.EnqueueWorkers,
	}, c.Query("format"))
}

func (s *Service) handleGraphConfigSet(c *gin.Context) {
	g := s.requireGraph(c)
	if g == nil {
		return
	}
	var req struct {
		Workers        int `json:"workers"`
		EnqueueWorkers int `json:"enqueue_workers"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.Err(c, errors.InvalidArg("body"))
		return
	}
	if req.Workers <= 0 {
		req.Workers = 1
	}
	if req.EnqueueWorkers <= 0 {
		req.EnqueueWorkers = 1
	}
	if err := g.SetWorkers(req.Workers, req.EnqueueWorkers); err != nil {
		errors.Err(c, err)
		return
	}
	writeByFormat(c, gin.H{"ok": true, "status": g.Status()}, c.Query("format"))
}

func (s *Service) handleGraphIngestMessage(c *gin.Context) {
	g := s.requireGraph(c)
	if g == nil {
		return
	}
	var batch []temporalgraph.IngestMessage
	if err := bindSingleOrBatch(c, &batch); err != nil {
		errors.Err(c, err)
		return
	}
	ids := make([]int64, 0, len(batch))
	for _, item := range batch {
		id, err := g.IngestMessage(c.Request.Context(), item)
		if err != nil {
			errors.Err(c, err)
			return
		}
		ids = append(ids, id)
	}
	writeByFormat(c, gin.H{"ok": true, "count": len(ids), "ids": ids, "status": g.Status()}, c.Query("format"))
}

func (s *Service) handleGraphIngestBusiness(c *gin.Context) {
	g := s.requireGraph(c)
	if g == nil {
		return
	}
	var batch []temporalgraph.IngestBusiness
	if err := bindSingleOrBatch(c, &batch); err != nil {
		errors.Err(c, err)
		return
	}
	ids := make([]int64, 0, len(batch))
	for _, item := range batch {
		id, err := g.IngestBusiness(c.Request.Context(), item)
		if err != nil {
			errors.Err(c, err)
			return
		}
		ids = append(ids, id)
	}
	writeByFormat(c, gin.H{"ok": true, "count": len(ids), "ids": ids, "status": g.Status()}, c.Query("format"))
}

func (s *Service) handleGraphIngestEvent(c *gin.Context) {
	g := s.requireGraph(c)
	if g == nil {
		return
	}
	var batch []temporalgraph.IngestEvent
	if err := bindSingleOrBatch(c, &batch); err != nil {
		errors.Err(c, err)
		return
	}
	ids := make([]int64, 0, len(batch))
	for _, item := range batch {
		id, err := g.IngestEvent(c.Request.Context(), item)
		if err != nil {
			errors.Err(c, err)
			return
		}
		ids = append(ids, id)
	}
	writeByFormat(c, gin.H{"ok": true, "count": len(ids), "ids": ids, "status": g.Status()}, c.Query("format"))
}

func bindSingleOrBatch[T any](c *gin.Context, out *[]T) error {
	raw, err := c.GetRawData()
	if err != nil {
		return err
	}
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return fmt.Errorf("empty request body")
	}
	if raw[0] == '[' {
		return json.Unmarshal(raw, out)
	}
	var item T
	if err := json.Unmarshal(raw, &item); err != nil {
		return err
	}
	*out = []T{item}
	return nil
}

func (s *Service) handleGraphRebuild(c *gin.Context) {
	g := s.requireGraph(c)
	if g == nil {
		return
	}
	var req struct {
		Reset bool `json:"reset"`
	}
	_ = c.ShouldBindJSON(&req)
	if c.Query("reset") == "1" || c.Query("reset") == "true" {
		req.Reset = true
	}
	if err := g.Rebuild(context.Background(), req.Reset); err != nil {
		errors.Err(c, err)
		return
	}
	writeByFormat(c, gin.H{"ok": true, "accepted": true, "status": g.Status()}, c.Query("format"))
}

func (s *Service) handleGraphPause(c *gin.Context) {
	g := s.requireGraph(c)
	if g == nil {
		return
	}
	if err := g.Pause(); err != nil {
		errors.Err(c, err)
		return
	}
	writeByFormat(c, gin.H{"ok": true, "status": g.Status()}, c.Query("format"))
}

func (s *Service) handleGraphResume(c *gin.Context) {
	g := s.requireGraph(c)
	if g == nil {
		return
	}
	if err := g.Resume(); err != nil {
		errors.Err(c, err)
		return
	}
	writeByFormat(c, gin.H{"ok": true, "status": g.Status()}, c.Query("format"))
}

func (s *Service) handleGraphQuery(c *gin.Context) {
	g := s.requireGraph(c)
	if g == nil {
		return
	}
	start, end := graphTimeRange(c)
	limit := graphLimit(c)
	result, err := g.Query(c.Query("keyword"), c.Query("entity"), c.Query("relation"), start, end, limit)
	if err != nil {
		errors.Err(c, err)
		return
	}
	writeByFormat(c, result, c.Query("format"))
}

func (s *Service) handleGraphTimeline(c *gin.Context) {
	g := s.requireGraph(c)
	if g == nil {
		return
	}
	start, end := graphTimeRange(c)
	items, err := g.Timeline(c.Query("keyword"), start, end, graphLimit(c))
	if err != nil {
		errors.Err(c, err)
		return
	}
	writeByFormat(c, gin.H{"items": items, "count": len(items)}, c.Query("format"))
}

func (s *Service) handleGraphVisualize(c *gin.Context) {
	g := s.requireGraph(c)
	if g == nil {
		return
	}
	start, end := graphTimeRange(c)
	result, err := g.Visualize(c.Query("keyword"), start, end, graphLimit(c))
	if err != nil {
		errors.Err(c, err)
		return
	}
	writeByFormat(c, result, c.Query("format"))
}

func (s *Service) handleGraphQA(c *gin.Context) {
	g := s.requireGraph(c)
	if g == nil {
		return
	}
	var req struct {
		Query  string `json:"query"`
		Window string `json:"window"`
		Start  string `json:"start"`
		End    string `json:"end"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.Err(c, err)
		return
	}
	start, end := graphWindow(req.Window, req.Start, req.End)
	answer, evidence, err := g.QA(c.Request.Context(), strings.TrimSpace(req.Query), start, end)
	if err != nil {
		errors.Err(c, err)
		return
	}
	writeByFormat(c, gin.H{"answer": answer, "evidence": evidence}, c.Query("format"))
}

func graphLimit(c *gin.Context) int {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "80"))
	if limit <= 0 {
		limit = 80
	}
	if limit > 300 {
		limit = 300
	}
	return limit
}

func graphTimeRange(c *gin.Context) (time.Time, time.Time) {
	return graphWindow(c.DefaultQuery("window", ""), c.Query("start"), c.Query("end"))
}

func graphWindow(window, startRaw, endRaw string) (time.Time, time.Time) {
	now := time.Now()
	var start, end time.Time
	if strings.TrimSpace(startRaw) != "" {
		start = parseGraphTime(startRaw)
	}
	if strings.TrimSpace(endRaw) != "" {
		end = parseGraphTime(endRaw)
	}
	if !start.IsZero() || !end.IsZero() {
		return start, end
	}
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "today", "1d":
		y, m, d := now.Date()
		return time.Date(y, m, d, 0, 0, 0, 0, now.Location()), now
	case "7d":
		return now.AddDate(0, 0, -7), now
	case "30d":
		return now.AddDate(0, -1, 0), now
	case "90d":
		return now.AddDate(0, -3, 0), now
	case "1y":
		return now.AddDate(-1, 0, 0), now
	default:
		return time.Time{}, time.Time{}
	}
}

func parseGraphTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	layouts := []string{time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"}
	for _, layout := range layouts {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}

func (s *Service) handleGraphDigest(c *gin.Context) {
	g := s.requireGraph(c)
	if g == nil {
		return
	}

	// Parse parameters: days or start/end, summary flag.
	daysStr := c.DefaultQuery("days", "7")
	days, _ := strconv.Atoi(daysStr)
	summaryEnabled := strings.ToLower(c.DefaultQuery("summary", "false")) == "true"

	// Resolve time window.
	var start, end time.Time
	if startRaw := c.Query("start"); startRaw != "" {
		if endRaw := c.Query("end"); endRaw != "" {
			start = parseGraphTime(startRaw)
			end = parseGraphTime(endRaw)
			if start.IsZero() || end.IsZero() || start.After(end) {
				c.JSON(http.StatusBadRequest, gin.H{"error": "invalid start/end times or start > end"})
				return
			}
		}
	} else if days > 0 {
		now := time.Now()
		end = now
		start = now.AddDate(0, 0, -days)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"error": "days must be > 0 or start/end must be provided"})
		return
	}

	// Call Digest with read-only aggregation.
	result, err := g.Digest(start, end, temporalgraph.DigestOptions{
		MaxEntities:   20,
		MaxEvents:     50,
		MaxEventsScan: 2000,
		EnableSummary: summaryEnabled,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to aggregate digest: " + err.Error()})
		return
	}

	// Render Markdown.
	markdown := temporalgraph.RenderDigestMarkdown(result)

	// Determine output path: CWD/reports/ (not sandboxed like daily report).
	wd, _ := os.Getwd()
	reportsDir := filepath.Join(wd, "reports")

	// Create reports directory if needed.
	if err := os.MkdirAll(reportsDir, 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cannot create reports directory"})
		return
	}

	// Write file with idempotent name.
	startStr := start.Format("2006-01-02")
	endStr := end.Format("2006-01-02")
	filename := fmt.Sprintf("graph-digest-%s_%s.md", startStr, endStr)
	filePath := filepath.Join(reportsDir, filename)

	if err := os.WriteFile(filePath, []byte(markdown), 0o644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to write digest file"})
		return
	}

	// Get file info for response metadata.
	fileInfo, err := os.Stat(filePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to stat file"})
		return
	}

	// format=json: return metadata only, no chat content.
	if c.Query("format") == "json" {
		c.JSON(http.StatusOK, gin.H{
			"path":           filePath,
			"size_bytes":     fileInfo.Size(),
			"window_start":   result.StartTime.Format(time.RFC3339),
			"window_end":     result.EndTime.Format(time.RFC3339),
			"entity_count":   len(result.TopEntities),
			"event_count":    len(result.EventTimeline),
			"fact_count":     result.FactCount,
			"relation_count": result.RelationCount,
			"summary_used":   result.SummaryUsed,
		})
		return
	}

	// Default: return path and counts.
	c.JSON(http.StatusOK, gin.H{
		"ok":             true,
		"path":           filePath,
		"window_start":   result.StartTime.Format(time.RFC3339),
		"window_end":     result.EndTime.Format(time.RFC3339),
		"entity_count":   len(result.TopEntities),
		"event_count":    len(result.EventTimeline),
		"fact_count":     result.FactCount,
		"relation_count": result.RelationCount,
		"summary_used":   result.SummaryUsed,
	})
}
