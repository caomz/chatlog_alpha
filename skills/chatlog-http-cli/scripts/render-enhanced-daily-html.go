package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/sjzar/chatlog/pkg/util/dat2img"
)

const defaultHTMLDir = "/Volumes/WorkSSD/Dev/openclaw_mz/knowledge/raw/微信每日聊天记录"

type localConfig struct {
	DataDir string `json:"data_dir"`
	ImgKey  string `json:"img_key"`
}

type report struct {
	Date           string          `json:"date"`
	GeneratedAt    string          `json:"generated_at"`
	Overview       map[string]any  `json:"overview"`
	Mentions       []mention       `json:"mentions"`
	GroupAnalyses  []groupAnalysis `json:"group_analyses"`
	PrivateUpdates []privateUpdate `json:"private_updates"`
	Todos          []todo          `json:"todos"`
	Evidence       []evidence      `json:"evidence"`
	AnalysisError  string          `json:"analysis_error"`
	Options        map[string]any  `json:"options"`
}

type mediaRef struct {
	Type string `json:"type"`
	Key  string `json:"key"`
	Path string `json:"path"`
}

type message struct {
	ChatID             string     `json:"chat_id"`
	ChatName           string     `json:"chat_name"`
	Sender             string     `json:"sender"`
	SenderName         string     `json:"sender_name"`
	Time               string     `json:"time"`
	Timestamp          int64      `json:"timestamp"`
	Seq                int64      `json:"seq"`
	MsgID              int64      `json:"msg_id"`
	Content            string     `json:"content"`
	IsSelf             bool       `json:"is_self"`
	Position           string     `json:"position"`
	MediaRefs          []mediaRef `json:"media_refs"`
	ImageAnalysis      string     `json:"image_analysis"`
	ImageAnalysisError string     `json:"image_analysis_error"`
}

type mention struct {
	EvidenceID string          `json:"evidence_id"`
	ChatID     string          `json:"chat_id"`
	ChatName   string          `json:"chat_name"`
	Sender     string          `json:"sender"`
	SenderName string          `json:"sender_name"`
	Time       string          `json:"time"`
	Timestamp  int64           `json:"timestamp"`
	Seq        int64           `json:"seq"`
	MsgID      int64           `json:"msg_id"`
	Content    string          `json:"content"`
	Status     string          `json:"status"`
	Before     []message       `json:"before"`
	After      []message       `json:"after"`
	Analysis   mentionAnalysis `json:"analysis"`
	MediaRefs  []mediaRef      `json:"media_refs"`
}

type mentionAnalysis struct {
	Topic           string   `json:"topic"`
	Context         string   `json:"context"`
	SuggestedAction string   `json:"suggested_action"`
	NeedsReply      bool     `json:"needs_reply"`
	EvidenceIDs     []string `json:"evidence_ids"`
}

type groupAnalysis struct {
	ChatID      string   `json:"chat_id"`
	ChatName    string   `json:"chat_name"`
	Summary     string   `json:"summary"`
	Risks       []string `json:"risks"`
	Todos       []string `json:"todos"`
	EvidenceIDs []string `json:"evidence_ids"`
}

type privateUpdate struct {
	ChatID           string  `json:"chat_id"`
	ChatName         string  `json:"chat_name"`
	TotalMessages    int     `json:"total_messages"`
	IncomingMessages int     `json:"incoming_messages"`
	SelfMessages     int     `json:"self_messages"`
	NeedsReply       bool    `json:"needs_reply"`
	LatestMessage    message `json:"latest_message"`
	Summary          string  `json:"summary"`
}

type todo struct {
	ChatID     string `json:"chat_id"`
	ChatName   string `json:"chat_name"`
	EvidenceID string `json:"evidence_id"`
	Text       string `json:"text"`
}

type evidence struct {
	ID         string `json:"id"`
	Type       string `json:"type"`
	ChatID     string `json:"chat_id"`
	ChatName   string `json:"chat_name"`
	SenderName string `json:"sender_name"`
	Time       string `json:"time"`
	Seq        int64  `json:"seq"`
	Content    string `json:"content"`
}

type imageAsset struct {
	ID       string
	DataURL  string
	MimeType string
	Path     string
	Error    string
}

