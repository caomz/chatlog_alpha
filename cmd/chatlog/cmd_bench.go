package chatlog

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"

	"github.com/sjzar/chatlog/internal/chatlog/conf"
	"github.com/sjzar/chatlog/internal/chatlog/semantic"
	"github.com/sjzar/chatlog/internal/chatlog/temporalgraph"
)

// bench command flags. The default mode is dry-run + fixture-only (AC#5):
// the runner only consumes the synthetic fixture defined in
// internal/chatlog/temporalgraph/benchmark.go and never reads real chat data.
// Real upstream calls require --execute.
var (
	benchProvider     string
	benchBaseline     string
	benchCandidate    string
	benchAPIKey       string
	benchBaseURL      string
	benchFastPath     string
	benchOutput       string
	benchExecute      bool
	benchTimeoutSec   int
	benchPersistPath  string
)

func init() {
	rootCmd.AddCommand(benchCmd)
	benchCmd.Flags().StringVar(&benchProvider, "provider", conf.ProviderMMX, "semantic chat provider used for the bench calls (mmx/ollama/deepseek/...)")
	benchCmd.Flags().StringVar(&benchBaseline, "baseline", conf.LegacyMMXChatM27, "baseline model (e.g. MiniMax-M2.7)")
	benchCmd.Flags().StringVar(&benchCandidate, "candidate", "m2.1", "candidate fast-path model (e.g. m2.1)")
	benchCmd.Flags().StringVar(&benchAPIKey, "api-key", "", "api key override (used only for non-mmx providers; mmx uses env keys)")
	benchCmd.Flags().StringVar(&benchBaseURL, "base-url", "", "base URL override (non-mmx providers)")
	benchCmd.Flags().StringVar(&benchFastPath, "fast-path", "", "fast-path model to record for routing (defaults to --candidate)")
	benchCmd.Flags().StringVar(&benchOutput, "output", "summary", "output format: summary|json")
	benchCmd.Flags().BoolVar(&benchExecute, "execute", false, "call the upstream model APIs; default is dry-run (fixture only)")
	benchCmd.Flags().IntVar(&benchTimeoutSec, "timeout-sec", 600, "overall timeout in seconds")
	benchCmd.Flags().StringVar(&benchPersistPath, "persist", "", "if set, write the persisted fast-path record to this JSON file")
}

var benchCmd = &cobra.Command{
	Use:   "bench",
	Short: "Run the model benchmark gate (HA-005)",
	Long: `Run the redacted, fixture-only benchmark that gates whether a candidate
fast-path model (e.g. "m2.1") is allowed to be used for low-risk bulk
extraction. The baseline is "MiniMax-M2.7" by default.

By default the command is DRY-RUN / fixture-only: it does NOT call the
upstream provider. Use --execute to actually invoke the chat models.

The benchmark fixture is fully synthetic (see temporalgraph benchmark.go)
and never contains real chat content.`,
	Run: runBench,
}

