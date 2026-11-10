package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// ProjectSettings stays off the inbox list: instructions may be large and
// belong only in an explicit editor read, never every phone poll.
type ProjectSettings struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	SystemPrompt       string `json:"system_prompt"`
	Workdir            string `json:"workdir"`
	ResolvedWorkdir    string `json:"resolved_workdir"`
	MemoryEnabled      bool   `json:"memory_enabled"`
	NuwaEnabled        bool   `json:"nuwa_enabled"`
	PlannerProviderID  string `json:"nuwa_planner_provider_id,omitempty"`
	PlannerModel       string `json:"nuwa_planner_model,omitempty"`
	ExecutorProviderID string `json:"nuwa_executor_provider_id,omitempty"`
	ExecutorModel      string `json:"nuwa_executor_model,omitempty"`
}

type projectPatch struct {
	Name               *string `json:"name"`
	SystemPrompt       *string `json:"system_prompt"`
	Workdir            *string `json:"workdir"`
	MemoryEnabled      *bool   `json:"memory_enabled"`
	NuwaEnabled        *bool   `json:"nuwa_enabled"`
	PlannerProviderID  *string `json:"nuwa_planner_provider_id"`
	PlannerModel       *string `json:"nuwa_planner_model"`
	ExecutorProviderID *string `json:"nuwa_executor_provider_id"`
	ExecutorModel      *string `json:"nuwa_executor_model"`
}

func projectSettings(e *engine.Engine, p *store.Project) *ProjectSettings {
	return &ProjectSettings{ID: p.ID, Name: p.Name, SystemPrompt: p.SystemPrompt, Workdir: p.Workdir,
		ResolvedWorkdir: e.ProjectWorkdir(p), MemoryEnabled: p.MemoryEnabled, NuwaEnabled: p.NuwaEnabled,
		PlannerProviderID: p.NuwaPlannerProviderID, PlannerModel: p.NuwaPlannerModel,
		ExecutorProviderID: p.NuwaExecutorProviderID, ExecutorModel: p.NuwaExecutorModel}
}

func handleProjectSettings(e *engine.Engine, req Request, path, sessionID string) Response {
	req.Op = strings.TrimSpace(req.Op)
	resp := okBase(req.ID, path, sessionID)
	resp.MemoryAvailable = new(bool)
	*resp.MemoryAvailable = e.Config().Memory.Enabled
	var current *store.Project
	var err error
	if req.Op != OpProjectCreate {
		current, err = e.GetProject(req.ProjectID)
		if err != nil {
			return mapErr(req.ID, path, sessionID, err)
		}
	}
	if req.Op == OpProjectGet {
		resp.Project = projectSettings(e, current)
		if !timeoutReplyFits(resp) {
			return fail(req.ID, path, sessionID, "too_large", "project settings do not fit one frame")
		}
		return resp
	}
	var patch projectPatch
	d := json.NewDecoder(bytes.NewReader(req.ProjectPatch))
	d.DisallowUnknownFields()
	if err := d.Decode(&patch); err != nil || bytes.Equal(bytes.TrimSpace(req.ProjectPatch), []byte("null")) || len(req.ProjectPatch) > maxCatalogFrame/2 {
		return fail(req.ID, path, sessionID, "bad_request", "invalid project settings")
	}
	var trailing any
	if d.Decode(&trailing) != io.EOF || patch == (projectPatch{}) {
		return fail(req.ID, path, sessionID, "bad_request", "project settings required")
	}
	fields := []*string{patch.PlannerProviderID, patch.PlannerModel, patch.ExecutorProviderID, patch.ExecutorModel}
	count := 0
	for _, f := range fields {
		if f != nil {
			count++
		}
	}
	var pair *engine.NuwaModelPair
	if count != 0 {
		if count != 4 {
			return mapErr(req.ID, path, sessionID, engine.ErrNuwaModelInvalid)
		}
		pair = &engine.NuwaModelPair{PlannerProviderID: strings.TrimSpace(*fields[0]), PlannerModel: strings.TrimSpace(*fields[1]), ExecutorProviderID: strings.TrimSpace(*fields[2]), ExecutorModel: strings.TrimSpace(*fields[3])}
	}
	enabled := current != nil && current.NuwaEnabled
	if patch.NuwaEnabled != nil {
		enabled = *patch.NuwaEnabled
	}
	// Resolve before any writes; an unavailable model must not rename or enable
	// a project as a side effect of a refused save.
	pair, err = e.ResolveNuwaModelPair(enabled, current, pair)
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	if req.Op == OpProjectCreate {
		if patch.Name == nil {
			return fail(req.ID, path, sessionID, "bad_request", "project name required")
		}
		memory := e.Config().Memory.Enabled
		if patch.MemoryEnabled != nil {
			memory = *patch.MemoryEnabled
		}
		text, dir := "", ""
		if patch.SystemPrompt != nil {
			text = *patch.SystemPrompt
		}
		if patch.Workdir != nil {
			dir = *patch.Workdir
		}
		current, err = e.CreateProject(*patch.Name, text, dir, memory, enabled)
	} else {
		current, err = e.UpdateProject(req.ProjectID, patch.Name, patch.SystemPrompt, patch.Workdir, patch.MemoryEnabled, patch.NuwaEnabled)
	}
	if errors.Is(err, engine.ErrInvalidWorkdir) {
		return fail(req.ID, path, sessionID, "workdir", err.Error())
	}
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	if pair != nil {
		saved, saveErr := e.SaveNuwaModelPair(current.ID, *pair)
		if saveErr != nil {
			if req.Op == OpProjectCreate {
				_ = e.DeleteProject(current.ID)
			}
			return mapErr(req.ID, path, sessionID, saveErr)
		}
		current = saved
	}
	// Mutations acknowledge identity only, so an existing oversized instruction
	// cannot turn a successful name-only save into a lost reply.
	resp.Projects = []ProjectView{{ID: current.ID, Name: current.Name}}
	return resp
}
