package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ErrNotFound is returned instead of gorm's sentinel so callers do not have to
// import gorm to tell "missing" from "broken".
var ErrNotFound = errors.New("store: not found")

// Store owns the SQLite database.
//
// Sequence numbers are handed out from an in-memory counter per thread, seeded
// from the table on first use. One process owns the database file (this is a
// desktop app), so that is both correct and far cheaper than a MAX(seq) query
// per event — and events arrive once per streamed token.
type Store struct {
	db *gorm.DB

	mu          sync.Mutex
	evtSeq      map[string]int64
	msgSeq      map[string]int64
	turnSeq     map[string]int64
	followupSeq map[string]int64
	inMemory    bool
}

// Open opens (creating if needed) the database at path and migrates it. A path
// of ":memory:" opens a private in-memory database, which is what tests use.
func Open(path string) (*Store, error) {
	dsn := path
	inMemory := path == ":memory:"
	if inMemory {
		dsn = "file::memory:?cache=shared"
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, fmt.Errorf("store: create db dir: %w", err)
		}
		// WAL keeps the UI's reads from blocking on a write-heavy streaming
		// turn; busy_timeout absorbs the brief overlap when one does.
		dsn = path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	}

	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger:                 logger.Default.LogMode(logger.Silent),
		SkipDefaultTransaction: true,
	})
	if err != nil {
		return nil, fmt.Errorf("store: open %s: %w", path, err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("store: sql db: %w", err)
	}
	// One connection. SQLite serializes writers; a GORM pool of N turns a
	// turn still flushing events into SQLITE_BUSY on rewind/truncate. This
	// process is the only client of the file.
	sqlDB.SetMaxOpenConns(1)
	sqlDB.SetMaxIdleConns(1)
	s := &Store{
		db:          db,
		evtSeq:      map[string]int64{},
		msgSeq:      map[string]int64{},
		turnSeq:     map[string]int64{},
		followupSeq: map[string]int64{},
		inMemory:    inMemory,
	}
	if err := db.AutoMigrate(&Project{}, &Thread{}, &Message{}, &Turn{}, &Event{}, &LLMCall{}, &Attachment{}, &Followup{}, &Schedule{}, &ScheduleRun{}); err != nil {
		return nil, fmt.Errorf("store: migrate: %w", err)
	}
	return s, nil
}

// DB exposes the gorm handle for callers that need a query this API does not
// cover (the trace command, mainly).
func (s *Store) DB() *gorm.DB { return s.db }