type imageNode struct {
	ID       string
	AssetID  string
	Section  string
	ChatName string
	Sender   string
	Time     string
	Content  string
	Analysis string
	Error    string
	Ref      mediaRef
}

func main() {
	jsonPath := flag.String("json", "", "daily report json path; empty uses the latest reports/daily-YYYY-MM-DD.json")
	markdownPath := flag.String("markdown", "", "daily report markdown path; empty uses the same date as --json under reports/")
	outPath := flag.String("out", "", "output html path; empty writes daily-YYYY-MM-DD-enhanced.html under --out-dir")
	markdownOutPath := flag.String("markdown-out", "", "output markdown path; empty writes daily-YYYY-MM-DD.md under --out-dir")
	outDir := flag.String("out-dir", defaultHTMLDir, "directory for generated html when --out is empty")
	configPath := flag.String("config", "", "chatlog.json path with data_dir and img_key")
	dataDir := flag.String("data-dir", "", "WeChat data dir")
	imgKey := flag.String("img-key", "", "image decryption key")
	flag.Parse()

	var cfg localConfig
	if strings.TrimSpace(*configPath) != "" {
		b, err := os.ReadFile(*configPath)
		must(err)
		must(json.Unmarshal(b, &cfg))
	}
	if strings.TrimSpace(*dataDir) != "" {
		cfg.DataDir = *dataDir
	}
	if strings.TrimSpace(*imgKey) != "" {
		cfg.ImgKey = *imgKey
	}
	if strings.TrimSpace(cfg.DataDir) == "" {
		fail("missing data_dir")
	}

	resolvedJSONPath := strings.TrimSpace(*jsonPath)
	if resolvedJSONPath == "" {
		var err error
		resolvedJSONPath, err = latestDailyReportJSON("reports")
		must(err)
	}

	var r report
	b, err := os.ReadFile(resolvedJSONPath)
	must(err)
	must(json.Unmarshal(b, &r))

	resolvedOutPath := strings.TrimSpace(*outPath)
	if resolvedOutPath == "" {
		if strings.TrimSpace(*outDir) == "" {
			fail("missing out-dir")
		}
		resolvedOutPath = filepath.Join(*outDir, fmt.Sprintf("daily-%s-enhanced.html", r.Date))
	}
	resolvedMarkdownPath := strings.TrimSpace(*markdownPath)
	if resolvedMarkdownPath == "" {
		resolvedMarkdownPath = filepath.Join("reports", fmt.Sprintf("daily-%s.md", r.Date))
	}
	resolvedMarkdownOutPath := strings.TrimSpace(*markdownOutPath)
	if resolvedMarkdownOutPath == "" {
		if strings.TrimSpace(*outDir) == "" {
			fail("missing out-dir")
		}
		resolvedMarkdownOutPath = filepath.Join(*outDir, fmt.Sprintf("daily-%s.md", r.Date))
	}
	must(os.MkdirAll(filepath.Dir(resolvedOutPath), 0o755))
	must(os.MkdirAll(filepath.Dir(resolvedMarkdownOutPath), 0o755))

	nodes := collectImageNodes(r)
	assets := resolveAssets(cfg, nodes)
	doc := renderHTML(r, nodes, assets)
	must(os.WriteFile(resolvedOutPath, []byte(doc), 0644))
	must(copyFile(resolvedMarkdownPath, resolvedMarkdownOutPath))
	resolved, failed := 0, 0
	for _, a := range assets {
		if a.DataURL != "" {
			resolved++
		} else {
			failed++
		}
	}
	fmt.Printf("json: %s\nmarkdown: %s\nhtml: %s\nimage_nodes: %d\nunique_assets: %d resolved=%d failed=%d\n", resolvedJSONPath, resolvedMarkdownOutPath, resolvedOutPath, len(nodes), len(assets), resolved, failed)
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return fmt.Errorf("read markdown source %s: %w", src, err)
	}
	if err := os.WriteFile(dst, data, 0o644); err != nil {
		return fmt.Errorf("write markdown output %s: %w", dst, err)
	}
	return nil
}

