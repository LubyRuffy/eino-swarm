package engine

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/compose"
)

func decodeGraphArgs(args string, value any) error {
	if strings.TrimSpace(args) == "" {
		args = "{}"
	}
	dec := json.NewDecoder(strings.NewReader(args))
	dec.DisallowUnknownFields()
	if err := dec.Decode(value); err != nil {
		return fmt.Errorf("%w: invalid tool arguments: %v", graph.ErrInvalid, err)
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("%w: tool arguments must contain one JSON object", graph.ErrInvalid)
	}
	return nil
}

// graphCaller resolves who is calling from runtime provenance only. Nothing in
// the model's arguments selects the conversation, the turn or the agent.
func (e *Engine) graphCaller(ctx context.Context, turn *store.Turn) (store.GraphCaller, string, error) {
	if err := context.Cause(ctx); err != nil {
		return store.GraphCaller{}, "", err
	}
	if !e.graphEnabled(turn) {
		return store.GraphCaller{}, "", ErrGraphDisabled
	}
	owner, ok := ctx.Value(historyReadOwnerKey{}).(historyReadOwner)
	if !ok || owner.ThreadID != turn.ThreadID || owner.TurnID != turn.ID {
		return store.GraphCaller{}, "", fmt.Errorf("graph call has no matching trusted turn owner")
	}
	identity, ok := swarm.ToolCaller(ctx)
	callID := compose.GetToolCallID(ctx)
	if !ok || identity.AgentID == "" {
		// A graph replay is a host-driven pass over one stored round. The
		// middleware below stamps the turn; the model cannot supply it.
		replay, replayed := ctx.Value(graphReplayCallerKey{}).(graphReplayCaller)
		if !replayed || replay.turnID != turn.ID || replay.callID == "" {
			return store.GraphCaller{}, "", fmt.Errorf("graph calls require an actual agent invocation")
		}
		identity = swarm.AgentIdentity{AgentID: swarm.DefaultManagerID, Role: "manager"}
		callID = replay.callID
	}
	if callID == "" {
		return store.GraphCaller{}, "", fmt.Errorf("graph calls require a real tool call identity")
	}
	return store.GraphCaller{
		ThreadID: turn.ThreadID, TurnID: turn.ID, AgentID: identity.AgentID, Generation: identity.Generation,
		ProjectID: turn.ProjectID, CurrentToolCallID: callID, ToolReceiptScope: swarm.ToolReceiptScope(ctx),
	}, callID, nil
}

// lockGraphWrite orders a write against settings changes and turn removal the
// way other durable model writes are ordered.
func (e *Engine) lockGraphWrite(ctx context.Context, turnID string, replay bool) (func(), error) {
	e.configUpdateMu.Lock()
	if settings := e.cfg.SwarmSnapshot(); !settings.Graph || (!replay && !settings.GraphAutoRecord) {
		e.configUpdateMu.Unlock()
		return nil, ErrGraphDisabled
	}
	e.recordMu.Lock()
	unlock := func() {
		e.recordMu.Unlock()
		e.configUpdateMu.Unlock()
	}
	if _, dropped := e.droppedTurns[turnID]; dropped {
		unlock()
		return nil, store.ErrNotFound
	}
	if err := context.Cause(ctx); err != nil {
		unlock()
		return nil, err
	}
	return unlock, nil
}

