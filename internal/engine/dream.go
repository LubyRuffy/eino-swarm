package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type dreamEventRef struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id"`
	Seq      int64  `json:"seq"`
}

type dreamReceipt struct {
	Status        string          `json:"status"`
	InvalidReason string          `json:"invalid_reason,omitempty"`
	PatchID       uint            `json:"patch_event_id"`
	Patch         dreamEventRef   `json:"patch"`
	Skill         string          `json:"skill"`
	InputSHA256   string          `json:"input_sha256"`
	Sources       []dreamEventRef `json:"sources"`
	Selected      []dreamEventRef `json:"selected_sources,omitempty"`
	DiscoveryUS   int64           `json:"discovery_us"`
	ApplyUS       int64           `json:"apply_us"`
}

type dreamPromptSource struct {
	Command  string `json:"command"`
	Workdir  string `json:"workdir"`
	ExitCode *int   `json:"exit_code"`
	Pipefail bool   `json:"pipefail,omitempty"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr,omitempty"`
}

type dreamReview struct {
	candidate *dreamCandidate
	prompt    string
	ids       []string
	receipt   dreamReceipt
	offered   atomic.Bool
}

const dreamAuditHeader = "\n\nDream revision audit:\n"

// dreamInput projects only the process facts. The complete tool pair remains
// in history for revalidation. Paths identifying the working directories are
// replaced with per-input labels so scope stays comparable without sending a
// local home path. Clipping a result could hide a scope condition.
func dreamInput(c *dreamCandidate, visible map[int64]store.Event) (string, string, []string) {
	complete := func(ev store.Event) bool {
		got, ok := visible[ev.Seq]
		return ok && got.ID == ev.ID && got.ThreadID == ev.ThreadID && got.TurnID == ev.TurnID &&
			got.Kind == ev.Kind && got.Text == strings.TrimSpace(ev.Text)
	}
	if !complete(c.Read.Call) || !complete(c.Read.Result) {
		return "", "", nil
	}
	input := struct {
		Before   string              `json:"before"`
		After    string              `json:"after"`
		Original []dreamPromptSource `json:"original_observations"`
		Later    []dreamPromptSource `json:"later_observations"`
	}{Before: *c.Change.Before, After: *c.Change.After}
	dirs := map[string]string{}
	type processKey struct {
		command, workdir, stdout, stderr string
		code                             int
		pipefail                         bool
	}
	project := func(group []dreamSource) ([]dreamPromptSource, bool) {
		out := make([]dreamPromptSource, 0, len(group))
		observed := make(map[processKey]bool, len(group))
		for _, source := range group {
			var observation struct {
				dreamPromptSource
				FullCommand string `json:"full_command"`
			}
			if err := json.Unmarshal([]byte(source.Result.Text), &observation); err != nil || observation.Workdir == "" || observation.ExitCode == nil || observation.FullCommand == "" {
				return nil, false
			}
			_, args := dreamCall(source.Call.Text)
			var invocation struct{ Pipefail bool }
			if err := json.Unmarshal([]byte(args), &invocation); err != nil {
				return nil, false
			}
			observation.Pipefail = invocation.Pipefail
			key := processKey{observation.FullCommand, observation.Workdir, observation.Stdout, observation.Stderr, *observation.ExitCode, observation.Pipefail}
			if observed[key] {
				return nil, false
			}
			observed[key] = true
			dir := observation.Workdir
			if dirs[dir] == "" {
				dirs[dir] = strconv.Itoa(len(dirs) + 1)
			}
			observation.Workdir, observation.Command = dirs[dir], observation.FullCommand
			out = append(out, observation.dreamPromptSource)
		}
		return out, true
	}
	var ok bool
	if input.Original, ok = project(c.Original); !ok {
		return "", "", nil
	}
	if input.Later, ok = project(c.Sources); !ok {
		return "", "", nil
	}
	previous, current := dreamRevisionDelta(input.Before, input.After)
	audit := struct {
		PreviousExcerpt string              `json:"previous_excerpt"`
		CurrentExcerpt  string              `json:"current_excerpt"`
		CurrentResult   int64               `json:"current_result"`
		Original        []dreamPromptSource `json:"original_observations"`
		Later           [][3]any            `json:"later"`
	}{PreviousExcerpt: previous, CurrentExcerpt: current, CurrentResult: c.Read.Result.Seq, Original: input.Original}
	ids := make([]string, 0, len(input.Later))
	for i, source := range c.Sources {
		if !complete(source.Call) || !complete(source.Result) {
			return "", "", nil
		}
		audit.Later = append(audit.Later, [3]any{source.Call.Seq, source.Result.Seq, input.Later[i].Workdir})
		ids = append(ids, strconv.FormatInt(source.Result.Seq, 10))
	}
	// Hash semantic content rather than per-turn sequence numbers, so replay
	// of the same effective observations does not add another audit.
	key, _ := json.Marshal(input)
	raw, _ := json.Marshal(audit)
	if len(raw) > config.DefaultDreamInputBytes {
		return "", "", nil
	}
	digest := sha256.Sum256(key)
	return dreamAuditHeader + string(raw), hex.EncodeToString(digest[:]), ids
}