func latestDailyReportJSON(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", err
	}
	pattern := regexp.MustCompile(`^daily-\d{4}-\d{2}-\d{2}\.json$`)
	var candidates []string
	for _, entry := range entries {
		if entry.IsDir() || !pattern.MatchString(entry.Name()) {
			continue
		}
		candidates = append(candidates, filepath.Join(dir, entry.Name()))
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no daily report json found in %s", dir)
	}
	sort.Strings(candidates)
	return candidates[len(candidates)-1], nil
}

func collectImageNodes(r report) []imageNode {
	var nodes []imageNode
	add := func(section, chatName string, m message) {
		if len(m.MediaRefs) == 0 && strings.TrimSpace(m.ImageAnalysis) == "" && strings.TrimSpace(m.ImageAnalysisError) == "" {
			return
		}
		ref := mediaRef{}
		if len(m.MediaRefs) > 0 {
			ref = m.MediaRefs[0]
		}
		id := fmt.Sprintf("img-%03d", len(nodes)+1)
		nodes = append(nodes, imageNode{
			ID:       id,
			AssetID:  assetID(ref),
			Section:  section,
			ChatName: firstNonEmpty(m.ChatName, chatName),
			Sender:   m.SenderName,
			Time:     m.Time,
			Content:  m.Content,
			Analysis: m.ImageAnalysis,
			Error:    m.ImageAnalysisError,
			Ref:      ref,
		})
	}
	for _, item := range r.Mentions {
		selfMsg := message{
			ChatName: item.ChatName, SenderName: item.SenderName, Time: item.Time, Content: item.Content, MediaRefs: item.MediaRefs,
		}
		add("@"+item.EvidenceID, item.ChatName, selfMsg)
		for _, m := range item.Before {
			add("前文 "+item.EvidenceID, item.ChatName, m)
		}
		for _, m := range item.After {
			add("后续 "+item.EvidenceID, item.ChatName, m)
		}
	}
	for _, u := range r.PrivateUpdates {
		add("私聊最新", u.ChatName, u.LatestMessage)
	}
	return nodes
}

func resolveAssets(cfg localConfig, nodes []imageNode) map[string]imageAsset {
	if strings.TrimSpace(cfg.ImgKey) != "" {
		dat2img.SetAesKey(cfg.ImgKey)
	}
	if strings.TrimSpace(cfg.DataDir) != "" {
		_, _ = dat2img.ScanAndSetXorKey(cfg.DataDir)
	}
	assets := map[string]imageAsset{}
	for _, n := range nodes {
		if n.AssetID == "" {
			continue
		}
		if _, ok := assets[n.AssetID]; ok {
			continue
		}
		a := imageAsset{ID: n.AssetID}
		data, mt, path, err := resolveOne(cfg.DataDir, n.Ref)
		if err != nil {
			a.Error = err.Error()
		} else {
			a.MimeType = mt
			a.Path = path
			a.DataURL = "data:" + mt + ";base64," + base64.StdEncoding.EncodeToString(data)
		}
		assets[n.AssetID] = a
	}
	return assets
}

func resolveOne(dataDir string, ref mediaRef) ([]byte, string, string, error) {
	for _, p := range candidatePaths(dataDir, ref) {
		data, mt, err := readImageFile(p)
		if err == nil {
			return data, mt, p, nil
		}
	}
	return nil, "", "", fmt.Errorf("image file not found")
}

func candidatePaths(dataDir string, ref mediaRef) []string {
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(dataDir, p)
		}
		out = append(out, imagePathCandidates(p)...)
	}
	add(ref.Path)
	if strings.Contains(ref.Key, "/") {
		add(ref.Key)
	}
	return unique(out)
}

func imagePathCandidates(path string) []string {
	out := []string{path}
	if filepath.Ext(path) == "" {
		for _, suffix := range []string{"_t.dat", ".dat", "_h.dat", "_m.dat", "_s.dat", ".jpg", ".jpeg", ".png", ".gif"} {
			out = append(out, path+suffix)
		}
	}
	return out
}

