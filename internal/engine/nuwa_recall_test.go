package engine

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func nuwaRecallNodeIDs(nodes []store.NuwaNode) []string {
	ids := make([]string, len(nodes))
	for i, node := range nodes {
		ids[i] = node.ID
	}
	return ids
}

// Each worker's initial task must bring its own older metadata ahead of an
// unrelated recent page, without borrowing the other worker's task or details.
func TestNuwaRecallUsesEachTaskToRankOlderTitleAndSummary(t *testing.T) {
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	nodes := []store.NuwaNode{
		{ID: "new", Title: "Recent unrelated metadata", UpdatedAt: at.Add(time.Hour)},
		{ID: "title", Title: "Topic_alder", UpdatedAt: at},
		{ID: "summary", Title: "Archived metadata", Summary: "Topic_birch", UpdatedAt: at},
	}
	for _, test := range []struct {
		task, first string
	}{
		{"Inspect TOPIC_ALDER", "title"},
		{"Inspect topic_birch", "summary"},
	} {
		t.Run(test.first, func(t *testing.T) {
			ordered := orderNuwaContextNodes(nodes, test.task)
			if len(ordered) != len(nodes) || ordered[0].ID != test.first {
				t.Fatalf("task did not prioritize its own matching metadata: %v", nuwaRecallNodeIDs(ordered))
			}
		})
	}
}

func TestNuwaRecallWeightsDistinctiveTermsWithoutRewardingRepetition(t *testing.T) {
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	nodes := []store.NuwaNode{{ID: "rare", Title: "Distinctive", UpdatedAt: at}}
	for _, id := range []string{"a", "b", "c", "d"} {
		nodes = append(nodes, store.NuwaNode{
			ID: id, Title: strings.Repeat("common ", 50), UpdatedAt: at.Add(time.Hour),
		})
	}
	first := orderNuwaContextNodes(nodes, "common distinctive")
	if first[0].ID != "rare" {
		t.Fatalf("a common repeated word displaced the distinctive shared term: %v", nuwaRecallNodeIDs(first))
	}
	for _, task := range []string{"distinctive common", "COMMON distinctive common"} {
		ordered := orderNuwaContextNodes(nodes, task)
		if !reflect.DeepEqual(first, ordered) {
			t.Fatalf("query order or repeated terms changed recall: %v", nuwaRecallNodeIDs(ordered))
		}
	}
}

func TestNuwaRecallFallsBackToRecentThenIDForZeroOrTiedMatches(t *testing.T) {
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	nodes := []store.NuwaNode{
		{ID: "b", Title: "shared", UpdatedAt: at},
		{ID: "new", Title: "shared", UpdatedAt: at.Add(time.Hour)},
		{ID: "a", Summary: "shared", UpdatedAt: at},
	}
	for _, task := range []string{"", " \t.!😀 ", "unmatched", "shared"} {
		ordered := orderNuwaContextNodes(nodes, task)
		if got := nuwaRecallNodeIDs(ordered); !slices.Equal(got, []string{"new", "a", "b"}) {
			t.Fatalf("zero or tied matches changed the existing ordering: task=%q ids=%v", task, got)
		}
		reversed := []store.NuwaNode{nodes[2], nodes[1], nodes[0]}
		if !reflect.DeepEqual(ordered, orderNuwaContextNodes(reversed, task)) {
			t.Fatalf("source order changed an equal-score preview: task=%q", task)
		}
	}
	for _, nodes := range [][]store.NuwaNode{nil, {}} {
		for _, task := range []string{"", "query"} {
			if got := orderNuwaContextNodes(nodes, task); !reflect.DeepEqual(got, nodes) {
				t.Fatalf("empty recall changed its input shape: %#v", got)
			}
		}
	}
}

