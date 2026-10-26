package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

type largeRestoreReads struct {
	CheckpointReads   int                    `json:"checkpoint_reads"`
	FilteredReads     int                    `json:"filtered_reads"`
	FullReads         int                    `json:"full_reads"`
	ThreadBodyReads   int                    `json:"thread_body_reads"`
	TurnBodyReads     int                    `json:"turn_body_reads"`
	ScannedEvents     int                    `json:"scanned_events"`
	AskRecovered      bool                   `json:"ask_recovered"`
	GenerationStarted bool                   `json:"generation_started"`
	FullReadEvidence  []largeRestoreFullRead `json:"full_read_evidence,omitempty"`
}

type largeRestoreFullRead struct {
	Scope     string   `json:"scope"`
	SQL       string   `json:"sql"`
	Rows      int      `json:"rows"`
	BodyBytes int      `json:"body_bytes"`
	Callers   []string `json:"callers"`
}

type largeRestoreStep struct {
	Phase     string `json:"phase"`
	ThreadID  string `json:"thread_id,omitempty"`
	ElapsedMS int64  `json:"elapsed_ms"`
}

type largeRestoreReceipt struct {
	PID               int                           `json:"pid"`
	Phase             string                        `json:"phase"`
	ContextError      string                        `json:"context_error,omitempty"`
	Threads           map[string]*largeRestoreReads `json:"threads"`
	Steps             []largeRestoreStep            `json:"steps"`
	BarrierThread     string                        `json:"barrier_thread,omitempty"`
	BarrierEntered    bool                          `json:"barrier_entered"`
	BarrierCancelled  bool                          `json:"barrier_cancelled"`
	Frozen            bool                          `json:"frozen"`
	UnattributedReads int                           `json:"unattributed_reads"`
}

type largeRestoreProbe struct {
	mu      sync.Mutex
	dir     string
	started time.Time
	receipt largeRestoreReceipt
	owners  map[string]string
}

// Only the reexec test helper calls this probe. Production has no recovery
// delay, query callback, filesystem control path or public testing switch.
func startLargeRestoreProbe(ctx context.Context, a *app.App) (*largeRestoreProbe, error) {
	dir := os.Getenv("ZWAI_LARGE_RESTORE_PROBE")
	if dir == "" {
		return nil, nil
	}
	if a.Config == nil || a.Config.Search.SemanticEnabled() || a.Config.Remote.Enabled || a.Config.Swarm.AutoTitle || a.Config.Memory.AutoReview {
		return nil, fmt.Errorf("restore probe requires disabled embedding, remote, title and memory-review background work")
	}
	threads, err := a.Store.ListThreads(true, "")
	if err != nil {
		return nil, err
	}
	p := &largeRestoreProbe{dir: dir, started: time.Now(), owners: map[string]string{}, receipt: largeRestoreReceipt{
		PID: os.Getpid(), Phase: "restore_started", Threads: map[string]*largeRestoreReads{},
		BarrierThread: os.Getenv("ZWAI_LARGE_RESTORE_BLOCK_THREAD"),
	}}
	for _, thread := range threads {
		p.receipt.Threads[thread.ID] = &largeRestoreReads{}
		p.owners[thread.ID] = thread.ID
		turns, err := a.Store.ListTurns(thread.ID)
		if err != nil {
			return nil, err
		}
		for _, turn := range turns {
			p.owners[turn.ID] = thread.ID
		}
	}
	if id := p.receipt.BarrierThread; id != "" && p.receipt.Threads[id] == nil {
		return nil, fmt.Errorf("restore probe barrier has no fixture thread: %s", id)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := p.writeLocked("restore_started", ""); err != nil {
		return nil, err
	}
	callbacks := a.Store.DB().Callback()
	read := func(db *gorm.DB) { p.read(ctx, db) }
	if err := callbacks.Query().After("gorm:query").Register("large_restore_reads", read); err != nil {
		return nil, err
	}
	if err := callbacks.Row().After("gorm:row").Register("large_restore_reads", read); err != nil {
		return nil, err
	}
	if err := callbacks.Create().After("gorm:create").Register("large_restore_bootstrap", func(db *gorm.DB) {
		event, ok := db.Statement.Dest.(*store.Event)
		if !ok || db.Error != nil || event.Kind != "generation_started" && event.Kind != "ask_recovered" {
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		if row := p.receipt.Threads[event.ThreadID]; row != nil && !p.receipt.Frozen {
			row.AskRecovered = row.AskRecovered || event.Kind == "ask_recovered"
			row.GenerationStarted = row.GenerationStarted || event.Kind == "generation_started"
			db.AddError(p.writeLocked(event.Kind, event.ThreadID))
		}
	}); err != nil {
		return nil, err
	}
	if err := p.writeLocked("callbacks_installed", ""); err != nil {
		return nil, err
	}
	// Freeze only after the parent has observed the actual resumed model/ask
	// boundaries. Its later HTTP Trace reads must not count as recovery scans.
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := os.Stat(filepath.Join(dir, "freeze-"+strconv.Itoa(os.Getpid()))); err != nil {
					continue
				}
				p.mu.Lock()
				p.receipt.Frozen = true
				err := p.writeLocked("observation_frozen", "")
				p.mu.Unlock()
				if err != nil {
					fmt.Fprintln(os.Stderr, "restore probe freeze receipt:", err)
				}
				return
			}
		}
	}()
	return p, nil
}

