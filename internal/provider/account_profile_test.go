package provider

import (
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestOfficialProfileReplacesTheCatalogUntilFullPresentation(t *testing.T) {
	cfg := config.Default()
	cfg.Models.Default = "own"
	cfg.Models.DefaultSource = config.DefaultSourceProvider
	cfg.Models.Providers = []config.Provider{{
		ID: "own", Label: "Own", BaseURL: "http://127.0.0.1:1/v1", Model: "own-model",
	}}
	pool := New(cfg)
	src, err := NewAccountProfile(config.AccountSourcePrefix+"0123456789abcdef0123456789abcdef", "http://127.0.0.1:9/v1", "token", AccountProfile{
		Label: "Partner model", Instruction: "added guidance", Tools: []string{"read"}, RestrictTools: true, ContextWindow: 1000,
	})
	if err != nil {
		t.Fatal(err)
	}
	pool.SetAccountSource(src)
	if !pool.OfficialOnly() || pool.DefaultProviderID() != src.ID() {
		t.Fatalf("official default = %s only=%v", pool.DefaultProviderID(), pool.OfficialOnly())
	}
	rows := pool.List()
	if len(rows) != 1 || rows[0].Model != config.OfficialModelAlias || rows[0].Label != "Partner model" || !rows[0].DefaultFollowsSetting {
		t.Fatalf("official rows: %+v", rows)
	}
	resolved, err := pool.ResolveModel("own", "own-model")
	if err != nil || resolved.ID != src.ID() || resolved.Model != config.OfficialModelAlias {
		t.Fatalf("resolve = %+v %v", resolved, err)
	}
	if note := pool.AccountInstruction("own"); note != "added guidance" {
		t.Fatalf("official instruction = %q", note)
	}
	restrict, names := pool.AccountToolPolicy("own")
	if !restrict || len(names) != 1 || names[0] != "read" {
		t.Fatalf("policy = %v %v", restrict, names)
	}

	cfg.Models.AccountPresentation = config.AccountPresentationFull
	if pool.OfficialOnly() || pool.DefaultProviderID() != "own" {
		t.Fatalf("full mode still hid own providers: default=%s only=%v", pool.DefaultProviderID(), pool.OfficialOnly())
	}
	rows = pool.List()
	if len(rows) != 2 || rows[0].Model != config.OfficialModelAlias || rows[1].Model != "own-model" {
		t.Fatalf("full rows: %+v", rows)
	}
	own, err := pool.ResolveModel("own", "own-model")
	if err != nil || own.ID != "own" || own.Model != "own-model" {
		t.Fatalf("own resolve = %+v %v", own, err)
	}
	if pool.AccountInstruction("own") != "" {
		t.Fatal("a personal turn received the official instruction")
	}
	account, err := pool.ResolveModel(src.ID(), "vendor-secret-model")
	if err != nil || account.Model != config.OfficialModelAlias {
		t.Fatalf("account resolve kept a concrete name: %+v %v", account, err)
	}
	if pool.AccountInstruction(src.ID()) != "added guidance" {
		t.Fatal("the official source lost its instruction in full mode")
	}
	cfg.Models.DefaultSource = config.DefaultSourceAccount
	if pool.AccountInstruction("") != "added guidance" || pool.AccountInstruction("other") != "" {
		t.Fatal("an empty account selection did not follow the default source")
	}
	trace, ok := pool.AccountProfileTrace(src.ID())
	if !ok || trace.Label != "Partner model" || trace.Alias != config.OfficialModelAlias || trace.InstructionBytes != len("added guidance") || len(trace.Tools) != 1 {
		t.Fatalf("trace = %+v %v", trace, ok)
	}
	if _, ok := pool.AccountProfileTrace("own"); ok {
		t.Fatal("a personal turn recorded the official profile")
	}
}

func TestProfileRefreshNoticesAnInstructionChange(t *testing.T) {
	first, err := NewAccountProfile(config.AccountSourcePrefix+"0123456789abcdef0123456789abcdef", "http://127.0.0.1:9/v1", "token", AccountProfile{Label: "Partner model", Instruction: "one"})
	if err != nil {
		t.Fatal(err)
	}
	same, err := NewAccountProfile(config.AccountSourcePrefix+"0123456789abcdef0123456789abcdef", "http://127.0.0.1:9/v1", "token", AccountProfile{Label: "Partner model", Instruction: "one"})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := NewAccountProfile(config.AccountSourcePrefix+"0123456789abcdef0123456789abcdef", "http://127.0.0.1:9/v1", "token", AccountProfile{Label: "Partner model", Instruction: "two", Tools: []string{"exec"}, RestrictTools: true})
	if err != nil {
		t.Fatal(err)
	}
	if !first.equal(same) || first.equal(changed) {
		t.Fatal("profile equality did not follow the agreement")
	}
	if !first.Profiled() || first.Instruction() != "one" {
		t.Fatal("the stored agreement was not readable")
	}
	trace, ok := first.profileTrace()
	if !ok || trace.RestrictTools || len(trace.Tools) != 0 {
		t.Fatalf("an absent allowlist was reported: %+v %v", trace, ok)
	}
	left, err := NewAccountProfile(first.ID(), "http://127.0.0.1:9/v1", "token", AccountProfile{Label: "Partner model", Instruction: "one", Tools: []string{"read", "exec"}, RestrictTools: true})
	if err != nil {
		t.Fatal(err)
	}
	right, err := NewAccountProfile(first.ID(), "http://127.0.0.1:9/v1", "token", AccountProfile{Label: "Partner model", Instruction: "one", Tools: []string{"read", "write"}, RestrictTools: true})
	if err != nil {
		t.Fatal(err)
	}
	if left.sameProfile(right) {
		t.Fatal("a different allowlist compared equal")
	}
	plain, err := NewAccountSource(first.ID(), "Partner model", "http://127.0.0.1:9/v1", "token", Catalog{Names: []string{config.OfficialModelAlias}})
	if err != nil {
		t.Fatal(err)
	}
	if plain.Profiled() || plain.Instruction() != "" || plain.sameProfile(first) || !plain.sameProfile(plain) {
		t.Fatal("an ordinary catalog grew an official agreement")
	}
	if restrict, names := plain.ToolAllow(); restrict || names != nil {
		t.Fatalf("ordinary allowlist = %v %v", restrict, names)
	}
	if restrict, names := first.ToolAllow(); restrict || names != nil {
		t.Fatalf("absent allowlist = %v %v", restrict, names)
	}
	var none *AccountSource
	if none.Profiled() || none.Instruction() != "" {
		t.Fatal("a missing source answered")
	}
	if restrict, names := none.ToolAllow(); restrict || names != nil {
		t.Fatalf("nil allowlist = %v %v", restrict, names)
	}
	if _, ok := none.profileTrace(); ok {
		t.Fatal("a missing source produced a trace")
	}
	if _, err := NewAccountProfile("provider", "http://127.0.0.1:9/v1", "token", AccountProfile{Label: "Partner model"}); err == nil {
		t.Fatal("a profile accepted an ordinary provider id")
	}
	var pool *Pool
	if pool.AccountInstruction("own") != "" {
		t.Fatal("a missing pool returned guidance")
	}
	if New(config.Default()).AccountInstruction("") != "" {
		t.Fatal("a pool without an account returned guidance")
	}
	if restrict, names := pool.AccountToolPolicy("own"); restrict || names != nil {
		t.Fatalf("nil policy = %v %v", restrict, names)
	}
}
