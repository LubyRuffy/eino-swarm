package engine

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/knowledge"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// KnowledgeProjectDir is the host-managed path for an already validated project.
func (e *Engine) KnowledgeProjectDir(projectID string) string {
	return e.cfg.ProjectKnowledgeDir(projectID)
}

// ProjectKnowledge returns the shared knowledge store of an existing project.
func (e *Engine) ProjectKnowledge(projectID string) (*knowledge.Store, error) {
	if !filepath.IsLocal(projectID) || projectID == "." || strings.ContainsAny(projectID, "/\\:") {
		return nil, store.ErrNotFound
	}

	p, err := e.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	if current, ok := e.knowledge.Load(p.ID); ok {
		return current.(*knowledge.Store), nil
	}
	kb, _ := e.knowledge.LoadOrStore(p.ID, knowledge.New(e.KnowledgeProjectDir(p.ID)))
	return kb.(*knowledge.Store), nil
}

func (e *Engine) snapshotProjectKnowledge(pc *projectContext) {
	if !e.cfg.SwarmSnapshot().Knowledge {
		return
	}
	kb, err := e.ProjectKnowledge(pc.project.ID)
	if err == nil {
		var snapshot *knowledge.Snapshot
		snapshot, err = kb.Snapshot()
		if err == nil && !snapshot.Enabled() && !pc.planning {
			snapshot, err = e.enableKnowledgeFiles(context.Background(), pc.project.ID, kb)
		}
		if err == nil && snapshot.Enabled() {
			pc.knowledge = kb
			pc.knowledgeSummary = snapshot.Summary(knowledgeSchemaPromptBudget)
		}
	}
	if err != nil {
		e.log.Warn("could not read a project's knowledge base", "project", pc.project.ID, "err", err)
	}
}

func (pc *projectContext) hasKnowledge() bool {
	return pc != nil && pc.knowledge != nil
}

// EnableProjectKnowledge materializes the starter schema once for a live project.
func (e *Engine) EnableProjectKnowledge(projectID string) (*knowledge.Snapshot, error) {
	if !e.cfg.SwarmSnapshot().Knowledge {
		return nil, knowledge.ErrDisabled
	}
	done, err := e.beginAdmission()
	if err != nil {
		return nil, err
	}
	defer done()
	e.workspaceMu.Lock()
	defer e.workspaceMu.Unlock()
	kb, err := e.ProjectKnowledge(projectID)
	if err != nil {
		return nil, err
	}
	return e.enableKnowledgeFiles(context.Background(), projectID, kb)
}

func (e *Engine) enableKnowledgeFiles(ctx context.Context, projectID string, kb *knowledge.Store) (*knowledge.Snapshot, error) {
	snap, err := kb.Snapshot()
	if err != nil || snap.Enabled() {
		return snap, err
	}
	if err := e.store.ForgetWikiSourceScans(ctx, projectID); err != nil {
		return nil, err
	}
	return kb.Enable()
}

// ProjectKnowledgePage resolves the live status of its references for HTTP.
func (e *Engine) ProjectKnowledgePage(ctx context.Context, projectID, id string) (*knowledge.PageDetail, error) {
	if !e.cfg.SwarmSnapshot().Knowledge {
		return nil, knowledge.ErrDisabled
	}
	kb, err := e.ProjectKnowledge(projectID)
	if err != nil {
		return nil, err
	}
	snap, err := kb.Snapshot()
	if err != nil {
		return nil, err
	}
	return e.knowledgePage(ctx, projectID, snap, id)
}

func (e *Engine) knowledgePage(ctx context.Context, projectID string, snap *knowledge.Snapshot, id string) (*knowledge.PageDetail, error) {
	page, err := snap.Page(id)
	if err != nil {
		return nil, err
	}
	if err := e.enrichKnowledgeSources(ctx, projectID, page.Sources); err != nil {
		return nil, err
	}
	if err := e.addWikiObservations(ctx, projectID, page); err != nil {
		return nil, err
	}
	page.UpdatedThread, err = e.knowledgeOrigin(ctx, projectID, page.UpdatedThread, page.UpdatedTurn)
	if err != nil {
		return nil, err
	}
	return page, nil
}

