package swarm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func waitDelegationDone(t *testing.T, h *Handle) {
	t.Helper()
	select {
	case <-h.Done():
	case <-time.After(3 * time.Second):
		t.Fatalf("worker %s did not exit", h.ID)
	}
}

func delegationContext(h *Handle) context.Context {
	ctx := withToolCaller(context.Background(), AgentIdentity{AgentID: h.ID, Role: h.Role, Generation: h.generation(), ParentID: h.ParentID})
	h.mu.Lock()
	config := h.workerConfig
	h.mu.Unlock()
	if config != nil {
		ctx = withWorkerConfig(ctx, *config)
	}
	return ctx
}

func blockedDelegationRegistry(t *testing.T) (*Registry, map[string]*workerContextProbeModel) {
	t.Helper()
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	reg.EnableDelegation = true
	reg.SetMaxConcurrent(6)
	probes := map[string]*workerContextProbeModel{}
	reg.ModelBuilder = func(role, _ string) model.BaseChatModel { return probes[role] }
	return reg, probes
}

func spawnDelegationWorker(t *testing.T, reg *Registry, probes map[string]*workerContextProbeModel, role string, parent *Handle) *Handle {
	t.Helper()
	probe := &workerContextProbeModel{seen: make(chan context.Context, 1), release: make(chan struct{})}
	probes[role] = probe
	ctx := context.Background()
	if parent != nil {
		ctx = delegationContext(parent)
	}
	body, _ := json.Marshal(map[string]any{"role": role, "task": "inspect assigned input for " + role})
	out, err := reg.spawn(ctx, string(body))
	if err != nil || strings.Contains(out, `"error"`) {
		t.Fatalf("spawn %s: %s %v", role, out, err)
	}
	var receipt struct {
		ID string `json:"agent_id"`
	}
	if err := json.Unmarshal([]byte(out), &receipt); err != nil {
		t.Fatal(err)
	}
	h, ok := reg.get(receipt.ID)
	if !ok {
		t.Fatal("missing worker")
	}
	select {
	case <-probe.seen:
	case <-time.After(3 * time.Second):
		t.Fatalf("worker %s did not start", role)
	}
	return h
}

func TestDelegationSharesGlobalCapacityAndRefusesCrossTreeControlBeforeSideEffects(t *testing.T) {
	reg, probes := blockedDelegationRegistry(t)
	parent := spawnDelegationWorker(t, reg, probes, "parent", nil)
	peer := spawnDelegationWorker(t, reg, probes, "peer", nil)
	child := spawnDelegationWorker(t, reg, probes, "child", parent)
	grandchild := spawnDelegationWorker(t, reg, probes, "grandchild", child)
	reg.SetMaxConcurrent(4)
	ctx := delegationContext(parent)
	if out, err := reg.spawn(ctx, `{"role":"extra","task":"over capacity"}`); err != nil || !strings.Contains(out, "concurrency cap") {
		t.Fatalf("nested spawn escaped global cap: %s %v", out, err)
	}
	if reg.seq != 4 {
		t.Fatal("refused worker consumed an id")
	}
	for name, args := range map[string]string{
		"wait":   `{"agent_ids":["child-3","peer-2"],"timeout_s":1}`,
		"resume": `{"agent_id":"peer-2","task":"must not reach peer"}`,
		"close":  `{"agent_id":"peer-2"}`,
		"spawn":  `{"role":"peer","task":"must not steer peer"}`,
	} {
		var out string
		var err error
		switch name {
		case "wait":
			out, err = reg.wait(ctx, args)
		case "resume":
			out, err = reg.resume(ctx, args)
		case "close":
			out, err = reg.close(ctx, args)
		case "spawn":
			out, err = reg.spawn(ctx, args)
		}
		if err != nil || !strings.Contains(out, "outside its subtree") {
			t.Fatalf("%s crossed tree: %s %v", name, out, err)
		}
	}
	if cp := reg.SnapshotWorkers(); len(cp) != 4 || len(cp[1].Inbox) != 0 {
		t.Fatalf("refused calls changed worker state %+v", cp)
	}
	out, err := reg.close(context.Background(), `{"agent_id":"parent-1"}`)
	if err != nil || !strings.Contains(out, `"closed_agents"`) {
		t.Fatalf("close subtree %s %v", out, err)
	}
	for _, h := range []*Handle{parent, child, grandchild} {
		waitDelegationDone(t, h)
		if _, err, done := h.Result(); !done || err == nil {
			t.Fatalf("descendant %s was not cancelled", h.ID)
		}
	}
	if _, _, done := peer.Result(); done {
		t.Fatal("closing one subtree cancelled its peer")
	}
	if out, err := reg.spawn(ctx, `{"role":"late","task":"must not revive subtree"}`); err != nil || !strings.Contains(out, "no longer active") {
		t.Fatalf("closed parent spawned child %s %v", out, err)
	}
}

