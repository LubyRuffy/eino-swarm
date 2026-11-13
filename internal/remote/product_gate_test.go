package remote

import (
	"encoding/json"
	"testing"
)

func TestInstallAllowsKeepsFOFASignedOutAndLocalOpen(t *testing.T) {
	work, models := installAllows(false, true, AccountSignedOut)
	if work || models {
		t.Fatalf("signed-out fofa work=%v models=%v", work, models)
	}
	work, models = installAllows(false, true, AccountPending)
	if !work || models {
		t.Fatalf("signed-in fofa work=%v models=%v", work, models)
	}
	work, models = installAllows(true, true, AccountSignedOut)
	if !work || models {
		t.Fatalf("mock fofa work=%v models=%v", work, models)
	}
	work, models = installAllows(false, false, AccountSignedOut)
	if !work || !models {
		t.Fatalf("local work=%v models=%v", work, models)
	}
}

func TestLinkPumpRefusesFOFAWorkAndModelChanges(t *testing.T) {
	var got Response
	p := &LinkPump{
		path: "relay", sessionID: "s",
		allow: func() (bool, bool) { return false, false },
		send: func(raw []byte) error {
			return json.Unmarshal(raw, &got)
		},
	}
	p.Dispatch([]byte(`{"id":"1","op":"start","text":"go"}`))
	if got.OK || got.Code != "login_required" {
		t.Fatalf("start = %+v", got)
	}
	p.Dispatch([]byte(`{"id":"2","op":"tune"}`))
	if got.OK || got.Code != "custom_models_disabled" {
		t.Fatalf("tune = %+v", got)
	}
	if !p.permit(Request{ID: "3", Op: OpStop}) {
		t.Fatal("stop was treated as new work")
	}
}
