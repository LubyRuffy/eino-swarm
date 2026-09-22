package server

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/gin-gonic/gin"
)

// The cap lives on the engine so a phone put cannot accept a file the desktop
// upload would refuse.

func (s *Server) listFiles(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	files, err := s.engine.ListFiles(th.ID)
	if err != nil {
		s.fail(c, err)
		return
	}
	uploads, err := s.engine.Store().ListAttachments(th.ID)
	if err != nil {
		s.fail(c, err)
		return
	}
	// Mark which files the human put there, so the Files panel can separate
	// "what I gave it" from "what it produced".
	uploaded := map[string]bool{}
	for _, a := range uploads {
		uploaded[a.RelPath] = true
	}
	type fileView struct {
		engineFile
		Uploaded bool `json:"uploaded"`
	}
	out := make([]fileView, 0, len(files))
	for _, f := range files {
		out = append(out, fileView{engineFile: engineFile(f), Uploaded: uploaded[f.Path]})
	}
	c.JSON(http.StatusOK, gin.H{
		"workspace": s.engine.WorkspaceDir(th.ID),
		"files":     out,
	})
}

// engineFile mirrors engine.FileEntry so the response can add a field to it
// without the engine having to know about HTTP concerns.
type engineFile = fileEntryAlias

func (s *Server) uploadFiles(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	form, err := c.MultipartForm()
	if err != nil {
		badRequest(c, "could not read the upload: %v", err)
		return
	}
	headers := form.File["files"]
	if len(headers) == 0 {
		headers = form.File["file"]
	}
	if len(headers) == 0 {
		badRequest(c, "no files were attached")
		return
	}

	saved := make([]*store.Attachment, 0, len(headers))
	for _, fh := range headers {
		if fh.Size > engine.MaxUploadBytes {
			badRequest(c, "%s is larger than the %d MiB upload limit", fh.Filename, engine.MaxUploadBytes>>20)
			return
		}
		att, err := s.engine.SaveUpload(th.ID, fh.Filename, copyFrom(fh))
		if err != nil {
			s.fail(c, err)
			return
		}
		saved = append(saved, att)
	}
	c.JSON(http.StatusCreated, gin.H{"files": saved})
}

// copyFrom streams one multipart part to its destination instead of buffering
// it: uploads are routinely bigger than is polite to hold in memory.
func copyFrom(fh *multipart.FileHeader) func(dst string) error {
	return func(dst string) error {
		src, err := fh.Open()
		if err != nil {
			return fmt.Errorf("open upload: %w", err)
		}
		defer src.Close()
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return fmt.Errorf("create %s: %w", filepath.Base(dst), err)
		}
		defer out.Close()
		if _, err := io.Copy(out, io.LimitReader(src, engine.MaxUploadBytes)); err != nil {
			return fmt.Errorf("write upload: %w", err)
		}
		return nil
	}
}

func (s *Server) downloadFile(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	rel := strings.TrimPrefix(c.Param("path"), "/")
	abs, err := s.engine.ResolveWorkspacePath(th.ID, rel)
	if err != nil {
		s.fail(c, err)
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no such file in this conversation's workspace"})
		return
	}
	if info.IsDir() {
		badRequest(c, "%s is a directory", rel)
		return
	}
	// Force a download rather than letting the browser render workspace
	// content in the app's own origin: an agent-written HTML file would
	// otherwise run as same-origin script.
	c.Header("Content-Disposition",
		fmt.Sprintf("attachment; filename*=UTF-8''%s", escapePath(filepath.Base(abs))))
	c.Header("X-Content-Type-Options", "nosniff")
	c.File(abs)
}

func (s *Server) deleteFile(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	rel := strings.TrimPrefix(c.Param("path"), "/")
	if err := s.engine.DeleteFile(th.ID, rel); err != nil {
		s.fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

type revealRequest struct {
	Path string `json:"path"`
}

// reveal opens the platform file manager at a workspace path. Only the desktop
// shell can do this; a browser gets 501 and the UI offers a download instead.
func (s *Server) reveal(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	if s.opts.Reveal == nil {
		c.JSON(http.StatusNotImplemented, gin.H{
			"error": "showing files in the file manager is only available in the desktop app",
		})
		return
	}
	var req revealRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "could not read the request body: %v", err)
		return
	}
	target := s.engine.WorkspaceDir(th.ID)
	if strings.TrimSpace(req.Path) != "" {
		abs, err := s.engine.ResolveWorkspacePath(th.ID, req.Path)
		if err != nil {
			s.fail(c, err)
			return
		}
		target = abs
	}
	if err := s.opts.Reveal(target); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"revealed": target})
}

// escapePath percent-encodes a file name for the Content-Disposition header.
func escapePath(name string) string {
	var b strings.Builder
	for _, r := range []byte(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '-', r == '_', r == '.', r == '~':
			b.WriteByte(r)
		default:
			fmt.Fprintf(&b, "%%%02X", r)
		}
	}
	return b.String()
}
