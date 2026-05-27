package http

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/sjzar/chatlog/internal/chatlog/conf"
	"github.com/sjzar/chatlog/internal/chatlog/dailyreport"
	"github.com/sjzar/chatlog/internal/chatlog/semantic"
	"github.com/sjzar/chatlog/internal/errors"
)

type dailyReportSaveReq struct {
	Date                string   `json:"date"`
	Mention             string   `json:"mention"`
	Aliases             []string `json:"aliases"`
	Before              int      `json:"before"`
	After               int      `json:"after"`
	ReplyWindowMinutes  int      `json:"reply_window_minutes"`
	IncludePrivate      *bool    `json:"include_private"`
	Summary             bool     `json:"summary"`
	Vision              bool     `json:"vision"`
	MaxImages           int      `json:"max_images"`
	AnalysisConcurrency int      `json:"analysis_concurrency"`
	Format              string   `json:"format"`
	OutDir              string   `json:"out_dir"`
}

type dailyHTTPVisionClient struct {
	client *semantic.Client
	cfg    conf.SemanticConfig
}

func (s *Service) handleDailyReport(c *gin.Context) {
	opts, format, ok := parseDailyReportQuery(c)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), dailyReportTimeout(opts))
	defer cancel()
	report, err := dailyreport.GenerateDailyReport(ctx, s.db, opts)
	if err != nil {
		errors.Err(c, err)
		return
	}
	if err := s.applyDailyAIAnalysis(ctx, report, opts, ""); err != nil {
		errors.Err(c, err)
		return
	}
	switch format {
	case "markdown":
		md, err := dailyreport.RenderMarkdown(report)
		if err != nil {
			errors.Err(c, err)
			return
		}
		c.Data(http.StatusOK, "text/markdown; charset=utf-8", []byte(md))
	case "json", "":
		c.JSON(http.StatusOK, report)
	default:
		errors.Err(c, errors.InvalidArg("format"))
	}
}

func (s *Service) handleDailyReportSave(c *gin.Context) {
	var req dailyReportSaveReq
	if err := c.ShouldBindJSON(&req); err != nil {
		errors.Err(c, errors.InvalidArg("body"))
		return
	}
	includePrivate := true
	if req.IncludePrivate != nil {
		includePrivate = *req.IncludePrivate
	}
	opts := dailyreport.ReportOptions{
		Date:                req.Date,
		Mention:             req.Mention,
		MentionAliases:      req.Aliases,
		BeforeCount:         req.Before,
		AfterCount:          req.After,
		ReplyWindowMinutes:  req.ReplyWindowMinutes,
		IncludePrivate:      includePrivate,
		Summary:             req.Summary,
		Vision:              req.Vision,
		MaxImages:           req.MaxImages,
		AnalysisConcurrency: req.AnalysisConcurrency,
	}
	opts = dailyreport.NormalizeOptions(opts)
	outDir, err := resolveDailyReportOutputDir(s.conf.GetWorkDir(), req.OutDir)
	if err != nil {
		errors.Err(c, err)
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), dailyReportTimeout(opts))
	defer cancel()
	report, err := dailyreport.GenerateDailyReport(ctx, s.db, opts)
	if err != nil {
		errors.Err(c, err)
		return
	}
	if err := s.applyDailyAIAnalysis(ctx, report, opts, outDir); err != nil {
		errors.Err(c, err)
		return
	}
	result, err := dailyreport.SaveReport(report, outDir, "both")
	if err != nil {
		errors.Err(c, err)
		return
	}
	resp := gin.H{
		"ok":            true,
		"markdown_path": result.MarkdownPath,
		"json_path":     result.JSONPath,
	}
	if opts.Vision || opts.Summary {
		if path, err := dailyreport.SaveDialogueAnalysis(report, outDir); err == nil {
			resp["dialogue_analysis_path"] = path
		}
	}
	c.JSON(http.StatusOK, resp)
}

func parseDailyReportQuery(c *gin.Context) (dailyreport.ReportOptions, string, bool) {
	before, ok := parseDailyInt(c, "before", dailyreport.DefaultBeforeCount)
	if !ok {
		return dailyreport.ReportOptions{}, "", false
	}
	after, ok := parseDailyInt(c, "after", dailyreport.DefaultAfterCount)
	if !ok {
		return dailyreport.ReportOptions{}, "", false
	}
	replyWindow, ok := parseDailyInt(c, "reply_window_minutes", dailyreport.DefaultReplyWindowMinute)
	if !ok {
		return dailyreport.ReportOptions{}, "", false
	}
	includePrivate, ok := parseDailyBool(c, "include_private", true)
	if !ok {
		return dailyreport.ReportOptions{}, "", false
	}
	summary, ok := parseDailyBool(c, "summary", false)
	if !ok {
		return dailyreport.ReportOptions{}, "", false
	}
	vision, ok := parseDailyBool(c, "vision", false)
	if !ok {
		return dailyreport.ReportOptions{}, "", false
	}
	maxImages, ok := parseDailyInt(c, "max_images", dailyreport.DefaultMaxImages)
	if !ok {
		return dailyreport.ReportOptions{}, "", false
	}
	analysisConcurrency, ok := parseDailyInt(c, "analysis_concurrency", 0)
	if !ok {
		return dailyreport.ReportOptions{}, "", false
	}
	aliases := splitDailyAliases(c.Query("aliases"))
	opts := dailyreport.ReportOptions{
		Date:                c.DefaultQuery("date", "today"),
		Mention:             c.DefaultQuery("mention", dailyreport.DefaultMention),
		MentionAliases:      aliases,
		BeforeCount:         before,
		AfterCount:          after,
		ReplyWindowMinutes:  replyWindow,
		IncludePrivate:      includePrivate,
		Summary:             summary,
		Vision:              vision,
		MaxImages:           maxImages,
		AnalysisConcurrency: analysisConcurrency,
	}
	return dailyreport.NormalizeOptions(opts), strings.ToLower(strings.TrimSpace(c.DefaultQuery("format", "json"))), true
}

