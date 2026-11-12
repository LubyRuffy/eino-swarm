package backup

import (
	"database/sql"
	"errors"
	"fmt"
)

const (
	FormatVersion = 1
	Extension     = ".zwai-backup"

	KindFull    = "full"
	KindProject = "project"

	ActionReplaceAll     = "replace-all"
	ActionAddProject     = "add-project"
	ActionReplaceProject = "replace-project"
	ActionBlocked        = "blocked"

	generationFile = "data-generation"
	databaseName   = "database.db"
	manifestName   = "manifest.json"
	filesPrefix    = "files/"
)

// ErrInvalid means the archive, the path, or the request cannot be used.
var ErrInvalid = errors.New("backup: invalid backup")

// ErrConflict means an identity in the archive already belongs to other data.
var ErrConflict = errors.New("backup: the backup overlaps data that belongs to another project")

// ErrConfirm means a restore would replace data and the caller did not confirm it.
var ErrConfirm = errors.New("backup: confirmation required")

// Kind is a full data-directory backup or one project.
type Kind string

// Why a conversation was left out of one archive. The values travel in the
// manifest and in the 409 body, so renaming one is a wire change.
const (
	ReasonRunning = "running"
	ReasonAsking  = "asking"
	ReasonWorkers = "workers"
)

// OmittedThread is a conversation that was still running when this archive
// was written. The archive does not contain it. A later full restore of
// this file will not bring it back.
type OmittedThread struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Reason string `json:"reason,omitempty"`
}

// Options selects what Create writes. DB is the engine's open pool when the
// engine is running; a nil DB opens the data directory's database and closes it.
// SkipActive is not read here. The engine turns that choice into OmitThreads
// after it has seen which conversations are actually running.
type Options struct {
	DataDir     string
	DB          *sql.DB
	Kind        Kind
	ProjectID   string
	OmitSecrets bool
	Output      string
	SkipActive  bool
	OmitThreads []OmittedThread
}

// ExternalWorkdir is a project path the archive records and does not copy.
type ExternalWorkdir struct {
	ProjectID string `json:"project_id"`
	Path      string `json:"path"`
}

// Counts are the rows in the archive, so a preview can say what it holds.
type Counts struct {
	Projects  int `json:"projects"`
	Threads   int `json:"threads"`
	Messages  int `json:"messages"`
	Turns     int `json:"turns"`
	Schedules int `json:"schedules"`
	NuwaNodes int `json:"nuwa_nodes"`
}

// Manifest is what a restore trusts. SHA256 covers every file except the
// manifest itself.
type Manifest struct {
	Format           int               `json:"format"`
	Kind             Kind              `json:"kind"`
	CreatedAt        string            `json:"created_at"`
	IncludesSecrets  bool              `json:"includes_secrets"`
	ProjectID        string            `json:"project_id,omitempty"`
	ExternalWorkdirs []ExternalWorkdir `json:"external_workdirs,omitempty"`
	OmittedThreads   []OmittedThread   `json:"omitted_threads,omitempty"`
	Counts           Counts            `json:"counts"`
	SHA256           map[string]string `json:"sha256"`
}

// Conflict is one identity that already belongs to a different owner.
type Conflict struct {
	Table string `json:"table"`
	ID    string `json:"id"`
	Owner string `json:"owner,omitempty"`
}

// Report describes an archive without changing the data directory.
type Report struct {
	Manifest  Manifest   `json:"manifest"`
	Conflicts []Conflict `json:"conflicts"`
	Action    string     `json:"action"`
}

// RestoreOptions chooses add, replace-project, or replace-all.
// DB is the engine's open pool, or nil when this process may open the file.
type RestoreOptions struct {
	DataDir        string
	DB             *sql.DB
	Path           string
	Confirm        bool
	Replace        bool
	ReplaceProject bool
}

type conflictError struct {
	Conflicts []Conflict
}

func (e *conflictError) Error() string {
	return fmt.Sprintf("%s (%d)", ErrConflict.Error(), len(e.Conflicts))
}

func (e *conflictError) Unwrap() error { return ErrConflict }

// ConflictsOf returns the identities a restore refused, or nil when err is
// some other failure. The HTTP layer shows them without importing the type.
func ConflictsOf(err error) []Conflict {
	var conflict *conflictError
	if !errors.As(err, &conflict) || conflict == nil {
		return nil
	}
	return conflict.Conflicts
}

func invalidf(format string, args ...any) error {
	return fmt.Errorf("%w: "+format, append([]any{ErrInvalid}, args...)...)
}

func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}