func (e *Engine) updateGraphTool(ctx context.Context, turn *store.Turn, args string) (string, error) {
	caller, callID, err := e.graphCaller(ctx, turn)
	if err != nil {
		return "", err
	}
	if turn.GraphReadOnly {
		return "", fmt.Errorf("%w: this turn is read-only", graph.ErrInvalid)
	}
	var batch graph.Batch
	if err := decodeGraphArgs(args, &batch); err != nil {
		return "", err
	}
	// A replay has no tool results of its own. Confirming or closing a to-do
	// would refuse the whole batch, and the round would leave the graph unchanged.
	if turn.GraphReplay {
		softenReplayBatch(&batch)
	}
	// Ill-formed evidence would refuse the whole batch and make the model
	// resend it. Drop it and cite the caller's own completed tool results.
	prepareGraphEvidence(&batch)
	unlock, err := e.lockGraphWrite(ctx, turn.ID, turn.GraphReplay)
	if err != nil {
		return "", err
	}
	defer unlock()
	dropped := e.relaxGraphBatch(caller.OwnerID(), &batch)
	if err := batch.Valid(); err != nil {
		return "", err
	}
	hash, err := store.HashGraphBatch(batch)
	if err != nil {
		return "", err
	}
	callerIdentity := []any{turn.ThreadID, turn.ID, caller.AgentID, caller.Generation, callID}
	if replay, ok := ctx.Value(graphReplayCallerKey{}).(graphReplayCaller); turn.GraphReplay && ok {
		// Each source round has its own model conversation and may reuse a call ID.
		callerIdentity = append(callerIdentity, replay.sourceTurnID)
	}
	identity, err := json.Marshal(callerIdentity)
	if err != nil {
		return "", err
	}
	caller.ReceiptKey, caller.ReceiptHash = fmt.Sprintf("%x", sha256.Sum256(identity)), hash
	// The receipt hash is the batch before a repair, so a retry of the same
	// call replays instead of conflicting with the repaired body.
	held, err := alignGraphTodos(ctx, e.store, caller.OwnerID(), &batch)
	if err != nil {
		return "", err
	}
	known, err := e.knownGraphSubjects(ctx, caller.OwnerID(), &batch)
	if err != nil {
		return "", err
	}
	retractLeadsLinkedToConfirmed(&batch, known)
	if held {
		// Validate the final batch before showing progress. A refusal must not
		// leave the in-progress frame, or the rows behind it, in the database.
		if err := e.store.ProbeGraph(ctx, caller, batch); err != nil {
			if errors.Is(err, store.ErrOwnToolReceiptUnavailable) && batch.OwnToolCallIDs != nil {
				batch.OwnToolCallIDs, batch.CiteOwnResults = nil, true
				err = e.store.ProbeGraph(ctx, caller, batch)
			}
			if err != nil {
				return "", err
			}
		}
		base, err := e.store.ReadGraph(ctx, caller.OwnerID(), store.GraphQuery{All: true, Limit: -1})
		if err != nil {
			return "", err
		}
		e.showGraphPreview(caller.OwnerID(), previewGraph(base, batch))
		defer e.clearGraphPreview(caller.OwnerID())
		if err := waitGraphFrame(ctx); err != nil {
			return "", err
		}
		// Drop the frame before the commit. A refetch of the new revision then
		// sees the settled to-do, not the in-progress picture.
		e.clearGraphPreview(caller.OwnerID())
	}
	result, replayed, err := e.store.ApplyGraph(ctx, caller, batch)
	if errors.Is(err, store.ErrOwnToolReceiptUnavailable) && batch.OwnToolCallIDs != nil {
		// A call id the model made up is not evidence. Cite its real results.
		batch.OwnToolCallIDs, batch.CiteOwnResults = nil, true
		result, replayed, err = e.store.ApplyGraph(ctx, caller, batch)
	}
	if err != nil {
		return "", err
	}
	e.rememberGraphRefs(caller.OwnerID(), result)
	// The stored result still has every link. The model already sent them, and
	// the next request would bill that list again. The receipt keeps the
	// subject ids the model needs for a later change.
	shown := result
	shown.Links = nil
	reply := map[string]any{
		"ok": true, "result": shown, "link_count": len(result.Links),
		"note": "recorded; a later call adds only a change and does not resend this batch",
	}
	if len(dropped) > 0 {
		reply["omitted_attributes"] = dropped
	}
	if replayed {
		reply["replayed"] = true
	}
	return encodeGraph(reply)
}

