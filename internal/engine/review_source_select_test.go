package engine

import (
	"fmt"
	"strings"
	"testing"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Late human constraints must survive earlier tool output without turning a
// retracted or superseded steer into a second, conflicting instruction.
func TestReviewSourceKeepsLateHumanInputAheadOfToolNoise(t *testing.T) {
	events := []store.Event{{Seq: 1, Kind: KindUser, Text: "inspect the fixture"}}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("call-%d", i)
		events = append(events, store.Event{Seq: int64(len(events) + 1), Kind: swarm.NotifyToolCall.String(), AgentID: "worker", ToolCallID: id, Text: "read_fixture({})"})
		events = append(events, store.Event{Seq: int64(len(events) + 1), Kind: swarm.NotifyToolResult.String(), AgentID: "worker", ToolCallID: id, Text: strings.Repeat("trace界", 1000)})
	}
	events = append(events,
		store.Event{Seq: 42, Kind: KindSteer, Text: "obsolete human instruction"},
		store.Event{Seq: 43, Kind: KindSteerRevised, Text: `{"seq":42,"text":"latest human requirement"}`},
		store.Event{Seq: 44, Kind: KindSteer, Text: "withdrawn human instruction"},
		store.Event{Seq: 45, Kind: KindSteerRetracted, Text: `{"seq":44}`})
	got := renderReviewFromEvents(events, strings.Repeat("old briefing ", 1000), "finished")
	if !strings.Contains(got, "human: latest human requirement") {
		t.Fatalf("late effective human input was lost: %s", got)
	}
	if strings.Contains(got, "obsolete human instruction") || strings.Contains(got, "withdrawn human instruction") {
		t.Fatal("inactive human text survived")
	}
	if !strings.Contains(got, "seq=42") {
		t.Fatal("reviewer cannot trace the surviving human input to its event")
	}
	if utf8.RuneCountInString(got) > reviewMaxChars {
		t.Fatalf("framed review exceeds budget: %d", utf8.RuneCountInString(got))
	}
}

func TestReviewSourceSelectsRecentToolPairsAndRendersChronologically(t *testing.T) {
	var events []store.Event
	for i := 0; i < 30; i++ {
		id := fmt.Sprintf("tool-%02d", i)
		events = append(events, store.Event{Seq: int64(len(events) + 1), Kind: swarm.NotifyToolCall.String(), AgentID: "worker", ToolCallID: id, Text: "probe_fixture({})"})
		events = append(events, store.Event{Seq: int64(len(events) + 1), Kind: swarm.NotifyToolResult.String(), AgentID: "worker", ToolCallID: id, Text: fmt.Sprintf("result-%02d ", i) + strings.Repeat("payload ", 1000)})
	}
	got := renderReviewFromEvents(events, "", "")
	if !strings.Contains(got, `call="tool-29"`) || !strings.Contains(got, "result-29") {
		t.Fatal("latest tool evidence was lost")
	}
	if strings.Contains(got, "result-00") {
		t.Fatal("old results displaced recent evidence")
	}
	previous := -1
	for i := 0; i < 30; i++ {
		token := fmt.Sprintf(`call="tool-%02d"`, i)
		count := strings.Count(got, token)
		if count != 0 && count != 2 {
			t.Fatalf("selected tool %d has %d framed halves", i, count)
		}
		if at := strings.Index(got, token); at >= 0 {
			if at < previous {
				t.Fatal("selected events were rendered out of order")
			}
			previous = at
		}
	}
	if !strings.Contains(got, "omitted") || !strings.Contains(got, " …") {
		t.Fatal("missing omission or text clipping marker")
	}
	if utf8.RuneCountInString(got) > reviewMaxChars {
		t.Fatal("paired evidence exceeds rune budget")
	}
}

func TestReviewSourceCountsCJKAndFramingInOneBudget(t *testing.T) {
	var events []store.Event
	for i := 0; i < 60; i++ {
		events = append(events, store.Event{Seq: int64(i + 1), Kind: KindSteer, AgentID: "manager", Role: "manager", Text: fmt.Sprintf("human-%02d ", i) + strings.Repeat("界A", 1500)})
	}
	got := renderReviewFromEvents(events, "", "")
	if !strings.Contains(got, "human-59") || strings.Contains(got, "human-00") {
		t.Fatal("human selection did not retain newest input first")
	}
	if utf8.RuneCountInString(got) > reviewMaxChars {
		t.Fatalf("mixed input plus framing exceeds cap: %d", utf8.RuneCountInString(got))
	}
	if !strings.Contains(got, `role="manager"`) {
		t.Fatal("original agent role disappeared")
	}
}

