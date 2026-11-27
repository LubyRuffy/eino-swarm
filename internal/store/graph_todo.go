package store

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"gorm.io/gorm"
)

// loadOrder reads the owner's list order once per batch. Reordering rewrites
// positions inside the same transaction, so two workers reordering at once end
// with one consistent order and no lost item.
func (w *graphWriter) loadOrder() error {
	if w.orderLoad {
		return nil
	}
	var rows []struct {
		ID       string
		Position int
	}
	if err := w.tx.Model(&GraphTodo{}).Select("id", "position").Where("owner_id = ?", w.owner()).
		Order("position, created_at, id").Find(&rows).Error; err != nil {
		return err
	}
	w.stored = make(map[string]int, len(rows))
	for _, r := range rows {
		w.order = append(w.order, r.ID)
		w.stored[r.ID] = r.Position
	}
	w.orderLoad = true
	return nil
}

func (w *graphWriter) resolveTodo(ref string) (string, error) {
	if id, ok := w.todoRefs[ref]; ok {
		return id, nil
	}
	if err := w.loadOrder(); err != nil {
		return "", err
	}
	if slices.Contains(w.order, ref) {
		return ref, nil
	}
	return "", fmt.Errorf("%w: before %q is neither a to-do id nor a ref of this call", graph.ErrInvalid, ref)
}

// place puts id in front of target, or at the end when target is empty.
func (w *graphWriter) place(id, target string) error {
	if err := w.loadOrder(); err != nil {
		return err
	}
	at := len(w.order)
	if target != "" {
		before, err := w.resolveTodo(target)
		if err != nil {
			return err
		}
		if before == id {
			return nil
		}
		w.order = slices.DeleteFunc(w.order, func(s string) bool { return s == id })
		at = slices.Index(w.order, before)
	} else {
		w.order = slices.DeleteFunc(w.order, func(s string) bool { return s == id })
		at = len(w.order)
	}
	w.order = slices.Insert(w.order, at, id)
	return nil
}

func (w *graphWriter) savePositions(result *graph.Result) error {
	if !w.orderLoad {
		return nil
	}
	for i, id := range w.order {
		if got, known := w.stored[id]; !known || got != i {
			if err := w.tx.Model(&GraphTodo{}).Where("id = ?", id).UpdateColumn("position", i).Error; err != nil {
				return err
			}
		}
	}
	for id, idx := range w.todoIdx {
		result.Todos[idx].Position = slices.Index(w.order, id)
	}
	return nil
}

func (w *graphWriter) todo(in graph.TodoInput) (graph.TodoResult, error) {
	if in.Ref != "" {
		if _, dup := w.todoRefs[in.Ref]; dup {
			return graph.TodoResult{}, fmt.Errorf("%w: ref %q is used twice in one call", graph.ErrInvalid, in.Ref)
		}
	}
	in.Title, in.Result = strings.TrimSpace(in.Title), strings.TrimSpace(in.Result)
	if in.Status != "" && !slices.Contains([]string{graph.TodoOpen, graph.TodoDoing, graph.TodoDone, graph.TodoFailed}, in.Status) {
		return graph.TodoResult{}, fmt.Errorf("%w: to-do status must be open, doing, done or failed", graph.ErrInvalid)
	}
	if utf8.RuneCountInString(in.Title) > graph.MaxTitle {
		return graph.TodoResult{}, fmt.Errorf("%w: a to-do title is at most %d characters", graph.ErrInvalid, graph.MaxTitle)
	}
	if utf8.RuneCountInString(in.Result) > graph.MaxResult {
		return graph.TodoResult{}, fmt.Errorf("%w: a to-do result is at most %d characters", graph.ErrInvalid, graph.MaxResult)
	}
	if in.ID == "" {
		if in.Version != 0 {
			return graph.TodoResult{}, fmt.Errorf("%w: version needs the to-do id", graph.ErrInvalid)
		}
		return w.createTodo(in)
	}
	return w.updateTodo(in)
}

func (w *graphWriter) settleCheck(status, result string) error {
	switch status {
	case graph.TodoDone:
		if len(w.evidence) == 0 {
			return fmt.Errorf("%w: a done to-do needs evidence: cite events with evidence or own_tool_call_ids", graph.ErrInvalid)
		}
	case graph.TodoFailed:
		if result == "" {
			return fmt.Errorf("%w: a failed to-do needs a result saying what was tried", graph.ErrInvalid)
		}
	}
	return nil
}

func (w *graphWriter) aboutSubject(about string) (string, error) {
	if about == "" {
		return "", nil
	}
	return w.resolveSubject(about)
}

