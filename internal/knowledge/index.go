package knowledge

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

type indexedPage struct {
	page    Page
	path    string
	modTime time.Time
}

type Snapshot struct {
	schema     Schema
	index      schemaIndex
	revision   string
	pages      map[string]indexedPage
	ambiguous  map[string]bool
	ordered    []string
	names      map[string][]PageRef
	outgoing   map[string][]Connection
	incoming   map[string][]Connection
	mentions   map[string][]PageRef
	unresolved map[string][]string
	issues     []Issue
}

type Status struct {
	Enabled      bool    `json:"enabled"`
	Schema       *Schema `json:"schema,omitempty"`
	Revision     string  `json:"revision,omitempty"`
	PageCount    int     `json:"page_count"`
	PendingCount int     `json:"pending_count"`
}

type Connection struct {
	Relation string   `json:"relation"`
	Label    string   `json:"label"`
	Page     *PageRef `json:"page,omitempty"`
	Target   string   `json:"target"`
	Resolved bool     `json:"resolved"`
}

type WikiObservation struct {
	Source Source `json:"source"`
	Quote  string `json:"quote"`
}

type PageDetail struct {
	Page
	Outgoing         []Connection      `json:"outgoing"`
	Incoming         []Connection      `json:"incoming"`
	Mentions         []PageRef         `json:"mentions"`
	Unresolved       []string          `json:"unresolved"`
	WikiObservations []WikiObservation `json:"wiki_observations,omitempty"`
}

type Issue struct {
	Kind     string `json:"kind"`
	PageID   string `json:"page_id,omitempty"`
	Target   string `json:"target,omitempty"`
	Relation string `json:"relation,omitempty"`
	Path     string `json:"path,omitempty"`
	Detail   string `json:"detail,omitempty"`
}

func (s *Snapshot) Enabled() bool {
	return s.revision != ""
}

func (s *Snapshot) Status(now time.Time) Status {
	status := Status{Enabled: s.Enabled(), Revision: s.revision, PageCount: len(s.pages), PendingCount: len(s.Lint(now))}
	if status.Enabled {
		schema := cloneSchema(s.schema)
		status.Schema = &schema
	}
	return status
}

func (s *Snapshot) Page(id string) (*PageDetail, error) {
	if !ValidSlug(id) {
		return nil, fmt.Errorf("%w: invalid page slug %q", ErrInvalid, id)
	}
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	item, ok := s.pages[id]
	if !ok {
		return nil, fmt.Errorf("%w: page %q", ErrNotFound, id)
	}
	page := clonePage(item.page)
	if page.Wiki != nil {
		page.Wiki.Pending = page.Wiki.Written < len(page.Wiki.Evidence)
	}
	return &PageDetail{
		Page: page, Outgoing: cloneConnections(s.outgoing[id]), Incoming: cloneConnections(s.incoming[id]),
		Mentions: append([]PageRef{}, s.mentions[id]...), Unresolved: append([]string{}, s.unresolved[id]...),
	}, nil
}

func cloneConnections(connections []Connection) []Connection {
	out := append([]Connection{}, connections...)
	for i := range out {
		if out[i].Page != nil {
			page := *out[i].Page
			out[i].Page = &page
		}
	}
	return out
}

// Search matches IDs, titles, aliases and Markdown content in deterministic
// page order. Exact normalized identity matches precede substring matches.
func (s *Snapshot) Search(query string, limit int) []PageRef {
	result := []PageRef{}
	if limit <= 0 {
		return result
	}
	key := normalizedName(query)
	matched := map[string]bool{}
	for _, page := range s.names[key] {
		result = append(result, page)
		matched[page.ID] = true
		if len(result) == limit {
			return result
		}
	}
	for _, id := range s.ordered {
		page := s.pages[id].page
		if !matched[id] && pageContains(page, key) {
			result = append(result, page.ref())
			if len(result) == limit {
				break
			}
		}
	}
	return result
}

func pageContains(page Page, needle string) bool {
	return strings.Contains(normalizedName(page.ID), needle) || strings.Contains(normalizedName(page.Title), needle) ||
		slices.ContainsFunc(page.Aliases, func(alias string) bool { return strings.Contains(normalizedName(alias), needle) }) ||
		strings.Contains(normalizedName(page.Body), needle)
}

// Lint combines structural issues from the indexed files with freshness at
// the time of the read, including when no file changed since a previous read.
func (s *Snapshot) Lint(now time.Time) []Issue {
	result := append([]Issue{}, s.issues...)
	for _, id := range s.ordered {
		item := s.pages[id]
		p := item.page
		if days := s.index.types[p.Type].FreshnessDays; days > 0 && isStale(item, days, now) {
			result = append(result, Issue{Kind: "stale", PageID: id})
		}
		for _, rel := range s.schema.Relations {
			if rel.Required && slices.Contains(rel.From, p.Type) && !slices.ContainsFunc(s.outgoing[id], func(link Connection) bool {
				return link.Relation == rel.ID && resolvedEndpoint(link, rel)
			}) {
				result = append(result, Issue{Kind: "missing_relation", PageID: id, Relation: rel.ID})
			}
		}
	}
	return result
}

