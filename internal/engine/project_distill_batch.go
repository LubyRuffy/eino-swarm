package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
)

const distillBatchName = "apply_project_distillation"

const projectDistillPrompt = `Review the supplied Project history once. Keep only useful durable changes.
Notes hold explicit project working preferences and constraints. Skills hold reusable, observed procedures, including verified recovery conditions. 
The input labels each original source with a unique review seq and its real conversation and event_seq. In evidence use the review seq and an exact quote from visible source bytes. Cite only original human input or successful paired tools for notes and Skills. From a failed turn, use only explicit human requirements for notes.
Keep each note short and preserve scope, conditions, negation and numeric spelling. For Skills, check an existing matching skill before changing it. Retain reusable, observed steps and separate normal and recovery conditions. Prior attempts, derived summaries and existing memory do not by themselves support new assertions.
Prefer updates to existing related content. Supply the changes array to apply_project_distillation. It can be empty; submit it empty when nothing warrants an update. After a refusal without writes, repair within the available rounds. One accepted proposal ends the review.`

type distillProposal struct {
	Changes *[]reviewOperation `json:"changes"`
}

type projectDistillBatch struct {
	e               *Engine
	work            *projectDistillWork
	mu              *sync.Mutex
	outcome         *reviewOutcome
	publishMu       sync.Mutex
	closed          bool
	memory          *reviewBatchTool
	complete        bool
	startedWrite    bool
	lastRefusal     string
	citationRefused bool
}

func (b *projectDistillBatch) Info(context.Context) (*schema.ToolInfo, error) {
	changes := reviewChangesParameter()
	if b.memory == nil {
		changes.Desc = "Memory is disabled. Supply an empty array."
	}
	return &schema.ToolInfo{
		Name: distillBatchName, Desc: "Submit a complete, source-bound project distillation proposal for project memory.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{"changes": changes}),
	}, nil
}

func (b *projectDistillBatch) InvokableRun(ctx context.Context, raw string, _ ...tool.Option) (string, error) {
	b.publishMu.Lock()
	defer b.publishMu.Unlock()
	if b.closed {
		return "", context.Canceled
	}
	b.mu.Lock()
	complete := b.complete
	b.mu.Unlock()
	if complete {
		return reviewBatchResult(true, "Project distillation already finished."), nil
	}
	if compose.GetToolCallID(ctx) == "" {
		return "", errors.New("project distillation requires a real tool call identity")
	}
	var proposal distillProposal
	if err := decodeToolArgs(raw, &proposal); err != nil {
		return b.refuse(err), nil
	}
	if proposal.Changes == nil {
		return b.refuse(errors.New("provide the changes array")), nil
	}
	handles, err := b.validateSources(proposal)
	if err != nil {
		return b.refuse(err), nil
	}
	response, committed, err := b.publish(ctx, proposal, handles)
	if err != nil && !committed {
		var terminal *reviewTerminalError
		if errors.As(err, &terminal) {
			return "", err
		}
		return b.refuse(err), nil
	}
	if !committed {
		return response, nil
	}
	b.mu.Lock()
	if err == nil && b.work.checkpoint != nil {
		err = b.work.checkpoint.accept(b.work.result, b.outcome.Changes, true)
	}
	b.complete = true
	b.outcome.StopReason = "completed"
	b.outcome.Note = "Project distillation finished."
	if err != nil {
		b.work.result.Error = publicTurnError(err)
	}
	b.mu.Unlock()
	if err := adk.SendToolGenAction(ctx, distillBatchName, adk.NewExitAction()); err != nil {
		return "", err
	}
	if err != nil {
		return reviewBatchResult(false, err.Error()), nil
	}
	return reviewBatchResult(true, "Project distillation finished."), nil
}

