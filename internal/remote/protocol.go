package remote

import (
	"encoding/json"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/speech"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

const (
	OpSetProductMode = "set_product_mode"
	ProtocolV        = 1

	OpMeta                = "meta"
	OpList                = "list"
	OpMore                = "more"
	OpOpen                = "open"
	OpRead                = "read"
	OpStart               = "start"
	OpSend                = "send"
	OpDeliver             = "deliver"
	OpSteer               = "steer"
	OpStop                = "stop"
	OpContinue            = "continue"
	OpAnswer              = "answer"
	OpWatch               = "watch"
	OpUnwatch             = "unwatch"
	OpLog                 = "log"
	OpEvent               = "event"
	OpReady               = "ready"
	OpLagged              = "lagged"
	OpRunNow              = "run_now"
	OpCancelWait          = "cancel_wait"
	OpResumeGoal          = "resume_goal"
	OpHello               = "hello"
	OpProjectGet          = "project_get"
	OpProjectCreate       = "project_create"
	OpProjectUpdate       = "project_update"
	OpCatalog             = "catalog"
	OpRefreshCatalog      = "refresh_catalog"
	OpGetTimeouts         = "get_timeouts"
	OpSetTimeouts         = "set_timeouts"
	OpTune                = "tune"
	OpPut                 = "put"
	OpFollowupDrop        = "followup_drop"
	OpFollowupSteer       = "followup_steer"
	OpPreempt             = "preempt"
	OpClients             = "clients"
	OpClientRead          = "client_read"
	OpFiles               = "files"
	OpFileChunk           = "file_chunk"
	OpInputImageChunk     = "input_image_chunk"
	OpGeneratedImageChunk = "generated_image_chunk"
	OpGraph               = "graph"
	OpTaskPanels          = "task_panels"
	OpTaskPanelRead       = "task_panel_read"
	OpTaskPanelCancel     = "task_panel_cancel"
	OpTaskPanelClose      = "task_panel_close"
	OpTranslateSelection  = "translate_selection"
	OpCancelTranslation   = "cancel_translation"
	OpRefreshCapabilities = "refresh_capabilities"
	OpSpeechConfig        = "speech_config"
	OpAudioTranscribe     = "audio_transcribe"
	OpAudioSpeech         = "audio_speech"
	OpAudioChunk          = "audio_chunk"
	OpAudioCancel         = "audio_cancel"
)

// GroupRecent is the inbox section for conversations with no project.
const GroupRecent = "recent"

// GroupAttention pages completed results independently of idle inbox groups.
const GroupAttention = "attention"

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
	// ProductMode is explicit install-wide debug intent, never phone credentials.
	ProductMode  string          `json:"product_mode,omitempty"`
	V            int             `json:"v"`
	ID           string          `json:"id"`
	Op           string          `json:"op"`
	Cursor       string          `json:"cursor,omitempty"`
	ThreadID     string          `json:"thread_id,omitempty"`
	ProjectID    string          `json:"project_id,omitempty"`
	Text         string          `json:"text,omitempty"`
	DeliveryMode string          `json:"mode,omitempty"`
	ExpectedTurn string          `json:"expected_turn_id,omitempty"`
	CallID       string          `json:"call_id,omitempty"`
	Answers      json.RawMessage `json:"answers,omitempty"`
	// Continue is explicit so a missing decision cannot decline a paused turn.
	Continue *bool `json:"continue,omitempty"`
	Since    int64 `json:"since,omitempty"`
	// IncludeDetail opens a conversation in the watch ready reply. Older hosts
	// ignore it; the phone can still request open separately.
	IncludeDetail  bool  `json:"include_detail,omitempty"`
	CompletedSeq   int64 `json:"completed_seq,omitempty"`
	Before         int64 `json:"before,omitempty"`
	CompactHistory bool  `json:"compact_history,omitempty"`
	WorkFrom       int64 `json:"work_from,omitempty"`
	WorkThrough    int64 `json:"work_through,omitempty"`
	// ProviderID and Model switch the conversation's endpoint. Empty leaves it.
	ProviderID string `json:"provider_id,omitempty"`
	Model      string `json:"model,omitempty"`
	// Reasoning is a pointer so the phone can select the empty default.
	// A missing field leaves the conversation's level alone.
	Reasoning *string `json:"reasoning,omitempty"`
	// WebExecution changes only this conversation's next-turn preference.
	// Missing preserves it; an explicit empty string inherits the PC setting.
	WebExecution *string `json:"web_execution,omitempty"`
	// Puts are finished upload ids from OpPut, consumed by start/send/steer.
	Puts []string `json:"puts,omitempty"`
	// Put fields stream one file or image across frames. Pairlink's plaintext
	// cap is 64KiB, so a photo cannot ride in a single request.
	PutID   string `json:"put_id,omitempty"`
	Purpose string `json:"purpose,omitempty"`
	AudioID string `json:"audio_id,omitempty"`
	Offset  int64  `json:"offset,omitempty"`
	Name    string `json:"name,omitempty"`
	MIME    string `json:"mime,omitempty"`
	Part    int    `json:"part,omitempty"`
	Parts   int    `json:"parts,omitempty"`
	Data    string `json:"data,omitempty"`
	// Group pages one inbox section. "recent" is conversations with no
	// project. A project id pages that folder. Empty is the global idle page.
	Group string `json:"group,omitempty"`
	// FollowupID names one queued message for drop or steer.
	FollowupID string `json:"followup_id,omitempty"`
	// TaskID selects one local agent session for a read-only view.
	// Before on client_read is that view's byte offset, not a log seq.
	TaskID string `json:"task_id,omitempty"`
	// FilePath is workspace-relative; Before is the byte offset for file_chunk.
	FilePath string `json:"file_path,omitempty"`
	ImageID  string `json:"image_id,omitempty"`
	// Directory is the workspace directory OpFiles lists. Empty is the root.
	// Text on OpFiles searches the whole listed workspace instead of one directory.
	Directory string `json:"directory,omitempty"`
	FileScope string `json:"file_scope,omitempty"`
	FileRoot  string `json:"file_root,omitempty"`

	PanelID           string `json:"panel_id,omitempty"`
	EASMPanelVersion  string `json:"easm_panel_version,omitempty"`
	PanelBaseRevision *int64 `json:"base_revision,omitempty"`
	PanelTransferID   string `json:"transfer_id,omitempty"`

	ProjectPatch    json.RawMessage `json:"project_patch,omitempty"`
	TimeoutSettings json.RawMessage `json:"timeout_settings,omitempty"`
	timeoutInvalid  bool
}

