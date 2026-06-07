package temporalgraph

// benchmark.go implements the HA-005 model-routing gate: a redacted, fixture-only
// benchmark that compares a candidate chat model (e.g. "m2.1") against the
// configured baseline (e.g. "MiniMax-M2.7") using the same system prompt and the
// same synthetic inputs. It never reads real chat data; the fixture is fully
// synthesized inside this file.
//
// The output of RunBenchmark is the input of the routing gate
// (EvaluateFastPathGate). If the candidate's json_valid_rate is more than
// jsonValidRateGap percentage points below the baseline, or its
// non_empty_graph_rate is more than nonEmptyGraphRateGap percentage points below
// the baseline, the candidate is not allowed to become the default fast path.
//
// Decode-retry behavior (chatDecodeExtraction in manager.go) and bulk/low-risk
// routing decisions in PickChatModel consult EvaluateFastPathGate, so the
// benchmark result is the single source of truth for routing.

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sjzar/chatlog/internal/chatlog/conf"
	"github.com/sjzar/chatlog/internal/chatlog/semantic"
)

// Benchmark fixture is split into categories that mirror the AC list. The
// fixture content is fully synthetic, redacted, and contains no real chat
// content. Names like "T-Party", "ProjectAtlas", and "Customer-G" are
// placeholders; numbers are obviously fake.
const benchmarkSystemPrompt = `你是时间知识图谱抽取器。请只输出 JSON，不要解释。
Schema:
{
  "entities":[{"name":"实体名","type":"person|organization|project|product|customer|group|topic|keyword|event|unknown","aliases":["别名"],"confidence":0.0}],
  "relations":[{"subject":"实体A","predicate":"关系英文或短中文","object":"实体B","time_text":"原文中的时间表达","change_type":"observed|created|updated|ended|conflict","status":"active|ended|conflict","confidence":0.0,"evidence":"原文证据"}],
  "events":[{"event_type":"事件类型","title":"标题","summary":"摘要","time_text":"原文中的时间表达","actors":["参与方"],"targets":["对象"],"confidence":0.0,"evidence":"原文证据"}],
  "facts":[{"statement":"可追溯事实陈述","time_text":"原文中的时间表达","change_type":"observed|created|updated|ended|conflict","status":"active|ended|conflict","confidence":0.0,"evidence":"原文证据"}]
}
要求：实体名优先使用 participants/entity_hints 中的可识别名称；context 是理解前后文的证据，target 或 content 是当前抽取重点；关系和事实必须能从 content/context 中直接得到；如果出现"今天/明天/下周/月底"等时间表达，原样写入 time_text；如果出现"取消/结束/不再/改为/变更/冲突"等表达，设置 change_type/status；不确定时降低 confidence。`

// benchmarkFixtureCategory is the closed list of categories the benchmark
// covers. The categories are intentionally explicit so a future review can
// grep for them and confirm none of the categories contains real chat data.
const (
	benchmarkCatShort         = "short_sentence"
	benchmarkCatEllipsis      = "ellipsis"
	benchmarkCatTime          = "time_expression"
	benchmarkCatMultiParty     = "multi_party"
	benchmarkCatBusiness      = "business_entity"
	benchmarkCatRelation      = "relation_direction"
	benchmarkCatEmpty         = "empty_info"
)

type benchmarkFixture struct {
	Category string
	Source   SourceRecord
}

