package clients

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Cursor names a project directory by encoding its absolute path. The list
// shows that directory's own name, the same label Claude and Codex take from
// the session's working directory. A removed worktree still shows the
// repository that contains the path it encoded.
func cursorCWD(transcript string) string {
	return cursorProjectDir(cursorSlug(transcript))
}

func cursorSlug(transcript string) string {
	idDir := filepath.Dir(transcript)
	transcripts := filepath.Dir(idDir)
	if filepath.Base(transcripts) != "agent-transcripts" {
		return ""
	}
	return filepath.Base(filepath.Dir(transcripts))
}

var cursorSlugCache sync.Map

func cursorProjectDir(slug string) string {
	slug = strings.TrimSpace(slug)
	if slug == "" || strings.Contains(slug, "..") {
		return ""
	}
	return lookupCursorProject(string(os.PathSeparator), slug)
}

func lookupCursorProject(start, slug string) string {
	if cached, ok := cursorSlugCache.Load(slug); ok {
		return cached.(string)
	}
	decoded, full := decodeCursorProject(start, slug)
	if full && decoded != "" {
		cursorSlugCache.Store(slug, decoded)
	}
	return decoded
}

// cursorComponent is the piece Cursor puts in a project folder name.
// Separators and dots both become "-"; a leading dot is omitted, so a
// hidden worktree directory still matches the folder name.
func cursorComponent(name string) string {
	name = strings.TrimLeft(name, ".")
	if name == "" {
		return ""
	}
	return strings.ReplaceAll(name, ".", "-")
}

func cursorIsDir(parent string, ent os.DirEntry) bool {
	if ent.IsDir() {
		return true
	}
	if ent.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(parent, ent.Name()))
	return err == nil && info.IsDir()
}

type cursorCandidate struct {
	enc  string
	full string
	dots int
}

type cursorPartial struct {
	path    string
	matched int
	depth   int
}

// decodeCursorProject walks real directories from start. The longest
// directory name is tried first, so demo-app is not cut down to demo; a
// shorter name is used when only that name reaches the rest of the path.
// The bool is true when the whole slug was consumed. Otherwise the result
// is the git repository containing the longest real prefix, or empty.
func decodeCursorProject(start, slug string) (string, bool) {
	var complete string
	best := cursorPartial{matched: -1}
	var walk func(cur, rest string, depth int)
	walk = func(cur, rest string, depth int) {
		matched := len(slug) - len(rest)
		if matched > best.matched || (matched == best.matched && depth > best.depth) {
			best = cursorPartial{path: cur, matched: matched, depth: depth}
		}
		if rest == "" {
			complete = cur
			return
		}
		entries, err := os.ReadDir(cur)
		if err != nil {
			return
		}
		var cands []cursorCandidate
		for _, ent := range entries {
			if !cursorIsDir(cur, ent) {
				continue
			}
			name := ent.Name()
			enc := cursorComponent(name)
			if enc == "" || (rest != enc && !strings.HasPrefix(rest, enc+"-")) {
				continue
			}
			cands = append(cands, cursorCandidate{
				enc:  enc,
				full: filepath.Join(cur, name),
				dots: strings.Count(name, "."),
			})
		}
		sort.Slice(cands, func(i, j int) bool {
			if len(cands[i].enc) != len(cands[j].enc) {
				return len(cands[i].enc) > len(cands[j].enc)
			}
			return cands[i].dots > cands[j].dots
		})
		for _, cand := range cands {
			next := ""
			if rest != cand.enc {
				next = rest[len(cand.enc)+1:]
			}
			walk(cand.full, next, depth+1)
			if complete != "" {
				return
			}
		}
	}
	walk(start, slug, 0)
	if complete != "" {
		return complete, true
	}
	return cursorRepoDir(start, best.path), false
}

// cursorRepoDir is the nearest directory of path that contains a git
// checkout. Hidden path pieces such as a worktree container are skipped,
// and a prefix that never reaches a repository is not a project name.
func cursorRepoDir(start, path string) string {
	if path == "" || path == start {
		return ""
	}
	for path != start && strings.HasPrefix(filepath.Base(path), ".") {
		next := filepath.Dir(path)
		if next == path {
			return ""
		}
		path = next
	}
	for path != start && path != string(os.PathSeparator) {
		if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
			return path
		}
		next := filepath.Dir(path)
		if next == path {
			break
		}
		path = next
	}
	return ""
}
