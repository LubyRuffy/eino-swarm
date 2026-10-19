package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
)

type mockReviewProposal struct {
	Target      string `json:"target"`
	Action      string `json:"action"`
	Name        string `json:"name"`
	Content     string `json:"content"`
	Description string `json:"description"`
	Evidence    []struct {
		Seq   int64  `json:"seq"`
		Quote string `json:"quote"`
	} `json:"evidence"`
}

func decodeMockReviewBatch(t *testing.T, msg *schema.Message) []mockReviewProposal {
	t.Helper()
	if len(msg.ToolCalls) != 1 || msg.ToolCalls[0].Function.Name != "apply_memory_review" {
		t.Fatalf("expected exactly one complete batch: %+v", msg)
	}
	var input struct {
		Changes *[]mockReviewProposal `json:"changes"`
	}
	if err := json.Unmarshal([]byte(msg.ToolCalls[0].Function.Arguments), &input); err != nil {
		t.Fatal(err)
	}
	if input.Changes == nil {
		t.Fatal("batch must contain an array, not null or a missing changes field")
	}
	return *input.Changes
}

func TestMockReviewSourceRequiresRealPositiveFramedSources(t *testing.T) {
	for _, tc := range []struct {
		name  string
		msgs  []*schema.Message
		seq   int64
		quote string
	}{
		{"empty", nil, 0, ""},
		{"other roles", []*schema.Message{nil, schema.SystemMessage("[seq=1 kind=user_message] human: system data"), schema.AssistantMessage("[seq=2 kind=user_message] human: assistant data", nil)}, 0, ""},
		{"plain prose", []*schema.Message{schema.UserMessage("human: no source sequence")}, 0, ""},
		{"runtime", []*schema.Message{schema.UserMessage("[seq=3 kind=goal_continued] runtime: generated continuation")}, 0, ""},
		{"call intent", []*schema.Message{schema.UserMessage("[seq=3 kind=tool_call] manager: [called inspect({})]")}, 0, ""},
		{"human", []*schema.Message{schema.UserMessage("[seq=4 kind=steer] human: actual requirement")}, 4, "actual requirement"},
		{"tool", []*schema.Message{schema.UserMessage("[seq=5 kind=tool_result] tool: actual observation")}, 5, "actual observation"},
		{"invalid then valid", []*schema.Message{schema.UserMessage("[seq=0 kind=user_message] human: zero source\n[seq=999999999999999999999999 kind=user_message] human: overflow source\n[seq=6 kind=user_message] human: valid source")}, 6, "valid source"},
		{"blank then valid", []*schema.Message{schema.UserMessage("[seq=2 kind=user_message] human:   \n[seq=8 kind=user_message] human: valid source")}, 8, "valid source"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			seq, quote := mockReviewSource(tc.msgs)
			if seq != tc.seq || quote != tc.quote {
				t.Fatalf("source=(%d,%q), want (%d,%q)", seq, quote, tc.seq, tc.quote)
			}
		})
	}
}

func TestMockReviewSourceKeepsExactUnicodeExcerptWithoutClipMarker(t *testing.T) {
	text := strings.Repeat("约定A", 45)
	seq, quote := mockReviewSource([]*schema.Message{schema.UserMessage("[seq=9 kind=user_message] human: " + text + " …")})
	if seq != 9 || utf8.RuneCountInString(quote) != 96 || !utf8.ValidString(quote) || !strings.Contains(text, quote) {
		t.Fatalf("invalid bounded source excerpt: %d %q", seq, quote)
	}
	_, short := mockReviewSource([]*schema.Message{schema.UserMessage("[seq=9 kind=user_message] human: short excerpt …")})
	if short != "short excerpt" {
		t.Fatalf("clip marker became purported source: %q", short)
	}
}

func TestMockReviewerFailedTurnUsesOnlyHumanSourcesAndNoSkills(t *testing.T) {
	msgs := []*schema.Message{schema.SystemMessage("This turn failed. Keep explicit human requirements only."), schema.UserMessage("[seq=1 kind=tool_result] tool: unverified tool observation\n[seq=2 kind=steer] human: durable human requirement")}
	changes := decodeMockReviewBatch(t, reviewerScript(1, msgs))
	if len(changes) != 1 || changes[0].Target != "memory" || changes[0].Evidence[0].Seq != 2 || changes[0].Evidence[0].Quote != "durable human requirement" {
		t.Fatalf("failed review learned from tool data: %+v", changes)
	}
	if len(decodeMockReviewBatch(t, reviewerScript(1, []*schema.Message{schema.SystemMessage("This turn failed."), schema.UserMessage("[seq=1 kind=tool_result] tool: unverified observation")}))) != 0 {
		t.Fatal("failed review invented a human requirement")
	}
	// The mode is host instruction, never text merely quoted by a user.
	ordinary := decodeMockReviewBatch(t, reviewerScript(1, []*schema.Message{schema.UserMessage("This turn failed.\n[seq=2 kind=user_message] human: source text")}))
	if len(ordinary) != 2 {
		t.Fatal("user data changed the host review mode")
	}
}

