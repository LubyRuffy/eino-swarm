package store

import (
	"fmt"
	"strings"
)

// threadOwnedTables hold rows that belong to one conversation and can be
// written by work that outlives it.
var threadOwnedTables = []string{"turns", "events", "messages", "llm_calls", "attachments", "followups"}

const (
	threadDeletedAbort = "zwai: conversation deleted"
	turnRemovedAbort   = "zwai: turn removed"
)

// ensureThreadOwnership refuses an owned row whose conversation is gone.
// DeleteThread removes a conversation's rows in one transaction, but its
// workers, finishing turn and post-turn review can still write after that
// commits; those rows would keep deleted text on disk with nothing left that
// can show or delete them. The check runs inside the INSERT itself, so it is
// atomic with the write without holding a transaction on the store's only
// connection. An empty thread_id names no conversation (a standalone
// schedule's namer records its model call that way) and is allowed.
func (s *Store) ensureThreadOwnership() error {
	for _, table := range threadOwnedTables {
		q := fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS %[1]s_require_thread BEFORE INSERT ON %[1]s
			WHEN NEW.thread_id <> '' AND NOT EXISTS (SELECT 1 FROM threads WHERE id = NEW.thread_id)
			BEGIN SELECT RAISE(ABORT, '%[2]s'); END`, table, threadDeletedAbort)
		if err := s.db.Exec(q).Error; err != nil {
			return fmt.Errorf("store: create %s ownership guard: %w", table, err)
		}
	}
	// A post-turn review keeps calling the model after its turn closed. A
	// rewind deletes the removed turns with their model calls in one
	// transaction; a call finishing afterwards must not name a turn again.
	q := fmt.Sprintf(`CREATE TRIGGER IF NOT EXISTS llm_calls_require_turn BEFORE INSERT ON llm_calls
		WHEN NEW.turn_id <> '' AND NOT EXISTS (SELECT 1 FROM turns WHERE id = NEW.turn_id AND thread_id = NEW.thread_id)
		BEGIN SELECT RAISE(ABORT, '%s'); END`, turnRemovedAbort)
	if err := s.db.Exec(q).Error; err != nil {
		return fmt.Errorf("store: create llm_calls turn guard: %w", err)
	}
	return nil
}

// dropThreadOwnership removes the guards before a schema migration. SQLite
// validates every trigger when a migration renames a rebuilt table into
// place, and a guard naming threads would fail while threads is rebuilt.
func (s *Store) dropThreadOwnership() error {
	guards := []string{"llm_calls_require_turn"}
	for _, table := range threadOwnedTables {
		guards = append(guards, table+"_require_thread")
	}
	for _, guard := range guards {
		if err := s.db.Exec(`DROP TRIGGER IF EXISTS ` + guard).Error; err != nil {
			return fmt.Errorf("store: drop ownership guard %s: %w", guard, err)
		}
	}
	return nil
}

// threadWriteErr reports a write an ownership guard refused as ErrNotFound
// and drops the in-memory counters that write seeded again.
func (s *Store) threadWriteErr(threadID string, err error) error {
	if err == nil || !strings.Contains(err.Error(), threadDeletedAbort) && !strings.Contains(err.Error(), turnRemovedAbort) {
		return err
	}
	s.forgetThreadSeqs(threadID)
	return ErrNotFound
}

// forgetThreadSeqs drops a conversation's in-memory sequence counters.
func (s *Store) forgetThreadSeqs(threadID string) {
	s.mu.Lock()
	delete(s.evtSeq, threadID)
	delete(s.msgSeq, threadID)
	delete(s.turnSeq, threadID)
	delete(s.followupSeq, threadID)
	s.mu.Unlock()
}