func readImageFile(path string) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".dat" || ext == "" {
		out, convertedExt, err := dat2img.Dat2Image(data)
		if err == nil {
			data = out
			ext = "." + convertedExt
		} else if ext == ".dat" {
			return nil, "", err
		}
	}
	mt := http.DetectContentType(data)
	if !strings.HasPrefix(strings.ToLower(mt), "image/") {
		if byExt := mime.TypeByExtension(ext); strings.HasPrefix(strings.ToLower(byExt), "image/") {
			mt = byExt
		}
	}
	if !strings.HasPrefix(strings.ToLower(mt), "image/") {
		return nil, "", fmt.Errorf("not an image")
	}
	return data, mt, nil
}

func renderHTML(r report, nodes []imageNode, assets map[string]imageAsset) string {
	var b bytes.Buffer
	resolved, failed := 0, 0
	for _, n := range nodes {
		a := assets[n.AssetID]
		if a.DataURL != "" {
			resolved++
		} else {
			failed++
		}
	}
	pending := countStatus(r.Mentions, "pending")
	replied := countStatus(r.Mentions, "replied")
	write(&b, `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>微信日报 - %s</title><style>%s</style></head><body>`, esc(r.Date), css())
	write(&b, `<div class="shell"><aside class="side"><div class="brand"><div class="brand-mark">日</div><div><strong>微信日报</strong><span>%s</span></div></div><nav><a href="#overview">总览</a><a href="#todos">待办</a><a href="#groups">AI 群聊解析</a><a href="#mentions">@ 消息</a><a href="#images">图片证据</a><a href="#private">私聊更新</a><a href="#evidence">证据索引</a></nav></aside><main>`, esc(r.Date))
	write(&b, `<header class="top" id="overview"><div><p class="eyebrow">Local Daily Report</p><h1>曹明哲微信日报</h1><p class="sub">生成时间：%s · 本页自包含，可离线打开；未重新调用模型。</p></div><div class="hero-stat"><span>待处理</span><strong>%d</strong></div></header>`, esc(r.GeneratedAt), pending)
	write(&b, `<section class="metrics">`)
	metric(&b, "群聊 @", fmt.Sprint(len(r.Mentions)), "命中曹明哲 / caomz")
	metric(&b, "待处理", fmt.Sprint(pending), "需要回复或跟进")
	metric(&b, "已回复", fmt.Sprint(replied), "按日报规则识别")
	metric(&b, "图片节点", fmt.Sprintf("%d/%d", resolved, len(nodes)), fmt.Sprintf("未嵌入 %d", failed))
	metric(&b, "群聊解析", fmt.Sprint(len(r.GroupAnalyses)), "AI summary")
	metric(&b, "私聊更新", fmt.Sprint(len(r.PrivateUpdates)), "今日私聊会话")
	write(&b, `</section>`)

	writeTodos(&b, r.Todos)
	writeGroups(&b, r.GroupAnalyses)
	writeMentions(&b, r.Mentions, assets)
	writeImages(&b, nodes, assets)
	writePrivate(&b, r.PrivateUpdates, assets)
	writeEvidence(&b, r.Evidence)
	write(&b, `</main></div><div class="lightbox" id="lightbox" aria-hidden="true"><button class="lightbox-close" onclick="closeLightbox()">关闭</button><img alt="图片大图" id="lightbox-img"></div><script>%s</script></body></html>`, js())
	return b.String()
}

func writeTodos(b *bytes.Buffer, todos []todo) {
	write(b, `<section id="todos" class="section"><div class="section-head"><div><p class="eyebrow">Action</p><h2>待办优先</h2></div><span class="pill danger">%d 项</span></div><div class="todo-list">`, len(todos))
	limit := len(todos)
	if limit > 18 {
		limit = 18
	}
	for i := 0; i < limit; i++ {
		t := todos[i]
		write(b, `<article class="todo"><span class="check"></span><div><strong>%s</strong><p>%s</p><small>%s</small></div></article>`, esc(t.ChatName), esc(t.Text), esc(t.EvidenceID))
	}
	if len(todos) > limit {
		write(b, `<details class="more"><summary>展开剩余 %d 项待办</summary>`, len(todos)-limit)
		for i := limit; i < len(todos); i++ {
			t := todos[i]
			write(b, `<p class="compact-line"><b>%s</b> · %s · <code>%s</code></p>`, esc(t.ChatName), esc(t.Text), esc(t.EvidenceID))
		}
		write(b, `</details>`)
	}
	write(b, `</div></section>`)
}

