package remote

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestTimeoutSettingsAreReachableWithoutReturningHostSecrets(t *testing.T) {
	e := testEngine(t)
	resp := Handle(e, config.RemoteConfig{}, Request{ID: "read", Op: "get_timeouts"}, "relay", "test")
	if !resp.OK {
		t.Fatalf("phone cannot read timeout settings: code=%s", resp.Code)
	}
}

func timeoutRequest(t *testing.T, raw string) Request {
	t.Helper()
	var req Request
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatal(err)
	}
	return req
}

func TestTimeoutSettingsValidateAtomicPatchesAndKeepSecretsOnPC(t *testing.T) {
	e := catalogEngine(t, config.Provider{ID: "p", Label: "Provider", BaseURL: "http://private.invalid", APIKey: "secret-key", Model: "model", TimeoutSeconds: 10})
	get := Handle(e, config.RemoteConfig{}, Request{ID: "read", Op: OpGetTimeouts}, "relay", "test")
	v := get.TimeoutSettings
	if !get.OK || v == nil || len(v.Providers) != 1 || v.Providers[0].FirstByteTimeoutSeconds != 30 || v.Providers[0].EffectiveFirstByteTimeoutSeconds != 30 {
		t.Fatalf("get=%+v", get)
	}
	raw, _ := json.Marshal(get)
	for _, secret := range []string{"private.invalid", "secret-key", "base_url", "api_key"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("secret leaked")
		}
	}
	send := func(raw string) Response {
		return Handle(e, config.RemoteConfig{}, timeoutRequest(t, raw), "relay", "test")
	}
	resp := send(`{"op":"set_timeouts","timeout_settings":{"agent_timeout_seconds":52,"providers":[{"id":"p","timeout_seconds":20,"first_byte_timeout_seconds":12}]}}`)
	if !resp.OK || resp.TimeoutSettings.AgentTimeoutSeconds != 52 || resp.TimeoutSettings.Providers[0].EffectiveFirstByteTimeoutSeconds != 12 {
		t.Fatalf("save=%+v", resp)
	}
	before, _ := os.ReadFile(e.Config().Path())
	for _, bad := range []string{
		`{}`, `null`, `[]`, `{"agent_timeout_seconds":0}`, `{"agent_timeout_seconds":-1}`, `{"agent_timeout_seconds":1.5}`, `{"agent_timeout_seconds":9223372037}`, `{"agent_timeout_seconds":9223372036854775808}`,
		`{"label":"readonly"}`, `{"providers":[{"id":"p","label":"readonly","timeout_seconds":2}]}`, `{"providers":[{"id":"p","effective_first_byte_timeout_seconds":2}]}`,
		`{"providers":[{"id":"unknown","timeout_seconds":2}]}`, `{"providers":[{"id":"p"}]}`, `{"providers":[{"id":" p","timeout_seconds":2}]}`, `{"providers":[{"id":"p","timeout_seconds":0}]}`, `{"providers":[{"id":"p","first_byte_timeout_seconds":-1}]}`,
		`{"providers":[{"id":"p","timeout_seconds":2},{"id":"p","timeout_seconds":3}]}`, `{"agent_timeout_seconds":9,"providers":[{"id":"unknown","timeout_seconds":2}]}`,
	} {
		r := send(`{"op":"set_timeouts","timeout_settings":` + bad + `}`)
		if r.OK || r.Code != "bad_request" {
			t.Fatalf("accepted %s: %+v", bad, r)
		}
	}
	for _, bad := range []string{`{"op":"set_timeouts","label":"bad","timeout_settings":{"agent_timeout_seconds":5}}`, `{"op":"get_timeouts","timeout_settings":{}}`, `{"op":"set_timeouts"}`} {
		r := send(bad)
		if r.Code != "bad_request" {
			t.Fatalf("bad envelope=%+v", r)
		}
	}
	after, _ := os.ReadFile(e.Config().Path())
	if !bytes.Equal(before, after) || e.Config().Timeouts().AgentTimeoutSeconds != 52 {
		t.Fatal("rejected patch changed state")
	}
	if send(`{"v":999,"op":"set_timeouts"}`).Code != "bad_version" {
		t.Fatal("version bypass")
	}
}

