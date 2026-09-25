package remote

import (
	"encoding/json"
	"time"
)

const (
	ProtocolV = 1

	OpList          = "list"
	OpMore          = "more"
	OpOpen          = "open"
	OpStart         = "start"
	OpSend          = "send"
	OpSteer         = "steer"
	OpStop          = "stop"
	OpAnswer        = "answer"
	OpWatch         = "watch"
	OpUnwatch       = "unwatch"
	OpLog           = "log"
	OpEvent         = "event"
	OpReady         = "ready"
	OpLagged        = "lagged"
	OpRunNow        = "run_now"
	OpCancelWait    = "cancel_wait"
	OpResumeGoal    = "resume_goal"
	OpHello         = "hello"
	OpCatalog       = "catalog"
	OpTune          = "tune"
	OpPut           = "put"
	OpFollowupDrop  = "followup_drop"
	OpFollowupSteer = "followup_steer"
	OpPreempt       = "preempt"
	OpClients       = "clients"
	OpClientRead    = "client_read"
)

// GroupRecent is the inbox section for conversations with no project.
const GroupRecent = "recent"

// MaxPushPayload is pairlink's sealed-frame cap. Seal adds an 8-byte
// counter and a 16-byte tag, and a reply may still stamp a display name,
// so a JSON body packed to this size never leaves the PC.
const MaxPushPayload = 64 << 10

// plaintextBudget is the JSON a frame can carry and still seal.
// 40 runes of display name are at most 160 bytes plus the host key.
func plaintextBudget() int {
	const sealOverhead = 8 + 16
	const hostSlack = 256
	return MaxPushPayload - sealOverhead - hostSlack
}

// Request is the slim RPC the phone sends over a sealed pairlink frame.
type Request struct {
	V         int             `json:"v"`
	ID        string          `json:"id"`
	Op        string          `json:"op"`
	Cursor    string          `json:"cursor,omitempty"`
	ThreadID  string          `json:"thread_id,omitempty"`
	ProjectID string          `json:"project_id,omitempty"`
	Text      string          `json:"text,omitempty"`
	CallID    string          `json:"call_id,omitempty"`
	Answers   json.RawMessage `json:"answers,omitempty"`
	Since     int64           `json:"since,omitempty"`
	Before    int64           `json:"before,omitempty"`
	// ProviderID and Model switch the conversation's endpoint. Empty leaves it.
	ProviderID string `json:"provider_id,omitempty"`
	Model      string `json:"model,omitempty"`
	// Reasoning is a pointer so the phone can select the empty default.
	// A missing field leaves the conversation's level alone.
	Reasoning *string `json:"reasoning,omitempty"`
	// Puts are finished upload ids from OpPut, consumed by start/send/steer.
	Puts []string `json:"puts,omitempty"`
	// Put fields stream one file or image across frames. Pairlink's plaintext
	// cap is 64KiB, so a photo cannot ride in a single request.
	PutID string `json:"put_id,omitempty"`
	Name  string `json:"name,omitempty"`
	MIME  string `json:"mime,omitempty"`
	Part  int    `json:"part,omitempty"`
	Parts int    `json:"parts,omitempty"`
	Data  string `json:"data,omitempty"`
	// Group pages one inbox section. "recent" is conversations with no
	// project. A project id pages that folder. Empty is the global idle page.
	Group string `json:"group,omitempty"`
	// FollowupID names one queued message for drop or steer.
	FollowupID string `json:"followup_id,omitempty"`
	// TaskID selects one local agent session for a read-only view.
	TaskID string `json:"task_id,omitempty"`
}

// Response is what the PC replies. Path and SessionID are pairlink
// metadata so zwai trace can join the remote hop.
type Response struct {
	V               int             `json:"v"`
	ID              string          `json:"id"`
	OK              bool            `json:"ok"`
	Error           string          `json:"error,omitempty"`
	Code            string          `json:"code,omitempty"`
	Path            string          `json:"path,omitempty"`
	SessionID       string          `json:"session_id,omitempty"`
	Host            string          `json:"host,omitempty"`
	Projects        []ProjectView   `json:"projects,omitempty"`
	Threads         []ThreadView    `json:"threads,omitempty"`
	Running         []RunningView   `json:"running,omitempty"`
	More            bool            `json:"more,omitempty"`
	Next            string          `json:"next,omitempty"`
	Detail          *ThreadDetail   `json:"detail,omitempty"`
	Op              string          `json:"op,omitempty"`
	ThreadID        string          `json:"thread_id,omitempty"`
	Seq             int64           `json:"seq,omitempty"`
	Event           *EventView      `json:"event,omitempty"`
	Events          []EventView     `json:"events,omitempty"`
	Status          *WatchStatus    `json:"status,omitempty"`
	Models          []ModelView     `json:"models,omitempty"`
	ReasoningLevels []string        `json:"reasoning_levels,omitempty"`
	Put             *PutView        `json:"put,omitempty"`
	Groups          []ThreadGroup   `json:"groups,omitempty"`
	Followups       *[]FollowupView `json:"followups,omitempty"`
	Clients         *ClientCatalog  `json:"clients,omitempty"`
	ClientView      *ClientView     `json:"client_view,omitempty"`
}

// ClientCatalog is the read-only local-agent list. Enabled false means the
// switch is off and Tools is empty. A phone that does not know the field
// ignores it.
type ClientCatalog struct {
	Enabled bool         `json:"enabled"`
	Pending bool         `json:"pending,omitempty"`
	Tools   []ClientTool `json:"tools,omitempty"`
}

// ClientTool is one agent family's page. Next is the before-cursor for More.
type ClientTool struct {
	ID    string       `json:"id"`
	Tasks []ClientTask `json:"tasks"`
	More  bool         `json:"more"`
	Next  string       `json:"next,omitempty"`
}