func resolvedEndpoint(link Connection, rel Relation) bool {
	return link.Resolved && slices.Contains(rel.To, link.Page.Type)
}

func isStale(item indexedPage, days int, now time.Time) bool {
	base := item.page.UpdatedAt
	if base.IsZero() {
		base = item.modTime
	}
	if asOf, err := time.Parse(time.DateOnly, item.page.AsOf); err == nil {
		base = asOf
	}
	return !base.IsZero() && !now.Before(base.AddDate(0, 0, days))
}

func (s *Snapshot) Summary(budget int) string {
	if !s.Enabled() || budget <= 0 {
		return ""
	}
	var out strings.Builder
	fmt.Fprintf(&out, "%d pages.\n", len(s.pages))
	for _, kind := range s.schema.Types {
		fmt.Fprintf(&out, "- Type %s: %q\n", kind.ID, kind.Description)
	}
	for _, relation := range s.schema.Relations {
		fmt.Fprintf(&out, "- Relation %s: %q\n", relation.ID, relation.Description)
	}
	value := out.String()
	if len(value) <= budget {
		return value
	}
	for !utf8.RuneStart(value[budget]) {
		budget--
	}
	return value[:budget]
}

func (s *Snapshot) derive() {
	s.ordered = make([]string, 0, len(s.pages))
	s.names = make(map[string][]PageRef)
	s.outgoing = make(map[string][]Connection)
	s.incoming = make(map[string][]Connection)
	s.mentions = make(map[string][]PageRef)
	s.unresolved = make(map[string][]string)
	for id := range s.pages {
		s.ordered = append(s.ordered, id)
	}
	slices.SortFunc(s.ordered, func(a, b string) int {
		titleDiff := cmp.Compare(normalizedName(s.pages[a].page.Title), normalizedName(s.pages[b].page.Title))
		if titleDiff != 0 {
			return titleDiff
		}
		return cmp.Compare(a, b)
	})
	for _, id := range s.ordered {
		page := s.pages[id].page
		s.indexPageNames(page)
		s.indexPageLinks(page)
	}
}

// DuplicatePageError returns a stable reference to the existing page so the
// caller can offer an update after a refused create or rename.
type DuplicatePageError struct {
	Matched PageRef
}

func (e *DuplicatePageError) Error() string {
	return fmt.Sprintf("%s: %q (%s)", ErrDuplicate, e.Matched.Title, e.Matched.ID)
}

func (e *DuplicatePageError) Unwrap() error { return ErrDuplicate }

func (s *Snapshot) matchedPage(page Page, exclude string) (PageRef, bool) {
	for _, key := range nameKeys(page) {
		for _, ref := range s.names[key] {
			if ref.ID != exclude {
				return ref, true
			}
		}
	}
	return PageRef{}, false
}

func (s *Snapshot) indexPageNames(page Page) {
	if previous, found := s.matchedPage(page, ""); found {
		s.issues = append(s.issues, Issue{Kind: "invalid_page", PageID: page.ID, Target: previous.ID,
			Detail: "page ID, title or alias collides with another page"})
	}
	for _, key := range nameKeys(page) {
		s.names[key] = append(s.names[key], page.ref())
	}
}

func nameKeys(page Page) []string {
	keys := []string{normalizedName(page.ID)}
	for _, name := range append([]string{page.Title}, page.Aliases...) {
		key := normalizedName(name)
		if key != "" && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys
}

func (s *Snapshot) indexPageLinks(page Page) {
	for _, link := range page.Links {
		rel := s.index.relations[link.Rel]
		target, found := s.pages[link.To]
		connection := Connection{Relation: link.Rel, Label: link.Rel, Target: link.To, Resolved: found}
		if found {
			targetRef := target.page.ref()
			connection.Page = &targetRef
			if slices.Contains(rel.From, page.Type) && slices.Contains(rel.To, target.page.Type) {
				sourceRef := page.ref()
				inverse := rel.Inverse
				if inverse == "" {
					inverse = rel.ID
				}
				s.incoming[link.To] = append(s.incoming[link.To], Connection{
					Relation: rel.ID, Label: inverse, Page: &sourceRef, Target: page.ID, Resolved: true,
				})
			} else {
				s.issues = append(s.issues, Issue{Kind: "invalid_page", PageID: page.ID, Target: link.To, Relation: link.Rel,
					Detail: "relation does not accept the source and target types"})
			}
		} else if ValidSlug(link.To) {
			s.addUnresolved(page.ID, link.To, link.Rel)
		}
		s.outgoing[page.ID] = append(s.outgoing[page.ID], connection)
	}
	for _, target := range wikiTargets(page.Body) {
		if _, found := s.pages[target]; found {
			s.mentions[target] = append(s.mentions[target], page.ref())
		} else {
			s.addUnresolved(page.ID, target, "")
		}
	}
}

func (s *Snapshot) addUnresolved(source, target, relation string) {
	if !slices.Contains(s.unresolved[source], target) {
		s.unresolved[source] = append(s.unresolved[source], target)
	}
	s.issues = append(s.issues, Issue{Kind: "broken_link", PageID: source, Target: target, Relation: relation})
}
