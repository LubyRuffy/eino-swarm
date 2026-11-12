package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestAccountInstructionPrecedesPersonality(t *testing.T) {
	got := applyAccountInstruction("## Personality\n\nBe brief.\n", "  profile guidance marker 1842 ")
	if strings.Index(got, "## Account") < 0 || strings.Index(got, "## Account") > strings.Index(got, "## Personality") {
		t.Fatalf("instruction order:\n%s", got)
	}
	if applyAccountInstruction("tail", " ") != "tail" || AccountInstructionPrompt(" ") != "" {
		t.Fatal("a blank account note changed the prompt")
	}
}

func TestOfficialProfileSendsTheAliasAndLeavesTheStoredProvider(t *testing.T) {
	const marker = "profile guidance marker 1842"
	var mu sync.Mutex
	var bodies [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, append([]byte(nil), raw...))
		mu.Unlock()
		writeCheckedCompletion(w, raw)
	}))
	defer srv.Close()

	e := newAccountEngine(t, srv.URL+"/v1", "vendor-secret-model")
	if err := e.cfg.Update(func(c *config.Config) error {
		c.Models.Default = "own"
		c.Models.DefaultSource = config.DefaultSourceProvider
		c.Models.Providers = []config.Provider{{
			ID: "own", BaseURL: srv.URL + "/v1", Model: "own-model", APIKey: "local-key",
		}}
		c.Tools.Disabled = []string{"read"}
		c.MCP.Servers = []config.MCPServer{{
			ID: "external", Transport: "streamable_http", URL: "http://127.0.0.1:1", Enabled: true, TimeoutSeconds: 1,
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e.pool.SetAccountSource(nil)
	th, err := e.CreateThread("", "own", "")
	if err != nil {
		t.Fatal(err)
	}
	src, err := provider.NewAccountProfile(accountCatalogID, srv.URL+"/v1", "token", provider.AccountProfile{
		Label: "Partner model", Instruction: marker, Tools: []string{"read", "exec"}, RestrictTools: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	e.pool.SetAccountSource(src)

	first, err := e.StartTurn(th.ID, "Continue the current task.")
	if err != nil {
		t.Fatal(err)
	}
	waitTurn(t, e, first.ID)
	official := snapshotBodies(&mu, &bodies)
	if len(official) == 0 || !strings.Contains(string(official[0]), `"model":"account"`) && !strings.Contains(string(official[0]), `"model": "account"`) {
		t.Fatalf("official request did not send the alias: %s", official[0])
	}
	assertProfileRequest(t, official[0], marker, true)
	stored, err := e.store.GetThread(th.ID)
	if err != nil || stored.ProviderID != "own" || stored.Model != "" {
		t.Fatalf("official send rewrote the conversation: %+v %v", stored, err)
	}
	assertProfileEvent(t, e, th.ID, first.ID, marker)

	if err := e.cfg.Update(func(c *config.Config) error {
		c.Models.AccountPresentation = config.AccountPresentationFull
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	second, err := e.StartTurn(th.ID, "Continue the current task.")
	if err != nil {
		t.Fatal(err)
	}
	waitTurn(t, e, second.ID)
	all := snapshotBodies(&mu, &bodies)
	if len(all) <= len(official) {
		t.Fatal("the personal turn made no request")
	}
	personal := all[len(official)]
	if strings.Contains(string(personal), `"model":"account"`) || strings.Contains(string(personal), `"model": "account"`) || !strings.Contains(string(personal), "own-model") {
		t.Fatalf("full mode personal turn used the official alias: %s", personal)
	}
	assertProfileRequest(t, personal, marker, false)
	events, err := e.store.ListEventsByTurn(th.ID, second.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range events {
		if ev.Kind == KindAccountProfile {
			t.Fatalf("a personal turn recorded the official profile: %s", ev.Text)
		}
	}
}

func TestOfficialProfileDoesNotUseTheSavedNuwaPair(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		writeCheckedCompletion(w, raw)
	}))
	defer srv.Close()
	e := newAccountEngine(t, srv.URL+"/v1", "vendor-secret-model")
	if err := e.cfg.Update(func(c *config.Config) error {
		c.Models.Providers = []config.Provider{{
			ID: "own", BaseURL: srv.URL + "/v1", Model: "planner-model",
			Catalog: []string{"planner-model", "executor-model"},
		}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	e.pool.SetAccountSource(nil)
	project, err := e.CreateProject("Chosen models", "", t.TempDir(), false, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.SaveNuwaModelPair(project.ID, NuwaModelPair{
		PlannerProviderID: "own", PlannerModel: "planner-model",
		ExecutorProviderID: "own", ExecutorModel: "executor-model",
	}); err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("", "own", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	src, err := provider.NewAccountProfile(accountCatalogID, srv.URL+"/v1", "token", provider.AccountProfile{Label: "Partner model"})
	if err != nil {
		t.Fatal(err)
	}
	e.pool.SetAccountSource(src)
	turn, err := e.StartTurn(th.ID, "Continue the current task.")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Interrupt(th.ID) })
	stored, err := e.store.GetTurn(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.Model != config.OfficialModelAlias || stored.ProviderID != accountCatalogID || stored.NuwaExecutorModel != config.OfficialModelAlias {
		t.Fatalf("official nuwa turn used the saved pair: %+v", stored)
	}
}

func assertProfileRequest(t *testing.T, raw []byte, marker string, official bool) {
	t.Helper()
	text := string(raw)
	if strings.Contains(text, "vendor-secret-model") {
		t.Fatalf("request leaked the vendor model id: %s", text)
	}
	names := profileRequestToolNames(t, raw)
	has := func(name string) bool {
		for _, got := range names {
			if got == name {
				return true
			}
		}
		return false
	}
	if !has("spawn_agent") || !has("exec") || has("read") {
		t.Fatalf("tools = %v", names)
	}
	if official {
		if !strings.Contains(text, marker) || has("write") {
			t.Fatalf("official request tools=%v body missing guidance or still offering write", names)
		}
		return
	}
	if strings.Contains(text, marker) || !has("write") {
		t.Fatalf("personal request kept the official agreement: tools=%v", names)
	}
}

func profileRequestToolNames(t *testing.T, raw []byte) []string {
	t.Helper()
	var body struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
			Name string `json:"name"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatalf("request is not JSON: %v\n%s", err, raw)
	}
	var names []string
	for _, tool := range body.Tools {
		name := tool.Function.Name
		if name == "" {
			name = tool.Name
		}
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func assertProfileEvent(t *testing.T, e *Engine, threadID, turnID, marker string) {
	t.Helper()
	events, err := e.store.ListEventsByTurn(threadID, turnID)
	if err != nil {
		t.Fatal(err)
	}
	var found *store.Event
	for i := range events {
		if events[i].Kind == KindAccountProfile {
			if found != nil {
				t.Fatal("the profile was recorded twice")
			}
			found = &events[i]
		}
	}
	if found == nil || strings.Contains(found.Text, marker) {
		t.Fatalf("profile event = %+v", found)
	}
	var trace provider.ProfileTrace
	if err := json.Unmarshal([]byte(found.Text), &trace); err != nil {
		t.Fatal(err)
	}
	if trace.Label != "Partner model" || trace.Alias != config.OfficialModelAlias || trace.InstructionBytes != len(marker) || len(trace.Tools) != 2 {
		t.Fatalf("trace = %+v", trace)
	}
}

func snapshotBodies(mu *sync.Mutex, bodies *[][]byte) [][]byte {
	mu.Lock()
	defer mu.Unlock()
	out := make([][]byte, len(*bodies))
	for i, body := range *bodies {
		out[i] = append([]byte(nil), body...)
	}
	return out
}

func writeCheckedCompletion(w http.ResponseWriter, raw []byte) {
	var body struct {
		Stream bool `json:"stream"`
	}
	_ = json.Unmarshal(raw, &body)
	content := "Checked."
	if strings.Contains(string(raw), "Schedule answer claims") {
		content = `{"claims":[]}`
	}
	if body.Stream {
		w.Header().Set("Content-Type", "text/event-stream")
		rawContent, _ := json.Marshal(content)
		fmt.Fprintf(w, "data: {\"id\":\"c\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":%s},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", rawContent)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	rawContent, _ := json.Marshal(content)
	fmt.Fprintf(w, `{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":%s},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`, rawContent)
}