// ClientView is the read-only body of one foreign session.
type ClientView struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Status    string        `json:"status"`
	UpdatedAt time.Time     `json:"updated_at"`
	Entries   []ClientEntry `json:"entries"`
	Truncated bool          `json:"truncated,omitempty"`
}

// ClientEntry is one visible line. Role is user, assistant, or tool.
type ClientEntry struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// ClientTask is one foreign session. Status is "running" or "done".
type ClientTask struct {
	ID        string    `json:"id"`
	Title     string    `json:"title"`
	Status    string    `json:"status"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ThreadGroup is one inbox section's idle page: a project, or recent.
type ThreadGroup struct {
	ID      string       `json:"id"`
	Threads []ThreadView `json:"threads,omitempty"`
	More    bool         `json:"more,omitempty"`
	Next    string       `json:"next,omitempty"`
}

// FollowupView is a message waiting until this turn finishes. The phone
// paints it immediately; the transcript will not mention it until then.
type FollowupView struct {
	ID   string `json:"id"`
	Seq  int64  `json:"seq"`
	Text string `json:"text"`
}

// ModelView is one composer row. No endpoint, key, or token window: the phone
// only needs enough to pick a name the PC already knows.
type ModelView struct {
	ProviderID    string `json:"provider_id"`
	ProviderLabel string `json:"provider_label,omitempty"`
	Model         string `json:"model"`
	Default       bool   `json:"default,omitempty"`
}

// PutView is the progress of one chunked upload on this link.
type PutView struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Ready bool   `json:"ready"`
	Kind  string `json:"kind,omitempty"`
}

type WatchStatus struct {
	Running        bool            `json:"running,omitempty"`
	TurnID         string          `json:"turn_id,omitempty"`
	AwaitingAnswer bool            `json:"awaiting_answer,omitempty"`
	Waiting        bool            `json:"waiting,omitempty"`
	Wake           *WakeView       `json:"wake,omitempty"`
	Followups      *[]FollowupView `json:"followups,omitempty"`
}

// WakeView is the parked thread wait the phone paints. Prompt is clipped.
type WakeView struct {
	ID        string `json:"id"`
	Title     string `json:"title,omitempty"`
	Prompt    string `json:"prompt,omitempty"`
	NextRunAt string `json:"next_run_at,omitempty"`
}

// EventView is one conversation event on pairlink. Same kind/seq as the
// desktop SSE payload; text may be clipped so a frame stays under MaxPushPayload.
type EventView struct {
	ThreadID   string    `json:"thread_id"`
	TurnID     string    `json:"turn_id,omitempty"`
	Seq        int64     `json:"seq"`
	Kind       string    `json:"kind"`
	AgentID    string    `json:"agent_id,omitempty"`
	Role       string    `json:"role,omitempty"`
	Text       string    `json:"text"`
	ToolCallID string    `json:"tool_call_id,omitempty"`
	Err        string    `json:"err,omitempty"`
	HasImages  bool      `json:"has_images,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

type ProjectView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ThreadView struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	ProjectID    string    `json:"project_id,omitempty"`
	Running      bool      `json:"running"`
	Waiting      bool      `json:"waiting,omitempty"`
	LastActiveAt time.Time `json:"last_active_at"`
	Summary      string    `json:"summary,omitempty"`
}

type RunningView struct {
	ThreadID string `json:"thread_id"`
	Title    string `json:"title"`
	TurnID   string `json:"turn_id,omitempty"`
	// Action is a one-line human preview of the live turn: assistant
	// prose or a tool payload field (findings, command, path). Raw
	// tool_call envelopes stay off the wire. Empty when the latest
	// work is bookkeeping (schedule_wake, memory, …) so the phone can
	// localize Waiting / running. A parked wait has no live turn, so it
	// carries the thread's own summary instead of nothing at all.
	Action  string `json:"action,omitempty"`
	AskUser bool   `json:"ask_user,omitempty"`
	Waiting bool   `json:"waiting,omitempty"`
	// LastActiveAt dates the row. An In progress row is not in the
	// `threads` list — `list` drops it so Recents does not repeat it and
	// it does not consume thread_limit — so this is the only place a
	// parked wait's age can come from, and a wait armed last week reading
	// like today's work is the whole problem.
	LastActiveAt time.Time `json:"last_active_at"`
	// ProjectID is the folder this conversation belongs to. The idle page
	// still omits the row; the phone uses this to list it under the project
	// as well as under In progress. Empty when the conversation has no project.
	ProjectID string `json:"project_id,omitempty"`
}

type ThreadDetail struct {
	ID              string       `json:"id"`
	Title           string       `json:"title"`
	Goal            string       `json:"goal,omitempty"`
	GoalOn          bool         `json:"goal_on,omitempty"`
	GoalComplete    bool         `json:"goal_complete,omitempty"`
	GoalBlocked     bool         `json:"goal_blocked,omitempty"`
	GoalBlockReason string       `json:"goal_block_reason,omitempty"`
	GoalCapped      bool         `json:"goal_capped,omitempty"`
	GoalIdle        bool         `json:"goal_idle,omitempty"`
	GoalStartedAt   string       `json:"goal_started_at,omitempty"`
	PlanOn          bool         `json:"plan_on,omitempty"`
	ProviderID      string       `json:"provider_id,omitempty"`
	Model           string       `json:"model,omitempty"`
	Reasoning       string       `json:"reasoning,omitempty"`
	Waiting         bool         `json:"waiting,omitempty"`
	Wake            *WakeView    `json:"wake,omitempty"`
	Running         *RunningView `json:"running,omitempty"`
	Turns           []TurnView   `json:"turns,omitempty"`
}

type TurnView struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Text   string `json:"text,omitempty"`
}