// benchmarkFixtures returns the canonical, redacted fixture set. The content
// is fully synthetic; do NOT replace it with real chat data.
func benchmarkFixtures() []benchmarkFixture {
	return []benchmarkFixture{
		{
			Category: benchmarkCatShort,
			Source: SourceRecord{
				TalkerName: "T-Party",
				SenderName: "T-Alpha",
				Content:    "明天下午三点开产品评审，重点看 ProjectAtlas 的 V2 计划。",
				Participants: []GraphParticipant{
					{DisplayName: "T-Alpha", UserName: "redacted-1"},
					{DisplayName: "T-Beta", UserName: "redacted-2"},
				},
			},
		},
		{
			Category: benchmarkCatEllipsis,
			Source: SourceRecord{
				TalkerName: "T-Party",
				SenderName: "T-Alpha",
				Content:    "那个事情... 等老大回来再说吧，下周吧。",
				Participants: []GraphParticipant{
					{DisplayName: "T-Alpha", UserName: "redacted-1"},
				},
			},
		},
		{
			Category: benchmarkCatTime,
			Source: SourceRecord{
				TalkerName: "T-Party",
				SenderName: "T-Alpha",
				Content:    "上周五 18:30 把合同发给了 Customer-G 的张总，截止 9/30。",
				Participants: []GraphParticipant{
					{DisplayName: "T-Alpha", UserName: "redacted-1"},
				},
			},
		},
		{
			Category: benchmarkCatMultiParty,
			Source: SourceRecord{
				TalkerName: "T-Party",
				SenderName: "T-Alpha",
				Content:    "A: 我先过去；B: 我也一起；C: 那 6 点在 Customer-G 楼下见。",
				Participants: []GraphParticipant{
					{DisplayName: "A", UserName: "redacted-3"},
					{DisplayName: "B", UserName: "redacted-4"},
					{DisplayName: "C", UserName: "redacted-5"},
				},
			},
		},
		{
			Category: benchmarkCatBusiness,
			Source: SourceRecord{
				TalkerName: "T-Party",
				SenderName: "T-Alpha",
				Content:    "ProjectAtlas 的 V2 计划：9 月底交付 v2.1，10 月推 Customer-G 试点。",
				Participants: []GraphParticipant{
					{DisplayName: "T-Alpha", UserName: "redacted-1"},
				},
				EntityHints: []string{"ProjectAtlas", "Customer-G", "V2"},
			},
		},
		{
			Category: benchmarkCatRelation,
			Source: SourceRecord{
				TalkerName: "T-Party",
				SenderName: "T-Alpha",
				Content:    "A 直接汇报给 B，B 协助 C 处理 Customer-G 投诉。",
				Participants: []GraphParticipant{
					{DisplayName: "A", UserName: "redacted-3"},
					{DisplayName: "B", UserName: "redacted-4"},
					{DisplayName: "C", UserName: "redacted-5"},
				},
			},
		},
		{
			Category: benchmarkCatEmpty,
			Source: SourceRecord{
				TalkerName: "T-Party",
				SenderName: "T-Alpha",
				Content:    "ok",
				Participants: []GraphParticipant{
					{DisplayName: "T-Alpha", UserName: "redacted-1"},
				},
			},
		},
	}
}

// BenchmarkMetrics is the public result type. All rates are in [0, 1]. Counts
// are deltas between the candidate model and the baseline (positive means
// candidate produced more). avg_latency_ms is the average wall time per call
// to candidate's Chat. retry_rate is the fraction of calls that needed a
// decode retry (i.e. the first response was not valid extraction JSON).
type BenchmarkMetrics struct {
	Model              string             `json:"model"`
	SampleCount        int                `json:"sample_count"`
	JSONValidRate      float64            `json:"json_valid_rate"`
	NonEmptyGraphRate  float64            `json:"non_empty_graph_rate"`
	EntityCountDelta   int                `json:"entity_count_delta"`
	FactCountDelta     int                `json:"fact_count_delta"`
	EventCountDelta    int                `json:"event_count_delta"`
	RelationCountDelta int                `json:"relation_count_delta"`
	DecodeRetryRate    float64            `json:"decode_retry_rate"`
	AvgLatencyMS       float64            `json:"avg_latency_ms"`
	CategoryBreakdown  map[string]int     `json:"category_breakdown,omitempty"`
}

// ChatInvoker is the minimal surface RunBenchmark needs from a chat client.
// It exists so tests can inject deterministic responses without hitting the
// network. Production code passes a *semantic.Client wrapper.
type ChatInvoker interface {
	Chat(ctx context.Context, cfg conf.SemanticConfig, messages []semantic.ChatMessage) (string, error)
}

// ChatInvokerFunc adapts a plain function to ChatInvoker.
type ChatInvokerFunc func(ctx context.Context, cfg conf.SemanticConfig, messages []semantic.ChatMessage) (string, error)

func (f ChatInvokerFunc) Chat(ctx context.Context, cfg conf.SemanticConfig, messages []semantic.ChatMessage) (string, error) {
	return f(ctx, cfg, messages)
}

// NewSemanticClientInvoker returns a ChatInvoker backed by a real
// *semantic.Client. The client is required; a nil client is rejected.
func NewSemanticClientInvoker(client *semantic.Client) ChatInvoker {
	if client == nil {
		panic("temporalgraph: NewSemanticClientInvoker requires a non-nil client")
	}
	return ChatInvokerFunc(client.Chat)
}