func TestReviewSourceMarksAutomaticContinuationAsRuntime(t *testing.T) {
	got := renderReviewFromEvents([]store.Event{{Seq: 3, Kind: KindGoalContinued, Text: "continue the open objective"}}, "", "")
	if strings.Contains(got, "human: continue") || !strings.Contains(got, "runtime: continue") {
		t.Fatalf("runtime continuation became human input: %s", got)
	}
}

func TestReviewSourcePairsInterleavedCallsOnlyWithTheirOwnAgent(t *testing.T) {
	events := []store.Event{
		{Seq: 1, Kind: swarm.NotifyToolCall.String(), AgentID: "first", ToolCallID: "shared", Text: "inspect_first({})"},
		{Seq: 2, Kind: swarm.NotifyToolCall.String(), AgentID: "second", ToolCallID: "shared", Text: "inspect_second({})"},
		{Seq: 3, Kind: swarm.NotifyToolResult.String(), AgentID: "first", ToolCallID: "shared", Text: "first result"},
		{Seq: 4, Kind: swarm.NotifyToolResult.String(), AgentID: "second", ToolCallID: "shared", Text: "second result"},
		{Seq: 5, Kind: swarm.NotifyToolResult.String(), AgentID: "third", ToolCallID: "shared", Err: "unmatched failure"},
	}
	got := renderReviewFromEvents(events, "", "")
	if strings.Count(got, " paired]") != 4 || strings.Count(got, " unpaired]") != 1 {
		t.Fatalf("different agents' calls were confused: %s", got)
	}
	previous := -1
	for i := 1; i <= 5; i++ {
		at := strings.Index(got, fmt.Sprintf("seq=%d ", i))
		if at <= previous {
			t.Fatal("interleaved source order changed")
		}
		previous = at
	}
	if !strings.Contains(got, "tool: unmatched failure") {
		t.Fatal("an unmatched failed result was silently discarded")
	}
}

func TestReviewSourceCompletingOldCallMakesItsWholePairRecent(t *testing.T) {
	events := []store.Event{{Seq: 1, Kind: swarm.NotifyToolCall.String(), AgentID: "worker", ToolCallID: "slow", Text: "slow_probe({})"}}
	for i := 0; i < 25; i++ {
		id := fmt.Sprintf("fast-%02d", i)
		events = append(events, store.Event{Seq: int64(len(events) + 1), Kind: swarm.NotifyToolCall.String(), AgentID: "worker", ToolCallID: id, Text: "fast_probe({})"})
		events = append(events, store.Event{Seq: int64(len(events) + 1), Kind: swarm.NotifyToolResult.String(), AgentID: "worker", ToolCallID: id, Text: strings.Repeat("observation ", 500)})
	}
	events = append(events, store.Event{Seq: 52, Kind: swarm.NotifyToolResult.String(), AgentID: "worker", ToolCallID: "slow", Text: "latest completed observation"})
	got := renderReviewFromEvents(events, "", "")
	if strings.Count(got, `call="slow" paired]`) != 2 || !strings.Contains(got, "latest completed observation") {
		t.Fatal("latest completion lost its earlier call")
	}
	if strings.Index(got, "slow_probe") > strings.Index(got, "fast_probe") {
		t.Fatal("pair selection reordered the old call")
	}
}

