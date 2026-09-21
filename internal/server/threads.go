package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/gin-gonic/gin"
)

type createThreadRequest struct {
	Title      string `json:"title"`
	ProviderID string `json:"provider_id"`
	ProjectID  string `json:"project_id"`
}

type threadView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// TitleAuto is always sent: a missing field is indistinguishable from
	// false, and the sidebar needs the true value to keep a generated name
	// across a listing fetch that still has the placeholder.
	TitleAuto       bool   `json:"title_auto"`
	ProjectID       string `json:"project_id"`
	ProviderID      string `json:"provider_id"`
	Model           string `json:"model"`
	ReasoningEffort string `json:"reasoning_effort"`
	Goal            string `json:"goal"`
	GoalComplete    bool   `json:"goal_complete"`
	GoalBlocked     bool   `json:"goal_blocked"`
	GoalBlockReason string `json:"goal_block_reason,omitempty"`
	GoalCapped      bool   `json:"goal_capped"`
	GoalIdle        bool   `json:"goal_idle"`
	PlanMode        bool   `json:"plan_mode"`
	PlanMarkdown    string `json:"plan_markdown,omitempty"`
	GoalAutoTurns   int    `json:"goal_auto_turns,omitempty"`
	GoalStartedAt   string `json:"goal_started_at,omitempty"`
	Compacted       bool   `json:"compacted"`
	ContextChars    int    `json:"context_chars,omitempty"`
	ContextBudget   int    `json:"context_budget,omitempty"`
	Archived        bool   `json:"archived"`
	Pinned          bool   `json:"pinned"`
	PinnedAt        string `json:"pinned_at,omitempty"`
	SortRank        int    `json:"sort_rank"`
	CreatedAt       string `json:"created_at"`
	LastActiveAt    string `json:"last_active_at"`
	Running         bool   `json:"running"`
	// Live only: ask_user is blocked waiting for the human. The turn is
	// still Running. Omitted when idle so a listing of finished rows
	// does not dump a field of falses.
	AwaitingAnswer bool `json:"awaiting_answer,omitempty"`
	// Parked thread wake: the next turn is that wait, not a live tool call.
	// Omitted when idle-without-a-wait so a listing of finished rows does
	// not dump a field of falses.
	Waiting bool `json:"waiting,omitempty"`
}

func viewThread(th *store.Thread, running, awaitingAnswer, waiting bool) threadView {
	v := threadView{
		ID:              th.ID,
		Title:           th.Title,
		TitleAuto:       th.TitleAuto,
		ProjectID:       th.ProjectID,
		ProviderID:      th.ProviderID,
		Model:           th.Model,
		ReasoningEffort: th.ReasoningEffort,
		Goal:            th.Goal,
		GoalComplete:    th.GoalComplete,
		GoalBlocked:     th.GoalBlocked,
		GoalBlockReason: th.GoalBlockReason,
		GoalCapped:      th.GoalCapped,
		GoalIdle:        th.GoalIdle,
		PlanMode:        th.PlanMode,
		PlanMarkdown:    th.PlanMarkdown,
		GoalAutoTurns:   th.GoalAutoTurns,
		Compacted:       strings.TrimSpace(th.CompactSummary) != "" && th.CompactThroughSeq > 0,
		Archived:        th.Archived,
		Pinned:          th.Pinned,
		SortRank:        th.SortRank,
		CreatedAt:       th.CreatedAt.Format(timeFormat),
		LastActiveAt:    th.LastActiveAt.Format(timeFormat),
		Running:         running,
		AwaitingAnswer:  awaitingAnswer,
		Waiting:         waiting,
	}
	if th.GoalStartedAt != nil && !th.GoalStartedAt.IsZero() {
		v.GoalStartedAt = th.GoalStartedAt.Format(timeFormat)
	}
	if th.PinnedAt != nil && !th.PinnedAt.IsZero() {
		v.PinnedAt = th.PinnedAt.Format(timeFormat)
	}
	return v
}

func viewThreadWithUsage(th *store.Thread, running, awaitingAnswer, waiting bool, chars, budget int) threadView {
	v := viewThread(th, running, awaitingAnswer, waiting)
	v.ContextChars = chars
	v.ContextBudget = budget
	return v
}