// Keep the exact changed region in each revision. Matching whole lines at
// either end are available in the complete current skill result.
func dreamRevisionDelta(before, after string) (string, string) {
	previous := strings.SplitAfter(before, "\n")
	current := strings.SplitAfter(after, "\n")
	for len(previous) > 0 && len(current) > 0 && previous[0] == current[0] {
		previous, current = previous[1:], current[1:]
	}
	for len(previous) > 0 && len(current) > 0 && previous[len(previous)-1] == current[len(current)-1] {
		previous, current = previous[:len(previous)-1], current[:len(current)-1]
	}
	return strings.Join(previous, ""), strings.Join(current, "")
}

func dreamReference(ev store.Event) dreamEventRef {
	return dreamEventRef{ThreadID: ev.ThreadID, TurnID: ev.TurnID, Seq: ev.Seq}
}

func seenDreamInput(c *dreamCandidate, digest string) bool {
	for _, ev := range c.Attempts {
		var out struct {
			Dream *dreamReceipt `json:"dream"`
		}
		if json.Unmarshal([]byte(ev.Text), &out) == nil && out.Dream != nil &&
			out.Dream.PatchID == c.Patch.ID && out.Dream.InputSHA256 == digest {
			return true
		}
	}
	return false
}

// prepareDream runs inside the existing project review gate. It freezes an
// eligible revision and its exact visible evidence before the normal review.
func (e *Engine) prepareDream(turn *store.Turn, pc *projectContext, events []store.Event, visible map[int64]store.Event) *dreamReview {
	settings := e.cfg.Snapshot().Memory
	if e.reviews.dreamCtx.Err() != nil || e.restarting() || !settings.Enabled || !settings.AutoReview || !settings.AutoDream || len(events) == 0 ||
		turn.Status != store.TurnDone || turn.GoalContinue || turn.ScheduleContinue || turn.NuwaReadOnly ||
		pc == nil || pc.library || pc.planning || pc.project == nil || pc.memory == nil || turn.ProviderID == "" || e.pool.RouteUnavailable(turn.ProviderID) {
		return nil
	}
	started := time.Now()
	ctx, cancel := context.WithTimeout(e.reviews.dreamCtx, config.DefaultDreamTimeout)
	defer cancel()
	candidate, err := e.discoverDream(ctx, turn, pc.project.ID, pc.memory, events)
	if err != nil {
		e.log.Warn("could not discover a memory verification candidate", "turn", turn.ID, "err", err)
		return nil
	}
	if candidate == nil || len(candidate.Sources) < 2 {
		return nil
	}
	prompt, digest, ids := dreamInput(candidate, visible)
	if prompt == "" || ctx.Err() != nil || seenDreamInput(candidate, digest) {
		return nil
	}
	thread, err := e.store.GetThread(turn.ThreadID)
	if err != nil || thread.ProjectID != pc.project.ID || thread.PlanMode || ctx.Err() != nil {
		return nil
	}
	receipt := dreamReceipt{PatchID: candidate.Patch.ID, Patch: dreamReference(candidate.Patch), Skill: candidate.Change.Name, InputSHA256: digest, DiscoveryUS: time.Since(started).Microseconds()}
	for _, source := range candidate.Sources {
		receipt.Sources = append(receipt.Sources, dreamReference(source.Result))
	}
	return &dreamReview{candidate: candidate, prompt: prompt, ids: ids, receipt: receipt}
}

func (d *dreamReview) decision(raw json.RawMessage) (string, []dreamEventRef, string) {
	if d == nil {
		return "", nil, ""
	}
	if len(raw) == 0 {
		return "not_submitted", nil, ""
	}
	change := d.candidate.Change
	verdict, reason := parseDreamVerdict(string(raw), *change.Before, *change.After, d.ids)
	if reason != "" {
		return "invalid", nil, reason
	}
	var refs []dreamEventRef
	for i, id := range d.ids {
		for _, selected := range verdict.Sources {
			if id == selected {
				refs = append(refs, d.receipt.Sources[i])
				break
			}
		}
	}
	return verdict.Decision, refs, ""
}

