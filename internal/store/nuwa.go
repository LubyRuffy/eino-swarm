package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// ErrNuwaConflict means another writer already committed this node version.
var ErrNuwaConflict = errors.New("store: shared node version conflict")

// ErrInvalidNuwaNode identifies invalid content, ownership or evidence.
var ErrInvalidNuwaNode = errors.New("store: invalid shared node")

// NuwaEvidence references an actual durable event, never a model-authored quote.
type NuwaEvidence struct {
	ThreadID string `json:"thread_id"`
	TurnID   string `json:"turn_id,omitempty"`
	Seq      int64  `json:"seq"`
	Label    string `json:"label,omitempty"`
}

// NuwaNode is a project-scoped shared task, reported observation or artifact.
// Origin fields remain fixed; Updated* records the last trusted caller.
type NuwaNode struct {
	ID                 string           `gorm:"primaryKey;size:64" json:"id"`
	ProjectID          string           `gorm:"index;size:64" json:"project_id"`
	Kind               string           `gorm:"size:16" json:"kind"`
	Title              string           `json:"title"`
	Summary            string           `json:"summary"`
	Detail             string           `json:"detail"`
	Status             string           `gorm:"size:24" json:"status"`
	DependsOn          []string         `gorm:"serializer:json" json:"depends_on"`
	DependencyVersions map[string]int64 `gorm:"serializer:json" json:"dependency_versions"`
	Evidence           []NuwaEvidence   `gorm:"serializer:json" json:"evidence"`
	ArtifactPath       string           `json:"artifact_path,omitempty"`
	ThreadID           string           `gorm:"index;size:64" json:"thread_id"`
	TurnID             string           `gorm:"size:64" json:"turn_id"`
	AgentID            string           `gorm:"size:64" json:"agent_id"`
	Generation         int              `json:"generation"`
	UpdatedBy          string           `json:"updated_by"`
	UpdatedThreadID    string           `json:"updated_thread_id"`
	UpdatedTurnID      string           `json:"updated_turn_id"`
	UpdatedGeneration  int              `json:"updated_generation"`
	Version            int64            `json:"version"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	// Deleted sources stay explicit instead of silently removing a prerequisite.
	MissingDependencies []string       `gorm:"-" json:"missing_dependencies,omitempty"`
	StaleDependencies   []string       `gorm:"-" json:"stale_dependencies,omitempty"`
	UnavailableEvidence []NuwaEvidence `gorm:"-" json:"unavailable_evidence,omitempty"`
}

// NuwaRevision preserves every committed full node version. An empty NodeID
// records a worker-state invalidation without inventing a shared observation.
type NuwaRevision struct {
	ID          uint      `gorm:"primaryKey" json:"-"`
	ProjectID   string    `gorm:"uniqueIndex:idx_nuwa_revision;uniqueIndex:idx_nuwa_request;size:64" json:"project_id"`
	Revision    int64     `gorm:"uniqueIndex:idx_nuwa_revision" json:"revision"`
	NodeID      string    `gorm:"index;size:64" json:"node_id,omitempty"`
	Version     int64     `json:"version,omitempty"`
	Payload     string    `json:"payload"`
	RequestKey  *string   `gorm:"uniqueIndex:idx_nuwa_request" json:"request_key,omitempty"`
	RequestHash string    `json:"request_hash,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// NuwaWriteReceipt binds a trusted runtime tool call to its canonical arguments.
// A repeated call returns the exact committed version without adding a node.
type NuwaWriteReceipt struct {
	Key, Hash string
	// A nil list preserves literal evidence. An empty list clears it.
	OwnToolCallIDs    []string
	CurrentToolCallID string
	ToolReceiptScope  string
}

func invalidNuwa(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidNuwaNode, fmt.Sprintf(format, args...))
}

func validateNuwaNode(node *NuwaNode) error {
	if strings.TrimSpace(node.Title) == "" || node.ProjectID == "" || node.ThreadID == "" || node.TurnID == "" || node.AgentID == "" || node.Generation < 0 {
		return invalidNuwa("title and trusted project/thread/turn/agent identity are required")
	}
	var statuses string
	switch node.Kind {
	case "task":
		statuses = "|pending|running|done|blocked|cancelled|"
		if node.Status == "" {
			node.Status = "pending"
		}
	case "fact", "artifact":
		statuses = "|reported|superseded|retracted|"
		if node.Status == "" {
			node.Status = "reported"
		}
	default:
		return invalidNuwa("kind must be task, fact or artifact")
	}
	if !strings.Contains(statuses, "|"+node.Status+"|") {
		return invalidNuwa("status is invalid for %s", node.Kind)
	}
	if node.Kind == "artifact" && strings.TrimSpace(node.ArtifactPath) == "" {
		return invalidNuwa("artifact_path is required for an artifact")
	}
	return nil
}

