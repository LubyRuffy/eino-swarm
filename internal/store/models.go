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

// Thread is one conversation. Its ID also names its workspace directory, so it
// must stay filesystem-safe.
type Thread struct {
	ID           string    `gorm:"primaryKey;size:64" json:"id"`
	Title        string    `gorm:"size:400" json:"title"`
	ProviderID   string    `gorm:"size:64" json:"provider_id"`
	Archived     bool      `json:"archived"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastActiveAt time.Time `gorm:"index" json:"last_active_at"`
}

// Message is one entry of the conversation as the model sees it. This is the
// record replayed into the next turn, which is why tool calls and tool results
// are kept verbatim rather than summarized.
type Message struct {
	ID         uint      `gorm:"primaryKey" json:"id"`
	ThreadID   string    `gorm:"index:idx_msg_thread_seq;size:64" json:"thread_id"`
	TurnID     string    `gorm:"index;size:64" json:"turn_id"`
	Seq        int64     `gorm:"index:idx_msg_thread_seq" json:"seq"`
	Role       string    `gorm:"size:32" json:"role"`
	AgentID    string    `gorm:"size:64" json:"agent_id"`
	Content    string    `json:"content"`
	Reasoning  string    `json:"reasoning,omitempty"`
	ToolCalls  string    `json:"tool_calls,omitempty"` // JSON array, as received
	ToolCallID string    `gorm:"size:128" json:"tool_call_id,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// Turn is one user request and everything the swarm did to answer it. The ID
// is the handle a user pastes into `zwai trace` to replay the whole thing.
type Turn struct {
	ID         string     `gorm:"primaryKey;size:64" json:"id"`
	ThreadID   string     `gorm:"index;size:64" json:"thread_id"`
	Seq        int64      `json:"seq"`
	Status     string     `gorm:"size:32" json:"status"`
	UserText   string     `json:"user_text"`
	Final      string     `json:"final"`
	Error      string     `json:"error,omitempty"`
	ProviderID string     `gorm:"size:64" json:"provider_id"`
	Model      string     `gorm:"size:128" json:"model"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	DurationMS int64      `json:"duration_ms"`
}

// Event is one entry of the UI timeline. Seq is per-thread and gap-free, so a
// reconnecting client asks for everything after the last seq it saw and misses
// nothing.
type Event struct {
	ID         uint      `gorm:"primaryKey" json:"-"`
	ThreadID   string    `gorm:"index:idx_evt_thread_seq;size:64" json:"thread_id"`
	TurnID     string    `gorm:"index;size:64" json:"turn_id"`
	Seq        int64     `gorm:"index:idx_evt_thread_seq" json:"seq"`
	Kind       string    `gorm:"size:32" json:"kind"`
	AgentID    string    `gorm:"size:64" json:"agent_id"`
	Role       string    `gorm:"size:64" json:"role,omitempty"`
	Text       string    `json:"text"`
	ToolCallID string    `gorm:"size:128" json:"tool_call_id,omitempty"`
	Err        string    `json:"err,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// LLMCall records one model request. It carries sizes and timings rather than
// full prompts: enough to explain why a turn was slow or failed, without
// turning the database into a transcript archive.
type LLMCall struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	ThreadID    string    `gorm:"index;size:64" json:"thread_id"`
	TurnID      string    `gorm:"index;size:64" json:"turn_id"`
	AgentID     string    `gorm:"size:64" json:"agent_id"`
	ProviderID  string    `gorm:"size:64" json:"provider_id"`
	Model       string    `gorm:"size:128" json:"model"`
	InputMsgs   int       `json:"input_msgs"`
	InputChars  int       `json:"input_chars"`
	OutputChars int       `json:"output_chars"`
	DurationMS  int64     `json:"duration_ms"`
	Err         string    `json:"err,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
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
