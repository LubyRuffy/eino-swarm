package memory

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"unicode/utf8"
)

// SkillFamilies groups recorded skills that are the same subject written
// more than once. A family of one is not returned: there is nothing to fold.
//
// Clustering is transitive, so A~B and B~C become one group even when A
// and C would not pair on their own.
func SkillFamilies(skills []Skill) [][]Skill {
	n := len(skills)
	if n < 2 {
		return nil
	}
	parent := make([]int, n)
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		if parent[i] != i {
			parent[i] = find(parent[i])
		}
		return parent[i]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			if skillOverlap(skills[i].Name, skills[i].Description, skills[i].Body, skills[j]) != "" {
				union(i, j)
			}
		}
	}
	groups := map[int][]Skill{}
	order := make([]int, 0, n)
	seen := map[int]bool{}
	for i := 0; i < n; i++ {
		r := find(i)
		if !seen[r] {
			seen[r] = true
			order = append(order, r)
		}
		groups[r] = append(groups[r], skills[i])
	}
	var out [][]Skill
	for _, r := range order {
		fam := groups[r]
		if len(fam) < 2 {
			continue
		}
		sort.Slice(fam, func(i, j int) bool { return fam[i].Name < fam[j].Name })
		out = append(out, fam)
	}
	sort.Slice(out, func(i, j int) bool { return out[i][0].Name < out[j][0].Name })
	return out
}

// SkillFamilyNames is the catalog view of SkillFamilies: just the names, in
// stable order, so a reviewer can be told which groups to merge without
// inlining every procedure.
func (s *Store) SkillFamilyNames() ([][]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	skills, err := s.loadAllSkills()
	if err != nil {
		return nil, err
	}
	var out [][]string
	for _, fam := range SkillFamilies(skills) {
		names := make([]string, len(fam))
		for i, sk := range fam {
			names[i] = sk.Name
		}
		out = append(out, names)
	}
	return out, nil
}

// FoldMerge is one family the catalog folded: the name that remains, the
// chapter-skills that were deleted, and whether the keeper had to be created
// because the shared stem was not already a skill.
type FoldMerge struct {
	Keep    string   `json:"keep"`
	Dropped []string `json:"dropped"`
	Created bool     `json:"created"`
}

// FoldReport is what the Memory panel shows after a tidy click: counts plus
// the names that were merged, deleted, or created. A finished turn still
// only records Changes; the panel needs the rest so a click is not a shrug.
type FoldReport struct {
	Scanned   int         `json:"scanned"`
	Before    int         `json:"before"`
	After     int         `json:"after"`
	Families  int         `json:"families"`
	Unchanged int         `json:"unchanged"`
	Created   []string    `json:"created"`
	Deleted   []string    `json:"deleted"`
	Merged    []FoldMerge `json:"merged"`
	Changes   []Change    `json:"changes"`
}

// Folded is true when at least one family collapsed.
func (r FoldReport) Folded() bool { return len(r.Merged) > 0 }

func emptyFoldReport(scanned int) FoldReport {
	return FoldReport{
		Scanned:   scanned,
		Before:    scanned,
		After:     scanned,
		Unchanged: scanned,
		Created:   []string{},
		Deleted:   []string{},
		Merged:    []FoldMerge{},
		Changes:   []Change{},
	}
}

// FoldSkillFamilies rewrites each same-subject group as one skill and deletes
// the rest. It is the quality guarantee that does not wait for a model: a
// catalog that grew chapter-skills still collapses after a turn, or when
// the Memory panel asks.
//
// The kept name is the shared stem when that stem is a usable skill name,
// otherwise the shortest existing name. Bodies of the dropped skills become
// headed sections so the steps are not lost. Returns one Change per fold
// that landed.
func (s *Store) FoldSkillFamilies() ([]Change, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rep, err := s.foldReportLocked()
	return rep.Changes, err
}

// FoldSkillFamiliesReport is FoldSkillFamilies plus the counts and name
// lists the Memory panel prints after a click.
func (s *Store) FoldSkillFamiliesReport() (FoldReport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.foldReportLocked()
}

func (s *Store) foldReportLocked() (FoldReport, error) {
	skills, err := s.loadAllSkills()
	if err != nil {
		return FoldReport{}, err
	}
	families := SkillFamilies(skills)
	report := emptyFoldReport(len(skills))
	report.Families = len(families)
	touched := 0
	for _, fam := range families {
		merge, change, err := s.foldOneFamilyLocked(fam)
		if err != nil {
			report.Unchanged = report.Scanned - touched
			report.After = report.Before - len(report.Deleted) + len(report.Created)
			return report, err
		}
		touched += len(fam)
		report.Merged = append(report.Merged, merge)
		report.Changes = append(report.Changes, change)
		report.Deleted = append(report.Deleted, merge.Dropped...)
		if merge.Created {
			report.Created = append(report.Created, merge.Keep)
		}
	}
	sort.Strings(report.Created)
	sort.Strings(report.Deleted)
	report.Unchanged = report.Scanned - touched
	report.After = report.Before - len(report.Deleted) + len(report.Created)
	return report, nil
}