// Response is what the PC replies. Path and SessionID are pairlink
// metadata so zwai trace can join the remote hop.
type Response struct {
	Translation     string                  `json:"translation,omitempty"`
	ModelPolicy     *provider.ModelPolicy   `json:"model_policy,omitempty"`
	Speech          *speech.SnapshotView    `json:"speech,omitempty"`
	SpeechError     string                  `json:"speech_error,omitempty"`
	Audio           *AudioView              `json:"audio,omitempty"`
	Startup         string                  `json:"startup,omitempty"`
	Restore         *engine.RestoreProgress `json:"restore,omitempty"`
	DataGeneration  *int64                  `json:"data_generation,omitempty"`
	ProductMode     string                  `json:"product_mode,omitempty"`
	V               int                     `json:"v"`
	ID              string                  `json:"id"`
	OK              bool                    `json:"ok"`
	Error           string                  `json:"error,omitempty"`
	Code            string                  `json:"code,omitempty"`
	Path            string                  `json:"path,omitempty"`
	SessionID       string                  `json:"session_id,omitempty"`
	Host            string                  `json:"host,omitempty"`
	Projects        []ProjectView           `json:"projects,omitempty"`
	Threads         []ThreadView            `json:"threads,omitempty"`
	Running         []RunningView           `json:"running,omitempty"`
	Attention       []ThreadView            `json:"attention,omitempty"`
	AttentionMore   bool                    `json:"attention_more,omitempty"`
	AttentionCursor string                  `json:"attention_cursor,omitempty"`
	More            bool                    `json:"more,omitempty"`
	Next            string                  `json:"next,omitempty"`
	Detail          *ThreadDetail           `json:"detail,omitempty"`
	Op              string                  `json:"op,omitempty"`
	ThreadID        string                  `json:"thread_id,omitempty"`
	Seq             int64                   `json:"seq,omitempty"`
	Event           *EventView              `json:"event,omitempty"`
	Events          []EventView             `json:"events,omitempty"`
	// Roster is set only on a live-edge watch ready. A non-nil empty slice
	// means that snapshot has no running worker outside its event page.
	// Older log pages leave it nil so a phone cannot walk its cursor from a
	// spawned seq and skip the events in between.
	Roster                *[]EventView               `json:"roster,omitempty"`
	HistoryWork           []HistoryWork              `json:"history_work,omitempty"`
	FocusSeq              int64                      `json:"focus_seq,omitempty"`
	HistoryBefore         int64                      `json:"history_before,omitempty"`
	Status                *WatchStatus               `json:"status,omitempty"`
	Models                []ModelView                `json:"models,omitempty"`
	ReasoningLevels       []string                   `json:"reasoning_levels,omitempty"`
	WebExecutionSupported bool                       `json:"web_execution_supported,omitempty"`
	GraphAvailable        bool                       `json:"graph_available,omitempty"`
	Put                   *PutView                   `json:"put,omitempty"`
	Groups                []ThreadGroup              `json:"groups,omitempty"`
	Followups             *[]FollowupView            `json:"followups,omitempty"`
	Delivery              *DeliveryView              `json:"delivery,omitempty"`
	Clients               *ClientCatalog             `json:"clients,omitempty"`
	Files                 []FileView                 `json:"files,omitempty"`
	FileView              *engine.WorkspaceView      `json:"file_view,omitempty"`
	FileChunk             *FileChunkView             `json:"file_chunk,omitempty"`
	TaskPanels            *[]store.TaskPanelManifest `json:"task_panels,omitempty"`
	PanelTransfer         *PanelTransferView         `json:"panel_transfer,omitempty"`
	Graph                 *GraphReply                `json:"graph,omitempty"`

	Project             *ProjectSettings          `json:"project,omitempty"`
	ComposerPredictions *bool                     `json:"composer_predictions,omitempty"`
	MemoryAvailable     *bool                     `json:"memory_available,omitempty"`
	TimeoutSettings     *config.TimeoutSettings   `json:"timeout_settings,omitempty"`
	WebExecutionStatus  *tools.WebExecutionStatus `json:"web_execution_status,omitempty"`
}

