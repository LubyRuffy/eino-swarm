package remote

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func truncate(s string, n int) string {
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return string(r[:n]) + "…"
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.Join(strings.Fields(s), " ")
}

func listProjects(eng *engine.Engine) ([]ProjectView, error) {
	ps, err := eng.Store().ListProjects()
	if err != nil {
		return nil, err
	}
	out := make([]ProjectView, 0, len(ps))
	for _, p := range ps {
		out = append(out, ProjectView{ID: p.ID, Name: p.Name})
	}
	return out, nil
}

func sortedThreads(eng *engine.Engine) ([]store.Thread, error) {
	all, err := eng.Store().ListThreads(false, "")
	if err != nil {
		return nil, err
	}
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].LastActiveAt.After(all[j].LastActiveAt)
	})
	return all, nil
}

func pageThreads(all []store.Thread, cursor string, limit int) (page []store.Thread, next string, more bool) {
	if limit <= 0 {
		limit = config.DefaultRemoteThreadLimit
	}
	start := 0
	if cursor != "" {
		if raw, err := base64.RawURLEncoding.DecodeString(cursor); err == nil {
			want := string(raw)
			for i, th := range all {
				if th.ID == want {
					start = i + 1
					break
				}
			}
		}
	}
	if start > len(all) {
		start = len(all)
	}
	end := start + limit
	if end > len(all) {
		end = len(all)
	}
	page = all[start:end]
	if end < len(all) && len(page) > 0 {
		more = true
		next = encodeCursor(page[len(page)-1].ID)
	}
	return page, next, more
}

func threadView(eng *engine.Engine, th store.Thread, cfg config.RemoteConfig) ThreadView {
	st := eng.Status(th.ID)
	title := strings.TrimSpace(th.Title)
	return ThreadView{
		ID:           th.ID,
		Title:        title,
		ProjectID:    th.ProjectID,
		Running:      st.Running,
		LastActiveAt: th.LastActiveAt,
		Summary:      threadSummary(eng, th.ID, cfg.SummaryChars),
	}
}

func threadSummary(eng *engine.Engine, threadID string, chars int) string {
	turns, err := eng.Store().ListTurns(threadID)
	if err != nil || len(turns) == 0 {
		return ""
	}
	for i := len(turns) - 1; i >= 0; i-- {
		if t := oneLine(turns[i].Final); t != "" {
			return truncate(t, chars)
		}
		if t := oneLine(turns[i].UserText); t != "" {
			return truncate(t, chars)
		}
	}
	return ""
}

func runningViews(eng *engine.Engine) []RunningView {
	ids := eng.Running()
	if len(ids) == 0 {
		return nil
	}
	out := make([]RunningView, 0, len(ids))
	for _, id := range ids {
		th, err := eng.Store().GetThread(id)
		if err != nil {
			continue
		}
		out = append(out, runningView(eng, th))
	}
	return out
}

func runningView(eng *engine.Engine, th *store.Thread) RunningView {
	st := eng.Status(th.ID)
	v := RunningView{
		ThreadID: th.ID,
		Title:    strings.TrimSpace(th.Title),
		TurnID:   st.TurnID,
		AskUser:  st.AwaitingAnswer,
	}
	if st.AwaitingAnswer {
		v.Action = "ask_user"
		return v
	}
	if st.TurnID == "" {
		return v
	}
	evts, err := eng.Store().ListEvents(th.ID, 0, 0)
	if err != nil {
		return v
	}
	for i := len(evts) - 1; i >= 0; i-- {
		if evts[i].TurnID != st.TurnID {
			continue
		}
		if evts[i].Kind == "tool_call" && strings.TrimSpace(evts[i].Text) != "" {
			v.Action = strings.TrimSpace(evts[i].Text)
			return v
		}
	}
	return v
}

func openDetail(eng *engine.Engine, threadID string, cfg config.RemoteConfig) (*ThreadDetail, error) {
	th, err := eng.Store().GetThread(threadID)
	if err != nil {
		return nil, err
	}
	d := &ThreadDetail{
		ID:     th.ID,
		Title:  strings.TrimSpace(th.Title),
		Goal:   th.Goal,
		GoalOn: strings.TrimSpace(th.Goal) != "" && !th.GoalComplete,
		PlanOn: th.PlanMode,
	}
	st := eng.Status(th.ID)
	if st.Running {
		rv := runningView(eng, th)
		d.Running = &rv
	}
	turns, err := eng.Store().ListTurns(th.ID)
	if err != nil {
		return nil, err
	}
	n := cfg.OpenTurns
	if n <= 0 {
		n = config.DefaultRemoteOpenTurns
	}
	done := make([]store.Turn, 0, n)
	for i := len(turns) - 1; i >= 0 && len(done) < n; i-- {
		if turns[i].Status == store.TurnRunning {
			continue
		}
		done = append(done, turns[i])
	}
	for i := len(done) - 1; i >= 0; i-- {
		t := done[i]
		text := t.Final
		if text == "" {
			text = t.UserText
		}
		d.Turns = append(d.Turns, TurnView{
			ID:     t.ID,
			Status: t.Status,
			Text:   truncate(oneLine(text), cfg.SummaryChars),
		})
	}
	return d, nil
}

func fail(id, path, sessionID, code, msg string) Response {
	return Response{
		V: ProtocolV, ID: id, Path: path, SessionID: sessionID,
		Error: msg, Code: code,
	}
}

func okBase(id, path, sessionID string) Response {
	return Response{V: ProtocolV, ID: id, OK: true, Path: path, SessionID: sessionID}
}

func encodeCursor(id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(id))
}

func fmtErr(err error) string {
	if err == nil {
		return ""
	}
	return fmt.Sprint(err)
}