func TestReviewSourceDoesNotMutatePersistedSteerAndMarksDerivedText(t *testing.T) {
	events := []store.Event{{Seq: 7, Kind: KindSteer, Text: "old text"}, {Seq: 8, Kind: KindSteerRevised, Text: `{"seq":7,"text":"effective text"}`}}
	got := renderReviewFromEvents(events, "earlier context", "final observation")
	if events[0].Text != "old text" {
		t.Fatal("rendering rewrote the source event")
	}
	for _, want := range []string{"human: effective text", "[derived] session briefing: earlier context", "[derived] final answer: final observation"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	for _, text := range []string{strings.Repeat("界", 500), strings.Repeat("x", 500)} {
		body := renderReviewFromEvents([]store.Event{{Seq: 1, Kind: KindUser, Text: text}}, "", "")
		if strings.Contains(body, "omitted") {
			t.Fatal("a short complete source was reported as incomplete")
		}
	}
}

// A small review budget must not format or copy every omitted tool body. The
// source inventory keeps full text until a group is actually considered.
func TestReviewSourceCollectionDefersUnselectedFormatting(t *testing.T) {
	body := strings.Repeat("unselected observation 界 ", 1000)
	items, groups := collectReviewSources([]store.Event{
		{Seq: 1, Kind: KindUser, Text: body},
		{Seq: 2, Kind: swarm.NotifyToolCall.String(), ToolCallID: "one", Text: "probe_fixture({})"},
		{Seq: 3, Kind: swarm.NotifyToolResult.String(), ToolCallID: "one", Text: body},
	}, "earlier context", "final observation")
	for _, item := range items {
		if item.prefix != "" || item.size != 0 || item.clipped {
			t.Fatal("source inventory eagerly formatted or clipped an unselected item")
		}
	}
	if items[1].text != strings.TrimSpace(body) || items[3].text != strings.TrimSpace(body) {
		t.Fatal("inventory discarded original source bytes before selection")
	}
	if _, ok := selectReviewSourceGroup(items, groups[items[2].group], reviewMaxChars); !ok {
		t.Fatal("tool pair did not fit the ordinary review budget")
	}
	if items[2].prefix == "" || items[3].prefix == "" || !items[3].clipped {
		t.Fatal("selection did not prepare both paired items")
	}
	for _, index := range []int{0, 1, 4} {
		if items[index].prefix != "" || items[index].size != 0 || items[index].selected {
			t.Fatal("selecting one group prepared another source")
		}
	}
}

func TestReviewSourceOffsetsSkipDisplayDecorationsWithTheSameWords(t *testing.T) {
	for _, tc := range []struct {
		name string
		ev   store.Event
		want int
	}{
		{"human", store.Event{Kind: KindUser, Text: "called"}, 0},
		{"call", store.Event{Kind: swarm.NotifyToolCall.String(), Text: "called"}, len("[called ")},
		{"result", store.Event{Kind: swarm.NotifyToolResult.String(), Text: "called"}, 0},
		{"failed_result", store.Event{Kind: swarm.NotifyToolResult.String(), Text: "failed", Err: "failed first"}, len("failed first\n")},
		{"error_without_stdout", store.Event{Kind: swarm.NotifyToolResult.String(), Err: "failed first"}, -1},
		{"display_only_roster", store.Event{Kind: swarm.NotifySpawned.String(), AgentID: "worker", Text: "worker details"}, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items, _ := collectReviewSources([]store.Event{tc.ev}, "", "")
			if len(items) != 1 || items[0].sourceAt != tc.want {
				t.Fatalf("source offset=%+v want=%d", items, tc.want)
			}
		})
	}
}

func TestReviewInputDoesNotGrantAuthorityToClippedCallDecoration(t *testing.T) {
	event := store.Event{Seq: 1, Kind: swarm.NotifyToolCall.String(), Text: "called"}
	items, _ := collectReviewSources([]store.Event{event}, "", "")
	prepareReviewSourceItem(&items[0], 1)
	// Leave room for the display's "[called" prefix and the clip marker, but
	// none of the actual call body that happens to contain the same word.
	padding := reviewMaxCharsPerMessage - 2 - utf8.RuneCountInString(items[0].prefix) - 9
	event.ToolCallID = strings.Repeat("a", padding)
	input := selectReviewInput([]store.Event{event}, "", "")
	if !strings.Contains(input.Transcript, "assistant: [called …") {
		t.Fatal("fixture did not leave only the call display decoration")
	}
	if len(input.Events) != 0 {
		t.Fatalf("call display decoration became original source authority: %+v", input.Events)
	}
}

func TestReviewSourceGroupWithNoFramingRoomLeavesSourcesUnprepared(t *testing.T) {
	items, groups := collectReviewSources([]store.Event{{Seq: 1, Kind: KindUser, Text: "human requirement"}}, "", "")
	if used, ok := selectReviewSourceGroup(items, groups[0], len("[derived] final answer: \n\n")-1); ok || used != 0 {
		t.Fatalf("group fit below the shortest possible source frame: used=%d ok=%v", used, ok)
	}
	if items[0].prefix != "" || items[0].size != 0 || items[0].selected {
		t.Fatal("exhausted selection still prepared a source")
	}
}

