package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"gorm.io/gorm"
)

// GraphCaller is the trusted identity of a write. The engine fills it from the
// running turn; nothing in it comes from the model's arguments.
type GraphCaller struct {
	ThreadID   string
	TurnID     string
	AgentID    string
	Generation int
	// ProjectID is the turn's frozen project, empty for a standalone
	// conversation. It decides the owner.
	ProjectID string
	// CurrentToolCallID and ToolReceiptScope locate this agent's own calls for
	// own_tool_call_ids evidence.
	CurrentToolCallID string
	ToolReceiptScope  string
	// ReceiptKey and ReceiptHash make a repeated tool call idempotent. Both or
	// neither.
	ReceiptKey  string
	ReceiptHash string
}

// OwnerID is the project when there is one, else the conversation.
func (c GraphCaller) OwnerID() string { return GraphOwnerID(c.ProjectID, c.ThreadID) }

// HashGraphBatch binds a tool-call receipt to the exact body of the batch.
func HashGraphBatch(batch graph.Batch) (string, error) {
	raw, err := json.Marshal(batch)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// ApplyGraph commits one batch in a single transaction. The result says
// whether it was replayed from an earlier identical call, in which case
// nothing was written. A refusal leaves the graph exactly as it was.
func (s *Store) ApplyGraph(ctx context.Context, caller GraphCaller, batch graph.Batch) (graph.Result, bool, error) {
	if err := ctx.Err(); err != nil {
		return graph.Result{}, false, err
	}
	if err := batch.Valid(); err != nil {
		return graph.Result{}, false, err
	}
	if (caller.ReceiptKey == "") != (caller.ReceiptHash == "") {
		return graph.Result{}, false, fmt.Errorf("%w: a write receipt needs both key and hash", graph.ErrInvalid)
	}
	cat, err := graph.Default()
	if err != nil {
		return graph.Result{}, false, err
	}
	var (
		result   graph.Result
		replayed bool
	)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := graphOwnerCheck(tx, caller); err != nil {
			return err
		}
		if caller.ReceiptKey != "" {
			frozen, found, err := replayGraphReceipt(tx, caller)
			if err != nil {
				return err
			}
			if found {
				result, replayed = frozen, true
				return nil
			}
		}
		evidence, err := resolveGraphEvidence(tx, caller, batch)
		if err != nil {
			return err
		}
		w := &graphWriter{tx: tx, cat: cat, caller: caller, evidence: evidence, now: time.Now().UTC(),
			subjectRefs: map[string]string{}, todoRefs: map[string]string{}}
		if err := w.apply(batch); err != nil {
			return err
		}
		result = w.result
		return w.commit(&result)
	})
	if err != nil {
		return graph.Result{}, false, fmt.Errorf("store: write graph: %w", err)
	}
	return result, replayed, nil
}

// graphOwnerCheck refuses a write whose conversation or turn is gone, belongs
// to another owner than the one frozen at turn start, or is planning.
func graphOwnerCheck(tx *gorm.DB, caller GraphCaller) error {
	var thread Thread
	if err := tx.Select("id", "project_id", "plan_mode").Take(&thread, "id = ?", caller.ThreadID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: the conversation no longer exists", graph.ErrInvalid)
		}
		return err
	}
	var turn Turn
	if err := tx.Select("id", "project_id", "thread_id", "status", "work_ledger_read_only").
		Take(&turn, "id = ?", caller.TurnID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: the turn no longer exists", graph.ErrInvalid)
		}
		return err
	}
	if turn.ThreadID != thread.ID || turn.Status == TurnCancelled {
		return fmt.Errorf("%w: the turn has another owner or was cancelled", graph.ErrInvalid)
	}
	if turn.ProjectID != caller.ProjectID || thread.ProjectID != caller.ProjectID {
		return fmt.Errorf("%w: the conversation moved to another project during this turn", graph.ErrInvalid)
	}
	if thread.PlanMode || turn.GraphReadOnly {
		return fmt.Errorf("%w: planning can read the graph but cannot write it", graph.ErrInvalid)
	}
	return nil
}

