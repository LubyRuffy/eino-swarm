package knowledge

import (
	"context"
	"fmt"
	"maps"
	"os"
	"slices"
	"strings"
)

const wikiNodeType = "wiki-node"
const wikiRelation = "wiki-related"
const wikiLocalWitnessLimit = 12

// WikiPage tracks verified observations awaiting publication into a page's
// document. The graph can display its title, links and sources immediately.
type WikiPage struct {
	Kind     string         `yaml:"kind" json:"kind"`
	Category string         `yaml:"category" json:"category"`
	Job      string         `yaml:"job" json:"-"`
	Evidence []WikiEvidence `yaml:"evidence" json:"-"`
	Written  int            `yaml:"written,omitempty" json:"-"`
	Pending  bool           `yaml:"-" json:"document_pending"`
}

type WikiEvidence struct {
	Source Source `yaml:"source"`
	Quote  string `yaml:"quote"`
	To     string `yaml:"to,omitempty"`
	Local  bool   `yaml:"local,omitempty"`
}

type WikiNode struct {
	ID, Title, Kind, Category string
	Evidence                  []WikiEvidence
}

func (s *Store) EnsureWikiSchema() error {
	snap, err := s.Snapshot()
	if err != nil {
		return err
	}
	if !snap.Enabled() {
		return ErrDisabled
	}
	var add SchemaAppend
	if _, ok := snap.index.types[wikiNodeType]; !ok {
		add.Types = []PageType{{ID: wikiNodeType, Dir: "wiki-nodes", Description: "An entity or event with attributable observations."}}
	}
	if relation, ok := snap.index.relations[wikiRelation]; !ok {
		add.Relations = []Relation{{ID: wikiRelation, From: []string{wikiNodeType}, To: []string{wikiNodeType}, Inverse: wikiRelation,
			Description: "An explicit connection supported by an observed source."}}
	} else if !slices.Contains(relation.From, wikiNodeType) || !slices.Contains(relation.To, wikiNodeType) {
		return fmt.Errorf("%w: wiki relation has incompatible endpoint types", ErrConflict)
	}
	if len(add.Types)+len(add.Relations) == 0 {
		return nil
	}
	_, err = s.AppendSchema(add, snap.revision)
	return err
}

// StageWikiNodes validates against one snapshot and publishes each Markdown
// file atomically. Retrying a partial batch reuses the same node and evidence
// identity and leaves any existing document text intact.
func (s *Store) StageWikiNodes(ctx context.Context, job string, nodes []WikiNode, stamp Stamp) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, snap, err := s.openEnabled()
	if err != nil {
		return err
	}
	defer root.Close()
	names := maps.Clone(snap.names)
	planned := make(map[string]bool, len(nodes))
	for _, node := range nodes {
		if !ValidSlug(node.ID) || planned[node.ID] || (node.Kind != "entity" && node.Kind != "event") ||
			strings.TrimSpace(node.Title) == "" || strings.TrimSpace(node.Category) == "" || len(node.Evidence) == 0 {
			return fmt.Errorf("%w: invalid wiki node %q", ErrInvalid, node.ID)
		}
		planned[node.ID] = true
	}
	pages := make([]Page, 0, len(nodes))
	for _, node := range nodes {
		page, err := wikiNodePage(snap, names, job, node, planned, stamp)
		if err != nil {
			return err
		}
		if old, ok := snap.pages[node.ID]; ok && len(old.page.Wiki.Evidence) == len(page.Wiki.Evidence) &&
			len(old.page.Links) == len(page.Links) && len(old.page.Sources) == len(page.Sources) {
			// An idempotent replay or a saturated local witness set has
			// no new content and requires no file replacement.
			continue
		}
		pages = append(pages, page)
	}
	return s.publishWikiPages(ctx, root, snap, pages)
}