// Quotes are plain source excerpts. A moved, edited or rewound source
// stays in the source list with its unavailable marker and no excerpt.
func (e *Engine) addWikiObservations(ctx context.Context, projectID string, page *knowledge.PageDetail) error {
	if page.Wiki == nil {
		return nil
	}
	const maxObservations, maxQuoteChecks, maxQuoteBytes = 24, 48, 1024
	available := make(map[store.HistoryReference]knowledge.Source, len(page.Sources))
	for _, source := range page.Sources {
		if source.Available != nil && *source.Available && source.RecordID != "" && source.TurnID != "" {
			available[historyReference(source)] = source
		}
	}
	checks := 0
	for _, evidence := range page.Wiki.Evidence {
		ref := historyReference(evidence.Source)
		source, found := available[ref]
		if !found || evidence.Source.RecordID != source.RecordID || strings.TrimSpace(evidence.Quote) == "" {
			continue
		}
		quote := evidence.Quote
		if len(quote) > maxQuoteBytes {
			end := maxQuoteBytes
			for !utf8.RuneStart(quote[end]) {
				end--
			}
			quote = quote[:end]
		}
		if checks >= maxQuoteChecks {
			break
		}
		checks++
		identity, err := e.store.ReadProjectWikiQuoteIdentity(ctx, projectID, ref, quote)
		if err != nil {
			return err
		}
		if identity != nil && identity.CreatedAt.Valid && identity.TurnID == source.TurnID &&
			source.Timestamp.Equal(identity.CreatedAt.Time) && source.RecordID == sourceRecordID(identity) {
			page.WikiObservations = append(page.WikiObservations, knowledge.WikiObservation{Source: source, Quote: quote})
			delete(available, ref)
			if len(page.WikiObservations) >= maxObservations {
				break
			}
		}
	}
	return nil
}

