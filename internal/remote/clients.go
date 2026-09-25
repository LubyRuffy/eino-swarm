package remote

import (
	"strconv"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/clients"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
)

// clientWireLimit keeps a sealed frame small. More walks the rest.
const clientWireLimit = 30

func handleClientRead(eng *engine.Engine, req Request, path, sessionID string) Response {
	doc, ok := clients.Read(eng.Config().Clients, strings.TrimSpace(req.TaskID), time.Now())
	if !ok {
		return fail(req.ID, path, sessionID, "not_found", "task not found")
	}
	view := &ClientView{
		ID: doc.ID, Title: doc.Title, Status: doc.Status, UpdatedAt: doc.UpdatedAt,
		Truncated: doc.Truncated, Entries: []ClientEntry{},
	}
	entries := doc.Entries
	if trimmed, cut := clients.TrimKeepingRequest(entries, 30); cut {
		entries = trimmed
		view.Truncated = true
	}
	for _, e := range entries {
		text := e.Text
		if len([]rune(text)) > 180 {
			text = string([]rune(text)[:179]) + "…"
		}
		view.Entries = append(view.Entries, ClientEntry{Role: e.Role, Text: text})
	}
	resp := okBase(req.ID, path, sessionID)
	resp.ClientView = view
	return resp
}

func handleClients(eng *engine.Engine, req Request, path, sessionID string) Response {
	resp := okBase(req.ID, path, sessionID)
	resp.Clients = clientCatalog(eng.Config().Clients, req.Before, true)
	if req.Group != "" && resp.Clients != nil {
		filtered := []ClientTool{}
		for _, tool := range resp.Clients.Tools {
			if tool.ID == req.Group {
				filtered = append(filtered, tool)
			}
		}
		resp.Clients.Tools = filtered
	}
	return resp
}

// clientCatalog reads the last finished walk. includePending keeps a
// not-yet-scanned catalog on the clients op so that poll can show the
// switch without waiting. The inbox list omits it: a pending payload
// would ride the same RPC that has to answer inside the phone's timeout.
func clientCatalog(cfg config.ClientsConfig, before int64, includePending bool) *ClientCatalog {
	cat := clients.View(cfg, time.Now(), before)
	if cat.Pending && !includePending {
		return nil
	}
	out := &ClientCatalog{Enabled: cat.Enabled, Pending: cat.Pending}
	if cat.Pending {
		return out
	}
	if !cat.Enabled {
		return out
	}
	for _, g := range cat.Tools {
		tool := ClientTool{ID: g.ID, Tasks: []ClientTask{}, More: g.More, Next: g.Next}
		tasks := g.Tasks
		if len(tasks) > clientWireLimit {
			tool.More = true
			tool.Next = strconv.FormatInt(tasks[clientWireLimit-1].UpdatedAt.UnixMilli(), 10)
			tasks = tasks[:clientWireLimit]
		}
		for _, task := range tasks {
			tool.Tasks = append(tool.Tasks, ClientTask{
				ID:        task.ID,
				Title:     task.Title,
				Status:    task.Status,
				UpdatedAt: task.UpdatedAt,
			})
		}
		out.Tools = append(out.Tools, tool)
	}
	return out
}
