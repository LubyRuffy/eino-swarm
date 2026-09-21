// Package store is zwai's persistence layer: conversations, their transcripts,
// the event timeline the UI replays, and the model-call records that make a
// turn reconstructible after the fact.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Turn status values.
const (
	TurnRunning   = "running"
	TurnDone      = "done"
	TurnError     = "error"
	TurnCancelled = "cancelled"
)

// Project groups conversations that share a working directory, a system prompt
// and a memory store. Its ID names directories, so it must stay
// filesystem-safe.
type Project struct {
	ID   string `gorm:"primaryKey;size:64" json:"id"`
	Name string `gorm:"size:200" json:"name"`
	// SystemPrompt is added to the manager's prompt for every conversation in
	// this project. It is the user's text, verbatim.
	SystemPrompt string `json:"system_prompt"`
	// Workdir is the directory the agents work in. Empty means the managed one
	// under the data directory, so a project is usable before anyone has a
	// path in mind.
	Workdir string `gorm:"size:1000" json:"workdir"`
	// MemoryEnabled lets one project opt out of memory while the rest keep it.
	MemoryEnabled bool `json:"memory_enabled"`
	// SortRank is the sidebar's manual order. Zero means "never dragged":
	// those rows interleave by UpdatedAt with ranked rows, so an idle
	// unranked project cannot sit above one that was just used.
	SortRank  int       `json:"sort_rank"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Thread is one conversation. Its ID also names its workspace directory, so it
// must stay filesystem-safe.
type Thread struct {
	ID    string `gorm:"primaryKey;size:64" json:"id"`
	Title string `gorm:"size:400" json:"title"`
	// TitleAuto is true while the engine still owns the title: empty or the
	// first-message placeholder. A user rename or a landed generated name
	// clears it so a later namer cannot overwrite the sidebar.
	TitleAuto bool `json:"title_auto"`
	// ProjectID is empty for a conversation that belongs to no project. Those
	// keep their own workspace directory; a project's conversations share the
	// project's.
	ProjectID  string `gorm:"index;size:64" json:"project_id"`
	ProviderID string `gorm:"size:64" json:"provider_id"`
	// Model is the name this conversation sends. Empty means the provider's
	// configured default, so a Settings change applies until someone picks
	// a different name in the composer.
	Model string `gorm:"size:200" json:"model"`
	// ReasoningEffort is this conversation's thinking level ("", low, medium,
	// high). Empty means the model's own default. It applies from the next turn.
	ReasoningEffort string `gorm:"size:16" json:"reasoning_effort"`
	// Goal is a standing objective the human set with /goal. Empty means none.
	// It is injected into later turns until they change or clear it.
	Goal string `json:"goal"`
	// GoalComplete is true after the manager called complete_goal (or the
	// human cleared and reset). A completed goal stays on the thread so the
	// banner can show what was achieved; it is no longer pursued.
	GoalComplete bool `json:"goal_complete"`
	// GoalBlocked is true after the manager called block_goal: progress
	// needs the human or an external change. Auto-continue stops until
	// they resume. A human message also clears it.
	GoalBlocked bool `json:"goal_blocked"`
	// GoalBlockReason is the optional one-line reason from block_goal.
	GoalBlockReason string `json:"goal_block_reason"`
	// GoalStartedAt is when the current objective was set (not edited).
	// Nil when there is no goal. The banner uses it as an elapsed clock.
	GoalStartedAt *time.Time `json:"goal_started_at,omitempty"`
	// GoalAutoTurns counts consecutive runtime-started turns that kept
	// pursuing an open goal. A human message resets it. Hitting the cap
	// sets GoalCapped and stops auto-continue until the human speaks.
	// A human interrupt of a pursuing turn also sets GoalCapped (paused).
	GoalAutoTurns int  `json:"goal_auto_turns"`
	GoalCapped    bool `json:"goal_capped"`
	// GoalIdle is true after an engine-started continuation finished with
	// no counted tool activity. Auto-continue stops until a human message
	// or resume; the objective stays open (not blocked, not capped).
	GoalIdle bool `json:"goal_idle"`
	// PlanMode is true while /plan is open: the manager explores and drafts,
	// and write/edit/exec are not mounted.
	PlanMode bool `json:"plan_mode"`
	// PlanMarkdown is the current plan body. The file under the data directory
	// is the on-disk copy; this column is what GET returns.
	PlanMarkdown string `json:"plan_markdown"`
	// CompactSummary replaces earlier replay messages (seq <= CompactThroughSeq)
	// in the next turn's prompt. The event log is untouched: compacting is
	// what the model sees, not what the transcript shows.
	CompactSummary    string `json:"compact_summary"`
	CompactThroughSeq int64  `json:"compact_through_seq"`
	// SessionMemory is a rolling briefing of this conversation, updated from
	// the event log before compact. CompactSummary is what later turns see;
	// this field is the source of that briefing and of the post-turn review.
	SessionMemory           string `json:"session_memory"`
	SessionMemoryThroughSeq int64  `json:"session_memory_through_seq"`
	SessionMemoryTokens     int    `json:"session_memory_tokens"`
	Archived                bool   `json:"archived"`
	// Pinned is a conversation the user is tracking at the top of the sidebar.
	// PinnedAt is when they pinned it; nil when they have not.
	Pinned   bool       `json:"pinned"`
	PinnedAt *time.Time `json:"pinned_at,omitempty"`
	// SortRank is the sidebar's manual order. Zero means "never dragged":
	// those rows interleave by LastActiveAt with ranked rows, so a stale
	// unranked conversation cannot sit above one that just ran.
	SortRank     int       `json:"sort_rank"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastActiveAt time.Time `gorm:"index" json:"last_active_at"`
}