func replayGraphReceipt(tx *gorm.DB, caller GraphCaller) (graph.Result, bool, error) {
	var row GraphRevision
	err := tx.Take(&row, "request_key = ?", caller.ReceiptKey).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return graph.Result{}, false, nil
	}
	if err != nil {
		return graph.Result{}, false, err
	}
	if row.OwnerID != caller.OwnerID() || row.RequestHash != caller.ReceiptHash {
		return graph.Result{}, false, fmt.Errorf("%w: this tool call id was already used with a different body", graph.ErrConflict)
	}
	var frozen graph.Result
	if err := json.Unmarshal([]byte(row.Payload), &frozen); err != nil {
		return graph.Result{}, false, fmt.Errorf("store: invalid graph receipt: %w", err)
	}
	return frozen, true, nil
}

type graphWriter struct {
	tx       *gorm.DB
	cat      *graph.Catalog
	caller   GraphCaller
	evidence []graph.Evidence
	now      time.Time
	result   graph.Result

	subjectRefs map[string]string
	todoRefs    map[string]string

	createdSubjects []string
	createdLinks    []string
	settled         []string

	order     []string // todo ids in list order, loaded on first use
	stored    map[string]int
	orderLoad bool
	todoIdx   map[string]int // result index by todo id
}

func (w *graphWriter) owner() string { return w.caller.OwnerID() }

func (w *graphWriter) apply(batch graph.Batch) error {
	for i, in := range batch.Subjects {
		res, err := w.subject(in)
		if err != nil {
			return fmt.Errorf("subjects[%d]: %w", i, err)
		}
		w.result.Subjects = append(w.result.Subjects, res)
	}
	w.todoIdx = map[string]int{}
	for i, in := range batch.Todos {
		res, err := w.todo(in)
		if err != nil {
			return fmt.Errorf("todos[%d]: %w", i, err)
		}
		w.todoIdx[res.ID] = len(w.result.Todos)
		w.result.Todos = append(w.result.Todos, res)
	}
	for i, in := range batch.Links {
		res, err := w.link(in)
		if err != nil {
			return fmt.Errorf("links[%d]: %w", i, err)
		}
		w.result.Links = append(w.result.Links, res)
	}
	return nil
}

// commit stamps the settlement, saves the list order and the revision.
func (w *graphWriter) commit(result *graph.Result) error {
	if len(w.settled) == 1 {
		// One settlement in the batch owns what the batch found. With several
		// the attribution would be a guess, so none is made.
		for _, set := range []struct {
			table string
			ids   []string
		}{{"graph_subjects", w.createdSubjects}, {"graph_links", w.createdLinks}} {
			if len(set.ids) == 0 {
				continue
			}
			if err := w.tx.Table(set.table).Where("id IN ?", set.ids).Update("found_by_todo", w.settled[0]).Error; err != nil {
				return err
			}
		}
	}
	if err := w.savePositions(result); err != nil {
		return err
	}
	var last int64
	if err := w.tx.Model(&GraphRevision{}).Where("owner_id = ?", w.owner()).
		Select("COALESCE(MAX(revision), 0)").Scan(&last).Error; err != nil {
		return err
	}
	result.Revision = last + 1
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	row := GraphRevision{OwnerID: w.owner(), Revision: result.Revision, Payload: string(payload), ThreadID: w.caller.ThreadID, CreatedAt: w.now}
	if w.caller.ReceiptKey != "" {
		key := w.caller.ReceiptKey
		row.RequestKey, row.RequestHash = &key, w.caller.ReceiptHash
	}
	return w.tx.Create(&row).Error
}

// ---------- subjects ----------