// relaxGraphBatch repairs the mechanical mistakes that otherwise make the
// model resend a whole batch. A new subject's id copied from its ref is not
// an existing id. An attribute the kind does not declare is omitted. A link
// end written as ref:<ref> is that ref, and a ref used in an earlier call of
// this process is the stored id.
func (e *Engine) relaxGraphBatch(owner string, batch *graph.Batch) []string {
	refs := map[string]bool{}
	for _, subject := range batch.Subjects {
		if subject.Ref != "" {
			refs[subject.Ref] = true
		}
	}
	known := e.graphRefs[owner]
	resolve := func(value string) string {
		if rest, ok := strings.CutPrefix(value, "ref:"); ok && (refs[rest] || known[rest] != "") {
			value = rest
		}
		if id := known[value]; id != "" && !refs[value] {
			return id
		}
		return value
	}
	// Literal evidence is the more specific claim; the ids are then not needed.
	if len(batch.Evidence) > 0 && len(batch.OwnToolCallIDs) > 0 {
		batch.OwnToolCallIDs = nil
	}
	var dropped []string
	cat, err := graph.Default()
	for i := range batch.Subjects {
		subject := &batch.Subjects[i]
		// Stored subject ids are minted by the host with this prefix; anything
		// else is a name the model chose for a subject that does not exist yet.
		if subject.ID != "" && (subject.ID == subject.Ref || !strings.HasPrefix(subject.ID, "sj_")) {
			subject.ID = ""
		}
		if subject.ID == "" {
			subject.Version = 0
		}
		for name, value := range subject.Attrs {
			if value == nil {
				delete(subject.Attrs, name)
			}
		}
		if err != nil || subject.Kind == "" || len(subject.Attrs) == 0 {
			continue
		}
		kind := cat.Kind(subject.Kind)
		if kind == nil {
			continue
		}
		for name := range subject.Attrs {
			if kind.Attr(name) == nil {
				delete(subject.Attrs, name)
				dropped = append(dropped, kind.ID+"."+name)
			}
		}
	}
	sort.Strings(dropped)
	dropped = slices.Compact(dropped)
	for i := range batch.Todos {
		todo := &batch.Todos[i]
		if todo.ID != "" && !strings.HasPrefix(todo.ID, "td_") {
			todo.ID = ""
		}
		if todo.ID == "" {
			todo.Version = 0
		}
	}
	for i := range batch.Links {
		batch.Links[i].From = resolve(batch.Links[i].From)
		batch.Links[i].To = resolve(batch.Links[i].To)
	}
	return dropped
}

func (e *Engine) rememberGraphRefs(owner string, result graph.Result) {
	for _, subject := range result.Subjects {
		if subject.Ref == "" || subject.ID == "" {
			continue
		}
		if e.graphRefs == nil {
			e.graphRefs = map[string]map[string]string{}
		}
		if e.graphRefs[owner] == nil {
			e.graphRefs[owner] = map[string]string{}
		}
		e.graphRefs[owner][subject.Ref] = subject.ID
	}
}

func encodeGraph(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimRight(buf.String(), "\n"), nil
}