func writeGroups(b *bytes.Buffer, groups []groupAnalysis) {
	write(b, `<section id="groups" class="section"><div class="section-head"><div><p class="eyebrow">AI Analysis</p><h2>AI 群聊解析</h2></div><span class="pill">%d 个群</span></div><div class="group-grid">`, len(groups))
	for _, g := range groups {
		write(b, `<article class="panel"><div class="panel-title"><h3>%s</h3><span>%s</span></div><p>%s</p>`, esc(g.ChatName), esc(strings.Join(g.EvidenceIDs, " / ")), esc(g.Summary))
		if len(g.Risks) > 0 {
			write(b, `<h4>风险</h4><ul>`)
			for _, item := range g.Risks {
				write(b, `<li>%s</li>`, esc(item))
			}
			write(b, `</ul>`)
		}
		if len(g.Todos) > 0 {
			write(b, `<h4>建议动作</h4><ul>`)
			for _, item := range g.Todos {
				write(b, `<li>%s</li>`, esc(item))
			}
			write(b, `</ul>`)
		}
		write(b, `</article>`)
	}
	write(b, `</div></section>`)
}

func writeMentions(b *bytes.Buffer, mentions []mention, assets map[string]imageAsset) {
	write(b, `<section id="mentions" class="section"><div class="section-head"><div><p class="eyebrow">Mentions</p><h2>群聊 @ 汇总</h2></div><span class="pill danger">%d 条</span></div><div class="mention-stack">`, len(mentions))
	for _, m := range mentions {
		statusClass := "danger"
		if m.Status == "replied" {
			statusClass = "ok"
		}
		write(b, `<article class="mention"><div class="mention-head"><div><h3>%s</h3><p>%s · %s · %s</p></div><div><span class="pill %s">%s</span><code>%s</code></div></div><blockquote>%s</blockquote>`, esc(m.ChatName), esc(m.Time), esc(m.SenderName), esc(m.EvidenceID), statusClass, esc(statusText(m.Status)), esc(m.EvidenceID), esc(m.Content))
		if strings.TrimSpace(m.Analysis.Topic+m.Analysis.Context+m.Analysis.SuggestedAction) != "" {
			write(b, `<div class="analysis"><b>%s</b><p>%s</p><p><strong>建议：</strong>%s</p></div>`, esc(m.Analysis.Topic), esc(m.Analysis.Context), esc(m.Analysis.SuggestedAction))
		}
		write(b, `<details><summary>查看前后文</summary><div class="timeline">`)
		for _, msg := range m.Before {
			writeMessage(b, msg, assets)
		}
		writeMessage(b, message{Time: m.Time, SenderName: m.SenderName, Content: m.Content, MediaRefs: m.MediaRefs, Position: "hit"}, assets)
		for _, msg := range m.After {
			writeMessage(b, msg, assets)
		}
		write(b, `</div></details></article>`)
	}
	write(b, `</div></section>`)
}

func writeMessage(b *bytes.Buffer, msg message, assets map[string]imageAsset) {
	pos := msg.Position
	if pos == "" {
		pos = "msg"
	}
	write(b, `<div class="msg %s"><time>%s</time><b>%s</b><p>%s</p>`, escAttr(pos), esc(msg.Time), esc(msg.SenderName), esc(msg.Content))
	if len(msg.MediaRefs) > 0 {
		a := assets[assetID(msg.MediaRefs[0])]
		writeImageThumb(b, a, msg.ImageAnalysis, msg.ImageAnalysisError)
	}
	write(b, `</div>`)
}

func writeImages(b *bytes.Buffer, nodes []imageNode, assets map[string]imageAsset) {
	write(b, `<section id="images" class="section"><div class="section-head"><div><p class="eyebrow">Image Evidence</p><h2>图片证据</h2></div><span class="pill">%d 张/处</span></div><div class="image-grid">`, len(nodes))
	for _, n := range nodes {
		a := assets[n.AssetID]
		write(b, `<article class="image-card"><div class="image-meta"><span>%s</span><code>%s</code></div>`, esc(n.Section), esc(n.ID))
		writeImageThumb(b, a, n.Analysis, n.Error)
		write(b, `<div class="image-info"><b>%s</b><p>%s · %s</p><p>%s</p></div></article>`, esc(n.ChatName), esc(n.Time), esc(n.Sender), esc(n.Content))
	}
	write(b, `</div></section>`)
}