func (w *graphWriter) subject(in graph.SubjectInput) (graph.SubjectResult, error) {
	if in.Ref != "" {
		if _, dup := w.subjectRefs[in.Ref]; dup {
			return graph.SubjectResult{}, fmt.Errorf("%w: ref %q is used twice in one call", graph.ErrInvalid, in.Ref)
		}
	}
	if in.Status != "" && !slices.Contains([]string{graph.StatusLead, graph.StatusConfirmed, graph.StatusRetracted}, in.Status) {
		return graph.SubjectResult{}, fmt.Errorf("%w: subject status must be lead, confirmed or retracted", graph.ErrInvalid)
	}
	var (
		current *GraphSubject
		byID    = in.ID != ""
	)
	if byID {
		var row GraphSubject
		if err := w.tx.Take(&row, "id = ? AND owner_id = ?", in.ID, w.owner()).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return graph.SubjectResult{}, fmt.Errorf("%w: subject %s does not exist here", graph.ErrInvalid, in.ID)
			}
			return graph.SubjectResult{}, err
		}
		current = &row
		if in.Kind != "" && in.Kind != row.Kind {
			return graph.SubjectResult{}, fmt.Errorf("%w: a subject cannot change kind", graph.ErrInvalid)
		}
	} else if in.Version != 0 {
		return graph.SubjectResult{}, fmt.Errorf("%w: version needs the subject id", graph.ErrInvalid)
	}
	kindID := in.Kind
	if current != nil {
		kindID = current.Kind
	}
	kind := w.cat.Kind(kindID)
	if kind == nil {
		return graph.SubjectResult{}, fmt.Errorf("%w: unknown kind %q; use one of %s", graph.ErrInvalid, kindID, strings.Join(w.cat.IDs(), ", "))
	}
	attrs, err := kind.Clean(in.Attrs)
	if err != nil {
		return graph.SubjectResult{}, err
	}
	if current == nil {
		keys, err := kind.IdentityKeys(attrs)
		if err != nil {
			return graph.SubjectResult{}, err
		}
		matches, err := w.matchKeys(kind.ID, keys)
		if err != nil {
			return graph.SubjectResult{}, err
		}
		switch len(matches) {
		case 0:
			return w.createSubject(in, kind, attrs, keys)
		case 1:
			var row GraphSubject
			if err := w.tx.Take(&row, "id = ?", matches[0]).Error; err != nil {
				return graph.SubjectResult{}, err
			}
			current = &row
		default:
			return graph.SubjectResult{}, &graph.AmbiguousError{IDs: matches}
		}
	}
	return w.updateSubject(in, kind, current, attrs, byID)
}

// matchKeys returns the distinct subjects holding any of the keys.
func (w *graphWriter) matchKeys(kind string, keys []graph.KeyValue) ([]string, error) {
	var ids []string
	for _, key := range keys {
		var found []string
		if err := w.tx.Model(&GraphSubjectKey{}).
			Where("owner_id = ? AND kind = ? AND key_name = ? AND value = ?", w.owner(), kind, key.Name, key.Value).
			Pluck("subject_id", &found).Error; err != nil {
			return nil, err
		}
		for _, id := range found {
			if !slices.Contains(ids, id) {
				ids = append(ids, id)
			}
		}
	}
	slices.Sort(ids)
	return ids, nil
}

func (w *graphWriter) requireEvidence(status string) error {
	if status == graph.StatusConfirmed && len(w.evidence) == 0 {
		return fmt.Errorf("%w: a confirmed subject needs evidence: cite events with evidence or own_tool_call_ids", graph.ErrInvalid)
	}
	return nil
}

func (w *graphWriter) createSubject(in graph.SubjectInput, kind *graph.Kind, attrs map[string]any, keys []graph.KeyValue) (graph.SubjectResult, error) {
	status := in.Status
	if status == "" {
		status = graph.StatusLead
	}
	if err := w.requireEvidence(status); err != nil {
		return graph.SubjectResult{}, err
	}
	row := GraphSubject{
		ID: NewID("sj_"), OwnerID: w.owner(), Kind: kind.ID, Name: kind.DisplayName(attrs), Attrs: attrs,
		Status: status, Evidence: unionEvidence(nil, w.evidence), Version: 1,
		ThreadID: w.caller.ThreadID, TurnID: w.caller.TurnID, AgentID: w.caller.AgentID, Generation: w.caller.Generation,
		UpdatedThreadID: w.caller.ThreadID, UpdatedTurnID: w.caller.TurnID, UpdatedBy: w.caller.AgentID, UpdatedGeneration: w.caller.Generation,
		CreatedAt: w.now, UpdatedAt: w.now,
	}
	if err := w.tx.Create(&row).Error; err != nil {
		return graph.SubjectResult{}, err
	}
	if err := w.insertKeys(row, keys); err != nil {
		return graph.SubjectResult{}, err
	}
	w.createdSubjects = append(w.createdSubjects, row.ID)
	w.noteSubjectRef(in.Ref, row.ID)
	return graph.SubjectResult{Ref: in.Ref, ID: row.ID, Kind: row.Kind, Name: row.Name, Version: row.Version, Created: true}, nil
}

