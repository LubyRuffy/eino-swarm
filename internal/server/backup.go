package server

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/backup"
	"github.com/gin-gonic/gin"
)

// A backup can be the whole data directory. The multipart limit is for chat
// uploads, so a restore streams the body to a file instead of buffering it.
const maxBackupBody = 64 << 30

type backupRequest struct {
	Kind        string `json:"kind"`
	ProjectID   string `json:"project_id"`
	OmitSecrets bool   `json:"omit_secrets"`
	Output      string `json:"output"`
	SkipActive  bool   `json:"skip_active"`
}

type restoreRequest struct {
	Path           string `json:"path"`
	Replace        bool   `json:"replace"`
	ReplaceProject bool   `json:"replace_project"`
	Confirm        bool   `json:"confirm"`
}

func (s *Server) postBackup(c *gin.Context) {
	var req backupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		s.fail(c, backup.ErrInvalid)
		return
	}
	kind := backup.Kind(req.Kind)
	if kind == "" {
		kind = backup.KindFull
	}
	output := req.Output
	stream := output == ""
	if stream {
		path, cleanup, err := backupTemp(s.engine.Config().DataDir(), "zwai-backup-*"+backup.Extension)
		if err != nil {
			s.fail(c, err)
			return
		}
		defer cleanup()
		output = path
	}
	manifest, err := s.engine.BackupData(c.Request.Context(), backup.Options{
		Kind:        kind,
		ProjectID:   req.ProjectID,
		OmitSecrets: req.OmitSecrets,
		Output:      output,
		SkipActive:  req.SkipActive,
	})
	if err != nil {
		s.fail(c, err)
		return
	}
	if !stream {
		c.JSON(http.StatusOK, manifest)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="zwai.zwai-backup"`)
	c.File(output)
}

func (s *Server) postBackupPreview(c *gin.Context) {
	path, cleanup, err := s.backupSource(c)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		s.fail(c, err)
		return
	}
	report, err := s.engine.PreviewBackup(c.Request.Context(), path)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, report)
}

func (s *Server) postBackupRestore(c *gin.Context) {
	path, cleanup, err := s.backupSource(c)
	if cleanup != nil {
		defer cleanup()
	}
	if err != nil {
		s.fail(c, err)
		return
	}
	opts := restoreFlags(c, path)
	manifest, err := s.engine.RestoreBackup(c.Request.Context(), opts)
	if err != nil {
		s.fail(c, err)
		return
	}
	c.JSON(http.StatusOK, manifest)
}

// backupSource is a path from JSON, or a streamed body saved outside the
// data directory. The cleanup removes that temporary file.
func (s *Server) backupSource(c *gin.Context) (string, func(), error) {
	if c.ContentType() == "application/octet-stream" {
		return saveUpload(c.Request.Body, s.engine.Config().DataDir())
	}
	var req restoreRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Path) == "" {
		return "", nil, backup.ErrInvalid
	}
	c.Set("backup_request", req)
	return req.Path, nil, nil
}

func restoreFlags(c *gin.Context, path string) backup.RestoreOptions {
	opts := backup.RestoreOptions{Path: path}
	if raw, ok := c.Get("backup_request"); ok {
		req := raw.(restoreRequest)
		opts.Confirm = req.Confirm
		opts.Replace = req.Replace
		opts.ReplaceProject = req.ReplaceProject
		return opts
	}
	opts.Confirm = truthy(c.Query("confirm"))
	opts.Replace = truthy(c.Query("replace"))
	opts.ReplaceProject = truthy(c.Query("replace_project"))
	return opts
}

func truthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

func saveUpload(r io.Reader, dataDir string) (string, func(), error) {
	path, cleanup, err := backupTemp(dataDir, "zwai-upload-*"+backup.Extension)
	if err != nil {
		return "", nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_TRUNC, 0)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	n, copyErr := io.Copy(f, io.LimitReader(r, maxBackupBody+1))
	closeErr := f.Close()
	if copyErr != nil || closeErr != nil || n == 0 || n > maxBackupBody {
		cleanup()
		if copyErr != nil {
			return "", nil, copyErr
		}
		if closeErr != nil {
			return "", nil, closeErr
		}
		return "", nil, backup.ErrInvalid
	}
	return path, cleanup, nil
}

func backupTemp(dataDir, pattern string) (string, func(), error) {
	f, err := os.CreateTemp(uploadParent(dataDir), pattern)
	if err != nil {
		return "", nil, err
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", nil, err
	}
	return path, func() { _ = os.Remove(path) }, nil
}

func uploadParent(dataDir string) string {
	parent := os.TempDir()
	absData, err := filepath.Abs(dataDir)
	if err != nil {
		return parent
	}
	absParent, err := filepath.Abs(parent)
	if err != nil {
		return parent
	}
	if absParent == absData || strings.HasPrefix(absParent, absData+string(os.PathSeparator)) {
		return filepath.Dir(absData)
	}
	return parent
}
