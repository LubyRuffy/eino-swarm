package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// CompleteGoalTool is the manager-only tool that records an open standing
// objective as done. The TUI binds a local flag; the app binds the store.
func CompleteGoalTool(complete func(summary string) (string, error)) tool.BaseTool {
	if complete == nil {
		complete = func(string) (string, error) {
			return `{"ok":false,"error":"complete_goal is not wired"}`, nil
		}
	}
	return &completeGoalTool{complete: complete}
}

type completeGoalTool struct {
	complete func(summary string) (string, error)
}

func (t *completeGoalTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ToolCompleteGoal,
		Desc: "Record that the conversation's standing objective is actually satisfied. " +
			"The runtime then stops starting new turns for it. Call this only when " +
			"current evidence proves the objective itself is done — not to pause, " +
			"to end a turn, to wait, to record that a slice finished, to ask the " +
			"human a question, or because a single turn finished. A turn ends when " +
			"you stop calling tools. If you called this in error, call reopen_goal " +
			"before the turn ends.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"summary": {Type: schema.String,
				Desc: "optional one-line reason the objective is satisfied"},
		}),
	}, nil
}

func (t *completeGoalTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	var a struct {
		Summary string `json:"summary"`
	}
	if s := strings.TrimSpace(args); s != "" && s != "{}" {
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return goalToolFailure("could not read the arguments: %v", err), nil
		}
	}
	out, err := t.complete(a.Summary)
	if err != nil {
		return goalToolFailure("%s", err.Error()), nil
	}
	if strings.TrimSpace(out) == "" {
		return `{"ok":true}`, nil
	}
	return out, nil
}

func (e *Engine) completeGoalJSON(threadID, summary string) (string, error) {
	if err := e.CompleteThreadGoal(threadID, summary); err != nil {
		return goalToolFailure("%s", err.Error()), nil
	}
	body, _ := json.Marshal(map[string]any{"ok": true})
	return string(body), nil
}

// BlockGoalTool is the manager-only tool that records an open standing
// objective as stuck. Pause and resume stay with the human.
func BlockGoalTool(block func(reason string) (string, error)) tool.BaseTool {
	if block == nil {
		block = func(string) (string, error) {
			return `{"ok":false,"error":"block_goal is not wired"}`, nil
		}
	}
	return &blockGoalTool{block: block}
}

type blockGoalTool struct {
	block func(reason string) (string, error)
}

func (t *blockGoalTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ToolBlockGoal,
		Desc: "Record that the conversation's standing objective cannot make meaningful " +
			"progress without the human or an external change. Call this only after the " +
			"same genuine blocker has already repeated for at least three consecutive " +
			"turns, counting the original turn and automatic continuations. The runtime " +
			"then stops starting new turns until they resume. Do not call this to pause, " +
			"to ask a question, because a single attempt failed, because a turn finished, " +
			"or because the work is hard, slow, or uncertain. After a resume, treat the " +
			"run as a fresh blocked audit: only block again if the same obstacle recurs " +
			"for another three consecutive turns.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"reason": {Type: schema.String,
				Desc: "optional one-line reason progress is stuck"},
		}),
	}, nil
}

func (t *blockGoalTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	var a struct {
		Reason string `json:"reason"`
	}
	if s := strings.TrimSpace(args); s != "" && s != "{}" {
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return goalToolFailure("could not read the arguments: %v", err), nil
		}
	}
	out, err := t.block(a.Reason)
	if err != nil {
		return goalToolFailure("%s", err.Error()), nil
	}
	if strings.TrimSpace(out) == "" {
		return `{"ok":true}`, nil
	}
	return out, nil
}

func (e *Engine) blockGoalJSON(threadID, reason string) (string, error) {
	if err := e.BlockThreadGoal(threadID, reason); err != nil {
		return goalToolFailure("%s", err.Error()), nil
	}
	body, _ := json.Marshal(map[string]any{"ok": true})
	return string(body), nil
}

// ReopenGoalTool is the manager-only tool that undoes a complete_goal from
// this turn. The TUI binds a local flag; the app binds the store.
func ReopenGoalTool(reopen func(reason string) (string, error)) tool.BaseTool {
	if reopen == nil {
		reopen = func(string) (string, error) {
			return `{"ok":false,"error":"reopen_goal is not wired"}`, nil
		}
	}
	return &reopenGoalTool{reopen: reopen}
}

type reopenGoalTool struct {
	reopen func(reason string) (string, error)
}

func (t *reopenGoalTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: ToolReopenGoal,
		Desc: "Record that complete_goal was a mistake and the standing objective " +
			"is still open. The runtime then keeps starting turns for it. Call this " +
			"only after complete_goal in this turn, when the objective itself is " +
			"not actually satisfied. Do not call this to pause, to ask, or to start " +
			"new work.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"reason": {Type: schema.String,
				Desc: "optional one-line reason complete_goal was wrong"},
		}),
	}, nil
}

func (t *reopenGoalTool) InvokableRun(_ context.Context, args string, _ ...tool.Option) (string, error) {
	var a struct {
		Reason string `json:"reason"`
	}
	if s := strings.TrimSpace(args); s != "" && s != "{}" {
		if err := json.Unmarshal([]byte(args), &a); err != nil {
			return goalToolFailure("could not read the arguments: %v", err), nil
		}
	}
	out, err := t.reopen(a.Reason)
	if err != nil {
		return goalToolFailure("%s", err.Error()), nil
	}
	if strings.TrimSpace(out) == "" {
		return `{"ok":true}`, nil
	}
	return out, nil
}

func (e *Engine) reopenGoalJSON(threadID, reason string) (string, error) {
	if err := e.ReopenThreadGoal(threadID, reason); err != nil {
		return goalToolFailure("%s", err.Error()), nil
	}
	body, _ := json.Marshal(map[string]any{"ok": true})
	return string(body), nil
}

func goalToolFailure(format string, args ...any) string {
	body, _ := json.Marshal(map[string]any{
		"ok":    false,
		"error": fmt.Sprintf(format, args...),
	})
	return string(body)
}
