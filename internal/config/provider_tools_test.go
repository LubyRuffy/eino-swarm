package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderBuiltinToolsKeepNestedOptionsAcrossSaveAndLoad(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := &cfg.Models.Providers[0]
	p.APIMode = APIModeResponses
	p.BuiltinTools = []map[string]any{
		{"type": "web_search", "filters": map[string]any{"allowed_domains": []any{"example.invalid"}}, "search_context_size": "low"},
		{"type": "file_search", "vector_store_ids": []any{"vs_fixture"}, "max_num_results": 3},
		{"type": "code_interpreter", "container": map[string]any{"type": "auto"}},
	}
	want, _ := json.Marshal(p.BuiltinTools)
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(cfg.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(loaded.Models.Providers[0].BuiltinTools)
	if loaded.Models.Providers[0].APIMode != APIModeResponses || !bytes.Equal(got, want) {
		t.Fatalf("cloud tool options changed: %s want %s", got, want)
	}
	legacy, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if legacy.Models.Providers[0].APIMode != "" || len(legacy.Models.Providers[0].BuiltinTools) != 0 {
		t.Fatal("old providers must retain chat completions without cloud tools")
	}
}

func TestProviderBuiltinToolsValidateBeforeWritingOrChangingLiveConfig(t *testing.T) {
	cases := []struct {
		name, mode string
		tools      []map[string]any
	}{
		{"unknown mode", "messages", nil},
		{"chat tools", APIModeChatCompletions, []map[string]any{{"type": "web_search"}}},
		{"implicit chat tools", "", []map[string]any{{"type": "web_search"}}},
		{"missing type", APIModeResponses, []map[string]any{{"filters": map[string]any{}}}},
		{"null tool", APIModeResponses, []map[string]any{nil}},
		{"nonstring type", APIModeResponses, []map[string]any{{"type": 3}}},
		{"empty type", APIModeResponses, []map[string]any{{"type": ""}}},
		{"duplicate type", APIModeResponses, []map[string]any{{"type": "web_search"}, {"type": "web_search"}}},
		{"unknown tool", APIModeResponses, []map[string]any{{"type": "future_cloud_tool"}}},
		{"local function", APIModeResponses, []map[string]any{{"type": "function"}}},
		{"local custom", APIModeResponses, []map[string]any{{"type": "custom"}}},
		{"local computer", APIModeResponses, []map[string]any{{"type": "computer"}}},
		{"computer preview", APIModeResponses, []map[string]any{{"type": "computer_use_preview"}}},
		{"mcp approval", APIModeResponses, []map[string]any{{"type": "mcp"}}},
		{"local shell", APIModeResponses, []map[string]any{{"type": "shell"}}},
		{"local patch", APIModeResponses, []map[string]any{{"type": "apply_patch"}}},
		{"nonjson options", APIModeResponses, []map[string]any{{"type": "web_search", "filters": map[any]any{4: "bad"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Load(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(cfg.Path())
			if err != nil {
				t.Fatal(err)
			}
			next := *cfg
			next.Models.Providers = []Provider{{ID: "candidate", APIMode: tc.mode, BuiltinTools: tc.tools}}
			if err := cfg.Replace(&next); !errors.Is(err, ErrInvalidProviderConfig) {
				t.Fatalf("Replace error=%v", err)
			}
			if cfg.Models.Providers[0].ID == "candidate" {
				t.Fatal("invalid cloud tools changed live provider")
			}
			after, err := os.ReadFile(cfg.Path())
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatal("invalid cloud tools changed config file")
			}
			next.dataDir = cfg.dataDir
			if err := next.Save(); !errors.Is(err, ErrInvalidProviderConfig) {
				t.Fatalf("Save error=%v", err)
			}
			after, _ = os.ReadFile(cfg.Path())
			if !bytes.Equal(after, before) {
				t.Fatal("invalid Save changed config file")
			}
		})
	}
}

func TestProviderBuiltinToolsRejectInvalidYAMLOnLoad(t *testing.T) {
	for _, raw := range []string{
		"api_mode: responses\n      builtin_tools:\n        - type: function\n",
		"api_mode: responses\n      builtin_tools:\n        - type: web_search\n          filters:\n            4: invalid\n",
		"api_mode: unsupported\n",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte("models:\n  providers:\n    - id: default\n      "+raw), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(dir); !errors.Is(err, ErrInvalidProviderConfig) {
			t.Fatalf("invalid file accepted: %v", err)
		}
	}
}

func TestCloneBuiltinToolsIsolatesNestedOptions(t *testing.T) {
	tools := []map[string]any{{"type": "web_search_preview", "filters": map[string]any{"allowed_domains": []any{"original.invalid"}}}}
	cloned, err := CloneBuiltinTools(tools)
	if err != nil {
		t.Fatal(err)
	}
	cloned[0]["filters"].(map[string]any)["allowed_domains"].([]any)[0] = "changed.invalid"
	if tools[0]["filters"].(map[string]any)["allowed_domains"].([]any)[0] != "original.invalid" {
		t.Fatal("cloned config aliases nested tool options")
	}
	if _, err := CloneBuiltinTools([]map[string]any{{"type": "web_search", "bad": make(chan int)}}); err == nil {
		t.Fatal("unencodable tools silently cloned")
	}
	nilTools, err := CloneBuiltinTools(nil)
	if err != nil || nilTools != nil {
		t.Fatalf("nil clone=%v, %v", nilTools, err)
	}
}

func TestProviderAllowsEmptyAndSupportedAPIModes(t *testing.T) {
	for _, mode := range []string{"", APIModeChatCompletions, APIModeResponses} {
		if err := (Provider{APIMode: mode}).Validate(); err != nil {
			t.Fatalf("valid API mode %q: %v", mode, err)
		}
	}
	if err := (Provider{APIMode: APIModeResponses, BuiltinTools: []map[string]any{{"type": "web_search_preview"}}}).Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestCloneBuiltinToolsPreservesIntegerPrecisionAndNumericRepresentations(t *testing.T) {
	tools := []map[string]any{{"type": "file_search", "max_num_results": uint64(9007199254740993)}}
	cloned, err := CloneBuiltinTools(tools)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := json.Marshal(tools)
	got, _ := json.Marshal(cloned)
	if !bytes.Equal(got, want) {
		t.Fatalf("clone rounded integer options: %s want %s", got, want)
	}
	rawTools := []map[string]any{{"type": "web_search", "weight": json.RawMessage("1e9999"), "absent": nil, "nil_map": map[string]any(nil), "nil_slice": []string(nil), "domains": []string{"fixture.invalid"}}}
	cloned, err = CloneBuiltinTools(rawTools)
	if err != nil {
		t.Fatal(err)
	}
	want, _ = json.Marshal(rawTools)
	got, _ = json.Marshal(cloned)
	if !bytes.Equal(got, want) {
		t.Fatalf("clone changed JSON options: %s want %s", got, want)
	}
	cloned[0]["domains"].([]string)[0] = "changed.invalid"
	if rawTools[0]["domains"].([]string)[0] != "fixture.invalid" {
		t.Fatal("typed JSON list retained an alias")
	}
}
