package search

import (
	"sort"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Hit is one conversation in a search response.
type Hit struct {
	ThreadID string  `json:"thread_id"`
	Title    string  `json:"title"`
	Snippet  string  `json:"snippet"`
	Score    float64 `json:"score"`
	Source   string  `json:"source"`
}

func mergeHits(fts []store.ThreadSearchHit, sem []store.ChunkHit, titles map[string]string) []Hit {
	type acc struct {
		title   string
		snippet string
		score   float64
		fts     bool
		sem     bool
	}
	byID := map[string]*acc{}
	for i, h := range fts {
		a := byID[h.ThreadID]
		if a == nil {
			a = &acc{title: h.Title, snippet: h.Snippet}
			byID[h.ThreadID] = a
		}
		a.fts = true
		a.score += 1 / float64(rrfK+i+1)
		if a.title == "" {
			a.title = h.Title
		}
		if a.snippet == "" {
			a.snippet = h.Snippet
		}
	}
	for i, h := range sem {
		a := byID[h.ThreadID]
		if a == nil {
			a = &acc{title: titles[h.ThreadID]}
			byID[h.ThreadID] = a
		}
		a.sem = true
		a.score += 1 / float64(rrfK+i+1)
		if a.title == "" {
			a.title = titles[h.ThreadID]
		}
		if a.snippet == "" {
			a.snippet = titles[h.ThreadID]
		}
	}
	out := make([]Hit, 0, len(byID))
	for id, a := range byID {
		src := "fts"
		switch {
		case a.fts && a.sem:
			src = "hybrid"
		case a.sem:
			src = "semantic"
		}
		out = append(out, Hit{
			ThreadID: id,
			Title:    a.title,
			Snippet:  a.snippet,
			Score:    a.score,
			Source:   src,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ThreadID < out[j].ThreadID
		}
		return out[i].Score > out[j].Score
	})
	return out
}
