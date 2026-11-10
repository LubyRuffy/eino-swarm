package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	einoexec "github.com/LubyRuffy/eino-tools/exec"
)

const (
	dreamMaxSources     = 4
	dreamMinLater       = 2
	dreamMaxSourceBytes = 24_000
)

type dreamSource struct {
	ID           string
	Call, Result store.Event
}

type dreamCandidate struct {
	Patch    store.Event
	Change   memory.Change
	Original []dreamSource
	Sources  []dreamSource
	Read     dreamSource
	Attempts []store.Event
}

// discoverDream finds leads worth evaluating, not semantic contradictions.
// Complete native exec receipts prove only host observations of a process.
func (e *Engine) discoverDream(ctx context.Context, turn *store.Turn, projectID string, mem *memory.Store, events []store.Event) (*dreamCandidate, error) {
	if turn == nil || turn.Status != store.TurnDone || projectID == "" || turn.ProjectID != projectID || turn.ProviderID == "" || mem == nil || len(events) > store.DreamToolEventLimit {
		return nil, nil
	}
	pairs := dreamToolPairs(turn.ThreadID, turn.ID, events)
	var anchor dreamSource
	var document, skillName string
	var observations []dreamSource
	for _, pair := range pairs {
		name, args := dreamCall(pair.Call.Text)
		if name != memory.ToolSkillView {
			continue
		}
		var request struct{ Name string }
		var receipt struct {
			Success                 bool
			Name, Description, Body string
		}
		if json.Unmarshal([]byte(args), &request) != nil || json.Unmarshal([]byte(pair.Result.Text), &receipt) != nil || !receipt.Success || strings.TrimSpace(request.Name) != receipt.Name || memory.ValidSkillName(receipt.Name) != nil {
			continue
		}
		var later []dreamSource
		for _, observation := range pairs {
			if observation.Call.Seq > pair.Result.Seq && dreamSameActor(pair.Call, observation.Call) && dreamExecObservation(observation) {
				later = append(later, observation)
			}
		}
		if len(later) < dreamMinLater || len(later) > dreamMaxSources {
			continue
		}
		current, raw, err := mem.ReadSkillRevision(receipt.Name)
		if errors.Is(err, memory.ErrSkillRevisionConflict) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if current.Origin != nil || current.Description != receipt.Description || current.Body != receipt.Body {
			continue
		}
		if anchor.ID != "" && (skillName != receipt.Name || document != raw) {
			return nil, nil
		}
		anchor, document, skillName, observations = pair, raw, receipt.Name, later
	}
	if anchor.ID == "" {
		return nil, nil
	}
	patches, err := e.store.DreamReviewEvents(ctx, projectID, turn.ProviderID, turn.ThreadID, turn.ID)
	if errors.Is(err, store.ErrHistorySourceUnavailable) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var candidate *dreamCandidate
	var citations []int64
	var attempts []store.Event
	for _, patch := range patches {
		var outcome reviewOutcome
		if !dreamLocalEvent(patch) || patch.AgentID != ReviewAgentID || json.Unmarshal([]byte(patch.Text), &outcome) != nil || outcome.ProjectID != projectID {
			continue
		}
		if outcome.Dream != nil {
			attempts = append(attempts, patch)
			if outcome.Dream.Skill == skillName {
				continue // A rollback is not a new observation-backed review patch.
			}
		}
		change, seqs, ok := dreamPatch(patch, anchor.Call, outcome, skillName, document)
		if !ok {
			continue
		}
		if candidate != nil {
			return nil, nil
		}
		candidate = &dreamCandidate{Patch: patch, Change: change, Read: anchor, Sources: observations}
		citations = seqs
	}
	if candidate == nil {
		return nil, nil
	}
	original, err := e.store.DreamToolEvents(ctx, projectID, turn.ProviderID, candidate.Patch.ThreadID, candidate.Patch.TurnID)
	if errors.Is(err, store.ErrHistorySourceUnavailable) || errors.Is(err, store.ErrHistoryEvidenceLimit) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	bySeq := make(map[int64]dreamSource)
	for _, pair := range dreamToolPairs(candidate.Patch.ThreadID, candidate.Patch.TurnID, original) {
		if pair.Result.Seq < candidate.Patch.Seq && dreamExecObservation(pair) {
			bySeq[pair.Result.Seq] = pair
		}
	}
	used := len(*candidate.Change.Before) + len(*candidate.Change.After)
	for _, seq := range citations {
		pair, ok := bySeq[seq]
		if !ok {
			// Do not send a truncated case that hides why the original patch
			// was applied. This version only interprets native exec evidence.
			return nil, nil
		}
		candidate.Original = append(candidate.Original, pair)
		used += len(pair.Call.Text) + len(pair.Result.Text)
	}
	sort.Slice(candidate.Original, func(i, j int) bool { return candidate.Original[i].Result.Seq < candidate.Original[j].Result.Seq })
	for _, pair := range candidate.Sources {
		used += len(pair.Call.Text) + len(pair.Result.Text)
	}
	if used > dreamMaxSourceBytes {
		return nil, nil
	}
	candidate.Attempts = attempts
	return candidate, nil
}