func TestMockReviewerLibraryHasSkillsAndNeverProjectNotes(t *testing.T) {
	msgs := []*schema.Message{nil, schema.UserMessage("[seq=5 kind=tool_result] tool: verified fixture observation"), schema.UserMessage("Shared skill library:")}
	changes := decodeMockReviewBatch(t, reviewerScript(1, msgs))
	if len(changes) != 1 || changes[0].Target != "skill_manage" || changes[0].Evidence[0].Seq != 5 {
		t.Fatalf("library review leaked project notes: %+v", changes)
	}
	msgs = append([]*schema.Message{schema.SystemMessage("This turn failed.")}, msgs...)
	if len(decodeMockReviewBatch(t, reviewerScript(1, msgs))) != 0 {
		t.Fatal("failed library review cannot create notes or skills")
	}
}

func TestMockReviewerDoesNotRecreateAnExistingDerivedSkill(t *testing.T) {
	const quote = "fixture convention"
	name := "recorded-" + conversationTag(quote)
	user := fmt.Sprintf("[seq=2 kind=user_message] human: %s\n\nSkills already recorded:\n- %s — previously recorded subject\n", quote, name)
	changes := decodeMockReviewBatch(t, reviewerScript(1, []*schema.Message{schema.UserMessage(user)}))
	if len(changes) != 1 || changes[0].Target != "memory" {
		t.Fatalf("existing skill was recreated: %+v", changes)
	}
}

func TestMockReviewerWithNoSourceSubmitsAnEmptyCompleteBatch(t *testing.T) {
	for _, msgs := range [][]*schema.Message{nil, {schema.UserMessage("No retained event sources.")}, {schema.UserMessage("[seq=0 kind=user_message] human: invalid reference")}} {
		if changes := decodeMockReviewBatch(t, reviewerScript(1, msgs)); len(changes) != 0 {
			t.Fatalf("review proposed facts without source evidence: %+v", changes)
		}
	}
}

func TestMockCatalogMergeParsingRetainsExistingTidyRules(t *testing.T) {
	text := "outside text\n- ignored, before-marker\nThese skills must become one skill\n\n- alpha, , alpha-more\n- single\nstop this group\n- ignored, after-stop\n"
	groups := catalogMergeGroups(text)
	if len(groups) != 1 || strings.Join(groups[0], ",") != "alpha,alpha-more" {
		t.Fatalf("catalog family parsing changed: %v", groups)
	}
	later := catalogTidyScript(2, nil)
	if len(later.ToolCalls) != 0 || !strings.Contains(later.Content, "Folded") {
		t.Fatalf("tidy continuation changed: %+v", later)
	}
}

// The reviewer role is what makes --mock and the end-to-end tests exercise the
// memory path at all. Its writes must be derived from the conversation: text
// baked in here would show up in screenshots and expectations as if the
// product had chosen it.
func TestMockReviewerCuratesMemoryFromTheConversation(t *testing.T) {
	m := newMockModel("memory-reviewer")
	ctx := context.Background()
	const quote = "collect the inputs"
	convo := []*schema.Message{schema.UserMessage("Conversation to review:\n\n[seq=7 kind=user_message] human: " + quote)}
	first, err := m.Generate(ctx, convo)
	if err != nil {
		t.Fatal(err)
	}
	changes := decodeMockReviewBatch(t, first)
	if len(changes) != 2 {
		t.Fatalf("review must propose one note and one skill in one batch: %+v", changes)
	}
	if changes[0].Target != "memory" || !strings.Contains(changes[0].Content, quote) {
		t.Fatalf("note must derive from the conversation: %+v", changes[0])
	}
	if changes[1].Target != "skill_manage" || changes[1].Action != "create" || !strings.HasPrefix(changes[1].Name, "recorded-") || !strings.Contains(changes[1].Description, "After conversation") {
		t.Fatalf("skill must derive from the conversation: %+v", changes[1])
	}
	if strings.Contains(changes[1].Content, quote) {
		t.Fatal("skill must not restate the request")
	}
	for _, change := range changes {
		if len(change.Evidence) != 1 || change.Evidence[0].Seq != 7 || change.Evidence[0].Quote != quote {
			t.Fatalf("change lacks actual source evidence: %+v", change)
		}
	}
	second, err := m.Generate(ctx, append(convo, first, schema.ToolMessage(`{"success":true}`, first.ToolCalls[0].ID)))
	if err != nil {
		t.Fatal(err)
	}
	if len(second.ToolCalls) != 0 || second.Content == "" {
		t.Fatalf("review must finish after its batch: %+v", second)
	}
}