func wikiNodePage(snap *Snapshot, names map[string][]PageRef, job string, node WikiNode, planned map[string]bool, stamp Stamp) (Page, error) {
	page := Page{ID: node.ID, Type: wikiNodeType, Title: node.Title,
		Wiki: &WikiPage{Kind: node.Kind, Category: node.Category}}
	if found, ok := snap.pages[node.ID]; ok {
		page = clonePage(found.page)
		if page.Wiki == nil || page.Type != wikiNodeType || page.Wiki.Kind != node.Kind || page.Wiki.Category != node.Category {
			return Page{}, fmt.Errorf("%w: wiki node %q conflicts with an existing page", ErrConflict, node.ID)
		}
	} else {
		// The page index requires unique titles even when separate entities
		// legitimately have the same name.
		if len(names[normalizedName(page.Title)]) != 0 {
			page.Title += " (" + node.Category + " · " + node.ID + ")"
		}
		for _, key := range nameKeys(page) {
			if len(names[key]) != 0 {
				return Page{}, fmt.Errorf("%w: wiki node %q collides with a page name", ErrDuplicate, node.ID)
			}
			names[key] = []PageRef{page.ref()}
		}
	}
	page.UpdatedAt, page.UpdatedTurn, page.UpdatedThread = stamp.UpdatedAt.UTC(), stamp.UpdatedTurn, stamp.UpdatedThread
	page.Wiki.Job = job
	evidenceSeen := make(map[WikiEvidence]bool, len(page.Wiki.Evidence)+len(node.Evidence))
	sourceSeen := make(map[Source]bool, len(page.Sources))
	localWitnesses := make(map[string]int)
	for _, old := range page.Wiki.Evidence {
		evidenceSeen[old] = true
		if old.Local {
			localWitnesses[old.To]++
		}
	}
	for _, source := range page.Sources {
		sourceSeen[source] = true
	}
	for _, evidence := range node.Evidence {
		if !validSource(evidence.Source) || strings.TrimSpace(evidence.Quote) == "" {
			return Page{}, fmt.Errorf("%w: empty or invalid wiki evidence for %q", ErrInvalid, page.ID)
		}
		if evidence.To != "" {
			if evidence.To == page.ID || (!planned[evidence.To] && snap.pages[evidence.To].page.Type != wikiNodeType) {
				return Page{}, fmt.Errorf("%w: invalid wiki link from %q to %q", ErrInvalid, page.ID, evidence.To)
			}
			link := Link{Rel: wikiRelation, To: evidence.To}
			if !slices.Contains(page.Links, link) {
				page.Links = append(page.Links, link)
			}
		}
		if evidenceSeen[evidence] || (evidence.Local && localWitnesses[evidence.To] >= wikiLocalWitnessLimit) {
			continue
		}
		if !sourceSeen[evidence.Source] {
			sourceSeen[evidence.Source] = true
			page.Sources = append(page.Sources, evidence.Source)
		}
		evidenceSeen[evidence] = true
		if evidence.Local {
			localWitnesses[evidence.To]++
		}
		page.Wiki.Evidence = append(page.Wiki.Evidence, evidence)
	}
	return page, snap.validatePageWrite(page)
}

// WikiDocumentProgress is derived from the same files that contain the graph.
// An acknowledged document cannot be written twice on recovery.
func (s *Snapshot) WikiDocumentProgress(job string) (total, written int) {
	for _, item := range s.pages {
		if wiki := item.page.Wiki; wiki != nil && wiki.Job == job {
			total++
			if wiki.Written == len(wiki.Evidence) {
				written++
			}
		}
	}
	return total, written
}

func (s *Store) WriteWikiDocuments(ctx context.Context, job string, limit int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	root, snap, err := s.openEnabled()
	if err != nil {
		return err
	}
	defer root.Close()
	var pages []Page
	for _, id := range snap.ordered {
		current := snap.pages[id].page
		if wiki := current.Wiki; wiki == nil || wiki.Job != job || wiki.Written >= len(wiki.Evidence) {
			continue
		}
		if err := contentError(current, snap.index); err != nil {
			return err
		}
		page := clonePage(current)
		appendWikiObservations(&page)
		pages = append(pages, page)
		if len(pages) >= limit {
			break
		}
	}
	return s.publishWikiPages(ctx, root, snap, pages)
}

func appendWikiObservations(page *Page) {
	type excerpt struct {
		source Source
		quote  string
	}
	seen := make(map[excerpt]bool)
	var doc strings.Builder
	doc.WriteString(strings.TrimRight(page.Body, "\n"))
	started := false
	for i, evidence := range page.Wiki.Evidence {
		key := excerpt{evidence.Source, evidence.Quote}
		if seen[key] {
			continue
		}
		seen[key] = true
		if i < page.Wiki.Written {
			continue
		}
		if !started {
			doc.WriteString("\n\n## Source observations\n")
			started = true
		}
		doc.WriteByte('\n')
		for line := range strings.SplitSeq(evidence.Quote, "\n") {
			doc.WriteString("> ")
			doc.WriteString(wikiQuoteText.Replace(line))
			doc.WriteByte('\n')
		}
		fmt.Fprintf(&doc, "\nSource %s #%d (%s)\n", evidence.Source.Kind, evidence.Source.Seq, evidence.Source.ThreadID)
	}
	page.Body = strings.TrimLeft(doc.String(), "\n")
	page.Wiki.Written = len(page.Wiki.Evidence)
}

var wikiQuoteText = strings.NewReplacer(
	"\\", "\\\\", "[", "\\[", "]", "\\]", "<", "&lt;", ">", "&gt;", "&", "&amp;",
	"*", "\\*", "_", "\\_", "`", "\\`",
)

func (s *Store) publishWikiPages(ctx context.Context, root *os.Root, snap *Snapshot, pages []Page) error {
	if len(pages) == 0 {
		return nil
	}
	folder, err := openTypeDir(root, snap.index.types[wikiNodeType].Dir, true)
	if err != nil {
		return err
	}
	defer folder.Close()
	for _, page := range pages {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		data, err := marshalPage(page)
		if err != nil {
			return err
		}
		if len(data) > maxFileBytes {
			return fmt.Errorf("%w: wiki page %q exceeds the file size limit", ErrInvalid, page.ID)
		}
		s.cached = nil
		if err := publishFile(folder, page.ID+".md", page.Revision, data); err != nil {
			return err
		}
	}
	return nil
}
