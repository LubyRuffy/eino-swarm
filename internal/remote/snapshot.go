package remote

import (
	"encoding/base64"
	"fmt"
	"sort"
	"strings"
	"time"
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

// runningIDs is the In progress roster. Those rows already have a home;
// they must not also consume thread_limit or Recents starves to leftovers.
func runningIDs(rows []RunningView) map[string]struct{} {
	ids := make(map[string]struct{}, len(rows))
	for _, r := range rows {
		if r.ThreadID == "" {
			continue
		}
		ids[r.ThreadID] = struct{}{}
	}
	return ids
}

func excludeLiveThreads(all []store.Thread, live map[string]struct{}) []store.Thread {
	if len(live) == 0 {
		return all
	}
	out := make([]store.Thread, 0, len(all))
	for _, th := range all {
		if _, ok := live[th.ID]; ok {
			continue
		}
		out = append(out, th)
	}
	return out
}

func threadView(eng *engine.Engine, th store.Thread, cfg config.RemoteConfig) ThreadView {
	st := eng.Status(th.ID)
	title := strings.TrimSpace(th.Title)
	return ThreadView{
		ID:           th.ID,
		Title:        title,
		ProjectID:    th.ProjectID,
		Running:      st.Running,
		Waiting:      st.Waiting,
		LastActiveAt: th.LastActiveAt,
		Summary:      threadSummary(eng, th.ID, summaryChars(cfg)),
	}
}

func threadSummary(eng *engine.Engine, threadID string, chars int) string {
	turns, err := eng.Store().ListTurns(threadID)
	if err != nil || len(turns) == 0 {
		return ""
	}
	return summaryFromTurns(turns, chars)
}

func runningViews(eng *engine.Engine, cfg config.RemoteConfig) []RunningView {
	ids := eng.Running()
	waiting := eng.Waiting()
	if len(ids) == 0 && len(waiting) == 0 {
		return nil
	}
	chars := summaryChars(cfg)
	seen := make(map[string]int, len(ids)+len(waiting))
	out := make([]RunningView, 0, len(ids)+len(waiting))
	for _, id := range ids {
		th, err := eng.Store().GetThread(id)
		if err != nil {
			continue
		}
		seen[id] = len(out)
		out = append(out, runningView(eng, th, chars))
	}
	for _, id := range waiting {
		if i, ok := seen[id]; ok {
			out[i].Waiting = true
			continue
		}
		th, err := eng.Store().GetThread(id)
		if err != nil {
			continue
		}
		v := runningView(eng, th, chars)
		v.Waiting = true
		out = append(out, v)
	}
	return out
}

func runningView(eng *engine.Engine, th *store.Thread, chars int) RunningView {
	st := eng.Status(th.ID)
	v := RunningView{
		ThreadID:     th.ID,
		Title:        strings.TrimSpace(th.Title),
		TurnID:       st.TurnID,
		AskUser:      st.AwaitingAnswer,
		LastActiveAt: th.LastActiveAt,
	}
	if st.AwaitingAnswer {
		v.Action = "ask_user"
		return v
	}
	if st.TurnID == "" {
		// A parked wait is an In progress row with no live turn. Without a
		// line it is a title and a badge, which says nothing about what is
		// being waited on; the thread's own summary does.
		v.Action = threadSummary(eng, th.ID, chars)
		return v
	}
	evts, err := eng.Store().ListEvents(th.ID, 0, 0)
	if err != nil {
		return v
	}
	v.Action = clipPreview(runningPreview(evts, st.TurnID), chars)
	return v
}

func openDetail(eng *engine.Engine, threadID string, cfg config.RemoteConfig) (*ThreadDetail, error) {
	th, err := eng.Store().GetThread(threadID)
	if err != nil {
		return nil, err
	}
	d := threadDetail(eng, th, cfg)
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

func threadDetail(eng *engine.Engine, th *store.Thread, cfg config.RemoteConfig) *ThreadDetail {
	st := eng.Status(th.ID)
	d := &ThreadDetail{
		ID:              th.ID,
		Title:           strings.TrimSpace(th.Title),
		Goal:            th.Goal,
		GoalOn:          strings.TrimSpace(th.Goal) != "",
		GoalComplete:    th.GoalComplete,
		GoalBlocked:     th.GoalBlocked,
		GoalBlockReason: truncate(oneLine(th.GoalBlockReason), summaryChars(cfg)),
		GoalCapped:      th.GoalCapped,
		GoalIdle:        th.GoalIdle,
		PlanOn:          th.PlanMode,
		Waiting:         st.Waiting,
		Wake:            wakeView(eng, th.ID, cfg),
	}
	if th.GoalStartedAt != nil && !th.GoalStartedAt.IsZero() {
		d.GoalStartedAt = th.GoalStartedAt.UTC().Format(time.RFC3339)
	}
	if st.Running {
		rv := runningView(eng, th, summaryChars(cfg))
		d.Running = &rv
	}
	return d
}

func wakeView(eng *engine.Engine, threadID string, cfg config.RemoteConfig) *WakeView {
	row, err := eng.Store().ActiveThreadWake(threadID)
	if err != nil || row == nil {
		return nil
	}
	v := &WakeView{
		ID:     row.ID,
		Title:  strings.TrimSpace(row.Title),
		Prompt: truncate(oneLine(row.Prompt), summaryChars(cfg)),
	}
	if !row.NextRunAt.IsZero() {
		v.NextRunAt = row.NextRunAt.UTC().Format(time.RFC3339)
	}
	return v
}

func watchStatus(eng *engine.Engine, threadID string, cfg config.RemoteConfig) *WatchStatus {
	st := eng.Status(threadID)
	ws := &WatchStatus{
		Running:        st.Running,
		TurnID:         st.TurnID,
		AwaitingAnswer: st.AwaitingAnswer,
		Waiting:        st.Waiting,
	}
	if st.Waiting {
		ws.Wake = wakeView(eng, threadID, cfg)
	}
	return ws
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
