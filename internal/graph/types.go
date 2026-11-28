package graph

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrInvalid wraps a refusal the model can repair by changing its request.
var ErrInvalid = errors.New("graph: invalid request")

// ErrConflict means a version no longer matches, or a replayed tool call
// carries a different body. The model must re-read before it retries.
var ErrConflict = errors.New("graph: version conflict")

// AmbiguousError reports a write that matched several existing subjects. The
// host does not merge subjects on its own; the model decides.
type AmbiguousError struct{ IDs []string }

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("graph: this write matches %d existing subjects (%s); write to one of them by id", len(e.IDs), strings.Join(e.IDs, ", "))
}

// Is lets errors.Is(err, ErrInvalid) classify it as a repairable request.
func (e *AmbiguousError) Is(target error) bool { return target == ErrInvalid }

// Subject statuses.
const (
	StatusLead      = "lead"
	StatusConfirmed = "confirmed"
	StatusRetracted = "retracted"
)

// To-do statuses.
const (
	TodoOpen   = "open"
	TodoDoing  = "doing"
	TodoDone   = "done"
	TodoFailed = "failed"
)

// Limits that keep one row and one batch bounded. The batch size itself is
// bounded by the tool argument budget, not here.
const (
	MaxTitle        = 300
	MaxResult       = 4000
	MaxRel          = 64
	MaxLinkAttrs    = 16
	MaxLinkAttrSize = 2048
	MaxEvidence     = 32
	MaxOwnToolCalls = 16
	MaxEvidenceURL  = 2048
	MaxEvidenceNote = 200
)

// Evidence points at something that can be checked: an event of this scope,
// or an external address. The host validates identity, never meaning.
type Evidence struct {
	ThreadID string `json:"thread_id,omitempty"`
	TurnID   string `json:"turn_id,omitempty"`
	Seq      int64  `json:"seq,omitempty"`
	URL      string `json:"url,omitempty"`
	Label    string `json:"label,omitempty"`
	// Unavailable is set on read when the event no longer exists.
	Unavailable bool `json:"unavailable,omitempty"`
}

// Key is the identity of an evidence entry for deduplication.
func (e Evidence) Key() string {
	if e.URL != "" {
		return "u:" + e.URL
	}
	return fmt.Sprintf("e:%s:%d", e.ThreadID, e.Seq)
}

// ValidateShape checks the parts that need no storage.
func (e *Evidence) ValidateShape() error {
	e.Label = strings.TrimSpace(e.Label)
	if len([]rune(e.Label)) > MaxEvidenceNote {
		return invalid("evidence label is longer than %d characters", MaxEvidenceNote)
	}
	if e.URL != "" {
		if e.ThreadID != "" || e.Seq != 0 {
			return invalid("evidence is either an event or an address, not both")
		}
		u, err := url.Parse(strings.TrimSpace(e.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || len(e.URL) > MaxEvidenceURL {
			return invalid("evidence address must be an http or https URL")
		}
		e.URL = u.String()
		return nil
	}
	if e.ThreadID == "" || e.Seq <= 0 {
		return invalid("evidence requires thread_id and a positive event seq, or a url")
	}
	return nil
}

// SubjectInput is one subject the model reports. Ref names it inside the
// batch so links and to-dos can point at a subject that has no id yet.
type SubjectInput struct {
	Ref     string         `json:"ref,omitempty"`
	ID      string         `json:"id,omitempty"`
	Version int64          `json:"version,omitempty"`
	Kind    string         `json:"kind,omitempty"`
	Attrs   map[string]any `json:"attrs,omitempty"`
	Status  string         `json:"status,omitempty"`
	// HostRetract marks a lead the engine is storing retracted because this
	// call links it to a confirmed subject of the same kind. The model cannot
	// set it. It is not a versioned attribute overwrite.
	HostRetract bool `json:"-"`
}

// TodoInput is one piece of work. About is a subject id or a ref of this
// batch; Before is a to-do id or ref the item should be placed in front of.
type TodoInput struct {
	Ref     string `json:"ref,omitempty"`
	ID      string `json:"id,omitempty"`
	Version int64  `json:"version,omitempty"`
	Title   string `json:"title,omitempty"`
	About   string `json:"about,omitempty"`
	Status  string `json:"status,omitempty"`
	Result  string `json:"result,omitempty"`
	Before  string `json:"before,omitempty"`
}

// LinkInput records how one subject led to another.
type LinkInput struct {
	From  string         `json:"from"`
	To    string         `json:"to"`
	Rel   string         `json:"rel"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// Batch is everything one update_graph call carries.
type Batch struct {
	Todos    []TodoInput    `json:"todos,omitempty"`
	Subjects []SubjectInput `json:"subjects,omitempty"`
	Links    []LinkInput    `json:"links,omitempty"`
	Evidence []Evidence     `json:"evidence,omitempty"`
	// OwnToolCallIDs cites this agent's own completed tool calls. nil keeps the
	// literal Evidence; an empty list clears it.
	OwnToolCallIDs []string `json:"own_tool_call_ids,omitempty"`
	// CiteOwnResults asks the host to cite this caller's own completed tool
	// results. The model cannot set it.
	CiteOwnResults bool `json:"-"`
}

// Empty reports whether the batch writes nothing.
func (b Batch) Empty() bool { return len(b.Todos)+len(b.Subjects)+len(b.Links) == 0 }

// Valid checks the parts of a batch that need no storage.
func (b Batch) Valid() error {
	if b.Empty() {
		return invalid("provide at least one to-do, subject or link")
	}
	if len(b.Evidence) > MaxEvidence {
		return invalid("evidence lists at most %d entries", MaxEvidence)
	}
	for i := range b.Evidence {
		if err := b.Evidence[i].ValidateShape(); err != nil {
			return err
		}
	}
	if len(b.Evidence) > 0 && b.OwnToolCallIDs != nil {
		return invalid("use evidence or own_tool_call_ids, not both")
	}
	if len(b.OwnToolCallIDs) > MaxOwnToolCalls {
		return invalid("own_tool_call_ids lists at most %d calls", MaxOwnToolCalls)
	}
	return nil
}

// Result is the committed outcome of one batch. It is stored with the
// revision so a replayed tool call returns the same answer.
type Result struct {
	Revision int64           `json:"revision"`
	Subjects []SubjectResult `json:"subjects,omitempty"`
	Todos    []TodoResult    `json:"todos,omitempty"`
	Links    []LinkResult    `json:"links,omitempty"`
}

// SubjectResult tells the model the id and version to use next time.
type SubjectResult struct {
	Ref     string `json:"ref,omitempty"`
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Version int64  `json:"version"`
	Created bool   `json:"created,omitempty"`
	// Ignored lists attributes or a status that were not applied because they
	// would overwrite existing values without the current version.
	Ignored []string `json:"ignored,omitempty"`
}

// TodoResult carries a to-do's id, version and place.
type TodoResult struct {
	Ref      string `json:"ref,omitempty"`
	ID       string `json:"id"`
	Version  int64  `json:"version"`
	Status   string `json:"status"`
	Position int    `json:"position"`
	Created  bool   `json:"created,omitempty"`
}

// LinkResult identifies a stored link.
type LinkResult struct {
	ID      string `json:"id"`
	From    string `json:"from"`
	To      string `json:"to"`
	Rel     string `json:"rel"`
	Created bool   `json:"created,omitempty"`
}
