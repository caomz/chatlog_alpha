package dailyreport

import "time"

const (
	DefaultMention           = "caomz"
	DefaultTimezone          = "Asia/Shanghai"
	DefaultBeforeCount       = 5
	DefaultAfterCount        = 10
	DefaultReplyWindowMinute = 60
	DefaultMaxImages         = 50

	StatusReplied = "replied"
	StatusPending = "pending"
)

type ReportOptions struct {
	Date                string   `json:"date"`
	Mention             string   `json:"mention"`
	MentionAliases      []string `json:"mention_aliases"`
	Timezone            string   `json:"timezone"`
	BeforeCount         int      `json:"before_count"`
	AfterCount          int      `json:"after_count"`
	ReplyWindowMinutes  int      `json:"reply_window_minutes"`
	IncludePrivate      bool     `json:"include_private"`
	Summary             bool     `json:"summary"`
	Vision              bool     `json:"vision"`
	MaxImages           int      `json:"max_images"`
	AnalysisConcurrency int      `json:"analysis_concurrency"`
}

type Report struct {
	Date               string              `json:"date"`
	Timezone           string              `json:"timezone"`
	GeneratedAt        string              `json:"generated_at"`
	Options            ReportOptions       `json:"options"`
	Overview           ReportOverview      `json:"overview"`
	Mentions           []MentionItem       `json:"mentions"`
	PrivateUpdates     []PrivateChatUpdate `json:"private_updates,omitempty"`
	GroupAnalyses      []GroupAnalysis     `json:"group_analyses,omitempty"`
	Todos              []TodoItem          `json:"todos,omitempty"`
	Summary            string              `json:"summary,omitempty"`
	SummaryError       string              `json:"summary_error,omitempty"`
	AnalysisError      string              `json:"analysis_error,omitempty"`
	Evidence           []EvidenceItem      `json:"evidence"`
	SkippedTalkerCount int                 `json:"skipped_talker_count,omitempty"`
}

type ReportOverview struct {
	GroupMentionCount   int `json:"group_mention_count"`
	RepliedCount        int `json:"replied_count"`
	PendingCount        int `json:"pending_count"`
	PrivateChatCount    int `json:"private_chat_count"`
	ImportantTodoCount  int `json:"important_todo_count"`
	ScannedGroupCount   int `json:"scanned_group_count"`
	ScannedPrivateCount int `json:"scanned_private_count"`
	ScannedMessageCount int `json:"scanned_message_count"`
	SkippedMessageCount int `json:"skipped_message_count"`
}

type ChatMessage struct {
	ChatID             string     `json:"chat_id"`
	ChatName           string     `json:"chat_name"`
	IsChatRoom         bool       `json:"is_chatroom"`
	Sender             string     `json:"sender"`
	SenderName         string     `json:"sender_name"`
	Seq                int64      `json:"seq"`
	MsgID              int64      `json:"msg_id"`
	Timestamp          int64      `json:"timestamp"`
	Time               time.Time  `json:"-"`
	TimeText           string     `json:"time"`
	Content            string     `json:"content"`
	IsSelf             bool       `json:"is_self"`
	Type               int64      `json:"type,omitempty"`
	SubType            int64      `json:"sub_type,omitempty"`
	MediaRefs          []MediaRef `json:"media_refs,omitempty"`
	ImageAnalysis      string     `json:"image_analysis,omitempty"`
	ImageAnalysisError string     `json:"image_analysis_error,omitempty"`
}

type MentionItem struct {
	EvidenceID         string           `json:"evidence_id"`
	ChatID             string           `json:"chat_id"`
	ChatName           string           `json:"chat_name"`
	Sender             string           `json:"sender"`
	SenderName         string           `json:"sender_name"`
	Seq                int64            `json:"seq"`
	MsgID              int64            `json:"msg_id"`
	Timestamp          int64            `json:"timestamp"`
	Time               string           `json:"time"`
	Content            string           `json:"content"`
	MediaRefs          []MediaRef       `json:"media_refs,omitempty"`
	ImageAnalysis      string           `json:"image_analysis,omitempty"`
	ImageAnalysisError string           `json:"image_analysis_error,omitempty"`
	Status             string           `json:"status"`
	Analysis           AnalysisResult   `json:"analysis,omitempty"`
	Before             []ContextMessage `json:"before,omitempty"`
	After              []ContextMessage `json:"after,omitempty"`
	Replies            []ReplyItem      `json:"replies,omitempty"`
}