func runBench(cmd *cobra.Command, args []string) {
	if benchOutput != "summary" && benchOutput != "json" {
		fmt.Fprintf(os.Stderr, "unsupported --output %q (want summary|json)\n", benchOutput)
		os.Exit(2)
	}
	if strings.TrimSpace(benchBaseline) == "" || strings.TrimSpace(benchCandidate) == "" {
		fmt.Fprintln(os.Stderr, "baseline and candidate must not be empty")
		os.Exit(2)
	}
	if strings.EqualFold(benchBaseline, benchCandidate) {
		fmt.Fprintln(os.Stderr, "baseline and candidate must differ")
		os.Exit(2)
	}
	fastPath := benchFastPath
	if fastPath == "" {
		fastPath = benchCandidate
	}

	cfg := conf.SemanticConfig{
		Enabled:        true,
		APIKey:         benchAPIKey,
		BaseURL:        benchBaseURL,
		ChatProvider:   benchProvider,
		ChatModel:      benchBaseline,
		ChatMaxTokens:  4096,
		ChatTemperature: 0.2,
	}
	cfg = conf.NormalizeSemanticConfig(cfg)

	if !benchExecute {
		// Dry-run: never call upstream. Print a summary that proves the gate
		// would reject/allow the candidate without sending anything to the
		// network. This is the default per AC#5.
		decision := temporalgraph.EvaluateFastPathGate(temporalgraph.BenchmarkComparison{
			BaselineModel:      benchBaseline,
			CandidateModel:     benchCandidate,
			JSONValidRateDelta: 0,
			NonEmptyGraphDelta: 0,
		})
		renderDryRun(benchBaseline, benchCandidate, fastPath, decision, benchOutput)
		return
	}

	if !conf.SemanticChatReady(cfg) {
		fmt.Fprintln(os.Stderr, "semantic chat is not ready for provider", benchProvider, "; provide --api-key and --base-url (or env keys for mmx)")
		os.Exit(2)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(benchTimeoutSec)*time.Second)
	defer cancel()

	client := semantic.NewClient()
	runner := temporalgraph.NewBenchmarkRunner(temporalgraph.NewSemanticClientInvoker(client))
	baseline, candidate, cmp := runner.RunBenchmarkComparison(ctx, cfg, benchBaseline, benchCandidate)
	decision := temporalgraph.EvaluateFastPathGate(cmp)

	if benchOutput == "json" {
		out := map[string]any{
			"baseline":    baseline,
			"candidate":   candidate,
			"comparison":  cmp,
			"decision":    decision,
			"fast_path":   fastPath,
			"ran_at":      time.Now().UTC().Format(time.RFC3339),
			"execute":     true,
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(out); err != nil {
			log.Err(err).Msg("bench json encode failed")
		}
	} else {
		renderSummary(benchBaseline, benchCandidate, baseline, candidate, cmp, decision)
	}

	if benchPersistPath != "" {
		rec := temporalgraph.PersistedFastPathRecord{
			BaselineModel:    benchBaseline,
			CandidateModel:   benchCandidate,
			UpdatedAt:        time.Now().UTC(),
			Decision:         decision,
			BaselineMetrics:  baseline,
			CandidateMetrics: candidate,
		}
		f, err := os.Create(benchPersistPath)
		if err != nil {
			log.Err(err).Msg("bench persist create failed")
			return
		}
		defer f.Close()
		enc := json.NewEncoder(f)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rec); err != nil {
			log.Err(err).Msg("bench persist encode failed")
		}
	}

	if !decision.AllowFastPath {
		// Non-zero exit so the operator notices the gate failure, but only in
		// execute mode (dry-run is informational).
		os.Exit(3)
	}
}

func renderDryRun(baseline, candidate, fastPath string, decision temporalgraph.FastPathDecision, format string) {
	switch format {
	case "json":
		out := map[string]any{
			"mode":         "dry-run",
			"baseline":     baseline,
			"candidate":    candidate,
			"fast_path":    fastPath,
			"decision":     decision,
			"privacy_note": "dry-run never calls the upstream; benchmark fixture is fully synthetic",
		}
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(out)
	default:
		fmt.Println("mode: dry-run (no upstream calls)")
		fmt.Println("baseline:", baseline)
		fmt.Println("candidate:", candidate)
		fmt.Println("fast_path:", fastPath)
		fmt.Println("gate:")
		fmt.Printf("  json_valid_rate gap threshold = %.2f\n", decision.JSONValidRateGap)
		fmt.Printf("  non_empty_graph_rate gap threshold = %.2f\n", decision.NonEmptyGraphGap)
		fmt.Println("result: candidate meets gate (no live measurement; pass/fail pending --execute)")
		fmt.Println("privacy: fixture-only; no real chat content; no API keys printed")
	}
}

func renderSummary(baseline, candidate string, baseM, candM temporalgraph.BenchmarkMetrics, cmp temporalgraph.BenchmarkComparison, decision temporalgraph.FastPathDecision) {
	fmt.Println("baseline:", baseline)
	fmt.Printf("  sample_count=%d  json_valid_rate=%.4f  non_empty_graph_rate=%.4f  decode_retry_rate=%.4f  avg_latency_ms=%.2f\n",
		baseM.SampleCount, baseM.JSONValidRate, baseM.NonEmptyGraphRate, baseM.DecodeRetryRate, baseM.AvgLatencyMS)
	fmt.Println("candidate:", candidate)
	fmt.Printf("  sample_count=%d  json_valid_rate=%.4f  non_empty_graph_rate=%.4f  decode_retry_rate=%.4f  avg_latency_ms=%.2f\n",
		candM.SampleCount, candM.JSONValidRate, candM.NonEmptyGraphRate, candM.DecodeRetryRate, candM.AvgLatencyMS)
	fmt.Println("comparison:")
	fmt.Printf("  json_valid_rate_delta=%+.4f  non_empty_graph_rate_delta=%+.4f\n", cmp.JSONValidRateDelta, cmp.NonEmptyGraphDelta)
	fmt.Printf("  entity_count_delta=%+d  fact_count_delta=%+d  event_count_delta=%+d  relation_count_delta=%+d\n",
		cmp.EntityCountDelta, cmp.FactCountDelta, cmp.EventCountDelta, cmp.RelationCountDelta)
	fmt.Println("gate:")
	fmt.Printf("  allow_fast_path=%t  reason=%s\n", decision.AllowFastPath, decision.Reason)
}
