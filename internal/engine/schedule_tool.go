package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Tool names travel in transcripts and Trace. Renaming one is a protocol change.
const (
	ToolScheduleWake   = "schedule_wake"
	ToolScheduleTask   = "schedule_task"
	ToolCancelSchedule = "cancel_schedule"
	ToolReportSchedule = "report_schedule"
)

const (
	scheduleWakeDesc = "Arm or replace a wait on this conversation. Call this when progress is gated on time or a condition that is not worth polling inside this turn, then end the turn. Do not schedule work that can finish now. Do not use a wait instead of asking the human."
	scheduleTaskDesc = "Arm an independent job that mints its own conversation when it fires. Only from a human-originated turn — never from a continuation, a scheduled check, or an accepted-plan execute turn."
	cancelSchedDesc  = "Cancel a wait by id so it stops firing."
	reportSchedDesc  = "Report the result of a scheduled check. Empty findings mean nothing to surface. Call this only on a scheduled turn."
)

func scheduleCadenceParams() map[string]*schema.ParameterInfo {
	return map[string]*schema.ParameterInfo{
		"prompt":   {Type: schema.String, Required: true, Desc: "instruction for each fire"},
		"delay_s":  {Type: schema.Integer, Desc: "one-shot wait in seconds"},
		"every_s":  {Type: schema.Integer, Desc: "interval in seconds"},
		"cron":     {Type: schema.String, Desc: "5-field expression in the host timezone"},
		"title":    {Type: schema.String, Desc: "optional short label"},
		"max_runs": {Type: schema.Integer, Desc: "optional cap; 0 means until cancelled"},
		"until":    {Type: schema.String, Desc: "optional RFC3339 end time"},
	}
}

// ScheduleWakeTool upserts a thread wake on the current conversation.
func ScheduleWakeTool(run func(args string) (string, error)) tool.BaseTool {
	params := scheduleCadenceParams()
	params["id"] = &schema.ParameterInfo{Type: schema.String, Desc: "optional id of an existing wait on this conversation to replace"}
	return newScheduleTool(ToolScheduleWake, scheduleWakeDesc, params, run)
}

// ScheduleTaskTool creates a standalone job. The handler must refuse
// synthetic turns (goal continue, schedule continue, plan implement).
func ScheduleTaskTool(run func(args string) (string, error)) tool.BaseTool {
	return newScheduleTool(ToolScheduleTask, scheduleTaskDesc, scheduleCadenceParams(), run)
}

// CancelScheduleTool stops a wait by id.
func CancelScheduleTool(run func(args string) (string, error)) tool.BaseTool {
	return newScheduleTool(ToolCancelSchedule, cancelSchedDesc, map[string]*schema.ParameterInfo{
		"id": {Type: schema.String, Required: true, Desc: "the wait to cancel"},
	}, run)
}

// ReportScheduleTool records findings for a scheduled turn. Empty findings
// are quiet; the finish path (later) is what archives them.
func ReportScheduleTool(run func(args string) (string, error)) tool.BaseTool {
	return newScheduleTool(ToolReportSchedule, reportSchedDesc, map[string]*schema.ParameterInfo{
		"findings":  {Type: schema.String, Desc: "what changed; empty means nothing to surface"},
		"keep":      {Type: schema.Boolean, Desc: "keep firing; false cancels the wait. Default true"},
		"next_in_s": {Type: schema.Integer, Desc: "optional new interval in seconds for the next fire"},
	}, run)
}

func newScheduleTool(name, desc string, params map[string]*schema.ParameterInfo, run func(string) (string, error)) tool.BaseTool {
	if run == nil {
		run = func(string) (string, error) {
			return scheduleToolFailure("%s is not wired", name), nil
		}
	}
	return &jsonArgsTool{name: name, desc: desc, params: params, run: run}
}

type jsonArgsTool struct {
	name, desc string
	params     map[string]*schema.ParameterInfo
	run        func(args string) (string, error)
}

func (t *jsonArgsTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name:        t.name,
		Desc:        t.desc,
		ParamsOneOf: schema.NewParamsOneOfByParams(t.params),
	}, nil
}

func (t *jsonArgsTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	out, err := t.run(args)
	if err != nil {
		return scheduleToolFailure("%s", err.Error()), nil
	}
	if strings.TrimSpace(out) == "" {
		return `{"ok":true}`, nil
	}
	return out, nil
}

func denyScheduleTools() []tool.BaseTool {
	deny := func(string) (string, error) {
		return scheduleToolFailure("%s", "workers cannot schedule"), nil
	}
	return []tool.BaseTool{
		ScheduleWakeTool(deny),
		ScheduleTaskTool(deny),
		CancelScheduleTool(deny),
		ReportScheduleTool(deny),
	}
}

