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
	"github.com/LubyRuffy/eino-swarm/internal/slash"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

// maxCatalogFrame is the catalog reply budget. withHost adds the PC name
// after dispatch, so the check leaves room and a long display name cannot
// push the frame over pairlink's plaintext cap.
var maxCatalogFrame = MaxPushPayload - 512

func handleCatalog(eng *engine.Engine, req Request, path, sessionID string) Response {
	resp := okBase(req.ID, path, sessionID)
	policy := eng.ModelPolicyView()
	resp.ModelPolicy = &policy
	resp.ComposerPredictions = new(bool)
	*resp.ComposerPredictions = eng.Config().SwarmSnapshot().ComposerPredictions
	resp.MemoryAvailable = new(bool)
	*resp.MemoryAvailable = eng.Config().Memory.Enabled
	resp.Models = modelViews(eng)
	resp.ReasoningLevels = config.ReasoningEfforts()
	resp.WebExecutionSupported = true
	webStatus := tools.DescribeWebExecution(eng.Config())
	resp.WebExecutionStatus = &webStatus
	resp.Speech = speechSnapshot(eng)
	resp = boundSpeechResponse(resp, false)
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
	if !eng.Providers().IsMock() && !eng.Providers().ModelPolicy().Ready {
		return nil
	}
	listed := eng.Providers().List()
	out := make([]ModelView, 0, len(listed))
	for _, info := range listed {
		if strings.TrimSpace(info.Model) == "" {
			continue
		}
		if !info.Ready && !(info.Default && info.DefaultFollowsSetting) {
			continue
		}
		label := info.Label
		if label == info.Model {
			label = ""
		}
		out = append(out, ModelView{
			CatalogGroup:          info.CatalogGroup,
			ProviderID:            info.ProviderID,
			ProviderLabel:         info.ProviderLabel,
			Label:                 label,
			Model:                 info.Model,
			Default:               info.Default,
			DefaultFollowsSetting: info.DefaultFollowsSetting,
			Ready:                 info.Ready,
			Locked:                eng.Providers().OfficialOnly() || (info.Capabilities != nil && !info.Capabilities.Capabilities["conversation"].Configurable),
		})
	}
	if len(out) == 0 {
		return nil
	}
	// A phone starting a conversation uses the first default row. Keep each
	// provider's own default for existing conversations on that provider.
	defaultProvider := eng.Providers().DefaultProviderID()
	for i, row := range out {
		if row.Default && row.ProviderID == defaultProvider {
			copy(out[1:i+1], out[:i])
			out[0] = row
			break
		}
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
	if engine.IsImageCommand(req.Text) && len(req.Puts) > 0 {
		return mapErr(req.ID, path, sessionID, engine.ErrImageTextOnly)
	}
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
	if engine.IsImageCommand(req.Text) && len(req.Puts) > 0 {
		return mapErr(req.ID, path, sessionID, engine.ErrImageTextOnly)
	}
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
	if engine.IsImageCommand(req.Text) {
		return composeSend(eng, stage, req, path, sessionID)
	}
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

func composeDelivery(eng *engine.Engine, stage *Staging, req Request, path, sessionID string) Response {
	mode := engine.DeliveryMode(req.DeliveryMode)
	if req.ThreadID == "" || (req.FollowupID != "" && (req.Text != "" || len(req.Puts) != 0)) {
		return mapErr(req.ID, path, sessionID, engine.ErrInvalidDelivery)
	}
	if err := requireStage(stage, req); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	if mode == engine.DeliveryQueue {
		if err := prepareTurn(eng, req); err != nil {
			return mapErr(req.ID, path, sessionID, err)
		}
	}
	in, err := inputFromPuts(eng, stage, req.ThreadID, req)
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	in.Text = req.Text
	receipt, err := eng.DeliverComposerInput(req.ThreadID, mode, req.ExpectedTurn, req.FollowupID, in)
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	resp := okBase(req.ID, path, sessionID)
	resp.Delivery = &DeliveryView{TurnID: receipt.TurnID, SteerSeq: receipt.SteerSeq, Preempted: receipt.Preempted}
	if receipt.Turn != nil {
		resp.Delivery.TurnID = receipt.Turn.ID
	}
	return attachFollowups(eng, resp, req.ThreadID)
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
	if req.WebExecution != nil {
		if _, err := config.ParseWebExecution(*req.WebExecution); err != nil {
			return err
		}
	}
	if strings.TrimSpace(req.ProviderID) != "" || strings.TrimSpace(req.Model) != "" {
		if eng == nil || eng.Providers() == nil {
			return fmt.Errorf("provider: unknown provider %q", req.ProviderID)
		}
		// Repeating the conversation's stored pair is not a new choice. A
		// managed preset still executes its own model and keeps the binding.
		if !(eng.Providers().OfficialOnly() && repeatsStoredConversation(eng, req.ThreadID, req)) {
			if err := eng.Providers().ValidateConversationSelection(req.ProviderID, req.Model); err != nil {
				return err
			}
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

func repeatsStoredConversation(eng *engine.Engine, threadID string, req Request) bool {
	if eng == nil || eng.Store() == nil || strings.TrimSpace(threadID) == "" {
		return false
	}
	th, err := eng.Store().GetThread(threadID)
	if err != nil {
		return false
	}
	id := strings.TrimSpace(req.ProviderID)
	if id == "" {
		id = th.ProviderID
	}
	name := strings.TrimSpace(req.Model)
	stored := strings.TrimSpace(th.Model)
	if name == "" {
		name = stored
	}
	return id == th.ProviderID && name == stored
}

func keepsSettingsDefault(eng *engine.Engine, threadID string, req Request) bool {
	if eng == nil || eng.Store() == nil || eng.Providers() == nil {
		return false
	}
	th, err := eng.Store().GetThread(threadID)
	if err != nil || strings.TrimSpace(th.Model) != "" {
		return false
	}
	providerID := strings.TrimSpace(req.ProviderID)
	if providerID == "" {
		providerID = th.ProviderID
	}
	if strings.TrimSpace(th.ProviderID) != "" && providerID != th.ProviderID {
		return false
	}
	name, follows, ok := eng.Providers().ConversationDefault(providerID)
	return ok && follows && name != "" && strings.TrimSpace(req.Model) == name
}

func applySelection(eng *engine.Engine, threadID string, req Request) error {
	if req.WebExecution != nil {
		if err := eng.SetThreadWebExecution(threadID, *req.WebExecution); err != nil {
			return err
		}
	}
	if strings.TrimSpace(req.ProviderID) != "" || strings.TrimSpace(req.Model) != "" {
		// Repeating the user-configured default must not freeze it onto the
		// conversation, or a later settings change would stop applying.
		// A managed preset also leaves a historical binding untouched.
		historical := eng.Providers() != nil && eng.Providers().OfficialOnly() && repeatsStoredConversation(eng, threadID, req)
		if !historical && !keepsSettingsDefault(eng, threadID, req) {
			if err := eng.SetThreadProvider(threadID, req.ProviderID, req.Model); err != nil {
				return err
			}
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
	if engine.IsImageCommand(in.Text) {
		_, err := eng.StartTurnInput(threadID, in)
		return err
	}
	// Goal commands change the current task's lifetime immediately, even when
	// an ordinary phone Send would queue behind the current manager turn.
	if _, goal := slash.Lookup(in.Text, "goal"); goal {
		_, err := eng.StartTurnInput(threadID, in)
		return err
	}
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