// Message is one entry of the conversation as the model sees it. This is the
// record replayed into the next turn, which is why tool calls and tool results
// are kept verbatim rather than summarized.
type Message struct {
	ID         uint   `gorm:"primaryKey" json:"id"`
	ThreadID   string `gorm:"index:idx_msg_thread_seq;size:64" json:"thread_id"`
	TurnID     string `gorm:"index;size:64" json:"turn_id"`
	Seq        int64  `gorm:"index:idx_msg_thread_seq" json:"seq"`
	Role       string `gorm:"size:32" json:"role"`
	AgentID    string `gorm:"size:64" json:"agent_id"`
	Content    string `json:"content"`
	Reasoning  string `json:"reasoning,omitempty"`
	ToolCalls  string `json:"tool_calls,omitempty"` // JSON array, as received
	ToolCallID string `gorm:"size:128" json:"tool_call_id,omitempty"`
	// Images are pasted vision inputs, stored as files under the data
	// directory. The bytes do not live here: replaying a turn reloads them.
	Images []ImageRef `gorm:"serializer:json" json:"images,omitempty"`
	// EventSeq ties a stored [steer] user row to the timeline event so
	// retracting unread steering can drop that one message without
	// matching on caption text.
	EventSeq  int64     `json:"event_seq,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// ImageRef is the handle a pasted image travels as on the wire and in the
// database. The pixels sit in the data directory, named by ID.
type ImageRef struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
	MIME string `json:"mime"`
}

// Turn is one user request and everything the swarm did to answer it. The ID
// is the handle a user pastes into `zwai trace` to replay the whole thing.
type Turn struct {
	ID         string `gorm:"primaryKey;size:64" json:"id"`
	ThreadID   string `gorm:"index;size:64" json:"thread_id"`
	Seq        int64  `json:"seq"`
	Status     string `gorm:"size:32" json:"status"`
	UserText   string `json:"user_text"`
	Final      string `json:"final"`
	Error      string `json:"error,omitempty"`
	ProviderID string `gorm:"size:64" json:"provider_id"`
	Model      string `gorm:"size:128" json:"model"`
	// ReasoningEffort records the thinking level this turn actually ran with,
	// so a trace shows what produced the answer, not what is configured now.
	ReasoningEffort string `gorm:"size:16" json:"reasoning_effort,omitempty"`
	// GoalContinue is true when the engine started this turn to keep pursuing
	// an open standing objective. The transcript records goal_continued, not
	// a human user_message.
	GoalContinue bool `json:"goal_continue,omitempty"`
	// ScheduleRunID is set when this turn is a scheduled fire. Empty for
	// every other origin. Trace uses it to join the run.
	ScheduleRunID string `gorm:"size:64" json:"schedule_run_id,omitempty"`
	// Quiet is true when a scheduled turn had nothing to report: the
	// transcript hides the bubbles, the row still exists for `zwai trace`.
	Quiet bool `json:"quiet,omitempty"`
	// ScheduleContinue is true when the engine started this turn because a
	// schedule fired. The transcript records schedule_fired, not user_message.
	ScheduleContinue bool       `json:"schedule_continue,omitempty"`
	StartedAt        time.Time  `json:"started_at"`
	EndedAt          *time.Time `json:"ended_at,omitempty"`
	DurationMS       int64      `json:"duration_ms"`
}

// Event is one entry of the UI timeline. Seq is per-thread and gap-free, so a
// reconnecting client asks for everything after the last seq it saw and misses
// nothing.
type Event struct {
	ID         uint       `gorm:"primaryKey" json:"-"`
	ThreadID   string     `gorm:"index:idx_evt_thread_seq;size:64" json:"thread_id"`
	TurnID     string     `gorm:"index;size:64" json:"turn_id"`
	Seq        int64      `gorm:"index:idx_evt_thread_seq" json:"seq"`
	Kind       string     `gorm:"size:32" json:"kind"`
	AgentID    string     `gorm:"size:64" json:"agent_id"`
	Role       string     `gorm:"size:64" json:"role,omitempty"`
	Text       string     `json:"text"`
	ToolCallID string     `gorm:"size:128" json:"tool_call_id,omitempty"`
	Err        string     `json:"err,omitempty"`
	Images     []ImageRef `gorm:"serializer:json" json:"images,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

// LLMCall records one model request. It carries sizes, token counts and
// timings rather than full prompts: enough to explain why a turn was slow,
// expensive or failed, without turning the database into a transcript archive.
type LLMCall struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	ThreadID    string `gorm:"index;size:64" json:"thread_id"`
	TurnID      string `gorm:"index;size:64" json:"turn_id"`
	AgentID     string `gorm:"size:64" json:"agent_id"`
	ProviderID  string `gorm:"size:64" json:"provider_id"`
	Model       string `gorm:"size:128" json:"model"`
	InputMsgs   int    `json:"input_msgs"`
	InputChars  int    `json:"input_chars"`
	OutputChars int    `json:"output_chars"`
	// PromptTokens and friends come from the endpoint's usage block. Zero
	// means the endpoint did not say; the UI must not invent a tokenizer.
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	CachedTokens     int       `json:"cached_tokens"`
	ReasoningTokens  int       `json:"reasoning_tokens"`
	DurationMS       int64     `json:"duration_ms"`
	Err              string    `json:"err,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

// Attachment is a file the user uploaded into a conversation's workspace.
type Attachment struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ThreadID  string    `gorm:"index;size:64" json:"thread_id"`
	TurnID    string    `gorm:"size:64" json:"turn_id,omitempty"`
	Name      string    `gorm:"size:400" json:"name"`
	RelPath   string    `gorm:"size:800" json:"rel_path"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

// Followup is a message typed while a turn was already running. It waits for
// that turn to finish; Steer pulls it into the current turn instead.
type Followup struct {
	ID        string    `gorm:"primaryKey;size:64" json:"id"`
	ThreadID  string    `gorm:"index;size:64" json:"thread_id"`
	Seq       int64     `json:"seq"`
	Text      string    `json:"text"`
	CreatedAt time.Time `json:"created_at"`
}

// NewID returns a short, filesystem- and URL-safe identifier. Thread ids name
// directories, so no separators or shell metacharacters are allowed in them.
func NewID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is fatal-grade, but an id derived from the clock
		// still keeps the app usable instead of crashing a desktop session.
		now := time.Now().UnixNano()
		for i := range b {
			b[i] = byte(now >> (8 * i))
		}
	}
	return prefix + hex.EncodeToString(b[:])
}
