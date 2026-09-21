package search

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type stubEmbed struct {
	vecs  map[string][]float32
	calls int
	err   error
}

func (s *stubEmbed) Embed(_ context.Context, _, _ string, texts []string) ([][]float32, error) {
	s.calls++
	if s.err != nil {
		return nil, s.err
	}
	out := make([][]float32, len(texts))
	for i, text := range texts {
		if v, ok := s.vecs[text]; ok {
			out[i] = v
			continue
		}
		out[i] = []float32{0, 1}
	}
	return out, nil
}

func testCfg(t *testing.T, embedding bool) (*config.Config, *store.Store) {
	t.Helper()
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	cfg, err := config.Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg.Search.Embedding = embedding
	cfg.Search.EmbeddingModel = "named-embed"
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	return cfg, st
}

func addThread(t *testing.T, st *store.Store, title, body string) *store.Thread {
	t.Helper()
	th := &store.Thread{Title: title, ProviderID: "default"}
	if err := st.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, Status: store.TurnDone}
	if err := st.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendMessages(th.ID, turn.ID, []store.Message{
		{Role: "user", Content: body},
	}); err != nil {
		t.Fatal(err)
	}
	return th
}

func TestSwitchOnWithoutAModelStaysKeywordOnly(t *testing.T) {
	cfg, st := testCfg(t, true)
	cfg.Search.EmbeddingModel = ""
	hit := addThread(t, st, "gamma", "unique-body needle")
	embed := &stubEmbed{}
	svc := New(st, embed, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := svc.Search(context.Background(), "unique-body", 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedding {
		t.Fatal("a switch without a model is not semantic search")
	}
	if embed.calls != 0 {
		t.Fatalf("embedder called %d times without a model name", embed.calls)
	}
	if len(res.Hits) != 1 || res.Hits[0].ThreadID != hit.ID || res.Hits[0].Source != "fts" {
		t.Fatalf("keyword search must still work: %+v", res.Hits)
	}
}

func TestKeywordSearchFindsABodyHitWhenEmbeddingIsOff(t *testing.T) {
	cfg, st := testCfg(t, false)
	hit := addThread(t, st, "gamma", "unique-body needle")
	_ = addThread(t, st, "alpha", "something else")
	embed := &stubEmbed{vecs: map[string][]float32{}}
	svc := New(st, embed, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := svc.Search(context.Background(), "unique-body", 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedding {
		t.Fatal("off must not claim a semantic search")
	}
	if embed.calls != 0 {
		t.Fatalf("embedder called %d times while off", embed.calls)
	}
	if len(res.Hits) != 1 || res.Hits[0].ThreadID != hit.ID || res.Hits[0].Source != "fts" {
		t.Fatalf("hits=%+v", res.Hits)
	}
}

func TestSemanticSearchRanksAParaphraseTheKeywordsMiss(t *testing.T) {
	cfg, st := testCfg(t, true)
	near := addThread(t, st, "gamma", "the vessel left the harbor")
	far := addThread(t, st, "alpha", "unrelated tokens sit here")
	embed := &stubEmbed{vecs: map[string][]float32{
		"the vessel left the harbor": {1, 0},
		"unrelated tokens sit here":  {0, 1},
		"the ship departed":          {1, 0},
		near.Title:                   {0.2, 0.8},
		far.Title:                    {0.1, 0.9},
	}}
	svc := New(st, embed, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Start()
	t.Cleanup(svc.Stop)
	svc.WaitIdle()

	res, err := svc.Search(context.Background(), "the ship departed", 10)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Embedding {
		t.Fatal("a successful embed must set embedding true")
	}
	if len(res.Hits) == 0 || res.Hits[0].ThreadID != near.ID {
		t.Fatalf("paraphrase should rank the near conversation first: %+v", res.Hits)
	}
	if res.Hits[0].Source != "semantic" && res.Hits[0].Source != "hybrid" {
		t.Fatalf("source=%q", res.Hits[0].Source)
	}
}

func TestFailedQueryEmbeddingFallsBackToKeywords(t *testing.T) {
	cfg, st := testCfg(t, true)
	hit := addThread(t, st, "gamma", "unique-body needle")
	embed := &stubEmbed{err: context.DeadlineExceeded}
	svc := New(st, embed, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := svc.Search(context.Background(), "unique-body", 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedding {
		t.Fatal("a failed embed must not claim semantic ranking")
	}
	if len(res.Hits) != 1 || res.Hits[0].ThreadID != hit.ID {
		t.Fatalf("keyword fallback lost the hit: %+v", res.Hits)
	}
}

func TestMergeHitsTagsHybridWhenBothListsAgree(t *testing.T) {
	fts := []store.ThreadSearchHit{{ThreadID: "a", Title: "A", Snippet: "s"}, {ThreadID: "b", Title: "B", Snippet: "t"}}
	sem := []store.ChunkHit{{ThreadID: "a", Score: 0.9}, {ThreadID: "c", Score: 0.1}}
	got := mergeHits(fts, sem, map[string]string{"c": "C"})
	byID := map[string]Hit{}
	for _, h := range got {
		byID[h.ThreadID] = h
	}
	if byID["a"].Source != "hybrid" || byID["b"].Source != "fts" || byID["c"].Source != "semantic" {
		t.Fatalf("%+v", got)
	}
	if byID["a"].Score <= byID["b"].Score {
		t.Fatalf("a hit on both lists must outrank fts-only: %+v", got)
	}
}

func TestPassagesSkipToolDumpsAndHashTheText(t *testing.T) {
	th := &store.Thread{Title: "alpha", Goal: "stand"}
	msgs := []store.Message{
		{ID: 1, Role: "user", Content: "hello"},
		{ID: 2, Role: "tool", Content: `{"file_path":"x"}`},
		{ID: 3, Role: "assistant", Content: "there"},
	}
	got := passagesFor(th, msgs)
	joined := ""
	for _, p := range got {
		joined += p.Key + ":" + p.Text + ";"
		if p.Hash == "" {
			t.Fatal("every passage needs a hash")
		}
	}
	if strings.Contains(joined, "file_path") {
		t.Fatalf("tool dump leaked: %s", joined)
	}
	if !strings.Contains(joined, "title:alpha") || !strings.Contains(joined, "goal:stand") {
		t.Fatalf("missing title/goal: %s", joined)
	}
	long := strings.Repeat("字", maxChunkRunes+8)
	clipped := passagesFor(&store.Thread{Title: "t"}, []store.Message{{ID: 9, Role: "user", Content: long}})
	if len(clipped) != 2 {
		t.Fatalf("title+msg: %d", len(clipped))
	}
	if utf8.RuneCountInString(clipped[1].Text) != maxChunkRunes {
		t.Fatalf("long passage must clip, got %d runes", utf8.RuneCountInString(clipped[1].Text))
	}
}

func TestBlankQueryReturnsNoHits(t *testing.T) {
	cfg, st := testCfg(t, false)
	svc := New(st, &stubEmbed{}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := svc.Search(context.Background(), "  ", 10)
	if err != nil || len(res.Hits) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestIndexQueuesEmbeddingsOnlyWhenEnabled(t *testing.T) {
	cfg, st := testCfg(t, false)
	th := addThread(t, st, "gamma", "unique-body needle")
	embed := &stubEmbed{vecs: map[string][]float32{}}
	svc := New(st, embed, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Start()
	t.Cleanup(svc.Stop)
	svc.Index("")
	svc.Index(th.ID)
	svc.WaitIdle()
	if embed.calls != 0 {
		t.Fatalf("off must not embed, calls=%d", embed.calls)
	}

	cfg.Search.Embedding = true
	svc.Reload()
	svc.WaitIdle()
	if embed.calls == 0 {
		t.Fatal("turning the switch on must backfill embeddings")
	}
	calls := embed.calls
	svc.Index(th.ID)
	svc.WaitIdle()
	if embed.calls != calls {
		t.Fatalf("unchanged text must not re-embed, %d → %d", calls, embed.calls)
	}
	cfg.Search.Embedding = false
	svc.Reload()
	calls = embed.calls
	svc.Index(th.ID)
	svc.WaitIdle()
	if embed.calls != calls {
		t.Fatalf("turning off must stop embedding, %d → %d", calls, embed.calls)
	}
}

func TestReloadDropsVectorsFromThePreviousModel(t *testing.T) {
	cfg, st := testCfg(t, true)
	th := addThread(t, st, "gamma", "unique-body needle")
	if err := st.UpsertSearchChunk(store.SearchChunk{
		ThreadID: th.ID, ChunkKey: "title", Model: "old-embed",
		TextHash: "x", Dim: 1, Vector: store.EncodeVector([]float32{1}),
	}); err != nil {
		t.Fatal(err)
	}
	embed := &stubEmbed{vecs: map[string][]float32{
		"gamma":              {1, 0},
		"unique-body needle": {1, 0},
	}}
	svc := New(st, embed, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Start()
	t.Cleanup(svc.Stop)
	svc.WaitIdle()
	if _, err := st.SearchChunkByKey(th.ID, "title", "old-embed"); err == nil {
		t.Fatal("the previous pin must not keep vectors")
	}
}

func TestEmptyQueryEmbeddingFallsBackToKeywords(t *testing.T) {
	cfg, st := testCfg(t, true)
	hit := addThread(t, st, "gamma", "unique-body needle")
	embed := &stubEmbed{vecs: map[string][]float32{
		"unique-body": {},
	}}
	svc := New(st, embed, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := svc.Search(context.Background(), "unique-body", 10)
	if err != nil {
		t.Fatal(err)
	}
	if res.Embedding {
		t.Fatal("an empty query vector is not a semantic search")
	}
	if len(res.Hits) != 1 || res.Hits[0].ThreadID != hit.ID {
		t.Fatalf("keyword fallback lost the hit: %+v", res.Hits)
	}
}

func TestSearchClampsLimitAndStopIsIdempotent(t *testing.T) {
	cfg, st := testCfg(t, false)
	for i := 0; i < 3; i++ {
		addThread(t, st, "alpha", "body")
	}
	svc := New(st, &stubEmbed{}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := svc.Search(context.Background(), "alpha", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 3 {
		t.Fatalf("default limit should keep all three, got %d", len(res.Hits))
	}
	res, err = svc.Search(context.Background(), "alpha", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("limit 1 leaked %d hits", len(res.Hits))
	}
	svc.Start()
	svc.Stop()
	svc.Stop()
	svc.Remove("th_x")
}

func TestEmbedThreadRejectsAShortBatch(t *testing.T) {
	cfg, st := testCfg(t, true)
	th := addThread(t, st, "gamma", "unique-body needle")
	embed := &shortEmbed{}
	svc := New(st, embed, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := svc.embedThread(context.Background(), th.ID); err == nil {
		t.Fatal("a short batch must fail")
	}
	if err := svc.embedThread(context.Background(), "th_missing"); err != nil {
		t.Fatalf("a deleted conversation is not an embed error: %v", err)
	}
}

type shortEmbed struct{}

func (shortEmbed) Embed(_ context.Context, _, _ string, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	return [][]float32{{1, 0}}, nil
}

func TestNewUsesDefaultLoggerAndRemoveIsHarmless(t *testing.T) {
	cfg, st := testCfg(t, false)
	svc := New(st, nil, cfg, nil)
	svc.Remove("")
	svc.Remove("th_x")
	res, err := svc.Search(context.Background(), "alpha", 10)
	if err != nil || len(res.Hits) != 0 {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestIndexAfterStopDoesNotHang(t *testing.T) {
	cfg, st := testCfg(t, true)
	th := addThread(t, st, "gamma", "unique-body needle")
	svc := New(st, &stubEmbed{err: context.DeadlineExceeded}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Start()
	svc.WaitIdle()
	svc.Stop()
	svc.Index(th.ID)
	svc.Index("th_missing")
}

func TestMergeHitsClipsToTheLimit(t *testing.T) {
	cfg, st := testCfg(t, false)
	for i := 0; i < 5; i++ {
		addThread(t, st, "alpha", "body")
	}
	svc := New(st, &stubEmbed{}, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	res, err := svc.Search(context.Background(), "alpha", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 2 {
		t.Fatalf("want 2, got %d", len(res.Hits))
	}
	res, err = svc.Search(context.Background(), "alpha", MaxLimit+20)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 5 {
		t.Fatalf("over-max still returns existing rows, got %d", len(res.Hits))
	}
}

type gatedEmbed struct {
	started chan struct{}
	block   chan struct{}
	n       int32
	once    sync.Once
}

func (g *gatedEmbed) Embed(_ context.Context, _, _ string, texts []string) ([][]float32, error) {
	atomic.AddInt32(&g.n, 1)
	g.once.Do(func() { close(g.started) })
	<-g.block
	out := make([][]float32, len(texts))
	for i := range texts {
		out[i] = []float32{0, 1}
	}
	return out, nil
}

func TestEmbeddingOverflowIsDrainedNotDropped(t *testing.T) {
	orig := queueSize
	queueSize = 2
	t.Cleanup(func() { queueSize = orig })

	cfg, st := testCfg(t, true)
	embed := &gatedEmbed{
		started: make(chan struct{}),
		block:   make(chan struct{}),
	}
	svc := New(st, embed, cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	svc.Start()
	t.Cleanup(svc.Stop)

	first := addThread(t, st, "gamma", "unique-body needle")
	svc.Index(first.ID)
	select {
	case <-embed.started:
	case <-time.After(2 * time.Second):
		t.Fatal("worker never picked up the first conversation")
	}
	want := 1
	for i := 0; i < 5; i++ {
		th := addThread(t, st, "gamma", "unique-body needle")
		svc.Index(th.ID)
		want++
	}
	close(embed.block)
	done := make(chan struct{})
	go func() {
		svc.WaitIdle()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("overflowed conversations were dropped; WaitIdle hung")
	}
	if got := int(atomic.LoadInt32(&embed.n)); got != want {
		t.Fatalf("want %d embeds after overflow drain, got %d", want, got)
	}
}