func TestFinishedParentKeepsLiveDescendantsAndCanCloseThemAfterArchivePruning(t *testing.T) {
	reg, probes := blockedDelegationRegistry(t)
	reg.ArchiveLimit = 1
	parent := spawnDelegationWorker(t, reg, probes, "parent", nil)
	child := spawnDelegationWorker(t, reg, probes, "child", parent)
	grandchild := spawnDelegationWorker(t, reg, probes, "grandchild", child)
	close(probes["parent"].release)
	waitDelegationDone(t, parent)
	close(probes["child"].release)
	waitDelegationDone(t, child)
	reg.Stats()
	for i := 0; i < 3; i++ {
		h, err := reg.Spawn(context.Background(), "temporary", "bounded work", finishedBindingBuilder)
		if err != nil {
			t.Fatal(err)
		}
		waitDelegationDone(t, h)
		reg.Stats()
	}
	if _, _, done := grandchild.Result(); done {
		t.Fatal("parent completion implicitly cancelled child work")
	}
	if _, ok := reg.past[parent.ID]; !ok {
		t.Fatal("archive pruned the ancestor needed to control live descendants")
	}
	out, err := reg.close(context.Background(), `{"agent_id":"parent-1"}`)
	if err != nil || !strings.Contains(out, `"already_finished":true`) {
		t.Fatalf("finished-parent close %s %v", out, err)
	}
	waitDelegationDone(t, grandchild)
	if result, err, done := parent.Result(); !done || err != nil || result != "complete" {
		t.Fatalf("closing descendants fabricated parent cancellation: %s %v %v", result, err, done)
	}
}

func TestDelegationKeepsGlobalRoleIdentityAndAllowsOnlyOwnDescendantResumption(t *testing.T) {
	reg, probes := blockedDelegationRegistry(t)
	parent := spawnDelegationWorker(t, reg, probes, "parent", nil)
	child := spawnDelegationWorker(t, reg, probes, "child", parent)
	close(probes["child"].release)
	waitDelegationDone(t, child)
	reg.ModelBuilder = finishedBindingBuilder
	out, err := reg.resume(delegationContext(parent), `{"agent_id":"child-2","task":"next assigned part"}`)
	if err != nil || !strings.Contains(out, `"agent_id":"child-2"`) {
		t.Fatalf("descendant resume %s %v", out, err)
	}
	waitDelegationDone(t, child)
	if child.generation() != 2 || child.ParentID != parent.ID {
		t.Fatal("resumption changed child identity or parent")
	}
	if out, err := reg.wait(delegationContext(parent), `{"agent_ids":["child-2"],"timeout_s":1}`); err != nil || !strings.Contains(out, `"generation":2`) {
		t.Fatalf("parent could not collect child %s %v", out, err)
	}
	disabled := reg.WorkerConfiguration()
	disabled.EnableDelegation = false
	if out, err := reg.spawn(withWorkerConfig(delegationContext(parent), disabled), `{"role":"new","task":"disabled delegation"}`); err != nil || !strings.Contains(out, "not enabled") {
		t.Fatalf("disabled worker delegated %s %v", out, err)
	}
}

