package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

func validFixture() *fixture {
	body := "result"
	return &fixture{SchemaVersion: 1, Instruction: "test", Workers: []workerFixture{{AgentID: "w", Role: "r", Generation: 1, Status: "done", Result: &body}}, Rounds: []struct {
		User string `json:"user"`
	}{{User: "inspect"}}}
}

func TestFixtureValidationPreservesSelectedFileBytesAndRejectsMalformedIdentity(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "case.json")
	if _, err := loadFixture(file); err == nil {
		t.Fatal("missing fixture accepted")
	}
	if err := os.WriteFile(file, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFixture(file); err == nil {
		t.Fatal("malformed fixture accepted")
	}
	valid, _ := json.Marshal(validFixture())
	for _, body := range [][]byte{append(append([]byte(nil), valid...), []byte(" {}")...), []byte(strings.Replace(string(valid), `"schema_version":1`, `"files":{},"schema_version":1`, 1))} {
		if err := os.WriteFile(file, body, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadFixture(file); err == nil {
			t.Fatal("trailing or removed Files interface was accepted")
		}
	}
	for _, mutate := range []func(*fixture){
		func(f *fixture) { f.SchemaVersion = 2 }, func(f *fixture) { f.Workers[0].Generation = 0 },
		func(f *fixture) { f.Workers = append(f.Workers, f.Workers[0]) }, func(f *fixture) { f.Workers[0].Status = "running" },
		func(f *fixture) { f.Workers[0].Status = "failed" }, func(f *fixture) { f.Workers[0].Error = "unexpected" },
		func(f *fixture) {
			f.Messages = append(f.Messages, struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			}{Role: "system"})
		},
		func(f *fixture) { f.Rounds[0].User = "" },
	} {
		f := validFixture()
		mutate(f)
		data, _ := json.Marshal(f)
		if err := os.WriteFile(file, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := loadFixture(file); err == nil {
			t.Fatal("invalid fixture accepted")
		}
	}
	name := "body with trailing space "
	wanted := "  -73\nonly if denied=false\n"
	if err := privateFile(filepath.Join(root, name), []byte(wanted)); err != nil {
		t.Fatal(err)
	}
	f := validFixture()
	f.Workers[0].Result = nil
	f.Workers[0].ResultFile = &name
	data, _ := json.Marshal(f)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadFixture(file)
	if err != nil {
		t.Fatal(err)
	}
	if *loaded.Workers[0].Result != wanted {
		t.Fatal("selected bytes changed")
	}
	if err := os.Remove(filepath.Join(root, name)); err != nil {
		t.Fatal(err)
	}
	if _, err := loadFixture(file); err == nil {
		t.Fatal("deleted selected file accepted")
	}
	reg := swarm.NewRegistry()
	defer reg.Close()
	f = validFixture()
	f.Workers[0].Status = "failed"
	f.Workers[0].Error = "actual failure"
	if err := plantWorkers(reg, f.Workers); err != nil {
		t.Fatal(err)
	}
	reg.Close()
	if err := plantWorkers(reg, f.Workers); err == nil {
		t.Fatal("closed registry accepted a worker")
	}
	if got := initialMessages(&fixture{Messages: []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{{Role: "user", Content: "first"}}}); len(got) != 1 || got[0].Content != "first" {
		t.Fatal("initial messages lost")
	}
}

type simpleModel struct{ failure error }

func (m *simpleModel) Generate(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return schema.AssistantMessage("finished", nil), m.failure
}
func (m *simpleModel) Stream(ctx context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if m.failure != nil {
		return nil, m.failure
	}
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("finished", nil)}), nil
}

type failingTool struct{ infoErr, runErr error }

func (t *failingTool) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "read"}, t.infoErr
}
func (t *failingTool) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return "body", t.runErr
}

func TestModelBoundaryGenerateStreamAndEvidenceFailures(t *testing.T) {
	log := &audit{}
	m := &contextModel{inner: &simpleModel{}, arm: "a", audit: log}
	if _, err := m.Generate(context.Background(), []*schema.Message{schema.UserMessage("input")}); err != nil {
		t.Fatal(err)
	}
	bad := []*schema.Message{{Extra: map[string]any{"invalid": func() {}}}}
	if _, err := m.Generate(context.Background(), bad); err == nil {
		t.Fatal("invalid evidence reached model")
	}
	if _, err := m.Stream(context.Background(), bad); err == nil {
		t.Fatal("invalid evidence reached stream")
	}
	log.recordCall(provider.CallRecord{Backoff: &provider.BackoffRecord{}})
	log.recordCall(provider.CallRecord{AgentID: "manager", Err: errors.New("failure")})
	if len(log.Calls) != 1 || log.Calls[0].Error != "failure" {
		t.Fatal("backoff counted as request or failure lost")
	}
}

