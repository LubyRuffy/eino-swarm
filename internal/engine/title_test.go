package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func waitForTitle(t *testing.T, e *Engine, turnID string) store.Event {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		events, err := e.Store().ListTurnEvents(turnID)
		if err != nil {
			t.Fatalf("ListTurnEvents: %v", err)
		}
		for _, ev := range events {
			if ev.Kind == KindTitle {
				return ev
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("turn %s never recorded a title event", turnID)
	return store.Event{}
}

// After the opening message the sidebar shows a generated name, not the
// raw request. That is the whole feature: quoting the user was the old bug.
func TestAFinishedTurnGetsAGeneratedTitle(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	const ask = "look into the reporting pipeline"
	turn, err := e.StartTurn(th.ID, ask)
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	ev := waitForTitle(t, e, turn.ID)
	if ev.AgentID != TitleAgentID {
		t.Fatalf("title event agent=%q", ev.AgentID)
	}
	if ev.Err != "" || ev.Text == "" {
		t.Fatalf("title event=%+v", ev)
	}
	if ev.Text == ask {
		t.Fatalf("the generated title was the request verbatim: %q", ev.Text)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Title != ev.Text {
		t.Fatalf("stored title=%q event=%q", got.Title, ev.Text)
	}
	if got.TitleAuto {
		t.Fatal("a landed title must not be overwritten by a later namer")
	}

	next, err := e.StartTurn(th.ID, "a different follow-up entirely")
	if err != nil {
		t.Fatalf("second StartTurn: %v", err)
	}
	waitForTurn(t, e, next.ID)
	time.Sleep(50 * time.Millisecond)
	later, _ := e.Store().ListTurnEvents(next.ID)
	for _, ev := range later {
		if ev.Kind == KindTitle {
			t.Fatal("a later turn must not name the conversation again")
		}
	}
	got, _ = e.Store().GetThread(th.ID)
	if got.Title != ev.Text {
		t.Fatalf("a later turn overwrote the title: %q", got.Title)
	}

	calls, err := e.Store().ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatalf("ListLLMCalls: %v", err)
	}
	found := false
	for _, c := range calls {
		if c.AgentID == TitleAgentID {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("the namer's model call is missing from the turn's trace")
	}
}

func TestAnExplicitTitleIsNeverGeneratedOver(t *testing.T) {
	e := newTestEngine(t)
	named, _ := e.CreateThread("My Name", "", "")
	if named.TitleAuto {
		t.Fatal("an explicit title must not be machine-owned")
	}
	turn, err := e.StartTurn(named.ID, "something else entirely")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	time.Sleep(80 * time.Millisecond)
	events, _ := e.Store().ListTurnEvents(turn.ID)
	for _, ev := range events {
		if ev.Kind == KindTitle {
			t.Fatalf("named a conversation the user already named: %+v", ev)
		}
	}
	got, _ := e.Store().GetThread(named.ID)
	if got.Title != "My Name" {
		t.Fatalf("an explicit title was replaced: %q", got.Title)
	}
}

func TestAUserRenameBeatsASlowNamer(t *testing.T) {
	e := newTestEngine(t)
	// The mock namer is one Generate. Without a hold it can land before
	// RenameThread and emit a title that was still machine-owned. The
	// rule is a rename that arrives while that call is in flight.
	started := make(chan struct{})
	release := make(chan struct{})
	orig := titleGenerate
	titleGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
		select {
		case <-started:
		default:
			close(started)
		}
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return orig(ctx, m, msgs)
	}
	t.Cleanup(func() { titleGenerate = orig })

	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "look into the reporting pipeline")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("the namer never started")
	}
	if err := e.RenameThread(th.ID, "Keep this"); err != nil {
		t.Fatal(err)
	}
	close(release)
	waitForTurn(t, e, turn.ID)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		events, _ := e.Store().ListTurnEvents(turn.ID)
		for _, ev := range events {
			if ev.Kind == KindTitle {
				t.Fatalf("emitted a title event after a user rename: %+v", ev)
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Title != "Keep this" || got.TitleAuto {
		t.Fatalf("rename lost the race: %+v", got)
	}
}

func TestAnInterruptedOpeningTurnKeepsOneName(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "look into the reporting pipeline")
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(120 * time.Millisecond)
	if err := e.Interrupt(th.ID); err != nil && !errors.Is(err, ErrIdle) {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	ev := waitForTitle(t, e, turn.ID)
	if ev.Err != "" || ev.Text == "" {
		t.Fatalf("opening message must still be named: %+v", ev)
	}
	named, _ := e.Store().GetThread(th.ID)
	if named.Title != ev.Text {
		t.Fatalf("stored title=%q event=%q", named.Title, ev.Text)
	}
	if named.TitleAuto {
		t.Fatal("a landed title must not be overwritten by a later namer")
	}

	next, err := e.StartTurn(th.ID, "a different follow-up entirely")
	if err != nil {
		t.Fatalf("second StartTurn: %v", err)
	}
	waitForTurn(t, e, next.ID)
	time.Sleep(50 * time.Millisecond)
	later, _ := e.Store().ListTurnEvents(next.ID)
	for _, ev := range later {
		if ev.Kind == KindTitle {
			t.Fatal("a later turn must not name the conversation again")
		}
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Title != ev.Text {
		t.Fatalf("a later turn overwrote the title: %q", got.Title)
	}
}

func TestAutoTitleOffLeavesThePlaceholder(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoTitle = false
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "look into the reporting pipeline")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	time.Sleep(80 * time.Millisecond)
	events, _ := e.Store().ListTurnEvents(turn.ID)
	for _, ev := range events {
		if ev.Kind == KindTitle {
			t.Fatalf("named a conversation with auto-title off: %+v", ev)
		}
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Title != "look into the reporting pipeline" {
		t.Fatalf("title=%q", got.Title)
	}
}

func TestTitlePromptIsGenericAndGrounded(t *testing.T) {
	p := titlePrompt()
	for _, need := range []string{"sidebar", "noun phrase", "same language"} {
		if !strings.Contains(strings.ToLower(p), strings.ToLower(need)) {
			t.Fatalf("the namer prompt does not say %q", need)
		}
	}
	for _, leak := range []string{
		"summarize", "researcher", "reviewer", "notes/", "elasticsearch",
		"datakanban", "reporting pipeline",
	} {
		if strings.Contains(strings.ToLower(p), strings.ToLower(leak)) {
			t.Fatalf("the namer prompt hardcodes example-specific text %q", leak)
		}
	}
}

func TestSanitizeTitle(t *testing.T) {
	cases := map[string]string{
		`  "Weekly status"  `:   "Weekly status",
		"# Pipeline work\nmore": "Pipeline work",
		"Hello world.":          "Hello world",
		"   ":                   "",
		strings.Repeat("字", 80): strings.Repeat("字", titleMaxRunes) + "…",
	}
	for in, want := range cases {
		if got := sanitizeTitle(in); got != want {
			t.Fatalf("sanitizeTitle(%q)=%q want %q", in, got, want)
		}
	}
}

func TestTitlePoolRefusesWorkAfterStop(t *testing.T) {
	p := newTitlePool()
	if !p.stop(time.Second) {
		t.Fatal("an idle pool did not stop")
	}
	if p.begin("th") {
		t.Fatal("accepted a title job after stop")
	}
}

func TestTitlePoolAllowsOnlyOneJobPerConversation(t *testing.T) {
	p := newTitlePool()
	if !p.begin("th") {
		t.Fatal("first job refused")
	}
	if p.begin("th") {
		t.Fatal("a second job for the same conversation was accepted")
	}
	if !p.begin("other") {
		t.Fatal("a different conversation was blocked")
	}
	p.done("th")
	p.done("other")
	if !p.begin("th") {
		t.Fatal("a finished job should free the conversation")
	}
	p.done("th")
}

func TestTitleInputCarriesUserAndAnswer(t *testing.T) {
	got := titleInput("  look into  this  ", "the answer is ready")
	if !strings.Contains(got, "User: look into this") {
		t.Fatalf("user line missing: %q", got)
	}
	if !strings.Contains(got, "Assistant: the answer is ready") {
		t.Fatalf("assistant line missing: %q", got)
	}
	if strings.Contains(strings.ToLower(got), "summarize") {
		t.Fatalf("title input leaked example text: %q", got)
	}
	if strings.Contains(titleInput("ask only", "  "), "Assistant:") {
		t.Fatal("a blank answer must not invent an assistant line")
	}
}

func TestTitleFromQuotedMessageUsesTheRequest(t *testing.T) {
	tagged := "<selected_text>\nalpha beta\n</selected_text>\n\n<user_request>\ndo this\n</user_request>"
	if got := titleFrom(tagged); got != "do this" {
		t.Fatalf("titleFrom=%q", got)
	}
	if got := titleInput(tagged, "ready"); !strings.Contains(got, "User: do this") {
		t.Fatalf("title input kept the wrapper: %q", got)
	}
	if strings.Contains(titleInput(tagged, "ready"), "<selected_text>") {
		t.Fatal("the namer must not see the quote tags")
	}
	legacy := "Selected text:\nalpha beta\n\ndo this"
	if got := titleFrom(legacy); got != "do this" {
		t.Fatalf("legacy titleFrom=%q", got)
	}
	if got := titleFrom("<selected_text>\nalpha beta\n</selected_text>"); got != "alpha beta" {
		t.Fatalf("quote-only titleFrom=%q", got)
	}
}

func TestAutoTitleSkipsBlankInput(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if e.autoTitle(th, " \n\t ") {
		t.Fatal("blank input reported a plant")
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Title != "" {
		t.Fatalf("blank input set a title: %q", got.Title)
	}
}

func TestAutoTitleReportsWhetherItPlanted(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !e.autoTitle(th, "look into the reporting pipeline") {
		t.Fatal("the opening line must plant a placeholder")
	}
	if e.autoTitle(th, "a different follow-up entirely") {
		t.Fatal("a second message must not plant again")
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Title != "look into the reporting pipeline" {
		t.Fatalf("placeholder=%q", got.Title)
	}
}

func TestAutoTitleSurvivesAClosedStore(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().Close(); err != nil {
		t.Fatal(err)
	}
	e.autoTitle(th, "look into the reporting pipeline")
}

func TestScheduleTitleSkipsAMissingConversation(t *testing.T) {
	e := newTestEngine(t)
	turn := &store.Turn{ID: "tn_gone", ThreadID: "th_gone", ProviderID: e.Config().Models.Default}
	e.scheduleTitle("th_gone", turn, "ask", "ans")
	time.Sleep(30 * time.Millisecond)
	events, err := e.Store().ListTurnEvents("tn_gone")
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindTitle {
			t.Fatalf("named a conversation that does not exist: %+v", ev)
		}
	}
}

func TestScheduleTitleRefusesASecondNamer(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if !e.titles.begin(th.ID) {
		t.Fatal("begin")
	}
	t.Cleanup(func() { e.titles.done(th.ID) })
	turn := &store.Turn{ID: "tn_second", ThreadID: th.ID, ProviderID: e.Config().Models.Default}
	e.scheduleTitle(th.ID, turn, "ask", "ans")
	time.Sleep(40 * time.Millisecond)
	events, _ := e.Store().ListTurnEvents("tn_second")
	for _, ev := range events {
		if ev.Kind == KindTitle {
			t.Fatal("a second namer ran")
		}
	}
}

func TestScheduleTitleDoesNotNeedAFinishedTurn(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ID: "tn_open", ThreadID: th.ID, ProviderID: e.Config().Models.Default}
	e.scheduleTitle(th.ID, turn, "look into the reporting pipeline", "")
	ev := waitForTitle(t, e, "tn_open")
	if ev.Err != "" || ev.Text == "" {
		t.Fatalf("title event=%+v", ev)
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Title != ev.Text || got.TitleAuto {
		t.Fatalf("landed title=%+v", got)
	}
}

func TestAFailedOpeningNamerDoesNotRenameOnTheNextTurn(t *testing.T) {
	e := newTestEngine(t)
	orig := titleGenerate
	titleGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return nil, errors.New("namer down")
	}
	t.Cleanup(func() { titleGenerate = orig })

	th, _ := e.CreateThread("", "", "")
	const ask = "look into the reporting pipeline"
	turn, err := e.StartTurn(th.ID, ask)
	if err != nil {
		t.Fatal(err)
	}
	ev := waitForTitle(t, e, turn.ID)
	if ev.Err == "" {
		t.Fatal("want the opening namer to fail")
	}
	waitForTurn(t, e, turn.ID)
	got, _ := e.Store().GetThread(th.ID)
	if got.Title != ask {
		t.Fatalf("placeholder=%q", got.Title)
	}

	titleGenerate = orig
	next, err := e.StartTurn(th.ID, "a different follow-up entirely")
	if err != nil {
		t.Fatalf("second StartTurn: %v", err)
	}
	waitForTurn(t, e, next.ID)
	time.Sleep(80 * time.Millisecond)
	later, _ := e.Store().ListTurnEvents(next.ID)
	for _, ev := range later {
		if ev.Kind == KindTitle {
			t.Fatal("a later turn must not get a second namer")
		}
	}
	got, _ = e.Store().GetThread(th.ID)
	if got.Title != ask {
		t.Fatalf("a later turn overwrote the placeholder: %q", got.Title)
	}
}

func TestANamerPanicDoesNotKillTheProcess(t *testing.T) {
	e := newTestEngine(t)
	orig := titleGenerate
	titleGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		panic("namer boom")
	}
	t.Cleanup(func() { titleGenerate = orig })

	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ID: "tn_panic", ThreadID: th.ID, ProviderID: e.Config().Models.Default}
	e.scheduleTitle(th.ID, turn, "ask", "ans")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if e.titles.begin(th.ID) {
			e.titles.done(th.ID)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("the panicked namer never released the conversation")
}

func TestANilTurnPanicDoesNotKillTheProcess(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	e.scheduleTitle(th.ID, nil, "ask", "ans")

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if e.titles.begin(th.ID) {
			e.titles.done(th.ID)
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("a nil-turn namer never released the conversation")
}

func TestRunTitleRecordsAnUnknownProvider(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ID: "tn_noprov", ThreadID: th.ID, ProviderID: "nope"}
	e.runTitle(th.ID, turn, "ask", "ans")
	ev := waitForTitle(t, e, "tn_noprov")
	if ev.Err == "" {
		t.Fatal("want an error for an unknown provider")
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Title != "" {
		t.Fatalf("a failed namer still named it: %q", got.Title)
	}
}

func TestRunTitleRecordsAFailedGenerate(t *testing.T) {
	e := newTestEngine(t)
	orig := titleGenerate
	titleGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return nil, errors.New("endpoint down")
	}
	t.Cleanup(func() { titleGenerate = orig })

	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ID: "tn_genfail", ThreadID: th.ID, ProviderID: e.Config().Models.Default}
	e.runTitle(th.ID, turn, "ask", "ans")
	ev := waitForTitle(t, e, "tn_genfail")
	if ev.Err == "" {
		t.Fatal("want the generate error on the title event")
	}
}

func TestRunTitleRejectsAnEmptyModelReply(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ID: "tn_empty", ThreadID: th.ID, ProviderID: e.Config().Models.Default}
	e.runTitle(th.ID, turn, "...", "")
	ev := waitForTitle(t, e, "tn_empty")
	if ev.Err == "" || ev.Text != "" {
		t.Fatalf("dots-only reply must not become a title: %+v", ev)
	}
}

