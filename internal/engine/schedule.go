package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Schedule event kinds. Same stability rule as goal: Trace and the UI match
// on these strings, so renaming one is a protocol change.
const (
	KindSchedule          = "schedule"
	KindScheduleFired     = "schedule_fired"
	KindScheduleSkipped   = "schedule_skipped"
	KindScheduleReport    = "schedule_report"
	KindScheduleCancelled = "schedule_cancelled"
)

// ScheduleInput is the human or manager spec for one wait. Cadence is
// exactly one of DelayS, EveryS, or Cron — parseScheduleSpec enforces it.
type ScheduleInput struct {
	Kind            string
	ThreadID        string
	OriginThreadID  string
	ProjectID       string
	ProviderID      string
	Model           string
	ReasoningEffort string
	Title           string
	Prompt          string
	DelayS          int
	EveryS          int
	Cron            string
	MaxRuns         int
	UntilAt         *time.Time
	CreatedBy       string
}

// CreateSchedule arms a wait. It does not fire: the ticker (later) is what
// starts a turn. Thread wakes require an existing conversation.
func (e *Engine) CreateSchedule(in ScheduleInput) (*store.Schedule, error) {
	in.Kind = strings.TrimSpace(in.Kind)
	in.ThreadID = strings.TrimSpace(in.ThreadID)
	in.OriginThreadID = strings.TrimSpace(in.OriginThreadID)
	in.ProjectID = strings.TrimSpace(in.ProjectID)
	in.ProviderID = strings.TrimSpace(in.ProviderID)
	in.Model = strings.TrimSpace(in.Model)
	in.ReasoningEffort = strings.TrimSpace(in.ReasoningEffort)
	in.Title = strings.TrimSpace(in.Title)
	in.Prompt = strings.TrimSpace(in.Prompt)
	in.Cron = strings.TrimSpace(in.Cron)
	in.CreatedBy = strings.TrimSpace(in.CreatedBy)

	switch in.Kind {
	case store.ScheduleThread, store.ScheduleStandalone:
	default:
		return nil, fmt.Errorf("engine: unknown schedule kind")
	}
	if in.Prompt == "" {
		return nil, fmt.Errorf("engine: a schedule needs a prompt")
	}

	spec, err := parseScheduleSpec(in.DelayS, in.EveryS, in.Cron, e.cfg.Swarm.ScheduleMinInterval())
	if err != nil {
		return nil, err
	}

	if in.Kind == store.ScheduleThread {
		if in.ThreadID == "" {
			return nil, fmt.Errorf("engine: a thread wake needs a conversation")
		}
		if _, err := e.store.GetThread(in.ThreadID); err != nil {
			return nil, err
		}
		if in.OriginThreadID == "" {
			in.OriginThreadID = in.ThreadID
		}
	} else {
		// Standalone fires mint a conversation; the origin is only a chip.
		in.ThreadID = ""
	}
	if in.OriginThreadID != "" {
		if _, err := e.store.GetThread(in.OriginThreadID); err != nil {
			return nil, err
		}
	}

	now := time.Now().UTC()
	next, err := firstRunAt(spec, now)
	if err != nil {
		return nil, err
	}

	row := &store.Schedule{
		Kind:            in.Kind,
		OriginThreadID:  in.OriginThreadID,
		ThreadID:        in.ThreadID,
		ProjectID:       in.ProjectID,
		ProviderID:      in.ProviderID,
		Model:           in.Model,
		ReasoningEffort: in.ReasoningEffort,
		Title:           in.Title,
		Prompt:          in.Prompt,
		DelayS:          in.DelayS,
		EveryS:          in.EveryS,
		Cron:            in.Cron,
		Status:          store.ScheduleActive,
		NextRunAt:       next,
		MaxRuns:         in.MaxRuns,
		UntilAt:         in.UntilAt,
		CreatedBy:       in.CreatedBy,
	}
	if err := e.store.CreateScheduleUnderCap(row, e.cfg.Swarm.MaxActiveSchedules()); err != nil {
		if errors.Is(err, store.ErrScheduleCap) {
			return nil, fmt.Errorf("engine: too many active schedules")
		}
		return nil, err
	}
	e.recordScheduleArmed(row)
	return row, nil
}

// firstRunAt is the initial due time. nextAfter is for the fire AFTER a
// successful run; a one-shot delay is due now+delay here and zero later.
func firstRunAt(spec scheduleSpec, now time.Time) (time.Time, error) {
	switch {
	case spec.delay != 0:
		return now.Add(spec.delay), nil
	case spec.every != 0:
		return now.Add(spec.every), nil
	default:
		next := spec.nextAfter(now)
		if next.IsZero() {
			return time.Time{}, fmt.Errorf("engine: cron has no next run")
		}
		return next, nil
	}
}

func (e *Engine) recordScheduleArmed(row *store.Schedule) {
	threadID := row.OriginThreadID
	if threadID == "" {
		return
	}
	text, _ := json.Marshal(struct {
		ID        string    `json:"id"`
		Kind      string    `json:"kind"`
		Title     string    `json:"title"`
		NextRunAt time.Time `json:"next_run_at"`
	}{ID: row.ID, Kind: row.Kind, Title: row.Title, NextRunAt: row.NextRunAt})
	e.record(store.Event{
		ThreadID: threadID, TurnID: e.lastTurnID(threadID),
		Kind: KindSchedule, AgentID: swarm.DefaultManagerID,
		Text: string(text),
	})
}

// CancelSchedule marks a row cancelled and records a chip on the origin
// conversation, or on the wake target if origin was never set.
func (e *Engine) CancelSchedule(id string) error {
	row, err := e.store.CancelSchedule(id)
	if err != nil {
		return err
	}
	if row == nil {
		return nil
	}
	e.recordScheduleCancelled(row)
	return nil
}

func (e *Engine) recordScheduleCancelled(row *store.Schedule) {
	threadID := row.OriginThreadID
	if threadID == "" {
		threadID = row.ThreadID
	}
	if threadID == "" {
		return
	}
	e.record(store.Event{
		ThreadID: threadID, TurnID: e.lastTurnID(threadID),
		Kind: KindScheduleCancelled, AgentID: swarm.DefaultManagerID,
		Text: row.ID,
	})
}

// ListSchedules returns every stored wait. Filtering belongs to the inbox.
func (e *Engine) ListSchedules() ([]store.Schedule, error) {
	return e.store.ListSchedules()
}

// PatchSchedule pauses or resumes one wait. Cancel is CancelSchedule.
func (e *Engine) PatchSchedule(id, status string) (*store.Schedule, error) {
	status = strings.TrimSpace(status)
	switch status {
	case store.SchedulePaused, store.ScheduleActive:
	default:
		return nil, fmt.Errorf("engine: schedule patch is pause or resume")
	}
	row, err := e.store.GetSchedule(id)
	if err != nil {
		return nil, err
	}
	if row.Status == status {
		return row, nil
	}
	if row.Status != store.ScheduleActive && row.Status != store.SchedulePaused {
		return nil, fmt.Errorf("engine: schedule is not pauseable")
	}
	if status == store.ScheduleActive {
		got, err := e.store.ResumeScheduleUnderCap(id, e.cfg.Swarm.MaxActiveSchedules())
		if errors.Is(err, store.ErrScheduleCap) {
			return nil, fmt.Errorf("engine: too many active schedules")
		}
		return got, err
	}
	if err := e.store.UpdateSchedule(id, map[string]any{"status": status}); err != nil {
		return nil, err
	}
	return e.store.GetSchedule(id)
}
