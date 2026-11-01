package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const referenceDescription = " Repeated completed results may replace result with result_ref {tool_call_id,agent_id,generation,result_error_sha256}; it identifies the exact earlier full result in this same model input. The SHA-256 hashes the JSON array [result,error]. Resolve it from that earlier entry; it is not a newly collected result or a reference to another request."

type roundResult struct {
	Final       string  `json:"final"`
	Error       string  `json:"error"`
	WallSeconds float64 `json:"wall_s"`
}

type sessionResult struct {
	Arm             string        `json:"arm"`
	Error           string        `json:"error"`
	ScopeViolations []string      `json:"scope_violations"`
	Rounds          []roundResult `json:"rounds"`
	Calls           []callUsage   `json:"calls"`
	InputMetrics    inputMetrics  `json:"input_metrics"`
	Cleanup         struct {
		RegistryClosed bool `json:"registry_closed"`
		LiveWorkers    int  `json:"live_workers"`
	} `json:"cleanup"`
	CostComplete bool   `json:"cost_complete"`
	Boundary     string `json:"boundary"`
}

type guardedTool struct {
	inner tool.InvokableTool
	name  string
	audit *audit
}

func (t *guardedTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	info, err := t.inner.Info(ctx)
	if err != nil {
		return nil, err
	}
	copy := *info
	// The experimental protocol is common to both arms. Only the outgoing
	// model-input projection differs; tool descriptions are not a treatment.
	if t.name == "wait_agents" {
		copy.Desc += referenceDescription
	}
	return &copy, nil
}

func (t *guardedTool) InvokableRun(ctx context.Context, arguments string, opts ...tool.Option) (string, error) {
	allowed := t.name == "wait_agents"
	var result string
	var err error
	if !allowed {
		result = `{"error":"tool operation is outside this experiment's allowed scope"}`
		t.audit.mu.Lock()
		t.audit.Violations = append(t.audit.Violations, t.name+":"+arguments)
		t.audit.mu.Unlock()
	} else {
		result, err = t.inner.InvokableRun(ctx, arguments, opts...)
	}
	errText := ""
	if err != nil {
		errText = err.Error()
	}
	t.audit.mu.Lock()
	t.audit.Receipts = append(t.audit.Receipts, toolReceipt{Tool: t.name, CallID: compose.GetToolCallID(ctx), Arguments: arguments, Result: result, Error: errText})
	t.audit.mu.Unlock()
	return result, err
}

func executeSession(ctx context.Context, f *fixture, arm string, base model.BaseChatModel, log *audit, maxIterations int, roundTimeout time.Duration) (sessionResult, []*schema.Message, error) {
	result := sessionResult{Arm: arm, Rounds: []roundResult{}, Boundary: "Registry/ADK outgoing model input; no HTTP engine, scheduler, notification or process-recovery acceptance"}
	reg := swarm.NewRegistry()
	defer reg.Close()
	if err := plantWorkers(reg, f.Workers); err != nil {
		return result, nil, err
	}
	modelAdapter := &contextModel{inner: base, arm: arm, audit: log}
	manager := reg.ManagerConfig(swarm.DefaultManagerID, "worker-result context experiment", modelAdapter,
		swarm.WithInstruction(f.Instruction), swarm.WithMaxIterations(maxIterations))
	for index, baseTool := range manager.ToolsConfig.ToolsNodeConfig.Tools {
		invokable, ok := baseTool.(tool.InvokableTool)
		if !ok {
			return result, nil, errors.New("experiment received a non-invokable tool")
		}
		info, err := baseTool.Info(ctx)
		if err != nil {
			return result, nil, err
		}
		manager.ToolsConfig.ToolsNodeConfig.Tools[index] = &guardedTool{inner: invokable, name: info.Name, audit: log}
	}
	agent, err := adk.NewChatModelAgent(ctx, manager)
	if err != nil {
		return result, nil, err
	}
	history := initialMessages(f)
	inputs := f.Rounds
	if len(inputs) == 0 {
		inputs = append(inputs, struct {
			User string `json:"user"`
		}{})
	}
	for _, input := range inputs {
		if input.User != "" {
			history = append(history, schema.UserMessage(input.User))
		}
		started := time.Now()
		roundContext, cancel := context.WithTimeout(ctx, roundTimeout)
		final, runErr := driveAgent(roundContext, agent, history)
		cancel()
		round := roundResult{Final: final, WallSeconds: time.Since(started).Seconds()}
		if runErr != nil {
			round.Error, result.Error = runErr.Error(), runErr.Error()
		}
		result.Rounds = append(result.Rounds, round)
		history = nil
		for _, message := range reg.ManagerHistory() {
			if message.Role != schema.System {
				history = append(history, message)
			}
		}
		if runErr != nil {
			break
		}
	}
	result.Cleanup.LiveWorkers = 0
	for _, worker := range reg.Progress() {
		if worker.Running {
			result.Cleanup.LiveWorkers++
		}
	}
	transcript := reg.ManagerHistory()
	reg.Close()
	result.Cleanup.RegistryClosed = true
	finalAudit := log.snapshot()
	result.ScopeViolations, result.Calls, result.InputMetrics = finalAudit.Violations, finalAudit.Calls, finalAudit.Metrics
	if result.ScopeViolations == nil {
		result.ScopeViolations = []string{}
	}
	if result.Calls == nil {
		result.Calls = []callUsage{}
	}
	result.CostComplete = result.Error == "" && len(result.Calls) > 0 && len(result.Calls) == result.InputMetrics.Requests
	for _, call := range result.Calls {
		if call.TotalTokens <= 0 {
			result.CostComplete = false
		}
	}
	return result, transcript, nil
}

func driveAgent(ctx context.Context, agent adk.Agent, messages []adk.Message) (string, error) {
	iterator := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: true}).Run(ctx, messages)
	final := ""
	for {
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			return final, event.Err
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		output := event.Output.MessageOutput
		message := output.Message
		if output.IsStreaming && output.MessageStream != nil {
			var chunks []*schema.Message
			for {
				chunk, err := output.MessageStream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					output.MessageStream.Close()
					return final, err
				}
				chunks = append(chunks, chunk)
			}
			output.MessageStream.Close()
			if len(chunks) > 0 {
				var err error
				message, err = schema.ConcatMessages(chunks)
				if err != nil {
					return final, err
				}
			}
		}
		if message != nil && message.Role == schema.Assistant && len(message.ToolCalls) == 0 && message.Content != "" {
			final = message.Content
		}
	}
	return final, ctx.Err()
}

func privateFile(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("write evidence: %w", writeErr)
	}
	return closeErr
}