func TestRunTitleRejectsANilModelReply(t *testing.T) {
	e := newTestEngine(t)
	orig := titleGenerate
	titleGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return nil, nil
	}
	t.Cleanup(func() { titleGenerate = orig })

	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ID: "tn_nil", ThreadID: th.ID, ProviderID: e.Config().Models.Default}
	e.runTitle(th.ID, turn, "ask", "ans")
	ev := waitForTitle(t, e, "tn_nil")
	if ev.Err == "" {
		t.Fatalf("a nil reply must not become a title: %+v", ev)
	}
}

func TestRunTitleLosesToARename(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.RenameThread(th.ID, "Keep this"); err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ID: "tn_race", ThreadID: th.ID, ProviderID: e.Config().Models.Default}
	e.runTitle(th.ID, turn, "look into the reporting pipeline", "final")
	events, err := e.Store().ListTurnEvents("tn_race")
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindTitle {
			t.Fatalf("emitted a title event after a user rename: %+v", ev)
		}
	}
	got, _ := e.Store().GetThread(th.ID)
	if got.Title != "Keep this" || got.TitleAuto {
		t.Fatalf("rename lost: %+v", got)
	}
}

func TestRunTitleRecordsAStoreFailure(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ID: "tn_store", ThreadID: th.ID, ProviderID: e.Config().Models.Default}
	if err := e.Store().Close(); err != nil {
		t.Fatal(err)
	}
	e.runTitle(th.ID, turn, "look into the reporting pipeline", "final")
}

