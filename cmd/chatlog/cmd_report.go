package chatlog

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/sjzar/chatlog/internal/chatlog/conf"
	"github.com/sjzar/chatlog/internal/chatlog/dailyreport"
	"github.com/sjzar/chatlog/internal/chatlog/semantic"
	"github.com/sjzar/chatlog/internal/model"
	"github.com/sjzar/chatlog/internal/wechatdb"
)

var (
	reportDate                string
	reportMention             string
	reportAliases             []string
	reportBefore              int
	reportAfter               int
	reportReplyWindow         int
	reportIncludePrivate      bool
	reportFormat              string
	reportOutDir              string
	reportHTTPBase            string
	reportVision              bool
	reportMaxImages           int
	reportSummary             bool
	reportAnalysisConcurrency int

	reportGraphDays    int
	reportGraphSummary bool
	reportGraphBaseURL string

	reportCmd = &cobra.Command{
		Use:   "report",
		Short: "Generate reports",
	}
	reportDailyCmd = &cobra.Command{
		Use:   "daily",
		Short: "Generate daily WeChat mention report",
		Run:   runReportDaily,
	}
	reportGraphCmd = &cobra.Command{
		Use:   "graph",
		Short: "Generate temporal graph digest report via HTTP service",
		Run:   runReportGraph,
	}
)

type reportDB struct {
	db *wechatdb.DB
}

type reportVisionClient struct {
	client *semantic.Client
	cfg    conf.SemanticConfig
}

func init() {
	rootCmd.AddCommand(reportCmd)
	reportCmd.AddCommand(reportDailyCmd)
	reportCmd.AddCommand(reportGraphCmd)
	reportGraphCmd.Flags().IntVar(&reportGraphDays, "days", 7, "time window in days")
	reportGraphCmd.Flags().BoolVar(&reportGraphSummary, "summary", false, "enable model summarization (consumes quota)")
	reportGraphCmd.Flags().StringVar(&reportGraphBaseURL, "base-url", "http://127.0.0.1:5030", "chatlog HTTP service base URL")
	reportDailyCmd.Flags().StringVar(&reportDate, "date", "today", "report date: today or YYYY-MM-DD")
	reportDailyCmd.Flags().StringVar(&reportMention, "mention", dailyreport.DefaultMention, "primary mention name")
	reportDailyCmd.Flags().StringArrayVar(&reportAliases, "alias", nil, "extra mention alias, repeatable")
	reportDailyCmd.Flags().IntVar(&reportBefore, "before", dailyreport.DefaultBeforeCount, "context messages before mention")
	reportDailyCmd.Flags().IntVar(&reportAfter, "after", dailyreport.DefaultAfterCount, "context messages after mention")
	reportDailyCmd.Flags().IntVar(&reportReplyWindow, "reply-window", dailyreport.DefaultReplyWindowMinute, "reply matching window in minutes")
	reportDailyCmd.Flags().BoolVar(&reportIncludePrivate, "include-private", true, "include private chat updates")
	reportDailyCmd.Flags().StringVar(&reportFormat, "format", "both", "output format: markdown, json, or both")
	reportDailyCmd.Flags().StringVar(&reportOutDir, "out", "./reports", "output directory")
	reportDailyCmd.Flags().StringVar(&reportHTTPBase, "http", "", "optional HTTP base URL, e.g. http://127.0.0.1:5030")
	reportDailyCmd.Flags().BoolVar(&reportVision, "vision", false, "analyze image messages with configured chat model")
	reportDailyCmd.Flags().IntVar(&reportMaxImages, "max-images", dailyreport.DefaultMaxImages, "maximum images to analyze when --vision is set")
	reportDailyCmd.Flags().BoolVar(&reportSummary, "summary", false, "analyze mention, group, and private chat text with configured chat model")
	reportDailyCmd.Flags().IntVar(&reportAnalysisConcurrency, "analysis-concurrency", 0, "AI analysis workers; 0 uses MiniMax API key count")
}

func runReportDaily(cmd *cobra.Command, args []string) {
	opts := dailyreport.NormalizeOptions(dailyreport.ReportOptions{
		Date:                reportDate,
		Mention:             reportMention,
		MentionAliases:      reportAliases,
		BeforeCount:         reportBefore,
		AfterCount:          reportAfter,
		ReplyWindowMinutes:  reportReplyWindow,
		IncludePrivate:      reportIncludePrivate,
		Summary:             reportSummary,
		Vision:              reportVision,
		MaxImages:           reportMaxImages,
		AnalysisConcurrency: reportAnalysisConcurrency,
	})
	format := strings.ToLower(strings.TrimSpace(reportFormat))
	if format == "" {
		format = "both"
	}
	if format != "both" && format != "markdown" && format != "json" {
		log.Error().Msg("invalid --format, expected markdown, json, or both")
		return
	}
	if strings.TrimSpace(reportHTTPBase) != "" {
		if err := runReportDailyHTTP(opts, format, reportOutDir); err != nil {
			log.Error().Err(err).Msg("daily report failed")
		}
		return
	}
	if err := runReportDailyLocal(opts, format, reportOutDir); err != nil {
		log.Error().Err(err).Msg("daily report failed")
	}
}

