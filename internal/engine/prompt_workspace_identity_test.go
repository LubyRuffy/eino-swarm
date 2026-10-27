package engine

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

func selectedPromptWorkspace(t *testing.T, e *Engine, siblingExists bool) (string, string) {
	t.Helper()
	if goruntime.GOOS == "windows" {
		t.Skip("Windows does not expose these trailing-whitespace directory identities")
	}
	sibling := filepath.Join(t.TempDir(), "selected")
	selected := sibling + " \t"
	if err := os.Mkdir(selected, 0o700); err != nil {
		t.Fatal(err)
	}
	if siblingExists {
		if err := os.Mkdir(sibling, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	p, err := e.CreateProject("Workspace identity", "", selected, false)
	if err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("Workspace identity", "", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	return selected, th.ID
}

// A selected path is a filesystem identity. The model must not be instructed
// to use its missing or different trimmed sibling while tools use the real one.
func TestManagerPromptPreservesTheSelectedWorkspacePath(t *testing.T) {
	for _, siblingExists := range []bool{false, true} {
		name := "selected_only"
		if siblingExists {
			name = "trimmed_sibling_exists"
		}
		t.Run(name, func(t *testing.T) {
			e := newTestEngine(t)
			selected, threadID := selectedPromptWorkspace(t, e, siblingExists)
			set := buildTestToolset(t, e, threadID)
			if set.WorkspaceDir != selected {
				t.Fatalf("actual tool workspace=%q, want %q", set.WorkspaceDir, selected)
			}
			prompt := ManagerPrompt(set, "")
			before, path, found := strings.Cut(prompt, "\n## Workspace path\n\n")
			if !found || path != selected+"\n" {
				t.Errorf("manager workspace bytes=%q, want %q", path, selected+"\n")
			}
			other := *set
			other.WorkspaceDir = strings.TrimSpace(selected)
			otherPrefix, _, otherFound := strings.Cut(ManagerPrompt(&other, ""), "\n## Workspace path\n\n")
			if !found || !otherFound || before != otherPrefix {
				t.Error("preserving workspace bytes must keep the generic prefix shared")
			}
		})
	}
}

func TestManagerPromptOmitsBlankWorkspacePath(t *testing.T) {
	for _, blank := range []string{"", " \t\n"} {
		if prompt := ManagerPrompt(&tools.Set{WorkspaceDir: blank}, ""); strings.Contains(prompt, "## Workspace path") {
			t.Fatalf("blank workspace %q invented a path section", blank)
		}
	}
}

// Cross the actual Engine/Registry/ADK/provider adapter boundary: preserving a
// formatted helper result alone cannot establish what the model receives.
func TestManagerRequestKeepsTheSelectedWorkspacePath(t *testing.T) {
	requests := make(chan string, 1)
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected fixture request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		var request struct {
			Tools    []json.RawMessage `json:"tools"`
			Stream   bool              `json:"stream"`
			Messages []struct{ Role, Content string }
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		if len(request.Messages) == 0 {
			t.Error("actual model request has no system message")
			http.Error(w, "missing fixture system", http.StatusBadRequest)
			return
		}
		if request.Messages[0].Role != "system" {
			t.Errorf("actual first model message role=%q, want system", request.Messages[0].Role)
			http.Error(w, "invalid fixture system role", http.StatusBadRequest)
			return
		}
		if replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		select {
		case requests <- request.Messages[0].Content:
		default:
			t.Error("fixture unexpectedly received another manager request")
		}
		writeContextFlowReply(w, request.Stream, map[string]any{"content": "The inspection is complete."}, "stop")
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	e.cfg.Models.Default = config.DefaultProviderID
	e.cfg.Models.Providers = []config.Provider{{ID: config.DefaultProviderID,
		BaseURL: endpoint.URL, Model: "workspace-path-fixture", ContextWindow: 128000}}
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.pool = provider.New(e.cfg)
	selected, threadID := selectedPromptWorkspace(t, e, true)
	turn, err := e.StartTurn(threadID, "Inspect the workspace.")
	if err != nil {
		t.Fatal(err)
	}
	turn = waitForTurn(t, e, turn.ID)
	waitSettled(t, e, threadID)
	if turn.Status != store.TurnDone || turn.Error != "" {
		t.Fatalf("actual fixture turn did not finish: %+v", turn)
	}
	select {
	case instruction := <-requests:
		if !strings.Contains(instruction, "\n## Workspace path\n\n"+selected+"\n") {
			_, path, _ := strings.Cut(instruction, "\n## Workspace path\n\n")
			t.Fatalf("actual manager model input changed the selected path: got %q, want %q", strings.SplitN(path, "\n", 2)[0], selected)
		}
	default:
		t.Fatal("no actual manager HTTP request was observed")
	}
}
