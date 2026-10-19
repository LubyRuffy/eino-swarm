package desktop

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const directoryTestURL = "http://127.0.0.1:12345/?shell=desktop"

type directoryRecordingWindow struct {
	application.Window
	script string
}

func (w *directoryRecordingWindow) ExecJS(script string) {
	w.script = script
}

func directoryTestOrigin() *application.OriginInfo {
	return &application.OriginInfo{Origin: directoryTestURL, IsMainFrame: true}
}

func directoryTestMessage(t *testing.T, req directoryRequest) string {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func directoryTestReply(t *testing.T, script string) directoryResult {
	t.Helper()
	const prefix = `window.dispatchEvent(new CustomEvent("zwai:directory-result",{detail:`
	const suffix = `}));`
	start, end := strings.Index(script, prefix), strings.LastIndex(script, suffix)
	if start < 0 || end < start {
		t.Fatalf("directory result never reaches the page: %q", script)
	}
	var got directoryResult
	if err := json.Unmarshal([]byte(script[start+len(prefix):end]), &got); err != nil {
		t.Fatalf("directory result is not valid JSON: %v", err)
	}
	return got
}

func TestDesktopDirectorySelectionUsesTheNativeBridgeWithoutTakingFileDrops(t *testing.T) {
	// A whole-window native file-drop overlay would consume the composer's
	// existing DOM uploads. Directory selection must leave that path alone.
	opts := Options{URL: directoryTestURL}
	if appOptions(opts).RawMessageHandler == nil {
		t.Fatal("the external HTTP page has no native directory bridge")
	}
	if windowOptions(opts).EnableFileDrop {
		t.Fatal("directory selection intercepted composer file drops")
	}
	appOptions(opts).RawMessageHandler(nil, "{}", directoryTestOrigin())
}

func TestNativeDirectoryReplyReachesTheWindowThatRequestedIt(t *testing.T) {
	window := &directoryRecordingWindow{}
	path := t.TempDir()
	req := directoryRequest{Type: "zwai:directory", RequestID: "window", Action: "choose"}
	handler := directoryMessageHandler(directoryTestURL, func(got application.Window, selection directoryRequest) (string, error) {
		if got != window || selection != req {
			t.Fatal("native selection became detached from its window or request")
		}
		return path, nil
	})
	handler(window, directoryTestMessage(t, req), directoryTestOrigin())
	got := directoryTestReply(t, window.script)
	if got.Path != path || got.RequestID != req.RequestID {
		t.Fatalf("selection was delivered to the wrong window or request: %+v", got)
	}
}

func TestDirectoryPickerOnlySelectsOneExistingDirectory(t *testing.T) {
	req := directoryRequest{Directory: t.TempDir(), Title: "选择工作目录", ButtonText: "选择目录"}
	got := directoryDialogOptions(req, nil)
	if !got.CanChooseDirectories || got.CanChooseFiles || got.CanCreateDirectories || got.AllowsMultipleSelection {
		t.Fatalf("picker allows files, new directories or multiple selections: %+v", got)
	}
	if !got.AllowsOtherFileTypes {
		t.Fatal("a directory picker without extension filters must accept folders of any type")
	}
	if got.Title != req.Title || got.ButtonText != req.ButtonText || got.Directory != req.Directory || !got.ResolvesAliases {
		t.Fatalf("picker lost the caller's localized labels or directory: %+v", got)
	}
}

func TestChoosingADirectoryReturnsTheWholePathToTheSameRequest(t *testing.T) {
	// Quotes, backslashes and CJK must survive the JavaScript callback;
	// interpolating an unescaped path would either break or execute script.
	path := filepath.Join(t.TempDir(), `目录 "quoted" \ suffix`)
	if runtime.GOOS == "windows" {
		// A Windows path already exercises JSON backslash escaping; quotes
		// and backslashes cannot be part of a directory's basename there.
		path = filepath.Join(t.TempDir(), "目录 with spaces")
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	req := directoryRequest{Type: "zwai:directory", RequestID: "request-1", Action: "choose", Directory: t.TempDir(), Title: "选择工作目录", ButtonText: "选择目录"}
	var script string
	handleDirectoryMessage(directoryTestURL, directoryTestMessage(t, req), directoryTestOrigin(), func(got directoryRequest) (string, error) {
		if got != req {
			t.Fatalf("picker input changed: %+v", got)
		}
		return path, nil
	}, func(js string) { script = js })
	got := directoryTestReply(t, script)
	if got.RequestID != req.RequestID || got.Path != path || got.Error != "" || got.Code != "" {
		t.Fatalf("selection did not reach its request intact: %+v", got)
	}
}

func TestCancellingDirectorySelectionPreservesTheCurrentValue(t *testing.T) {
	req := directoryRequest{Type: "zwai:directory", RequestID: "cancel", Action: "choose"}
	var script string
	handleDirectoryMessage(directoryTestURL, directoryTestMessage(t, req), directoryTestOrigin(), func(directoryRequest) (string, error) {
		return "", nil
	}, func(js string) { script = js })
	got := directoryTestReply(t, script)
	if got.RequestID != req.RequestID || got.Path != "" || got.Error != "" || got.Code != "" {
		t.Fatalf("cancel became a failure or a replacement path: %+v", got)
	}
	if !strings.Contains(script, `"path":""`) {
		t.Fatal("cancel omitted the empty path required by the native result protocol")
	}
}

func TestNativeSelectionPreservesAWhitespaceSuffix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support these trailing-whitespace directory names")
	}
	path := filepath.Join(t.TempDir(), "chosen \t")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	req := directoryRequest{Type: "zwai:directory", RequestID: "identity", Action: "choose"}
	var script string
	handleDirectoryMessage(directoryTestURL, directoryTestMessage(t, req), directoryTestOrigin(), func(directoryRequest) (string, error) {
		return path, nil
	}, func(js string) { script = js })
	if got := directoryTestReply(t, script); got.Path != path || got.Error != "" {
		t.Fatalf("native reply changed the selected filesystem identity: %+v", got)
	}
}