func writeImageThumb(b *bytes.Buffer, a imageAsset, analysis, analysisErr string) {
	if a.DataURL != "" {
		write(b, `<figure class="thumb"><img loading="lazy" src="%s" alt="聊天图片" onclick="openLightbox(this.src)"><figcaption><span class="pill ok">图片已解析</span>%s</figcaption></figure>`, a.DataURL, esc(analysis))
		return
	}
	err := firstNonEmpty(analysisErr, a.Error, "原图未嵌入")
	write(b, `<div class="thumb fallback"><span class="pill warn">原图未嵌入</span><p>%s</p><small>%s</small></div>`, esc(analysis), esc(err))
}

func writePrivate(b *bytes.Buffer, updates []privateUpdate, assets map[string]imageAsset) {
	write(b, `<section id="private" class="section"><div class="section-head"><div><p class="eyebrow">Private</p><h2>私聊更新</h2></div><span class="pill">%d 个联系人</span></div><div class="private-grid">`, len(updates))
	for _, u := range updates {
		status := "ok"
		label := "无需立即回复"
		if u.NeedsReply {
			status = "danger"
			label = "可能待回复"
		}
		write(b, `<article class="private"><div class="panel-title"><h3>%s</h3><span class="pill %s">%s</span></div><p>%s</p><small>总消息 %d · 对方 %d · 我 %d · 最新 %s</small>`, esc(u.ChatName), status, label, esc(u.Summary), u.TotalMessages, u.IncomingMessages, u.SelfMessages, esc(u.LatestMessage.Time))
		if len(u.LatestMessage.MediaRefs) > 0 {
			a := assets[assetID(u.LatestMessage.MediaRefs[0])]
			writeImageThumb(b, a, u.LatestMessage.ImageAnalysis, u.LatestMessage.ImageAnalysisError)
		}
		write(b, `</article>`)
	}
	write(b, `</div></section>`)
}

func writeEvidence(b *bytes.Buffer, rows []evidence) {
	write(b, `<section id="evidence" class="section"><div class="section-head"><div><p class="eyebrow">Evidence</p><h2>原始证据索引</h2></div><span class="pill">%d 条</span></div><div class="table-wrap"><table><thead><tr><th>ID</th><th>群聊</th><th>时间</th><th>发送人</th><th>Seq</th><th>内容</th></tr></thead><tbody>`, len(rows))
	for _, row := range rows {
		write(b, `<tr><td><code>%s</code></td><td>%s</td><td>%s</td><td>%s</td><td>%d</td><td>%s</td></tr>`, esc(row.ID), esc(row.ChatName), esc(row.Time), esc(row.SenderName), row.Seq, esc(row.Content))
	}
	write(b, `</tbody></table></div></section>`)
}

func metric(b *bytes.Buffer, label, value, hint string) {
	write(b, `<article class="metric"><span>%s</span><strong>%s</strong><small>%s</small></article>`, esc(label), esc(value), esc(hint))
}

