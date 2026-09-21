package search

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Search ranks conversations. Keyword search always runs. When embeddings are
// on, the query is vectorised and fused with FTS via reciprocal rank fusion.
// A failed embedding call falls back to keywords rather than failing the palette.
func (s *Service) Search(ctx context.Context, q string, limit int) (Result, error) {
	q = strings.TrimSpace(q)
	limit = clampLimit(limit)
	res := Result{Query: q, Hits: []Hit{}}
	if q == "" {
		return res, nil
	}
	fts, err := s.store.SearchThreads(q, limit)
	if err != nil {
		return res, err
	}
	if !s.semantic() {
		res.Hits = clipHits(mergeHits(fts, nil, nil), limit)
		return res, nil
	}
	vecs, err := s.embed.Embed(ctx, s.providerID(), s.pinModel(), []string{q})
	if err != nil || len(vecs) != 1 || len(vecs[0]) == 0 {
		s.log.Warn("embedding the query failed; falling back to keywords", "err", err)
		res.Hits = clipHits(mergeHits(fts, nil, nil), limit)
		return res, nil
	}
	sem, err := s.store.SearchSimilarChunks(s.pinModel(), vecs[0], limit)
	if err != nil {
		return res, err
	}
	titles := map[string]string{}
	for _, h := range sem {
		th, err := s.store.GetThread(h.ThreadID)
		if err != nil {
			continue
		}
		titles[h.ThreadID] = th.Title
	}
	res.Embedding = true
	res.Hits = clipHits(mergeHits(fts, sem, titles), limit)
	return res, nil
}

func clipHits(hits []Hit, limit int) []Hit {
	if len(hits) > limit {
		return hits[:limit]
	}
	return hits
}

func (s *Service) embedThread(ctx context.Context, id string) error {
	if !s.semantic() {
		return nil
	}
	th, err := s.store.GetThread(id)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}
	msgs, err := s.store.ListMessages(id)
	if err != nil {
		return err
	}
	model := s.pinModel()
	parts := passagesFor(th, msgs)
	keep := make([]string, 0, len(parts))
	pending := make([]passage, 0, len(parts))
	for _, p := range parts {
		keep = append(keep, p.Key)
		existing, err := s.store.SearchChunkByKey(id, p.Key, model)
		if err == nil && existing.TextHash == p.Hash && len(existing.Vector) > 0 {
			continue
		}
		pending = append(pending, p)
	}
	if len(pending) > 0 {
		texts := make([]string, len(pending))
		for i, p := range pending {
			texts[i] = p.Text
		}
		vecs, err := s.embed.Embed(ctx, s.providerID(), model, texts)
		if err != nil {
			return err
		}
		if len(vecs) != len(pending) {
			return fmt.Errorf("search: embedder returned %d vectors for %d passages", len(vecs), len(pending))
		}
		for i, p := range pending {
			if err := s.store.UpsertSearchChunk(store.SearchChunk{
				ThreadID: id,
				ChunkKey: p.Key,
				Model:    model,
				TextHash: p.Hash,
				Dim:      len(vecs[i]),
				Vector:   store.EncodeVector(vecs[i]),
			}); err != nil {
				return err
			}
		}
	}
	return s.store.DeleteSearchChunksNotIn(id, model, keep)
}
