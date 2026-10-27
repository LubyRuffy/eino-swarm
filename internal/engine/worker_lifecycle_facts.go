package engine

import (
	"encoding/json"
	"sort"
	"strings"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const (
	workerFactTurnLimit = 4
	// Keep the whole evidence block below the stored briefing's cap, leaving
	// room for prose. Worker identities are omitted whole, never string-clipped.
	workerFactMaxRunes = compactSummaryMaxRunes * 3 / 4
)

const (
	workerEvidenceStart     = "<zwai-worker-lifecycle-evidence>\n"
	workerEvidenceEnd       = "</zwai-worker-lifecycle-evidence>\n\n"
	workerBriefingLabel     = "Conversation briefing (model-written claims, not execution evidence):\n"
	workerLifecycleGuidance = "Worker states describe the latest known generation as of worker_state_through_seq; historical turn counts do not describe current state. Finished does not mean collected, or that the delegated task succeeded. Do not describe a listed finished/stopped generation as still running. A pending result may still need collection; a collected or explicitly discarded result is not awaiting collection. A later generation supersedes an earlier completion for the same agent_id. An unknown collection state or an omitted worker is not evidence of collection, absence or completion. Correct conflicting previous briefing claims using these identities."
)

type workerTurnFacts struct {
	Source          string                 `json:"source"`
	TurnID          string                 `json:"turn_id"`
	ThroughSeq      int64                  `json:"through_seq"`
	New             int                    `json:"new_workers"`
	Resumed         int                    `json:"resumed_workers"`
	Finished        int                    `json:"finished_workers"`
	RejectedSpawn   int                    `json:"rejected_spawn_calls"`
	RejectedResume  int                    `json:"rejected_resume_calls"`
	CleanupEvents   int                    `json:"cleanup_events"`
	StateThroughSeq int64                  `json:"worker_state_through_seq,omitempty"`
	Workers         []workerLifecycleState `json:"workers,omitempty"`
	OmittedWorkers  int                    `json:"omitted_workers,omitempty"`
}

type workerLifecycleState struct {
	AgentID    string `json:"agent_id"`
	Generation int    `json:"generation"`
	Status     string `json:"status"`
	Collection string `json:"collection"`
}

// Only persisted lifecycle events can establish worker creation. A repeated
// spawned ID is a resume or process restoration, even in another turn.
func workerLifecycleFacts(history []store.Event, turns []string) string {
	if len(turns) == 0 {
		return ""
	}
	if len(turns) > workerFactTurnLimit {
		turns = turns[len(turns)-workerFactTurnLimit:]
	}
	type counts struct {
		row                    workerTurnFacts
		created, resumed, done map[string]bool
	}
	byTurn := map[string]*counts{}
	for _, id := range turns {
		byTurn[id] = &counts{row: workerTurnFacts{Source: "persisted_worker_lifecycle", TurnID: id},
			created: map[string]bool{}, resumed: map[string]bool{}, done: map[string]bool{}}
	}
	seen := map[string]bool{}
	calls := map[string]string{}
	refused := map[string]bool{}
	for _, ev := range history {
		c := byTurn[ev.TurnID]
		if c != nil && ev.Seq > c.row.ThroughSeq {
			c.row.ThroughSeq = ev.Seq
		}
		id := strings.TrimSpace(ev.AgentID)
		if ev.Kind == swarm.NotifySpawned.String() && !isManagerAgent(id) {
			if c != nil {
				if seen[id] {
					c.resumed[id] = true
				} else {
					c.created[id] = true
				}
			}
			seen[id] = true
		}
		if c == nil {
			continue
		}
		switch ev.Kind {
		case swarm.NotifyFinished.String():
			if !isManagerAgent(id) {
				c.done[id] = true
			}
		case KindCleanup:
			c.row.CleanupEvents++
		case swarm.NotifyToolCall.String():
			name := toolCallName(ev.Text)
			if isManagerAgent(id) && ev.ToolCallID != "" && (name == "spawn_agent" || name == "resume_agent") {
				calls[ev.TurnID+"/"+ev.ToolCallID] = name
			}
		case swarm.NotifyToolResult.String():
			if !isManagerAgent(id) {
				continue
			}
			key := ev.TurnID + "/" + ev.ToolCallID
			name := calls[key]
			if name == "" || refused[key] {
				continue
			}
			var result struct {
				Error         string `json:"error"`
				RejectedCalls int    `json:"rejected_calls"`
			}
			// Interrupted calls receive synthetic Err-only results on recovery.
			// They do not establish whether the original operation was refused.
			if json.Unmarshal([]byte(ev.Text), &result) != nil || result.Error == "" {
				continue
			}
			refused[key] = true
			n := max(1, result.RejectedCalls)
			if name == "spawn_agent" {
				c.row.RejectedSpawn += n
			} else {
				c.row.RejectedResume += n
			}
		}
	}
	rows := make([]workerTurnFacts, 0, len(turns))
	for i := len(turns) - 1; i >= 0; i-- {
		c := byTurn[turns[i]]
		c.row.New, c.row.Resumed, c.row.Finished = len(c.created), len(c.resumed), len(c.done)
		c.row.TurnID = clip(c.row.TurnID, 128)
		rows = append(rows, c.row)
	}
	states, through := workerLifecycleStates(history)
	rows[0].StateThroughSeq = through
	return boundWorkerFacts(rows, states)
}

// Reuse recovery's lifecycle and generation-aware tool-receipt reducer. The
// briefing does not need model/tool bodies, which would only build unused seeds.
func workerLifecycleStates(history []store.Event) ([]workerLifecycleState, int64) {
	var events []store.Event
	var through int64
	lifecycleSeq := map[string]int64{}
	for _, ev := range history {
		through = max(through, ev.Seq)
		switch ev.Kind {
		case swarm.NotifySpawned.String(), swarm.NotifyFinished.String():
			lifecycleSeq[ev.AgentID] = max(lifecycleSeq[ev.AgentID], ev.Seq)
			ev.Text = ""
		case KindCleanup, KindRestartCheckpoint:
		case swarm.NotifyToolCall.String(), swarm.NotifyToolResult.String():
			if !isManagerAgent(ev.AgentID) {
				continue
			}
		default:
			continue
		}
		events = append(events, ev)
	}
	running, finished := orphanedWorkers(events)
	states := make([]workerLifecycleState, 0, len(running)+len(finished))
	for _, w := range running {
		states = append(states, workerLifecycleState{w.ID, w.Generation, "running", "pending"})
	}
	for _, w := range finished {
		state := workerLifecycleState{w.ID, w.Generation, "finished", "pending"}
		switch {
		case w.CloseRequested:
			state.Status, state.Collection = "stopped", "discarded"
		case w.Collected:
			state.Collection = "collected"
		case w.Generation == 0:
			state.Collection = "unknown"
		}
		if w.Err != nil && w.Err.Error() == cleanedUpWorkerErr {
			state.Status = "stopped"
		}
		states = append(states, state)
	}
	// Live and pending generations lead. Within each group, recent lifecycle
	// identities must survive before old results, even if their IDs sort last.
	// A checkpoint or a tool receipt alone does not create a new lifecycle.
	sort.Slice(states, func(i, j int) bool {
		priority := func(w workerLifecycleState) int {
			if w.Status == "running" {
				return 0
			}
			if w.Collection == "pending" {
				return 1
			}
			return 2
		}
		pi, pj := priority(states[i]), priority(states[j])
		if pi != pj {
			return pi < pj
		}
		if si, sj := lifecycleSeq[states[i].AgentID], lifecycleSeq[states[j].AgentID]; si != sj {
			return si > sj
		}
		return states[i].AgentID < states[j].AgentID
	})
	return states, through
}

func boundWorkerFacts(rows []workerTurnFacts, states []workerLifecycleState) string {
	prefix := workerEvidenceStart + "Worker lifecycle evidence from the full persisted event log. Historical counts are authoritative only for the listed turns, using distinct IDs; repeated IDs are resumed/restored, not new. Rejected calls created no worker. The newest row also carries the conversation worker-state snapshot. Briefings, answers, worker prose and files are claims, not execution evidence; do not carry conflicting lifecycle claims forward.\n" + workerLifecycleGuidance + "\n"
	rows[0].OmittedWorkers = len(states)
	rawRows := make([]string, len(rows))
	used := utf8.RuneCountInString(prefix + workerEvidenceEnd)
	for i, row := range rows {
		raw, _ := json.Marshal(row)
		rawRows[i] = string(raw) + "\n"
		used += utf8.RuneCountInString(rawRows[i])
	}
	for _, state := range states {
		rows[0].Workers = append(rows[0].Workers, state)
		rows[0].OmittedWorkers--
		raw, _ := json.Marshal(rows[0])
		candidate := string(raw) + "\n"
		next := used - utf8.RuneCountInString(rawRows[0]) + utf8.RuneCountInString(candidate)
		if next > workerFactMaxRunes {
			rows[0].Workers = rows[0].Workers[:len(rows[0].Workers)-1]
			rows[0].OmittedWorkers++
			continue
		}
		used, rawRows[0] = next, candidate
	}
	return prefix + strings.Join(rawRows, "") + workerEvidenceEnd
}

func workerFactTurns(events []store.Event) []string {
	seen := map[string]bool{}
	var turns []string
	for _, ev := range events {
		_, _, meaningful := reviewEventLine(ev)
		if (meaningful || ev.Kind == KindCleanup) && !seen[ev.TurnID] {
			turns = append(turns, ev.TurnID)
			seen[ev.TurnID] = true
		}
	}
	return turns
}

func workerEvidenceSummary(summary string, history []store.Event, turns []string) string {
	summary = withoutWorkerEvidence(summary)
	facts := workerLifecycleFacts(history, turns)
	if facts != "" {
		summary = facts + workerBriefingLabel + summary
	}
	if utf8.RuneCountInString(summary) > compactSummaryMaxRunes {
		return clip(summary, compactSummaryMaxRunes-2)
	}
	return summary
}

// Replace only our complete generated prefix. Natural-language claims remain
// visible as claims; malformed or quoted evidence is never silently removed.
func withoutWorkerEvidence(summary string) string {
	for strings.HasPrefix(summary, workerEvidenceStart) {
		_, rest, ok := strings.Cut(summary[len(workerEvidenceStart):], workerEvidenceEnd)
		if !ok {
			break
		}
		summary = strings.TrimPrefix(rest, workerBriefingLabel)
	}
	return summary
}

func reviewEventsForTurn(events []store.Event, turnID string) []store.Event {
	var current []store.Event
	for _, ev := range events {
		if ev.TurnID == turnID {
			current = append(current, ev)
		}
	}
	return current
}