func css() string {
	return `
:root{--bg:#f5f7f8;--paper:#fff;--ink:#182026;--muted:#63717d;--line:#dce3e8;--accent:#0f766e;--accent2:#2563eb;--danger:#b42318;--warn:#a15c07;--ok:#087443;--shadow:0 10px 30px rgba(20,38,50,.08)}
*{box-sizing:border-box}html{scroll-behavior:smooth}body{margin:0;background:var(--bg);color:var(--ink);font:14px/1.65 -apple-system,BlinkMacSystemFont,"Segoe UI",Roboto,"PingFang SC","Noto Sans CJK SC",Arial,sans-serif;letter-spacing:0}a{color:inherit;text-decoration:none}
.shell{display:grid;grid-template-columns:240px minmax(0,1fr);min-height:100vh}.side{position:sticky;top:0;height:100vh;padding:24px 18px;border-right:1px solid var(--line);background:#eef3f4}.brand{display:flex;gap:12px;align-items:center;margin-bottom:28px}.brand-mark{display:grid;place-items:center;width:38px;height:38px;border-radius:8px;background:var(--ink);color:#fff;font-weight:800}.brand span{display:block;color:var(--muted);font-size:12px}.side nav{display:grid;gap:6px}.side a{padding:9px 10px;border-radius:7px;color:#31414c}.side a:hover{background:#fff}
main{max-width:1280px;width:100%;padding:26px 30px 60px}.top{display:flex;justify-content:space-between;gap:24px;align-items:flex-end;margin-bottom:18px}.eyebrow{margin:0 0 4px;color:var(--accent);font-size:12px;font-weight:800;text-transform:uppercase;letter-spacing:.08em}.top h1{margin:0;font-size:32px;line-height:1.2;letter-spacing:0}.sub{margin:8px 0 0;color:var(--muted)}.hero-stat{min-width:142px;padding:16px;border:1px solid var(--line);border-radius:8px;background:var(--paper);box-shadow:var(--shadow)}.hero-stat span,.metric span{display:block;color:var(--muted);font-size:12px}.hero-stat strong{font-size:36px;line-height:1.1;color:var(--danger)}
.metrics{display:grid;grid-template-columns:repeat(6,minmax(0,1fr));gap:12px;margin:18px 0 24px}.metric{padding:14px;border:1px solid var(--line);border-radius:8px;background:var(--paper)}.metric strong{display:block;margin:3px 0;font-size:24px}.metric small{color:var(--muted)}
.section{margin:24px 0 34px}.section-head{display:flex;justify-content:space-between;align-items:flex-end;gap:16px;margin:0 0 12px}.section h2{margin:0;font-size:22px}.pill{display:inline-flex;align-items:center;gap:6px;padding:2px 8px;border:1px solid #b7d4d0;border-radius:999px;background:#e9f7f5;color:var(--accent);font-size:12px;font-weight:700;white-space:nowrap}.pill.danger{border-color:#f0b8b1;background:#fff0ee;color:var(--danger)}.pill.warn{border-color:#f2d6a2;background:#fff8e8;color:var(--warn)}.pill.ok{border-color:#b9dec8;background:#eefaf2;color:var(--ok)}
.todo-list,.mention-stack{display:grid;gap:10px}.todo,.mention,.panel,.private,.image-card{border:1px solid var(--line);border-radius:8px;background:var(--paper);box-shadow:var(--shadow)}.todo{display:flex;gap:12px;padding:12px}.check{width:18px;height:18px;margin-top:3px;border:2px solid var(--danger);border-radius:5px}.todo p{margin:2px 0}.todo small,.compact-line{color:var(--muted)}.more{padding:12px;border:1px dashed var(--line);border-radius:8px;background:#fff}
.group-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.panel{padding:16px}.panel-title{display:flex;justify-content:space-between;gap:12px;align-items:flex-start}.panel h3,.private h3{margin:0;font-size:16px}.panel h4{margin:12px 0 4px;font-size:13px}.panel p{margin:10px 0}.panel ul{margin:6px 0 0;padding-left:18px}.panel li{margin:3px 0}.panel-title span{color:var(--muted);font-size:12px;text-align:right}
.mention{padding:16px}.mention-head{display:flex;justify-content:space-between;gap:16px}.mention h3{margin:0;font-size:17px}.mention-head p{margin:3px 0 0;color:var(--muted)}blockquote{margin:12px 0;padding:10px 12px;border-left:3px solid var(--accent2);background:#f4f7fb;border-radius:0 7px 7px 0}.analysis{padding:12px;border:1px solid #cbdcea;background:#f7fbff;border-radius:8px}.analysis p{margin:6px 0}.mention details{margin-top:12px}.timeline{display:grid;gap:8px;margin-top:10px}.msg{padding:10px;border:1px solid var(--line);border-radius:8px;background:#fbfcfd}.msg.hit{border-color:#9bbff5;background:#f4f8ff}.msg time{display:inline-block;min-width:144px;color:var(--muted)}.msg p{margin:5px 0 0}
.image-grid{display:grid;grid-template-columns:repeat(3,minmax(0,1fr));gap:12px}.image-card{overflow:hidden}.image-meta{display:flex;justify-content:space-between;padding:10px 12px;border-bottom:1px solid var(--line);color:var(--muted);font-size:12px}.image-info{padding:10px 12px}.image-info p{margin:4px 0;color:var(--muted)}.thumb{margin:0;padding:10px}.thumb img{display:block;width:100%;max-height:320px;object-fit:contain;border:1px solid var(--line);border-radius:7px;background:#f1f4f6;cursor:zoom-in}.thumb figcaption{margin-top:8px;color:#33424d;white-space:pre-wrap}.thumb.fallback{min-height:160px;border:1px dashed var(--line);border-radius:8px;background:#fffaf0}.thumb.fallback p{white-space:pre-wrap}.thumb.fallback small{color:var(--muted)}
.private-grid{display:grid;grid-template-columns:repeat(2,minmax(0,1fr));gap:12px}.private{padding:14px}.private p{margin:8px 0}.private small{color:var(--muted)}
.table-wrap{overflow:auto;border:1px solid var(--line);border-radius:8px;background:var(--paper)}table{width:100%;border-collapse:collapse;min-width:980px}th,td{padding:9px 10px;border-bottom:1px solid var(--line);text-align:left;vertical-align:top}th{position:sticky;top:0;background:#f6f8fa;color:#4b5a65;font-size:12px}code{font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:12px;color:#23313a}
.lightbox{position:fixed;inset:0;display:none;place-items:center;background:rgba(8,16,22,.86);z-index:10;padding:28px}.lightbox.open{display:grid}.lightbox img{max-width:96vw;max-height:90vh;border-radius:8px;background:#fff}.lightbox-close{position:fixed;right:22px;top:18px;border:0;border-radius:7px;background:#fff;color:#111;padding:8px 12px;cursor:pointer}
@media (max-width:1000px){.shell{display:block}.side{position:sticky;height:auto;z-index:5;padding:12px;border-right:0;border-bottom:1px solid var(--line)}.brand{margin-bottom:10px}.side nav{display:flex;overflow:auto}.side a{white-space:nowrap}.metrics{grid-template-columns:repeat(3,minmax(0,1fr))}.group-grid,.private-grid{grid-template-columns:1fr}.image-grid{grid-template-columns:repeat(2,minmax(0,1fr))}main{padding:18px}}
@media (max-width:640px){.top{display:block}.hero-stat{margin-top:12px}.metrics{grid-template-columns:repeat(2,minmax(0,1fr))}.image-grid{grid-template-columns:1fr}.mention-head,.section-head,.panel-title{display:block}.msg time{display:block;min-width:0}.top h1{font-size:26px}}
`
}

