package engine

import (
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// callRecorder persists one row per model call so `zwai trace <turn>` can
// explain where a turn's time went.
func (e *Engine) callRecorder(threadID, turnID string) provider.Recorder {
	return func(r provider.CallRecord) {
		if r.Backoff != nil {
			e.recordModelBackoff(threadID, turnID, r)
			return
		}
		rec := &store.LLMCall{
			ThreadID: threadID, TurnID: turnID, AgentID: r.AgentID,
			ProviderID: r.ProviderID, Model: r.Model,
			InputMsgs: r.InputMsgs, InputChars: r.InputChars, OutputChars: r.OutputChars,
			PromptTokens: r.PromptTokens, CompletionTokens: r.CompletionTokens,
			TotalTokens: r.TotalTokens, CachedTokens: r.CachedTokens,
			ReasoningTokens: r.ReasoningTokens,
			DurationMS:      r.Duration.Milliseconds(),
		}
		if r.Err != nil {
			rec.Err = r.Err.Error()
		}
		if err := e.store.AppendLLMCall(rec); err != nil {
			e.logPersistFailure("could not record a model call", err, "turn", turnID)
			return
		}
		e.recordBuiltinTools(rec, r.BuiltinTools)
		if isAuxiliaryAgent(r.AgentID) {
			return
		}
		e.emitUsage(threadID, turnID)
	}
}