// HistoryWork marks an inclusive interval of deferred transcript details.
// Its sequence bounds are cursors, separate from the durable event payloads.
type HistoryWork struct {
	From     int64    `json:"from"`
	Through  int64    `json:"through"`
	AgentIDs []string `json:"agent_ids,omitempty"`
}

// FileView is one PC workspace entry, including upload provenance.
type FileView struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Dir      bool      `json:"dir"`
	Modified time.Time `json:"modified"`
	Uploaded bool      `json:"uploaded"`
}

// FileChunkView carries one bounded slice of a workspace file over pairlink.
type FileChunkView struct {
	Path       string `json:"path"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	MIME       string `json:"mime"`
	Offset     int64  `json:"offset"`
	NextOffset int64  `json:"next_offset"`
	Data       string `json:"data"`
	More       bool   `json:"more"`
}

// ClientCatalog is the disabled response retained for old phones.
type ClientCatalog struct {
	Enabled bool `json:"enabled"`
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
	ID          string `json:"id"`
	Seq         int64  `json:"seq"`
	Text        string `json:"text"`
	Attachments int    `json:"attachments,omitempty"`
}

type DeliveryView struct {
	TurnID    string `json:"turn_id,omitempty"`
	SteerSeq  int64  `json:"steer_seq,omitempty"`
	Preempted bool   `json:"preempted,omitempty"`
}

// ModelView is one composer row. No endpoint, key, or token window: the phone
// only needs enough to pick a name the PC already knows.
type ModelView struct {
	CatalogGroup  string `json:"catalog_group,omitempty"`
	ProviderID    string `json:"provider_id"`
	ProviderLabel string `json:"provider_label,omitempty"`
	// Label is the name to show when it differs from the model field. An
	// official profile uses it so the phone does not display the alias.
	Label                 string `json:"label,omitempty"`
	Model                 string `json:"model"`
	Default               bool   `json:"default,omitempty"`
	DefaultFollowsSetting bool   `json:"default_follows_setting,omitempty"`
	// Ready is false when the name is a saved default the catalog no longer
	// lists. The phone keeps it on the button instead of substituting another.
	Ready bool `json:"ready"`
	// Locked is set on the only row while official presentation hides the list.
	Locked bool `json:"locked,omitempty"`
}

// PutView is the progress of one chunked upload on this link.
type PutView struct {
	ID    string `json:"id"`
	Name  string `json:"name,omitempty"`
	Ready bool   `json:"ready"`
	Kind  string `json:"kind,omitempty"`
}

type WatchStatus struct {
	Running          bool            `json:"running,omitempty"`
	TurnID           string          `json:"turn_id,omitempty"`
	AwaitingAnswer   bool            `json:"awaiting_answer,omitempty"`
	AwaitingContinue bool            `json:"awaiting_continue"`
	Waiting          bool            `json:"waiting,omitempty"`
	Wake             *WakeView       `json:"wake,omitempty"`
	Followups        *[]FollowupView `json:"followups,omitempty"`
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
	SourceThreadID string          `json:"source_thread_id,omitempty"`
	SourceTurnID   string          `json:"source_turn_id,omitempty"`
	ThreadID       string          `json:"thread_id"`
	TurnID         string          `json:"turn_id,omitempty"`
	Seq            int64           `json:"seq"`
	Kind           string          `json:"kind"`
	AgentID        string          `json:"agent_id,omitempty"`
	Role           string          `json:"role,omitempty"`
	Text           string          `json:"text"`
	ToolCallID     string          `json:"tool_call_id,omitempty"`
	MessageID      string          `json:"message_id,omitempty"`
	Err            string          `json:"err,omitempty"`
	HasImages      bool            `json:"has_images,omitempty"`
	ImageIDs       []string        `json:"image_ids,omitempty"`
	GeneratedImage *store.ImageRef `json:"generated_image,omitempty"`
	CreatedAt      time.Time       `json:"created_at"`
}

type ProjectView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ThreadView struct {
	ScheduleSource *store.ScheduleSource `json:"schedule_source,omitempty"`
	Unread         bool                  `json:"unread"`
	CompletedSeq   int64                 `json:"completed_seq"`
	ID             string                `json:"id"`
	Title          string                `json:"title"`
	ProjectID      string                `json:"project_id,omitempty"`
	WebExecution   string                `json:"web_execution,omitempty"`
	Running        bool                  `json:"running"`
	Waiting        bool                  `json:"waiting,omitempty"`
	LastActiveAt   time.Time             `json:"last_active_at"`
	Summary        string                `json:"summary,omitempty"`
}

type RunningView struct {
	ScheduleSource *store.ScheduleSource `json:"schedule_source,omitempty"`
	ThreadID       string                `json:"thread_id"`
	Title          string                `json:"title"`
	TurnID         string                `json:"turn_id,omitempty"`
	// Action is a one-line human preview of the live turn: assistant
	// prose or a tool payload field (findings, command, path). Raw
	// tool_call envelopes stay off the wire. Empty when the latest
	// work is bookkeeping (schedule_wake, memory, …) so the phone can
	// localize Waiting / running. A parked wait has no live turn, so it
	// carries the thread's own summary instead of nothing at all.
	Action           string `json:"action,omitempty"`
	AskUser          bool   `json:"ask_user,omitempty"`
	AwaitingContinue bool   `json:"awaiting_continue"`
	Waiting          bool   `json:"waiting,omitempty"`
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
	Unread          bool         `json:"unread"`
	CompletedSeq    int64        `json:"completed_seq"`
	ID              string       `json:"id"`
	ProjectID       string       `json:"project_id,omitempty"`
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
	WebExecution    string       `json:"web_execution,omitempty"`
	Waiting         bool         `json:"waiting,omitempty"`
	Wake            *WakeView    `json:"wake,omitempty"`
	Running         *RunningView `json:"running,omitempty"`
	Turns           []TurnView   `json:"turns,omitempty"`
}

type TurnView struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Text         string `json:"text,omitempty"`
	UserText     string `json:"user_text,omitempty"`
	WebExecution string `json:"web_execution,omitempty"`
}