func dreamPatch(patch, viewed store.Event, outcome reviewOutcome, name, document string) (memory.Change, []int64, bool) {
	if !dreamLocalEvent(patch) || patch.Kind != KindMemoryReview || patch.AgentID != ReviewAgentID || patch.Err != "" || patch.TurnID == viewed.TurnID || patch.ID >= viewed.ID || !patch.CreatedAt.Before(viewed.CreatedAt) {
		return memory.Change{}, nil, false
	}
	if outcome.Library || !outcome.Changed || outcome.Err != "" || outcome.StopReason != "completed" {
		return memory.Change{}, nil, false
	}
	var change memory.Change
	for _, entry := range outcome.Changes {
		if entry.Target != memory.ToolSkillManage || entry.Name != name {
			continue
		}
		if change.Name != "" || entry.Action != "patch" || entry.Before == nil || entry.After == nil || *entry.Before == "" || *entry.Before == *entry.After || *entry.After != document {
			return memory.Change{}, nil, false
		}
		change = entry
	}
	var sources []int64
	for _, evidence := range outcome.Evidence {
		if evidence.Target != memory.ToolSkillManage || evidence.Name != name || evidence.Action != "patch" {
			continue
		}
		if sources != nil || len(evidence.Sources) == 0 || len(evidence.Sources) > dreamMaxSources {
			return memory.Change{}, nil, false
		}
		sources = evidence.Sources
	}
	seen := make(map[int64]struct{}, len(sources))
	for _, seq := range sources {
		if _, duplicate := seen[seq]; duplicate || seq <= 0 || seq >= patch.Seq {
			return memory.Change{}, nil, false
		}
		seen[seq] = struct{}{}
	}
	return change, sources, change.Name != "" && len(sources) > 0
}

type dreamToolKey struct {
	agent, scope, id string
	generation       int
}

func dreamToolPairs(threadID, turnID string, events []store.Event) []dreamSource {
	type attempt struct {
		call, result store.Event
		invalid      bool
	}
	attempts := make(map[dreamToolKey]attempt)
	for _, event := range events {
		if event.ThreadID != threadID || event.TurnID != turnID || event.ToolCallID == "" || event.AgentID == "" {
			continue
		}
		key := dreamToolKey{event.AgentID, event.ToolReceiptScope, event.ToolCallID, event.WorkerGeneration}
		pair := attempts[key]
		var slot *store.Event
		switch event.Kind {
		case swarm.NotifyToolCall.String():
			slot = &pair.call
		case swarm.NotifyToolResult.String():
			slot = &pair.result
		default:
			continue
		}
		if slot.Seq != 0 || !dreamLocalEvent(event) || event.Err != "" || event.Text == "" || len(event.Text) > store.DreamToolTextBytes {
			pair.invalid = true
		}
		*slot = event
		attempts[key] = pair
	}
	out := make([]dreamSource, 0, len(attempts))
	for _, pair := range attempts {
		if pair.invalid || pair.call.Seq <= 0 || pair.result.Seq <= pair.call.Seq || pair.result.ID <= pair.call.ID {
			continue
		}
		id := fmt.Sprintf("event:%s:%d", threadID, pair.result.Seq)
		out = append(out, dreamSource{ID: id, Call: pair.call, Result: pair.result})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Result.Seq < out[j].Result.Seq })
	return out
}

func dreamLocalEvent(event store.Event) bool {
	return event.ID > 0 && event.Seq > 0 && event.SourceThreadID == "" && event.SourceTurnID == ""
}

func dreamSameActor(a, b store.Event) bool {
	return a.AgentID == b.AgentID && a.WorkerGeneration == b.WorkerGeneration && a.ToolReceiptScope == b.ToolReceiptScope
}

func dreamCall(text string) (string, string) {
	name, args, ok := strings.Cut(text, "(")
	if !ok || !strings.HasSuffix(args, ")") {
		return "", ""
	}
	return name, strings.TrimSuffix(args, ")")
}

func dreamExecObservation(pair dreamSource) bool {
	name, args := dreamCall(pair.Call.Text)
	if name != einoexec.ToolName {
		return false
	}
	var request struct {
		Command string
		Stdin   string
		Env     map[string]json.RawMessage
	}
	var receipt struct {
		FullCommand string `json:"full_command"`
		Workdir     string
		Status      string
		ExitCode    *int `json:"exit_code"`
		Failed      *bool
		Truncated   *bool
		Stdout      *string
		Stderr      *string
	}
	return json.Unmarshal([]byte(args), &request) == nil && json.Unmarshal([]byte(pair.Result.Text), &receipt) == nil &&
		request.Stdin == "" && len(request.Env) == 0 && strings.TrimSpace(request.Command) != "" && receipt.FullCommand == strings.TrimSpace(request.Command) && receipt.Workdir != "" &&
		(receipt.Status == "" || receipt.Status == "exited") && receipt.ExitCode != nil && *receipt.ExitCode >= 0 && receipt.Failed != nil && *receipt.Failed == (*receipt.ExitCode != 0) &&
		receipt.Truncated != nil && !*receipt.Truncated && receipt.Stdout != nil && receipt.Stderr != nil
}