func (e *Engine) commitDream(turn *store.Turn, pc *projectContext, c *dreamCandidate, receipt *dreamReceipt, out *reviewOutcome) bool {
	started := time.Now()
	done, err := e.beginAdmission()
	if err != nil {
		return false
	}
	defer done()

	// Same order as event ownership deletion and truncation. No model call
	// holds either lock. The durable owner and evidence must still exist when
	// the atomic skill replacement and its receipt are attempted.
	e.generations.Lock()
	defer e.generations.Unlock()
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	projectID := pc.project.ID
	if !e.dreamTurnOwned(turn.ID, turn.ThreadID, projectID, turn.ProviderID, true) {
		return false
	}
	project, err := e.store.GetProject(projectID)
	settings := e.cfg.Snapshot().Memory
	if err != nil || e.reviews.dreamCtx.Err() != nil || !project.MemoryEnabled || !settings.Enabled || !settings.AutoReview || !settings.AutoDream {
		return false
	}
	if receipt.Status == "revert" {
		if !e.dreamSourcesLive(projectID, turn.ProviderID, c) {
			receipt.Status = "source_changed"
		} else {
			e.restoreDream(pc.memory, c, receipt, out)
		}
	}
	receipt.ApplyUS = time.Since(started).Microseconds()
	out.Dream = receipt
	out.ProjectID = projectID
	if out.Notify == "" {
		out.Notify = settings.NotifyLevel()
	}
	raw, err := json.Marshal(out)
	if err != nil {
		e.logPersistFailure("could not encode a memory verification receipt", err, "turn", turn.ID)
		return true
	}
	ev := store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: KindMemoryReview, AgentID: ReviewAgentID, Text: string(raw), Err: out.Err}
	if err := e.store.AppendEvent(&ev); err != nil {
		e.logPersistFailure("could not persist a memory verification receipt", err, "turn", turn.ID)
		if receipt.Status == "restored" {
			change := c.Change
			// The exact reverse CAS leaves concurrent manual edits untouched.
			if _, _, undoErr := pc.memory.RestoreSkillRevision(change.Name, *change.Before, *change.After); undoErr != nil {
				e.logPersistFailure("could not undo a restoration without a durable receipt", undoErr, "turn", turn.ID, "skill", change.Name)
			}
		}
		return true
	}
	e.broadcast(ev)
	return true
}

func (e *Engine) restoreDream(mem *memory.Store, c *dreamCandidate, receipt *dreamReceipt, out *reviewOutcome) {
	change, changed, err := mem.RestoreSkillRevision(c.Change.Name, *c.Change.After, *c.Change.Before)
	switch {
	case errors.Is(err, memory.ErrSkillRevisionConflict):
		receipt.Status = "revision_changed"
	case err != nil:
		receipt.Status = "storage_error"
		e.log.Warn("could not restore a skill revision", "skill", c.Change.Name, "err", err)
	case changed:
		receipt.Status = "restored"
		collectReviewChange(out, change)
		if out.Err == "" {
			out.Note = "Restored the previous skill after checking later evidence."
		}
	default:
		receipt.Status = "unchanged"
	}
}

func (e *Engine) dreamTurnOwned(turnID, threadID, projectID, providerID string, requireNormalMode bool) bool {
	if _, dropped := e.droppedTurns[turnID]; dropped {
		return false
	}
	thread, err := e.store.GetThread(threadID)
	if err != nil || projectID == "" || thread.ProjectID != projectID || requireNormalMode && thread.PlanMode {
		return false
	}
	turn, err := e.store.GetTurn(turnID)
	return err == nil && turn.ThreadID == threadID && turn.ProjectID == projectID && turn.ProviderID == providerID && turn.Status == store.TurnDone
}

func (e *Engine) dreamSourcesLive(projectID, providerID string, c *dreamCandidate) bool {
	events := []store.Event{c.Patch, c.Read.Call, c.Read.Result}
	for _, group := range [][]dreamSource{c.Original, c.Sources} {
		for _, source := range group {
			events = append(events, source.Call, source.Result)
		}
	}
	turns := map[string]bool{}
	for _, event := range events {
		if !turns[event.TurnID] {
			if !e.dreamTurnOwned(event.TurnID, event.ThreadID, projectID, providerID, false) {
				return false
			}
			turns[event.TurnID] = true
		}
		actual, err := e.store.GetEvent(event.ThreadID, event.Seq)
		if err != nil || actual.ID != event.ID || actual.TurnID != event.TurnID || actual.Kind != event.Kind ||
			actual.AgentID != event.AgentID || actual.ToolCallID != event.ToolCallID || actual.ToolReceiptScope != event.ToolReceiptScope ||
			actual.WorkerGeneration != event.WorkerGeneration || actual.Err != event.Err || actual.Text != event.Text ||
			strings.TrimSpace(actual.SourceThreadID) != "" || strings.TrimSpace(actual.SourceTurnID) != "" {
			return false
		}
	}
	return true
}
