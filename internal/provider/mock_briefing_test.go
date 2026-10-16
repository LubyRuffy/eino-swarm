package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestMockSessionBriefingKeepsNewWorkAfterEvidencePreamble(t *testing.T) {
	out := compactSummarizerScript(0, []*schema.Message{schema.UserMessage(
		strings.Repeat("evidence preamble ", 100) + "\nNew events:\nmanager: current work marker " + strings.Repeat("next work ", 20))})
	if !strings.Contains(out.Content, "current work marker") {
		t.Fatalf("evidence framing replaced the work in the offline briefing: %q", out.Content)
	}
}

func TestMockReviewerRecordsTheHumanTaskAfterLifecycleEvidence(t *testing.T) {
	for _, request := range []string{"collect current inputs", "inspect the next report"} {
		input := "<zwai-worker-lifecycle-evidence>\nWorker lifecycle evidence\n{\"new_workers\":0}\n</zwai-worker-lifecycle-evidence>\n\nConversation to review:\n\nsession briefing (model-written claims): earlier work\n\nhuman: " + request + "\n\nmanager: finished"
		out := reviewerScript(1, []*schema.Message{schema.UserMessage(input)})
		var note struct {
			Content string `json:"content"`
		}
		if len(out.ToolCalls) == 0 || json.Unmarshal([]byte(out.ToolCalls[0].Function.Arguments), &note) != nil || !strings.Contains(note.Content, request) || strings.Contains(note.Content, "lifecycle evidence") {
			t.Fatalf("reviewer learned framing instead of the human task: %+v", out.ToolCalls)
		}
	}
}

func TestMockReviewSubjectKeepsPlainAndToolOnlyInput(t *testing.T) {
	for _, input := range []string{"plain review work", "Conversation to review:\n\nmanager: [called a tool]"} {
		got := mockReviewSubject([]*schema.Message{schema.UserMessage(input)})
		want := strings.TrimPrefix(input, "Conversation to review:\n\n")
		if got != want {
			t.Fatalf("a review without a human row lost its actual work: %q", got)
		}
	}
}

func TestMockLibraryReviewerUsesTheHumanSubject(t *testing.T) {
	const request = "inspect the current inputs"
	msgs := []*schema.Message{schema.UserMessage("Shared skill library:\n\n<zwai-worker-lifecycle-evidence>\nmetadata\n</zwai-worker-lifecycle-evidence>\n\nConversation to review:\n\nhuman: " + request)}
	out := reviewerScript(1, msgs)
	var skill struct {
		Name string `json:"name"`
	}
	if len(out.ToolCalls) != 1 || out.ToolCalls[0].Function.Name != "skill_manage" || json.Unmarshal([]byte(out.ToolCalls[0].Function.Arguments), &skill) != nil || skill.Name != "recorded-"+conversationTag(request) {
		t.Fatalf("library review learned the envelope rather than the task: %+v", out.ToolCalls)
	}
	if finish := reviewerScript(2, msgs); len(finish.ToolCalls) != 0 || !strings.Contains(finish.Content, "Stored one skill") {
		t.Fatalf("library review did not finish after the write: %+v", finish)
	}
}
