package desktop

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"
)

type directoryRequest struct {
	Type       string `json:"type"`
	RequestID  string `json:"requestId"`
	Action     string `json:"action"`
	Directory  string `json:"directory,omitempty"`
	Title      string `json:"title,omitempty"`
	ButtonText string `json:"buttonText,omitempty"`
}

type directoryResult struct {
	RequestID string `json:"requestId"`
	Path      string `json:"path"`
	Error     string `json:"error,omitempty"`
	Code      string `json:"code,omitempty"`
}

// The window loads the engine's HTTP origin, which cannot serve Wails'
// /wails/runtime endpoint. Its native message bridge still reaches this shell.
func directoryMessageHandler(expectedURL string, choose func(application.Window, directoryRequest) (string, error)) func(application.Window, string, *application.OriginInfo) {
	return func(window application.Window, message string, origin *application.OriginInfo) {
		if window == nil {
			return
		}
		handleDirectoryMessage(expectedURL, message, origin, func(req directoryRequest) (string, error) {
			return choose(window, req)
		}, window.ExecJS)
	}
}

func chooseNativeDirectory(window application.Window, req directoryRequest) (string, error) {
	return application.Get().Dialog.OpenFileWithOptions(directoryDialogOptions(req, window)).PromptForSingleSelection()
}

func directoryDialogOptions(req directoryRequest, window application.Window) *application.OpenFileDialogOptions {
	return &application.OpenFileDialogOptions{
		CanChooseDirectories: true,
		CanChooseFiles:       false,
		CanCreateDirectories: false,
		// With no extension filter, macOS otherwise disables directory confirmation.
		AllowsOtherFileTypes: true,
		ResolvesAliases:      true,
		Window:               window,
		Title:                req.Title,
		ButtonText:           req.ButtonText,
		Directory:            req.Directory,
	}
}

func handleDirectoryMessage(expectedURL, message string, origin *application.OriginInfo, choose func(directoryRequest) (string, error), execJS func(string)) {
	// Navigation must not give an unrelated page the engine window's local
	// directory capability. Windows also supplies the top frame's URL.
	if origin == nil || !sameDirectoryOrigin(expectedURL, origin.Origin) ||
		(origin.TopOrigin != "" && !sameDirectoryOrigin(expectedURL, origin.TopOrigin)) {
		return
	}
	var req directoryRequest
	if err := json.Unmarshal([]byte(message), &req); err != nil || req.Type != "zwai:directory" || req.Action != "choose" || req.RequestID == "" {
		return
	}

	result := directoryResult{RequestID: req.RequestID}
	path, err := choose(req)
	if err != nil {
		result.Code = "directory_pick_failed"
		result.Error = fmt.Sprintf("choose working directory: %v", err)
	} else if path != "" {
		if err := validatePickedDirectory(path); err != nil {
			result.Code = "directory_invalid"
			result.Error = err.Error()
		} else {
			result.Path = path
		}
	}
	// An empty selection is cancellation. JSON escapes paths before they
	// enter script; the request ID lets a closed dialog ignore a late result.
	data, _ := json.Marshal(result)
	// The native panel can outlive a page navigation. Recheck the receiving
	// page before giving its JavaScript access to the selected local path.
	expected, _ := url.Parse(expectedURL) // Already validated before opening the panel.
	trustedOrigin, _ := json.Marshal(expected.Scheme + "://" + expected.Host)
	execJS(fmt.Sprintf(`if(window.location.origin===%s){window.dispatchEvent(new CustomEvent("zwai:directory-result",{detail:%s}));}`, trustedOrigin, data))
}

func sameDirectoryOrigin(expectedURL, actualURL string) bool {
	expected, err := url.Parse(expectedURL)
	if err != nil || expected.Scheme == "" || expected.Host == "" {
		return false
	}
	actual, err := url.Parse(actualURL)
	if err != nil {
		return false
	}
	return actual.Scheme == expected.Scheme && actual.Host == expected.Host
}

func validatePickedDirectory(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("working directory must be an absolute path")
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("read working directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("working directory is not a directory")
	}
	return nil
}
