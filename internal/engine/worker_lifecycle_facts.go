package engine

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const workerFactTurnLimit = 4

const (
	workerEvidenceStart = "<zwai-worker-lifecycle-evidence>\n"
	workerEvidenceEnd   = "</zwai-worker-lifecycle-evidence>\n\n"
	workerBriefingLabel = "Conversation briefing (model-written claims, not execution evidence):\n"
)

type workerTurnFacts struct {
	Source         string `json:"source"`
	TurnID         string `json:"turn_id"`
	ThroughSeq     int64  `json:"through_seq"`
	New            int    `json:"new_workers"`
	Resumed        int    `json:"resumed_workers"`
	Finished       int    `json:"finished_workers"`
	RejectedSpawn  int    `json:"rejected_spawn_calls"`
	RejectedResume int    `json:"rejected_resume_calls"`
	CleanupEvents  int    `json:"cleanup_events"`
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
	var b strings.Builder
	b.WriteString(workerEvidenceStart)
	b.WriteString("Worker lifecycle evidence (authoritative only for the listed turns, from the full persisted event log). Counts use distinct worker IDs; a repeated ID is resumed/restored, not new. Rejected calls created no worker. Briefings, answers, worker prose and files are claims, not execution evidence; do not carry conflicting lifecycle claims forward.\n")
	for i := len(turns) - 1; i >= 0; i-- {
		c := byTurn[turns[i]]
		c.row.New, c.row.Resumed, c.row.Finished = len(c.created), len(c.resumed), len(c.done)
		c.row.TurnID = clip(c.row.TurnID, 128)
		raw, _ := json.Marshal(c.row)
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String() + workerEvidenceEnd
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