func TestReviewSourcePartialPairStillFitsWithAllFraming(t *testing.T) {
	items, groups := collectReviewSources([]store.Event{
		{Seq: 1, Kind: swarm.NotifyToolCall.String(), ToolCallID: "one", Text: strings.Repeat("argument", 50)},
		{Seq: 2, Kind: swarm.NotifyToolResult.String(), ToolCallID: "one", Text: strings.Repeat("结果", 300)},
	}, "", "")
	for i := range items {
		prepareReviewSourceItem(&items[i], len(groups[0].items))
	}
	framing := utf8.RuneCountInString(items[0].prefix+items[1].prefix) + 4
	if _, ok := selectReviewSourceGroup(items, groups[0], framing+5); ok {
		t.Fatal("selected pair without space for both content markers")
	}
	used, ok := selectReviewSourceGroup(items, groups[0], framing+120)
	if !ok || used > framing+120 {
		t.Fatalf("partial pair exceeds budget: used=%d ok=%v", used, ok)
	}
	for _, item := range items {
		if !item.selected || !item.clipped || !strings.HasSuffix(item.text, " …") {
			t.Fatal("pair half lost its selection or clipping marker")
		}
	}
	// Empty call IDs are retained as unpaired evidence, never guessed together.
	body := renderReviewFromEvents([]store.Event{{Kind: swarm.NotifyToolCall.String(), Text: "pending_probe({})"}, {Kind: swarm.NotifyToolResult.String(), Text: "legacy observation"}}, "", "")
	if strings.Count(body, " unpaired]") != 2 {
		t.Fatal("legacy events were assigned an invented pairing")
	}
}

func TestReviewSourceClipCountsRunesAndNeverExceedsItsAllowance(t *testing.T) {
	for _, tc := range []struct {
		text    string
		limit   int
		want    string
		clipped bool
	}{
		{"", 0, "", false}, {"abc", 0, "", true}, {"abc", -1, "", true},
		{"界AB", 1, "…", true}, {"界AB", 2, " …", true},
		{"界AB", 3, "界AB", false}, {"界AB", 4, "界AB", false},
		{"界 ABCDE", 5, "界 A …", true}, {"abc", 3, "abc", false},
	} {
		got, clipped := clipReviewSourceText(tc.text, tc.limit)
		if got != tc.want || clipped != tc.clipped {
			t.Errorf("clip(%q,%d)=(%q,%v), want (%q,%v)", tc.text, tc.limit, got, clipped, tc.want, tc.clipped)
		}
		if utf8.RuneCountInString(got) > max(tc.limit, 0) || !utf8.ValidString(got) {
			t.Fatal("clip allowance or encoding violated")
		}
	}
}

// A failed tool can print plausible progress before it fails. Losing the
// terminal error while keeping that stdout would teach an unverified success.
func TestReviewSourcePreservesToolFailureBeforeTruncatingOutput(t *testing.T) {
	const failure = "fixture verification failed"
	events := []store.Event{
		{Seq: 1, Kind: swarm.NotifyToolCall.String(), AgentID: "worker", ToolCallID: "failed-probe", Text: "probe_fixture({})"},
		{Seq: 2, Kind: swarm.NotifyToolResult.String(), AgentID: "worker", ToolCallID: "failed-probe", Text: strings.Repeat("partial observation 界 ", 1000), Err: failure},
	}
	for _, render := range []struct {
		name string
		f    func() string
	}{
		{"review", func() string { return renderReviewFromEvents(events, "", "") }},
		{"session briefing", func() string { return sessionMemoryInput("", events) }},
	} {
		t.Run(render.name, func(t *testing.T) {
			got := render.f()
			if !strings.Contains(got, failure) {
				t.Fatalf("tool failure disappeared behind clipped stdout: %s", got)
			}
			if strings.Index(got, failure) > strings.Index(got, "partial observation") {
				t.Fatal("failure must precede output that may be truncated")
			}
		})
	}
}