// Close releases the underlying connection.
func (s *Store) Close() error {
	sqlDB, err := s.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// nextSeq hands out the next sequence number for key in counters, seeding the
// counter from the database the first time a key is seen.
func (s *Store) nextSeq(counters map[string]int64, key string, seed func() int64) int64 {
	s.mu.Lock()
	cur, ok := counters[key]
	if !ok {
		s.mu.Unlock()
		cur = seed()
		s.mu.Lock()
		if existing, raced := counters[key]; raced {
			cur = existing
		}
	}
	cur++
	counters[key] = cur
	s.mu.Unlock()
	return cur
}

func (s *Store) maxSeq(model any, column, threadID string) int64 {
	var max *int64
	s.db.Model(model).Where("thread_id = ?", threadID).Select("MAX(" + column + ")").Scan(&max)
	if max == nil {
		return 0
	}
	return *max
}

// ---------- threads ----------

// CreateThread inserts a new conversation, filling in the id and timestamps.
func (s *Store) CreateThread(t *Thread) error {
	if t.ID == "" {
		t.ID = NewID("th_")
	}
	now := time.Now().UTC()
	t.CreatedAt, t.UpdatedAt, t.LastActiveAt = now, now, now
	if err := s.db.Create(t).Error; err != nil {
		return fmt.Errorf("store: create thread: %w", err)
	}
	return s.bumpProject(t.ProjectID)
}

// GetThread loads one conversation.
func (s *Store) GetThread(id string) (*Thread, error) {
	var t Thread
	err := s.db.First(&t, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get thread: %w", err)
	}
	return &t, nil
}

// ListThreads returns conversations in sidebar order. Rank 0 is never
// dragged: those rows interleave by last activity with ranked rows, so a
// stale unranked conversation cannot sit above one that just ran.
func (s *Store) ListThreads(includeArchived bool, projectID string) ([]Thread, error) {
	q := s.db.Order("sort_rank asc, last_active_at desc")
	if !includeArchived {
		q = q.Where("archived = ?", false)
	}
	if projectID = strings.TrimSpace(projectID); projectID != "" {
		q = q.Where("project_id = ?", projectID)
	}
	var out []Thread
	if err := q.Find(&out).Error; err != nil {
		return nil, fmt.Errorf("store: list threads: %w", err)
	}
	return interleaveByTime(out, func(th Thread) int { return th.SortRank }, func(th Thread) time.Time { return th.LastActiveAt }), nil
}

// UpdateThread applies a field patch to one conversation and refreshes
// UpdatedAt. Unknown ids report ErrNotFound rather than silently doing nothing.
func (s *Store) UpdateThread(id string, fields map[string]any) error {
	if len(fields) == 0 {
		return nil
	}
	fields["updated_at"] = time.Now().UTC()
	res := s.db.Model(&Thread{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return fmt.Errorf("store: update thread: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// ApplyAutoTitle sets the title only while the conversation is still
// machine-named. A user rename in between is a no-op (ok=false), not an
// error: the namer lost the race and must not clobber the sidebar.
func (s *Store) ApplyAutoTitle(id, title string) (bool, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return false, nil
	}
	fields := map[string]any{
		"title":      title,
		"title_auto": false,
		"updated_at": time.Now().UTC(),
	}
	res := s.db.Model(&Thread{}).Where("id = ? AND title_auto = ?", id, true).Updates(fields)
	if res.Error != nil {
		return false, fmt.Errorf("store: apply auto title: %w", res.Error)
	}
	return res.RowsAffected > 0, nil
}

// TouchThread marks a conversation as just active so it sorts to the top of
// the auto-ordered rows. A project it belongs to is bumped the same way, so
// the project list follows last use rather than creation.
func (s *Store) TouchThread(id string) error {
	now := time.Now().UTC()
	if err := s.UpdateThread(id, map[string]any{"last_active_at": now}); err != nil {
		return err
	}
	th, err := s.GetThread(id)
	if err != nil {
		return err
	}
	return s.bumpProject(th.ProjectID)
}

// DeleteThread removes a conversation and everything attached to it. Wakes
// that targeted it are cancelled; standalone jobs that only originated here
// stay. The workspace directory is the caller's to delete: the store owns
// rows, not files.
func (s *Store) DeleteThread(id string) error {
	if _, err := s.GetThread(id); err != nil {
		return err
	}
	err := s.db.Transaction(func(tx *gorm.DB) error {
		if err := cancelWakesForThread(tx, id); err != nil {
			return err
		}
		for _, m := range []any{&Message{}, &Turn{}, &Event{}, &LLMCall{}, &Attachment{}, &Followup{}} {
			if err := tx.Where("thread_id = ?", id).Delete(m).Error; err != nil {
				return err
			}
		}
		return tx.Where("id = ?", id).Delete(&Thread{}).Error
	})
	if err != nil {
		return fmt.Errorf("store: delete thread: %w", err)
	}
	s.mu.Lock()
	delete(s.evtSeq, id)
	delete(s.msgSeq, id)
	delete(s.turnSeq, id)
	delete(s.followupSeq, id)
	s.mu.Unlock()
	return nil
}

// ---------- messages ----------

// AppendMessages stores transcript entries in order, assigning sequence
// numbers so a later read reproduces exactly this ordering.
func (s *Store) AppendMessages(threadID, turnID string, msgs []Message) error {
	if len(msgs) == 0 {
		return nil
	}
	now := time.Now().UTC()
	for i := range msgs {
		msgs[i].ThreadID = threadID
		msgs[i].TurnID = turnID
		msgs[i].CreatedAt = now
		msgs[i].Seq = s.nextSeq(s.msgSeq, threadID, func() int64 {
			return s.maxSeq(&Message{}, "seq", threadID)
		})
	}
	if err := s.db.Create(&msgs).Error; err != nil {
		return fmt.Errorf("store: append messages: %w", err)
	}
	return nil
}

// DeleteMessageByEventSeq drops the one transcript row tagged with that
// timeline seq. Retracting unread steering uses it so replay does not keep
// a caption the manager was told to forget.
func (s *Store) DeleteMessageByEventSeq(threadID string, eventSeq int64) error {
	if eventSeq <= 0 {
		return nil
	}
	res := s.db.Where("thread_id = ? AND event_seq = ?", threadID, eventSeq).Delete(&Message{})
	if res.Error != nil {
		return fmt.Errorf("store: delete message by event seq: %w", res.Error)
	}
	return nil
}

// DeleteSteerMessage drops the replay row for a retracted steer. Newer
// rows are tagged with event_seq; pre-upgrade rows have event_seq 0 and
// match on the [steer] caption instead.
func (s *Store) DeleteSteerMessage(threadID string, eventSeq int64, caption string) error {
	if err := s.DeleteMessageByEventSeq(threadID, eventSeq); err != nil {
		return err
	}
	cap := strings.TrimSpace(caption)
	if cap == "" {
		return nil
	}
	if !strings.HasPrefix(cap, "[steer]") {
		cap = "[steer] " + cap
	}
	res := s.db.Where("thread_id = ? AND event_seq = 0 AND role = ? AND content = ?",
		threadID, "user", cap).Delete(&Message{})
	if res.Error != nil {
		return fmt.Errorf("store: delete steer message: %w", res.Error)
	}
	return nil
}

// ListMessages returns a conversation's transcript in order.
func (s *Store) ListMessages(threadID string) ([]Message, error) {
	var out []Message
	if err := s.db.Where("thread_id = ?", threadID).Order("seq asc").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("store: list messages: %w", err)
	}
	return out, nil
}

// ---------- turns ----------

// CreateTurn opens a new turn in the running state.
func (s *Store) CreateTurn(t *Turn) error {
	if t.ID == "" {
		t.ID = NewID("tn_")
	}
	if t.Status == "" {
		t.Status = TurnRunning
	}
	t.StartedAt = time.Now().UTC()
	t.Seq = s.nextSeq(s.turnSeq, t.ThreadID, func() int64 {
		return s.maxSeq(&Turn{}, "seq", t.ThreadID)
	})
	if err := s.db.Create(t).Error; err != nil {
		return fmt.Errorf("store: create turn: %w", err)
	}
	return nil
}

// FinishTurn closes a turn, recording how it ended and how long it took.
func (s *Store) FinishTurn(id, status, final, errText string) error {
	t, err := s.GetTurn(id)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	return s.applyTurnUpdate(id, map[string]any{
		"status":      status,
		"final":       final,
		"error":       errText,
		"ended_at":    now,
		"duration_ms": now.Sub(t.StartedAt).Milliseconds(),
	})
}

func (s *Store) applyTurnUpdate(id string, fields map[string]any) error {
	res := s.db.Model(&Turn{}).Where("id = ?", id).Updates(fields)
	if res.Error != nil {
		return fmt.Errorf("store: update turn: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

// GetTurn loads one turn.
func (s *Store) GetTurn(id string) (*Turn, error) {
	var t Turn
	err := s.db.First(&t, "id = ?", id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get turn: %w", err)
	}
	return &t, nil
}

// ListTurns returns a conversation's turns in order.
func (s *Store) ListTurns(threadID string) ([]Turn, error) {
	var out []Turn
	if err := s.db.Where("thread_id = ?", threadID).Order("seq asc").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("store: list turns: %w", err)
	}
	return out, nil
}

// ListRunningTurns returns every turn still marked running, oldest first.
// A previous process that died leaves these behind; startup resumes them.
func (s *Store) ListRunningTurns() ([]Turn, error) {
	var out []Turn
	if err := s.db.Where("status = ?", TurnRunning).Order("started_at asc").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("store: list running turns: %w", err)
	}
	return out, nil
}

// MarkStaleTurnsCancelled bulk-cancels every running turn. Startup no longer
// calls this — it resumes those turns — but the method stays for tests and
// for a deliberate wipe.
func (s *Store) MarkStaleTurnsCancelled() (int64, error) {
	now := time.Now().UTC()
	res := s.db.Model(&Turn{}).Where("status = ?", TurnRunning).Updates(map[string]any{
		"status":   TurnCancelled,
		"error":    "interrupted by shutdown",
		"ended_at": now,
	})
	if res.Error != nil {
		return 0, fmt.Errorf("store: close stale turns: %w", res.Error)
	}
	return res.RowsAffected, nil
}

// ---------- events ----------

// AppendEvent stores one timeline event, assigning its per-thread sequence
// number. The assigned Seq is written back into e.
func (s *Store) AppendEvent(e *Event) error {
	e.Seq = s.nextSeq(s.evtSeq, e.ThreadID, func() int64 {
		return s.maxSeq(&Event{}, "seq", e.ThreadID)
	})
	e.CreatedAt = time.Now().UTC()
	if err := s.db.Create(e).Error; err != nil {
		return fmt.Errorf("store: append event: %w", err)
	}
	return nil
}

// ListEvents returns a thread's events with Seq greater than since, oldest
// first. limit <= 0 means no limit. This is the replay a reconnecting SSE
// client asks for.
func (s *Store) ListEvents(threadID string, since int64, limit int) ([]Event, error) {
	q := s.db.Where("thread_id = ? AND seq > ?", threadID, since).Order("seq asc")
	if limit > 0 {
		q = q.Limit(limit)
	}
	var out []Event
	if err := q.Find(&out).Error; err != nil {
		return nil, fmt.Errorf("store: list events: %w", err)
	}
	return out, nil
}

// ListTailEvents returns the newest events before `before` (before <= 0 means
// from the end), oldest first. hasMore is true when older rows still exist —
// a reconnecting UI asks for a viewport of the live edge, then pages up.
func (s *Store) ListTailEvents(threadID string, before int64, limit int) ([]Event, bool, error) {
	if limit <= 0 {
		return []Event{}, false, nil
	}
	q := s.db.Where("thread_id = ?", threadID)
	if before > 0 {
		q = q.Where("seq < ?", before)
	}
	var newest []Event
	if err := q.Order("seq desc").Limit(limit + 1).Find(&newest).Error; err != nil {
		return nil, false, fmt.Errorf("store: list tail events: %w", err)
	}
	hasMore := len(newest) > limit
	if hasMore {
		newest = newest[:limit]
	}
	out := make([]Event, len(newest))
	for i, ev := range newest {
		out[len(newest)-1-i] = ev
	}
	return out, hasMore, nil
}

// rosterEventKinds reconstruct the Agents tab. The live-edge log page is a
// viewport of recent tools; these kinds otherwise fall out of that window
// and the panel claims there are no sub-agents.
var rosterEventKinds = []string{"spawned", "finished", "cleanup"}

// ListRosterEvents returns spawned, finished and cleanup rows, oldest first.
// Opening a long conversation paints one viewport of tools; the roster is
// these few rows, not that window.
func (s *Store) ListRosterEvents(threadID string) ([]Event, error) {
	var out []Event
	err := s.db.Where("thread_id = ? AND kind IN ?", threadID, rosterEventKinds).
		Order("seq asc").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("store: list roster events: %w", err)
	}
	return out, nil
}

// ListAgentEvents returns one worker's stored rows, oldest first. The live-edge
// log page is a viewport of recent tools; opening a worker whose spawn fell
// out of that window needs these rows, not the whole conversation.
func (s *Store) ListAgentEvents(threadID, agentID string) ([]Event, error) {
	if agentID == "" {
		return nil, nil
	}
	var out []Event
	err := s.db.Where("thread_id = ? AND agent_id = ?", threadID, agentID).
		Order("seq asc").Find(&out).Error
	if err != nil {
		return nil, fmt.Errorf("store: list agent events: %w", err)
	}
	return out, nil
}

// ListTurnEvents returns one turn's events, oldest first — the trace view.
func (s *Store) ListTurnEvents(turnID string) ([]Event, error) {
	var out []Event
	if err := s.db.Where("turn_id = ?", turnID).Order("seq asc").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("store: list turn events: %w", err)
	}
	return out, nil
}

// ---------- llm calls ----------

// AppendLLMCall records one model request.
func (s *Store) AppendLLMCall(c *LLMCall) error {
	c.CreatedAt = time.Now().UTC()
	if err := s.db.Create(c).Error; err != nil {
		return fmt.Errorf("store: append llm call: %w", err)
	}
	return nil
}

// ListLLMCalls returns one turn's model requests, oldest first.
func (s *Store) ListLLMCalls(turnID string) ([]LLMCall, error) {
	var out []LLMCall
	if err := s.db.Where("turn_id = ?", turnID).Order("id asc").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("store: list llm calls: %w", err)
	}
	return out, nil
}

// ---------- attachments ----------

// AddAttachment records an uploaded file.
func (s *Store) AddAttachment(a *Attachment) error {
	a.CreatedAt = time.Now().UTC()
	if err := s.db.Create(a).Error; err != nil {
		return fmt.Errorf("store: add attachment: %w", err)
	}
	return nil
}

// ListAttachments returns a conversation's uploads, newest first.
func (s *Store) ListAttachments(threadID string) ([]Attachment, error) {
	var out []Attachment
	if err := s.db.Where("thread_id = ?", threadID).Order("id desc").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("store: list attachments: %w", err)
	}
	return out, nil
}

// DeleteAttachmentByPath drops the record for a workspace-relative path.
func (s *Store) DeleteAttachmentByPath(threadID, relPath string) error {
	if err := s.db.Where("thread_id = ? AND rel_path = ?", threadID, relPath).
		Delete(&Attachment{}).Error; err != nil {
		return fmt.Errorf("store: delete attachment: %w", err)
	}
	return nil
}

// BindAttachmentTurn records that a previously uploaded file belongs to this
// turn, so a later send can tell this request's files from leftovers.
func (s *Store) BindAttachmentTurn(threadID, relPath, turnID string) error {
	res := s.db.Model(&Attachment{}).
		Where("thread_id = ? AND rel_path = ?", threadID, relPath).
		Update("turn_id", turnID)
	if res.Error != nil {
		return fmt.Errorf("store: bind attachment: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("store: bind attachment: no such upload")
	}
	return nil
}
