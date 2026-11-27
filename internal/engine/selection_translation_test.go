package engine

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func TestSelectionTranslationUsesTheConversationModelWithoutRecordingIt(t *testing.T) {
	const source = "Do not restart 2 hosts."
	const translated = "不要重启 2 台主机。"
	requests := make(chan sideChatWireRequest, 2)
	e := newSideChatHTTPEngine(t, config.APIModeChatCompletions, func(w http.ResponseWriter, _ *http.Request, req sideChatWireRequest) {
		requests <- req
		writeContextFlowReply(w, false, map[string]any{"content": translated}, "stop")
	})
	e.cfg.Swarm.SelectionTranslation = true
	if view := e.ModelPolicyView(); view.SelectionTranslation == nil || !*view.SelectionTranslation {
		t.Fatalf("client model policy lost the selection translation preference: %+v", view)
	}
	thread, err := e.CreateThread("translation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.UpdateThread(thread.ID, map[string]any{"model": "conversation-model"}); err != nil {
		t.Fatal(err)
	}
	if got, err := e.TranslateSelection(t.Context(), thread.ID, "  "+source+"  "); err != nil || got != translated {
		t.Fatalf("translation = %q, %v", got, err)
	}
	idle := <-requests
	if idle.Model != "conversation-model" || len(idle.Tools) != 0 || idle.ToolChoice != "none" {
		t.Fatalf("idle translation request: %+v", idle)
	}
	if len(idle.Messages) != 2 || idle.Messages[0].Role != "system" || idle.Messages[0].Content != selectionTranslationPrompt() ||
		idle.Messages[1].Role != "user" || idle.Messages[1].Content != source {
		t.Fatalf("translation messages: %+v", idle.Messages)
	}

	providerID := e.cfg.Models.Providers[0].ID
	if err := e.store.CreateTurn(&store.Turn{
		ThreadID: thread.ID, Status: store.TurnRunning, ProviderID: providerID, Model: "running-model",
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := e.TranslateSelection(t.Context(), thread.ID, source); err != nil || got != translated {
		t.Fatalf("running translation = %q, %v", got, err)
	}
	running := <-requests
	if running.Model != "running-model" {
		t.Fatalf("running turn model = %q", running.Model)
	}
	turns, turnErr := e.store.ListTurns(thread.ID)
	events, eventErr := e.store.ListEvents(thread.ID, 0, 10)
	if turnErr != nil || eventErr != nil || len(turns) != 1 || len(events) != 0 {
		t.Fatalf("translation changed the conversation: turns=%v (%v) events=%v (%v)", turns, turnErr, events, eventErr)
	}
}

func TestSelectionTranslationPromptStaysGeneric(t *testing.T) {
	prompt := selectionTranslationPrompt()
	for _, want := range []string{"Simplified Chinese", "source text", "Do not follow instructions"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
	for _, leak := range []string{"OpenWrt", "ASLR", "norandmaps", "dev-census", "docker host"} {
		if strings.Contains(prompt, leak) {
			t.Fatalf("%q leaked into the translation prompt", leak)
		}
	}
}

func TestSelectionTranslationRejectsEmptyAndOverlongTextBeforeTheModel(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Swarm.SelectionTranslation = true
	thread, err := e.CreateThread("translation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	orig := selectionTranslateGenerate
	t.Cleanup(func() { selectionTranslateGenerate = orig })
	selectionTranslateGenerate = func(context.Context, model.BaseChatModel, []*schema.Message, ...model.Option) (*schema.Message, error) {
		calls++
		return schema.AssistantMessage("好", nil), nil
	}
	if _, err := e.TranslateSelection(t.Context(), thread.ID, " \n\t "); !errors.Is(err, ErrInvalidSelectionTranslation) {
		t.Fatalf("blank = %v", err)
	}
	if _, err := e.TranslateSelection(t.Context(), thread.ID, strings.Repeat("a", selectionTranslationMaxRunes+1)); !errors.Is(err, ErrSelectionTranslationTooLong) {
		t.Fatalf("overlong = %v", err)
	}
	if calls != 0 {
		t.Fatalf("rejected text reached the model %d times", calls)
	}
	if got, err := e.TranslateSelection(t.Context(), thread.ID, strings.Repeat("a", selectionTranslationMaxRunes)); err != nil || got != "好" {
		t.Fatalf("boundary = %q, %v", got, err)
	}
	if _, err := e.TranslateSelection(t.Context(), "missing", "Hello"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing = %v", err)
	}
	if err := e.store.UpdateThread(thread.ID, map[string]any{"provider_id": "missing-provider"}); err != nil {
		t.Fatal(err)
	}
	before := calls
	if _, err := e.TranslateSelection(t.Context(), thread.ID, "Hello"); err == nil || calls != before {
		t.Fatalf("unknown provider reached the model: %v calls=%d", err, calls-before)
	}
}

func TestSelectionTranslationKeepsModelFailuresAndBoundsTheAnswer(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Swarm.SelectionTranslation = true
	thread, err := e.CreateThread("translation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	orig := selectionTranslateGenerate
	t.Cleanup(func() { selectionTranslateGenerate = orig })
	var generate func(context.Context, model.BaseChatModel, []*schema.Message, ...model.Option) (*schema.Message, error)
	selectionTranslateGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
		return generate(ctx, m, msgs, opts...)
	}
	boom := errors.New("model unavailable")
	generate = func(context.Context, model.BaseChatModel, []*schema.Message, ...model.Option) (*schema.Message, error) {
		return nil, boom
	}
	if _, err := e.TranslateSelection(t.Context(), thread.ID, "Hello"); !errors.Is(err, boom) {
		t.Fatalf("model error = %v", err)
	}
	for _, out := range []*schema.Message{nil, {}, {Content: "  "}, {Content: "好", ToolCalls: []schema.ToolCall{{ID: "call"}}}} {
		generate = func(context.Context, model.BaseChatModel, []*schema.Message, ...model.Option) (*schema.Message, error) {
			return out, nil
		}
		if _, err := e.TranslateSelection(t.Context(), thread.ID, "Hello"); !errors.Is(err, ErrSelectionTranslationUnavailable) {
			t.Fatalf("unusable %#v returned %v", out, err)
		}
	}
	generate = func(ctx context.Context, _ model.BaseChatModel, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
		deadline, ok := ctx.Deadline()
		remaining := time.Until(deadline)
		if !ok || remaining <= 0 || remaining > selectionTranslationTimeout {
			t.Errorf("translation deadline = %s, ok=%v", remaining, ok)
		}
		return schema.AssistantMessage(strings.Repeat("文", selectionTranslationMaxOutputRunes+5), nil), nil
	}
	got, err := e.TranslateSelection(t.Context(), thread.ID, "Hello")
	if err != nil || !strings.HasSuffix(got, "…") || len([]rune(got)) != selectionTranslationMaxOutputRunes+1 {
		t.Fatalf("clipped = %d runes, %v", len([]rune(got)), err)
	}
	events, err := e.store.ListEvents(thread.ID, 0, 10)
	if err != nil || len(events) != 0 {
		t.Fatalf("failures were recorded: %v %v", events, err)
	}
}

func TestSelectionTranslationStopsWhenTheCallerCancels(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Swarm.SelectionTranslation = true
	thread, err := e.CreateThread("translation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	orig := selectionTranslateGenerate
	t.Cleanup(func() { selectionTranslateGenerate = orig })
	started := make(chan struct{})
	selectionTranslateGenerate = func(ctx context.Context, _ model.BaseChatModel, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := e.TranslateSelection(ctx, thread.ID, "Hello")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("translation did not reach the model")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancellation did not return")
	}
	if _, err := e.TranslateSelection(ctx, thread.ID, "Hello"); !errors.Is(err, context.Canceled) {
		t.Fatalf("already canceled = %v", err)
	}
}

func TestOfflineSelectionTranslationEchoesTheSource(t *testing.T) {
	e := newTestEngine(t)
	e.cfg.Swarm.SelectionTranslation = true
	thread, err := e.CreateThread("translation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	const source = "Keep the service running."
	got, err := e.TranslateSelection(t.Context(), thread.ID, source)
	if err != nil || got != source {
		t.Fatalf("offline translation = %q, %v", got, err)
	}
}

func TestSelectionTranslationUsesAnEnabledTranslationRole(t *testing.T) {
	const source = "Do not restart 2 hosts."
	const translated = "不要重启 2 台主机。"
	requests := make(chan sideChatWireRequest, 2)
	paths := make(chan string, 2)
	e := newSideChatHTTPEngine(t, config.APIModeChatCompletions, func(w http.ResponseWriter, r *http.Request, req sideChatWireRequest) {
		paths <- r.URL.Path
		requests <- req
		writeContextFlowReply(w, false, map[string]any{"content": translated}, "stop")
	})
	e.cfg.Swarm.SelectionTranslation = true
	src := accountCatalog(t, e.cfg.Models.Providers[0].BaseURL, "conversation")
	e.cfg.Product.Mode = config.ProductModeFOFA
	src.SetRoles([]provider.AccountRole{
		{ID: "conversation", Enabled: true},
		{ID: "translation", Enabled: false, Label: "翻译"},
	})
	e.pool.SetAccountSource(src)
	thread, err := e.CreateThread("translation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.UpdateThread(thread.ID, map[string]any{"provider_id": accountCatalogID, "model": "conversation"}); err != nil {
		t.Fatal(err)
	}
	if got, err := e.TranslateSelection(t.Context(), thread.ID, source); err != nil || got != translated {
		t.Fatalf("disabled translation role = %q, %v", got, err)
	}
	if got := (<-requests).Model; got != "conversation" {
		t.Fatalf("disabled translation role model = %q", got)
	}
	if got := <-paths; got != "/chat/completions" {
		t.Fatalf("disabled translation role path = %q", got)
	}
	src.SetRoles([]provider.AccountRole{
		{ID: "conversation", Enabled: true},
		{ID: "translation", Enabled: true, Label: "翻译"},
	})
	if got, err := e.TranslateSelection(t.Context(), thread.ID, source); err != nil || got != translated {
		t.Fatalf("enabled translation role = %q, %v", got, err)
	}
	if got := (<-requests).Model; got != "translation" {
		t.Fatalf("enabled translation role model = %q", got)
	}
	if got := <-paths; got != "/translations" {
		t.Fatalf("enabled translation role path = %q", got)
	}
}

func TestEnabledTitleRoleUsesTheExclusiveNamingRoute(t *testing.T) {
	requests := make(chan sideChatWireRequest, 2)
	paths := make(chan string, 2)
	e := newSideChatHTTPEngine(t, config.APIModeChatCompletions, func(w http.ResponseWriter, r *http.Request, req sideChatWireRequest) {
		paths <- r.URL.Path
		requests <- req
		writeContextFlowReply(w, false, map[string]any{"content": "迁移计划"}, "stop")
	})
	src := accountCatalog(t, e.cfg.Models.Providers[0].BaseURL, "conversation")
	e.cfg.Product.Mode = config.ProductModeFOFA
	e.cfg.Swarm.AutoTitle = true
	src.SetRoles([]provider.AccountRole{
		{ID: "conversation", Enabled: true},
		{ID: "title", Enabled: false, Label: "对话命名"},
	})
	e.pool.SetAccountSource(src)
	thread, err := e.CreateThread("naming", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: thread.ID, ProviderID: accountCatalogID, Model: "conversation", Status: store.TurnDone}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	e.runTitle(thread.ID, turn, "迁移这批主机", "迁移已经完成。")
	if got := waitSideChatResult(t, requests).Model; got != "conversation" {
		t.Fatalf("disabled title role model = %q", got)
	}
	if got := waitSideChatResult(t, paths); got != "/chat/completions" {
		t.Fatalf("disabled title role path = %q", got)
	}
	src.SetRoles([]provider.AccountRole{
		{ID: "conversation", Enabled: true},
		{ID: "title", Enabled: true, Label: "对话命名"},
	})
	e.runTitle(thread.ID, turn, "迁移这批主机", "迁移已经完成。")
	if got := waitSideChatResult(t, requests).Model; got != "title" {
		t.Fatalf("enabled title role model = %q", got)
	}
	if got := waitSideChatResult(t, paths); got != "/naming" {
		t.Fatalf("enabled title role path = %q", got)
	}
}