func TestReviewSourceBudgetIncludesQuotedMetadataAndEveryPairPart(t *testing.T) {
	for _, humanCount := range []int{0, 1, 12, 40} {
		for _, bodySize := range []int{1, 2, 3, 1500, 3000} {
			t.Run(fmt.Sprintf("humans-%d/body-%d", humanCount, bodySize), func(t *testing.T) {
				var events []store.Event
				for i := 0; i < humanCount; i++ {
					events = append(events, store.Event{Seq: int64(len(events) + 1), Kind: KindUser, AgentID: "manager", Text: strings.Repeat("界A", bodySize)})
				}
				for i := 0; i < 15; i++ {
					id := fmt.Sprintf("probe-%d-\"quoted\"", i)
					events = append(events, store.Event{Seq: int64(len(events) + 1), Kind: swarm.NotifyToolCall.String(), AgentID: "观察者", Role: "reader\nrole", ToolCallID: id, Text: "probe_fixture({})"})
					for n := 0; n < 2; n++ {
						events = append(events, store.Event{Seq: int64(len(events) + 1), Kind: swarm.NotifyToolResult.String(), AgentID: "观察者", ToolCallID: id, Text: strings.Repeat("观察B", bodySize)})
					}
				}
				got := renderReviewFromEvents(events, strings.Repeat("旧摘要", bodySize), strings.Repeat("final ", bodySize))
				if utf8.RuneCountInString(got) > reviewMaxChars {
					t.Fatalf("metadata, pair parts or marker escaped cap: %d", utf8.RuneCountInString(got))
				}
				if !utf8.ValidString(got) {
					t.Fatal("clipping split a UTF-8 rune")
				}
				if strings.Contains(got, "reader\nrole") {
					t.Fatal("metadata injected an unframed newline")
				}
			})
		}
	}
}

// Metadata is part of the per-source allowance. An unrenderable half must
// not leave its paired receipt visible or crowd out useful source evidence.
func TestReviewSourceOmitsOversizedFramingAsACompleteGroup(t *testing.T) {
	for _, field := range []string{"agent", "role", "result role", "call"} {
		for _, padding := range []string{strings.Repeat("x", 2500), strings.Repeat("界\n\"", 800)} {
			t.Run(field+fmt.Sprint(len(padding)), func(t *testing.T) {
				call := store.Event{Seq: 1, Kind: swarm.NotifyToolCall.String(), AgentID: "worker", Role: "reader", ToolCallID: "oversized", Text: "inspect({})"}
				result := store.Event{Seq: 2, Kind: swarm.NotifyToolResult.String(), AgentID: "worker", Role: "reader", ToolCallID: "oversized", Text: "unrenderable observation"}
				switch field {
				case "agent":
					call.AgentID, result.AgentID = padding, padding
				case "role":
					call.Role, result.Role = padding, padding
				case "result role":
					result.Role = padding
				case "call":
					call.ToolCallID, result.ToolCallID = padding, padding
				}
				events := []store.Event{call, result,
					{Seq: 3, Kind: KindSteer, Text: "keep the supported convention 界 …"},
					{Seq: 4, Kind: swarm.NotifyToolCall.String(), AgentID: "reader", ToolCallID: "small", Text: "inspect({})"},
					{Seq: 5, Kind: swarm.NotifyToolResult.String(), AgentID: "reader", ToolCallID: "small", Text: "observed source bytes 界 …"},
				}
				input := selectReviewInput(events, "", "")
				if strings.Contains(input.Transcript, "[seq=1 ") || strings.Contains(input.Transcript, "[seq=2 ") {
					t.Fatal("oversized metadata escaped the per-source cap or left half a pair")
				}
				if _, ok := input.Events[1]; ok {
					t.Fatal("omitted call gained source authority")
				}
				if _, ok := input.Events[2]; ok {
					t.Fatal("omitted result gained source authority")
				}
				if len(input.Events) != 3 || input.Events[3].Text != events[2].Text || input.Events[5].Text != events[4].Text {
					t.Fatalf("other visible source bytes or identity changed: %+v", input.Events)
				}
				if sources := reviewEvidenceSources(events, input.Events); len(sources) != 2 || sources[5].Text != events[4].Text {
					t.Fatalf("omitted group affected writable evidence: %+v", sources)
				}
				if !strings.Contains(input.Transcript, reviewSourceOmitted) || utf8.RuneCountInString(input.Transcript) > reviewMaxChars || !utf8.ValidString(input.Transcript) {
					t.Fatal("review lost its omission marker, total budget, or valid UTF-8")
				}
				for _, source := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(input.Transcript, reviewSourceHeader), reviewSourceOmitted), "\n\n") {
					if source != "" && utf8.RuneCountInString(source)+2 > reviewMaxCharsPerMessage {
						t.Fatalf("source framing exceeds its own allowance: %d", utf8.RuneCountInString(source)+2)
					}
				}
			})
		}
	}
}
