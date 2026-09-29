package swarm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/tool"
)

func TestSpawnStemIsTheJobWord(t *testing.T) {
	cases := []struct{ in, want string }{
		{"helper", "helper"},
		{"helper/a/a/a-1/a", "helper"},
		{"helper-2", "helper-2"},
		{"code-reviewer/extra", "code-reviewer"},
		{"my helper/extra", "my-helper"},
		{"manager/a", "worker"},
		{"///", "worker"},
		{"审阅/子", "审阅"},
	}
	for _, tc := range cases {
		if got := spawnStem(tc.in); got != tc.want {
			t.Fatalf("spawnStem(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestCleanRoleIsNotRewritten(t *testing.T) {
	for _, role := range []string{"helper", "helper-2", "code-reviewer", "审阅"} {
		if dirtySpawnRole(role) {
			t.Fatalf("%q is a job name", role)
		}
	}
	for _, role := range []string{"helper/a", "helper/a/a", "my helper", "helper--2", "-helper", "helper-"} {
		if !dirtySpawnRole(role) {
			t.Fatalf("%q should not be stored as a role", role)
		}
	}
}

func TestPathShapedRolesBecomeDistinctJobNames(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot("ok")
	spawnT := invokable(t, reg.Tools()[0])
	first := spawnJob(t, spawnT, "helper/a/a/a-1/a", "one")
	second := spawnJob(t, spawnT, "helper/a/a/a-1/a/a", "two")
	if strings.Contains(first, "/") || strings.Contains(second, "/") {
		t.Fatalf("path survived: %s %s", first, second)
	}
	if first == second {
		t.Fatalf("two path roles collapsed onto one id %s", first)
	}
	if !strings.HasPrefix(first, "helper-") || !strings.HasPrefix(second, "helper-") {
		t.Fatalf("ids %s %s", first, second)
	}
	h, ok := reg.get(first)
	if !ok || h.Role != "helper" {
		t.Fatalf("stored role = %q", roleOf(h, ok))
	}
	h2, ok := reg.get(second)
	if !ok || h2.Role != "helper-2" {
		t.Fatalf("second role = %q", roleOf(h2, ok))
	}
}

func TestPlainRoleSurvivesSpawn(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = oneShot("ok")
	spawnT := invokable(t, reg.Tools()[0])
	id := spawnJob(t, spawnT, "worker-2", "one")
	if !strings.HasPrefix(id, "worker-2-") {
		t.Fatalf("clean role was rewritten: %s", id)
	}
}

func TestClaimedJobNamesDoNotCollide(t *testing.T) {
	reg := NewRegistry()
	a := reg.claimReadableRole("helper")
	b := reg.claimReadableRole("helper")
	if a != "helper" || b != "helper-2" {
		t.Fatalf("claims %q %q", a, b)
	}
	reg.releaseRoleClaim(a)
	reg.releaseRoleClaim(b)
	c := reg.claimReadableRole("helper")
	if c != "helper" {
		t.Fatalf("released name stayed taken: %q", c)
	}
}

func spawnJob(t *testing.T, spawnT tool.InvokableTool, role, task string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"role": role, "task": task})
	if err != nil {
		t.Fatal(err)
	}
	out, err := spawnT.InvokableRun(context.Background(), string(raw))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if json.Unmarshal([]byte(out), &got) != nil || got["agent_id"] == "" {
		t.Fatalf("spawn %q: %s", role, out)
	}
	if got["resumed_from"] != "" {
		t.Fatalf("path role resumed %s instead of starting: %s", got["resumed_from"], out)
	}
	return got["agent_id"]
}

func roleOf(h *Handle, ok bool) string {
	if !ok || h == nil {
		return ""
	}
	return h.Role
}