func (rt *runtime) managerScheduleTools(turn *store.Turn) []tool.BaseTool {
	e := rt.engine
	tid := rt.threadID
	turnID := ""
	if turn != nil {
		turnID = turn.ID
	}
	return []tool.BaseTool{
		ScheduleWakeTool(func(args string) (string, error) {
			return e.scheduleWakeJSON(tid, turnID, args)
		}),
		ScheduleTaskTool(func(args string) (string, error) {
			return e.scheduleTaskJSON(tid, turnID, args)
		}),
		CancelScheduleTool(func(args string) (string, error) {
			return e.cancelScheduleJSON(tid, turnID, args)
		}),
		ReportScheduleTool(func(args string) (string, error) {
			return e.reportScheduleJSON(tid, turnID, args)
		}),
	}
}

type scheduleToolArgs struct {
	ID       string `json:"id"`
	Prompt   string `json:"prompt"`
	Title    string `json:"title"`
	DelayS   int    `json:"delay_s"`
	EveryS   int    `json:"every_s"`
	Cron     string `json:"cron"`
	MaxRuns  int    `json:"max_runs"`
	Until    string `json:"until"`
	Findings string `json:"findings"`
	Keep     *bool  `json:"keep"`
	NextInS  int    `json:"next_in_s"`
}

func parseScheduleToolArgs(args string) (scheduleToolArgs, error) {
	var a scheduleToolArgs
	s := strings.TrimSpace(args)
	if s == "" || s == "{}" {
		return a, nil
	}
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return a, fmt.Errorf("could not read the arguments: %v", err)
	}
	return a, nil
}

func parseUntilAt(raw string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, fmt.Errorf("until must be RFC3339")
	}
	u := t.UTC()
	return &u, nil
}

func (e *Engine) scheduleInputFromArgs(a scheduleToolArgs) (ScheduleInput, error) {
	until, err := parseUntilAt(a.Until)
	if err != nil {
		return ScheduleInput{}, err
	}
	prompt := strings.TrimSpace(a.Prompt)
	if prompt == "" {
		return ScheduleInput{}, fmt.Errorf("a schedule needs a prompt")
	}
	return ScheduleInput{
		Prompt:  prompt,
		Title:   strings.TrimSpace(a.Title),
		DelayS:  a.DelayS,
		EveryS:  a.EveryS,
		Cron:    strings.TrimSpace(a.Cron),
		MaxRuns: a.MaxRuns,
		UntilAt: until,
	}, nil
}

func (e *Engine) scheduleWakeJSON(threadID, _, args string) (string, error) {
	a, err := parseScheduleToolArgs(args)
	if err != nil {
		return scheduleToolFailure("%s", err.Error()), nil
	}
	in, err := e.scheduleInputFromArgs(a)
	if err != nil {
		return scheduleToolFailure("%s", err.Error()), nil
	}
	in.Kind = store.ScheduleThread
	in.ThreadID = threadID
	in.OriginThreadID = threadID
	in.CreatedBy = store.ScheduleCreatedManager
	id := strings.TrimSpace(a.ID)
	var row *store.Schedule
	if id != "" {
		row, err = e.replaceThreadWake(threadID, id, in)
	} else {
		row, err = e.CreateSchedule(in)
	}
	if err != nil {
		return scheduleToolFailure("%s", err.Error()), nil
	}
	return scheduleArmedOK(row.ID), nil
}

func (e *Engine) replaceThreadWake(threadID, id string, in ScheduleInput) (*store.Schedule, error) {
	row, err := e.store.GetSchedule(id)
	if err != nil {
		return nil, err
	}
	if row.Kind != store.ScheduleThread || row.ThreadID != threadID {
		return nil, fmt.Errorf("engine: that wait is not a wake on this conversation")
	}
	if row.Status != store.ScheduleActive && row.Status != store.SchedulePaused {
		return nil, fmt.Errorf("engine: that wait is no longer active")
	}
	spec, err := parseScheduleSpec(in.DelayS, in.EveryS, in.Cron, e.cfg.Swarm.ScheduleMinInterval())
	if err != nil {
		return nil, err
	}
	next, err := firstRunAt(spec, time.Now().UTC())
	if err != nil {
		return nil, err
	}
	if err := e.store.UpdateSchedule(id, map[string]any{
		"prompt":      in.Prompt,
		"title":       in.Title,
		"delay_s":     in.DelayS,
		"every_s":     in.EveryS,
		"cron":        in.Cron,
		"max_runs":    in.MaxRuns,
		"until_at":    in.UntilAt,
		"next_run_at": next,
	}); err != nil {
		return nil, err
	}
	got, err := e.store.GetSchedule(id)
	if err != nil {
		return nil, err
	}
	e.recordScheduleArmed(got)
	return got, nil
}

