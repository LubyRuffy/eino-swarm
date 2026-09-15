package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SkillInfo is what the prompt's skills index and the Memory panel list: the
// name to call for and one line saying when it applies. The body is left on
// disk until something asks for it, which is what keeps a hundred skills from
// costing a hundred procedures' worth of tokens on every turn.
type SkillInfo struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Skill is one procedure in full.
type Skill struct {
	SkillInfo
	Body string `json:"body"`
}

// ListSkills returns every skill, by name, so the prompt is stable between
// turns — an index that reordered itself would invalidate the prompt cache for
// no reason.
func (s *Store) ListSkills() ([]SkillInfo, error) {
	entries, err := os.ReadDir(s.skillsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("memory: list skills: %w", err)
	}
	var out []SkillInfo
	for _, e := range entries {
		if !e.IsDir() || ValidSkillName(e.Name()) != nil {
			continue
		}
		skill, err := s.readSkill(e.Name())
		if err != nil {
			continue // a directory without a readable SKILL.md is not a skill
		}
		out = append(out, skill.SkillInfo)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// ReadSkill returns one skill in full, which is what the agent calls for once
// the index tells it a procedure is relevant.
func (s *Store) ReadSkill(name string) (Skill, error) {
	if err := ValidSkillName(name); err != nil {
		return Skill{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readSkill(name)
}

func (s *Store) readSkill(name string) (Skill, error) {
	raw, err := os.ReadFile(s.skillPath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return Skill{}, ErrNoMatch
		}
		return Skill{}, fmt.Errorf("memory: read skill %s: %w", name, err)
	}
	front, body := splitFrontMatter(string(raw))
	skill := Skill{SkillInfo: SkillInfo{Name: name, Description: front["description"]}, Body: body}
	if n := front["name"]; n != "" {
		// The directory name wins: it is what skill_view is called with, and a
		// front matter that disagrees would advertise a name that cannot be
		// opened.
		skill.Name = name
	}
	if info, statErr := os.Stat(s.skillPath(name)); statErr == nil {
		skill.UpdatedAt = info.ModTime().UTC()
	}
	return skill, nil
}

// WriteSkill creates or overwrites one skill.
func (s *Store) WriteSkill(name, description, body string) (Skill, error) {
	if err := ValidSkillName(name); err != nil {
		return Skill{}, err
	}
	description = strings.Join(strings.Fields(description), " ")
	if description == "" {
		return Skill{}, fmt.Errorf("memory: a skill needs a one-line description saying when it applies")
	}
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if body == "" {
		return Skill{}, fmt.Errorf("memory: a skill needs steps in its body, not only a description")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := writeAtomic(s.skillPath(name), renderSkill(name, description, body)); err != nil {
		return Skill{}, err
	}
	return s.readSkill(name)
}

// PatchSkill replaces the one occurrence of oldText in a skill's body. Skills
// are long, so patching is how an agent corrects a step it got wrong without
// rewriting — and re-deriving — the whole procedure.
func (s *Store) PatchSkill(name, oldText, newText string) (Skill, error) {
	if err := ValidSkillName(name); err != nil {
		return Skill{}, err
	}
	if strings.TrimSpace(oldText) == "" {
		return Skill{}, fmt.Errorf("memory: patch needs the text to find")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	skill, err := s.readSkill(name)
	if err != nil {
		return Skill{}, err
	}
	if n := strings.Count(skill.Body, oldText); n == 0 {
		return Skill{}, ErrNoMatch
	} else if n > 1 {
		return Skill{}, ErrAmbiguous
	}
	body := strings.TrimSpace(strings.Replace(skill.Body, oldText, newText, 1))
	if body == "" {
		return Skill{}, fmt.Errorf("memory: that patch would empty the skill; delete it instead")
	}
	if err := writeAtomic(s.skillPath(name), renderSkill(name, skill.Description, body)); err != nil {
		return Skill{}, err
	}
	return s.readSkill(name)
}

// DeleteSkill removes a skill and its directory.
func (s *Store) DeleteSkill(name string) error {
	if err := ValidSkillName(name); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := os.Stat(s.skillPath(name)); os.IsNotExist(err) {
		return ErrNoMatch
	}
	if err := os.RemoveAll(filepath.Join(s.skillsDir(), name)); err != nil {
		return fmt.Errorf("memory: delete skill %s: %w", name, err)
	}
	return nil
}

func (s *Store) skillsDir() string { return filepath.Join(s.dir, SkillsDir) }

func (s *Store) skillPath(name string) string {
	return filepath.Join(s.skillsDir(), name, SkillFile)
}

// ValidSkillName reports whether name can be a directory under the skills
// directory. The rule is narrow on purpose: a model invents these names, and a
// name is a path segment — `..` or a separator in one would write outside the
// store entirely.
func ValidSkillName(name string) error {
	if name == "" || len(name) > 64 {
		return ErrBadName
	}
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '-':
		default:
			return ErrBadName
		}
	}
	if strings.HasPrefix(name, "-") || strings.HasSuffix(name, "-") {
		return ErrBadName
	}
	return nil
}

// SafeSkillName turns a model's free-form title into a usable name, so a good
// skill is not lost to a capital letter or a space.
func SafeSkillName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteRune('-')
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if len(out) > 64 {
		out = strings.Trim(out[:64], "-")
	}
	return out
}

// renderSkill writes the agentskills.io shape: YAML front matter with the name
// and the description, then the procedure.
func renderSkill(name, description, body string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "name: %s\n", name)
	fmt.Fprintf(&b, "description: %s\n", yamlOneLine(description))
	b.WriteString("---\n\n")
	b.WriteString(body)
	b.WriteString("\n")
	return b.String()
}

// yamlOneLine quotes a description that would otherwise not parse as a plain
// scalar, so a colon in a summary cannot corrupt the front matter.
func yamlOneLine(s string) string {
	if strings.ContainsAny(s, ":#\"'{}[]&*!|>%@`") {
		return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
	}
	return s
}

// splitFrontMatter reads the leading `---` block as key: value pairs and
// returns the rest as the body. Anything it cannot parse is treated as body,
// so a hand-written skill without front matter still lists and still opens.
func splitFrontMatter(raw string) (map[string]string, string) {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	front := map[string]string{}
	if !strings.HasPrefix(raw, "---\n") {
		return front, strings.TrimSpace(raw)
	}
	rest := raw[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return front, strings.TrimSpace(raw)
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		front[strings.TrimSpace(key)] = unquoteYAML(strings.TrimSpace(value))
	}
	body := rest[end+len("\n---"):]
	return front, strings.TrimSpace(body)
}

func unquoteYAML(s string) string {
	if len(s) >= 2 && strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		inner := s[1 : len(s)-1]
		inner = strings.ReplaceAll(inner, `\"`, `"`)
		return strings.ReplaceAll(inner, `\\`, `\`)
	}
	return s
}
