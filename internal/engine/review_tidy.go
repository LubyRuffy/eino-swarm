package engine

import (
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// TidyEvent is one beat of a catalog tidy, pushed while the model is still
// writing. The panel streams `text` (the prose so far) and names a skill
// write as it lands. The finished report is not one of these.
type TidyEvent struct {
	Phase   string `json:"phase"`
	Scanned int    `json:"scanned,omitempty"`
	Text    string `json:"text,omitempty"`
	Action  string `json:"action,omitempty"`
	Name    string `json:"name,omitempty"`
}

// FoldProjectSkills is the Memory panel's tidy. Stem families still collapse
// without a model — that is the cheap hygiene after a hand edit — and then
// the reviewer reads the live catalog and merge/patch/deletes by content.
// A click that only scanned filenames was a shrug; this one costs a model
// call on purpose. Model calls hang on the project's latest finished turn
// when there is one, so `zwai trace <turn>` still reaches them.
func (e *Engine) FoldProjectSkills(projectID string) (memory.FoldReport, error) {
	return e.foldProjectSkills(projectID, nil)
}

// FoldProjectSkillsWatch is the same tidy, with a live beat for each scan,
// streamed sentence, and skill write. watch may be nil. It runs on the
// tidy goroutine and must not block on the caller's locks.
func (e *Engine) FoldProjectSkillsWatch(projectID string, watch func(TidyEvent)) (memory.FoldReport, error) {
	return e.foldProjectSkills(projectID, watch)
}

// FoldLibrarySkills curates the shared skill library the same way a project's
// tidy curates its catalog. The gate is the library, not a project row.
func (e *Engine) FoldLibrarySkills() (memory.FoldReport, error) {
	return e.FoldLibrarySkillsWatch(nil)
}

// FoldLibrarySkillsWatch is FoldLibrarySkills with a live beat for the panel.
func (e *Engine) FoldLibrarySkillsWatch(watch func(TidyEvent)) (memory.FoldReport, error) {
	return e.foldCatalog(memory.LibraryID, e.LibraryMemory(), e.libraryReviewAnchor, watch,
		memory.LibraryTidyPrompt(), "curates the shared skill library")
}

func (e *Engine) foldProjectSkills(projectID string, watch func(TidyEvent)) (memory.FoldReport, error) {
	var zero memory.FoldReport
	if _, err := e.store.GetProject(projectID); err != nil {
		return zero, err
	}
	return e.foldCatalog(projectID, e.ProjectMemory(projectID), func() (string, string, string, string, string) {
		return e.tidyReviewAnchor(projectID)
	}, watch, memory.CatalogTidyPrompt(), "curates this project's recorded skills")
}

func (e *Engine) foldCatalog(gateKey string, mem *memory.Store, anchor func() (string, string, string, string, string), watch func(TidyEvent), instruction, description string) (memory.FoldReport, error) {
	var zero memory.FoldReport
	gate := e.reviews.begin(gateKey)
	if gate == nil {
		return zero, ErrIdle
	}
	defer e.reviews.done()
	gate <- struct{}{}
	defer func() { <-gate }()

	before, err := mem.ListSkills()
	if err != nil {
		return zero, err
	}
	if watch != nil {
		watch(TidyEvent{Phase: "scan", Scanned: len(before)})
	}
	families, err := mem.SkillFamilyNames()
	if err != nil {
		return zero, err
	}

	var changes []memory.Change
	folded, err := mem.FoldSkillFamilies()
	if err != nil {
		return zero, err
	}
	changes = append(changes, folded...)

	remaining, err := mem.ListSkills()
	if err != nil {
		return zero, err
	}

	threadID, turnID, providerID, model, effort := anchor()
	var mu sync.Mutex
	outcome := reviewOutcome{Notes: map[string]int{}}
	reviewed := false
	if len(remaining) > 0 {
		onChange := func(c memory.Change) {
			mu.Lock()
			collectReviewChange(&outcome, c)
			mu.Unlock()
			if watch != nil && c.Target == memory.ToolSkillManage {
				watch(TidyEvent{Phase: "change", Action: c.Action, Name: c.Name})
			}
		}
		userMsg, msgErr := catalogTidyUserMessage(mem, gateKey == memory.LibraryID)
		if msgErr != nil {
			return zero, msgErr
		}
		e.driveReviewer(reviewRun{
			threadID:    threadID,
			turnID:      turnID,
			providerID:  providerID,
			model:       model,
			effort:      effort,
			description: description,
			instruction: instruction,
			userMsg:     userMsg,
			maxIter:     e.cfg.Memory.TidyIterations(),
			tools:       memory.CatalogTools(mem, onChange),
			onText: func(text string) {
				if watch == nil || strings.TrimSpace(text) == "" {
					return
				}
				watch(TidyEvent{Phase: "text", Text: text})
			},
		}, &mu, &outcome)
		reviewed = true
		mu.Lock()
		changes = append(changes, outcome.Changes...)
		llmErr := outcome.Err
		mu.Unlock()
		leftover, foldErr := mem.FoldSkillFamilies()
		if foldErr != nil && llmErr == "" {
			return zero, foldErr
		}
		changes = append(changes, leftover...)
		if foldErr != nil {
			mu.Lock()
			if outcome.Err == "" {
				outcome.Err = foldErr.Error()
			}
			mu.Unlock()
		}
	}

	after, err := mem.ListSkills()
	if err != nil {
		return zero, err
	}
	report := memory.ComposeTidyReport(skillInfoNames(before), skillInfoNames(after), len(families), changes)
	report.Reviewed = reviewed
	mu.Lock()
	report.Err = outcome.Err
	note := outcome.Note
	mu.Unlock()

	if turnID != "" && (report.Reviewed || report.Folded() || report.Err != "") {
		recorded := reviewOutcome{
			Changed: len(report.Changes) > 0,
			Skills:  skillChanges(report.Changes),
			Changes: report.Changes,
			Note:    note,
			Err:     report.Err,
			Notify:  config.MemoryNotifyOff,
			Library: gateKey == memory.LibraryID,
		}
		if recorded.Note == "" && recorded.Changed {
			recorded.Note = "Folded overlapping skills."
		}
		e.recordReview(threadID, turnID, recorded)
	}
	return report, nil
}

func skillInfoNames(list []memory.SkillInfo) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = s.Name
	}
	return out
}

