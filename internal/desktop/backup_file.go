package desktop

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/backup"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type backupFileRequest struct {
	Type       string `json:"type"`
	RequestID  string `json:"requestId"`
	Action     string `json:"action"`
	Directory  string `json:"directory,omitempty"`
	Title      string `json:"title,omitempty"`
	ButtonText string `json:"buttonText,omitempty"`
	Filename   string `json:"filename,omitempty"`
}

type backupFileResult struct {
	RequestID string `json:"requestId"`
	Path      string `json:"path"`
	Error     string `json:"error,omitempty"`
	Code      string `json:"code,omitempty"`
}

// backupFileMessageHandler opens or saves one .zwai-backup. The page is the
// engine origin, so the request arrives as a raw window message. A message
// for another bridge is ignored.
func backupFileMessageHandler(expectedURL string, choose func(application.Window, backupFileRequest) (string, error)) func(application.Window, string, *application.OriginInfo) {
	return func(window application.Window, message string, origin *application.OriginInfo) {
		if window == nil {
			return
		}
		handleBackupFileMessage(expectedURL, message, origin, func(req backupFileRequest) (string, error) {
			return choose(window, req)
		}, window.ExecJS)
	}
}

var (
	saveBackupDialog = func(opts *application.SaveFileDialogOptions) (string, error) {
		return application.Get().Dialog.SaveFileWithOptions(opts).PromptForSingleSelection()
	}
	openBackupDialog = func(opts *application.OpenFileDialogOptions) (string, error) {
		return application.Get().Dialog.OpenFileWithOptions(opts).PromptForSingleSelection()
	}
)

func chooseNativeBackupFile(window application.Window, req backupFileRequest) (string, error) {
	switch req.Action {
	case "save":
		return saveBackupDialog(saveBackupOptions(req, window))
	case "open":
		return openBackupDialog(openBackupOptions(req, window))
	default:
		return "", fmt.Errorf("unknown backup file action %q", req.Action)
	}
}

func backupFileFilter() []application.FileFilter {
	return []application.FileFilter{{
		DisplayName: "zwai backup",
		Pattern:     "*" + backup.Extension,
	}}
}

func saveBackupOptions(req backupFileRequest, window application.Window) *application.SaveFileDialogOptions {
	return &application.SaveFileDialogOptions{
		CanCreateDirectories: true,
		Title:                req.Title,
		ButtonText:           req.ButtonText,
		Directory:            req.Directory,
		Filename:             req.Filename,
		Filters:              backupFileFilter(),
		Window:               window,
	}
}

func openBackupOptions(req backupFileRequest, window application.Window) *application.OpenFileDialogOptions {
	return &application.OpenFileDialogOptions{
		CanChooseFiles:       true,
		CanChooseDirectories: false,
		Title:                req.Title,
		ButtonText:           req.ButtonText,
		Directory:            req.Directory,
		Filters:              backupFileFilter(),
		Window:               window,
	}
}

func handleBackupFileMessage(expectedURL, message string, origin *application.OriginInfo, choose func(backupFileRequest) (string, error), execJS func(string)) {
	if origin == nil || !sameDirectoryOrigin(expectedURL, origin.Origin) ||
		(origin.TopOrigin != "" && !sameDirectoryOrigin(expectedURL, origin.TopOrigin)) {
		return
	}
	var req backupFileRequest
	if err := json.Unmarshal([]byte(message), &req); err != nil || req.Type != "zwai:backup-file" || req.RequestID == "" {
		return
	}
	if req.Action != "save" && req.Action != "open" {
		return
	}
	result := backupFileResult{RequestID: req.RequestID}
	path, err := choose(req)
	if err != nil {
		result.Code = "backup_file_failed"
		result.Error = fmt.Sprintf("choose backup file: %v", err)
	} else if path != "" {
		if err := validateBackupFile(path, req.Action); err != nil {
			result.Code = "backup_file_invalid"
			result.Error = err.Error()
		} else {
			result.Path = path
		}
	}
	data, _ := json.Marshal(result)
	expected, _ := url.Parse(expectedURL)
	trustedOrigin, _ := json.Marshal(expected.Scheme + "://" + expected.Host)
	execJS(fmt.Sprintf(`if(window.location.origin===%s){window.dispatchEvent(new CustomEvent("zwai:backup-file-result",{detail:%s}));}`, trustedOrigin, data))
}

func validateBackupFile(path, action string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("backup file must be an absolute path")
	}
	if !strings.HasSuffix(strings.ToLower(path), backup.Extension) {
		return fmt.Errorf("backup file must use %s", backup.Extension)
	}
	info, err := os.Lstat(path)
	if action == "save" && os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read backup file: %w", err)
	}
	if info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("backup file is not a file")
	}
	return nil
}
