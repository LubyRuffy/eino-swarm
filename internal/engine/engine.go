// Package engine is the runtime that sits between the HTTP layer and the
// swarm library. It owns one live runtime per conversation, turns swarm
// notifications into the persisted event stream the UI replays, and manages
// each conversation's workspace directory.
package engine

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Errors callers are expected to distinguish.
var (
	// ErrBusy means the conversation is already running a turn. The UI steers
	// instead of starting a second one.
	ErrBusy = errors.New("engine: the conversation is already running a turn")
	// ErrIdle means there is nothing running to steer or interrupt.
	ErrIdle = errors.New("engine: the conversation is not running")
	// ErrNotFound means no such conversation.
	ErrNotFound = store.ErrNotFound
)

// Engine is the process-wide runtime.
type Engine struct {
	cfg   *config.Config
	store *store.Store
	pool  *provider.Pool
	log   *slog.Logger

	mu       sync.Mutex
	runtimes map[string]*runtime
	subs     map[string]map[int]*subscriber
	nextSub  int

	// recordMu keeps a stored event's sequence number and its delivery in the
	// same order. See record.
	recordMu sync.Mutex
}

// New builds an engine over an already-open store and provider pool.
func New(cfg *config.Config, st *store.Store, pool *provider.Pool, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.Default()
	}
	return &Engine{
		cfg:      cfg,
		store:    st,
		pool:     pool,
		log:      log,
		runtimes: map[string]*runtime{},
		subs:     map[string]map[int]*subscriber{},
	}
}

// Config exposes the live configuration, which the settings endpoints mutate.
func (e *Engine) Config() *config.Config { return e.cfg }

// Store exposes the persistence layer for read-only endpoints and tracing.
func (e *Engine) Store() *store.Store { return e.store }

// Providers exposes the model pool.
func (e *Engine) Providers() *provider.Pool { return e.pool }

// ---------- conversations ----------

// CreateThread opens a new conversation with its own workspace directory.
func (e *Engine) CreateThread(title, providerID string) (*store.Thread, error) {
	if strings.TrimSpace(providerID) == "" {
		providerID = e.cfg.Models.Default
	}
	if _, err := e.pool.Resolve(providerID); err != nil {
		return nil, err
	}
	th := &store.Thread{Title: strings.TrimSpace(title), ProviderID: providerID}
	if err := e.store.CreateThread(th); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(e.WorkspaceDir(th.ID), 0o700); err != nil {
		return nil, fmt.Errorf("engine: create workspace: %w", err)
	}
	return th, nil
}

// DeleteThread stops any running turn, drops the rows and removes the
// workspace. A conversation the user deleted must not leave its files behind.
func (e *Engine) DeleteThread(id string) error {
	if err := e.Interrupt(id); err != nil && !errors.Is(err, ErrIdle) && !errors.Is(err, ErrNotFound) {
		return err
	}
	e.closeRuntime(id)
	if err := e.store.DeleteThread(id); err != nil {
		return err
	}
	ws := e.WorkspaceDir(id)
	if err := os.RemoveAll(ws); err != nil {
		e.log.Warn("could not remove workspace", "thread", id, "path", ws, "err", err)
	}
	e.dropSubscribers(id)
	return nil
}

// RenameThread sets a conversation's title.
func (e *Engine) RenameThread(id, title string) error {
	return e.store.UpdateThread(id, map[string]any{"title": strings.TrimSpace(title)})
}

// SetThreadProvider switches which model a conversation uses from its next
// turn on.
func (e *Engine) SetThreadProvider(id, providerID string) error {
	if _, err := e.pool.Resolve(providerID); err != nil {
		return err
	}
	return e.store.UpdateThread(id, map[string]any{"provider_id": providerID})
}

// SetThreadArchived hides or restores a conversation in the sidebar.
func (e *Engine) SetThreadArchived(id string, archived bool) error {
	return e.store.UpdateThread(id, map[string]any{"archived": archived})
}

