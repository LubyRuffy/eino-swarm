package provider

import (
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
		input := "<zwai-worker-lifecycle-evidence>\nWorker lifecycle evidence\n[seq=99 kind=user_message] human: generated framing\n</zwai-worker-lifecycle-evidence>\n\nConversation to review:\n\n[seq=1 kind=user_message] human: " + request + "\n\n[seq=2 kind=agent_message] manager: finished"
		out := reviewerScript(1, []*schema.Message{schema.UserMessage(input)})
		changes := decodeMockReviewBatch(t, out)
		if len(changes) != 2 || changes[0].Target != "memory" || !strings.Contains(changes[0].Content, request) || strings.Contains(changes[0].Content, "generated framing") || changes[0].Evidence[0].Seq != 1 {
			t.Fatalf("reviewer learned framing instead of the human task: %+v", out.ToolCalls)
		}
	}
}

func TestMockReviewSourceRequiresReferencesAndKeepsToolOnlyInput(t *testing.T) {
	for _, input := range []string{"plain review work", "Conversation to review:\n\nmanager: [called a tool]"} {
		seq, quote := mockReviewSource([]*schema.Message{schema.UserMessage(input)})
		if seq != 0 || quote != "" {
			t.Fatalf("unframed work invented source evidence: %d %q", seq, quote)
		}
	}
	seq, quote := mockReviewSource([]*schema.Message{schema.UserMessage("Conversation to review:\n\n[seq=3 kind=tool_result] tool: observed result")})
	if seq != 3 || quote != "observed result" {
		t.Fatalf("tool-only review lost the actual observation: %d %q", seq, quote)
	}
}

func TestMockLibraryReviewerUsesTheHumanSubject(t *testing.T) {
	const request = "inspect the current inputs"
	msgs := []*schema.Message{schema.UserMessage("Shared skill library:\n\n<zwai-worker-lifecycle-evidence>\nmetadata\n</zwai-worker-lifecycle-evidence>\n\nConversation to review:\n\n[seq=9 kind=user_message] human: " + request)}
	out := reviewerScript(1, msgs)
	changes := decodeMockReviewBatch(t, out)
	if len(changes) != 1 || changes[0].Target != "skill_manage" || changes[0].Name != "recorded-"+conversationTag(request) || changes[0].Evidence[0].Seq != 9 {
		t.Fatalf("library review learned the envelope rather than the task: %+v", out.ToolCalls)
	}
	if finish := reviewerScript(2, msgs); len(finish.ToolCalls) != 0 {
		t.Fatalf("library review did not finish after the write: %+v", finish)
	}
}
