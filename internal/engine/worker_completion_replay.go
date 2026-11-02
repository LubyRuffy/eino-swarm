package engine

import (
	"encoding/json"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type workerCollectionCall struct {
	name             string
	ids              map[string]bool
	role             string
	startsGeneration bool
}
type workerCollectionCallKey struct {
	turn, call, actor string
	generation        int
}
type workerCollectionCalls map[workerCollectionCallKey]workerCollectionCall

func collectionCallKey(ev store.Event) workerCollectionCallKey {
	actor, generation := ev.AgentID, ev.WorkerGeneration
	if isManagerAgent(actor) {
		actor, generation = swarm.DefaultManagerID, 0
	}
	return workerCollectionCallKey{ev.TurnID, ev.ToolCallID, actor, generation}
}

func (calls workerCollectionCalls) note(ev store.Event) {
	if (!isManagerAgent(ev.AgentID) && ev.WorkerGeneration <= 0) || ev.Kind != swarm.NotifyToolCall.String() || ev.ToolCallID == "" {
		return
	}
	key := collectionCallKey(ev)
	delete(calls, key)
	name, args := splitToolCallText(ev.Text)
	switch name {
	case "spawn_agent", "resume_agent", "wait_agents", "close_agent":
	default:
		return
	}
	var body struct {
		AgentID  string   `json:"agent_id"`
		AgentIDs []string `json:"agent_ids"`
		Role     string   `json:"role"`
	}
	if json.Unmarshal([]byte(args), &body) != nil {
		return
	}
	ids := map[string]bool{}
	for _, id := range body.AgentIDs {
		ids[id] = true
	}
	if body.AgentID != "" {
		ids[strings.TrimSpace(body.AgentID)] = true
	}
	calls[key] = workerCollectionCall{name: name, ids: ids, role: strings.TrimSpace(body.Role), startsGeneration: true}
}

func (calls workerCollectionCalls) startsGeneration(ev store.Event) bool {
	for key, call := range calls {
		if !call.startsGeneration || key.turn != ev.TurnID {
			continue
		}
		if call.name == "resume_agent" && call.ids[ev.AgentID] || call.name == "spawn_agent" && call.role != "" && call.role == ev.Role {
			return true
		}
	}
	return false
}

// Only paired runtime tool receipts settle a generation. Assistant prose,
// another worker's result and a delayed result for an older generation cannot.
func (calls workerCollectionCalls) apply(ev store.Event, slots map[string]*workerSlot) {
	calls.note(ev)
	if (!isManagerAgent(ev.AgentID) && ev.WorkerGeneration <= 0) || ev.Kind != swarm.NotifyToolResult.String() {
		return
	}
	key := collectionCallKey(ev)
	call, ok := calls[key]
	if !ok {
		return
	}
	delete(calls, key)
	if ev.Err != "" {
		return
	}
	canControl := func(id string) bool {
		if isManagerAgent(ev.AgentID) {
			return true
		}
		caller := slots[ev.AgentID]
		if caller == nil || caller.generation != ev.WorkerGeneration {
			return false
		}
		return replayDescendant(slots, id, ev.AgentID)
	}
	var receipt struct {
		AgentID         string `json:"agent_id"`
		Generation      int    `json:"generation"`
		Error           string `json:"error"`
		Cancelled       bool   `json:"cancelled"`
		AlreadyFinished bool   `json:"already_finished"`
		ClosedAgents    []struct {
			AgentID         string `json:"agent_id"`
			Generation      int    `json:"generation"`
			Cancelled       bool   `json:"cancelled"`
			AlreadyFinished bool   `json:"already_finished"`
		} `json:"closed_agents"`
		Agents []struct {
			AgentID    string `json:"agent_id"`
			Generation int    `json:"generation"`
			Status     string `json:"status"`
		} `json:"agents"`
	}
	if json.Unmarshal([]byte(ev.Text), &receipt) != nil || receipt.Error != "" {
		return
	}
	if call.name == "wait_agents" {
		for _, item := range receipt.Agents {
			s := slots[item.AgentID]
			if s != nil && canControl(item.AgentID) && call.ids[item.AgentID] && item.Generation > 0 && s.generation == item.Generation && (item.Status == "done" || item.Status == "failed") {
				s.collected = true
			}
		}
		return
	}
	s := slots[receipt.AgentID]
	if s == nil || !canControl(receipt.AgentID) || !s.spawned || receipt.Generation <= 0 || strings.TrimSpace(receipt.AgentID) == "" {
		return
	}
	if call.name == "spawn_agent" || call.name == "resume_agent" {
		if receipt.Generation < s.generation {
			return
		}
		if call.name == "resume_agent" && !call.ids[receipt.AgentID] {
			return
		}
		// Spawned is the lifecycle fact; this successful receipt supplies
		// its generation identity. Legacy finished rosters have no identity.
		if receipt.Generation > s.generation {
			s.collected, s.closeRequested = false, false
		}
		s.generation = receipt.Generation
		return
	}
	if call.ids[receipt.AgentID] && s.generation == receipt.Generation {
		if receipt.Cancelled {
			s.closeRequested = true
		}
		if receipt.AlreadyFinished {
			s.collected = true
		}
		if call.name == "close_agent" {
			for _, item := range receipt.ClosedAgents {
				child := slots[item.AgentID]
				if child == nil || !canControl(item.AgentID) || item.Generation <= 0 || child.generation != item.Generation || item.Cancelled == item.AlreadyFinished {
					continue
				}
				if item.AgentID != receipt.AgentID && !replayDescendant(slots, item.AgentID, receipt.AgentID) {
					continue
				}
				if item.Cancelled {
					child.closeRequested = true
				}
				if item.AlreadyFinished {
					child.collected = true
				}
			}
		}
	}
}

func replayDescendant(slots map[string]*workerSlot, id, ancestor string) bool {
	seen := map[string]bool{}
	for id != "" && !seen[id] {
		seen[id] = true
		slot := slots[id]
		if slot == nil {
			return false
		}
		id = slot.parentID
		if id == ancestor {
			return true
		}
	}
	return false
}
