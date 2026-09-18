package provider

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestWaitReportParsingReportsProgressAndResults(t *testing.T) {
	// a wait that came back with one worker done and one still running
	partial := []*schema.Message{
		schema.ToolMessage(`{"agents":[{"agent_id":"a-1","status":"done","result":"ok"},{"agent_id":"b-2","status":"running","activity":"reading"}],"timed_out":false}`, "x"),
	}
	r := latestWaitReport(partial)
	if r == nil {
		t.Fatal("latestWaitReport found nothing in a real wait result")
	}
	if allFinished(r) {
		t.Fatal("a report with a running agent must not read as all finished")
	}
	if got := waitProgressLine(r); !strings.Contains(got, "1 finished") || !strings.Contains(got, "1 still working") {
		t.Fatalf("waitProgressLine=%q", got)
	}

	// a wait where everyone is done, one of them failed
	done := []*schema.Message{
		schema.ToolMessage(`{"agents":[{"agent_id":"a-1","status":"done","result":"ok"},{"agent_id":"b-2","status":"failed","error":"timed out"}],"timed_out":false}`, "y"),
	}
	r = latestWaitReport(done)
	if r == nil || !allFinished(r) {
		t.Fatalf("a report with no running agents should read as finished: %+v", r)
	}
	got := collectResults(r)
	if len(got) != 2 || got[0] != "ok" || !strings.Contains(got[1], "timed out") {
		t.Fatalf("collectResults=%+v", got)
	}

	// no wait has happened yet
	if latestWaitReport([]*schema.Message{schema.UserMessage("hi")}) != nil {
		t.Fatal("latestWaitReport should be nil before the manager waits")
	}
}
