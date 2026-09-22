package engine

import (
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// FoldProjectSkills is the Memory panel's tidy. Stem families still collapse
// without a model — that is the cheap hygiene after a hand edit — and then
// the reviewer reads the live catalog and merge/patch/deletes by content.
// A click that only scanned filenames was a shrug; this one costs a model
// call on purpose. Model calls hang on the project's latest finished turn
// when there is one, so `zwai trace <turn>` still reaches them.
func (e *Engine) FoldProjectSkills(projectID string) (memory.FoldReport, error) {
	var zero memory.FoldReport
	if _, err := e.store.GetProject(projectID); err != nil {
		return zero, err
	}
	gate := e.reviews.begin(projectID)
	if gate == nil {
		return zero, ErrIdle
	}
	defer e.reviews.done()
	gate <- struct{}{}
	defer func() { <-gate }()

	mem := e.ProjectMemory(projectID)
	before, err := mem.ListSkills()
	if err != nil {
		return zero, err
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

	threadID, turnID, providerID, model, effort := e.tidyReviewAnchor(projectID)
	var mu sync.Mutex
	outcome := reviewOutcome{Notes: map[string]int{}}
	reviewed := false
	if len(remaining) > 0 {
		onChange := func(c memory.Change) {
			mu.Lock()
			defer mu.Unlock()
			collectReviewChange(&outcome, c)
		}
		userMsg, msgErr := catalogTidyUserMessage(mem)
		if msgErr != nil {
			return zero, msgErr
		}
		e.driveReviewer(reviewRun{
			threadID:    threadID,
			turnID:      turnID,
			providerID:  providerID,
			model:       model,
			effort:      effort,
			description: "curates this project's recorded skills",
			instruction: memory.CatalogTidyPrompt(),
			userMsg:     userMsg,
			maxIter:     e.cfg.Memory.TidyIterations(),
			tools:       memory.CatalogTools(mem, onChange),
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

func catalogTidyUserMessage(mem *memory.Store) (string, error) {
	skills, err := mem.ListSkills()
	if err != nil {
		return "", err
	}
	families, err := mem.SkillFamilyNames()
	if err != nil {
		return "", err
	}
	return memory.CatalogTidyMessage(memory.ReviewCatalog(skills, families)), nil
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
