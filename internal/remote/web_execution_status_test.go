package remote

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestPhoneMetadataReportsThePCWebRouteWithoutExposingAccountAuthority(t *testing.T) {
	eng := testEngine(t)
	cfg := eng.Config()
	const origin = "https://account-web-status.invalid"
	const token = "private-pc-host-credential-must-stay-on-the-pc"
	const userID = "private-account-identity"
	const hostID = "private-host-identity"
	setMode := func(mode string) {
		t.Helper()
		if err := cfg.Update(func(next *config.Config) error {
			next.Tools.WebExecution = mode
			next.Remote.AuthMode = config.RemoteAuthAccount
			next.Remote.HubURL = origin
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	assertStatus := func(preference, execution, account string) {
		t.Helper()
		for _, op := range []string{OpMeta, OpCatalog} {
			// Extra credentials supplied by a phone cannot turn a signed-out PC
			// into an account-backed executor.
			var req Request
			if err := json.Unmarshal([]byte(`{"id":"web-status","op":"`+op+`","token":"phone-token-is-not-pc-authority"}`), &req); err != nil {
				t.Fatal(err)
			}
			reply := Handle(eng, config.RemoteConfig{}, req, "relay", "session")
			if !reply.OK {
				t.Fatalf("%s failed: %+v", op, reply)
			}
			raw, err := json.Marshal(reply)
			if err != nil {
				t.Fatal(err)
			}
			var wire struct {
				Status *struct {
					Preference   string `json:"preference"`
					Execution    string `json:"execution"`
					CloudAccount string `json:"cloud_account"`
				} `json:"web_execution_status"`
			}
			if err := json.Unmarshal(raw, &wire); err != nil {
				t.Fatal(err)
			}
			if wire.Status == nil {
				t.Fatalf("%s omitted the PC Web execution status", op)
			}
			if wire.Status.Preference != preference || wire.Status.Execution != execution || wire.Status.CloudAccount != account {
				t.Fatalf("%s reported the wrong PC route: %+v", op, wire.Status)
			}
			for _, secret := range []string{origin, token, userID, hostID, "phone-token-is-not-pc-authority", "host_token", "authorized", "cloud_available"} {
				if strings.Contains(string(raw), secret) {
					t.Fatalf("%s exposed private account data or claimed live server permission", op)
				}
			}
		}
	}
	setMode(config.WebExecutionAuto)
	assertStatus("auto", "local", "sign_in_required")
	path, err := cfg.StageRemoteToken(token, origin, "test-public-key")
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ActivateRemoteToken(path, config.RemoteCredential{Token: token, Origin: origin, Authorized: true, UserID: userID, HostID: hostID}); err != nil {
		t.Fatal(err)
	}
	assertStatus("auto", "cloud", "signed_in")
	setMode(config.WebExecutionLocal)
	assertStatus("local", "local", "signed_in")
	if err := cfg.Update(func(next *config.Config) error {
		next.Remote.HubURL = "https://another-web-status.invalid"
		next.Tools.WebExecution = config.WebExecutionAuto
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	assertStatus("auto", "local", "sign_in_required")
	setMode(config.WebExecutionCloud)
	if err := cfg.WriteHostToken("legacy-pc-token"); err != nil {
		t.Fatal(err)
	}
	assertStatus("cloud", "cloud", "sign_in_required")
	if err := os.WriteFile(filepath.Join(cfg.RemoteDir(), "host_token"), []byte(`{"schema":9,"token":"private-invalid-receipt"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	assertStatus("cloud", "cloud", "unavailable")
	setMode(config.WebExecutionAuto)
	assertStatus("auto", "cloud", "unavailable")
}
