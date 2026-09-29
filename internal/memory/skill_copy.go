package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// A copied skill is a separate file. The source is not a live link: a
// business project can change its copy without rewriting the project the
// steps came from, and a later edit there does not land here until someone
// pulls it. Origin records that project so the pull has somewhere to read.
const (
	OriginCurrent  = "current"
	OriginUpdate   = "update"
	OriginLocal    = "local"
	OriginDiverged = "diverged"
	OriginMissing  = "missing"
)

// ErrSkillExists means the destination already has that name. Copying must
// not overwrite a procedure the destination project already recorded.
var ErrSkillExists = errors.New("memory: this project already has a skill with that name")

// SkillOrigin is the project a skill was copied from, and the digest of the
// text at that moment. Status and ProjectName are filled when the skill is
// served; they are not written into SKILL.md.
type SkillOrigin struct {
	ProjectID   string `json:"project_id,omitempty"`
	Name        string `json:"name,omitempty"`
	Digest      string `json:"digest,omitempty"`
	Status      string `json:"status,omitempty"`
	ProjectName string `json:"project_name,omitempty"`
}

// SkillPullError is a pull that would throw away local edits. Reason is the
// code the API returns: "local", "diverged", or "unlinked".
type SkillPullError struct {
	Reason string
}

func (e *SkillPullError) Error() string {
	switch e.Reason {
	case OriginLocal:
		return "memory: this copy has local edits and the source has not changed"
	case OriginDiverged:
		return "memory: this copy and the source both changed since the copy"
	default:
		return "memory: this skill was not copied from another project"
	}
}

// SkillDigest is the identity of a skill's text. A copy stores it so a later
// edit on either side can be told apart from the text that was copied.
func SkillDigest(description, body string) string {
	description, body = normalizeSkill(description, body)
	sum := sha256.Sum256([]byte(description + "\n" + body))
	return hex.EncodeToString(sum[:])
}

func normalizeSkill(description, body string) (string, string) {
	description = strings.Join(strings.Fields(description), " ")
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	return description, body
}

func originFromFront(front map[string]string) *SkillOrigin {
	projectID := strings.TrimSpace(front["origin_project"])
	name := strings.TrimSpace(front["origin_name"])
	digest := strings.TrimSpace(front["origin_digest"])
	if projectID == "" && name == "" && digest == "" {
		return nil
	}
	return &SkillOrigin{ProjectID: projectID, Name: name, Digest: digest}
}

// CompareOrigin says whether a copy can take the source's current text.
// found is false when that project or skill is gone.
func CompareOrigin(localDigest, originDigest, sourceDigest string, found bool) string {
	if !found || originDigest == "" {
		return OriginMissing
	}
	localChanged := localDigest != originDigest
	sourceChanged := sourceDigest != originDigest
	switch {
	case !localChanged && !sourceChanged:
		return OriginCurrent
	case !localChanged && sourceChanged:
		return OriginUpdate
	case localChanged && !sourceChanged:
		return OriginLocal
	default:
		return OriginDiverged
	}
}

// OriginOfCopy chooses what a new copy tracks. An unmodified copy keeps
// tracking the project it already tracked, so copying that copy does not
// start following a project that has not changed the text. A copy that was
// edited tracks the project the bytes were just taken from.
func OriginOfCopy(srcProject string, skill Skill) SkillOrigin {
	digest := skill.ContentDigest
	if digest == "" {
		digest = SkillDigest(skill.Description, skill.Body)
	}
	if skill.Origin != nil &&
		skill.Origin.ProjectID != "" &&
		skill.Origin.Name != "" &&
		skill.Origin.Digest == digest {
		return SkillOrigin{ProjectID: skill.Origin.ProjectID, Name: skill.Origin.Name, Digest: digest}
	}
	return SkillOrigin{ProjectID: srcProject, Name: skill.Name, Digest: digest}
}

// ImportSkill writes a skill that already exists somewhere else. The name is
// refused when this project already has it; the caller picks another name
// instead of replacing a local procedure.
func (s *Store) ImportSkill(name, description, body string, origin SkillOrigin) (Skill, error) {
	if err := ValidSkillName(name); err != nil {
		return Skill{}, err
	}
	if err := ValidSkillName(origin.Name); err != nil {
		return Skill{}, err
	}
	origin.ProjectID = strings.TrimSpace(origin.ProjectID)
	if origin.ProjectID == "" || strings.ContainsAny(origin.ProjectID, "\r\n") || len(origin.ProjectID) > 80 {
		return Skill{}, fmt.Errorf("memory: a copied skill needs the project it came from")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.skillPath(name)); err == nil {
		return Skill{}, ErrSkillExists
	} else if err != nil && !os.IsNotExist(err) {
		return Skill{}, fmt.Errorf("memory: copy skill %s: %w", name, err)
	}
	description, body = normalizeSkill(description, body)
	origin.Digest = SkillDigest(description, body)
	return s.writeSkillLocked(name, description, body, nil, &origin)
}

// PullSkill replaces this skill with the source text. It refuses when the
// copy was edited, unless force is set — a source that has not changed would
// otherwise wipe the only copy of those edits, and a source that also changed
// would pick a side with nobody looking.
func (s *Store) PullSkill(name string, source Skill, force bool) (Skill, error) {
	if err := ValidSkillName(name); err != nil {
		return Skill{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	cur, err := s.readSkill(name)
	if err != nil {
		return Skill{}, err
	}
	if cur.Origin == nil || cur.Origin.ProjectID == "" || cur.Origin.Name == "" || cur.Origin.Digest == "" {
		return Skill{}, &SkillPullError{Reason: "unlinked"}
	}
	description, body := normalizeSkill(source.Description, source.Body)
	if description == "" || body == "" {
		return Skill{}, fmt.Errorf("memory: the source skill has nothing to copy")
	}
	incoming := SkillDigest(description, body)
	local := cur.ContentDigest
	origin := *cur.Origin
	if incoming == local {
		if origin.Digest == incoming {
			return cur, nil
		}
		origin.Digest = incoming
		return s.writeSkillLocked(name, cur.Description, cur.Body, nil, &origin)
	}
	localChanged := local != origin.Digest
	sourceChanged := incoming != origin.Digest
	if localChanged && !force {
		reason := OriginDiverged
		if !sourceChanged {
			reason = OriginLocal
		}
		return Skill{}, &SkillPullError{Reason: reason}
	}
	origin.Digest = incoming
	return s.writeSkillLocked(name, description, body, nil, &origin)
}