func validateNuwaOwner(tx *gorm.DB, node NuwaNode, checkRecordedProject bool) error {
	var thread Thread
	if err := tx.Select("id", "project_id").First(&thread, "id = ?", node.ThreadID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return invalidNuwa("source thread does not exist")
		}
		return err
	}
	if thread.ProjectID != node.ProjectID {
		return invalidNuwa("source thread belongs to another project")
	}
	var turn Turn
	if err := tx.Select("id", "project_id", "thread_id", "status").First(&turn, "id = ?", node.TurnID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return invalidNuwa("source turn does not exist")
		}
		return err
	}
	if turn.ThreadID != node.ThreadID || turn.Status == TurnCancelled {
		return invalidNuwa("source turn has another owner or was cancelled")
	}
	if checkRecordedProject && turn.ProjectID != node.ProjectID {
		return invalidNuwa("source turn belongs to another project")
	}
	return nil
}

func validateNuwaEvidence(tx *gorm.DB, projectID string, evidence NuwaEvidence) error {
	if evidence.ThreadID == "" || evidence.Seq <= 0 {
		return invalidNuwa("evidence requires thread_id and a positive event seq")
	}
	var event Event
	err := tx.Table("events").Select("events.*").Joins("JOIN threads ON threads.id = events.thread_id").
		Where("events.thread_id = ? AND events.seq = ? AND threads.project_id = ?", evidence.ThreadID, evidence.Seq, projectID).First(&event).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return invalidNuwa("evidence event is missing or belongs to another project")
	}
	if err != nil {
		return err
	}
	if evidence.TurnID != "" && evidence.TurnID != event.TurnID {
		return invalidNuwa("evidence turn does not match its event")
	}
	return nil
}

func validateNuwaDependencies(nodes []NuwaNode, candidate *NuwaNode) error {
	graph := make(map[string][]string, len(nodes)+1)
	versions := map[string]int64{}
	for _, node := range nodes {
		graph[node.ID] = node.DependsOn
		versions[node.ID] = node.Version
	}
	for _, dependency := range candidate.DependsOn {
		if dependency == candidate.ID {
			return invalidNuwa("a node cannot depend on itself")
		}
		if _, exists := graph[dependency]; !exists {
			return invalidNuwa("dependency %q is missing or belongs to another project", dependency)
		}
	}
	graph[candidate.ID] = candidate.DependsOn
	visiting, visited := map[string]bool{}, map[string]bool{}
	var visit func(string) bool
	visit = func(id string) bool {
		if visiting[id] {
			return true
		}
		if visited[id] {
			return false
		}
		visiting[id] = true
		for _, next := range graph[id] {
			if visit(next) {
				return true
			}
		}
		visiting[id], visited[id] = false, true
		return false
	}
	for id := range graph {
		if visit(id) {
			return invalidNuwa("dependencies contain a cycle")
		}
	}
	if candidate.DependencyVersions == nil {
		candidate.DependencyVersions = map[string]int64{}
		for _, dependency := range candidate.DependsOn {
			candidate.DependencyVersions[dependency] = versions[dependency]
		}
	} else {
		if len(candidate.DependencyVersions) != len(candidate.DependsOn) {
			return invalidNuwa("dependency_versions must match depends_on")
		}
		for _, dependency := range candidate.DependsOn {
			if candidate.DependencyVersions[dependency] <= 0 {
				return invalidNuwa("dependency_versions must record each positive source version")
			}
		}
	}
	return nil
}

func advanceNuwaRevision(tx *gorm.DB, projectID string) (int64, error) {
	result := tx.Model(&Project{}).Where("id = ?", projectID).UpdateColumn("nuwa_revision", gorm.Expr("COALESCE(nuwa_revision, 0) + 1"))
	if result.Error != nil {
		return 0, result.Error
	}
	if result.RowsAffected == 0 {
		return 0, invalidNuwa("project does not exist")
	}
	var project Project
	if err := tx.Select("nuwa_revision").First(&project, "id = ?", projectID).Error; err != nil {
		return 0, err
	}
	return project.NuwaRevision, nil
}