// WorkspaceDir is a conversation's workspace directory.
func (e *Engine) WorkspaceDir(threadID string) string {
	return e.cfg.WorkspaceDir(threadID)
}

// ---------- status ----------

// Status is what the UI needs to render a conversation's header: whether it is
// working, on which turn, and for how long.
type Status struct {
	ThreadID string `json:"thread_id"`
	Running  bool   `json:"running"`
	TurnID   string `json:"turn_id,omitempty"`
	// A pointer because omitempty does nothing for a time.Time: a conversation
	// that has never run would otherwise report the year 1, and a client that
	// believes it computes an elapsed time two thousand years long.
	StartedAt *time.Time `json:"started_at,omitempty"`
	ElapsedMS int64      `json:"elapsed_ms,omitempty"`
	Workers   int        `json:"workers"`
}

// Status reports a conversation's live state.
func (e *Engine) Status(threadID string) Status {
	e.mu.Lock()
	rt := e.runtimes[threadID]
	e.mu.Unlock()
	st := Status{ThreadID: threadID}
	if rt == nil {
		return st
	}
	return rt.status()
}

// Running reports which conversations are mid-turn, so the sidebar can show a
// working indicator without polling each one.
func (e *Engine) Running() []string {
	e.mu.Lock()
	rts := make([]*runtime, 0, len(e.runtimes))
	for _, rt := range e.runtimes {
		rts = append(rts, rt)
	}
	e.mu.Unlock()
	var out []string
	for _, rt := range rts {
		if s := rt.status(); s.Running {
			out = append(out, s.ThreadID)
		}
	}
	return out
}

// Shutdown interrupts every running turn and marks them cancelled, so a
// restarted app does not show conversations frozen mid-answer.
func (e *Engine) Shutdown() {
	e.mu.Lock()
	rts := make([]*runtime, 0, len(e.runtimes))
	for _, rt := range e.runtimes {
		rts = append(rts, rt)
	}
	e.mu.Unlock()
	for _, rt := range rts {
		rt.interrupt()
	}
	for _, rt := range rts {
		rt.waitIdle(5 * time.Second)
		rt.close()
	}
	if n, err := e.store.MarkStaleTurnsCancelled(); err != nil {
		e.log.Warn("could not close stale turns", "err", err)
	} else if n > 0 {
		e.log.Info("closed turns left running by shutdown", "count", n)
	}
}

// ---------- event bus ----------

// Subscription is one client's view of a conversation's live events.
//
// The channel is buffered and lossy by design: a streaming turn emits events
// far faster than a stalled browser tab reads them, and dropping events for a
// slow client is much better than stalling the run for everyone. Dropping a
// delta costs nothing — the complete text follows. Dropping a stored event
// would lose it for good, so those set Lagged instead, and the reader catches
// up from the database.
type Subscription struct {
	C <-chan store.Event

	lagged atomic.Bool
	once   sync.Once
	stop   func()
}

// Lagged reports, and clears, whether stored events were dropped because this
// client was not reading fast enough. A reader that sees it must replay from
// the last sequence number it received.
func (s *Subscription) Lagged() bool { return s.lagged.Swap(false) }

// Close unsubscribes. It is safe to call more than once.
func (s *Subscription) Close() { s.once.Do(s.stop) }

// Subscribe registers a client for this conversation's live events.
func (e *Engine) Subscribe(threadID string) *Subscription {
	ch := make(chan store.Event, subscriberBuffer)
	sub := &Subscription{C: ch}

	e.mu.Lock()
	id := e.nextSub
	e.nextSub++
	if e.subs[threadID] == nil {
		e.subs[threadID] = map[int]*subscriber{}
	}
	e.subs[threadID][id] = &subscriber{ch: ch, sub: sub}
	e.mu.Unlock()

	sub.stop = func() {
		e.mu.Lock()
		if m := e.subs[threadID]; m != nil {
			if s, ok := m[id]; ok {
				delete(m, id)
				close(s.ch)
			}
			if len(m) == 0 {
				delete(e.subs, threadID)
			}
		}
		e.mu.Unlock()
	}
	return sub
}

