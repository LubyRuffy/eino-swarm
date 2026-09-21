package search

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const (
	maxChunksPerThread = 64
	maxChunkRunes      = 4000
	rrfK               = 60
)

type passage struct {
	Key  string
	Text string
	Hash string
}

func passagesFor(th *store.Thread, msgs []store.Message) []passage {
	out := make([]passage, 0, 2+len(msgs))
	if title := strings.TrimSpace(th.Title); title != "" {
		out = append(out, makePassage("title", title))
	}
	if goal := strings.TrimSpace(th.Goal); goal != "" {
		out = append(out, makePassage("goal", goal))
	}
	indexable := make([]store.Message, 0, len(msgs))
	for _, m := range msgs {
		if !indexableRole(m.Role) || strings.TrimSpace(m.Content) == "" {
			continue
		}
		indexable = append(indexable, m)
	}
	if len(indexable) > maxChunksPerThread {
		indexable = indexable[len(indexable)-maxChunksPerThread:]
	}
	for _, m := range indexable {
		key := "msg:"
		if m.ID != 0 {
			key += strconv.FormatUint(uint64(m.ID), 10)
		} else {
			key += strconv.FormatInt(m.Seq, 10)
		}
		out = append(out, makePassage(key, clipRunes(m.Content, maxChunkRunes)))
	}
	return out
}

func makePassage(key, text string) passage {
	sum := sha256.Sum256([]byte(text))
	return passage{Key: key, Text: text, Hash: hex.EncodeToString(sum[:])}
}

func indexableRole(role string) bool {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "user", "assistant":
		return true
	default:
		return false
	}
}

func clipRunes(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 || utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}