func (s *Server) listThreads(c *gin.Context) {
	threads, err := s.engine.Store().ListThreads(c.Query("archived") == "1", c.Query("project"))
	if err != nil {
		s.fail(c, err)
		return
	}
	// One pass over the live runtimes instead of a status lookup per row: the
	// sidebar renders every conversation, and most of them are idle.
	running := map[string]bool{}
	for _, id := range s.engine.Running() {
		running[id] = true
	}
	asking := map[string]bool{}
	for _, id := range s.engine.AwaitingAnswer() {
		asking[id] = true
	}
	waiting := map[string]bool{}
	for _, id := range s.engine.Waiting() {
		waiting[id] = true
	}
	out := make([]threadView, 0, len(threads))
	for i := range threads {
		out = append(out, viewThread(&threads[i], running[threads[i].ID], asking[threads[i].ID], waiting[threads[i].ID]))
	}
	c.JSON(http.StatusOK, gin.H{"threads": out})
}

const timeFormat = "2006-01-02T15:04:05.000Z07:00"

func (s *Server) createThread(c *gin.Context) {
	var req createThreadRequest
	if err := c.ShouldBindJSON(&req); err != nil && err.Error() != "EOF" {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	th, err := s.engine.CreateThread(req.Title, req.ProviderID, req.ProjectID)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"thread": viewThread(th, false, false, false)})
}

func (s *Server) getThread(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	status := s.engine.Status(th.ID)
	chars, budget, err := s.engine.ContextUsage(th.ID)
	if err != nil {
		s.fail(c, err)
		return
	}
	usage, err := s.engine.Usage(th.ID, status.TurnID)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"thread": viewThreadWithUsage(th, status.Running, status.AwaitingAnswer, status.Waiting, chars, budget),
		"status": status,
		"usage":  usage,
	})
}

type patchThreadRequest struct {
	Title           *string `json:"title"`
	ProviderID      *string `json:"provider_id"`
	Model           *string `json:"model"`
	ReasoningEffort *string `json:"reasoning_effort"`
	Goal            *string `json:"goal"`
	// GoalEdit, with goal, changes the objective text without reopening
	// pursuit. A completed goal is still reopened. Missing or false is a
	// full set/clear.
	GoalEdit *bool `json:"goal_edit"`
	// GoalResume starts the next turn for an open objective (blocked,
	// capped, or idle). Complete or missing goals are rejected.
	GoalResume   *bool   `json:"goal_resume"`
	PlanMode     *bool   `json:"plan_mode"`
	PlanMarkdown *string `json:"plan_markdown"`
	Archived     *bool   `json:"archived"`
	Pinned       *bool   `json:"pinned"`
	// ProjectID moves a conversation into a project or, when empty, out of
	// every project. Its files stay where they are.
	ProjectID *string `json:"project_id"`
}

func (s *Server) patchThread(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	var req patchThreadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	if req.Title != nil {
		if strings.TrimSpace(*req.Title) == "" {
			badRequest(c, "a conversation title cannot be empty")
			return
		}
		if err := s.engine.RenameThread(th.ID, *req.Title); err != nil {
			s.fail(c, err)
			return
		}
	}
	if req.ProviderID != nil || req.Model != nil {
		providerID := th.ProviderID
		if req.ProviderID != nil {
			providerID = *req.ProviderID
		}
		model := th.Model
		if req.Model != nil {
			model = *req.Model
		}
		if err := s.engine.SetThreadProvider(th.ID, providerID, model); err != nil {
			s.fail(c, err)
			return
		}
	}
	if req.ReasoningEffort != nil {
		if err := s.engine.SetThreadReasoning(th.ID, *req.ReasoningEffort); err != nil {
			s.fail(c, err)
			return
		}
	}
	if req.Goal != nil {
		var err error
		if req.GoalEdit != nil && *req.GoalEdit {
			err = s.engine.EditThreadGoal(th.ID, *req.Goal)
		} else {
			err = s.engine.SetThreadGoal(th.ID, *req.Goal)
		}
		if err != nil {
			s.fail(c, err)
			return
		}
	}
	if req.GoalResume != nil && *req.GoalResume {
		if _, err := s.engine.ResumeThreadGoal(th.ID); err != nil {
			s.fail(c, err)
			return
		}
	}
	if req.PlanMode != nil {
		if err := s.engine.SetPlanMode(th.ID, *req.PlanMode); err != nil {
			s.fail(c, err)
			return
		}
	}
	if req.PlanMarkdown != nil {
		if err := s.engine.SavePlanMarkdown(th.ID, *req.PlanMarkdown); err != nil {
			s.fail(c, err)
			return
		}
	}
	if req.Archived != nil {
		if err := s.engine.SetThreadArchived(th.ID, *req.Archived); err != nil {
			s.fail(c, err)
			return
		}
	}
	if req.Pinned != nil {
		if err := s.engine.SetThreadPinned(th.ID, *req.Pinned); err != nil {
			s.fail(c, err)
			return
		}
	}
	if req.ProjectID != nil {
		if err := s.engine.MoveThread(th.ID, *req.ProjectID); err != nil {
			s.fail(c, err)
			return
		}
	}
	s.getThread(c)
}