type graphReadArgs struct {
	ID     string `json:"id"`
	Todos  bool   `json:"todos"`
	Status string `json:"status"`
	Text   string `json:"text"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

// graphReadSubject is what the model sees of a subject: no bookkeeping columns.
type graphReadSubject struct {
	ID          string           `json:"id"`
	Kind        string           `json:"kind"`
	Name        string           `json:"name"`
	Status      string           `json:"status"`
	Version     int64            `json:"version"`
	Attrs       map[string]any   `json:"attrs,omitempty"`
	Credentials []string         `json:"credentials,omitempty"`
	Evidence    []graph.Evidence `json:"evidence,omitempty"`
	FoundByTodo string           `json:"found_by_todo,omitempty"`
}

type graphReadLink struct {
	From     string           `json:"from"`
	To       string           `json:"to"`
	Rel      string           `json:"rel"`
	Attrs    map[string]any   `json:"attrs,omitempty"`
	Evidence []graph.Evidence `json:"evidence,omitempty"`
}

type graphReadTodo struct {
	ID       string           `json:"id"`
	Title    string           `json:"title"`
	About    string           `json:"about,omitempty"`
	Status   string           `json:"status"`
	Version  int64            `json:"version"`
	Result   string           `json:"result,omitempty"`
	Evidence []graph.Evidence `json:"evidence,omitempty"`
}

type graphReadReply struct {
	Revision int64              `json:"revision"`
	Total    int                `json:"total"`
	More     bool               `json:"more,omitempty"`
	Subjects []graphReadSubject `json:"subjects,omitempty"`
	Links    []graphReadLink    `json:"links,omitempty"`
	Todos    []graphReadTodo    `json:"todos,omitempty"`
}

func (e *Engine) readGraphTool(ctx context.Context, turn *store.Turn, args string) (string, error) {
	if err := context.Cause(ctx); err != nil {
		return "", err
	}
	if !e.graphEnabled(turn) {
		return "", ErrGraphDisabled
	}
	owner, ok := ctx.Value(historyReadOwnerKey{}).(historyReadOwner)
	if !ok || owner.ThreadID != turn.ThreadID || owner.TurnID != turn.ID {
		return "", fmt.Errorf("graph read has no matching trusted turn owner")
	}
	var in graphReadArgs
	if err := decodeGraphArgs(args, &in); err != nil {
		return "", err
	}
	if in.Offset < 0 || in.Limit < 0 {
		return "", fmt.Errorf("%w: offset and limit must be nonnegative", graph.ErrInvalid)
	}
	view, err := e.store.ReadGraph(ctx, store.GraphOwnerID(turn.ProjectID, turn.ThreadID), store.GraphQuery{
		ID: in.ID, Todos: in.Todos, Status: in.Status, Text: in.Text, Offset: in.Offset, Limit: in.Limit,
	})
	if err != nil {
		return "", err
	}
	reply := graphReply(view, in.ID)
	budget := e.cfg.Snapshot().Tools.ToolResultLimit()
	// Whole rows are dropped under the result budget; JSON is never cut.
	for {
		body, err := encodeGraph(reply)
		if err != nil {
			return "", err
		}
		if len([]rune(body)) <= budget || !reply.shrink() {
			return body, nil
		}
	}
}

func (r *graphReadReply) shrink() bool {
	switch {
	case len(r.Subjects) > 1 && len(r.Links) > 0:
		r.Links = r.Links[:len(r.Links)/2]
	case len(r.Subjects) > 1:
		r.Subjects = r.Subjects[:len(r.Subjects)/2]
	case len(r.Todos) > 1:
		r.Todos = r.Todos[:len(r.Todos)/2]
	default:
		return false
	}
	r.More = true
	return true
}

// graphReply projects a view for the model. Only the by-id read shows the
// subject's credentials; every other read names them and holds the value back.
func graphReply(view store.GraphView, byID string) *graphReadReply {
	catalog := graph.MustDefault()
	reply := &graphReadReply{Revision: view.Revision, Total: view.Total, More: view.More}
	for _, s := range view.Subjects {
		out := graphReadSubject{ID: s.ID, Kind: s.Kind, Name: s.Name, Status: s.Status, Version: s.Version,
			Attrs: s.Attrs, Evidence: s.Evidence, FoundByTodo: s.FoundByTodo}
		if s.ID != byID {
			out.Attrs, out.Credentials = catalog.Redact(s.Kind, s.Attrs)
		}
		reply.Subjects = append(reply.Subjects, out)
	}
	for _, l := range view.Links {
		reply.Links = append(reply.Links, graphReadLink{From: l.FromID, To: l.ToID, Rel: l.Rel, Attrs: l.Attrs, Evidence: l.Evidence})
	}
	for _, t := range view.Todos {
		reply.Todos = append(reply.Todos, graphReadTodo{ID: t.ID, Title: t.Title, About: t.About, Status: t.Status,
			Version: t.Version, Result: t.Result, Evidence: t.Evidence})
	}
	return reply
}

// prepareGraphEvidence drops evidence the host cannot check. With nothing
// left to cite, the host uses this caller's own completed tool results.
// A well-formed evidence list, or an explicit own_tool_call_ids list, stays.
func prepareGraphEvidence(batch *graph.Batch) {
	for i := range batch.Evidence {
		if err := batch.Evidence[i].ValidateShape(); err != nil {
			batch.Evidence = nil
			break
		}
	}
	// An empty list names nothing, so it is the same as leaving the field out.
	if len(batch.OwnToolCallIDs) == 0 {
		batch.OwnToolCallIDs = nil
	}
	if len(batch.Evidence) == 0 && batch.OwnToolCallIDs == nil {
		batch.CiteOwnResults = true
	}
}

// knownSubject is a subject that already exists, used when a link names it by
// id without repeating it in this batch.
type knownSubject struct {
	Kind   string
	Status string
}

// knownGraphSubjects loads existing subjects only when a link names an id this
// batch does not describe. Two new subjects need no read.
func (e *Engine) knownGraphSubjects(ctx context.Context, owner string, batch *graph.Batch) (map[string]knownSubject, error) {
	described := map[string]bool{}
	for _, subject := range batch.Subjects {
		if subject.Ref != "" {
			described[subject.Ref] = true
		}
		if subject.ID != "" {
			described[subject.ID] = true
		}
	}
	need := false
	for _, link := range batch.Links {
		if !described[link.From] || !described[link.To] {
			need = true
			break
		}
	}
	if !need {
		return nil, nil
	}
	view, err := e.store.ReadGraph(ctx, owner, store.GraphQuery{Limit: -1})
	if err != nil {
		return nil, err
	}
	known := make(map[string]knownSubject, len(view.Subjects))
	for _, subject := range view.Subjects {
		known[subject.ID] = knownSubject{Kind: subject.Kind, Status: subject.Status}
	}
	return known, nil
}

// retractLeadsLinkedToConfirmed stores a lead as retracted when this call links
// it to a confirmed subject of the same kind, whichever end of the link the
// lead is. A lead is still current, so leaving it beside the confirmed subject
// shows two current things. A stored lead named only as a link end is included.
// Two leads, or two confirmed subjects, stay as written: a peer relationship
// is not a version.
func retractLeadsLinkedToConfirmed(batch *graph.Batch, known map[string]knownSubject) {
	type end struct {
		kind, status string
		index        int
	}
	ends := map[string]end{}
	for i := range batch.Subjects {
		subject := &batch.Subjects[i]
		kind, status := subject.Kind, subject.Status
		if subject.ID != "" {
			if prev, ok := known[subject.ID]; ok {
				if kind == "" {
					kind = prev.Kind
				}
				if status == "" {
					status = prev.Status
				}
			}
		}
		if status == "" {
			status = graph.StatusLead
		}
		resolved := end{kind: kind, status: status, index: i}
		if subject.Ref != "" {
			ends[subject.Ref] = resolved
		}
		if subject.ID != "" {
			ends[subject.ID] = resolved
		}
	}
	for id, prev := range known {
		if _, ok := ends[id]; !ok {
			ends[id] = end{kind: prev.Kind, status: prev.Status, index: -1}
		}
	}
	for _, link := range batch.Links {
		from, okFrom := ends[link.From]
		to, okTo := ends[link.To]
		if !okFrom || !okTo || from.kind == "" || from.kind != to.kind {
			continue
		}
		lead, leadKey := from, link.From
		otherConfirmed := to.status == graph.StatusConfirmed && from.status == graph.StatusLead
		if to.status == graph.StatusLead && from.status == graph.StatusConfirmed {
			lead, leadKey = to, link.To
			otherConfirmed = true
		}
		if !otherConfirmed {
			continue
		}
		if lead.index < 0 {
			lead.index = len(batch.Subjects)
			batch.Subjects = append(batch.Subjects, graph.SubjectInput{ID: leadKey, HostRetract: true})
		} else {
			batch.Subjects[lead.index].Status = graph.StatusRetracted
			batch.Subjects[lead.index].HostRetract = true
		}
		lead.status = graph.StatusRetracted
		subject := batch.Subjects[lead.index]
		if subject.Ref != "" {
			ends[subject.Ref] = lead
		}
		if subject.ID != "" {
			ends[subject.ID] = lead
		}
		ends[leadKey] = lead
	}
}

// graphDoingFrame is how long a newly finished to-do is shown in progress
// before the one commit that marks it done. The frame is not itself a commit.
const graphDoingFrame = 500 * time.Millisecond

func waitGraphFrame(ctx context.Context) error {
	timer := time.NewTimer(graphDoingFrame)
	select {
	case <-ctx.Done():
		timer.Stop()
		return context.Cause(ctx)
	case <-timer.C:
		return nil
	}
}

// alignGraphTodos makes a repeated settlement land on the to-do already in
// progress. It also reports when the first new done to-do should be shown in
// progress before the batch commits. That frame is not stored: the commit
// writes the done status, or nothing if the batch is refused.
func alignGraphTodos(ctx context.Context, s *store.Store, owner string, batch *graph.Batch) (bool, error) {
	view, err := s.ReadGraph(ctx, owner, store.GraphQuery{Todos: true, Status: graph.TodoDoing, Limit: -1})
	if err != nil {
		return false, err
	}
	byTitle := make(map[string]store.GraphTodo, len(view.Todos))
	for _, todo := range view.Todos {
		if _, ok := byTitle[todo.Title]; !ok {
			byTitle[todo.Title] = todo
		}
	}
	for i := range batch.Todos {
		in := &batch.Todos[i]
		if in.ID == "" && in.Status == graph.TodoDone {
			if todo, ok := byTitle[in.Title]; ok {
				in.ID = todo.ID
				in.Version = todo.Version
				delete(byTitle, in.Title)
			}
		}
	}
	if len(view.Todos) > 0 {
		return false, nil
	}
	for _, in := range batch.Todos {
		if in.Status == graph.TodoDoing {
			return false, nil
		}
	}
	for i := range batch.Todos {
		if batch.Todos[i].ID == "" && batch.Todos[i].Status == graph.TodoDone {
			return true, nil
		}
	}
	return false, nil
}
