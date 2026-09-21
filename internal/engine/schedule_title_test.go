package engine

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func waitForScheduleNamed(t *testing.T, e *Engine, id string) *store.Schedule {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		got, err := e.Store().GetSchedule(id)
		if err != nil {
			t.Fatalf("GetSchedule: %v", err)
		}
		if !got.TitleAuto && strings.TrimSpace(got.Title) != "" {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("schedule never received a generated title")
	return nil
}

func waitForScheduleNamerIdle(t *testing.T, e *Engine, id string) {
	t.Helper()
	key := scheduleTitlePoolKey(id)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		e.titles.mu.Lock()
		_, busy := e.titles.inflight[key]
		e.titles.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("schedule namer still in flight")
}

func TestUntitledScheduleGetsAGeneratedInboxLabel(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	planted := titleFrom(scheduleWaitPrompt)
	if sch.Title != planted || !sch.TitleAuto {
		t.Fatalf("create must plant a placeholder: %+v", sch)
	}

	got := waitForScheduleNamed(t, e, sch.ID)
	if got.Title == scheduleWaitPrompt || got.Title == planted {
		t.Fatalf("generated title was the request: %q", got.Title)
	}
	if got.TitleAuto {
		t.Fatal("a landed title must not be overwritten by a later namer")
	}
	for _, w := range []string{"CI", "deploy", "GitHub", "notes.md", "summarize"} {
		if strings.Contains(got.Title, w) || strings.Contains(scheduleTitlePrompt(), w) {
			t.Fatalf("leaked %q", w)
		}
	}
	if hasKind(t, e, th.ID, KindTitle) {
		t.Fatal("naming a wait must not emit a conversation title event")
	}
}

func TestExplicitScheduleTitleIsNeverGeneratedOver(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Title: "wake", Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	if sch.Title != "wake" || sch.TitleAuto {
		t.Fatalf("an explicit title must not be machine-owned: %+v", sch)
	}
	time.Sleep(80 * time.Millisecond)
	got, _ := e.Store().GetSchedule(sch.ID)
	if got.Title != "wake" || got.TitleAuto {
		t.Fatalf("namer overwrote an explicit title: %+v", got)
	}
}

func TestScheduleAutoTitleOffLeavesThePlaceholder(t *testing.T) {
	e := newTestEngine(t)
	e.Config().Swarm.AutoTitle = false
	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	planted := titleFrom(scheduleWaitPrompt)
	if sch.Title != planted || !sch.TitleAuto {
		t.Fatalf("off still plants a placeholder: %+v", sch)
	}
	time.Sleep(80 * time.Millisecond)
	got, _ := e.Store().GetSchedule(sch.ID)
	if got.Title != planted || !got.TitleAuto {
		t.Fatalf("off must not run the namer: %+v", got)
	}
	_ = th
}

func TestAScheduleTitlePatchBeatsASlowNamer(t *testing.T) {
	e := newTestEngine(t)
	orig := titleGenerate
	t.Cleanup(func() { titleGenerate = orig })
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	titleGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
		close(started)
		select {
		case <-release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		return orig(ctx, m, msgs)
	}

	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("namer never started")
	}
	mine := "Keep this"
	if _, err := e.PatchScheduleFields(sch.ID, ScheduleFields{Title: &mine}); err != nil {
		t.Fatal(err)
	}
	once.Do(func() { close(release) })
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := e.Store().GetSchedule(sch.ID)
		if got.Title != mine || got.TitleAuto {
			t.Fatalf("patch lost the race: %+v", got)
		}
		time.Sleep(20 * time.Millisecond)
	}
	waitForScheduleNamerIdle(t, e, sch.ID)
}

func TestAFailedScheduleNamerKeepsThePlaceholder(t *testing.T) {
	e := newTestEngine(t)
	orig := titleGenerate
	t.Cleanup(func() { titleGenerate = orig })
	titleGenerate = func(context.Context, model.BaseChatModel, []*schema.Message) (*schema.Message, error) {
		return nil, context.Canceled
	}

	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleStandalone, Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	planted := titleFrom(scheduleWaitPrompt)
	waitForScheduleNamerIdle(t, e, sch.ID)
	got, _ := e.Store().GetSchedule(sch.ID)
	if got.Title != planted || !got.TitleAuto {
		t.Fatalf("a failed namer still named it: %+v", got)
	}
}

func TestScheduleTitlePromptIsGeneric(t *testing.T) {
	p := scheduleTitlePrompt()
	for _, w := range []string{"CI", "deploy", "GitHub", "notes.md", "summarize"} {
		if strings.Contains(strings.ToLower(p), strings.ToLower(w)) {
			t.Fatalf("leaked %q into the namer prompt", w)
		}
	}
}

func TestReplacingAWakeWithoutATitleKeepsTheGeneratedName(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sch, err := e.CreateSchedule(ScheduleInput{
		Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: scheduleWaitPrompt, EveryS: 60,
		CreatedBy: store.ScheduleCreatedHuman,
	})
	if err != nil {
		t.Fatal(err)
	}
	named := waitForScheduleNamed(t, e, sch.ID)

	out, err := e.scheduleWakeJSON(th.ID, "", `{"prompt":"`+scheduleWaitPrompt+`","delay_s":90}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, named.ID) {
		t.Fatalf("replace must target the open wake: %s", out)
	}
	got, _ := e.Store().GetSchedule(named.ID)
	if got.Title != named.Title || got.TitleAuto || got.DelayS != 90 {
		t.Fatalf("omitting title on replace wiped the name: %+v", got)
	}
}
