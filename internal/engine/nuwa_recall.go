package engine

import (
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Task recall ranks reported metadata, not truth or authorization. Detail stays
// on demand, and unmatched records remain eligible for the same preview budget.
func orderNuwaContextNodes(nodes []store.NuwaNode, task string) []store.NuwaNode {
	type rankedNode struct {
		node  store.NuwaNode
		score float64
	}
	ranked := make([]rankedNode, len(nodes))
	for i, node := range nodes {
		ranked[i].node = node
	}
	query := nuwaRecallTerms(task)
	terms := make([]string, 0, len(query))
	for term := range query {
		terms = append(terms, term)
	}
	// Map iteration must not change floating-point summation or preview order.
	slices.Sort(terms)
	if len(terms) > 0 {
		documents := make([]map[string]struct{}, len(nodes))
		frequency := make(map[string]int, len(terms))
		for i, node := range nodes {
			documents[i] = nuwaRecallTerms(node.Title + " " + node.Summary)
			for _, term := range terms {
				if _, ok := documents[i][term]; ok {
					frequency[term]++
				}
			}
		}
		for i, document := range documents {
			for _, term := range terms {
				if _, ok := document[term]; ok {
					ranked[i].score += math.Log(1 + float64(len(nodes))/float64(1+frequency[term]))
				}
			}
		}
	}
	slices.SortFunc(ranked, func(a, b rankedNode) int {
		if a.score > b.score {
			return -1
		}
		if a.score < b.score {
			return 1
		}
		if order := b.node.UpdatedAt.Compare(a.node.UpdatedAt); order != 0 {
			return order
		}
		return strings.Compare(a.node.ID, b.node.ID)
	})
	ordered := slices.Clone(nodes)
	for i, entry := range ranked {
		ordered[i] = entry.node
	}
	return ordered
}

// Like memory's overlap heuristic, use whole words and overlapping CJK bigrams.
// Unicode letters and numbers stay intact; punctuation separates unrelated words.
func nuwaRecallTerms(text string) map[string]struct{} {
	terms := make(map[string]struct{})
	var word, cjk []rune
	flushWord := func() {
		if len(word) > 0 {
			terms[string(word)] = struct{}{}
		}
		word = word[:0]
	}
	flushCJK := func() {
		if len(cjk) == 1 {
			terms[string(cjk)] = struct{}{}
		}
		for i := 0; i+1 < len(cjk); i++ {
			terms[string(cjk[i:i+2])] = struct{}{}
		}
		cjk = cjk[:0]
	}
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r):
			flushWord()
			cjk = append(cjk, r)
		case unicode.IsLetter(r) || unicode.IsNumber(r):
			flushCJK()
			word = append(word, r)
		default:
			flushWord()
			flushCJK()
		}
	}
	flushWord()
	flushCJK()
	return terms
}