func (s *Server) deleteThread(c *gin.Context) {
	if _, ok := s.thread(c); !ok {
		return
	}
	if err := s.engine.DeleteThread(c.Param("id")); err != nil {
		s.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// ---------- turns ----------

type turnRequest struct {
	Text         string         `json:"text"`
	Images       []rawImageJSON `json:"images"`
	Files        []string       `json:"files"`
	FromEventSeq int64          `json:"from_event_seq"`
}

type rawImageJSON struct {
	Name string `json:"name"`
	MIME string `json:"mime"`
	Data string `json:"data"`
}

func (req turnRequest) input() (engine.UserInput, error) {
	images, err := engine.DecodeImages(rawImages(req.Images))
	if err != nil {
		return engine.UserInput{}, err
	}
	return engine.UserInput{Text: req.Text, Images: images, Files: req.Files}, nil
}

func rawImages(in []rawImageJSON) []engine.RawImage {
	out := make([]engine.RawImage, 0, len(in))
	for _, img := range in {
		out = append(out, engine.RawImage{Name: img.Name, MIME: img.MIME, Data: img.Data})
	}
	return out
}

func (s *Server) startTurn(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	var req turnRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	in, err := req.input()
	if err != nil {
		s.fail(c, err)
		return
	}
	// Steer must not rewind: ordinary Enter while idle falls through to
	// StartTurn without this field. Only this endpoint is "edit and resend".
	in.FromEventSeq = req.FromEventSeq
	turn, err := s.engine.StartTurnInput(th.ID, in)
	if err != nil {
		s.fail(c, err)
		return
	}
	// 202: the turn is accepted and running. Its answer arrives on the event
	// stream, because a turn routinely outlives the request that started it.
	c.JSON(http.StatusAccepted, gin.H{"turn": turn})
}

// steer delivers guidance to a running turn. When nothing is running it starts
// a turn instead: from the user's side both are "I pressed Enter", and making
// the browser decide would race with the turn finishing.
func (s *Server) steer(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	var req turnRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	in, err := req.input()
	if err != nil {
		s.fail(c, err)
		return
	}
	err = s.engine.SteerInput(th.ID, in)
	if err == nil {
		c.JSON(http.StatusAccepted, gin.H{"steered": true})
		return
	}
	if !isIdle(err) {
		s.fail(c, err)
		return
	}
	turn, startErr := s.engine.StartTurnInput(th.ID, in)
	if startErr != nil {
		s.fail(c, startErr)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"steered": false, "turn": turn})
}

func isIdle(err error) bool {
	return err != nil && errors.Is(err, engine.ErrIdle)
}

func (s *Server) interrupt(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	if err := s.engine.Interrupt(th.ID); err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"interrupted": true})
}

type continueRequest struct {
	Continue *bool `json:"continue"`
}

func (s *Server) continueTurn(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	var req continueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	if req.Continue == nil {
		badRequest(c, "continue must be true or false")
		return
	}
	if err := s.engine.ContinueTurn(th.ID, *req.Continue); err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusAccepted, gin.H{"continued": *req.Continue})
}

func (s *Server) listTurns(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	turns, err := s.engine.Store().ListTurns(th.ID)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"turns": turns})
}

func (s *Server) compactThread(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	th, err := s.engine.CompactThread(th.ID)
	if err != nil {
		s.fail(c, err)
		return
	}
	status := s.engine.Status(th.ID)
	chars, budget, err := s.engine.ContextUsage(th.ID)
	if err != nil {
		s.fail(c, err)
		return
	}
	usage, err := s.engine.Usage(th.ID, status.TurnID)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"thread": viewThreadWithUsage(th, status.Running, status.AwaitingAnswer, status.Waiting, chars, budget),
		"status": status,
		"usage":  usage,
	})
}
