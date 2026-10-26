package engine

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

const messageSourceKey = "zwai_message_source"

func withMessageSource(message *schema.Message, source string) *schema.Message {
	if message == nil || source == "" {
		return message
	}
	if message.Extra == nil {
		message.Extra = map[string]any{}
	}
	message.Extra[messageSourceKey] = source
	return message
}

func runtimeUserMessage(text string) *schema.Message {
	return withMessageSource(schema.UserMessage(text), store.MessageSourceRuntime)
}

func messageSource(message *schema.Message) string {
	if message == nil {
		return ""
	}
	source, _ := message.Extra[messageSourceKey].(string)
	return source
}

func isRuntimeMessage(message *schema.Message) bool {
	return messageSource(message) == store.MessageSourceRuntime
}

func inputMessageSource(in UserInput) string {
	switch {
	case in.ContinueGoal:
		return store.MessageSourceRuntime
	case in.ContinueSchedule:
		return "schedule"
	case in.ImplementPlan:
		return "plan"
	default:
		return store.MessageSourceHuman
	}
}

// Legacy matching is deliberately exact. A quoted prefix or similar prose is
// not evidence that a human message came from the runtime.
func legacyRuntimeCue(text string) bool {
	switch text {
	case GoalContinueText(), workerCollectionCorrectionCue, modelRetryCue,
		resumeCue, resumeWorkersCue, parkedWorkersCue, scheduleCompletionCue,
		scheduleAnswerCorrectionCue, goalSessionWrapSteer(), "[steer] " + goalSessionWrapSteer():
		return true
	}
	if suffix, ok := strings.CutPrefix(text, workerCollectionCorrectionCue+"\nUncollected worker IDs: "); ok {
		var ids []string
		return json.Unmarshal([]byte(suffix), &ids) == nil && workerCollectionCue(ids) == text
	}
	lead := formatInterruptedWorkersCue([]stoppedWorker{{ID: ""}})
	if suffix, ok := strings.CutPrefix(text, lead); ok && suffix != "" {
		var workers []stoppedWorker
		for _, id := range strings.Split(suffix, "\n- ") {
			if id == "" || strings.ContainsAny(id, "\r\n") {
				return false
			}
			workers = append(workers, stoppedWorker{ID: id})
		}
		return formatInterruptedWorkersCue(workers) == text
	}
	return false
}

// Unmarked rows can be from an older runtime or an actual human quoting that
// runtime. Durable human input wins; source-marked rows need no text matching.
func (e *Engine) withoutRuntimeRows(rows []store.Message) ([]store.Message, error) {
	out := make([]store.Message, 0, len(rows))
	for _, row := range rows {
		if row.Source == store.MessageSourceRuntime {
			continue
		}
		if row.Role == string(schema.User) && row.Source == "" && legacyRuntimeCue(row.Content) {
			human, err := e.store.MessageHasHumanSource(row)
			if err != nil {
				return nil, fmt.Errorf("engine: classify legacy transcript source: %w", err)
			}
			if !human {
				continue
			}
			row.Source = store.MessageSourceHuman
		}
		out = append(out, row)
	}
	return out, nil
}