// BenchmarkRunner executes the fixture against a ChatInvoker. It is
// intentionally offline-friendly: callers MUST pass an invoker (real client
// in production, deterministic function in tests).
type BenchmarkRunner struct {
	Invoker ChatInvoker
}

// NewBenchmarkRunner returns a runner bound to the given ChatInvoker.
func NewBenchmarkRunner(invoker ChatInvoker) *BenchmarkRunner {
	if invoker == nil {
		panic("temporalgraph: NewBenchmarkRunner requires a non-nil invoker")
	}
	return &BenchmarkRunner{Invoker: invoker}
}

// RunBenchmark executes the synthetic fixture for a single model. The model
// string is injected as cfg.ChatModel for every call so the same client path
// is exercised end-to-end (MiniMax vs OpenAI-compatible vs other providers all
// pick up cfg.ChatModel from chatMMXRaw/chatOpenAICompatible).
//
// The function returns BenchmarkMetrics with all required fields. If the chat
// call returns an error, that sample is counted as json_invalid; the error is
// not propagated so the runner can produce a stable result even when the
// upstream is partially unhealthy.
func (r *BenchmarkRunner) RunBenchmark(
	ctx context.Context,
	cfg conf.SemanticConfig,
	modelName string,
) BenchmarkMetrics {
	if r == nil || r.Invoker == nil {
		return BenchmarkMetrics{Model: modelName}
	}
	fixtures := benchmarkFixtures()
	metrics := BenchmarkMetrics{
		Model:             modelName,
		SampleCount:       len(fixtures),
		CategoryBreakdown: map[string]int{},
	}
	if len(fixtures) == 0 {
		return metrics
	}
	overridden := cfg
	overridden.ChatModel = modelName
	overridden = conf.NormalizeSemanticConfig(overridden)
	if overridden.ChatModel == "" {
		// If the override is rejected by normalization, do not call the
		// upstream; report zero rates and return.
		return metrics
	}

	totalLatencyMS := 0.0
	for _, fx := range fixtures {
		messages := []semantic.ChatMessage{
			{Role: "system", Content: benchmarkSystemPrompt},
			{Role: "user", Content: toJSONString(sourcePromptPayload(fx.Source))},
		}
		start := time.Now()
		raw, err := r.Invoker.Chat(ctx, overridden, messages)
		latency := time.Since(start)
		totalLatencyMS += float64(latency.Microseconds()) / 1000.0

		jsonValid := false
		nonEmpty := false
		if err == nil {
			ext, derr := decodeExtraction(raw)
			if derr == nil {
				jsonValid = true
				if extractionHasContent(ext) {
					nonEmpty = true
				}
			} else {
				// First response is invalid; try one decode retry the same way
				// production does. If the retry succeeds, jsonValid=true and we
				// count this as a decode retry.
				retryMsgs := append([]semantic.ChatMessage{},
					messages...,
				)
				retryMsgs = append(retryMsgs,
					semantic.ChatMessage{Role: "assistant", Content: truncateRunes(raw, 1200)},
					semantic.ChatMessage{Role: "user", Content: "上一次输出不是合法 JSON，无法解析。请不要解释，不要输出 Markdown 代码块，只重新输出一个完整、严格合法的 JSON 对象，字段必须是 entities、relations、events、facts。"},
				)
				raw2, err2 := r.Invoker.Chat(ctx, overridden, retryMsgs)
				if err2 == nil {
					ext2, derr2 := decodeExtraction(raw2)
					if derr2 == nil {
						jsonValid = true
						if extractionHasContent(ext2) {
							nonEmpty = true
						}
					}
				}
				metrics.DecodeRetryRate += 1
			}
		}
		if jsonValid {
			metrics.JSONValidRate += 1
		}
		if nonEmpty {
			metrics.NonEmptyGraphRate += 1
		}
		metrics.CategoryBreakdown[fx.Category]++
	}
	n := float64(len(fixtures))
	metrics.JSONValidRate /= n
	metrics.NonEmptyGraphRate /= n
	metrics.DecodeRetryRate /= n
	metrics.AvgLatencyMS = totalLatencyMS / n
	return metrics
}

