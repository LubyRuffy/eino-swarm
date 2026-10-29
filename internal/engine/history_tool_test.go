package engine

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

func historyToolFixture(t *testing.T) (*Engine, *historyQueryTool, *tools.Set) {
	t.Helper()
	e := newTestEngine(t)
	th, err := e.CreateThread("History", "", "")
	if err != nil {
		t.Fatal(err)
	}
	set, err := e.conversationTools(context.Background(), th)
	if err != nil {
		t.Fatal(err)
	}
	for _, base := range set.Tools {
		if query, ok := base.(*historyQueryTool); ok {
			// These tests isolate argument decoding/result formatting. Actual
			// Registry/HTTP/worker tests retain the production durable guard.
			query.guard = nil
			return e, query, set
		}
	}
	t.Fatal("no mounted history query")
	return nil, nil, nil
}

func historyToolReply(t *testing.T, query *historyQueryTool, ctx context.Context, args string) map[string]any {
	t.Helper()
	out, err := query.InvokableRun(ctx, args)
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestHistoryToolDiscoversLiveSchemaAndPreservesScalarParameters(t *testing.T) {
	_, query, _ := historyToolFixture(t)
	info, err := query.Info(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	js, err := info.ToJSONSchema()
	if err != nil || info.Name != ToolQueryHistory {
		t.Fatalf("info=%+v schema=%+v err=%v", info, js, err)
	}
	for _, name := range []string{"sql", "params"} {
		if _, ok := js.Properties.Get(name); !ok {
			t.Fatalf("missing parameter %s", name)
		}
	}
	params, _ := js.Properties.Get("params")
	if got := strings.Join(params.Items.TypeEnhanced, ","); got != "string,number,boolean,null" {
		t.Fatalf("scalar array schema=%s", got)
	}
	result := historyToolReply(t, query, context.Background(), `{}`)
	if result["ok"] != true || len(result["schema"].([]any)) == 0 || result["context"].(map[string]any)["thread_id"] != query.context.ThreadID {
		t.Fatalf("schema/identity=%v", result)
	}
	args := `{"sql":"SELECT ? AS large_integer,typeof(?) AS integer_type,? AS fraction,? AS truth,? AS null_value,? AS text","params":[9007199254740993,9007199254740993,1.25,true,null,"原文"]}`
	out, err := query.InvokableRun(context.Background(), args)
	if err != nil || !strings.Contains(out, "9007199254740993") {
		t.Fatalf("large integer lost precision: %s %v", out, err)
	}
	result = historyToolReply(t, query, context.Background(), args)
	row, ok := result["rows"].([]any)[0].(map[string]any)
	if !ok {
		t.Fatalf("SQL labels are not bound to values in the actual tool response: %v", result)
	}
	if result["ok"] != true || row["integer_type"] != "integer" || row["fraction"] != 1.25 || row["truth"] != float64(1) || row["null_value"] != nil || row["text"] != "原文" {
		t.Fatalf("scalars=%v", result)
	}
}

func TestHistoryErrorsRespectTheCompleteConfiguredResultBudget(t *testing.T) {
	_, query, _ := historyToolFixture(t)
	query.maxChars = 512
	args, _ := json.Marshal(map[string]any{"sql": "SELECT " + strings.Repeat("missing_column", 1000)})
	out, err := query.InvokableRun(context.Background(), string(args))
	if err != nil {
		t.Fatal(err)
	}
	if utf8.RuneCountInString(out) > query.maxChars {
		t.Fatalf("error exceeded result limit: %d", utf8.RuneCountInString(out))
	}
	var response struct {
		OK        bool                           `json:"ok"`
		Context   historyContext                 `json:"context"`
		Truncated bool                           `json:"truncated"`
		Error     struct{ Code, Message string } `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &response); err != nil {
		t.Fatal(err)
	}
	if response.OK || !response.Truncated || response.Error.Code != "query_failed" || response.Error.Message == "" || response.Context != query.context {
		t.Fatalf("clipped diagnostic lost identity: %s", out)
	}
	// Escaping and multi-byte text consume the encoded JSON budget differently.
	out, err = query.failure("fixture", errors.New(strings.Repeat("原文<&>\"", 200)))
	if err != nil || !json.Valid([]byte(out)) || utf8.RuneCountInString(out) > query.maxChars {
		t.Fatalf("escaped diagnostic=%s %v", out, err)
	}
	query.context.ProjectName = strings.Repeat("scope", 200)
	out, err = query.failure("query_failed", errors.New("diagnostic"))
	if err != nil || !strings.Contains(out, `"code":"metadata_too_large"`) || strings.Contains(out, `"context"`) || utf8.RuneCountInString(out) > query.maxChars {
		t.Fatalf("oversized context refusal=%s %v", out, err)
	}
	query.maxChars = 1
	if out, err := query.failure("query_failed", errors.New("diagnostic")); err == nil || out != "" {
		t.Fatalf("impossible JSON budget=%q %v", out, err)
	}
}

func TestHistorySuccessKeepsCompleteOriginalRowsInsideItsEnvelopeBudget(t *testing.T) {
	_, query, _ := historyToolFixture(t)
	query.maxChars = 1024
	query.context.ProjectName = "项目范围"
	out, err := query.InvokableRun(context.Background(), `{"sql":"SELECT '原始记录' AS source"}`)
	if err != nil || utf8.RuneCountInString(out) > query.maxChars || !strings.Contains(out, "原始记录") {
		t.Fatalf("successful result=%s %v", out, err)
	}
}

func TestHistoryToolReturnsRepairableErrorsAndRetainsOwnership(t *testing.T) {
	_, query, _ := historyToolFixture(t)
	for _, args := range []string{
		`null`, `{`, `[]`, `{} {}`, `{"sql":42}`, `{"params":[{}]}`, `{"params":[[]]}`,
		`{"params":[9223372036854775808]}`, `{"params":[1e9999]}`,
		`{"sql":"DELETE FROM messages"}`, `{"sql":"SELECT * FROM pending_schedule_answers"}`,
		`{"sql":"SELECT missing_column FROM messages"}`,
	} {
		t.Run(args, func(t *testing.T) {
			result := historyToolReply(t, query, context.Background(), args)
			if result["ok"] != false || result["error"].(map[string]any)["message"] == "" || result["context"].(map[string]any)["thread_id"] != query.context.ThreadID {
				t.Fatalf("refusal=%v", result)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := historyToolReply(t, query, ctx, `{"sql":"SELECT 1"}`)
	if result["ok"] != false || !strings.Contains(result["error"].(map[string]any)["message"].(string), "canceled") {
		t.Fatalf("cancel=%v", result)
	}
	if result := historyToolReply(t, query, context.Background(), `{"sql":"SELECT 1"}`); result["ok"] != true {
		t.Fatalf("refusal poisoned subsequent query: %v", result)
	}
}

func TestHistoryPromptDescribesOnlyMountedGenericCapabilities(t *testing.T) {
	_, query, set := historyToolFixture(t)
	if historyPrompt(&tools.Set{}) != "" || historyPrompt(&tools.Set{Tools: set.Tools[:1]}) != "" {
		t.Fatal("unmounted tools were advertised")
	}
	prompt := historyPrompt(set)
	for _, want := range []string{query.context.ThreadID, "query_history", "project_id", "source material", "negation", "truncated", "runtime", "messages", "turns", "events"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("missing history guidance %q", want)
		}
	}
	for _, sample := range []string{"fofa", "mcp", "mythic", "glm-5"} {
		if strings.Contains(strings.ToLower(prompt), sample) {
			t.Errorf("a user's example leaked into the product prompt: %s", sample)
		}
	}
	if strings.Contains(prompt, `\n`) {
		t.Fatal("prompt contains an accidental escaped newline")
	}
	if query.context.ProjectID != "" || query.context.ProjectName != "" {
		t.Fatal("standalone conversation inherited a project")
	}
}

func TestHistoryToolsSurvivePlanningAndMemorySwitchesWithoutMutatingFiles(t *testing.T) {
	e := newTestEngineAt(t, t.TempDir())
	e.Config().Memory.Enabled = false
	project, err := e.CreateProject("Source project", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("Reader", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	set, err := e.conversationTools(context.Background(), th)
	if err != nil {
		t.Fatal(err)
	}
	set = tools.ExploreOnly(set)
	if got := toolNames(t, set.Tools); !got[ToolQueryHistory] || got["exec"] || got["write"] {
		t.Fatalf("planning surface=%v", got)
	}
	pc, err := e.projectContextFor(th)
	if err != nil {
		t.Fatal(err)
	}
	reg := e.newTurnRegistry(nil, set, pc)
	defer reg.Close()
	if !toolNames(t, reg.SubAgentTools)[ToolQueryHistory] || !strings.Contains(reg.WorkerPreamble, project.ID) || !strings.Contains(reg.WorkerPreamble, project.Name) {
		t.Fatal("worker lost current project/query context")
	}
	if files, err := os.ReadDir(e.ProjectWorkdir(project)); err != nil || len(files) != 0 {
		t.Fatalf("history added workspace files: %v %v", files, err)
	}
}

func TestHistoryToolAssemblyPreservesMissingProjectFallbackAndBuildErrors(t *testing.T) {
	e, _, _ := historyToolFixture(t)
	th, err := e.CreateThread("Detached reference", "", "")
	if err != nil {
		t.Fatal(err)
	}
	th.ProjectID = "missing-project"
	if _, err := e.conversationTools(context.Background(), th); err != nil {
		t.Fatalf("missing project fallback failed: %v", err)
	}
	if err := e.Store().DB().Migrator().DropTable(&store.Project{}); err != nil {
		t.Fatal(err)
	}
	if _, err := e.conversationTools(context.Background(), th); err == nil {
		t.Fatal("database failure was hidden as a missing project")
	}
	broken := newTestEngineAt(t, t.TempDir())
	th, err = broken.CreateThread("Bad workspace", "", "")
	if err != nil {
		t.Fatal(err)
	}
	dir := broken.WorkspaceDir(th.ID)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dir, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := broken.conversationTools(context.Background(), th); err == nil {
		t.Fatal("tool assembly ignored a failed workspace build")
	}
	// The configured result limit is reused instead of adding another setting.
	if broken.cfg.Tools.ToolResultLimit() != config.DefaultToolResultMaxChars || !filepath.IsAbs(broken.cfg.DBPath()) {
		t.Fatal("unexpected configured storage/result budget")
	}
}