func runReportDailyLocal(opts dailyreport.ReportOptions, format, outDir string) error {
	cfg, _, err := conf.LoadServiceConfig("", nil)
	if err != nil {
		return err
	}
	dbPath := cfg.GetWorkDir()
	if (cfg.GetPlatform() == "darwin" || cfg.GetPlatform() == "windows") && cfg.GetVersion() == 4 {
		dbPath = cfg.GetDataDir()
	}
	db, err := wechatdb.New(dbPath, cfg.GetPlatform(), cfg.GetVersion(), cfg.GetWalEnabled(), cfg.GetDataKey())
	if err != nil {
		return err
	}
	defer db.Close()
	dbSource := reportDB{db: db}
	report, err := dailyreport.GenerateDailyReport(context.Background(), dbSource, opts)
	if err != nil {
		return err
	}
	if opts.Vision || opts.Summary {
		semCfg := cfg.GetSemanticConfig()
		concurrency := opts.AnalysisConcurrency
		if concurrency <= 0 && semCfg.ChatProvider == conf.ProviderMMX {
			concurrency = semantic.MiniMaxConfiguredKeyCount()
		}
		if err := dailyreport.ApplyAIAnalysis(context.Background(), report,
			dailyreport.LocalImageResolver{DataDir: cfg.GetDataDir(), ImgKey: cfg.GetImgKey(), Media: dbSource},
			reportVisionClient{client: semantic.NewClient(), cfg: *semCfg},
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
			}); err != nil {
			return err
		}
	}
	result, err := dailyreport.SaveReport(report, outDir, format)
	if err != nil {
		return err
	}
	if opts.Vision || opts.Summary {
		path, err := dailyreport.SaveDialogueAnalysis(report, outDir)
		if err != nil {
			return err
		}
		result.DialogueAnalysisPath = path
	}
	printReportResult(report, result)
	return nil
}