// RunBenchmarkComparison runs the fixture for baseline and candidate, then
// returns the absolute deltas required by AC#2. Counts use the difference
// between candidate totals and baseline totals.
func (r *BenchmarkRunner) RunBenchmarkComparison(
	ctx context.Context,
	cfg conf.SemanticConfig,
	baselineModel string,
	candidateModel string,
) (BenchmarkMetrics, BenchmarkMetrics, BenchmarkComparison) {
	baseline := r.RunBenchmark(ctx, cfg, baselineModel)
	candidate := r.RunBenchmark(ctx, cfg, candidateModel)

	baselineCounts := r.countExtractionTotals(ctx, cfg, baselineModel)
	candidateCounts := r.countExtractionTotals(ctx, cfg, candidateModel)

	cmp := BenchmarkComparison{
		BaselineModel:      baselineModel,
		CandidateModel:     candidateModel,
		JSONValidRateDelta: candidate.JSONValidRate - baseline.JSONValidRate,
		NonEmptyGraphDelta: candidate.NonEmptyGraphRate - baseline.NonEmptyGraphRate,
		EntityCountDelta:   candidateCounts.Entities - baselineCounts.Entities,
		FactCountDelta:     candidateCounts.Facts - baselineCounts.Facts,
		EventCountDelta:    candidateCounts.Events - baselineCounts.Events,
		RelationCountDelta: candidateCounts.Relations - baselineCounts.Relations,
	}
	baseline.EntityCountDelta = 0
	baseline.FactCountDelta = 0
	baseline.EventCountDelta = 0
	baseline.RelationCountDelta = 0
	candidate.EntityCountDelta = cmp.EntityCountDelta
	candidate.FactCountDelta = cmp.FactCountDelta
	candidate.EventCountDelta = cmp.EventCountDelta
	candidate.RelationCountDelta = cmp.RelationCountDelta
	return baseline, candidate, cmp
}

// BenchmarkComparison captures the deltas required by AC#2 and AC#3. Deltas
// are reported in raw units (counts) or in fraction units (rates), as noted
// in the field name.
type BenchmarkComparison struct {
	BaselineModel      string  `json:"baseline_model"`
	CandidateModel     string  `json:"candidate_model"`
	JSONValidRateDelta float64 `json:"json_valid_rate_delta"`
	NonEmptyGraphDelta float64 `json:"non_empty_graph_rate_delta"`
	EntityCountDelta   int     `json:"entity_count_delta"`
	FactCountDelta     int     `json:"fact_count_delta"`
	EventCountDelta    int     `json:"event_count_delta"`
	RelationCountDelta int     `json:"relation_count_delta"`
}

type extractionTotals struct {
	Entities  int
	Facts     int
	Events    int
	Relations int
}

func (r *BenchmarkRunner) countExtractionTotals(
	ctx context.Context,
	cfg conf.SemanticConfig,
	modelName string,
) extractionTotals {
	if r == nil || r.Invoker == nil {
		return extractionTotals{}
	}
	overridden := cfg
	overridden.ChatModel = modelName
	overridden = conf.NormalizeSemanticConfig(overridden)
	if overridden.ChatModel == "" {
		return extractionTotals{}
	}
	totals := extractionTotals{}
	for _, fx := range benchmarkFixtures() {
		messages := []semantic.ChatMessage{
			{Role: "system", Content: benchmarkSystemPrompt},
			{Role: "user", Content: toJSONString(sourcePromptPayload(fx.Source))},
		}
		raw, err := r.Invoker.Chat(ctx, overridden, messages)
		if err != nil {
			continue
		}
		ext, derr := decodeExtraction(raw)
		if derr != nil {
			retryMsgs := append([]semantic.ChatMessage{}, messages...)
			retryMsgs = append(retryMsgs,
				semantic.ChatMessage{Role: "assistant", Content: truncateRunes(raw, 1200)},
				semantic.ChatMessage{Role: "user", Content: "上一次输出不是合法 JSON，无法解析。请不要解释，不要输出 Markdown 代码块，只重新输出一个完整、严格合法的 JSON 对象，字段必须是 entities、relations、events、facts。"},
			)
			raw2, err2 := r.Invoker.Chat(ctx, overridden, retryMsgs)
			if err2 != nil {
				continue
			}
			ext, derr = decodeExtraction(raw2)
			if derr != nil {
				continue
			}
		}
		totals.Entities += len(ext.Entities)
		totals.Facts += len(ext.Facts)
		totals.Events += len(ext.Events)
		totals.Relations += len(ext.Relations)
	}
	return totals
}

// Routing gate thresholds. A negative JSONValidRateDelta of more than
// JSONValidRateGap means the candidate is materially worse on JSON validity
// and must not be the default fast path. Same for NonEmptyGraphGap on
// non_empty_graph_rate.
const (
	JSONValidRateGap     = 0.02
	NonEmptyGraphRateGap = 0.05
)

