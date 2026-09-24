package remote

import (
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// inboxGroups is the first page of every project plus Recents. The global
// idle page stays on the response for a phone that still has one More button.
func inboxGroups(eng *engine.Engine, cfg config.RemoteConfig, projects []ProjectView, idle []store.Thread) []ThreadGroup {
	out := make([]ThreadGroup, 0, len(projects)+1)
	for _, p := range projects {
		out = append(out, threadGroup(eng, cfg, p.ID, idle))
	}
	out = append(out, threadGroup(eng, cfg, GroupRecent, idle))
	return out
}

func threadGroup(eng *engine.Engine, cfg config.RemoteConfig, id string, idle []store.Thread) ThreadGroup {
	page, next, more := pageThreads(threadsInGroup(idle, id), "", cfg.ThreadLimit)
	g := ThreadGroup{ID: id, More: more, Next: next}
	for _, th := range page {
		g.Threads = append(g.Threads, threadView(eng, th, cfg))
	}
	return g
}

func threadsInGroup(idle []store.Thread, group string) []store.Thread {
	out := make([]store.Thread, 0)
	for _, th := range idle {
		if group == GroupRecent {
			if th.ProjectID == "" {
				out = append(out, th)
			}
			continue
		}
		if th.ProjectID == group {
			out = append(out, th)
		}
	}
	return out
}

func followupViews(rows []store.Followup) *[]FollowupView {
	out := make([]FollowupView, 0, len(rows))
	for _, row := range rows {
		out = append(out, FollowupView{ID: row.ID, Seq: row.Seq, Text: row.Text})
	}
	return &out
}

func attachFollowups(eng *engine.Engine, resp Response, threadID string) Response {
	rows, err := eng.ListFollowups(threadID)
	if err != nil {
		return resp
	}
	resp.Followups = followupViews(rows)
	return resp
}