func runReportDailyHTTP(opts dailyreport.ReportOptions, format, outDir string) error {
	if strings.TrimSpace(outDir) == "" {
		outDir = "./reports"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	baseURL := strings.TrimRight(strings.TrimSpace(reportHTTPBase), "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "http://" + baseURL
	}
	result := dailyreport.SaveResult{}
	date := opts.Date
	if date == "" {
		date = "today"
	}
	if format == "both" || format == "markdown" {
		body, err := fetchDailyReportHTTP(baseURL, opts, "markdown")
		if err != nil {
			return err
		}
		reportDate, err := resolveOutputDate(date)
		if err != nil {
			return err
		}
		result.MarkdownPath = filepath.Join(outDir, "daily-"+reportDate+".md")
		if err := os.WriteFile(result.MarkdownPath, body, 0o600); err != nil {
			return err
		}
	}
	if format == "both" || format == "json" {
		body, err := fetchDailyReportHTTP(baseURL, opts, "json")
		if err != nil {
			return err
		}
		var report dailyreport.Report
		if err := json.Unmarshal(body, &report); err != nil {
			return err
		}
		result.JSONPath = filepath.Join(outDir, "daily-"+report.Date+".json")
		if err := os.WriteFile(result.JSONPath, body, 0o600); err != nil {
			return err
		}
		if opts.Vision || opts.Summary {
			path, err := dailyreport.SaveDialogueAnalysis(&report, outDir)
			if err != nil {
				return err
			}
			result.DialogueAnalysisPath = path
		}
		printReportResult(&report, result)
		return nil
	}
	fmt.Printf("generated markdown=%s\n", result.MarkdownPath)
	return nil
}

func fetchDailyReportHTTP(baseURL string, opts dailyreport.ReportOptions, format string) ([]byte, error) {
	u, err := url.Parse(baseURL + "/api/v1/daily/report")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("date", opts.Date)
	q.Set("mention", opts.Mention)
	if len(opts.MentionAliases) > 0 {
		q.Set("aliases", strings.Join(opts.MentionAliases, ","))
	}
	q.Set("before", fmt.Sprint(opts.BeforeCount))
	q.Set("after", fmt.Sprint(opts.AfterCount))
	q.Set("reply_window_minutes", fmt.Sprint(opts.ReplyWindowMinutes))
	if opts.IncludePrivate {
		q.Set("include_private", "1")
	} else {
		q.Set("include_private", "0")
	}
	if opts.Vision {
		q.Set("vision", "1")
		q.Set("max_images", fmt.Sprint(opts.MaxImages))
	}
	if opts.Summary {
		q.Set("summary", "1")
	}
	q.Set("analysis_concurrency", fmt.Sprint(opts.AnalysisConcurrency))
	q.Set("format", format)
	u.RawQuery = q.Encode()

	timeout := 60 * time.Second
	if opts.Vision || opts.Summary {
		timeout = 5 * time.Minute
	}
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func printReportResult(report *dailyreport.Report, result dailyreport.SaveResult) {
	if result.MarkdownPath != "" {
		fmt.Printf("markdown: %s\n", result.MarkdownPath)
	}
	if result.JSONPath != "" {
		fmt.Printf("json: %s\n", result.JSONPath)
	}
	if result.DialogueAnalysisPath != "" {
		fmt.Printf("dialogue_analysis: %s\n", result.DialogueAnalysisPath)
	}
	if report != nil {
		fmt.Printf("summary: mentions=%d replied=%d pending=%d private=%d todos=%d\n",
			report.Overview.GroupMentionCount,
			report.Overview.RepliedCount,
			report.Overview.PendingCount,
			report.Overview.PrivateChatCount,
			report.Overview.ImportantTodoCount,
		)
	}
}

func resolveOutputDate(date string) (string, error) {
	resolved, _, _, err := dailyreport.ResolveDateRange(date, dailyreport.DefaultTimezone, time.Now())
	return resolved, err
}

func (d reportDB) GetSessions(key string, limit, offset int) (*wechatdb.GetSessionsResp, error) {
	return d.db.GetSessions(key, limit, offset)
}

func (d reportDB) GetChatRooms(key string, limit, offset int) (*wechatdb.GetChatRoomsResp, error) {
	return d.db.GetChatRooms(key, limit, offset)
}

func (d reportDB) GetMessages(start, end time.Time, talker string, sender string, keyword string, limit, offset int) ([]*model.Message, error) {
	return d.db.GetMessages(start, end, talker, sender, keyword, limit, offset)
}

func (d reportDB) GetMedia(_type string, key string) (*model.Media, error) {
	return d.db.GetMedia(_type, key)
}

func (c reportVisionClient) AnalyzeImage(ctx context.Context, prompt string, imageData []byte, mimeType string) (string, error) {
	return c.client.AnalyzeImage(ctx, c.cfg, prompt, imageData, mimeType)
}

func (c reportVisionClient) AnalyzeText(ctx context.Context, title, data, instruction string) (string, error) {
	return c.client.Chat(ctx, c.cfg, []semantic.ChatMessage{
		{Role: "system", Content: "你是本地微信聊天记录分析助手。只能基于输入证据输出，不要编造。"},
		{Role: "user", Content: fmt.Sprintf("%s\n\n数据：\n%s\n\n要求：\n%s", title, data, instruction)},
	})
}

func runReportGraph(cmd *cobra.Command, args []string) {
	if err := runReportGraphHTTP(reportGraphDays, reportGraphSummary, reportGraphBaseURL); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runReportGraphHTTP(days int, summary bool, baseURL string) error {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") {
		baseURL = "http://" + baseURL
	}

	target := fmt.Sprintf("%s/api/v1/graph/digest?days=%d&format=json", baseURL, days)
	if summary {
		target += "&summary=true"
	}

	client := &http.Client{Timeout: 120 * time.Second}
	req, err := http.NewRequest(http.MethodPost, target, nil)
	if err != nil {
		return fmt.Errorf("failed to build request to %s: %w", target, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("cannot reach %s: %w\nHint: start the service with: chatlog serve", baseURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 500 {
		return fmt.Errorf("server error %d at %s: %s", resp.StatusCode, target, strings.TrimSpace(string(body)))
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("request error %d at %s: %s", resp.StatusCode, target, strings.TrimSpace(string(body)))
	}

	var result struct {
		Path          string `json:"path"`
		WindowStart   string `json:"window_start"`
		WindowEnd     string `json:"window_end"`
		EntityCount   int    `json:"entity_count"`
		EventCount    int    `json:"event_count"`
		FactCount     int    `json:"fact_count"`
		RelationCount int    `json:"relation_count"`
		SummaryUsed   bool   `json:"summary_used"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Printf("path: %s\n", result.Path)
	fmt.Printf("window_start: %s\n", result.WindowStart)
	fmt.Printf("window_end: %s\n", result.WindowEnd)
	fmt.Printf("entity_count: %d\n", result.EntityCount)
	fmt.Printf("event_count: %d\n", result.EventCount)
	fmt.Printf("fact_count: %d\n", result.FactCount)
	fmt.Printf("relation_count: %d\n", result.RelationCount)
	fmt.Printf("summary_used: %v\n", result.SummaryUsed)
	return nil
}
