package provider

import (
	"sync/atomic"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
)

type firstRequestReasoning struct {
	level string
	sent  atomic.Bool
}

// WithFirstRequestReasoningEffort derives a model for a recovery run. The
// original model, admission gate, recorder and completion cap stay bound to the
// same turn. An explicit conversation effort still applies to every request.
func WithFirstRequestReasoningEffort(base model.BaseChatModel, effort string) model.BaseChatModel {
	r, ok := base.(*recordingModel)
	effort = config.NormalizeReasoning(effort)
	if !ok || r.reasoning != "" || effort == "" {
		return base
	}
	copy := *r
	copy.firstReasoning = &firstRequestReasoning{level: effort}
	return &copy
}

// Consume the override after admission so a cancelled wait does not spend it.
// A routing probe or later tool round gets the original request options.
func (r *recordingModel) firstRequestOptions(opts []model.Option) []model.Option {
	first := r.firstReasoning
	if first == nil || !first.sent.CompareAndSwap(false, true) {
		return opts
	}
	return append([]model.Option{r.reasoningOption(first.level)}, opts...)
}