type AnalysisResult struct {
	Topic           string   `json:"topic,omitempty"`
	Context         string   `json:"context,omitempty"`
	SuggestedAction string   `json:"suggested_action,omitempty"`
	NeedsReply      *bool    `json:"needs_reply,omitempty"`
	EvidenceIDs     []string `json:"evidence_ids,omitempty"`
	Error           string   `json:"error,omitempty"`
}

type GroupAnalysis struct {
	ChatID      string   `json:"chat_id"`
	ChatName    string   `json:"chat_name"`
	Summary     string   `json:"summary,omitempty"`
	Risks       []string `json:"risks,omitempty"`
	Todos       []string `json:"todos,omitempty"`
	EvidenceIDs []string `json:"evidence_ids,omitempty"`
	Error       string   `json:"error,omitempty"`
}

type ReplyItem struct {
	EvidenceID string `json:"evidence_id"`
	Seq        int64  `json:"seq"`
	MsgID      int64  `json:"msg_id"`
	Timestamp  int64  `json:"timestamp"`
	Time       string `json:"time"`
	Content    string `json:"content"`
}

type ContextMessage struct {
	Seq                int64      `json:"seq"`
	MsgID              int64      `json:"msg_id"`
	Timestamp          int64      `json:"timestamp"`
	Time               string     `json:"time"`
	Sender             string     `json:"sender"`
	SenderName         string     `json:"sender_name"`
	Content            string     `json:"content"`
	MediaRefs          []MediaRef `json:"media_refs,omitempty"`
	ImageAnalysis      string     `json:"image_analysis,omitempty"`
	ImageAnalysisError string     `json:"image_analysis_error,omitempty"`
	IsSelf             bool       `json:"is_self"`
	Position           string     `json:"position"`
}

type PrivateChatUpdate struct {
	ChatID           string      `json:"chat_id"`
	ChatName         string      `json:"chat_name"`
	TotalMessages    int         `json:"total_messages"`
	IncomingMessages int         `json:"incoming_messages"`
	SelfMessages     int         `json:"self_messages"`
	LatestMessage    ChatMessage `json:"latest_message"`
	NeedsReply       bool        `json:"needs_reply"`
	Summary          string      `json:"summary,omitempty"`
}

type TodoItem struct {
	Text       string `json:"text"`
	EvidenceID string `json:"evidence_id,omitempty"`
	ChatID     string `json:"chat_id,omitempty"`
	ChatName   string `json:"chat_name,omitempty"`
}

type EvidenceItem struct {
	ID                 string     `json:"id"`
	Type               string     `json:"type"`
	ChatID             string     `json:"chat_id"`
	ChatName           string     `json:"chat_name"`
	Sender             string     `json:"sender"`
	SenderName         string     `json:"sender_name"`
	Seq                int64      `json:"seq"`
	MsgID              int64      `json:"msg_id"`
	Timestamp          int64      `json:"timestamp"`
	Time               string     `json:"time"`
	Content            string     `json:"content"`
	MediaRefs          []MediaRef `json:"media_refs,omitempty"`
	ImageAnalysis      string     `json:"image_analysis,omitempty"`
	ImageAnalysisError string     `json:"image_analysis_error,omitempty"`
	IsSelf             bool       `json:"is_self"`
}

type MediaRef struct {
	Type string `json:"type"`
	Key  string `json:"key"`
	Path string `json:"path,omitempty"`
}

type SaveResult struct {
	MarkdownPath         string `json:"markdown_path,omitempty"`
	JSONPath             string `json:"json_path,omitempty"`
	DialogueAnalysisPath string `json:"dialogue_analysis_path,omitempty"`
}
