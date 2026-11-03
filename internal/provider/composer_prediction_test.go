package provider

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestComposerPredictionMockIsNotAWorker(t *testing.T) {
	out, err := newMockModel("composer-prediction").Generate(context.Background(), []*schema.Message{
		schema.UserMessage("hi"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || out.Content != MockComposerPrediction || len(out.ToolCalls) != 0 {
		t.Fatalf("suggestion script = %+v", out)
	}
}