// FastPathDecision is the public decision the gate emits.
type FastPathDecision struct {
	AllowFastPath     bool    `json:"allow_fast_path"`
	JSONValidRateGap  float64 `json:"json_valid_rate_gap"`
	NonEmptyGraphGap  float64 `json:"non_empty_graph_gap"`
	JSONValidPassed   bool    `json:"json_valid_passed"`
	NonEmptyPassed    bool    `json:"non_empty_passed"`
	Reason            string  `json:"reason"`
}

// EvaluateFastPathGate decides whether a candidate model is allowed to become
// the default fast path. The baseline is the configured "good" model (e.g.
// "MiniMax-M2.7"); the candidate is the model the operator wants to use for
// low-risk bulk extraction (e.g. "m2.1").
//
// Per AC#3: the candidate is rejected if its json_valid_rate is more than 2pp
// below the baseline OR its non_empty_graph_rate is more than 5pp below the
// baseline. Both checks must pass.
func EvaluateFastPathGate(cmp BenchmarkComparison) FastPathDecision {
	jsonValidPassed := cmp.JSONValidRateDelta >= -JSONValidRateGap
	nonEmptyPassed := cmp.NonEmptyGraphDelta >= -NonEmptyGraphRateGap
	allow := jsonValidPassed && nonEmptyPassed
	reason := "candidate meets benchmark gate"
	if !allow {
		reason = buildGateReason(cmp, jsonValidPassed, nonEmptyPassed)
	}
	return FastPathDecision{
		AllowFastPath:    allow,
		JSONValidRateGap: JSONValidRateGap,
		NonEmptyGraphGap: NonEmptyGraphRateGap,
		JSONValidPassed:  jsonValidPassed,
		NonEmptyPassed:   nonEmptyPassed,
		Reason:           reason,
	}
}

func buildGateReason(cmp BenchmarkComparison, jsonValidPassed, nonEmptyPassed bool) string {
	reasons := []string{}
	if !jsonValidPassed {
		reasons = append(reasons, fmt.Sprintf(
			"json_valid_rate delta %.4f < -%.4f",
			cmp.JSONValidRateDelta, JSONValidRateGap,
		))
	}
	if !nonEmptyPassed {
		reasons = append(reasons, fmt.Sprintf(
			"non_empty_graph_rate delta %.4f < -%.4f",
			cmp.NonEmptyGraphDelta, NonEmptyGraphRateGap,
		))
	}
	sort.Strings(reasons)
	return "candidate fails benchmark gate: " + strings.Join(reasons, "; ")
}

// BulkRoutingConfig controls the routing decision in PickChatModel. The
// baseline model is always used for "key sources" (key_source=true), JSON
// decode errors, and low-quality output. The fast-path candidate is used
// only when the gate allows it.
type BulkRoutingConfig struct {
	BaselineModel  string
	FastPathModel  string
	AllowFastPath  bool
}

// PickChatModel returns the model to use for a given source. Per AC#4, the
// baseline (e.g. "MiniMax-M2.7") is used for:
//   - Key sources (KeySource=true) — sources that have already been flagged as
//     "important enough to upgrade".
//   - Recent JSON decode errors from this source — determined by the caller
//     passing decodeError=true when the last attempt was a decode error.
//   - Any source where the gate does not allow the fast path.
func PickChatModel(cfg BulkRoutingConfig, keySource bool, decodeError bool) string {
	if cfg.BaselineModel == "" {
		return ""
	}
	if keySource || decodeError {
		return cfg.BaselineModel
	}
	if cfg.AllowFastPath && cfg.FastPathModel != "" {
		return cfg.FastPathModel
	}
	return cfg.BaselineModel
}

// PersistedFastPathRecord is the shape of the small JSON file we keep on disk
// so an operator can audit the last benchmark gate result without re-running
// the benchmark. The file lives at <data-dir>/temporal_graph_fast_path.json
// and never contains real chat data, real keys, or model prompts.
type PersistedFastPathRecord struct {
	BaselineModel   string            `json:"baseline_model"`
	CandidateModel  string            `json:"candidate_model"`
	UpdatedAt       time.Time         `json:"updated_at"`
	Decision        FastPathDecision  `json:"decision"`
	BaselineMetrics BenchmarkMetrics  `json:"baseline_metrics"`
	CandidateMetrics BenchmarkMetrics `json:"candidate_metrics"`
}
