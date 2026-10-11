package engine

import (
	"errors"
	"fmt"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// destinationFields is a PATCH that moves where later fires land.
// A busy target is still accepted: the next tick skips it. The fire
// already claimed keeps the conversation and kind stored on its run.
func (e *Engine) destinationFields(row *store.Schedule, p ScheduleFields) (map[string]any, error) {
	if row.Status != store.ScheduleActive && row.Status != store.SchedulePaused {
		return nil, fmt.Errorf("engine: schedule destination can only change while the wait is active or paused")
	}
	kind := ""
	if p.Kind != nil {
		kind = strings.TrimSpace(*p.Kind)
	}
	switch kind {
	case store.ScheduleThread:
		return e.threadDestination(p)
	case store.ScheduleStandalone:
		return e.standaloneDestination(row, p)
	default:
		return nil, fmt.Errorf("engine: unknown schedule kind")
	}
}

func (e *Engine) threadDestination(p ScheduleFields) (map[string]any, error) {
	threadID := ""
	if p.ThreadID != nil {
		threadID = strings.TrimSpace(*p.ThreadID)
	}
	if threadID == "" {
		return nil, fmt.Errorf("engine: a thread wake needs a conversation")
	}
	th, err := e.store.GetThread(threadID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil, fmt.Errorf("engine: that conversation is not available")
		}
		return nil, err
	}
	if th.Archived {
		return nil, fmt.Errorf("engine: that conversation is not available")
	}
	return map[string]any{
		"kind":       store.ScheduleThread,
		"thread_id":  threadID,
		"project_id": "",
	}, nil
}

func (e *Engine) standaloneDestination(row *store.Schedule, p ScheduleFields) (map[string]any, error) {
	projectID := ""
	if p.ProjectID != nil {
		projectID = strings.TrimSpace(*p.ProjectID)
	}
	if projectID != "" {
		if _, err := e.store.GetProject(projectID); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return nil, fmt.Errorf("engine: that project is not available")
			}
			return nil, err
		}
	}
	fields := map[string]any{
		"kind":       store.ScheduleStandalone,
		"thread_id":  "",
		"project_id": projectID,
	}
	providerID, model, fill, err := e.standaloneModelFill(row)
	if err != nil {
		return nil, err
	}
	if fill {
		fields["provider_id"] = providerID
		fields["model"] = model
	}
	return fields, nil
}

// standaloneModelFill copies the default endpoint onto a row that cannot
// mint yet. A provider and model already stored stay, so switching back
// to a conversation does not wipe the snapshot.
func (e *Engine) standaloneModelFill(row *store.Schedule) (providerID, model string, fill bool, err error) {
	providerID = strings.TrimSpace(row.ProviderID)
	model = strings.TrimSpace(row.Model)
	if providerID != "" && model != "" {
		return providerID, model, false, nil
	}
	if providerID == "" {
		providerID = strings.TrimSpace(e.scheduleDefaultProvider())
	}
	prov, err := e.pool.ResolveModel(providerID, model)
	if err != nil {
		return "", "", false, fmt.Errorf("engine: a new conversation each run needs a model")
	}
	if model == "" {
		model = strings.TrimSpace(prov.Model)
	}
	if model == "" {
		return "", "", false, fmt.Errorf("engine: a new conversation each run needs a model")
	}
	return providerID, model, true, nil
}

// claimedScheduleKind is what the run must remember. A later PATCH of the
// schedule must not decide whether this fire archives its conversation.
func claimedScheduleKind(row store.Schedule, standalone bool) string {
	kind := strings.TrimSpace(row.Kind)
	switch kind {
	case store.ScheduleThread, store.ScheduleStandalone:
		return kind
	}
	if standalone {
		return store.ScheduleStandalone
	}
	return store.ScheduleThread
}

// scheduleRunMintsConversation is true when this fire opened its own
// conversation. An empty kind is a row claimed before that column: it
// still follows the schedule, which had not been retargeted yet.
func scheduleRunMintsConversation(run *store.ScheduleRun, sch *store.Schedule) bool {
	kind := ""
	if run != nil {
		kind = strings.TrimSpace(run.Kind)
	}
	if kind == "" && sch != nil {
		kind = strings.TrimSpace(sch.Kind)
	}
	return kind == store.ScheduleStandalone
}