func skillChanges(changes []memory.Change) []memory.Change {
	var out []memory.Change
	for _, c := range changes {
		if c.Target == memory.ToolSkillManage {
			out = append(out, c)
		}
	}
	return out
}

func catalogTidyUserMessage(mem *memory.Store, library bool) (string, error) {
	skills, err := mem.ListSkills()
	if err != nil {
		return "", err
	}
	families, err := mem.SkillFamilyNames()
	if err != nil {
		return "", err
	}
	catalog := memory.ReviewCatalog(skills, families)
	if library {
		catalog = memory.LibraryCatalog(skills, families)
	}
	return memory.CatalogTidyMessage(catalog), nil
}

// tidyReviewAnchor is where catalog-tidy model calls are attributed. The
// project's latest finished turn is the troubleshooting handle — sidebar
// order is not recency, and a pinned or just-opened older conversation
// must not steal the trace. Without a finished turn the most recently
// active conversation still supplies the provider; the calls have no
// turn id to hang on.
func (e *Engine) tidyReviewAnchor(projectID string) (threadID, turnID, providerID, model, effort string) {
	threads, err := e.store.ListThreads(true, projectID)
	if err != nil {
		return "", "", e.cfg.Models.Default, "", ""
	}
	return e.pickReviewAnchor(threads)
}

// libraryReviewAnchor hangs a library tidy on the newest finished turn that
// belongs to no project, so zwai trace still reaches the model calls.
func (e *Engine) libraryReviewAnchor() (threadID, turnID, providerID, model, effort string) {
	threads, err := e.store.ListThreads(true, "")
	if err != nil {
		return "", "", e.cfg.Models.Default, "", ""
	}
	loose := make([]store.Thread, 0, len(threads))
	for _, th := range threads {
		if strings.TrimSpace(th.ProjectID) == "" {
			loose = append(loose, th)
		}
	}
	return e.pickReviewAnchor(loose)
}

func (e *Engine) pickReviewAnchor(threads []store.Thread) (threadID, turnID, providerID, model, effort string) {
	var best *store.Turn
	var bestThread store.Thread
	var fallback *store.Thread
	for _, th := range threads {
		if th.ProviderID != "" && (fallback == nil || th.LastActiveAt.After(fallback.LastActiveAt)) {
			thread := th
			fallback = &thread
		}
		turns, turnErr := e.store.ListTurns(th.ID)
		if turnErr != nil {
			continue
		}
		for i := range turns {
			t := turns[i]
			if t.Status != store.TurnDone {
				continue
			}
			if best == nil || turnFinishedAfter(t, *best) {
				done := t
				best = &done
				bestThread = th
			}
		}
	}
	if best != nil {
		return bestThread.ID, best.ID, best.ProviderID, best.Model, best.ReasoningEffort
	}
	if fallback != nil {
		return fallback.ID, "", fallback.ProviderID, fallback.Model, ""
	}
	return "", "", e.cfg.Models.Default, "", ""
}

func turnFinishedAfter(a, b store.Turn) bool {
	at, bt := turnFinishedAt(a), turnFinishedAt(b)
	if !at.Equal(bt) {
		return at.After(bt)
	}
	return a.StartedAt.After(b.StartedAt)
}

func turnFinishedAt(t store.Turn) time.Time {
	if t.EndedAt != nil && !t.EndedAt.IsZero() {
		return *t.EndedAt
	}
	return t.StartedAt
}
