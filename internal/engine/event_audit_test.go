package engine

import (
	"regexp"
	"strconv"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// auditEventLog checks the stored log every engine test leaves behind
// against the C-003 rules that models/stream states: a seq names one row
// in its conversation, a rewind leaves nothing between its cut and its
// marker, and a stored row never outlives the turn it belongs to. Each test
// is one real interleaving of the engine, so the whole suite replays many of
// them against those rules. A deleted conversation leaves no owned rows.
// engineTurnID matches ids the engine generates. Fixtures that write rows
// under a readable invented turn id never had that turn, so only rows naming
// a real turn can have outlived it.
var engineTurnID = regexp.MustCompile(`^tn_[0-9a-f]{16}$`)

var eventAuditExempt sync.Map

// exemptFromEventAudit marks a test that removes rows behind the store's
// back to inject a fault. Its leftovers are the fault, not an engine write.
func exemptFromEventAudit(t testing.TB) {
	eventAuditExempt.Store(t.Name(), true)
}

func auditEventLog(t testing.TB, st *store.Store) {
	t.Helper()
	if _, exempt := eventAuditExempt.LoadAndDelete(t.Name()); exempt {
		return
	}
	db := st.DB()
	var dups []struct {
		ThreadID string
		Seq      int64
		N        int
	}
	if err := db.Raw(`SELECT thread_id, seq, COUNT(*) AS n FROM events GROUP BY thread_id, seq HAVING COUNT(*) > 1 OR seq <= 0`).Scan(&dups).Error; err != nil {
		t.Logf("event audit skipped: %v", err)
		return
	}
	for _, d := range dups {
		t.Errorf("event audit: thread %s seq %d stored %d times", d.ThreadID, d.Seq, d.N)
	}
	var markers []store.Event
	if err := db.Where("kind = ?", KindRewound).Find(&markers).Error; err != nil {
		t.Logf("event audit skipped: %v", err)
		return
	}
	for _, m := range markers {
		cut, err := strconv.ParseInt(m.Text, 10, 64)
		if err != nil || cut <= 0 || cut >= m.Seq {
			t.Errorf("event audit: rewind marker %d in %s has cut %q", m.Seq, m.ThreadID, m.Text)
			continue
		}
		var inside []store.Event
		if err := db.Where("thread_id = ? AND seq >= ? AND seq < ?", m.ThreadID, cut, m.Seq).Find(&inside).Error; err != nil {
			t.Logf("event audit skipped: %v", err)
			continue
		}
		for _, ev := range inside {
			t.Errorf("event audit: %s seq %d (turn %s) survived the rewind %d..%d in %s", ev.Kind, ev.Seq, ev.TurnID, cut, m.Seq, m.ThreadID)
		}
	}
	var orphans []store.Event
	if err := db.Raw(`SELECT * FROM events e WHERE e.turn_id <> '' AND NOT EXISTS (SELECT 1 FROM turns t WHERE t.id = e.turn_id AND t.thread_id = e.thread_id)`).Scan(&orphans).Error; err != nil {
		t.Logf("event audit skipped: %v", err)
		return
	}
	for _, ev := range orphans {
		if !engineTurnID.MatchString(ev.TurnID) {
			continue
		}
		t.Errorf("event audit: %s seq %d in %s names turn %s, which that conversation does not have", ev.Kind, ev.Seq, ev.ThreadID, ev.TurnID)
	}
	for _, table := range []string{"messages", "llm_calls"} {
		var rows []struct {
			ThreadID string
			TurnID   string
			AgentID  string
		}
		if err := db.Raw(`SELECT DISTINCT thread_id, turn_id, agent_id FROM ` + table + ` x WHERE x.turn_id <> '' AND NOT EXISTS (SELECT 1 FROM turns t WHERE t.id = x.turn_id AND t.thread_id = x.thread_id)`).Scan(&rows).Error; err != nil {
			t.Logf("event audit skipped: %v", err)
			continue
		}
		for _, r := range rows {
			if engineTurnID.MatchString(r.TurnID) {
				t.Errorf("event audit: %s rows by %q in %s name turn %s, which that conversation does not have", table, r.AgentID, r.ThreadID, r.TurnID)
			}
		}
	}
	for _, table := range []string{"events", "messages", "turns", "llm_calls", "attachments", "followups"} {
		var left []struct {
			ThreadID string
			N        int
		}
		if err := db.Raw(`SELECT thread_id, COUNT(*) AS n FROM ` + table + ` x WHERE x.thread_id <> '' AND NOT EXISTS (SELECT 1 FROM threads t WHERE t.id = x.thread_id) GROUP BY thread_id`).Scan(&left).Error; err != nil {
			t.Logf("event audit skipped: %v", err)
			continue
		}
		for _, l := range left {
			t.Errorf("event audit: %d %s rows outlived deleted conversation %s", l.N, table, l.ThreadID)
		}
	}
}