func (p *largeRestoreProbe) read(ctx context.Context, db *gorm.DB) {
	if db.Statement.Table != "events" {
		return
	}
	sql := db.Statement.SQL.String()
	checkpoint := false
	for _, value := range db.Statement.Vars {
		if kind, ok := value.(string); ok {
			checkpoint = checkpoint || kind == "engine_restart_checkpoint"
		}
	}
	checkpoint = checkpoint && strings.Contains(sql, "kind =") && strings.Contains(sql, "ORDER BY seq desc")
	filtered := strings.Contains(sql, "AND (kind IN")
	_, events := db.Statement.Dest.(*[]store.Event)
	allFields := len(db.Statement.Selects) == 0
	for _, selection := range db.Statement.Selects {
		allFields = allFields || selection == "*" || selection == "events.*"
	}
	where := ""
	if offset := strings.Index(sql, " WHERE "); offset >= 0 {
		where = sql[offset:]
	}
	full := events && allFields && !checkpoint && !filtered && !strings.Contains(where, "kind")
	if !checkpoint && !filtered && !full {
		return
	}
	p.mu.Lock()
	if p.receipt.Frozen {
		p.mu.Unlock()
		return
	}
	threadID := ""
	for _, value := range db.Statement.Vars {
		if id, ok := value.(string); ok && p.owners[id] != "" {
			threadID = p.owners[id]
			break
		}
	}
	if threadID == "" {
		// Do not silently drop a query the observer cannot attribute, or
		// alter the engine's actual recovery merely to diagnose that query.
		p.receipt.UnattributedReads++
		err := p.writeLocked("unattributed_event_read", "")
		p.mu.Unlock()
		db.AddError(err)
		return
	}
	row := p.receipt.Threads[threadID]
	if checkpoint {
		row.CheckpointReads++
	}
	if filtered {
		row.FilteredReads++
		if events, ok := db.Statement.Dest.(*[]store.Event); ok {
			row.ScannedEvents += len(*events)
		}
	}
	if full {
		row.FullReads++
		scope := "thread"
		if strings.Contains(where, "turn_id") {
			row.TurnBodyReads++
			scope = "turn"
		} else {
			row.ThreadBodyReads++
		}
		if len(row.FullReadEvidence) < 8 {
			evidence := largeRestoreFullRead{Scope: scope, SQL: sql}
			if events, ok := db.Statement.Dest.(*[]store.Event); ok {
				evidence.Rows = len(*events)
				for _, event := range *events {
					evidence.BodyBytes += len(event.Text) + len(event.Err)
				}
			}
			var pcs [32]uintptr
			n := runtime.Callers(2, pcs[:])
			frames := runtime.CallersFrames(pcs[:n])
			for {
				frame, more := frames.Next()
				evidence.Callers = append(evidence.Callers, fmt.Sprintf("%s %s:%d", frame.Function, frame.File, frame.Line))
				if !more {
					break
				}
			}
			row.FullReadEvidence = append(row.FullReadEvidence, evidence)
		}
	}
	block := filtered && p.receipt.BarrierThread == threadID && !p.receipt.BarrierEntered
	if block {
		p.receipt.BarrierEntered = true
	}
	phase := "recovery_read"
	if block {
		phase = "barrier_entered"
	}
	err := p.writeLocked(phase, threadID)
	p.mu.Unlock()
	if err != nil {
		db.AddError(err)
		return
	}
	if block {
		<-ctx.Done()
		p.mu.Lock()
		p.receipt.BarrierCancelled = true
		p.receipt.ContextError = ctx.Err().Error()
		err = p.writeLocked("barrier_cancelled", threadID)
		p.mu.Unlock()
		db.AddError(err)
		db.AddError(ctx.Err())
	}
}

func (p *largeRestoreProbe) finish(err error) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.receipt.Phase = "restore_returned"
	if err != nil {
		p.receipt.ContextError = err.Error()
	}
	return p.writeLocked("restore_returned", "")
}

func (p *largeRestoreProbe) writeLocked(phase, threadID string) error {
	p.receipt.Steps = append(p.receipt.Steps, largeRestoreStep{phase, threadID, time.Since(p.started).Milliseconds()})
	raw, err := json.MarshalIndent(p.receipt, "", "  ")
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(p.dir, ".probe-*")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	_, writeErr := file.Write(raw)
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr != nil {
		return writeErr
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(temporary, filepath.Join(p.dir, strconv.Itoa(os.Getpid())+".json"))
}
