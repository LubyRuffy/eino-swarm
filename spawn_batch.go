package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type spawnArgs struct {
	Role        string `json:"role"`
	Task        string `json:"task"`
	ForkContext bool   `json:"fork_context"`
}

func readSpawnArgs(args string) (spawnArgs, string) {
	var a spawnArgs
	if err := json.Unmarshal([]byte(args), &a); err != nil {
		return a, "spawn_agent: could not read the arguments"
	}
	a.Role = strings.TrimSpace(a.Role)
	a.Task = strings.TrimSpace(a.Task)
	if a.Role == "" {
		return a, "spawn_agent: role is required"
	}
	if a.Task == "" {
		return a, "spawn_agent: task is required"
	}
	return a, ""
}

// The retained call keeps its original id and arguments. Only calls that
// cannot create a worker are coalesced; valid calls and other tools survive.
type spawnRefusal struct {
	Error         string `json:"error"`
	RejectedCalls int    `json:"rejected_calls"`
	Hint          string `json:"hint"`
}

type spawnBatchFeedback struct {
	mu   sync.Mutex
	byID map[string]spawnRefusal
}

func (f *spawnBatchFeedback) filter(msg *schema.Message) *schema.Message {
	if msg == nil || len(msg.ToolCalls) == 0 {
		return msg
	}
	out := *msg
	out.ToolCalls = make([]schema.ToolCall, 0, len(msg.ToolCalls))
	groups := map[string]string{}
	refusals := map[string]spawnRefusal{}
	for _, call := range msg.ToolCalls {
		problem := ""
		if call.Function.Name == "spawn_agent" {
			_, problem = readSpawnArgs(call.Function.Arguments)
		}
		if problem == "" || call.ID == "" {
			out.ToolCalls = append(out.ToolCalls, call)
			continue
		}
		id, exists := groups[problem]
		if !exists {
			id = call.ID
			groups[problem] = id
			out.ToolCalls = append(out.ToolCalls, call)
		}
		r := refusals[id]
		r.Error = problem
		r.RejectedCalls++
		r.Hint = "These rejected calls did not create, steer, resume or stop a worker. Send one corrected call with both role and task; do not repeat incomplete calls or invent agent ids."
		refusals[id] = r
	}
	f.mu.Lock()
	f.byID = refusals
	f.mu.Unlock()
	return &out
}

func (f *spawnBatchFeedback) take(id string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[id]
	delete(f.byID, id)
	if !ok {
		return "", false
	}
	return marshal(r), true
}

// Tool arguments may arrive in separate chunks. Wait for the full call before
// deciding it lacks task; otherwise a valid fragmented call would be refused.
// Answer/reasoning chunks still stream immediately, as does the first tool's
// progress preview. No partial tool batch is executed after a stream error.
type spawnBatchModel struct {
	inner    model.BaseModel[*schema.Message]
	feedback *spawnBatchFeedback
	reg      *Registry
}

func (m *spawnBatchModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	out, err := m.inner.Generate(ctx, input, opts...)
	if err != nil {
		return out, err
	}
	return m.feedback.filter(out), nil
}

func (m *spawnBatchModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	sr, err := m.inner.Stream(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	out, sw := schema.Pipe[*schema.Message](8)
	go func() {
		defer sw.Close()
		defer sr.Close()
		var calls, previewCalls []schema.ToolCall
		preview := &streamAcc{reg: m.reg, agentID: DefaultManagerID, role: "manager"}
		first := ""
		for {
			msg, recvErr := sr.Recv()
			if recvErr != nil {
				if ctx.Err() != nil {
					sw.Send(nil, ctx.Err())
					return
				}
				if !errors.Is(recvErr, io.EOF) {
					sw.Send(nil, recvErr)
					return
				}
				break
			}
			if msg == nil {
				continue
			}
			calls = append(calls, msg.ToolCalls...)
			for _, call := range msg.ToolCalls {
				key := call.ID
				if call.Index != nil {
					key = "index:" + itoa(*call.Index)
				}
				if first == "" {
					first = key
				}
				if key == first {
					previewCalls = append(previewCalls, call)
				}
			}
			if m.reg != nil {
				preview.noteStreamingTools(previewCalls)
			}
			copy := *msg
			copy.ToolCalls = nil
			if sw.Send(&copy, nil) {
				return
			}
		}
		if len(calls) > 0 {
			msg := m.feedback.filter(schema.AssistantMessage("", mergeStreamedToolCalls(calls)))
			sw.Send(msg, nil)
		}
	}()
	return out, nil
}