func (e *Engine) scheduleTaskJSON(threadID, turnID, args string) (string, error) {
	if err := e.requireHumanOriginatedTurn(threadID, turnID); err != nil {
		return scheduleToolFailure("%s", err.Error()), nil
	}
	a, err := parseScheduleToolArgs(args)
	if err != nil {
		return scheduleToolFailure("%s", err.Error()), nil
	}
	in, err := e.scheduleInputFromArgs(a)
	if err != nil {
		return scheduleToolFailure("%s", err.Error()), nil
	}
	in.Kind = store.ScheduleStandalone
	in.OriginThreadID = threadID
	in.CreatedBy = store.ScheduleCreatedManager
	if th, err := e.store.GetThread(threadID); err == nil {
		in.ProjectID = th.ProjectID
		in.ProviderID = th.ProviderID
		in.Model = th.Model
		in.ReasoningEffort = th.ReasoningEffort
	}
	row, err := e.CreateSchedule(in)
	if err != nil {
		return scheduleToolFailure("%s", err.Error()), nil
	}
	return scheduleArmedOK(row.ID), nil
}

func (e *Engine) requireHumanOriginatedTurn(threadID, turnID string) error {
	turn, err := e.store.GetTurn(turnID)
	if err != nil {
		return err
	}
	if turn.ThreadID != threadID {
		return fmt.Errorf("engine: that turn is not on this conversation")
	}
	if turn.GoalContinue {
		return fmt.Errorf("engine: a standing-objective continuation cannot arm an independent job")
	}
	if turn.ScheduleContinue {
		return fmt.Errorf("engine: a scheduled check cannot arm an independent job")
	}
	// occupy() on resume wipes the in-memory flag. KindPlanImplemented is
	// what survived the crash; do not also require currentTurnID or a
	// leftover planImplement bit.
	if e.runtimeFor(threadID).recordingPlanImplement() || e.turnHasKind(turnID, KindPlanImplemented) {
		return fmt.Errorf("engine: an accepted-plan execute turn cannot arm an independent job")
	}
	return nil
}

func (e *Engine) cancelScheduleJSON(_, _, args string) (string, error) {
	a, err := parseScheduleToolArgs(args)
	if err != nil {
		return scheduleToolFailure("%s", err.Error()), nil
	}
	id := strings.TrimSpace(a.ID)
	if id == "" {
		return scheduleToolFailure("%s", "id is required"), nil
	}
	if err := e.CancelSchedule(id); err != nil {
		return scheduleToolFailure("%s", err.Error()), nil
	}
	body, _ := json.Marshal(map[string]any{"ok": true, "id": id})
	return string(body), nil
}

func (e *Engine) reportScheduleJSON(threadID, turnID, args string) (string, error) {
	turn, err := e.store.GetTurn(turnID)
	if err != nil {
		return scheduleToolFailure("%s", err.Error()), nil
	}
	if turn.ThreadID != threadID {
		return scheduleToolFailure("%s", "that turn is not on this conversation"), nil
	}
	if !turn.ScheduleContinue {
		return scheduleToolFailure("%s", "report_schedule is only for a scheduled check"), nil
	}
	a, err := parseScheduleToolArgs(args)
	if err != nil {
		return scheduleToolFailure("%s", err.Error()), nil
	}
	if a.NextInS != 0 {
		if _, err := parseScheduleSpec(0, a.NextInS, "", e.cfg.Swarm.ScheduleMinInterval()); err != nil {
			return scheduleToolFailure("%s", err.Error()), nil
		}
	}
	findings := strings.TrimSpace(a.Findings)
	quiet := findings == ""
	if sch := e.scheduleForTurn(turn); sch != nil {
		if a.Keep != nil && !*a.Keep {
			if err := e.CancelSchedule(sch.ID); err != nil {
				return scheduleToolFailure("%s", err.Error()), nil
			}
		} else if a.NextInS != 0 {
			next := time.Now().UTC().Add(time.Duration(a.NextInS) * time.Second)
			if err := e.store.UpdateSchedule(sch.ID, map[string]any{
				"every_s":     a.NextInS,
				"delay_s":     0,
				"cron":        "",
				"next_run_at": next,
			}); err != nil {
				return scheduleToolFailure("%s", err.Error()), nil
			}
		}
	}
	payload, _ := json.Marshal(struct {
		Findings string `json:"findings"`
		Quiet    bool   `json:"quiet"`
	}{Findings: findings, Quiet: quiet})
	e.record(store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: KindScheduleReport, AgentID: swarm.DefaultManagerID,
		Text: string(payload),
	})
	body, _ := json.Marshal(map[string]any{"ok": true, "quiet": quiet})
	return string(body), nil
}

func (e *Engine) scheduleForTurn(turn *store.Turn) *store.Schedule {
	if turn == nil || turn.ScheduleRunID == "" {
		return nil
	}
	run, err := e.store.GetRun(turn.ScheduleRunID)
	if err != nil {
		return nil
	}
	sch, err := e.store.GetSchedule(run.ScheduleID)
	if err != nil {
		return nil
	}
	return sch
}

func scheduleArmedOK(id string) string {
	body, _ := json.Marshal(map[string]any{"ok": true, "id": id})
	return string(body)
}

func scheduleToolFailure(format string, args ...any) string {
	body, _ := json.Marshal(map[string]any{
		"ok":    false,
		"error": fmt.Sprintf(format, args...),
	})
	return string(body)
}