// subscriber pairs the delivery channel with the handle the reader watches for
// dropped events.
type subscriber struct {
	ch  chan store.Event
	sub *Subscription
}

// subscriberBuffer is deep enough to absorb a burst of streamed deltas while a
// client renders a frame.
const subscriberBuffer = 256

// Replay returns a conversation's persisted events after seq, for a client
// that is catching up.
func (e *Engine) Replay(threadID string, since int64) ([]store.Event, error) {
	return e.store.ListEvents(threadID, since, 0)
}

// broadcast fans one event out to the conversation's subscribers.
func (e *Engine) broadcast(ev store.Event) {
	// The send stays under the same lock that Subscribe/Close use, so a
	// subscriber cannot be closed between being chosen as a target and being
	// sent to — sending on that closed channel would panic. It is safe to hold
	// the lock here because the send is non-blocking: a slow client is flagged
	// and skipped rather than waited on.
	e.mu.Lock()
	defer e.mu.Unlock()
	for _, s := range e.subs[ev.ThreadID] {
		select {
		case s.ch <- ev:
		default:
			// Slow client. A delta is disposable; a stored event is not, so
			// flag the gap and let the reader replay it. See Subscription.
			if ev.Seq > 0 {
				s.sub.lagged.Store(true)
			}
		}
	}
}

func (e *Engine) dropSubscribers(threadID string) {
	e.mu.Lock()
	m := e.subs[threadID]
	delete(e.subs, threadID)
	e.mu.Unlock()
	for _, s := range m {
		close(s.ch)
	}
}

// ---------- runtime registry ----------

func (e *Engine) runtimeFor(threadID string) *runtime {
	e.mu.Lock()
	defer e.mu.Unlock()
	rt, ok := e.runtimes[threadID]
	if !ok {
		rt = newRuntime(e, threadID)
		e.runtimes[threadID] = rt
	}
	return rt
}

func (e *Engine) closeRuntime(threadID string) {
	e.mu.Lock()
	rt := e.runtimes[threadID]
	delete(e.runtimes, threadID)
	e.mu.Unlock()
	if rt != nil {
		rt.close()
	}
}

// ---------- workspace files ----------