func (e *Engine) knowledgeOrigin(ctx context.Context, projectID, threadID, turnID string) (string, error) {
	if turnID == "" {
		return "", nil
	}
	turn, err := e.store.GetTurnContext(ctx, turnID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	// A stamped thread is checked against its current project below. Manual
	// distillation can use retained turns that predate a conversation move.
	if (threadID != "" && turn.ThreadID != threadID) || (threadID == "" && turn.ProjectID != projectID) {
		return "", nil
	}
	thread, err := e.store.GetThreadContext(ctx, turn.ThreadID)
	if errors.Is(err, store.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if thread.ProjectID != projectID {
		return "", nil
	}
	return thread.ID, nil
}

type knowledgeSourceRef struct {
	URI      string `json:"uri"`
	Kind     string `json:"kind"`
	ThreadID string `json:"thread_id"`
	Seq      int64  `json:"seq"`
}

func (e *Engine) resolveKnowledgeSources(ctx context.Context, projectID string, inputs []knowledgeSourceRef) ([]knowledge.Source, error) {
	sources := make([]knowledge.Source, len(inputs))
	var refs []store.HistoryReference
	for i, input := range inputs {
		sources[i] = knowledge.Source{URI: input.URI, Kind: input.Kind, ThreadID: input.ThreadID, Seq: input.Seq}
		if input.URI != "" {
			if input.Kind != "" || input.ThreadID != "" || input.Seq != 0 {
				return nil, fmt.Errorf("%w: source %d must contain only a URI or history locator", knowledge.ErrInvalid, i+1)
			}
			sources[i].Classification = "external"
			continue
		}
		refs = append(refs, historyReference(sources[i]))
	}
	rows, err := e.store.ReadProjectHistoryIdentities(ctx, projectID, refs)
	if err != nil {
		return nil, err
	}
	for i := range sources {
		source := &sources[i]
		if source.URI != "" {
			continue
		}
		row := rows[0]
		rows = rows[1:]
		if row == nil || !row.CreatedAt.Valid || row.CreatedAt.Time.IsZero() {
			return nil, fmt.Errorf("%w: source %d (%s, %s, %d)", store.ErrHistorySourceUnavailable, i+1, source.Kind, source.ThreadID, source.Seq)
		}
		source.Timestamp = row.CreatedAt.Time.UTC()
		source.Classification = knowledgeSourceClass(row)
		source.RecordID = sourceRecordID(row)
	}
	return sources, nil
}

func historyReference(source knowledge.Source) store.HistoryReference {
	return store.HistoryReference{Kind: source.Kind, ThreadID: source.ThreadID, Seq: source.Seq}
}

// A project-only restore offsets SQLite integer row IDs. The original turn ID
// and creation time survive that copy, while a new turn after rewind cannot
// inherit the old source's identity even when a sequence number is reused.
// An in-place steer edit keeps both, so bind its current caption as well.
func sourceRecordID(row *store.HistoryIdentity) string {
	id := row.OriginTurnID + "@" + row.CreatedAt.Time.UTC().Format(time.RFC3339Nano)
	if row.SteerDigest != "" {
		id += "#" + row.SteerDigest
	}
	return id
}

func knowledgeSourceClass(row *store.HistoryIdentity) string {
	if row.Role == string(schema.Tool) || (row.Role == string(schema.User) && row.Source == store.MessageSourceHuman) {
		return "primary"
	}
	switch row.EventKind {
	case KindUser, KindSteer, KindBuiltinTool, swarm.NotifyToolResult.String():
		return "primary"
	default:
		return "analysis"
	}
}

func validKnowledgeHistoryRef(source knowledge.Source) bool {
	return source.URI == "" && (source.Kind == "message" || source.Kind == "event") &&
		strings.TrimSpace(source.ThreadID) != "" && source.Seq > 0
}

func (e *Engine) enrichKnowledgeSources(ctx context.Context, projectID string, sources []knowledge.Source) error {
	var refs []store.HistoryReference
	for _, source := range sources {
		if validKnowledgeHistoryRef(source) {
			refs = append(refs, historyReference(source))
		}
	}
	rows, err := e.store.ReadProjectHistoryIdentities(ctx, projectID, refs)
	if err != nil {
		return err
	}
	for i := range sources {
		source := &sources[i]
		if source.URI != "" {
			source.Classification = "external"
			continue
		}
		var row *store.HistoryIdentity
		if validKnowledgeHistoryRef(*source) {
			row = rows[0]
			rows = rows[1:]
		}
		available := row != nil && row.CreatedAt.Valid && !row.CreatedAt.Time.IsZero() &&
			(source.RecordID == "" || source.RecordID == sourceRecordID(row)) &&
			(source.Timestamp.IsZero() || source.Timestamp.Equal(row.CreatedAt.Time))
		source.Available = &available
		if available {
			source.TurnID = row.TurnID
			source.Classification = knowledgeSourceClass(row)
			source.Timestamp = row.CreatedAt.Time.UTC()
		}
	}
	return nil
}

// The owner comes from the Registry. A retained worker may outlive its manager
// check; its original turn and current project membership must still agree.
func (e *Engine) knowledgeToolOwner(ctx context.Context, turn *store.Turn, projectID string, write bool) error {
	if err := context.Cause(ctx); err != nil {
		return err
	}
	owner, ok := ctx.Value(historyReadOwnerKey{}).(historyReadOwner)
	if !ok || turn == nil || owner.ThreadID != turn.ThreadID || owner.TurnID != turn.ID || turn.ProjectID != projectID {
		return fmt.Errorf("knowledge operation has no matching project turn owner")
	}
	if write {
		caller, ok := swarm.ToolCaller(ctx)
		if !ok || caller.AgentID == "" || turn.NuwaReadOnly {
			return fmt.Errorf("knowledge writes require a writable agent invocation")
		}
	}
	origin, thread, err := e.workerConfigurationOwner(ctx, owner.ThreadID, owner.TurnID)
	if err != nil {
		return err
	}
	if origin.ProjectID != projectID || (write && thread.PlanMode) {
		return fmt.Errorf("knowledge operation is outside its writable project scope")
	}
	return nil
}