func TestOnlyWaitIsInvokableAndTheCommonToolCatalogMatches(t *testing.T) {
	reg := swarm.NewRegistry()
	defer reg.Close()
	log := &audit{}
	tools := reg.Tools()
	if len(tools) != 5 {
		t.Fatal("unexpected lifecycle catalog")
	}
	for _, base := range tools {
		info, err := base.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if info.Name == "read" || info.Name == "write" || info.Name == "exec" {
			t.Fatal("filesystem or process tool advertised")
		}
		wrapped := &guardedTool{inner: base.(tool.InvokableTool), name: info.Name, audit: log}
		out, err := wrapped.InvokableRun(context.Background(), "{}")
		if err != nil {
			t.Fatal(err)
		}
		if info.Name != "wait_agents" && !strings.Contains(out, "outside this experiment") {
			t.Fatal("lifecycle action was not refused")
		}
		left, err := wrapped.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		right, err := wrapped.Info(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		a, _ := json.Marshal(left)
		b, _ := json.Marshal(right)
		if !bytes.Equal(a, b) {
			t.Fatal("common catalog changed")
		}
		if info.Name == "wait_agents" && !strings.Contains(left.Desc, "result_ref") {
			t.Fatal("common reference protocol absent")
		}
	}
	if len(log.Violations) != 4 || len(log.Receipts) != 5 {
		t.Fatal("scope evidence missing")
	}
	bad := &guardedTool{inner: &failingTool{infoErr: errors.New("info")}}
	if _, err := bad.Info(context.Background()); err == nil {
		t.Fatal("Info error lost")
	}
	failing := &guardedTool{inner: &failingTool{runErr: errors.New("actual tool error")}, name: "wait_agents", audit: log}
	if _, err := failing.InvokableRun(context.Background(), "{}"); err == nil {
		t.Fatal("tool error lost")
	}
}

func TestConfigAndEvidenceFailuresAreReportedWithoutOverwriting(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.yaml")
	if _, _, err := readConfig(options{config: path}); err == nil {
		t.Fatal("missing config accepted")
	}
	if err := privateFile(path, []byte("models: [")); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readConfig(options{config: path}); err == nil {
		t.Fatal("broken config accepted")
	}
	if err := os.WriteFile(path, []byte("models:\n  providers: []\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := readConfig(options{config: path, provider: "missing"}); err == nil {
		t.Fatal("unknown provider accepted")
	}
	data := []byte("models:\n  providers:\n    - id: local\n      model: old\n      base_url: :invalid\n      api_key: private-key\n")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, key, err := readConfig(options{config: path, provider: "local", model: "new", maxCompletion: 123})
	if err != nil || cfg.Models.Providers[0].Model != "new" || key != "private-key" || cfg.Swarm.CompletionTokenLimit() != 123 {
		t.Fatalf("override failed: %v", err)
	}
	if err := saveArtifact(root, "bad.json", func() {}, ""); err == nil {
		t.Fatal("invalid artifact accepted")
	}
	if err := saveArtifact(root, "secret.json", map[string]string{"key": "private-key"}, "private-key"); err != nil {
		t.Fatal(err)
	}
	secret, _ := os.ReadFile(filepath.Join(root, "secret.json"))
	if bytes.Contains(secret, []byte("private-key")) {
		t.Fatal("credential leaked")
	}
	fixturePath := filepath.Join(root, "case.json")
	writeTestJSON(t, fixturePath, validFixture())
	arguments := []string{"--config", path, "--provider", "local", "--fixture", fixturePath, "--out", filepath.Join(root, "evidence"), "--arm", "a"}
	if runCLI(context.Background(), arguments, io.Discard, io.Discard) != 2 {
		t.Fatal("invalid provider should fail")
	}
	if runCLI(context.Background(), arguments, io.Discard, io.Discard) != 2 {
		t.Fatal("out directory overwritten")
	}
	arguments[1] = "absent"
	if runCLI(context.Background(), arguments, io.Discard, io.Discard) != 2 {
		t.Fatal("missing config accepted")
	}
	if err := privateFile(filepath.Join(root, "secret.json", "child"), nil); err == nil {
		t.Fatal("file-parent evidence accepted")
	}
}

func TestSessionFailuresAndInitialMessageOnlyRound(t *testing.T) {
	f := validFixture()
	f.Rounds = nil
	f.Messages = append(f.Messages, struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}{Role: "user", Content: "inspect"})
	result, _, err := executeSession(context.Background(), f, "a", &simpleModel{}, &audit{}, 3, time.Second)
	if err != nil || len(result.Rounds) != 1 || result.Rounds[0].Final != "finished" {
		t.Fatalf("initial-message round failed: %+v %v", result, err)
	}
	f = validFixture()
	f.Workers[0].AgentID = ""
	if _, _, err := executeSession(context.Background(), f, "a", &simpleModel{}, &audit{}, 3, time.Second); err == nil {
		t.Fatal("invalid worker accepted")
	}
	result, _, err = executeSession(context.Background(), validFixture(), "a", &simpleModel{failure: errors.New("observed model failure")}, &audit{}, 3, time.Second)
	if err != nil || result.Error == "" || len(result.Rounds) != 1 || result.Rounds[0].Error == "" || result.CostComplete {
		t.Fatal("model failure or unknown cost lost")
	}
}
