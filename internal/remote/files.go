package remote

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
)

// FileChunkRaw leaves room for base64, response metadata and the pairlink seal.
const FileChunkRaw = 36 << 10

func handleFiles(eng *engine.Engine, req Request, path, sessionID string) Response {
	if _, err := eng.Store().GetThread(req.ThreadID); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	entries, err := eng.ListFiles(req.ThreadID)
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	uploads, err := eng.Store().ListAttachments(req.ThreadID)
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	uploaded := make(map[string]bool, len(uploads))
	for _, item := range uploads {
		uploaded[item.RelPath] = true
	}
	start := 0
	if req.Cursor != "" {
		start, err = strconv.Atoi(req.Cursor)
		if err != nil || start < 0 || start > len(entries) {
			return fail(req.ID, path, sessionID, "bad_request", "file cursor is not usable")
		}
	}
	resp := okBase(req.ID, path, sessionID)
	for i := start; i < len(entries); i++ {
		entry := entries[i]
		resp.Files = append(resp.Files, FileView{
			Path: entry.Path, Name: entry.Name, Size: entry.Size,
			Dir: entry.Dir, Modified: entry.Modified, Uploaded: uploaded[entry.Path],
		})
		// A long filename must not make a whole sealed response undeliverable.
		if raw, err := json.Marshal(resp); err != nil || len(raw) > plaintextBudget()-128 {
			resp.Files = resp.Files[:len(resp.Files)-1]
			if len(resp.Files) == 0 {
				return fail(req.ID, path, sessionID, "too_large", "file entry exceeds the phone frame limit")
			}
			resp.More, resp.Next = true, strconv.Itoa(i)
			return resp
		}
	}
	return resp
}

func handleFileChunk(eng *engine.Engine, req Request, path, sessionID string) Response {
	if _, err := eng.Store().GetThread(req.ThreadID); err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	if req.Before < 0 {
		return fail(req.ID, path, sessionID, "bad_request", "file offset is negative")
	}
	if _, err := eng.ResolveWorkspacePath(req.ThreadID, req.FilePath); err != nil {
		return fail(req.ID, path, sessionID, "bad_request", "file path is outside this conversation")
	}
	// Root.Open resolves symlinks beneath the workspace root and refuses escapes.
	root, err := os.OpenRoot(eng.WorkspaceDir(req.ThreadID))
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	defer root.Close()
	f, err := root.Open(req.FilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fail(req.ID, path, sessionID, "not_found", "file is not in this conversation")
		}
		return fail(req.ID, path, sessionID, "bad_request", "file path cannot be opened inside this conversation")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return mapErr(req.ID, path, sessionID, err)
	}
	if !info.Mode().IsRegular() || req.Before > info.Size() {
		return fail(req.ID, path, sessionID, "bad_request", "file is not regular or offset is beyond its end")
	}
	data := make([]byte, FileChunkRaw)
	n, err := f.ReadAt(data, req.Before)
	if err != nil && !errors.Is(err, io.EOF) {
		return mapErr(req.ID, path, sessionID, err)
	}
	data = data[:n]
	name := filepath.Base(req.FilePath)
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(name)))
	if contentType == "" {
		prefix := make([]byte, 512)
		read, _ := f.ReadAt(prefix, 0)
		contentType = http.DetectContentType(prefix[:read])
	}
	if kind, _, err := mime.ParseMediaType(contentType); err == nil {
		contentType = kind
	}
	next := req.Before + int64(n)
	resp := okBase(req.ID, path, sessionID)
	resp.FileChunk = &FileChunkView{
		Path: req.FilePath, Name: name, Size: info.Size(), MIME: contentType,
		Offset: req.Before, NextOffset: next, Data: base64.StdEncoding.EncodeToString(data),
		More: next < info.Size(),
	}
	return resp
}
