// Package skills stores explicitly installed third-party Agent Skills packages.
package skills

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

const (
	installedSkillsDir    = "installed-skills"
	skillStagePrefix      = ".stage-"
	skillFile             = "SKILL.md"
	maxSkillManifestBytes = 256 << 10
	maxSkillDescription   = 1024
	dirPerm               = 0o700
	filePerm              = 0o600
)

var (
	// ErrInvalidSkillBundle identifies an unsupported or malformed archive.
	ErrInvalidSkillBundle = errors.New("skills: invalid skill bundle")
	// ErrSkillInstallConflict means a package already occupies this name.
	ErrSkillInstallConflict = errors.New("skills: this project already has that installed skill name")
	// ErrNoMatch means no installed package has the requested name.
	ErrNoMatch = errors.New("skills: no installed skill has that name")
	// ErrProjectDeleted means this Store's owning project was deleted.
	ErrProjectDeleted = errors.New("skills: this project was deleted")
	// ErrBadName means the input is not a valid Agent Skills name.
	ErrBadName = errors.New("skills: name must be 1–64 lowercase letters, digits or single internal hyphens")
)

// SkillInfo is the metadata visible in an installed package's index.
type SkillInfo struct {
	Name        string    `json:"name"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// Skill includes the package instructions and a local resource directory.
type Skill struct {
	SkillInfo
	Body    string `json:"body"`
	BaseDir string `json:"-"`
}

// Store owns installed packages in one project. The engine shares a Store
// between callers so removal and publication serialize on the same lock.
type Store struct {
	projectDir string
	mu         sync.Mutex
	deleted    bool
}

// New returns a store rooted at an existing project directory.
func New(projectDir string) *Store {
	return &Store{projectDir: projectDir}
}

// Invalidate closes the catalog before its owning project directory is removed.
func (s *Store) Invalidate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deleted = true
}

func validName(name string) bool {
	if name == "" || len(name) > 64 || strings.HasPrefix(name, "-") ||
		strings.HasSuffix(name, "-") || strings.Contains(name, "--") {
		return false
	}
	for _, c := range name {
		if c != '-' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}
	_, err := filepath.Localize(name)
	return err == nil
}

// InstallSkillZIP validates and stages one Agent Skills bundle before giving it
// a visible name. The destination uses the manifest name, independent of the
// archive's filename and any wrapper directory.
func (s *Store) InstallSkillZIP(ctx context.Context, archive *zip.Reader) (SkillInfo, error) {
	if err := ctx.Err(); err != nil {
		return SkillInfo{}, err
	}
	entries, err := skillBundleEntries(archive)
	if err != nil {
		return SkillInfo{}, err
	}
	stage, err := s.beginInstallStage()
	if err != nil {
		return SkillInfo{}, err
	}
	defer os.RemoveAll(stage)
	var skill Skill
	stageErr := extractSkillBundle(ctx, stage, entries)
	if stageErr == nil {
		skill, stageErr = readInstalledSkillAt(stage)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleted {
		return SkillInfo{}, ErrProjectDeleted
	}
	if stageErr != nil {
		return SkillInfo{}, stageErr
	}
	if err := ctx.Err(); err != nil {
		return SkillInfo{}, err
	}
	destination := s.installedSkillDir(skill.Name)
	if _, err := os.Lstat(destination); err == nil {
		return SkillInfo{}, fmt.Errorf("%w: %s", ErrSkillInstallConflict, skill.Name)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return SkillInfo{}, fmt.Errorf("skills: check name %s: %w", skill.Name, err)
	}
	if err := os.Rename(stage, destination); err != nil {
		return SkillInfo{}, fmt.Errorf("skills: publish skill %s: %w", skill.Name, err)
	}
	return skill.SkillInfo, nil
}

func (s *Store) beginInstallStage() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleted {
		return "", ErrProjectDeleted
	}
	root := s.installedSkillsPath()
	if err := makeInstalledSkillsDir(root); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(root, skillStagePrefix)
	if err != nil {
		return "", fmt.Errorf("skills: stage skill bundle: %w", err)
	}
	return stage, nil
}

func makeInstalledSkillsDir(root string) error {
	// A request may race project removal. Mkdir requires the project parent
	// to still exist, and cannot recreate a deleted project.
	if err := os.Mkdir(root, dirPerm); errors.Is(err, fs.ErrExist) {
		return checkInstalledDirectory(root)
	} else if err != nil {
		return fmt.Errorf("skills: create installed skills directory: %w", err)
	}
	return nil
}

func checkInstalledDirectory(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%w: installed skill path is not a directory", ErrInvalidSkillBundle)
	}
	return nil
}

// ListInstalledSkills returns sorted metadata without changing the learned
// catalog or adding bundle contents to a model prompt.
func (s *Store) ListInstalledSkills() ([]SkillInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleted {
		return nil, ErrProjectDeleted
	}
	root := s.installedSkillsPath()
	if err := checkInstalledDirectory(root); errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("skills: check installed skills directory: %w", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("skills: list installed skills: %w", err)
	}
	var out []SkillInfo
	for _, entry := range entries {
		if !entry.IsDir() || !validName(entry.Name()) {
			continue
		}
		skill, err := s.readInstalledSkill(entry.Name())
		if err == nil {
			out = append(out, skill.SkillInfo)
		}
	}
	return out, nil
}

// ReadInstalledSkill opens the installed instructions and locates their
// resources. It does not record a learned skill view.
func (s *Store) ReadInstalledSkill(name string) (Skill, error) {
	if !validName(name) {
		return Skill{}, ErrBadName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleted {
		return Skill{}, ErrProjectDeleted
	}
	return s.readInstalledSkill(name)
}

func (s *Store) readInstalledSkill(name string) (Skill, error) {
	dir := s.installedSkillDir(name)
	for _, parent := range []string{s.installedSkillsPath(), dir} {
		if err := checkInstalledDirectory(parent); errors.Is(err, fs.ErrNotExist) {
			return Skill{}, ErrNoMatch
		} else if err != nil {
			return Skill{}, fmt.Errorf("skills: locate installed skill %s: %w", name, err)
		}
	}
	skill, err := readInstalledSkillAt(dir)
	if err != nil {
		return Skill{}, err
	}
	if skill.Name != name {
		return Skill{}, fmt.Errorf("%w: installed skill name does not match its directory", ErrInvalidSkillBundle)
	}
	skill.BaseDir, err = filepath.Abs(dir)
	return skill, err
}

// RemoveInstalledSkill detaches a whole package before removing its files.
// An active package never becomes partially visible during cleanup.
func (s *Store) RemoveInstalledSkill(name string) error {
	if !validName(name) {
		return ErrBadName
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.deleted {
		return ErrProjectDeleted
	}
	root := s.installedSkillsPath()
	dir := s.installedSkillDir(name)
	for _, parent := range []string{root, dir} {
		if err := checkInstalledDirectory(parent); errors.Is(err, fs.ErrNotExist) {
			return ErrNoMatch
		} else if err != nil {
			return fmt.Errorf("skills: locate installed skill %s: %w", name, err)
		}
	}
	stage, err := os.MkdirTemp(root, skillStagePrefix)
	if err != nil {
		return fmt.Errorf("skills: stage skill removal: %w", err)
	}
	if err := os.Rename(dir, filepath.Join(stage, name)); err != nil {
		_ = os.Remove(stage)
		return fmt.Errorf("skills: detach installed skill %s: %w", name, err)
	}
	if err := os.RemoveAll(stage); err != nil {
		return fmt.Errorf("skills: skill %s was removed but staging cleanup failed: %w", name, err)
	}
	return nil
}

func (s *Store) installedSkillsPath() string {
	return filepath.Join(s.projectDir, installedSkillsDir)
}

func (s *Store) installedSkillDir(name string) string {
	return filepath.Join(s.installedSkillsPath(), name)
}

func readInstalledSkillAt(dir string) (Skill, error) {
	filename := filepath.Join(dir, skillFile)
	info, err := os.Lstat(filename)
	if err != nil {
		return Skill{}, fmt.Errorf("skills: inspect installed SKILL.md: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() > maxSkillManifestBytes {
		return Skill{}, fmt.Errorf("%w: SKILL.md must be a regular file no larger than %d bytes",
			ErrInvalidSkillBundle, maxSkillManifestBytes)
	}
	file, err := os.Open(filename)
	if err != nil {
		return Skill{}, fmt.Errorf("skills: open installed SKILL.md: %w", err)
	}
	defer file.Close()
	raw, err := io.ReadAll(io.LimitReader(file, maxSkillManifestBytes+1))
	if err != nil {
		return Skill{}, fmt.Errorf("skills: read installed SKILL.md: %w", err)
	}
	if len(raw) > maxSkillManifestBytes {
		return Skill{}, fmt.Errorf("%w: SKILL.md exceeds %d bytes", ErrInvalidSkillBundle, maxSkillManifestBytes)
	}
	skill, err := parseInstalledSkill(raw)
	if err == nil {
		skill.UpdatedAt = info.ModTime().UTC()
	}
	return skill, err
}

func parseInstalledSkill(raw []byte) (Skill, error) {
	if !utf8.Valid(raw) {
		return Skill{}, fmt.Errorf("%w: SKILL.md must be UTF-8", ErrInvalidSkillBundle)
	}
	text := strings.TrimPrefix(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\ufeff")
	rest, leadingFence := strings.CutPrefix(text, "---\n")
	front, body, closingFence := strings.Cut(rest, "\n---\n")
	body = strings.Trim(body, "\n")
	if !leadingFence || !closingFence || strings.TrimSpace(body) == "" {
		return Skill{}, fmt.Errorf("%w: SKILL.md needs YAML front matter and Markdown instructions", ErrInvalidSkillBundle)
	}
	var manifest struct {
		Name        yaml.Node
		Description yaml.Node
	}
	if err := yaml.Unmarshal([]byte(front), &manifest); err != nil {
		return Skill{}, fmt.Errorf("%w: invalid SKILL.md front matter: %v", ErrInvalidSkillBundle, err)
	}
	name := manifest.Name.Value
	if manifest.Name.Tag != "!!str" || !validName(name) {
		return Skill{}, fmt.Errorf("%w: SKILL.md name must be 1–64 lowercase letters, digits or single hyphens", ErrInvalidSkillBundle)
	}
	description := strings.Join(strings.Fields(manifest.Description.Value), " ")
	if manifest.Description.Tag != "!!str" || description == "" ||
		utf8.RuneCountInString(strings.TrimSpace(manifest.Description.Value)) > maxSkillDescription {
		return Skill{}, fmt.Errorf("%w: SKILL.md description must be a nonempty string of at most %d characters",
			ErrInvalidSkillBundle, maxSkillDescription)
	}
	return Skill{SkillInfo: SkillInfo{Name: name, Description: description}, Body: body}, nil
}