func TestTimeoutSettingsFailClosedBeforeSaveAndRetainNormalizedDefault(t *testing.T) {
	e := catalogEngine(t, config.Provider{ID: "p", Model: "m", TimeoutSeconds: 60})
	call := func(raw string) Response {
		return Handle(e, config.RemoteConfig{}, timeoutRequest(t, raw), "relay", "test")
	}
	if r := call(`{"op":"set_timeouts","timeout_settings":{"agent_timeout_seconds":63}}`); !r.OK {
		t.Fatal(r)
	}
	p, _ := e.Config().Provider("p")
	if p.FirstByteTimeoutSeconds != int(config.DefaultFirstByteTimeout.Seconds()) {
		t.Fatal("normalized main default was changed")
	}
	before, _ := os.ReadFile(e.Config().Path())
	old := maxCatalogFrame
	maxCatalogFrame = 1
	if r := call(`{"op":"set_timeouts","timeout_settings":{"agent_timeout_seconds":5}}`); r.Code != "too_large" {
		t.Fatal(r)
	}
	if r := call(`{"op":"get_timeouts"}`); r.Code != "too_large" {
		t.Fatal(r)
	}
	maxCatalogFrame = old
	if err := os.Mkdir(e.Config().Path()+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	r := call(`{"op":"set_timeouts","timeout_settings":{"agent_timeout_seconds":5}}`)
	if r.OK || r.Code != "save_failed" || strings.Contains(r.Error, e.Config().DataDir()) {
		t.Fatalf("unsafe savefailure=%+v", r)
	}
	after, _ := os.ReadFile(e.Config().Path())
	if !bytes.Equal(before, after) || e.Config().Timeouts().AgentTimeoutSeconds != 63 {
		t.Fatal("failure advanced disk/memory")
	}
	var ignored Request
	if json.Unmarshal([]byte(`bad`), &ignored) == nil {
		t.Fatal("invalid wire accepted")
	}
	if r := call(`{"op":"catalog","unrelated_legacy_field":true}`); !r.OK {
		t.Fatal("legacy op made strict")
	}
}

func TestTimeoutReplyRetainsItsBudgetedSnapshotAfterAnotherSettingsSave(t *testing.T) {
	e := catalogEngine(t, config.Provider{ID: "p", Label: "small", Model: "m", TimeoutSeconds: 60})
	get := Handle(e, config.RemoteConfig{}, Request{ID: "read", Op: OpGetTimeouts}, "relay", "test")
	if !get.OK || !timeoutReplyFits(get) {
		t.Fatal("initial reply does not fit")
	}
	before, _ := json.Marshal(get)
	if err := e.Config().Update(func(c *config.Config) error { c.Models.Providers[0].Label = strings.Repeat("large", 100); return nil }); err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(get)
	if !bytes.Equal(before, after) || get.TimeoutSettings.Providers[0].Label != "small" {
		t.Fatal("checked DTO changed with live config")
	}
}

func TestTimeoutSettingsRoundTripOverEncryptedPairlinkKeepsConnectionUsable(t *testing.T) {
	e := catalogEngine(t, config.Provider{ID: "p", Label: "Provider", Model: "m", TimeoutSeconds: 60, BaseURL: "http://private.invalid", APIKey: "private-key"})
	if err := e.UpdateConfig(func(c *config.Config) error { c.Swarm.AgentTimeoutSeconds = 600; return nil }); err != nil {
		t.Fatal(err)
	}
	r := newFollowupRelay(t, e)
	first := r.rpc(t, Request{ID: "read", Op: OpGetTimeouts})
	if !first.OK || first.TimeoutSettings.AgentTimeoutSeconds != 600 || first.TimeoutSettings.Providers[0].FirstByteTimeoutSeconds != 30 {
		t.Fatalf("wire read=%+v", first)
	}
	saved := r.rpc(t, Request{ID: "save", Op: OpSetTimeouts, TimeoutSettings: json.RawMessage(`{"agent_timeout_seconds":3600,"providers":[{"id":"p","first_byte_timeout_seconds":45}]}`)})
	if !saved.OK || saved.TimeoutSettings.AgentTimeoutSeconds != 3600 || saved.TimeoutSettings.Providers[0].FirstByteTimeoutSeconds != 45 {
		t.Fatalf("wire save=%+v", saved)
	}
	readback := r.rpc(t, Request{ID: "readback", Op: OpGetTimeouts})
	if !readback.OK || readback.TimeoutSettings.Providers[0].EffectiveFirstByteTimeoutSeconds != 45 {
		t.Fatal("wire readback")
	}
	if list := r.rpc(t, Request{ID: "still-connected", Op: OpList}); !list.OK {
		t.Fatal("settings dropped encrypted link")
	}
	for _, resp := range []Response{first, saved, readback} {
		raw, _ := json.Marshal(resp)
		if strings.Contains(string(raw), "private-key") || strings.Contains(string(raw), "private.invalid") {
			t.Fatal("wire leaked host secret")
		}
	}
	disk, err := config.Load(e.Config().DataDir())
	if err != nil || disk.Swarm.AgentTimeoutSeconds != 3600 || disk.Models.Providers[0].FirstByteTimeoutSeconds != 45 {
		t.Fatal("wire save not persisted")
	}
}