func TestNuwaRecallRetainsUnicodeAndShortIdentifiers(t *testing.T) {
	text := "CAFÉ Résumé Go AI x 42 a_b 字 中文检索 あいう カタカナ 한국어"
	terms := nuwaRecallTerms(text)
	for _, term := range []string{"café", "résumé", "go", "ai", "x", "42", "a", "b", "字", "中文", "文检", "检索", "あい", "いう", "カタ", "タカ", "カナ", "한국", "국어"} {
		if _, ok := terms[term]; !ok {
			t.Errorf("Unicode word, CJK pair, or short identifier %q was discarded", term)
		}
	}
	for _, term := range []string{"CAFÉ", "a_b", "中文检索", "😀"} {
		if _, ok := terms[term]; ok {
			t.Errorf("unexpected recall token %q", term)
		}
	}
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	for _, test := range []struct{ task, metadata string }{
		{"Go", "go"}, {"AI", "ai"}, {"x", "x"}, {"42", "42"},
		{"RÉSUMÉ", "résumé"}, {"中文检索", "中文检索"}, {"字", "字"},
	} {
		nodes := []store.NuwaNode{
			{ID: "new", Title: "unrelated", UpdatedAt: at.Add(time.Hour)},
			{ID: "match", Summary: test.metadata, UpdatedAt: at},
		}
		if got := orderNuwaContextNodes(nodes, test.task); got[0].ID != "match" {
			t.Errorf("Unicode or short query %q did not recall matching older metadata", test.task)
		}
	}
}

func TestNuwaRecallIgnoresDetailAndPreservesSourceAndWarnings(t *testing.T) {
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	nodes := []store.NuwaNode{
		{ID: "detail", Title: "unrelated", Detail: "needle", UpdatedAt: at.Add(time.Hour)},
		{ID: "match", Title: "needle", Detail: "on-demand evidence", Status: "reported", UpdatedAt: at,
			DependsOn: []string{"source"}, DependencyVersions: map[string]int64{"source": 1},
			StaleDependencies: []string{"source"}, MissingDependencies: []string{"removed"}},
	}
	before := []store.NuwaNode{
		nodes[0],
		{ID: "match", Title: "needle", Detail: "on-demand evidence", Status: "reported", UpdatedAt: at,
			DependsOn: []string{"source"}, DependencyVersions: map[string]int64{"source": 1},
			StaleDependencies: []string{"source"}, MissingDependencies: []string{"removed"}},
	}
	ordered := orderNuwaContextNodes(nodes, "needle")
	if got := nuwaRecallNodeIDs(ordered); !slices.Equal(got, []string{"match", "detail"}) {
		t.Fatalf("on-demand detail influenced recall: %v", got)
	}
	text, err := renderNuwaContext("project", 3, nodes, "needle", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	view := decodeNuwaContextUnit(t, text)
	if len(view.Nodes) != 2 || view.Nodes[0].ID != "match" || view.Nodes[0].Status != "reported" ||
		!slices.Equal(view.Nodes[0].StaleDependencies, []string{"source"}) ||
		!slices.Equal(view.Nodes[0].MissingDependencies, []string{"removed"}) ||
		view.Nodes[0].Detail != "" || strings.Contains(text, "on-demand evidence") {
		t.Fatalf("recall hid a warning, judged reported data, or expanded detail: %+v", view)
	}
	if !reflect.DeepEqual(nodes, before) {
		t.Fatal("ranking or rendering mutated source order, detail, or dependency metadata")
	}
}

func TestNuwaRecallKeepsWholeNodeBudgetAndSkipsOversizedMatches(t *testing.T) {
	at := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	small := store.NuwaNode{ID: "small", Title: "other", UpdatedAt: at.Add(time.Hour)}
	full, err := renderNuwaContext("project", 1, []store.NuwaNode{small}, "needle", 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	budget := utf8.RuneCountInString(full)
	large := store.NuwaNode{ID: "large", Title: "needle", Summary: strings.Repeat("观察😀", budget), UpdatedAt: at}
	text, err := renderNuwaContext("project", 1, []store.NuwaNode{small, large}, "needle", budget)
	if err != nil || utf8.RuneCountInString(text) > budget {
		t.Fatalf("ranked preview exceeded the whole character budget: err=%v", err)
	}
	view := decodeNuwaContextUnit(t, text)
	if view.Total != 2 || view.Omitted != 1 || len(view.Nodes) != 1 || view.Nodes[0].ID != small.ID {
		t.Fatalf("an oversized relevant node displaced a later complete node: %+v", view)
	}
}