func TestDirectoryPickerFailuresRetainTheConcreteReason(t *testing.T) {
	req := directoryRequest{Type: "zwai:directory", RequestID: "failed", Action: "choose"}
	var script string
	handleDirectoryMessage(directoryTestURL, directoryTestMessage(t, req), directoryTestOrigin(), func(directoryRequest) (string, error) {
		return "", errors.New("native panel unavailable")
	}, func(js string) { script = js })
	got := directoryTestReply(t, script)
	if got.Code != "directory_pick_failed" || !strings.Contains(got.Error, "native panel unavailable") || got.Path != "" {
		t.Fatalf("picker failure lost its category or reason: %+v", got)
	}
}

func TestDirectoryPickerCannotReturnAFileOrUnavailablePath(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(file, []byte("content"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, path, reason string }{
		{"file", file, "not a directory"},
		{"relative", "relative/path", "absolute"},
		{"missing", filepath.Join(t.TempDir(), "missing"), "read working directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := directoryRequest{Type: "zwai:directory", RequestID: tc.name, Action: "choose"}
			var script string
			handleDirectoryMessage(directoryTestURL, directoryTestMessage(t, req), directoryTestOrigin(), func(directoryRequest) (string, error) {
				return tc.path, nil
			}, func(js string) { script = js })
			got := directoryTestReply(t, script)
			if got.Code != "directory_invalid" || !strings.Contains(got.Error, tc.reason) || got.Path != "" {
				t.Fatalf("invalid directory was accepted or became vague: %+v", got)
			}
		})
	}
}

func TestOtherPagesAndMessagesCannotOpenTheNativeDirectoryPicker(t *testing.T) {
	req := directoryRequest{Type: "zwai:directory", RequestID: "choose", Action: "choose"}
	message := directoryTestMessage(t, req)
	for _, tc := range []struct {
		name, expectedURL, message string
		origin                     *application.OriginInfo
	}{
		{"no origin", directoryTestURL, message, nil},
		{"blank origin", directoryTestURL, message, &application.OriginInfo{}},
		{"other scheme", directoryTestURL, message, &application.OriginInfo{Origin: "https://127.0.0.1:12345/"}},
		{"other host", directoryTestURL, message, &application.OriginInfo{Origin: "http://example.com:12345/"}},
		{"other port", directoryTestURL, message, &application.OriginInfo{Origin: "http://127.0.0.1:12346/"}},
		{"foreign top frame", directoryTestURL, message, &application.OriginInfo{Origin: directoryTestURL, TopOrigin: "https://example.com/"}},
		{"broken origin", directoryTestURL, message, &application.OriginInfo{Origin: "http://["}},
		{"relative expected URL", "/", message, directoryTestOrigin()},
		{"broken expected URL", "http://[", message, directoryTestOrigin()},
		{"broken message", directoryTestURL, "{", directoryTestOrigin()},
		{"other message", directoryTestURL, `{"type":"other","requestId":"a","action":"choose"}`, directoryTestOrigin()},
		{"no request id", directoryTestURL, `{"type":"zwai:directory","action":"choose"}`, directoryTestOrigin()},
		{"other action", directoryTestURL, `{"type":"zwai:directory","requestId":"a","action":"drop"}`, directoryTestOrigin()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			handleDirectoryMessage(tc.expectedURL, tc.message, tc.origin, func(directoryRequest) (string, error) {
				t.Fatal("untrusted page or invalid message opened the native picker")
				return "", nil
			}, func(string) { t.Fatal("untrusted page or invalid message received a native response") })
		})
	}
}

func TestDirectoryPickerAcceptsSameOriginURLsWithDifferentPageState(t *testing.T) {
	req := directoryRequest{Type: "zwai:directory", RequestID: "state", Action: "choose"}
	origin := &application.OriginInfo{Origin: "http://127.0.0.1:12345/?shell=desktop&pid=1#project", TopOrigin: directoryTestURL}
	var script string
	handleDirectoryMessage(directoryTestURL, directoryTestMessage(t, req), origin, func(directoryRequest) (string, error) {
		return "", nil
	}, func(js string) { script = js })
	if directoryTestReply(t, script).RequestID != req.RequestID {
		t.Fatal("page state changed the directory request's identity")
	}
}

func TestDirectorySelectionCannotReachAPageNavigatedWhileThePickerWasOpen(t *testing.T) {
	// The native panel waits independently of the page. A result must check
	// the receiving page again before exposing a local path to its JavaScript.
	req := directoryRequest{Type: "zwai:directory", RequestID: "navigation", Action: "choose"}
	path := t.TempDir()
	var script string
	handleDirectoryMessage(directoryTestURL, directoryTestMessage(t, req), directoryTestOrigin(), func(directoryRequest) (string, error) {
		return path, nil
	}, func(js string) { script = js })
	const guard = `if(window.location.origin==="http://127.0.0.1:12345"){`
	if !strings.HasPrefix(script, guard) || !strings.HasSuffix(script, "}") {
		t.Fatalf("a navigated page can receive the local path: %s", script)
	}
	if directoryTestReply(t, script).Path != path {
		t.Fatal("origin guard changed the trusted page's directory result")
	}
}