func (b *projectDistillBatch) refuse(err error) string {
	b.lastRefusal = err.Error()
	var repair *reviewRepairError
	b.citationRefused = errors.As(err, &repair)
	if b.citationRefused {
		// These excerpts were already admitted into this batch. Return them
		// only to the reviewer, keeping the receipt limited to its summary.
		result := maps.Clone(repair.feedback)
		result["success"], result["note"] = false, b.lastRefusal
		result["instruction"] = "Repair the identified operation using the listed citable review sequences and exact quotes from visible source text. Omit an unsupported operation only when there is no admissible supporting evidence. Keep the other supported operations and resubmit the changes array. This proposal saved nothing."
		raw, _ := json.Marshal(result)
		return string(raw)
	}
	return reviewBatchResult(false, err.Error())
}

func (b *projectDistillBatch) validateSources(p distillProposal) ([]int64, error) {
	if b.memory == nil && len(*p.Changes) > 0 {
		return nil, errors.New("Memory is disabled")
	}
	handles := make(map[int64]bool)
	for i, op := range *p.Changes {
		for _, cite := range op.Evidence {
			source, ok := b.work.sources.sources[cite.Seq]
			if !ok || (source.failed && op.Target != memory.ToolMemory) {
				return nil, fmt.Errorf("changes[%d]: source %d is outside this target's evidence", i, cite.Seq)
			}
			handles[cite.Seq] = true
		}
	}
	return slices.Sorted(maps.Keys(handles)), nil
}

func (b *projectDistillBatch) publish(ctx context.Context, proposal distillProposal, handles []int64) (response string, committed bool, err error) {
	done, err := b.e.beginLifecycleAdmission()
	if err != nil {
		return "", false, err
	}
	defer done()
	b.e.workspaceMu.Lock()
	defer b.e.workspaceMu.Unlock()
	b.e.recordMu.Lock()
	defer b.e.recordMu.Unlock()

	if err := b.e.checkDistillationAnchor(ctx, b.work); err != nil {
		return "", false, err
	}
	p, err := b.e.store.GetProject(b.work.projectID)
	if err != nil {
		return "", false, err
	}
	if len(*proposal.Changes) > 0 && !b.e.MemoryEnabled(p) {
		return "", false, errors.New("Memory was disabled during distillation")
	}
	if err := b.e.verifyDistillationSources(ctx, b.work, handles); err != nil {
		return "", false, err
	}
	if err := context.Cause(ctx); err != nil {
		return "", false, err
	}
	wroteBefore := b.startedWrite
	b.startedWrite = b.startedWrite || len(*proposal.Changes) > 0
	if b.work.checkpoint != nil && len(*proposal.Changes) > 0 {
		if err := b.work.checkpoint.beforeWrite(); err != nil {
			return "", false, &reviewTerminalError{cause: err}
		}
	}
	if len(*proposal.Changes) > 0 {
		body, err := json.Marshal(map[string]any{"changes": proposal.Changes})
		if err != nil {
			return "", false, err
		}
		result, err := b.memory.InvokableRun(ctx, string(body))
		var reply struct {
			Success         bool              `json:"success"`
			Error           string            `json:"error"`
			InvalidEvidence json.RawMessage   `json:"invalid_evidence"`
			Issues          []json.RawMessage `json:"issues"`
		}
		if err == nil {
			err = json.Unmarshal([]byte(result), &reply)
		}
		committed = b.outcome.Changed
		if err != nil {
			return "", committed, err
		}
		if !reply.Success {
			if !committed {
				b.startedWrite = wroteBefore
				b.lastRefusal = reply.Error
				// An unwritten batch refused only for its citations is worth one fresh reviewer.
				b.citationRefused = len(reply.InvalidEvidence) > 0 || slices.ContainsFunc(reply.Issues, func(issue json.RawMessage) bool {
					return bytes.Contains(issue, []byte(`"invalid_evidence"`))
				})
				return result, false, nil
			}
			return "", true, fmt.Errorf("Memory partially saved: %s", reply.Error)
		}
	}
	return "", true, nil
}