// UpsertNuwaNode validates the trusted caller and all references, then commits
// the node CAS, project revision and full audit payload in one transaction.
// On update the input's identity is the current caller, not a replacement origin.
func (s *Store) UpsertNuwaNode(ctx context.Context, node *NuwaNode, expectedVersion int64, receipts ...NuwaWriteReceipt) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if node == nil {
		return invalidNuwa("node is required")
	}
	candidate := *node
	if err := validateNuwaNode(&candidate); err != nil {
		return err
	}
	if expectedVersion < 0 || (candidate.ID == "" && expectedVersion != 0) {
		return ErrNuwaConflict
	}
	var receipt NuwaWriteReceipt
	if len(receipts) > 1 {
		return invalidNuwa("one tool write receipt is allowed")
	}
	if len(receipts) == 1 {
		receipt = receipts[0]
	}
	if (receipt.Key == "") != (receipt.Hash == "") {
		return invalidNuwa("write receipt requires both key and hash")
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := validateNuwaOwner(tx, candidate, false); err != nil {
			return err
		}
		if receipt.Key != "" {
			var previous NuwaRevision
			err := tx.First(&previous, "project_id = ? AND request_key = ?", candidate.ProjectID, receipt.Key).Error
			if err == nil {
				if previous.RequestHash != receipt.Hash {
					return ErrNuwaConflict
				}
				return json.Unmarshal([]byte(previous.Payload), &candidate)
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
		}
		if receipt.OwnToolCallIDs != nil {
			evidence, err := resolveNuwaOwnToolEvidence(tx, candidate, receipt)
			if err != nil {
				return err
			}
			candidate.Evidence = evidence
		}
		revision, err := advanceNuwaRevision(tx, candidate.ProjectID)
		if err != nil {
			return err
		}
		candidate.UpdatedBy, candidate.UpdatedThreadID, candidate.UpdatedTurnID, candidate.UpdatedGeneration = candidate.AgentID, candidate.ThreadID, candidate.TurnID, candidate.Generation
		create := candidate.ID == ""
		if create {
			candidate.ID, candidate.CreatedAt = NewID("nw_"), time.Now().UTC()
		} else {
			var previous NuwaNode
			if err := tx.First(&previous, "id = ? AND project_id = ?", candidate.ID, candidate.ProjectID).Error; err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return invalidNuwa("node does not exist in this project")
				}
				return err
			}
			if previous.Version != expectedVersion {
				return ErrNuwaConflict
			}
			candidate.ThreadID, candidate.TurnID, candidate.AgentID, candidate.Generation = previous.ThreadID, previous.TurnID, previous.AgentID, previous.Generation
			candidate.CreatedAt = previous.CreatedAt
		}
		if receipt.OwnToolCallIDs == nil {
			for _, evidence := range candidate.Evidence {
				if err := validateNuwaEvidence(tx, candidate.ProjectID, evidence); err != nil {
					return err
				}
			}
		}
		var nodes []NuwaNode
		if err := tx.Where("project_id = ?", candidate.ProjectID).Find(&nodes).Error; err != nil {
			return err
		}
		if err := validateNuwaDependencies(nodes, &candidate); err != nil {
			return err
		}
		candidate.Version, candidate.UpdatedAt = expectedVersion+1, time.Now().UTC()
		candidate.MissingDependencies, candidate.UnavailableEvidence = nil, nil
		candidate.StaleDependencies = nil
		if create {
			if err := tx.Create(&candidate).Error; err != nil {
				return err
			}
		} else {
			result := tx.Model(&NuwaNode{}).Where("id = ? AND project_id = ? AND version = ?", candidate.ID, candidate.ProjectID, expectedVersion).Select("*").Updates(&candidate)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrNuwaConflict
			}
		}
		payload, err := json.Marshal(candidate)
		if err != nil {
			return err
		}
		row := NuwaRevision{ProjectID: candidate.ProjectID, Revision: revision, NodeID: candidate.ID, Version: candidate.Version, Payload: string(payload), CreatedAt: candidate.UpdatedAt}
		if receipt.Key != "" {
			key := receipt.Key
			row.RequestKey = &key
			row.RequestHash = receipt.Hash
		}
		return tx.Create(&row).Error
	})
	if err != nil {
		return fmt.Errorf("store: save shared node: %w", err)
	}
	*node = candidate
	return nil
}