func js() string {
	return `function openLightbox(src){const b=document.getElementById('lightbox');document.getElementById('lightbox-img').src=src;b.classList.add('open');b.setAttribute('aria-hidden','false')}function closeLightbox(){const b=document.getElementById('lightbox');b.classList.remove('open');b.setAttribute('aria-hidden','true');document.getElementById('lightbox-img').src=''}document.addEventListener('keydown',e=>{if(e.key==='Escape')closeLightbox()});document.getElementById('lightbox').addEventListener('click',e=>{if(e.target.id==='lightbox')closeLightbox()});`
}

func countStatus(mentions []mention, status string) int {
	n := 0
	for _, m := range mentions {
		if m.Status == status {
			n++
		}
	}
	return n
}

func statusText(status string) string {
	if status == "replied" {
		return "已回复"
	}
	return "待处理"
}

func assetID(ref mediaRef) string {
	if strings.TrimSpace(ref.Path) != "" {
		return ref.Path
	}
	return ref.Key
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func write(b *bytes.Buffer, format string, args ...any) {
	_, _ = fmt.Fprintf(b, format, args...)
}

func esc(s string) string {
	return html.EscapeString(strings.TrimSpace(s))
}

func escAttr(s string) string {
	return strings.NewReplacer(`"`, "", `'`, "", "<", "", ">", "", " ", "-").Replace(strings.TrimSpace(s))
}

func must(err error) {
	if err != nil {
		fail(err.Error())
	}
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	os.Exit(1)
}
