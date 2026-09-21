package server_test

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const scheduleWaitPrompt = "Continue the wait."

func TestCreateSchedulesAreHumanCreated(t *testing.T) {
	h := newHarness(t)
	threadID := h.newThread()

	wake := h.json(http.MethodPost, "/api/schedules", map[string]any{
		"kind":      "thread",
		"thread_id": threadID,
		"title":     "wake",
		"prompt":    scheduleWaitPrompt,
		"every_s":   60,
	}, http.StatusCreated)
	row := wake["schedule"].(map[string]any)
	if row["created_by"] != "human" || row["kind"] != "thread" {
		t.Fatalf("wake=%v", wake)
	}
	if row["thread_id"] != threadID {
		t.Fatalf("thread_id=%v", row["thread_id"])
	}

	job := h.json(http.MethodPost, "/api/schedules", map[string]any{
		"kind":       "standalone",
		"title":      "check",
		"prompt":     scheduleWaitPrompt,
		"every_s":    60,
		"created_by": "manager",
	}, http.StatusCreated)
	standalone := job["schedule"].(map[string]any)
	if standalone["created_by"] != "human" || standalone["kind"] != "standalone" {
		t.Fatalf("standalone=%v", job)
	}
	for _, w := range []string{"CI", "deploy", "GitHub"} {
		if strings.Contains(row["prompt"].(string), w) {
			t.Fatalf("leaked %q", w)
		}
	}
}

func TestParkedThreadWakeMarksTheConversationWaiting(t *testing.T) {
	h := newHarness(t)
	threadID := h.newThread()
	idle := h.json(http.MethodGet, "/api/threads/"+threadID, nil, http.StatusOK)
	status, _ := idle["status"].(map[string]any)
	if status["waiting"] == true {
		t.Fatal("a conversation without a wake must not be waiting")
	}
	row, _ := idle["thread"].(map[string]any)
	if row["waiting"] == true {
		t.Fatal("listing thread must omit waiting when nothing is parked")
	}

	h.json(http.MethodPost, "/api/schedules", map[string]any{
		"kind": "thread", "thread_id": threadID,
		"prompt": scheduleWaitPrompt, "every_s": 60,
	}, http.StatusCreated)

	got := h.json(http.MethodGet, "/api/threads/"+threadID, nil, http.StatusOK)
	st, _ := got["status"].(map[string]any)
	if st["waiting"] != true {
		t.Fatalf("status waiting=%v, parked wake must not look idle", st["waiting"])
	}
	if st["running"] == true {
		t.Fatal("arming a wait must not start a turn")
	}
	th, _ := got["thread"].(map[string]any)
	if th["waiting"] != true {
		t.Fatalf("thread waiting=%v", th["waiting"])
	}
	listed := h.json(http.MethodGet, "/api/threads", nil, http.StatusOK)
	rows, _ := listed["threads"].([]any)
	found := false
	for _, raw := range rows {
		item, _ := raw.(map[string]any)
		if item["id"] != threadID {
			continue
		}
		found = true
		if item["waiting"] != true {
			t.Fatalf("list waiting=%v", item["waiting"])
		}
	}
	if !found {
		t.Fatal("created conversation missing from the list")
	}
}

func TestListSchedulesFiltersAndCountsUnread(t *testing.T) {
	h := newHarness(t)
	threadID := h.newThread()
	empty := h.json(http.MethodGet, "/api/schedules", nil, http.StatusOK)
	if empty["unread"] != float64(0) {
		t.Fatalf("empty unread=%v", empty["unread"])
	}
	if rows, _ := empty["schedules"].([]any); len(rows) != 0 {
		t.Fatalf("empty list=%v", empty)
	}

	h.json(http.MethodPost, "/api/schedules", map[string]any{
		"kind": "thread", "thread_id": threadID,
		"prompt": scheduleWaitPrompt, "every_s": 60,
	}, http.StatusCreated)
	paused := h.json(http.MethodPost, "/api/schedules", map[string]any{
		"kind": "standalone", "prompt": scheduleWaitPrompt, "every_s": 60,
	}, http.StatusCreated)
	pid := paused["schedule"].(map[string]any)["id"].(string)
	h.json(http.MethodPatch, "/api/schedules/"+pid, map[string]any{"status": "paused"}, http.StatusOK)

	listed := h.json(http.MethodGet, "/api/schedules", nil, http.StatusOK)
	if rows, _ := listed["schedules"].([]any); len(rows) != 2 {
		t.Fatalf("list=%v", listed)
	}
	byKind := h.json(http.MethodGet, "/api/schedules?kind=thread", nil, http.StatusOK)
	if rows, _ := byKind["schedules"].([]any); len(rows) != 1 {
		t.Fatalf("kind=thread %v", byKind)
	}
	byStatus := h.json(http.MethodGet, "/api/schedules?status=paused", nil, http.StatusOK)
	if rows, _ := byStatus["schedules"].([]any); len(rows) != 1 {
		t.Fatalf("status=paused %v", byStatus)
	}
	if byStatus["unread"] != float64(0) {
		t.Fatalf("unread is a count, not a filter: %v", byStatus["unread"])
	}
}

