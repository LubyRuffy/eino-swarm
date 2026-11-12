package remote

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

func TestProfileSkipsTheCatalogAndPublishesOneAlias(t *testing.T) {
	g := newAccountGateway(t)
	g.catalog = func(w http.ResponseWriter, r *http.Request) {
		t.Error("a profile must not fetch the vendor catalog")
		http.Error(w, "catalog", http.StatusInternalServerError)
	}
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		g.writeBootstrap(rec, g.server.URL, true)
		var body map[string]map[string]map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		body["data"]["models"]["profile"] = map[string]any{
			"label": "Partner model", "instruction": "profile guidance marker 1842",
			"tools": []string{"read", "exec"}, "context_window": 32000,
		}
		_ = json.NewEncoder(w).Encode(body)
	}
	am, pool, cfg := accountModelsFixture(t, g)
	st := am.Refresh(context.Background())
	if st.State != AccountReady || st.Models != 1 || st.Label != "Partner model" || st.Profile == nil || !st.Profile.Instruction || !st.Profile.RestrictTools || len(st.Profile.Tools) != 2 {
		t.Fatalf("state: %+v", st)
	}
	if st.Profile.Presentation != config.AccountPresentationOfficial {
		t.Fatalf("presentation %q", st.Profile.Presentation)
	}
	raw, _ := json.Marshal(st)
	if strings.Contains(string(raw), "profile guidance marker 1842") || strings.Contains(string(raw), "vendor") || strings.Contains(string(raw), accountModelsToken) {
		t.Fatalf("state leaked the instruction or a secret: %s", raw)
	}
	rows := pool.List()
	if len(rows) != 1 || rows[0].Model != config.OfficialModelAlias || rows[0].Label != "Partner model" || rows[0].ContextWindow != 32000 {
		t.Fatalf("rows: %+v", rows)
	}
	if !pool.OfficialOnly() {
		t.Fatal("official presentation still listed other models")
	}
	resolved, err := pool.ResolveModel("own", "own-model")
	if err != nil || resolved.Model != config.OfficialModelAlias || resolved.ID != accountModelsSourceID {
		t.Fatalf("official resolve = %+v %v", resolved, err)
	}
	if err := cfg.Update(func(next *config.Config) error {
		next.Models.AccountPresentation = config.AccountPresentationFull
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if pool.OfficialOnly() || am.State().Profile.Presentation != config.AccountPresentationFull {
		t.Fatalf("full presentation did not take effect: %+v", am.State().Profile)
	}
	personal, err := pool.ResolveModel("default", "kept-model")
	if err != nil || personal.ID == accountModelsSourceID {
		t.Fatalf("full mode redirected a personal provider: %+v %v", personal, err)
	}
	signOutAccount(t, cfg)
	st = am.Refresh(context.Background())
	if st.State != AccountSignedOut || st.Profile != nil || pool.AccountSourceID() != "" || pool.OfficialOnly() {
		t.Fatalf("sign-out kept the profile: %+v source=%s", st, pool.AccountSourceID())
	}
}

func TestInvalidProfilePublishesNothing(t *testing.T) {
	long := strings.Repeat("名", 65)
	cases := map[string]any{
		"empty label":      map[string]any{"label": " "},
		"long label":       map[string]any{"label": long},
		"long instruction": map[string]any{"label": "Partner model", "instruction": strings.Repeat("x", 8193)},
		"unknown tool":     map[string]any{"label": "Partner model", "tools": []string{"spawn_agent"}},
		"duplicate tool":   map[string]any{"label": "Partner model", "tools": []string{"read", "read"}},
		"null tools":       json.RawMessage(`{"label":"Partner model","tools":null}`),
		"zero context":     map[string]any{"label": "Partner model", "context_window": 0},
		"negative context": map[string]any{"label": "Partner model", "context_window": -1},
	}
	for name, profile := range cases {
		t.Run(name, func(t *testing.T) {
			g := newAccountGateway(t)
			g.catalog = func(http.ResponseWriter, *http.Request) { t.Error("invalid profile fetched the catalog") }
			g.bootstrap = func(w http.ResponseWriter, r *http.Request) {
				rec := httptest.NewRecorder()
				g.writeBootstrap(rec, g.server.URL, true)
				var body map[string]map[string]map[string]any
				_ = json.Unmarshal(rec.Body.Bytes(), &body)
				body["data"]["models"]["profile"] = profile
				_ = json.NewEncoder(w).Encode(body)
			}
			am, pool, _ := accountModelsFixture(t, g)
			st := am.Refresh(context.Background())
			if st.State != AccountUnavailable || st.Reason != AccountReasonUnsafe || pool.AccountSourceID() != "" || st.Profile != nil {
				t.Fatalf("invalid profile published: %+v source=%s", st, pool.AccountSourceID())
			}
		})
	}
}

func TestEmptyToolListRestrictsWithoutNamingAVendorModel(t *testing.T) {
	g := newAccountGateway(t)
	g.bootstrap = func(w http.ResponseWriter, r *http.Request) {
		rec := httptest.NewRecorder()
		g.writeBootstrap(rec, g.server.URL, true)
		var body map[string]map[string]map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		body["data"]["models"]["profile"] = map[string]any{"label": "Partner model", "tools": []string{}}
		_ = json.NewEncoder(w).Encode(body)
	}
	am, pool, _ := accountModelsFixture(t, g)
	st := am.Refresh(context.Background())
	if st.State != AccountReady || st.Profile == nil || !st.Profile.RestrictTools || len(st.Profile.Tools) != 0 {
		t.Fatalf("empty allowlist: %+v", st)
	}
	restrict, names := pool.AccountToolPolicy("")
	if !restrict || len(names) != 0 {
		t.Fatalf("policy = %v %v", restrict, names)
	}
	if strings.Contains(fmt.Sprint(pool.List()), "server-model") {
		t.Fatalf("catalog names leaked into the list: %+v", pool.List())
	}
}

func TestParseAccountProfileRejectsABrokenBody(t *testing.T) {
	absent, err := parseAccountProfile(nil)
	if absent != nil || err != nil {
		t.Fatalf("missing profile = %+v %v", absent, err)
	}
	absent, err = parseAccountProfile([]byte("null"))
	if absent != nil || err != nil {
		t.Fatalf("null profile = %+v %v", absent, err)
	}
	got, err := parseAccountProfile([]byte(`{"label":"Partner model"}`))
	if err != nil || got == nil || got.RestrictTools || got.Tools != nil {
		t.Fatalf("a name without a tool list was restricted: %+v %v", got, err)
	}
	got, err = parseAccountProfile([]byte(`{"label":" Partner model ","tools":[" read "]}`))
	if err != nil || got == nil || got.Label != "Partner model" || !got.RestrictTools || len(got.Tools) != 1 || got.Tools[0] != "read" {
		t.Fatalf("trimmed profile = %+v %v", got, err)
	}
	for _, raw := range []string{`[]`, `{"label":"   "}`, `{"label":"Partner model","tools":""}`, `{"label":"Partner model","tools":[" "]}`} {
		if _, err := parseAccountProfile([]byte(raw)); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
}

func TestProfileStringHidesTheInstructionAndCredential(t *testing.T) {
	src, err := provider.NewAccountProfile(accountModelsSourceID, "https://account.invalid/v1", "secret-token", provider.AccountProfile{
		Label: "Partner model", Instruction: "profile guidance marker 1842", Tools: []string{"read"}, RestrictTools: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	printed := fmt.Sprintf("%v %#v %+v", src, src, *src)
	if strings.Contains(printed, "secret-token") || strings.Contains(printed, "profile guidance marker 1842") {
		t.Fatalf("printable profile leaked: %s", printed)
	}
}
