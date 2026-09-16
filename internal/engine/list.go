package engine

import (
	"os"
	"path/filepath"
	"strings"
)

// listWorkspace walks a workspace breadth-first so a fat directory that
// sorts early cannot spend the listing cap before siblings the file
// manager still shows. Directories at a depth are recorded before files,
// and files are taken round-robin across parents at that depth.
func listWorkspace(root string, max int) []FileEntry {
	if max <= 0 {
		return nil
	}
	type queued struct{ abs, rel string }
	queue := []queued{{abs: root, rel: ""}}
	out := make([]FileEntry, 0, 64)

	for len(queue) > 0 && len(out) < max {
		remaining := max - len(out)
		type bucket struct {
			dirs  []FileEntry
			dabs  []string
			files []FileEntry
		}
		buckets := make([]bucket, 0, len(queue))
		for _, d := range queue {
			ents, err := os.ReadDir(d.abs)
			if err != nil {
				continue
			}
			var b bucket
			for _, ent := range ents {
				if ent.IsDir() && skipListedDir(ent.Name()) {
					continue
				}
				rel := ent.Name()
				if d.rel != "" {
					rel = d.rel + "/" + ent.Name()
				}
				item := FileEntry{Path: rel, Name: ent.Name(), Dir: ent.IsDir()}
				if info, err := ent.Info(); err == nil {
					item.Size = info.Size()
					item.Modified = info.ModTime()
				}
				if ent.IsDir() {
					b.dirs = append(b.dirs, item)
					b.dabs = append(b.dabs, filepath.Join(d.abs, ent.Name()))
				} else {
					b.files = append(b.files, item)
				}
			}
			buckets = append(buckets, b)
		}

		var next []queued
		for i := range buckets {
			b := &buckets[i]
			for j, item := range b.dirs {
				if remaining <= 0 {
					return out
				}
				out = append(out, item)
				remaining--
				next = append(next, queued{abs: b.dabs[j], rel: item.Path})
			}
		}
		for remaining > 0 {
			added := false
			for i := range buckets {
				if remaining <= 0 {
					break
				}
				b := &buckets[i]
				if len(b.files) == 0 {
					continue
				}
				out = append(out, b.files[0])
				b.files = b.files[1:]
				remaining--
				added = true
			}
			if !added {
				break
			}
		}
		queue = next
	}
	return out
}

func skipListedDir(name string) bool {
	if strings.HasPrefix(name, ".") {
		return true
	}
	switch name {
	case "node_modules", "vendor":
		return true
	default:
		return false
	}
}
