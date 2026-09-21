package store

import (
	"errors"
	"math"
	"testing"
)

func TestCosineIsOneForIdenticalVectorsAndZeroForEmpty(t *testing.T) {
	if Cosine(nil, []float32{1}) != 0 || Cosine([]float32{1}, nil) != 0 {
		t.Fatal("empty vectors must score 0")
	}
	if Cosine([]float32{1, 0}, []float32{1}) != 0 {
		t.Fatal("mismatched length must score 0")
	}
	if got := Cosine([]float32{1, 0}, []float32{1, 0}); math.Abs(float64(got-1)) > 1e-6 {
		t.Fatalf("identical cosine=%v", got)
	}
	if got := Cosine([]float32{1, 0}, []float32{0, 1}); math.Abs(float64(got)) > 1e-6 {
		t.Fatalf("orthogonal cosine=%v", got)
	}
}

func TestEncodeVectorRoundTrips(t *testing.T) {
	in := []float32{0, -1.5, 2}
	got := DecodeVector(EncodeVector(in))
	if len(got) != 3 || got[1] != in[1] || got[2] != in[2] {
		t.Fatalf("round trip %+v → %+v", in, got)
	}
	if DecodeVector([]byte{1, 2, 3}) != nil && len(DecodeVector([]byte{1, 2, 3})) != 0 {
		// 3 bytes is not a full float32; the leftover is dropped.
	}
	if n := len(DecodeVector([]byte{1, 2, 3})); n != 0 {
		t.Fatalf("trailing junk must be dropped, got %d", n)
	}
}

func TestSearchSimilarChunksRanksTheCloserThread(t *testing.T) {
	s := open(t)
	near := &Thread{Title: "near", ProviderID: "p"}
	far := &Thread{Title: "far", ProviderID: "p"}
	if err := s.CreateThread(near); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateThread(far); err != nil {
		t.Fatal(err)
	}
	model := "named-embed"
	if err := s.UpsertSearchChunk(SearchChunk{
		ThreadID: near.ID, ChunkKey: "title", Model: model, TextHash: "a",
		Dim: 2, Vector: EncodeVector([]float32{1, 0}),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSearchChunk(SearchChunk{
		ThreadID: far.ID, ChunkKey: "title", Model: model, TextHash: "b",
		Dim: 2, Vector: EncodeVector([]float32{0, 1}),
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchSimilarChunks(model, []float32{1, 0}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 2 || hits[0].ThreadID != near.ID {
		t.Fatalf("want the aligned vector first, got %+v", hits)
	}
	if hits[0].Score <= hits[1].Score {
		t.Fatalf("cosine order inverted: %+v", hits)
	}
}

func TestUpsertSearchChunkSkipsReembedWhenHashMatchesOnRead(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "alpha", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	c := SearchChunk{
		ThreadID: th.ID, ChunkKey: "title", Model: "named-embed",
		TextHash: "same", Dim: 1, Vector: EncodeVector([]float32{1}),
	}
	if err := s.UpsertSearchChunk(c); err != nil {
		t.Fatal(err)
	}
	c.Vector = EncodeVector([]float32{2})
	if err := s.UpsertSearchChunk(c); err != nil {
		t.Fatal(err)
	}
	got, err := s.SearchChunkByKey(th.ID, "title", "named-embed")
	if err != nil {
		t.Fatal(err)
	}
	vec := DecodeVector(got.Vector)
	if len(vec) != 1 || vec[0] != 2 {
		t.Fatalf("upsert did not replace the vector: %+v", vec)
	}
	if _, err := s.SearchChunkByKey(th.ID, "missing", "named-embed"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestDeleteThreadDropsEmbeddingChunks(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "alpha", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSearchChunk(SearchChunk{
		ThreadID: th.ID, ChunkKey: "title", Model: "named-embed",
		TextHash: "a", Dim: 1, Vector: EncodeVector([]float32{1}),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteThread(th.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SearchChunkByKey(th.ID, "title", "named-embed"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("chunk survived delete: %v", err)
	}
}

func TestDeleteSearchChunksNotInDropsStalePassages(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "alpha", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	model := "named-embed"
	for _, key := range []string{"title", "msg:1", "msg:2"} {
		if err := s.UpsertSearchChunk(SearchChunk{
			ThreadID: th.ID, ChunkKey: key, Model: model,
			TextHash: key, Dim: 1, Vector: EncodeVector([]float32{1}),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.DeleteSearchChunksNotIn(th.ID, model, []string{"title"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SearchChunkByKey(th.ID, "title", model); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SearchChunkByKey(th.ID, "msg:1", model); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale passage survived: %v", err)
	}
}

func TestDeleteSearchChunksNotModelDropsTheOldPin(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "alpha", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSearchChunk(SearchChunk{
		ThreadID: th.ID, ChunkKey: "title", Model: "old-embed",
		TextHash: "a", Dim: 1, Vector: EncodeVector([]float32{1}),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSearchChunk(SearchChunk{
		ThreadID: th.ID, ChunkKey: "title", Model: "named-embed",
		TextHash: "b", Dim: 1, Vector: EncodeVector([]float32{1}),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSearchChunksNotModel("named-embed"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SearchChunkByKey(th.ID, "title", "old-embed"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("old pin survived: %v", err)
	}
	if _, err := s.SearchChunkByKey(th.ID, "title", "named-embed"); err != nil {
		t.Fatal(err)
	}
}

func TestSearchSimilarChunksSkipsMismatchedDimensions(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "alpha", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertSearchChunk(SearchChunk{
		ThreadID: th.ID, ChunkKey: "title", Model: "named-embed",
		TextHash: "a", Dim: 3, Vector: EncodeVector([]float32{1, 0, 0}),
	}); err != nil {
		t.Fatal(err)
	}
	hits, err := s.SearchSimilarChunks("named-embed", []float32{1, 0}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 0 {
		t.Fatalf("mismatched dim must not rank: %+v", hits)
	}
	empty, err := s.SearchSimilarChunks("named-embed", nil, 10)
	if err != nil || empty != nil {
		t.Fatalf("empty query: %v %+v", err, empty)
	}
}

func TestCosineZeroVectorIsZero(t *testing.T) {
	if Cosine([]float32{0, 0}, []float32{1, 0}) != 0 {
		t.Fatal("a zero vector has no direction")
	}
}

func TestDeleteSearchChunksNotInWithEmptyKeepDropsTheThread(t *testing.T) {
	s := open(t)
	th := &Thread{Title: "alpha", ProviderID: "p"}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	model := "named-embed"
	if err := s.UpsertSearchChunk(SearchChunk{
		ThreadID: th.ID, ChunkKey: "title", Model: model,
		TextHash: "a", Dim: 1, Vector: EncodeVector([]float32{1}),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteSearchChunksNotIn(th.ID, model, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SearchChunkByKey(th.ID, "title", model); !errors.Is(err, ErrNotFound) {
		t.Fatalf("empty keep must drop every passage: %v", err)
	}
}
