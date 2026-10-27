package provider

import (
	"context"
	"net/http"

	"github.com/cloudwego/eino/components/model"
)

// AuxiliaryModelBuilder is ModelBuilder for the engine's own background calls
// (policy classifiers, memory review) whose effort is an application default
// rather than the conversation's chosen level. Sending reasoning_effort to an
// endpoint that does not support it is a 400, so such a rejection is asked
// once more without the effort instead of failing the call. The rejected
// attempt is still recorded, so Trace shows what the endpoint refused.
func (p *Pool) AuxiliaryModelBuilder(ctx context.Context, id, modelName, effort string, rec Recorder) (func(role, agentID string) model.BaseChatModel, error) {
	build, err := p.ModelBuilder(ctx, id, modelName, effort, rec)
	if err != nil {
		return nil, err
	}
	return func(role, agentID string) model.BaseChatModel {
		m := build(role, agentID)
		if r, ok := m.(*recordingModel); ok {
			r.optionalEffort = true
		}
		return m
	}, nil
}

func (r *recordingModel) effortRejected(err error) bool {
	return r.optionalEffort && r.reasoning != "" && HTTPStatus(err) == http.StatusBadRequest
}