func (s *Store) foldOneFamilyLocked(fam []Skill) (FoldMerge, Change, error) {
	names := make([]string, len(fam))
	for i, sk := range fam {
		names[i] = sk.Name
	}
	keep := familyKeepName(names)
	created := true
	for _, n := range names {
		if n == keep {
			created = false
			break
		}
	}
	desc, body := foldFamilyContent(keep, fam)
	except := map[string]struct{}{}
	for _, n := range names {
		except[n] = struct{}{}
	}
	except[keep] = struct{}{}
	skill, err := s.writeSkillLocked(keep, desc, body, except)
	if err != nil {
		return FoldMerge{}, Change{}, err
	}
	dropped := droppedNames(names, keep)
	for _, n := range dropped {
		if err := s.deleteSkillLocked(n); err != nil && err != ErrNoMatch {
			return FoldMerge{}, Change{}, err
		}
	}
	if dropped == nil {
		dropped = []string{}
	}
	return FoldMerge{Keep: skill.Name, Dropped: dropped, Created: created}, Change{
		Target: ToolSkillManage,
		Action: "merge",
		Name:   skill.Name,
		Text:   clipText(strings.Join(dropped, ", ")),
	}, nil
}

// MergeSkills folds sources into keep. keep may be new. An empty description
// or body is derived from the members, so a model that names the group but
// does not rewrite the procedure still lands one skill.
func (s *Store) MergeSkills(keep string, sources []string, description, body string) (Skill, []string, error) {
	if err := ValidSkillName(keep); err != nil {
		return Skill{}, nil, err
	}
	seen := map[string]struct{}{}
	var from []string
	for _, raw := range sources {
		name := strings.TrimSpace(raw)
		if ValidSkillName(name) != nil {
			name = SafeSkillName(name)
		}
		if name == "" || name == keep {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		from = append(from, name)
	}
	if len(from) == 0 {
		return Skill{}, nil, fmt.Errorf("memory: merge needs at least one other skill to fold in")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var members []Skill
	if existing, err := s.readSkill(keep); err == nil {
		members = append(members, existing)
	} else if err != ErrNoMatch {
		return Skill{}, nil, err
	}
	for _, name := range from {
		sk, err := s.readSkill(name)
		if err != nil {
			if err == ErrNoMatch {
				return Skill{}, nil, fmt.Errorf("memory: no skill named %q to merge", name)
			}
			return Skill{}, nil, err
		}
		members = append(members, sk)
	}

	desc := strings.Join(strings.Fields(description), " ")
	body = strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if desc == "" || body == "" {
		derivedDesc, derivedBody := foldFamilyContent(keep, members)
		if desc == "" {
			desc = derivedDesc
		}
		if body == "" {
			body = derivedBody
		}
	}

	except := map[string]struct{}{keep: {}}
	for _, name := range from {
		except[name] = struct{}{}
	}
	skill, err := s.writeSkillLocked(keep, desc, body, except)
	if err != nil {
		return Skill{}, nil, err
	}
	deleted := make([]string, 0, len(from))
	for _, name := range from {
		if err := s.deleteSkillLocked(name); err != nil && err != ErrNoMatch {
			return Skill{}, nil, err
		}
		deleted = append(deleted, name)
	}
	return skill, deleted, nil
}

func (s *Store) loadAllSkills() ([]Skill, error) {
	entries, err := os.ReadDir(s.skillsDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("memory: list skills: %w", err)
	}
	var out []Skill
	for _, e := range entries {
		if !e.IsDir() || ValidSkillName(e.Name()) != nil {
			continue
		}
		skill, err := s.readSkill(e.Name())
		if err != nil {
			continue
		}
		out = append(out, skill)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func familyKeepName(names []string) string {
	if len(names) == 0 {
		return ""
	}
	prefix := nameTokens(names[0])
	for _, n := range names[1:] {
		t := nameTokens(n)
		i := 0
		for i < len(prefix) && i < len(t) && prefix[i] == t[i] {
			i++
		}
		prefix = prefix[:i]
	}
	if len(prefix) >= nameShareMin {
		joined := strings.Join(prefix, "-")
		if ValidSkillName(joined) == nil {
			return joined
		}
	}
	best := names[0]
	for _, n := range names[1:] {
		if betterKeepName(n, best) {
			best = n
		}
	}
	return best
}

func betterKeepName(a, b string) bool {
	ta, tb := nameTokens(a), nameTokens(b)
	if len(ta) != len(tb) {
		return len(ta) < len(tb)
	}
	if len(a) != len(b) {
		return len(a) < len(b)
	}
	return a < b
}

func foldFamilyContent(keep string, members []Skill) (description, body string) {
	var keeper *Skill
	for i := range members {
		if members[i].Name == keep {
			keeper = &members[i]
			break
		}
	}
	if keeper != nil {
		description = keeper.Description
	}
	if description == "" {
		for _, m := range members {
			if utf8.RuneCountInString(m.Description) > utf8.RuneCountInString(description) {
				description = m.Description
			}
		}
	}
	if description == "" {
		description = "when this recorded procedure applies"
	}

	var b strings.Builder
	if keeper != nil {
		if text := strings.TrimSpace(keeper.Body); text != "" {
			b.WriteString(text)
			b.WriteString("\n")
		}
	}
	for _, m := range members {
		if m.Name == keep {
			continue
		}
		text := strings.TrimSpace(m.Body)
		if text == "" {
			continue
		}
		if keeper != nil && noteCoveredBy(text, keeper.Body) {
			continue
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "## %s\n\n%s\n", m.Name, text)
	}
	return description, strings.TrimSpace(b.String())
}

func droppedNames(names []string, keep string) []string {
	var out []string
	for _, n := range names {
		if n != keep {
			out = append(out, n)
		}
	}
	sort.Strings(out)
	return out
}