func TestWorkerCallerCarriesItsActualParentAndFrozenGeneration(t *testing.T) {
	reg, probes := blockedDelegationRegistry(t)
	seen := make(chan AgentIdentity, 1)
	reg.SubAgentTools = []tool.BaseTool{&ctlTool{name: "provenance", desc: "record actual provenance", fn: func(ctx context.Context, _ string) (string, error) {
		caller, _ := ToolCaller(ctx)
		seen <- caller
		return "recorded", nil
	}}}
	reg.ModelBuilder = func(role, _ string) model.BaseChatModel {
		if role == "parent" {
			return probes[role]
		}
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("", []schema.ToolCall{{ID: "provenance-call", Type: "function", Function: schema.FunctionCall{Name: "provenance", Arguments: `{"parent_id":"forged"}`}}})
			},
			func(int, []*schema.Message) *schema.Message { return schema.AssistantMessage("complete", nil) },
		}}
	}
	parent := spawnDelegationWorker(t, reg, probes, "parent", nil)
	if out, err := reg.spawn(delegationContext(parent), `{"role":"child","task":"inspect source"}`); err != nil || strings.Contains(out, `"error"`) {
		t.Fatalf("spawn %s %v", out, err)
	}
	select {
	case got := <-seen:
		if got.ParentID != parent.ID || got.AgentID != "child-2" || got.Generation != 1 {
			t.Fatalf("forged/incomplete caller %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("child tool did not execute")
	}
}

// Cancel itself is deliberately intercepted at the actual snapshot/cancel gap.
// Cleanup must have revoked delegation before invoking any owned cancellation.
func TestCleanupRevokesDelegationBeforeCancellingItsSnapshot(t *testing.T) {
	reg, probes := blockedDelegationRegistry(t)
	parent := spawnDelegationWorker(t, reg, probes, "parent", nil)
	reg.ModelBuilder = finishedBindingBuilder
	ctx := delegationContext(parent)
	var attempted string
	parent.mu.Lock()
	cancel := parent.cancel
	parent.cancel = func() { attempted, _ = reg.spawn(ctx, `{"role":"late-child","task":"must not escape Stop"}`); cancel() }
	parent.mu.Unlock()
	if count := reg.Cleanup(); count != 1 {
		t.Fatalf("cleanup count=%d", count)
	}
	waitDelegationDone(t, parent)
	if !strings.Contains(attempted, `"error"`) || len(reg.SnapshotWorkers()) != 1 {
		t.Fatalf("child escaped the actual snapshot/cancel gap: %s workers=%+v", attempted, reg.SnapshotWorkers())
	}
}

func TestWorkerForkReceivesItsParentConversationAndNotManagerHistory(t *testing.T) {
	reg, probes := blockedDelegationRegistry(t)
	input := make(chan []*schema.Message, 1)
	reg.ModelBuilder = func(role, _ string) model.BaseChatModel {
		if role == "parent" {
			return probes[role]
		}
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{func(_ int, messages []*schema.Message) *schema.Message {
			input <- cloneMessages(messages)
			return schema.AssistantMessage("complete", nil)
		}}}
	}
	reg.SetHistory([]*schema.Message{schema.UserMessage("manager-only-history-marker")})
	parent := spawnDelegationWorker(t, reg, probes, "parent", nil)
	out, err := reg.spawn(delegationContext(parent), `{"role":"child","task":"inspect inherited evidence","fork_context":true}`)
	if err != nil || strings.Contains(out, `"error"`) {
		t.Fatalf("worker fork %s %v", out, err)
	}
	select {
	case messages := <-input:
		var text strings.Builder
		for _, message := range messages {
			text.WriteString(message.Content)
		}
		if strings.Contains(text.String(), "manager-only-history-marker") || !strings.Contains(text.String(), "inspect assigned input for parent") {
			t.Fatalf("wrong inherited conversation %s", text.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("child model input not observed")
	}
}

func TestDelegationAdmissionRechecksParentAfterModelSelectionAndCancellation(t *testing.T) {
	for _, mode := range []string{"close-parent", "close-registry", "cancel-tool"} {
		t.Run(mode, func(t *testing.T) {
			reg, probes := blockedDelegationRegistry(t)
			entered, release := make(chan struct{}), make(chan struct{})
			reg.SelectWorkerModel = func(context.Context, string) (ModelBinding, ModelBuilder, error) {
				close(entered)
				<-release
				return ModelBinding{ProviderID: "p", Model: "m"}, finishedBindingBuilder, nil
			}
			// The parent must start without invoking the intentionally blocked selector.
			probe := &workerContextProbeModel{seen: make(chan context.Context, 1), release: make(chan struct{})}
			probes["parent"] = probe
			parent, err := reg.Spawn(context.Background(), "parent", "coordinate work", reg.ModelBuilder)
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-probe.seen:
			case <-time.After(3 * time.Second):
				t.Fatal("parent did not start")
			}
			ctx, cancel := context.WithCancel(delegationContext(parent))
			defer cancel()
			result := make(chan string, 1)
			go func() {
				out, _ := reg.spawn(ctx, `{"role":"late-child","task":"inspect source","model_ref":"selected","fork_context":true}`)
				result <- out
			}()
			<-entered
			switch mode {
			case "close-parent":
				if _, err := reg.close(context.Background(), `{"agent_id":"parent-1"}`); err != nil {
					t.Fatal(err)
				}
			case "close-registry":
				reg.Close()
			case "cancel-tool":
				cancel()
			}
			close(release)
			select {
			case out := <-result:
				if !strings.Contains(out, `"error"`) {
					t.Fatalf("late delegation admitted %s", out)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("late spawn did not return")
			}
			if reg.seq != 1 {
				t.Fatal("late child consumed an identity")
			}
		})
	}
}

func TestDelegationRejectsForgedAdmissionAndCyclicRestoredParents(t *testing.T) {
	reg, probes := blockedDelegationRegistry(t)
	parent := spawnDelegationWorker(t, reg, probes, "parent", nil)
	if _, err := reg.spawnAgentWithModel(context.Background(), "forged", "inspect", finishedBindingBuilder, nil, parent.ID, "", true); err == nil {
		t.Fatal("parent_id without runtime caller was accepted")
	}
	if out, err := reg.close(context.Background(), `{"agent_id":"missing"}`); err != nil || !strings.Contains(out, "unknown agent") {
		t.Fatalf("unknown subtree %s %v", out, err)
	}
	if _, err := reg.Restore(context.Background(), RestoredWorker{ID: "self-2", Role: "self", ParentID: "self-2", Task: "restore"}, reg.ModelBuilder); err == nil {
		t.Fatal("self parent restored")
	}
	if err := reg.PlantFinished(FinishedWorker{ID: "self-2", ParentID: "self-2"}); err == nil {
		t.Fatal("self parent planted")
	}
	if err := reg.PlantFinished(FinishedWorker{ID: "a-2", ParentID: "b-3", Result: "complete"}); err != nil {
		t.Fatal(err)
	}
	if err := reg.PlantFinished(FinishedWorker{ID: "b-3", ParentID: "a-2", Result: "complete"}); err == nil {
		t.Fatal("cyclic restored tree accepted")
	}
	if identity := reg.workerIdentity("unknown"); identity.Generation != 0 || identity.AgentID != "unknown" {
		t.Fatalf("invented worker identity %+v", identity)
	}
	var absent *Registry
	ctx := context.Background()
	if got := absent.bindToolOutput(ctx, "worker", "worker", nil); got != ctx {
		t.Fatal("missing binder changed context")
	}
}

// Both initial validations deliberately happen before either claim. The claim
// lock must recheck the graph and publish ParentID before another restore sees it.
func TestConcurrentRestorationCannotPublishACyclicWorkerTree(t *testing.T) {
	reg := NewRegistry()
	defer reg.Close()
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	reg.ResolveWorkerConfig = func(context.Context, string) (WorkerConfig, error) {
		entered <- struct{}{}
		<-release
		return WorkerConfig{Tag: "restore-barrier", ModelBuilder: finishedBindingBuilder}, nil
	}
	results := make(chan error, 2)
	for _, worker := range []RestoredWorker{{ID: "first-1", Role: "first", ParentID: "second-2", Task: "restore first", ConfigTag: "restore-barrier"}, {ID: "second-2", Role: "second", ParentID: "first-1", Task: "restore second", ConfigTag: "restore-barrier"}} {
		go func(worker RestoredWorker) {
			h, err := reg.Restore(context.Background(), worker, nil)
			if err == nil {
				<-h.Done()
			}
			results <- err
		}(worker)
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("restoration never reached the shared barrier")
		}
	}
	close(release)
	succeeded, refused := 0, 0
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				succeeded++
			} else if strings.Contains(err.Error(), "cycle") {
				refused++
			} else {
				t.Fatal(err)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("restoration claim did not finish")
		}
	}
	if succeeded != 1 || refused != 1 || len(reg.SnapshotWorkers()) != 1 {
		t.Fatalf("cyclic claims succeeded=%d refused=%d workers=%+v", succeeded, refused, reg.SnapshotWorkers())
	}
	if err := reg.PlantFinished(FinishedWorker{ID: "existing-3", ParentID: "original-parent", Generation: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Restore(context.Background(), RestoredWorker{ID: "existing-3", Role: "existing", ParentID: "new-parent", Task: "changed relation"}, finishedBindingBuilder); err == nil {
		t.Fatal("restoration changed an existing worker's parent identity")
	}
}
