package remote

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Handle runs one slim RPC against the local engine. Pairlink only
// transports the bytes; this is the application protocol.
func Handle(eng *engine.Engine, cfg config.RemoteConfig, req Request, path, sessionID string) Response {
	return HandleWith(eng, cfg, nil, req, path, sessionID)
}

// HandleWith is Handle plus the link's upload staging. A nil stage still
// serves every op that does not carry bytes; put and a send that names puts
// fail closed instead of pretending the file arrived.
func HandleWith(eng *engine.Engine, cfg config.RemoteConfig, stage *Staging, req Request, path, sessionID string) Response {
	return withHost(dispatch(eng, cfg, stage, req, path, sessionID), cfg)
}

func dispatch(eng *engine.Engine, cfg config.RemoteConfig, stage *Staging, req Request, path, sessionID string) Response {
	if req.V != 0 && req.V != ProtocolV {
		return fail(req.ID, path, sessionID, "bad_version", "unsupported protocol version")
	}
	switch strings.TrimSpace(req.Op) {
	case OpList, OpMore:
		return handleList(eng, cfg, req, path, sessionID)
	case OpClients:
		return handleClients(eng, req, path, sessionID)
	case OpOpen:
		return handleOpen(eng, cfg, req, path, sessionID)
	case OpStart:
		return composeStart(eng, cfg, stage, req, path, sessionID)
	case OpSend:
		return composeSend(eng, stage, req, path, sessionID)
	case OpSteer:
		return composeSteer(eng, stage, req, path, sessionID)
	case OpCatalog:
		return handleCatalog(eng, req, path, sessionID)
	case OpTune:
		return handleTune(eng, req, path, sessionID)
	case OpPut:
		return acceptPut(stage, req, path, sessionID)
	case OpFollowupDrop:
		if err := eng.DeleteFollowup(req.ThreadID, req.FollowupID); err != nil {
			return mapErr(req.ID, path, sessionID, err)
		}
		return attachFollowups(eng, okBase(req.ID, path, sessionID), req.ThreadID)
	case OpFollowupSteer:
		err := eng.SteerFollowup(req.ThreadID, req.FollowupID)
		if err != nil {
			return mapErr(req.ID, path, sessionID, err)
		}
		return attachFollowups(eng, okBase(req.ID, path, sessionID), req.ThreadID)
	case OpPreempt:
		return opErr(req.ID, path, sessionID, eng.Preempt(req.ThreadID))
	case OpStop:
		err := eng.Interrupt(req.ThreadID)
		return opErr(req.ID, path, sessionID, err)
	case OpAnswer:
		return handleAnswer(eng, req, path, sessionID)
	case OpLog:
		return handleLog(eng, cfg, req, path, sessionID)
	case OpRunNow:
		return handleRunNow(eng, req, path, sessionID)
	case OpCancelWait:
		return handleCancelWait(eng, req, path, sessionID)
	case OpResumeGoal:
		_, err := eng.ResumeThreadGoal(req.ThreadID)
		return opErr(req.ID, path, sessionID, err)
	default:
		return fail(req.ID, path, sessionID, "unknown_op", "unknown op")
	}
}

func withHost(resp Response, cfg config.RemoteConfig) Response {
	if !resp.OK {
		return resp
	}
	name := strings.TrimSpace(cfg.DisplayName)
	if name == "" {
		return resp
	}
	resp.Host = config.SeedRemoteDisplayName(name)
	return resp
}

func handleList(eng *engine.Engine, cfg config.RemoteConfig, req Request, path, sessionID string) Response {
	resp := okBase(req.ID, path, sessionID)
	ps, err := listProjects(eng)
	if err != nil {
		return fail(req.ID, path, sessionID, "", fmtErr(err))
	}
	resp.Projects = ps
	resp.Running = runningViews(eng, cfg)
	all, err := sortedThreads(eng)
	if err != nil {
		return fail(req.ID, path, sessionID, "", fmtErr(err))
	}
	idle := excludeLiveThreads(all, runningIDs(resp.Running))
	bucket := idle
	if req.Group != "" {
		bucket = threadsInGroup(idle, req.Group)
	}
	page, next, more := pageThreads(bucket, req.Cursor, cfg.ThreadLimit)
	for _, th := range page {
		resp.Threads = append(resp.Threads, threadView(eng, th, cfg))
	}
	resp.More = more
	resp.Next = next
	// The first page names every section. A later page is one section, and
	// an old phone never sends Group, so its global cursor is unchanged.
	if req.Op == OpList && req.Group == "" && req.Cursor == "" {
		resp.Groups = inboxGroups(eng, cfg, ps, idle)
		resp.Clients = clientCatalog(eng.Config().Clients, 0)
	}
	return resp
}

func handleOpen(eng *engine.Engine, cfg config.RemoteConfig, req Request, path, sessionID string) Response {
	d, err := openDetail(eng, req.ThreadID, cfg)
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	resp := okBase(req.ID, path, sessionID)
	resp.Detail = d
	return resp
}

func handleAnswer(eng *engine.Engine, req Request, path, sessionID string) Response {
	if len(req.Answers) > 0 && string(req.Answers) != "null" {
		var answers engine.AskAnswers
		if err := json.Unmarshal(req.Answers, &answers); err != nil {
			return fail(req.ID, path, sessionID, "bad_request", "could not read answers")
		}
		return opErr(req.ID, path, sessionID, eng.AnswerTurn(req.ThreadID, req.CallID, answers))
	}
	return opErr(req.ID, path, sessionID, eng.AnswerTurnText(req.ThreadID, req.Text))
}

func handleRunNow(eng *engine.Engine, req Request, path, sessionID string) Response {
	row, err := parkedWake(eng, req.ThreadID)
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	_, err = eng.RunScheduleNow(row.ID)
	return opErr(req.ID, path, sessionID, err)
}

func handleCancelWait(eng *engine.Engine, req Request, path, sessionID string) Response {
	row, err := parkedWake(eng, req.ThreadID)
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	return opErr(req.ID, path, sessionID, eng.CancelSchedule(row.ID))
}

func parkedWake(eng *engine.Engine, threadID string) (*store.Schedule, error) {
	row, err := eng.Store().ActiveThreadWake(threadID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, engine.ErrIdle
	}
	return row, nil
}

func opErr(id, path, sessionID string, err error) Response {
	if err != nil {
		return mapErr(id, path, sessionID, err)
	}
	return okBase(id, path, sessionID)
}

func mapErr(id, path, sessionID string, err error) Response {
	switch {
	case errors.Is(err, engine.ErrBusy):
		return fail(id, path, sessionID, "busy", err.Error())
	case errors.Is(err, engine.ErrIdle):
		return fail(id, path, sessionID, "idle", err.Error())
	case errors.Is(err, engine.ErrSkippedBusy):
		return fail(id, path, sessionID, "skipped_busy", err.Error())
	case errors.Is(err, store.ErrNotFound):
		return fail(id, path, sessionID, "not_found", err.Error())
	default:
		return fail(id, path, sessionID, "", fmtErr(err))
	}
}