func (w *graphWriter) noteSubjectRef(ref, id string) {
	if ref != "" {
		w.subjectRefs[ref] = id
	}
}

func (w *graphWriter) insertKeys(row GraphSubject, keys []graph.KeyValue) error {
	for _, key := range keys {
		rec := GraphSubjectKey{OwnerID: row.OwnerID, Kind: row.Kind, KeyName: key.Name, Value: key.Value, SubjectID: row.ID}
		var holder string
		err := w.tx.Model(&GraphSubjectKey{}).Select("subject_id").
			Where("owner_id = ? AND kind = ? AND key_name = ? AND value = ?", rec.OwnerID, rec.Kind, rec.KeyName, rec.Value).
			Limit(1).Scan(&holder).Error
		if err != nil {
			return err
		}
		if holder != "" && holder != row.ID {
			return &graph.AmbiguousError{IDs: []string{row.ID, holder}}
		}
		if holder == "" {
			if err := w.tx.Create(&rec).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *graphWriter) updateSubject(in graph.SubjectInput, kind *graph.Kind, current *GraphSubject, attrs map[string]any, byID bool) (graph.SubjectResult, error) {
	res := graph.SubjectResult{Ref: in.Ref, ID: current.ID, Kind: current.Kind}
	if in.Version != 0 && in.Version != current.Version {
		return res, fmt.Errorf("%w: subject %s is at version %d, not %d; read it again", graph.ErrConflict, current.ID, current.Version, in.Version)
	}
	versioned := in.Version != 0
	stored := kind.Normalized(current.Attrs)
	var next map[string]any
	if versioned {
		next = kind.Overwrite(stored, attrs)
	} else {
		var conflicts []string
		next, conflicts = kind.MergeAdditive(stored, attrs)
		for _, name := range conflicts {
			res.Ignored = append(res.Ignored, name)
		}
	}
	status := current.Status
	if in.Status != "" && in.Status != current.Status {
		switch {
		case versioned:
			status = in.Status
		case byID:
			return res, fmt.Errorf("%w: changing a subject's status needs its current version", graph.ErrInvalid)
		default:
			res.Ignored = append(res.Ignored, "status")
		}
	}
	if status != current.Status {
		if err := w.requireEvidence(status); err != nil {
			return res, err
		}
	}
	var storedKeys []graph.KeyValue
	var keyRows []GraphSubjectKey
	if err := w.tx.Where("subject_id = ?", current.ID).Find(&keyRows).Error; err != nil {
		return res, err
	}
	for _, k := range keyRows {
		storedKeys = append(storedKeys, graph.KeyValue{Name: k.KeyName, Value: k.Value})
	}
	nextKeys, err := kind.IdentityKeys(next)
	if err != nil {
		return res, err
	}
	if err := kind.CheckKeyTransition(storedKeys, nextKeys); err != nil {
		return res, err
	}
	changed := !sameAttrs(stored, next) || status != current.Status
	evidence := unionEvidence(current.Evidence, w.evidence)
	if !changed && len(evidence) == len(current.Evidence) {
		res.Name, res.Version = current.Name, current.Version
		w.noteSubjectRef(in.Ref, current.ID)
		return res, nil
	}
	update := map[string]any{"evidence": evidenceJSON(evidence)}
	version := current.Version
	if changed {
		version++
		update["attrs"] = attrsJSON(next)
		update["name"] = kind.DisplayName(next)
		update["status"] = status
		update["version"] = version
		update["updated_at"] = w.now
		update["updated_thread_id"], update["updated_turn_id"] = w.caller.ThreadID, w.caller.TurnID
		update["updated_by"], update["updated_generation"] = w.caller.AgentID, w.caller.Generation
	}
	if err := w.tx.Model(&GraphSubject{}).Where("id = ? AND version = ?", current.ID, current.Version).UpdateColumns(update).Error; err != nil {
		return res, err
	}
	if changed {
		// Keys that were superseded go; new ones are added.
		for _, k := range keyRows {
			if !slices.Contains(nextKeys, graph.KeyValue{Name: k.KeyName, Value: k.Value}) {
				if err := w.tx.Delete(&GraphSubjectKey{}, k.ID).Error; err != nil {
					return res, err
				}
			}
		}
		if err := w.insertKeys(GraphSubject{ID: current.ID, OwnerID: current.OwnerID, Kind: current.Kind}, nextKeys); err != nil {
			return res, err
		}
	}
	res.Name, res.Version = kind.DisplayName(next), version
	w.noteSubjectRef(in.Ref, current.ID)
	return res, nil
}

func sameAttrs(a, b map[string]any) bool {
	ra, _ := json.Marshal(a)
	rb, _ := json.Marshal(b)
	return string(ra) == string(rb)
}

// UpdateColumns with a map bypasses gorm's JSON serializer, so these columns
// are encoded here exactly as the serializer would write them.
func evidenceJSON(ev []graph.Evidence) string {
	if ev == nil {
		ev = []graph.Evidence{}
	}
	raw, _ := json.Marshal(ev)
	return string(raw)
}

func attrsJSON(attrs map[string]any) string {
	raw, _ := json.Marshal(attrs)
	return string(raw)
}

// ---------- links ----------

func (w *graphWriter) resolveSubject(ref string) (string, error) {
	if id, ok := w.subjectRefs[ref]; ok {
		return id, nil
	}
	var id string
	if ref != "" {
		if err := w.tx.Model(&GraphSubject{}).Select("id").Where("id = ? AND owner_id = ?", ref, w.owner()).Limit(1).Scan(&id).Error; err != nil {
			return "", err
		}
	}
	if id == "" {
		return "", fmt.Errorf("%w: %q is neither a subject id nor a ref of this call", graph.ErrInvalid, ref)
	}
	return id, nil
}

func (w *graphWriter) link(in graph.LinkInput) (graph.LinkResult, error) {
	rel := strings.TrimSpace(in.Rel)
	if rel == "" || utf8.RuneCountInString(rel) > graph.MaxRel {
		return graph.LinkResult{}, fmt.Errorf("%w: rel is 1 to %d characters", graph.ErrInvalid, graph.MaxRel)
	}
	attrs, err := graph.CleanLinkAttrs(in.Attrs)
	if err != nil {
		return graph.LinkResult{}, err
	}
	from, err := w.resolveSubject(in.From)
	if err != nil {
		return graph.LinkResult{}, err
	}
	to, err := w.resolveSubject(in.To)
	if err != nil {
		return graph.LinkResult{}, err
	}
	if from == to {
		return graph.LinkResult{}, fmt.Errorf("%w: a link needs two different subjects", graph.ErrInvalid)
	}
	var row GraphLink
	err = w.tx.Take(&row, "owner_id = ? AND from_id = ? AND to_id = ? AND rel = ?", w.owner(), from, to, rel).Error
	if err == nil {
		merged := map[string]any{}
		for k, v := range row.Attrs {
			merged[k] = v
		}
		for k, v := range attrs {
			if _, has := merged[k]; !has {
				merged[k] = v
			}
		}
		evidence := unionEvidence(row.Evidence, w.evidence)
		if !sameAttrs(row.Attrs, merged) || len(evidence) != len(row.Evidence) {
			if err := w.tx.Model(&GraphLink{}).Where("id = ?", row.ID).
				UpdateColumns(map[string]any{"attrs": attrsJSON(merged), "evidence": evidenceJSON(evidence)}).Error; err != nil {
				return graph.LinkResult{}, err
			}
		}
		return graph.LinkResult{ID: row.ID, From: from, To: to, Rel: rel}, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return graph.LinkResult{}, err
	}
	row = GraphLink{ID: NewID("lk_"), OwnerID: w.owner(), FromID: from, ToID: to, Rel: rel, Attrs: attrs,
		Evidence: unionEvidence(nil, w.evidence), ThreadID: w.caller.ThreadID, TurnID: w.caller.TurnID,
		AgentID: w.caller.AgentID, Generation: w.caller.Generation, CreatedAt: w.now}
	if err := w.tx.Create(&row).Error; err != nil {
		return graph.LinkResult{}, err
	}
	w.createdLinks = append(w.createdLinks, row.ID)
	return graph.LinkResult{ID: row.ID, From: from, To: to, Rel: rel, Created: true}, nil
}