func TestRunTitleHonoursAPinnedModel(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.TitleProvider = e.Config().Models.Default
	e.Config().Swarm.TitleModel = "tiny"
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ID: "tn_pin", ThreadID: th.ID, ProviderID: e.Config().Models.Default, Model: "heavy"}
	e.runTitle(th.ID, turn, "look into the reporting pipeline", "final")
	ev := waitForTitle(t, e, "tn_pin")
	if ev.Err != "" || ev.Text == "" {
		t.Fatalf("title event=%+v", ev)
	}
	calls, err := e.Store().ListLLMCalls("tn_pin")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range calls {
		if c.AgentID == TitleAgentID {
			found = true
			if c.Model != "tiny" {
				t.Fatalf("namer model=%q want the pinned name", c.Model)
			}
			if c.ProviderID != e.Config().Models.Default {
				t.Fatalf("namer provider=%q", c.ProviderID)
			}
		}
	}
	if !found {
		t.Fatal("the namer's model call is missing from the turn's trace")
	}
}

func TestAPinnedUnknownProviderFailsEvenWhenTheTurnIsFine(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.TitleProvider = "nope"
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ID: "tn_pin_bad", ThreadID: th.ID, ProviderID: e.Config().Models.Default}
	e.runTitle(th.ID, turn, "ask", "ans")
	ev := waitForTitle(t, e, "tn_pin_bad")
	if ev.Err == "" {
		t.Fatal("want an error for a pinned unknown provider")
	}
}

func TestTitlePoolStopDoesNotWaitForever(t *testing.T) {
	p := newTitlePool()
	if !p.begin("th") {
		t.Fatal("begin")
	}
	if p.stop(20 * time.Millisecond) {
		t.Fatal("stop should give up on a job that never finishes")
	}
}