// FileEntry describes one file in a conversation's workspace.
type FileEntry struct {
	Path     string    `json:"path"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Dir      bool      `json:"dir"`
	Modified time.Time `json:"modified"`
}

// maxWorkspaceEntries bounds the Files panel listing: an agent that generates
// thousands of files must not be able to hang the UI.
const maxWorkspaceEntries = 2000

// ListFiles walks a conversation's workspace, returning workspace-relative
// paths sorted so directories group with their contents.
func (e *Engine) ListFiles(threadID string) ([]FileEntry, error) {
	root := e.WorkspaceDir(threadID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		return nil, fmt.Errorf("engine: workspace: %w", err)
	}
	var out []FileEntry
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // unreadable entries are skipped, not fatal
		}
		if path == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		if len(out) >= maxWorkspaceEntries {
			return filepath.SkipAll
		}
		entry := FileEntry{Path: filepath.ToSlash(rel), Name: d.Name(), Dir: d.IsDir()}
		if info, statErr := d.Info(); statErr == nil {
			entry.Size = info.Size()
			entry.Modified = info.ModTime()
		}
		out = append(out, entry)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("engine: list workspace: %w", err)
	}
	return out, nil
}

// ResolveWorkspacePath turns a client-supplied relative path into an absolute
// one inside the conversation's workspace, rejecting anything that escapes.
//
// The agents run with full access on purpose, but the HTTP surface must not:
// an unauthenticated download endpoint that accepts `../../../etc/passwd`
// turns a local app into a file server for the whole machine.
func (e *Engine) ResolveWorkspacePath(threadID, rel string) (string, error) {
	root := e.WorkspaceDir(threadID)
	trimmed := strings.TrimSpace(rel)
	// Reject traversal instead of normalizing it away. Cleaning "../../x" to
	// "<workspace>/x" would be safe but silently serve a different file than
	// the client asked for; a plain error is easier to debug and to trust.
	for _, seg := range strings.Split(filepath.ToSlash(trimmed), "/") {
		if seg == ".." {
			return "", fmt.Errorf("engine: %q may not walk out of the conversation workspace", rel)
		}
	}
	clean := filepath.Clean("/" + filepath.FromSlash(trimmed))
	if clean == "/" {
		return "", fmt.Errorf("engine: no path given")
	}
	abs := filepath.Join(root, clean)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", fmt.Errorf("engine: workspace: %w", err)
	}
	absAbs, err := filepath.Abs(abs)
	if err != nil {
		return "", fmt.Errorf("engine: path: %w", err)
	}
	if absAbs != rootAbs && !strings.HasPrefix(absAbs, rootAbs+string(os.PathSeparator)) {
		return "", fmt.Errorf("engine: %q is outside the conversation workspace", rel)
	}
	return absAbs, nil
}

// SaveUpload writes an uploaded file into the conversation's workspace and
// records it, returning the workspace-relative path the agents will see.
func (e *Engine) SaveUpload(threadID, name string, read func(dst string) error) (*store.Attachment, error) {
	if _, err := e.store.GetThread(threadID); err != nil {
		return nil, err
	}
	base := filepath.Base(filepath.FromSlash(name))
	if base == "." || base == string(os.PathSeparator) || strings.TrimSpace(base) == "" {
		return nil, fmt.Errorf("engine: upload has no usable file name")
	}
	uploads := filepath.Join(e.WorkspaceDir(threadID), uploadsDir)
	if err := os.MkdirAll(uploads, 0o700); err != nil {
		return nil, fmt.Errorf("engine: create uploads dir: %w", err)
	}
	dst := uniquePath(filepath.Join(uploads, base))
	if err := read(dst); err != nil {
		return nil, err
	}
	info, err := os.Stat(dst)
	if err != nil {
		return nil, fmt.Errorf("engine: stat upload: %w", err)
	}
	rel, err := filepath.Rel(e.WorkspaceDir(threadID), dst)
	if err != nil {
		return nil, fmt.Errorf("engine: relative upload path: %w", err)
	}
	att := &store.Attachment{
		ThreadID: threadID,
		Name:     filepath.Base(dst),
		RelPath:  filepath.ToSlash(rel),
		Size:     info.Size(),
	}
	if err := e.store.AddAttachment(att); err != nil {
		return nil, err
	}
	_ = e.store.TouchThread(threadID)
	return att, nil
}

// uploadsDir keeps user uploads together so the system prompt can point the
// agents at one place and the Files panel can show provenance.
const uploadsDir = "uploads"

// DeleteFile removes one file from a conversation's workspace.
func (e *Engine) DeleteFile(threadID, rel string) error {
	abs, err := e.ResolveWorkspacePath(threadID, rel)
	if err != nil {
		return err
	}
	if err := os.RemoveAll(abs); err != nil {
		return fmt.Errorf("engine: delete %s: %w", rel, err)
	}
	return e.store.DeleteAttachmentByPath(threadID, filepath.ToSlash(strings.TrimPrefix(rel, "/")))
}

// uniquePath appends a counter until the path is free, so two uploads with the
// same name do not silently overwrite each other.
func uniquePath(path string) string {
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	stem := strings.TrimSuffix(path, ext)
	for i := 2; i < 10000; i++ {
		candidate := fmt.Sprintf("%s-%d%s", stem, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return fmt.Sprintf("%s-%d%s", stem, time.Now().UnixNano(), ext)
}
