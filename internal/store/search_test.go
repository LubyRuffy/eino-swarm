package store

import (
	"errors"
	"strings"
	"testing"
)

func TestKeywordSearchFindsBodyWhenTitleDoesNotMatch(t *testing.T) {
	s := open(t)
	hit := &Thread{Title: "gamma", ProviderID: "p"}
	miss := &Thread{Title: "alpha", ProviderID: "p"}
	if err := s.CreateThread(hit); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateThread(miss); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: hit.ID, Status: TurnDone}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(hit.ID, turn.ID, []Message{
		{Role: "user", Content: "unique-body needle sits here"},
		{Role: "assistant", Content: "acknowledged"},
		{Role: "tool", Content: `{"file_path":"unique-body"}`},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.IndexThread(hit.ID); err != nil {
		t.Fatalf("IndexThread hit: %v", err)
	}
	if err := s.IndexThread(miss.ID); err != nil {
		t.Fatalf("IndexThread miss: %v", err)
	}

	got, err := s.SearchThreads("unique-body", 10)
	if err != nil {
		t.Fatalf("SearchThreads: %v", err)
	}
	if len(got) != 1 || got[0].ThreadID != hit.ID {
		t.Fatalf("want the body hit only, got %+v", got)
	}
	if !strings.Contains(got[0].Snippet, "needle") && !strings.Contains(got[0].Snippet, "unique-body") {
		t.Fatalf("snippet dropped the match: %q", got[0].Snippet)
	}
	for _, h := range got {
		if strings.Contains(h.Snippet, "file_path") {
			t.Fatalf("tool JSON must not be indexed: %+v", h)
		}
	}
}

func TestKeywordSearchMatchesShortCJKSubstring(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "alpha", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID, Status: TurnDone}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(th.ID, turn.ID, []Message{
		{Role: "user", Content: "会话里写了训练计划"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.IndexThread(th.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchThreads("训练", 10)
	if err != nil {
		t.Fatalf("SearchThreads: %v", err)
	}
	if len(got) != 1 || got[0].ThreadID != th.ID {
		t.Fatalf("two-character CJK must still hit, got %+v", got)
	}
}

func TestKeywordSearchIgnoresFTSOperatorsInTheQuery(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "quoted alpha", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.IndexThread(th.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchThreads(`alpha " AND (`, 10)
	if err != nil {
		t.Fatalf("a hostile query must not fail: %v", err)
	}
	if len(got) != 1 || got[0].ThreadID != th.ID {
		t.Fatalf("want the title hit, got %+v", got)
	}
}

func TestKeywordSearchSkipsArchivedAndBlankQueries(t *testing.T) {
	s := open(t)
	live := &Thread{Title: "live alpha", ProviderID: "p"}
	dead := &Thread{Title: "archived alpha", ProviderID: "p", Archived: true}
	if err := s.CreateThread(live); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateThread(dead); err != nil {
		t.Fatal(err)
	}
	if err := s.IndexThread(live.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.IndexThread(dead.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchThreads("alpha", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ThreadID != live.ID {
		t.Fatalf("archived rows must stay out of Recents search: %+v", got)
	}
	empty, err := s.SearchThreads("   ", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatalf("blank query is not a wildcard: %+v", empty)
	}
}

func TestDeleteThreadDropsTheSearchIndex(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "doomed alpha", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.IndexThread(th.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteThread(th.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchThreads("alpha", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("deleted conversation still searchable: %+v", got)
	}
}

func TestFTSMatchFindsABodyTheTitleMisses(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "gamma", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID, Status: TurnDone}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(th.ID, turn.ID, []Message{
		{Role: "user", Content: "unique-body needle sits here"},
	}); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.Raw(
		`SELECT COUNT(*) FROM thread_fts WHERE thread_fts MATCH ?`,
		ftsPhrase("unique-body"),
	).Scan(&n).Error; err != nil {
		t.Fatalf("fts match: %v", err)
	}
	if n != 1 {
		t.Fatalf("trigram FTS must hold the body, count=%d", n)
	}
}

func TestIndexThreadMissingConversation(t *testing.T) {
	s := open(t)
	if err := s.IndexThread("th_missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestIndexThreadFollowsATitleChange(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "before", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.IndexThread(th.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateThread(th.ID, map[string]any{"title": "after unique-title"}); err != nil {
		t.Fatal(err)
	}
	if err := s.IndexThread(th.ID); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchThreads("unique-title", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "after unique-title" {
		t.Fatalf("stale title stayed in the index: %+v", got)
	}
}

func TestOpenRebuildsSearchFromExistingMessages(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/zwai.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	th := &Thread{Title: "gamma", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID, Status: TurnDone}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(th.ID, turn.ID, []Message{
		{Role: "user", Content: "rebuilt-needle"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	got, err := reopened.SearchThreads("rebuilt-needle", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ThreadID != th.ID {
		t.Fatalf("reopen must rebuild FTS from messages: %+v", got)
	}
}

func TestOpenRebuildsSearchWhenTheIndexIsEmpty(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/zwai.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	th := &Thread{Title: "gamma", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID, Status: TurnDone}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(th.ID, turn.ID, []Message{
		{Role: "user", Content: "rebuilt-needle"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.db.Exec("DELETE FROM search_docs").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	got, err := reopened.SearchThreads("rebuilt-needle", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ThreadID != th.ID {
		t.Fatalf("empty search_docs must rebuild from messages: %+v", got)
	}
}

func TestOpenRebuildsFTSWhenTheVirtualTableIsMissing(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/zwai.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	th := &Thread{Title: "gamma", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID, Status: TurnDone}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendMessages(th.ID, turn.ID, []Message{
		{Role: "user", Content: "unique-fts-needle"},
	}); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		"DROP TABLE thread_fts",
		"DROP TRIGGER IF EXISTS search_docs_ai",
		"DROP TRIGGER IF EXISTS search_docs_ad",
		"DROP TRIGGER IF EXISTS search_docs_au",
	} {
		if err := s.db.Exec(q).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	var n int
	if err := reopened.db.Raw(
		`SELECT COUNT(*) FROM thread_fts WHERE thread_fts MATCH ?`,
		ftsPhrase("unique-fts-needle"),
	).Scan(&n).Error; err != nil {
		t.Fatalf("fts after upgrade: %v", err)
	}
	if n != 1 {
		t.Fatalf("creating thread_fts must rebuild from search_docs, count=%d", n)
	}
}

func TestKeywordSearchEscapesLIKEMetacharacters(t *testing.T) {
	s := open(t)
	hit := &Thread{Title: "a_b unique", ProviderID: "p"}
	miss := &Thread{Title: "axb unique", ProviderID: "p"}
	if err := s.CreateThread(hit); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateThread(miss); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchThreads("a_b", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ThreadID != hit.ID {
		t.Fatalf("underscore is a character, not a LIKE wildcard: %+v", got)
	}
}

func TestListThreadIDsSkipsArchived(t *testing.T) {
	s := open(t)
	live := &Thread{Title: "live", ProviderID: "p"}
	dead := &Thread{Title: "dead", ProviderID: "p", Archived: true}
	if err := s.CreateThread(live); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateThread(dead); err != nil {
		t.Fatal(err)
	}
	ids, err := s.ListThreadIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != live.ID {
		t.Fatalf("archived rows must stay out of embed backfill: %v", ids)
	}
}

func TestLikeSnippetFallsBackToTitleAndTruncates(t *testing.T) {
	title := "unique-title-xyz"
	if got := likeSnippet(title, "", "unique-title"); got != title {
		t.Fatalf("title hit: %q", got)
	}
	body := strings.Repeat("n", snippetPadRunes*3)
	got := likeSnippet("alpha", body, "zzz")
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("long body without a needle must clip: %q", got)
	}
	short := likeSnippet("alpha", "short body", "zzz")
	if short != "short body" {
		t.Fatalf("short body: %q", short)
	}
}

func TestSearchOnAClosedStoreFails(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "alpha", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SearchThreads("alpha", 10); err == nil {
		t.Fatal("closed store must fail search")
	}
	if err := s.IndexThread(th.ID); err == nil {
		t.Fatal("closed store must fail index")
	}
	if _, err := s.ListThreadIDs(); err == nil {
		t.Fatal("closed store must fail list")
	}
}

func TestSearchThreadsClampsTheLimit(t *testing.T) {
	s := open(t)
	for i := 0; i < 3; i++ {
		th := &Thread{Title: "alpha", ProviderID: "p"}
		if err := s.CreateThread(th); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.SearchThreads("alpha", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("limit 0 means the default, got %d", len(got))
	}
	got, err = s.SearchThreads("alpha", maxSearchLimit+10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("over-max still returns existing rows, got %d", len(got))
	}
}

func TestDeleteSteerMessageDropsTheTextFromSearch(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "gamma", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &Turn{ThreadID: th.ID, Status: TurnRunning}
	if err := s.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	needle := "retracted-steer-needle"
	if err := s.AppendMessages(th.ID, turn.ID, []Message{
		{Role: "user", Content: "[steer] " + needle, EventSeq: 7},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchThreads(needle, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ThreadID != th.ID {
		t.Fatalf("steer must be searchable before retract, got %+v", got)
	}
	if err := s.DeleteSteerMessage(th.ID, 7, needle); err != nil {
		t.Fatal(err)
	}
	got, err = s.SearchThreads(needle, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("retracted steer still searchable: %+v", got)
	}
}