func parseDailyInt(c *gin.Context, key string, fallback int) (int, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return fallback, true
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		errors.Err(c, errors.InvalidArg(key))
		return 0, false
	}
	return n, true
}

func parseDailyBool(c *gin.Context, key string, fallback bool) (bool, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return fallback, true
	}
	switch strings.ToLower(raw) {
	case "1", "true", "yes", "on":
		return true, true
	case "0", "false", "no", "off":
		return false, true
	default:
		errors.Err(c, errors.InvalidArg(key))
		return false, false
	}
}

func splitDailyAliases(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func resolveDailyReportOutputDir(workDir, outDir string) (string, error) {
	base := strings.TrimSpace(workDir)
	if base == "" {
		base = "."
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(outDir) == "" {
		outDir = "reports"
	}
	clean := filepath.Clean(outDir)
	var target string
	if filepath.IsAbs(clean) {
		target = clean
	} else {
		target = filepath.Join(baseAbs, clean)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(baseAbs, targetAbs)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.InvalidArg("out_dir")
	}
	return targetAbs, nil
}

func (s *Service) applyDailyAIAnalysis(ctx context.Context, report *dailyreport.Report, opts dailyreport.ReportOptions, outDir string) error {
	if !opts.Vision && !opts.Summary {
		return nil
	}
	if strings.TrimSpace(outDir) == "" {
		outDir = filepath.Join(s.conf.GetWorkDir(), "reports")
	}
	semCfg := s.conf.GetSemanticConfig()
	concurrency := opts.AnalysisConcurrency
	if concurrency <= 0 && semCfg.ChatProvider == conf.ProviderMMX {
		concurrency = semantic.MiniMaxConfiguredKeyCount()
	}
	return dailyreport.ApplyAIAnalysis(ctx, report,
		dailyreport.LocalImageResolver{DataDir: s.conf.GetDataDir(), ImgKey: s.conf.GetImgKey(), Media: s.db},
		dailyHTTPVisionClient{client: semantic.NewClient(), cfg: *semCfg},
		dailyreport.AIAnalysisConfig{
			Vision: dailyreport.VisionConfig{
				Enabled:   opts.Vision,
				MaxImages: opts.MaxImages,
				CachePath: filepath.Join(outDir, ".image-analysis-cache.json"),
				Provider:  semCfg.ChatProvider,
				Model:     semCfg.ChatModel,
			},
			Summary:     opts.Summary,
			Concurrency: concurrency,
		})
}

func (c dailyHTTPVisionClient) AnalyzeImage(ctx context.Context, prompt string, imageData []byte, mimeType string) (string, error) {
	return c.client.AnalyzeImage(ctx, c.cfg, prompt, imageData, mimeType)
}

func (c dailyHTTPVisionClient) AnalyzeText(ctx context.Context, title, data, instruction string) (string, error) {
	return c.client.Chat(ctx, c.cfg, []semantic.ChatMessage{
		{Role: "system", Content: "你是本地微信聊天记录分析助手。只能基于输入证据输出，不要编造。"},
		{Role: "user", Content: title + "\n\n数据：\n" + data + "\n\n要求：\n" + instruction},
	})
}

func dailyReportTimeout(opts dailyreport.ReportOptions) time.Duration {
	if opts.Vision || opts.Summary {
		return 5 * time.Minute
	}
	return 60 * time.Second
}

func (r *dailyReportSaveReq) UnmarshalJSON(b []byte) error {
	type alias dailyReportSaveReq
	var a alias
	if err := json.Unmarshal(b, &a); err != nil {
		var flexible struct {
			Date                string          `json:"date"`
			Mention             string          `json:"mention"`
			Aliases             json.RawMessage `json:"aliases"`
			Before              int             `json:"before"`
			After               int             `json:"after"`
			ReplyWindowMinutes  int             `json:"reply_window_minutes"`
			IncludePrivate      *bool           `json:"include_private"`
			Summary             bool            `json:"summary"`
			Vision              bool            `json:"vision"`
			MaxImages           int             `json:"max_images"`
			AnalysisConcurrency int             `json:"analysis_concurrency"`
			Format              string          `json:"format"`
			OutDir              string          `json:"out_dir"`
		}
		if err2 := json.Unmarshal(b, &flexible); err2 != nil {
			return err
		}
		a.Date = flexible.Date
		a.Mention = flexible.Mention
		a.Before = flexible.Before
		a.After = flexible.After
		a.ReplyWindowMinutes = flexible.ReplyWindowMinutes
		a.IncludePrivate = flexible.IncludePrivate
		a.Summary = flexible.Summary
		a.Vision = flexible.Vision
		a.MaxImages = flexible.MaxImages
		a.AnalysisConcurrency = flexible.AnalysisConcurrency
		a.Format = flexible.Format
		a.OutDir = flexible.OutDir
		if len(flexible.Aliases) > 0 {
			var list []string
			if err := json.Unmarshal(flexible.Aliases, &list); err == nil {
				a.Aliases = list
			} else {
				var raw string
				if err := json.Unmarshal(flexible.Aliases, &raw); err == nil {
					a.Aliases = splitDailyAliases(raw)
				}
			}
		}
	}
	*r = dailyReportSaveReq(a)
	return nil
}