// BumpNuwaRevision invalidates the shared snapshot after a worker-state change.
func (s *Store) BumpNuwaRevision(ctx context.Context, projectID string) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		revision, err := advanceNuwaRevision(tx, projectID)
		if err != nil {
			return err
		}
		return tx.Create(&NuwaRevision{ProjectID: projectID, Revision: revision, Payload: "{}", CreatedAt: time.Now().UTC()}).Error
	})
}

// NuwaSnapshot reads the project revision and its nodes from one read snapshot.
// Deleted prerequisite and evidence IDs remain explicit and unavailable.
func (s *Store) NuwaSnapshot(ctx context.Context, projectID string) (int64, []NuwaNode, error) {
	return s.nuwaSnapshot(ctx, projectID, nil)
}

// NuwaSnapshotForTurn checks the caller's current project membership in the
// same snapshot used for the shared records.
func (s *Store) NuwaSnapshotForTurn(ctx context.Context, projectID, threadID, turnID string) (int64, []NuwaNode, error) {
	return s.nuwaSnapshot(ctx, projectID, &NuwaNode{ProjectID: projectID, ThreadID: threadID, TurnID: turnID})
}

func (s *Store) nuwaSnapshot(ctx context.Context, projectID string, owner *NuwaNode) (int64, []NuwaNode, error) {
	var project Project
	nodes := []NuwaNode{}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if owner != nil {
			if err := validateNuwaOwner(tx, *owner, true); err != nil {
				return err
			}
		}
		if err := tx.First(&project, "id = ?", projectID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}
		if err := tx.Where("project_id = ?", projectID).Order("created_at, id").Find(&nodes).Error; err != nil {
			return err
		}
		present := map[string]NuwaNode{}
		for _, node := range nodes {
			present[node.ID] = node
		}
		for i := range nodes {
			for _, dependency := range nodes[i].DependsOn {
				if source, exists := present[dependency]; !exists {
					nodes[i].MissingDependencies = append(nodes[i].MissingDependencies, dependency)
				} else if nodes[i].DependencyVersions[dependency] != source.Version || source.Status == "superseded" || source.Status == "retracted" {
					nodes[i].StaleDependencies = append(nodes[i].StaleDependencies, dependency)
				}
			}
			for _, evidence := range nodes[i].Evidence {
				if err := validateNuwaEvidence(tx, projectID, evidence); err != nil {
					if !errors.Is(err, ErrInvalidNuwaNode) {
						return err
					}
					nodes[i].UnavailableEvidence = append(nodes[i].UnavailableEvidence, evidence)
				}
			}
		}
		return nil
	})
	return project.NuwaRevision, nodes, err
}

func deleteNuwaThread(tx *gorm.DB, threadID string) error {
	var thread Thread
	if err := tx.First(&thread, "id = ?", threadID).Error; err != nil {
		return err
	}
	if thread.ProjectID == "" {
		return nil
	}
	var ids []string
	if err := tx.Model(&NuwaNode{}).Where("project_id = ? AND thread_id = ?", thread.ProjectID, threadID).Pluck("id", &ids).Error; err != nil {
		return err
	}
	if len(ids) > 0 {
		if err := tx.Where("project_id = ? AND node_id IN ?", thread.ProjectID, ids).Delete(&NuwaRevision{}).Error; err != nil {
			return err
		}
		if err := tx.Where("project_id = ? AND id IN ?", thread.ProjectID, ids).Delete(&NuwaNode{}).Error; err != nil {
			return err
		}
	}
	revision, err := advanceNuwaRevision(tx, thread.ProjectID)
	if err != nil {
		return err
	}
	return tx.Create(&NuwaRevision{ProjectID: thread.ProjectID, Revision: revision, Payload: "{}", CreatedAt: time.Now().UTC()}).Error
}

func deleteNuwaProject(tx *gorm.DB, projectID string) error {
	if err := tx.Where("project_id = ?", projectID).Delete(&NuwaRevision{}).Error; err != nil {
		return err
	}
	return tx.Where("project_id = ?", projectID).Delete(&NuwaNode{}).Error
}
