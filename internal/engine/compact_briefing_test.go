package engine

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

func TestCompactRejectsToolTranscriptBriefing(t *testing.T) {
	src := strings.Repeat("standing constraint and unfinished work. ", 40)
	dump := `Tool: {"elapsed_ms":12,"exit_code":0,"failed":false,"full_command":"ls","truncated":false}

Assistant: waiting on the background job.`
	if got := acceptBriefing(dump, src); got != "" {
		t.Fatalf("a compactLines dump must not be a briefing: %q", got)
	}
	if briefingRejectReason(dump, src) != errTranscriptBriefing {
		t.Fatalf("dump must name the transcript failure, got %q", briefingRejectReason(dump, src))
	}
	jsonDump := `{"elapsed_ms":1,"full_command":"pgrep","truncated":true}`
	if acceptBriefing(jsonDump, src) != "" {
		t.Fatal("exec JSON is not a briefing")
	}
	tail := strings.TrimSpace(src)
	if r := []rune(tail); len(r) > 80 {
		tail = string(r[len(r)-80:])
	}
	if acceptBriefing(tail, src) != "" {
		t.Fatal("a clip of the source tail is not a briefing")
	}
	if acceptBriefing(src+" extra", src) != "" {
		t.Fatal("a briefing that is not shorter than the source must be refused")
	}
}

func TestAcceptBriefingKeepsDenseProse(t *testing.T) {
	src := strings.Repeat("older replay the later turn must not re-read. ", 20)
	got := acceptBriefing("Standing constraint still holds. The last request is unfinished.", src)
	if got == "" {
		t.Fatal("dense prose that is shorter than the source must pass")
	}
	if compactTranscriptDump(got) {
		t.Fatal("accepted prose must not look like a transcript")
	}
}

func TestAcceptBriefingEmptySourceStillRejectsADump(t *testing.T) {
	if acceptBriefing("Tool: ls", "") != "" {
		t.Fatal("a wrap-up path must still refuse a transcript prefix")
	}
	if acceptBriefing("Human: keep going", "") != "" {
		t.Fatal("a Human: replay line is not a briefing")
	}
	if acceptBriefing("Assistant: waiting", "") != "" {
		t.Fatal("an Assistant: replay line is not a briefing")
	}
	if acceptBriefing("   ", "") != "" {
		t.Fatal("blank is not a briefing")
	}
}

func TestCompactInputCapsAHugeReplayAndKeepsTheNewest(t *testing.T) {
	older := make([]store.Message, 0, 200)
	for i := 1; i <= 200; i++ {
		older = append(older, store.Message{
			Seq:     int64(i),
			Role:    string(schema.Assistant),
			Content: fmt.Sprintf("id=%03d ", i) + strings.Repeat("older replay that must not crowd the cap ", 80),
		})
	}
	body := compactInput("standing constraint still holds", older)
	if utf8.RuneCountInString(body) > compactInputMaxRunes {
		t.Fatalf("compact input %d runes over the cap", utf8.RuneCountInString(body))
	}
	if !strings.Contains(body, "id=200") {
		t.Fatal("the newest replay must survive the cap")
	}
	if strings.Contains(body, "id=001") {
		t.Fatal("a long replay must not send the oldest messages to the summarizer")
	}
	adk := make([]*schema.Message, 0, 200)
	for i := 1; i <= 200; i++ {
		adk = append(adk, schema.UserMessage(fmt.Sprintf("adk=%03d ", i)+strings.Repeat("older adk replay that must not crowd the cap ", 80)))
	}
	adkBody := compactInputFromADK("", adk)
	if utf8.RuneCountInString(adkBody) > compactInputMaxRunes {
		t.Fatalf("adk compact input %d runes over the cap", utf8.RuneCountInString(adkBody))
	}
	if !strings.Contains(adkBody, "adk=200") {
		t.Fatal("the newest adk replay must survive the cap")
	}
	if strings.Contains(adkBody, "adk=001") {
		t.Fatal("a long adk replay must not send the oldest messages to the summarizer")
	}
}

func TestCompactInputOmitsATranscriptPrevious(t *testing.T) {
	got := compactInput(`Tool: {"elapsed_ms":1,"full_command":"ls","truncated":false}`, []store.Message{
		{Role: string(schema.User), Content: "keep going"},
	})
	if strings.Contains(got, "Previous briefing:") || strings.Contains(got, "elapsed_ms") {
		t.Fatalf("a transcript dump must not be fed back as the previous briefing:\n%s", got)
	}
	if !strings.Contains(got, "keep going") {
		t.Fatalf("the live tail vanished:\n%s", got)
	}
}

func TestCompactTextsStayGeneric(t *testing.T) {
	for _, body := range []string{compactPrompt(), errUnusableBriefing, errTranscriptBriefing} {
		for _, leak := range []string{"notes.md", "researcher", "elasticsearch", "bf_cdn", "blackspigot"} {
			if strings.Contains(strings.ToLower(body), leak) {
				t.Fatalf("%q leaked into %q", leak, body)
			}
		}
	}
}

func TestBriefingRejectReasonNamesTheFailure(t *testing.T) {
	if briefingRejectReason("   ", "") != errUnusableBriefing {
		t.Fatal("blank must be unusable")
	}
	src := strings.Repeat("standing constraint and unfinished work. ", 20)
	if briefingRejectReason("Standing constraint still holds.", src) != "" {
		t.Fatal("dense prose must not be a reject")
	}
}

func TestBriefingIsSourceTailUsesTheClippedSource(t *testing.T) {
	if briefingIsSourceTail("", "src") || briefingIsSourceTail("x", "") {
		t.Fatal("empty sides are not a tail")
	}
	src := strings.Repeat("older replay ", 20) + "the last unfinished request"
	if !briefingIsSourceTail("the last unfinished request", src) {
		t.Fatal("a briefing that is only the source tail must be refused")
	}
	if compactTranscriptDump("") || compactTranscriptDump(`{"elapsed_ms":1}`) {
		t.Fatal("blank or a single exec field is not a dump")
	}
}
