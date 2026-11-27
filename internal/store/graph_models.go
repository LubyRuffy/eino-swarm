package store

import (
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
)

// The graph tables. A graph belongs to an owner: the project when the
// conversation is in one, otherwise the conversation. The owner is frozen
// when a turn starts, so moving a conversation never splits its graph.
//
// Each row records the conversation, turn, agent and generation that wrote it.
// Identity and origin stay fixed; the Updated* fields follow the last writer.

// GraphSubject is something the work is about: an IP, a company, a query.
type GraphSubject struct {
	ID       string           `gorm:"primaryKey;size:64" json:"id"`
	OwnerID  string           `gorm:"not null;index:idx_graph_subject_owner,priority:1;size:64" json:"owner_id"`
	Kind     string           `gorm:"not null;size:32" json:"kind"`
	Name     string           `gorm:"size:600" json:"name"`
	Attrs    map[string]any   `gorm:"serializer:json" json:"attrs"`
	Status   string           `gorm:"not null;size:16" json:"status"`
	Evidence []graph.Evidence `gorm:"serializer:json" json:"evidence"`
	// Credentials names the credential attributes whose values a reader other
	// than the model's by-id read does not receive. Never stored.
	Credentials []string `gorm:"-" json:"credentials,omitempty"`
	// FoundByTodo is the to-do whose settlement first brought this subject in.
	FoundByTodo string `gorm:"size:64;index" json:"found_by_todo,omitempty"`
	Version     int64  `gorm:"not null" json:"version"`

	ThreadID          string    `gorm:"size:64" json:"thread_id"`
	TurnID            string    `gorm:"size:64" json:"turn_id"`
	AgentID           string    `gorm:"size:64" json:"agent_id"`
	Generation        int       `json:"generation"`
	UpdatedThreadID   string    `gorm:"size:64" json:"updated_thread_id"`
	UpdatedTurnID     string    `gorm:"size:64" json:"updated_turn_id"`
	UpdatedBy         string    `gorm:"size:64" json:"updated_by"`
	UpdatedGeneration int       `json:"updated_generation"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `gorm:"index:idx_graph_subject_owner,priority:2" json:"updated_at"`
}

// GraphSubjectKey is one identity key of a subject. The unique index is what
// turns two writes about the same thing into one subject.
type GraphSubjectKey struct {
	ID        uint   `gorm:"primaryKey" json:"-"`
	OwnerID   string `gorm:"not null;uniqueIndex:idx_graph_key_identity,priority:1;size:64" json:"owner_id"`
	Kind      string `gorm:"not null;uniqueIndex:idx_graph_key_identity,priority:2;size:32" json:"kind"`
	KeyName   string `gorm:"not null;uniqueIndex:idx_graph_key_identity,priority:3;size:32" json:"key_name"`
	Value     string `gorm:"not null;uniqueIndex:idx_graph_key_identity,priority:4;size:2200" json:"value"`
	SubjectID string `gorm:"not null;index;size:64" json:"subject_id"`
}

// GraphLink records how one subject led to another. Cycles are allowed.
type GraphLink struct {
	ID          string           `gorm:"primaryKey;size:64" json:"id"`
	OwnerID     string           `gorm:"not null;uniqueIndex:idx_graph_link_identity,priority:1;size:64" json:"owner_id"`
	FromID      string           `gorm:"not null;uniqueIndex:idx_graph_link_identity,priority:2;size:64" json:"from"`
	ToID        string           `gorm:"not null;uniqueIndex:idx_graph_link_identity,priority:3;index;size:64" json:"to"`
	Rel         string           `gorm:"not null;uniqueIndex:idx_graph_link_identity,priority:4;size:64" json:"rel"`
	Attrs       map[string]any   `gorm:"serializer:json" json:"attrs,omitempty"`
	Evidence    []graph.Evidence `gorm:"serializer:json" json:"evidence"`
	FoundByTodo string           `gorm:"size:64;index" json:"found_by_todo,omitempty"`

	ThreadID   string    `gorm:"size:64" json:"thread_id"`
	TurnID     string    `gorm:"size:64" json:"turn_id"`
	AgentID    string    `gorm:"size:64" json:"agent_id"`
	Generation int       `json:"generation"`
	CreatedAt  time.Time `json:"created_at"`
}

// GraphTodo is one piece of work in an ordered list. It is not a node of the
// graph. Position is the priority: dense, zero-based, owned by the list.
type GraphTodo struct {
	ID       string           `gorm:"primaryKey;size:64" json:"id"`
	OwnerID  string           `gorm:"not null;index:idx_graph_todo_owner,priority:1;size:64" json:"owner_id"`
	Title    string           `gorm:"not null;size:1200" json:"title"`
	About    string           `gorm:"size:64;index" json:"about,omitempty"`
	Status   string           `gorm:"not null;size:16" json:"status"`
	Result   string           `json:"result,omitempty"`
	Evidence []graph.Evidence `gorm:"serializer:json" json:"evidence"`
	Position int              `gorm:"not null;index:idx_graph_todo_owner,priority:2" json:"position"`
	Version  int64            `gorm:"not null" json:"version"`
	// SettledAt is set when the to-do became done or failed.
	SettledAt *time.Time `json:"settled_at,omitempty"`

	ThreadID          string    `gorm:"size:64" json:"thread_id"`
	TurnID            string    `gorm:"size:64" json:"turn_id"`
	AgentID           string    `gorm:"size:64" json:"agent_id"`
	Generation        int       `json:"generation"`
	UpdatedThreadID   string    `gorm:"size:64" json:"updated_thread_id"`
	UpdatedTurnID     string    `gorm:"size:64" json:"updated_turn_id"`
	UpdatedBy         string    `gorm:"size:64" json:"updated_by"`
	UpdatedGeneration int       `json:"updated_generation"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

// GraphRevision is one committed batch. Revision numbers count batches per
// owner, so a client can ask whether anything changed with one integer. A
// revision that carries a request key is also the receipt that replays a
// repeated tool call.
type GraphRevision struct {
	ID          uint    `gorm:"primaryKey" json:"-"`
	OwnerID     string  `gorm:"not null;uniqueIndex:idx_graph_revision,priority:1;size:64" json:"owner_id"`
	Revision    int64   `gorm:"not null;uniqueIndex:idx_graph_revision,priority:2" json:"revision"`
	RequestKey  *string `gorm:"uniqueIndex:idx_graph_request_key;size:200" json:"request_key,omitempty"`
	RequestHash string  `gorm:"size:64" json:"request_hash,omitempty"`
	Payload     string  `json:"payload"`
	// ThreadID is the conversation that committed the batch, so omitting a
	// conversation from a backup can drop its batches from a shared graph.
	ThreadID  string    `gorm:"size:64;index" json:"thread_id"`
	CreatedAt time.Time `json:"created_at"`
}

// GraphOwnerID is the owner a conversation's graph belongs to.
func GraphOwnerID(projectID, threadID string) string {
	if projectID != "" {
		return projectID
	}
	return threadID
}
