package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/gin-gonic/gin"
)

func (s *Server) composerDraftKey(c *gin.Context) (string, bool) {
	key := c.Param("key")
	var err error
	switch {
	case key == "new:home":
	case strings.HasPrefix(key, "thread:") && validDraftScopeID(strings.TrimPrefix(key, "thread:")):
		_, err = s.engine.Store().GetThread(strings.TrimPrefix(key, "thread:"))
	case strings.HasPrefix(key, "new:") && validDraftScopeID(strings.TrimPrefix(key, "new:")):
		_, err = s.engine.Store().GetProject(strings.TrimPrefix(key, "new:"))
	default:
		s.fail(c, fmt.Errorf("%w: invalid scope", store.ErrInvalidDraft))
		return "", false
	}
	if err != nil {
		s.fail(c, err)
		return "", false
	}
	return key, true
}

func validDraftScopeID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, ch := range id {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func (s *Server) getComposerDraft(c *gin.Context) {
	key, ok := s.composerDraftKey(c)
	if !ok {
		return
	}
	draft, err := s.engine.Store().GetComposerDraft(c.Request.Context(), key)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"draft": draft})
}

func (s *Server) putComposerDraft(c *gin.Context) {
	key, ok := s.composerDraftKey(c)
	if !ok {
		return
	}
	// JSON can encode each original text byte as a six-byte escape. Preserve
	// the documented UTF-8 limit while bounding encoded metadata and framing.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, store.MaxDraftBytes+(9<<20))
	err := c.Request.ParseMultipartForm(maxUploadMemory)
	if c.Request.MultipartForm != nil {
		defer c.Request.MultipartForm.RemoveAll()
	}
	if err != nil {
		s.fail(c, fmt.Errorf("%w: cannot read draft multipart body", store.ErrInvalidDraft))
		return
	}
	form := c.Request.MultipartForm
	var draft store.ComposerDraft
	values := form.Value["snapshot"]
	if len(values) != 1 || len(values[0]) > 8<<20 || json.Unmarshal([]byte(values[0]), &draft) != nil {
		s.fail(c, fmt.Errorf("%w: invalid snapshot", store.ErrInvalidDraft))
		return
	}
	if draft.Key != "" && draft.Key != key {
		s.fail(c, fmt.Errorf("%w: scope mismatch", store.ErrInvalidDraft))
		return
	}
	draft.Key = key
	uploads, err := readDraftUploads(draft, form.File)
	if err != nil {
		s.fail(c, err)
		return
	}
	saved, err := s.engine.Store().SaveComposerDraft(c.Request.Context(), draft, uploads)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"draft": saved})
}

func readDraftUploads(draft store.ComposerDraft, parts map[string][]*multipart.FileHeader) (map[string][]byte, error) {
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", store.ErrInvalidDraft, reason) }
	if len(draft.Files) > store.MaxDraftFiles || len(parts) > store.MaxDraftFiles {
		return nil, invalid("too many attachments")
	}
	metadata := make(map[string]store.DraftFile, len(draft.Files))
	var total int64
	for _, file := range draft.Files {
		if file.Size < 0 || file.Size > store.MaxDraftBytes {
			return nil, invalid("invalid attachment size")
		}
		total += file.Size
		if total > store.MaxDraftBytes {
			return nil, invalid("draft attachments exceed 256 MiB")
		}
		metadata[file.ID] = file
	}
	uploads := make(map[string][]byte, len(parts))
	for name, headers := range parts {
		id := strings.TrimPrefix(name, "file.")
		file, ok := metadata[id]
		if !strings.HasPrefix(name, "file.") || !ok || len(headers) != 1 || headers[0].Size != file.Size {
			return nil, invalid("attachment does not match snapshot")
		}
		src, err := headers[0].Open()
		if err != nil {
			return nil, fmt.Errorf("read draft attachment: %w", err)
		}
		data, err := io.ReadAll(io.LimitReader(src, file.Size+1))
		closeErr := src.Close()
		if err != nil {
			return nil, fmt.Errorf("read draft attachment: %w", err)
		}
		if closeErr != nil {
			return nil, fmt.Errorf("close draft attachment: %w", closeErr)
		}
		if int64(len(data)) != file.Size {
			return nil, invalid("attachment bytes were truncated")
		}
		if file.Kind == "image" {
			mime := strings.ToLower(strings.TrimSpace(file.MIME))
			if mime == "image/jpg" {
				mime = "image/jpeg"
			}
			if file.Size > 8<<20 || (mime != "image/png" && mime != "image/jpeg" && mime != "image/gif" && mime != "image/webp") || http.DetectContentType(data) != mime {
				return nil, invalid("unsupported or mismatched image")
			}
		}
		uploads[id] = data
	}
	return uploads, nil
}

func (s *Server) clearComposerDraft(c *gin.Context) {
	key, ok := s.composerDraftKey(c)
	if !ok {
		return
	}
	revision, err := strconv.ParseInt(c.Query("revision"), 10, 64)
	if err != nil || revision < 0 {
		s.fail(c, fmt.Errorf("%w: a valid revision is required", store.ErrInvalidDraft))
		return
	}
	draft, err := s.engine.Store().ClearComposerDraft(c.Request.Context(), key, revision)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"draft": draft})
}

func (s *Server) getComposerDraftFile(c *gin.Context) {
	key, ok := s.composerDraftKey(c)
	if !ok {
		return
	}
	draft, err := s.engine.Store().GetComposerDraft(c.Request.Context(), key)
	if err != nil {
		s.fail(c, err)
		return
	}
	for _, file := range draft.Files {
		if file.ID != c.Param("file") {
			continue
		}
		data, err := s.engine.Store().GetComposerDraftFile(c.Request.Context(), key, file.ID)
		if err != nil {
			s.fail(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		c.Header("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(file.Name))
		c.Header("X-Content-Type-Options", "nosniff")
		http.ServeContent(c.Writer, c.Request, file.Name, draft.UpdatedAt, bytes.NewReader(data))
		return
	}
	s.fail(c, store.ErrNotFound)
}
