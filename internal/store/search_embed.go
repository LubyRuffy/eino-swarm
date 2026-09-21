package store

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"

	"gorm.io/gorm"
)

// SearchChunk is one embedded passage of a conversation. Vectors are
// little-endian float32 so a model change is a different row, not a reshape.
type SearchChunk struct {
	ID       uint   `gorm:"primaryKey"`
	ThreadID string `gorm:"uniqueIndex:uid_search_chunk;size:64"`
	ChunkKey string `gorm:"uniqueIndex:uid_search_chunk;size:128"`
	Model    string `gorm:"uniqueIndex:uid_search_chunk;size:200"`
	TextHash string `gorm:"size:64"`
	Dim      int
	Vector   []byte
}

func (SearchChunk) TableName() string { return "search_chunks" }

// ChunkHit is one passage scored against a query vector.
type ChunkHit struct {
	ThreadID string
	ChunkKey string
	Score    float64
}

// UpsertSearchChunk stores or replaces one passage vector.
func (s *Store) UpsertSearchChunk(c SearchChunk) error {
	var existing SearchChunk
	err := s.db.Where("thread_id = ? AND chunk_key = ? AND model = ?", c.ThreadID, c.ChunkKey, c.Model).
		First(&existing).Error
	if err == nil {
		return s.db.Model(&existing).Updates(map[string]any{
			"text_hash": c.TextHash,
			"dim":       c.Dim,
			"vector":    c.Vector,
		}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("store: lookup search chunk: %w", err)
	}
	if err := s.db.Create(&c).Error; err != nil {
		return fmt.Errorf("store: insert search chunk: %w", err)
	}
	return nil
}

// SearchChunkByKey loads the cached vector for one passage and model.
func (s *Store) SearchChunkByKey(threadID, chunkKey, model string) (*SearchChunk, error) {
	var c SearchChunk
	err := s.db.Where("thread_id = ? AND chunk_key = ? AND model = ?", threadID, chunkKey, model).
		First(&c).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: get search chunk: %w", err)
	}
	return &c, nil
}

// ListSearchChunksForModel returns every live (non-archived) vector for ranking.
func (s *Store) ListSearchChunksForModel(model string) ([]SearchChunk, error) {
	var out []SearchChunk
	err := s.db.Raw(`
		SELECT c.id, c.thread_id, c.chunk_key, c.model, c.text_hash, c.dim, c.vector
		FROM search_chunks c
		JOIN threads t ON t.id = c.thread_id
		WHERE c.model = ? AND t.archived = ?`, model, false).Scan(&out).Error
	if err != nil {
		return nil, fmt.Errorf("store: list search chunks: %w", err)
	}
	return out, nil
}

// SearchSimilarChunks ranks passages by cosine similarity. Higher is better.
func (s *Store) SearchSimilarChunks(model string, query []float32, limit int) ([]ChunkHit, error) {
	if len(query) == 0 {
		return nil, nil
	}
	limit = clampSearchLimit(limit)
	chunks, err := s.ListSearchChunksForModel(model)
	if err != nil {
		return nil, err
	}
	best := map[string]ChunkHit{}
	for _, c := range chunks {
		vec := DecodeVector(c.Vector)
		if len(vec) != len(query) {
			continue
		}
		score := float64(Cosine(query, vec))
		prev, ok := best[c.ThreadID]
		if !ok || score > prev.Score {
			best[c.ThreadID] = ChunkHit{ThreadID: c.ThreadID, ChunkKey: c.ChunkKey, Score: score}
		}
	}
	out := make([]ChunkHit, 0, len(best))
	for _, v := range best {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// EncodeVector packs float32s little-endian.
func EncodeVector(v []float32) []byte {
	out := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(out[i*4:], math.Float32bits(x))
	}
	return out
}

// DecodeVector unpacks EncodeVector's layout. Odd trailing bytes are dropped.
func DecodeVector(b []byte) []float32 {
	n := len(b) / 4
	out := make([]float32, n)
	for i := 0; i < n; i++ {
		out[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return out
}

// Cosine is 0 when either vector is empty or zero.
func Cosine(a, b []float32) float32 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		av, bv := float64(a[i]), float64(b[i])
		dot += av * bv
		na += av * av
		nb += bv * bv
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return float32(dot / (math.Sqrt(na) * math.Sqrt(nb)))
}
