package remote

import (
	"encoding/json"
	"time"
)

const (
	ProtocolV = 1

	OpList    = "list"
	OpMore    = "more"
	OpOpen    = "open"
	OpStart   = "start"
	OpSend    = "send"
	OpSteer   = "steer"
	OpStop    = "stop"
	OpAnswer  = "answer"
	OpWatch   = "watch"
	OpUnwatch = "unwatch"
	OpEvent   = "event"
	OpReady   = "ready"
	OpLagged  = "lagged"
)

// MaxPushPayload is pairlink's plaintext cap. A tool_delta that would
// blow this is clipped or dropped (seq 0) rather than tearing the link.
const MaxPushPayload = 64 << 10

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
}

// Response is what the PC replies. Path and SessionID are pairlink
// metadata so zwai trace can join the remote hop.
type Response struct {
	V         int           `json:"v"`
	ID        string        `json:"id"`
	OK        bool          `json:"ok"`
	Error     string        `json:"error,omitempty"`
	Code      string        `json:"code,omitempty"`
	Path      string        `json:"path,omitempty"`
	SessionID string        `json:"session_id,omitempty"`
	Projects  []ProjectView `json:"projects,omitempty"`
	Threads   []ThreadView  `json:"threads,omitempty"`
	Running   []RunningView `json:"running,omitempty"`
	More      bool          `json:"more,omitempty"`
	Next      string        `json:"next,omitempty"`
	Detail    *ThreadDetail `json:"detail,omitempty"`
	Op        string        `json:"op,omitempty"`
	ThreadID  string        `json:"thread_id,omitempty"`
	Seq       int64         `json:"seq,omitempty"`
	Event     *EventView    `json:"event,omitempty"`
	Status    *WatchStatus  `json:"status,omitempty"`
}

type WatchStatus struct {
	Running        bool   `json:"running,omitempty"`
	TurnID         string `json:"turn_id,omitempty"`
	AwaitingAnswer bool   `json:"awaiting_answer,omitempty"`
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
	LastActiveAt time.Time `json:"last_active_at"`
	Summary      string    `json:"summary,omitempty"`
}

type RunningView struct {
	ThreadID string `json:"thread_id"`
	Title    string `json:"title"`
	TurnID   string `json:"turn_id,omitempty"`
	Action   string `json:"action,omitempty"`
	AskUser  bool   `json:"ask_user,omitempty"`
}

type ThreadDetail struct {
	ID      string       `json:"id"`
	Title   string       `json:"title"`
	Goal    string       `json:"goal,omitempty"`
	GoalOn  bool         `json:"goal_on,omitempty"`
	PlanOn  bool         `json:"plan_on,omitempty"`
	Running *RunningView `json:"running,omitempty"`
	Turns   []TurnView   `json:"turns,omitempty"`
}

type TurnView struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Text   string `json:"text,omitempty"`
}
