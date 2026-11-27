package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

func roleManifest(role, endpoint string, configurable bool) *SourceCapabilities {
	return &SourceCapabilities{Authorized: true, Available: true, Configurable: configurable, Capabilities: map[string]RoleCapability{
		role: {Supported: true, Available: true, Configurable: configurable, Endpoints: []string{endpoint}},
	}}
}

func TestMissingNegotiationDoesNotBorrowLegacyPermission(t *testing.T) {
	cfg := config.Default()
	p := New(cfg)
	src := accountSourceFor(t, "https://gateway.invalid/v1", "private")
	src.SetModelPolicy(true, map[string]string{"conversation": "server-model"})
	src.SetCapabilities(nil)
	p.SetAccountSource(src)
	if policy := p.ModelPolicy(); policy.Ready || policy.Configurable || policy.CapabilityStatus != "unavailable" {
		t.Fatalf("policy=%+v", policy)
	}
	for _, row := range p.List() {
		if row.Source == SourceAccount {
			t.Fatal("unknown capability opened conversation choices")
		}
	}
	if _, _, err := p.RoleProvider("speech"); !errors.Is(err, ErrModelPolicy) {
		t.Fatalf("speech=%v", err)
	}
	if _, configured := p.RoleSelection("translation", "personal", "fallback"); !configured {
		t.Fatal("unknown role fell back to personal selection")
	}
}

func TestCapabilityAdaptersAndSourcesStayIndependent(t *testing.T) {
	for _, role := range config.ModelRoles() {
		endpoints := ImplementedEndpoints(role)
		for _, endpoint := range endpoints {
			c := roleManifest(role, endpoint, true)
			if !c.Allows(role) {
				t.Fatalf("%s/%s refused", role, endpoint)
			}
			c.Capabilities[role] = RoleCapability{Supported: true, Available: true, Endpoints: []string{"unknown"}}
			if c.Allows(role) {
				t.Fatalf("%s enabled unknown adapter", role)
			}
		}
	}
	if roleManifest("image", "responses", true).Allows("image") {
		t.Fatal("image generation borrowed the chat adapter")
	}
	c := roleManifest("speech", "audio_speech", true)
	copy := cloneCapabilities(c)
	c.Capabilities["speech"].Endpoints[0] = "unknown"
	if !copy.Allows("speech") {
		t.Fatal("published capability aliased caller memory")
	}
	copy.Authorized = false
	if copy.Allows("speech") {
		t.Fatal("source denial borrowed role permission")
	}
	for _, raw := range []string{`{}`, `{"data":{"version":2}}`, `{"data":{"version":1,"model_roles_version":3,"sources":{"preset":{"capabilities":{"naming":{}}}}}}`} {
		if _, err := ParseCapabilities([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	raw, _ := json.Marshal(map[string]any{"data": ClientCapabilities{Version: 1, ModelRolesVersion: 3, Sources: map[string]*SourceCapabilities{"preset": copy, "gateway": {}}}})
	if _, err := ParseCapabilities(raw); err != nil {
		t.Fatal(err)
	}
}

func TestRoleUsesPublicResponsesAndRefusesAfterGrantRemoval(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/v1/responses" || r.Header.Get("Authorization") != "Bearer private" {
			t.Errorf("wrong role route")
		}
		fmt.Fprint(w, `{"id":"r","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"result"}]}]}`)
	}))
	defer server.Close()
	cfg := config.Default()
	p := New(cfg)
	src := accountSourceFor(t, server.URL+"/v1", "private")
	caps := roleManifest("translation", "responses", true)
	caps.Capabilities["conversation"] = RoleCapability{Supported: true, Available: true, Configurable: true, Endpoints: []string{"chat_completions"}}
	src.SetCapabilities(caps)
	src.SetModelPolicy(true, map[string]string{"translation": "server-model"})
	p.SetAccountSource(src)
	factory, err := p.RoleModelBuilder(context.Background(), src.id, "server-model", "", nil, "translation")
	if err != nil {
		t.Fatal(err)
	}
	m := factory("translator-id", "translator-id")
	out, err := m.Generate(t.Context(), []*schema.Message{schema.UserMessage("input")})
	if err != nil || out.Content != "result" || requests.Load() != 1 {
		t.Fatalf("output=%v err=%v calls=%d", out, err, requests.Load())
	}
	// Removing one role must refuse this cached adapter while the source and
	// conversation grant remain healthy; global discovery failure is different.
	revoked := *src
	revoked.SetCapabilities(roleManifest("conversation", "chat_completions", true))
	p.SetAccountSource(&revoked)
	if _, err := m.Generate(t.Context(), []*schema.Message{schema.UserMessage("input")}); !errors.Is(err, ErrModelPolicy) || requests.Load() != 1 {
		t.Fatalf("revoked role reached HTTP: err=%v calls=%d", err, requests.Load())
	}
	if !p.ModelPolicy().Ready {
		t.Fatal("role refusal fixture invalidated the whole conversation source")
	}
	p.InvalidateCapabilities()
	if _, err := m.Generate(t.Context(), []*schema.Message{schema.UserMessage("input")}); err == nil || requests.Load() != 1 {
		t.Fatal("revoked adapter sent a request")
	}
	if !strings.Contains(p.ModelPolicy().CapabilityStatus, "unavailable") {
		t.Fatal("missing retry state")
	}
}