func TestGetScheduleIncludesRecentRuns(t *testing.T) {
	h := newHarness(t)
	threadID := h.newThread()
	created := h.json(http.MethodPost, "/api/schedules", map[string]any{
		"kind": "thread", "thread_id": threadID,
		"prompt": scheduleWaitPrompt, "every_s": 60,
	}, http.StatusCreated)
	id := created["schedule"].(map[string]any)["id"].(string)
	got := h.json(http.MethodGet, "/api/schedules/"+id, nil, http.StatusOK)
	if got["schedule"].(map[string]any)["id"] != id {
		t.Fatalf("get=%v", got)
	}
	if runs, _ := got["runs"].([]any); runs == nil {
		t.Fatalf("runs must be an array: %v", got)
	}
}

func TestPatchAndDeleteSchedule(t *testing.T) {
	h := newHarness(t)
	threadID := h.newThread()
	created := h.json(http.MethodPost, "/api/schedules", map[string]any{
		"kind": "thread", "thread_id": threadID,
		"title": "wake", "prompt": scheduleWaitPrompt, "every_s": 60,
	}, http.StatusCreated)
	id := created["schedule"].(map[string]any)["id"].(string)

	h.json(http.MethodPatch, "/api/schedules/"+id, "\"nope\"", http.StatusBadRequest)
	paused := h.json(http.MethodPatch, "/api/schedules/"+id,
		map[string]any{"status": "paused"}, http.StatusOK)
	if paused["schedule"].(map[string]any)["status"] != "paused" {
		t.Fatalf("pause=%v", paused)
	}
	resumed := h.json(http.MethodPatch, "/api/schedules/"+id,
		map[string]any{"status": "active", "title": "later", "every_s": 90}, http.StatusOK)
	row := resumed["schedule"].(map[string]any)
	if row["status"] != "active" || row["title"] != "later" || row["every_s"] != float64(90) {
		t.Fatalf("resume+edit=%v", resumed)
	}

	resp := h.do(http.MethodDelete, "/api/schedules/"+id, nil)
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status %d", resp.StatusCode)
	}
	again := h.do(http.MethodDelete, "/api/schedules/"+id, nil)
	again.Body.Close()
	if again.StatusCode != http.StatusNoContent {
		t.Fatalf("second delete status %d, CancelSchedule is a no-op", again.StatusCode)
	}
	got := h.json(http.MethodGet, "/api/schedules/"+id, nil, http.StatusOK)
	if got["schedule"].(map[string]any)["status"] != "cancelled" {
		t.Fatalf("cancelled=%v", got)
	}
}

func TestRunScheduleNowOnIdleStartsAContinueTurn(t *testing.T) {
	h := newHarness(t)
	threadID := h.newThread()
	created := h.json(http.MethodPost, "/api/schedules", map[string]any{
		"kind": "thread", "thread_id": threadID,
		"prompt": scheduleWaitPrompt, "every_s": 60,
	}, http.StatusCreated)
	id := created["schedule"].(map[string]any)["id"].(string)

	got := h.json(http.MethodPost, "/api/schedules/"+id+"/run", nil, http.StatusAccepted)
	turn, _ := got["turn"].(map[string]any)
	if turn["schedule_continue"] != true {
		t.Fatalf("turn=%v", got)
	}
	if turn["thread_id"] != threadID {
		t.Fatalf("thread=%v", turn["thread_id"])
	}
	done := h.waitTurnDone(threadID)
	if !done.ScheduleContinue || done.Status != store.TurnDone {
		t.Fatalf("done=%+v", done)
	}

	// FinishTurn lands before FinishRun; poll the inbox, not the turn row.
	var listed map[string]any
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		listed = h.json(http.MethodGet, "/api/schedules", nil, http.StatusOK)
		if listed["unread"] == float64(1) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if listed["unread"] != float64(1) {
		t.Fatalf("findings must count as unread: %v", listed)
	}
	detail := h.json(http.MethodGet, "/api/schedules/"+id, nil, http.StatusOK)
	runs, _ := detail["runs"].([]any)
	if len(runs) == 0 {
		t.Fatalf("get must include the fire: %v", detail)
	}
	runID := runs[0].(map[string]any)["id"].(string)
	read := h.do(http.MethodPost, "/api/schedules/runs/"+runID+"/read", nil)
	read.Body.Close()
	if read.StatusCode != http.StatusNoContent {
		t.Fatalf("read status %d", read.StatusCode)
	}
	cleared := h.json(http.MethodGet, "/api/schedules", nil, http.StatusOK)
	if cleared["unread"] != float64(0) {
		t.Fatalf("unread after read=%v", cleared["unread"])
	}
}

