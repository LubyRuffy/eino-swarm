package provider

import (
	"context"
	"testing"
)

func TestBuiltinRecordsBelongToOneRequestAndSnapshotsDoNotGrow(t *testing.T) {
	ctx, first := observeBuiltinTools(context.Background(), true)
	otherCtx, other := observeBuiltinTools(context.Background(), true)
	newResponsesObserver(ctx).item(responsesItem{ID: "cloud-a", Type: "web_search_call", Status: "in_progress"})
	snapshot := first.snapshot()
	newResponsesObserver(ctx).item(responsesItem{ID: "cloud-a", Type: "web_search_call", Status: "completed"})
	newResponsesObserver(otherCtx).item(responsesItem{ID: "cloud-b", Type: "file_search_call", Status: "completed"})
	if len(snapshot) != 1 || len(first.snapshot()) != 2 || len(other.snapshot()) != 1 || other.snapshot()[0].ID != "cloud-b" {
		t.Fatalf("cloud receipts mixed across requests: %v %v %v", snapshot, first.snapshot(), other.snapshot())
	}
	unchanged, disabled := observeBuiltinTools(ctx, false)
	if unchanged != ctx || disabled != nil || disabled.snapshot() != nil {
		t.Fatal("a model without telemetry must not add a collector")
	}
}