func (w *graphWriter) createTodo(in graph.TodoInput) (graph.TodoResult, error) {
	if in.Title == "" {
		return graph.TodoResult{}, fmt.Errorf("%w: a new to-do needs a title", graph.ErrInvalid)
	}
	status := in.Status
	if status == "" {
		status = graph.TodoOpen
	}
	if err := w.settleCheck(status, in.Result); err != nil {
		return graph.TodoResult{}, err
	}
	about, err := w.aboutSubject(in.About)
	if err != nil {
		return graph.TodoResult{}, err
	}
	row := GraphTodo{
		ID: NewID("td_"), OwnerID: w.owner(), Title: in.Title, About: about, Status: status, Result: in.Result,
		Evidence: []graph.Evidence{}, Version: 1,
		ThreadID: w.caller.ThreadID, TurnID: w.caller.TurnID, AgentID: w.caller.AgentID, Generation: w.caller.Generation,
		UpdatedThreadID: w.caller.ThreadID, UpdatedTurnID: w.caller.TurnID, UpdatedBy: w.caller.AgentID, UpdatedGeneration: w.caller.Generation,
		CreatedAt: w.now, UpdatedAt: w.now,
	}
	if status == graph.TodoDone || status == graph.TodoFailed {
		row.Evidence = unionEvidence(nil, w.evidence)
		row.SettledAt = &w.now
		w.settled = append(w.settled, row.ID)
	}
	if err := w.loadOrder(); err != nil {
		return graph.TodoResult{}, err
	}
	// A not yet stored position forces savePositions to write the final index.
	row.Position = len(w.order)
	if err := w.tx.Create(&row).Error; err != nil {
		return graph.TodoResult{}, err
	}
	if in.Ref != "" {
		w.todoRefs[in.Ref] = row.ID
	}
	w.order = append(w.order, row.ID)
	if err := w.place(row.ID, in.Before); err != nil {
		return graph.TodoResult{}, err
	}
	w.stored[row.ID] = -1
	return graph.TodoResult{Ref: in.Ref, ID: row.ID, Version: row.Version, Status: status, Created: true}, nil
}

func (w *graphWriter) updateTodo(in graph.TodoInput) (graph.TodoResult, error) {
	var row GraphTodo
	if err := w.tx.Take(&row, "id = ? AND owner_id = ?", in.ID, w.owner()).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return graph.TodoResult{}, fmt.Errorf("%w: to-do %s does not exist here", graph.ErrInvalid, in.ID)
		}
		return graph.TodoResult{}, err
	}
	res := graph.TodoResult{Ref: in.Ref, ID: row.ID, Version: row.Version, Status: row.Status}
	if in.Ref != "" {
		w.todoRefs[in.Ref] = row.ID
	}
	if in.Version != 0 && in.Version != row.Version {
		return res, fmt.Errorf("%w: to-do %s is at version %d, not %d; read it again", graph.ErrConflict, row.ID, row.Version, in.Version)
	}
	about := row.About
	if in.About != "" {
		var err error
		if about, err = w.aboutSubject(in.About); err != nil {
			return res, err
		}
	}
	title, status, result := row.Title, row.Status, row.Result
	if in.Title != "" {
		title = in.Title
	}
	if in.Status != "" {
		status = in.Status
	}
	if in.Result != "" {
		result = in.Result
	}
	changed := title != row.Title || about != row.About || status != row.Status || result != row.Result
	if changed && in.Version == 0 {
		return res, fmt.Errorf("%w: changing a to-do needs its current version; moving it with before does not", graph.ErrInvalid)
	}
	if changed {
		if status != row.Status || result != row.Result {
			if err := w.settleCheck(status, result); err != nil {
				return res, err
			}
		}
		update := map[string]any{
			"title": title, "about": about, "status": status, "result": result, "version": row.Version + 1,
			"updated_at": w.now, "updated_thread_id": w.caller.ThreadID, "updated_turn_id": w.caller.TurnID,
			"updated_by": w.caller.AgentID, "updated_generation": w.caller.Generation,
		}
		settled := status == graph.TodoDone || status == graph.TodoFailed
		switch {
		case settled && status != row.Status:
			update["settled_at"] = w.now
			w.settled = append(w.settled, row.ID)
		case !settled:
			update["settled_at"] = nil
		}
		if settled {
			update["evidence"] = evidenceJSON(unionEvidence(row.Evidence, w.evidence))
		}
		if err := w.tx.Model(&GraphTodo{}).Where("id = ? AND version = ?", row.ID, row.Version).UpdateColumns(update).Error; err != nil {
			return res, err
		}
		res.Version, res.Status = row.Version+1, status
	}
	if in.Before != "" {
		if err := w.place(row.ID, in.Before); err != nil {
			return res, err
		}
	}
	return res, nil
}