func TestGatewayChatProtocolFollowsEachRoleAndKeepsOpenedClients(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer fixture-device" {
			t.Error("gateway call did not use POST and the device credential")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/chat/completions":
			fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"chat"},"finish_reason":"stop"}]}`)
		case "/v1/responses":
			fmt.Fprint(w, `{"id":"r","object":"response","status":"completed","output":[{"type":"message","role":"assistant","content":[{"type":"output_text","text":"responses"}]}]}`)
		default:
			t.Errorf("unexpected gateway path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	p := New(config.Default())
	ctx := t.Context()
	input := []*schema.Message{schema.UserMessage("input")}
	src := accountSourceFor(t, server.URL+"/v1", "fixture-device")
	src.SetModelPolicy(true, map[string]string{"conversation": "server-model", "translation": "server-model-2"})
	publish := func(authorized bool, endpoints ...string) {
		caps := roleManifest("translation", "chat_completions", true)
		caps.Authorized = authorized
		caps.Capabilities["conversation"] = RoleCapability{Supported: true, Available: true, Configurable: true, Endpoints: endpoints}
		next := *src
		next.SetCapabilities(caps)
		p.SetAccountSource(&next)
	}
	openConversation := func() model.BaseChatModel {
		t.Helper()
		factory, err := p.ModelBuilder(ctx, src.id, "", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		return factory("manager", "test-agent")
	}
	checkReply := func(client model.BaseChatModel, want string) {
		t.Helper()
		out, err := client.Generate(ctx, input)
		if err != nil || out == nil || out.Content != want {
			t.Fatalf("gateway reply = %v, err = %v, want %q", out, err, want)
		}
	}

	publish(true, "chat_completions")
	existing := openConversation()
	checkReply(existing, "chat")

	publish(true, "chat_completions", "responses")
	checkReply(openConversation(), "responses")
	checkReply(existing, "chat")
	translation, err := p.RoleModelBuilder(ctx, src.id, "server-model-2", "", nil, "translation")
	if err != nil {
		t.Fatal(err)
	}
	checkReply(translation("translator", "test-agent"), "chat")
	if out, err := p.CompleteFixedChat(ctx, src.id, "translation", "translate", "input", 100); err != nil || out != "chat" {
		t.Fatalf("fixed role reply = %q, err = %v", out, err)
	}

	publish(true, "responses", "chat_completions")
	checkReply(openConversation(), "responses")
	publish(true, "responses")
	checkReply(openConversation(), "responses")

	before := requests.Load()
	publish(true, "unknown")
	if err := p.ValidateConversationSelection(src.id, "server-model"); !errors.Is(err, ErrModelPolicy) {
		t.Fatalf("unknown endpoint selection = %v", err)
	}
	if _, err := existing.Generate(ctx, input); !errors.Is(err, ErrModelPolicy) {
		t.Fatalf("opened client with no implemented endpoint = %v", err)
	}
	publish(false, "responses")
	if err := p.ValidateConversationSelection(src.id, "server-model"); !errors.Is(err, ErrModelPolicy) {
		t.Fatalf("unauthorized endpoint selection = %v", err)
	}
	if _, err := existing.Generate(ctx, input); !errors.Is(err, ErrModelPolicy) {
		t.Fatalf("opened client with a revoked grant = %v", err)
	}
	if requests.Load() != before {
		t.Fatal("a refused endpoint reached the gateway")
	}
}

func TestEditableGatewayCanUseAPersonalFeatureModelOnlyWithItsGrant(t *testing.T) {
	cfg := config.Default()
	personal := cfg.Models.Default
	cfg.Models.Providers[0].BaseURL, cfg.Models.Providers[0].Model = "http://localhost:9/v1", "personal"
	pool := New(cfg)
	source := accountSourceFor(t, "https://gateway.invalid/v1", "private")
	source.SetCapabilities(roleManifest("translation", config.APIModeChatCompletions, true))
	pool.SetAccountSource(source)
	if !pool.AllowsPersonalFeatureModel(personal, "translation") ||
		pool.AllowsPersonalFeatureModel(personal, "conversation") ||
		pool.AllowsPersonalFeatureModel("", "translation") ||
		pool.AllowsPersonalFeatureModel("missing", "translation") ||
		pool.AllowsPersonalFeatureModel(source.id, "translation") {
		t.Fatal("personal feature routing did not honor the exact provider and capability")
	}
	source.SetCapabilities(roleManifest("translation", config.APIModeChatCompletions, false))
	pool.SetAccountSource(source)
	if pool.AllowsPersonalFeatureModel(personal, "translation") {
		t.Fatal("non-configurable role allowed a personal model")
	}
	source.SetModelPolicy(false, nil)
	source.SetCapabilities(roleManifest("translation", config.APIModeChatCompletions, true))
	pool.SetAccountSource(source)
	if pool.AllowsPersonalFeatureModel(personal, "translation") {
		t.Fatal("managed role allowed a personal model")
	}
}

func TestGatewayAutomaticTitleUsesItsPublishedModelAtHTTPBoundary(t *testing.T) {
	requests := make(chan string, 1)
	host := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body fixedChatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		requests <- body.Model
		fmt.Fprint(w, `{"choices":[{"message":{"role":"assistant","content":"Generated title"},"finish_reason":"stop"}]}`)
	}))
	defer host.Close()
	cfg := config.Default()
	pool := New(cfg)
	if _, _, _, err := pool.AutomaticTitleModel(); !errors.Is(err, ErrAccountLoginRequired) {
		t.Fatalf("unsigned Gateway naming: %v", err)
	}
	source := accountSourceFor(t, host.URL, "private")
	source.SetModelPolicy(true, map[string]string{"title": "gateway-title"})
	source.SetCapabilities(roleManifest("title", config.APIModeChatCompletions, true))
	pool.SetAccountSource(source)
	cfg.Models.Bindings = map[string]map[string]config.ModelRef{
		source.bindingScope(): {"title": {ProviderID: source.id, Model: "old-override"}},
	}
	ref, role, fixed, err := pool.AutomaticTitleModel()
	if err != nil || !fixed || role != "title" || ref.Model != "gateway-title" {
		t.Fatalf("ref=%+v role=%q fixed=%v err=%v", ref, role, fixed, err)
	}
	name, err := pool.CompleteFixedChat(t.Context(), ref.ProviderID, role, "Name this conversation", "Topic", 40)
	if err != nil || name != "Generated title" || <-requests != "gateway-title" {
		t.Fatalf("Gateway title=%q err=%v", name, err)
	}
	source.SetCapabilities(nil)
	pool.SetAccountSource(source)
	if _, _, _, err := pool.AutomaticTitleModel(); !errors.Is(err, ErrModelPolicy) {
		t.Fatalf("unavailable Gateway title capability: %v", err)
	}
}
