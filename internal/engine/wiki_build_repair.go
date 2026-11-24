package engine

import (
	"cmp"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

type wikiLinkPair struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (pair wikiLinkPair) canonical() wikiLinkPair {
	return wikiLinkPair{From: min(pair.From, pair.To), To: max(pair.From, pair.To)}
}

func wikiPairOrder(a, b wikiLinkPair) int {
	return cmp.Or(strings.Compare(a.From, b.From), strings.Compare(a.To, b.To))
}

// wikiDraftRepair holds accepted items only within one model review. The
// caller cannot publish any of them until coverage and all decisions settle.
type wikiDraftRepair struct {
	reviewed     map[int64]bool
	nodes        map[string]wikiModelNode
	invalidNodes map[string]error
	links        map[wikiModelLink]bool
	pendingLinks map[wikiLinkPair]wikiModelLink
}

func newWikiDraftRepair(first wikiProposal) (*wikiDraftRepair, error) {
	if first.Reviewed == nil || first.Nodes == nil || first.Links == nil {
		return nil, errors.New("the first submission requires reviewed, nodes and links arrays")
	}
	return &wikiDraftRepair{
		reviewed: make(map[int64]bool), nodes: make(map[string]wikiModelNode),
		invalidNodes: make(map[string]error), links: make(map[wikiModelLink]bool),
		pendingLinks: make(map[wikiLinkPair]wikiModelLink),
	}, nil
}

func wikiSubmitted[T any](items *[]T) []T {
	if items == nil {
		return nil
	}
	return *items
}

func (r *wikiDraftRepair) checkPatch(p wikiProposal) error {
	keys := make(map[string]bool)
	for _, node := range wikiSubmitted(p.Nodes) {
		if !wikiLabel(node.Key, 128) || keys[node.Key] {
			return errors.New("every submitted node needs a distinct valid key")
		}
		if old, present := r.nodes[node.Key]; present && r.invalidNodes[node.Key] == nil && old != node {
			return fmt.Errorf("node %q was already accepted; omit that key when repairing the draft", node.Key)
		}
		keys[node.Key] = true
	}
	for _, key := range p.DropNodes {
		if keys[key] || r.invalidNodes[key] == nil {
			return fmt.Errorf("drop_nodes may withdraw only a pending node that the patch does not replace: %q", key)
		}
		keys[key] = true
	}
	pairs := make(map[wikiLinkPair]bool)
	for _, link := range wikiSubmitted(p.Links) {
		if !wikiLabel(link.From, 128) || !wikiLabel(link.To, 128) {
			return errors.New("every submitted link needs valid endpoint keys")
		}
		pairs[(wikiLinkPair{link.From, link.To}).canonical()] = true
	}
	for _, pair := range p.DropLinks {
		pair = pair.canonical()
		if _, pending := r.pendingLinks[pair]; pairs[pair] || !pending {
			return fmt.Errorf("drop_links may withdraw only a pending link that the patch does not replace: %q to %q", pair.From, pair.To)
		}
		pairs[pair] = true
	}
	return nil
}

func (r *wikiDraftRepair) apply(p wikiProposal, visible map[int64]store.Event) (*wikiProposal, error) {
	if err := r.checkPatch(p); err != nil {
		return nil, err
	}
	var issues []string
	seen := make(map[int64]bool)
	for _, seq := range wikiSubmitted(p.Reviewed) {
		if _, exists := visible[seq]; !exists || seen[seq] {
			issues = append(issues, fmt.Sprintf("reviewed contains an unknown or repeated source number %d", seq))
			continue
		}
		seen[seq], r.reviewed[seq] = true, true
	}
	var missing []int64
	for _, seq := range slices.Sorted(maps.Keys(visible)) {
		if !r.reviewed[seq] {
			missing = append(missing, seq)
		}
	}
	if len(missing) > 0 {
		issues = append(issues, fmt.Sprintf("Missing source numbers: %v", missing))
	}

	for _, key := range p.DropNodes {
		delete(r.nodes, key)
		delete(r.invalidNodes, key)
	}
	for _, node := range wikiSubmitted(p.Nodes) {
		r.nodes[node.Key] = node
		if err := wikiNodeEvidenceError(node, visible); err != nil {
			r.invalidNodes[node.Key] = err
		} else {
			delete(r.invalidNodes, node.Key)
		}
	}
	names := make(map[string]string, len(r.nodes))
	for _, key := range slices.Sorted(maps.Keys(r.nodes)) {
		if err := r.invalidNodes[key]; err != nil {
			issues = append(issues, fmt.Sprintf("node %q %v", key, err))
		} else {
			names[key] = r.nodes[key].Title
		}
	}
	for _, pair := range p.DropLinks {
		delete(r.pendingLinks, pair.canonical())
	}
	for _, link := range wikiSubmitted(p.Links) {
		pair := (wikiLinkPair{link.From, link.To}).canonical()
		link.From, link.To = pair.From, pair.To
		if wikiLinkSupported(link, names, visible) {
			r.links[link] = true
			delete(r.pendingLinks, pair)
		} else {
			r.pendingLinks[pair] = link
		}
	}
	for _, pair := range slices.SortedFunc(maps.Keys(r.pendingLinks), wikiPairOrder) {
		link := r.pendingLinks[pair]
		if wikiLinkSupported(link, names, visible) {
			r.links[link] = true
			delete(r.pendingLinks, pair)
		} else {
			issues = append(issues, fmt.Sprintf("link %q to %q needs two supported nodes and one exact quote containing both names", pair.From, pair.To))
		}
	}
	if len(issues) > 0 {
		return nil, fmt.Errorf("the host retained valid items and source acknowledgements; repair only the unresolved items: %s", strings.Join(issues, "; "))
	}
	result := r.proposal()
	if err := result.validate(visible); err != nil {
		return nil, err
	}
	return result, nil
}

func (r *wikiDraftRepair) proposal() *wikiProposal {
	reviewed := slices.Sorted(maps.Keys(r.reviewed))
	nodes := make([]wikiModelNode, 0, len(r.nodes))
	for _, key := range slices.Sorted(maps.Keys(r.nodes)) {
		nodes = append(nodes, r.nodes[key])
	}
	links := slices.SortedFunc(maps.Keys(r.links), func(a, b wikiModelLink) int {
		return cmp.Or(
			wikiPairOrder(wikiLinkPair{a.From, a.To}, wikiLinkPair{b.From, b.To}),
			cmp.Compare(a.Evidence.Seq, b.Evidence.Seq),
			strings.Compare(a.Evidence.Quote, b.Evidence.Quote),
		)
	})
	return &wikiProposal{Reviewed: &reviewed, Nodes: &nodes, Links: &links}
}