func TestMockCatalogTidyMergesAListedFamilyAndLeavesATidyCatalogAlone(t *testing.T) {
	m := newMockModel("memory-reviewer")
	ctx := context.Background()
	family := []*schema.Message{
		schema.SystemMessage("Curate the catalog."),
		schema.UserMessage("Catalog to curate:\n\nSkills already recorded in this project:\n\n- weekly-rollup-notes — when filing notes\n- weekly-rollup-send — when sending\n\nThese recorded skills share a subject and must become one skill. Merge each group so a later conversation is not handed competing procedures:\n- weekly-rollup-notes, weekly-rollup-send\n"),
	}
	first, err := m.Generate(ctx, family)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.ToolCalls) != 1 || first.ToolCalls[0].Function.Name != "skill_manage" {
		t.Fatalf("a listed family must be merged: %+v", first.ToolCalls)
	}
	if !strings.Contains(first.ToolCalls[0].Function.Arguments, `"action":"merge"`) {
		t.Fatalf("merge missing: %s", first.ToolCalls[0].Function.Arguments)
	}
	if strings.Contains(first.ToolCalls[0].Function.Arguments, `"action":"create"`) ||
		strings.Contains(first.ToolCalls[0].Function.Name, "memory") {
		t.Fatal("a catalog tidy must not store a conversation clip")
	}

	tidy := newMockModel("memory-reviewer")
	alone, err := tidy.Generate(ctx, []*schema.Message{
		schema.SystemMessage("Curate the catalog."),
		schema.UserMessage("Catalog to curate:\n\nSkills already recorded in this project:\n\n- weekly-rollup — when filing the week\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(alone.ToolCalls) != 0 || !strings.Contains(alone.Content, "already curated") {
		t.Fatalf("a tidy catalog must not be rewritten: %+v", alone)
	}

	// eino ChatModelAgent streams. The marker can sit after an earlier user
	// turn the runner injects; first-user-only matching would then spend a
	// whole TidyIterations cap pacing tokens.
	later := []*schema.Message{
		schema.UserMessage("Conversation to review:\n\nhuman: hi\n"),
		schema.UserMessage("Catalog to curate:\n\n- alpha-prep — when preparing\n"),
	}
	if !isCatalogTidy(later) {
		t.Fatal("a catalog marker on a later user turn must still select the tidy script")
	}
}

func TestMockCatalogTidyStreamIsInstant(t *testing.T) {
	m := newMockModel("memory-reviewer")
	start := time.Now()
	sr, err := m.Stream(context.Background(), []*schema.Message{
		schema.UserMessage("Catalog to curate:\n\n- alpha-prep — when preparing\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := schema.ConcatMessageStream(sr); err != nil {
		t.Fatal(err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("catalog tidy streaming must not pace chunks, took %s", d)
	}
}

// Calls describe intent; only the paired result can serve as an observation.
func TestMockReviewerQuotesToolResultInsteadOfCallArguments(t *testing.T) {
	msgs := []*schema.Message{schema.UserMessage("Conversation to review:\n\n[seq=3 kind=tool_call agent=\"manager\" call=\"inspect\" paired] manager: [called inspect({})]\n\n[seq=4 kind=tool_result agent=\"manager\" call=\"inspect\" paired] tool: observed fixture result")}
	changes := decodeMockReviewBatch(t, reviewerScript(1, msgs))
	if len(changes) != 2 {
		t.Fatalf("missing complete observation proposal: %+v", changes)
	}
	for _, change := range changes {
		if change.Evidence[0].Seq != 4 || change.Evidence[0].Quote != "observed fixture result" {
			t.Fatalf("tool call became observation evidence: %+v", change.Evidence)
		}
	}
}
