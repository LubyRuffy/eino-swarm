package remote

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
)

// maxCatalogFrame is the catalog reply budget. withHost adds the PC name
// after dispatch, so the check leaves room and a long display name cannot
// push the frame over pairlink's plaintext cap.
var maxCatalogFrame = MaxPushPayload - 512

func handleCatalog(eng *engine.Engine, req Request, path, sessionID string) Response {
	resp := okBase(req.ID, path, sessionID)
	resp.Models = modelViews(eng)
	resp.ReasoningLevels = config.ReasoningEfforts()
	raw, err := json.Marshal(resp)
	if err != nil {
		return fail(req.ID, path, sessionID, "", fmtErr(err))
	}
	if len(raw) > maxCatalogFrame {
		return fail(req.ID, path, sessionID, "too_large", "model list does not fit one frame")
	}
	return resp
}

func modelViews(eng *engine.Engine) []ModelView {
	if eng == nil || eng.Providers() == nil {
		return nil
	}
	listed := eng.Providers().List()
	out := make([]ModelView, 0, len(listed))
	for _, info := range listed {
		if !info.Ready || strings.TrimSpace(info.Model) == "" {
			continue
		}
		out = append(out, ModelView{
			ProviderID:    info.ProviderID,
			ProviderLabel: info.ProviderLabel,
			Model:         info.Model,
			Default:       info.Default,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func handleTune(eng *engine.Engine, req Request, path, sessionID string) Response {
	if strings.TrimSpace(req.ThreadID) == "" {
		return fail(req.ID, path, sessionID, "bad_request", "thread_id required")
	}
	if err := validateSelection(eng, req); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	if err := applySelection(eng, req.ThreadID, req); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	return okBase(req.ID, path, sessionID)
}

func acceptPut(stage *Staging, req Request, path, sessionID string) Response {
	if stage == nil {
		return fail(req.ID, path, sessionID, "bad_request", "upload is not available")
	}
	view, err := stage.Accept(req)
	if err != nil {
		return fail(req.ID, path, sessionID, "bad_request", err.Error())
	}
	resp := okBase(req.ID, path, sessionID)
	resp.Put = &view
	return resp
}

func composeStart(eng *engine.Engine, cfg config.RemoteConfig, stage *Staging, req Request, path, sessionID string) Response {
	if err := requireStage(stage, req); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	if err := validateSelection(eng, req); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	th, err := eng.CreateThread("", "", strings.TrimSpace(req.ProjectID))
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	if err := applySelection(eng, th.ID, req); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	in, err := inputFromPuts(eng, stage, th.ID, req)
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	in.Text = req.Text
	if _, err := eng.StartTurnInput(th.ID, in); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	return startedView(eng, cfg, req, th.ID, path, sessionID)
}

func composeSend(eng *engine.Engine, stage *Staging, req Request, path, sessionID string) Response {
	if strings.TrimSpace(req.ThreadID) == "" {
		return fail(req.ID, path, sessionID, "bad_request", "thread_id required")
	}
	if err := requireStage(stage, req); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	if err := prepareTurn(eng, req); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	in, err := inputFromPuts(eng, stage, req.ThreadID, req)
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	in.Text = req.Text
	if err := deliverUser(eng, req.ThreadID, in, eng.Status(req.ThreadID).Running); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	return attachFollowups(eng, okBase(req.ID, path, sessionID), req.ThreadID)
}

func composeSteer(eng *engine.Engine, stage *Staging, req Request, path, sessionID string) Response {
	if strings.TrimSpace(req.ThreadID) == "" {
		return fail(req.ID, path, sessionID, "bad_request", "thread_id required")
	}
	if err := requireStage(stage, req); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	if err := prepareTurn(eng, req); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	in, err := inputFromPuts(eng, stage, req.ThreadID, req)
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	in.Text = req.Text
	err = eng.SteerInput(req.ThreadID, in)
	if errors.Is(err, engine.ErrIdle) {
		_, err = eng.StartTurnInput(req.ThreadID, in)
	}
	return opErr(req.ID, path, sessionID, err)
}

func requireStage(stage *Staging, req Request) error {
	if len(req.Puts) == 0 || stage != nil {
		return nil
	}
	return fmt.Errorf("remote: upload is not available")
}

func prepareTurn(eng *engine.Engine, req Request) error {
	if err := validateSelection(eng, req); err != nil {
		return err
	}
	return applySelection(eng, req.ThreadID, req)
}

func startedView(eng *engine.Engine, cfg config.RemoteConfig, req Request, threadID, path, sessionID string) Response {
	resp := okBase(req.ID, path, sessionID)
	loaded, err := eng.Store().GetThread(threadID)
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	resp.Threads = []ThreadView{threadView(eng, *loaded, cfg)}
	resp.Running = runningViews(eng, cfg)
	return resp
}

func validateSelection(eng *engine.Engine, req Request) error {
	if strings.TrimSpace(req.ProviderID) != "" || strings.TrimSpace(req.Model) != "" {
		if eng == nil || eng.Providers() == nil {
			return fmt.Errorf("provider: unknown provider %q", req.ProviderID)
		}
		if _, err := eng.Providers().ResolveModel(req.ProviderID, req.Model); err != nil {
			return err
		}
	}
	if req.Reasoning != nil {
		return checkReasoning(*req.Reasoning)
	}
	return nil
}

func checkReasoning(effort string) error {
	normalized := config.NormalizeReasoning(effort)
	if normalized == "" && strings.TrimSpace(effort) != "" {
		return fmt.Errorf("engine: unknown thinking level %q", effort)
	}
	return nil
}

func applySelection(eng *engine.Engine, threadID string, req Request) error {
	if strings.TrimSpace(req.ProviderID) != "" || strings.TrimSpace(req.Model) != "" {
		if err := eng.SetThreadProvider(threadID, req.ProviderID, req.Model); err != nil {
			return err
		}
	}
	if req.Reasoning != nil {
		if err := eng.SetThreadReasoning(threadID, *req.Reasoning); err != nil {
			return err
		}
	}
	return nil
}

func inputFromPuts(eng *engine.Engine, stage *Staging, threadID string, req Request) (engine.UserInput, error) {
	if len(req.Puts) == 0 {
		return engine.UserInput{}, nil
	}
	blobs, err := stage.Take(req.Puts)
	if err != nil {
		return engine.UserInput{}, err
	}
	return materialize(eng, threadID, blobs)
}

func materialize(eng *engine.Engine, threadID string, blobs []StagedBlob) (engine.UserInput, error) {
	var in engine.UserInput
	var raws []engine.RawImage
	for _, b := range blobs {
		if visionClaim(b.MIME, b.Name) {
			raws = append(raws, engine.RawImage{
				Name: b.Name,
				MIME: b.MIME,
				Data: base64.StdEncoding.EncodeToString(b.Data),
			})
			continue
		}
		payload := append([]byte(nil), b.Data...)
		att, err := eng.SaveUpload(threadID, b.Name, func(dst string) error {
			return os.WriteFile(dst, payload, 0o600)
		})
		if err != nil {
			return engine.UserInput{}, err
		}
		in.Files = append(in.Files, att.RelPath)
	}
	imgs, err := engine.DecodeImages(raws)
	if err != nil {
		return engine.UserInput{}, err
	}
	in.Images = imgs
	return in, nil
}

func deliverUser(eng *engine.Engine, threadID string, in engine.UserInput, running bool) error {
	attached := len(in.Images) > 0 || len(in.Files) > 0
	if running && !attached {
		if _, err := eng.EnqueueFollowup(threadID, in.Text); err != nil {
			if !errors.Is(err, engine.ErrIdle) {
				return err
			}
		} else {
			return nil
		}
	}
	if running && attached {
		if err := eng.SteerInput(threadID, in); err != nil {
			if !errors.Is(err, engine.ErrIdle) {
				return err
			}
		} else {
			return nil
		}
	}
	_, err := eng.StartTurnInput(threadID, in)
	return err
}