func TestRunScheduleNowWhileBusyIsSkippedBusy(t *testing.T) {
	provider.SetMockAskUser(true)
	t.Cleanup(func() { provider.SetMockAskUser(false) })

	h := newHarness(t)
	threadID := h.newThread()
	created := h.json(http.MethodPost, "/api/schedules", map[string]any{
		"kind": "thread", "thread_id": threadID,
		"prompt": scheduleWaitPrompt, "every_s": 60,
	}, http.StatusCreated)
	id := created["schedule"].(map[string]any)["id"].(string)

	h.json(http.MethodPost, "/api/threads/"+threadID+"/turns",
		map[string]any{"text": scheduleWaitPrompt}, http.StatusAccepted)
	waitThreadRunning(t, h, threadID)

	got := h.json(http.MethodPost, "/api/schedules/"+id+"/run", nil, http.StatusConflict)
	if got["code"] != "skipped_busy" {
		t.Fatalf("busy run-now: %v", got)
	}
	live := h.json(http.MethodGet, "/api/threads/"+threadID, nil, http.StatusOK)
	if live["status"].(map[string]any)["running"] != true {
		t.Fatalf("the live turn must stay up: %v", live)
	}
}

func TestMissingScheduleIs404(t *testing.T) {
	h := newHarness(t)
	h.json(http.MethodGet, "/api/schedules/sch_missing", nil, http.StatusNotFound)
	h.json(http.MethodPatch, "/api/schedules/sch_missing", map[string]any{"status": "paused"}, http.StatusNotFound)
	h.json(http.MethodDelete, "/api/schedules/sch_missing", nil, http.StatusNotFound)
	h.json(http.MethodPost, "/api/schedules/sch_missing/run", nil, http.StatusNotFound)
	h.json(http.MethodPost, "/api/schedules/runs/srun_missing/read", nil, http.StatusNotFound)
}

func TestScheduleListSurfacesADeadRunsTable(t *testing.T) {
	h := newHarness(t)
	threadID := h.newThread()
	created := h.json(http.MethodPost, "/api/schedules", map[string]any{
		"kind": "thread", "thread_id": threadID,
		"prompt": scheduleWaitPrompt, "every_s": 60,
	}, http.StatusCreated)
	id := created["schedule"].(map[string]any)["id"].(string)
	if err := h.app.Store.DB().Migrator().DropTable(&store.ScheduleRun{}); err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodGet, "/api/schedules", nil, http.StatusBadRequest)
	h.json(http.MethodGet, "/api/schedules/"+id, nil, http.StatusBadRequest)
}

func waitThreadRunning(t *testing.T, h *harness, threadID string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		got := h.json(http.MethodGet, "/api/threads/"+threadID, nil, http.StatusOK)
		if got["status"].(map[string]any)["running"] == true {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("conversation never went busy")
}

func TestUntitledScheduleCreateGetsAGeneratedTitle(t *testing.T) {
	h := newHarness(t)
	created := h.json(http.MethodPost, "/api/schedules", map[string]any{
		"kind": "standalone", "prompt": scheduleWaitPrompt, "every_s": 60,
	}, http.StatusCreated)
	row := created["schedule"].(map[string]any)
	if row["title_auto"] != true {
		t.Fatalf("untitled create must be machine-owned: %v", created)
	}
	planted, _ := row["title"].(string)
	if strings.TrimSpace(planted) == "" {
		t.Fatal("create must plant a placeholder")
	}
	id, _ := row["id"].(string)
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got := h.json(http.MethodGet, "/api/schedules/"+id, nil, http.StatusOK)
		sch := got["schedule"].(map[string]any)
		title, _ := sch["title"].(string)
		if sch["title_auto"] == false && title != "" && title != planted && title != scheduleWaitPrompt {
			for _, w := range []string{"CI", "deploy", "GitHub"} {
				if strings.Contains(title, w) {
					t.Fatalf("leaked %q", w)
				}
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("namer never replaced the placeholder")
}

func TestScheduleCreateRejectsGarbage(t *testing.T) {
	h := newHarness(t)
	resp := h.do(http.MethodPost, "/api/schedules", "\"not an object\"")
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}
