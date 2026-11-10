package desktop

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/backup"
	"github.com/wailsapp/wails/v3/pkg/application"
)

func TestBackupFileBridgeSavesAndOpensWithoutAPanel(t *testing.T) {
	window := &directoryRecordingWindow{}
	saved := t.TempDir() + "/copy" + backup.Extension
	opened := t.TempDir() + "/have" + backup.Extension
	if err := os.WriteFile(opened, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		saveBackupDialog = func(opts *application.SaveFileDialogOptions) (string, error) {
			return application.Get().Dialog.SaveFileWithOptions(opts).PromptForSingleSelection()
		}
		openBackupDialog = func(opts *application.OpenFileDialogOptions) (string, error) {
			return application.Get().Dialog.OpenFileWithOptions(opts).PromptForSingleSelection()
		}
	})
	saveBackupDialog = func(opts *application.SaveFileDialogOptions) (string, error) {
		if opts.Window != window || len(opts.Filters) != 1 || opts.Filters[0].Pattern != "*"+backup.Extension {
			t.Fatalf("save options = %+v", opts)
		}
		return saved, nil
	}
	openBackupDialog = func(opts *application.OpenFileDialogOptions) (string, error) {
		if opts.CanChooseDirectories || opts.Filters[0].Pattern != "*"+backup.Extension {
			t.Fatalf("open options = %+v", opts)
		}
		return opened, nil
	}
	handler := backupFileMessageHandler(directoryTestURL, chooseNativeBackupFile)
	handler(window, backupFileMessage(t, backupFileRequest{Type: "zwai:backup-file", RequestID: "save", Action: "save", Title: "保存备份"}), directoryTestOrigin())
	if got := backupFileReply(t, window.script); got.Path != saved || got.RequestID != "save" {
		t.Fatalf("save reply = %+v", got)
	}
	handler(window, backupFileMessage(t, backupFileRequest{Type: "zwai:backup-file", RequestID: "open", Action: "open"}), directoryTestOrigin())
	if got := backupFileReply(t, window.script); got.Path != opened {
		t.Fatalf("open reply = %+v", got)
	}
	if _, err := chooseNativeBackupFile(window, backupFileRequest{Action: "drop"}); err == nil {
		t.Fatal("unknown action was accepted")
	}
	handler(window, `{}`, directoryTestOrigin())
	handler(window, backupFileMessage(t, backupFileRequest{Type: "zwai:directory", RequestID: "nope", Action: "choose"}), directoryTestOrigin())
}

func TestBackupFileBridgeRejectsARelativePath(t *testing.T) {
	window := &directoryRecordingWindow{}
	handleBackupFileMessage(directoryTestURL, backupFileMessage(t, backupFileRequest{
		Type: "zwai:backup-file", RequestID: "bad", Action: "open",
	}), directoryTestOrigin(), func(backupFileRequest) (string, error) {
		return "relative" + backup.Extension, nil
	}, window.ExecJS)
	got := backupFileReply(t, window.script)
	if got.Code != "backup_file_invalid" || got.Path != "" {
		t.Fatalf("reply = %+v", got)
	}
}

func backupFileMessage(t *testing.T, req backupFileRequest) string {
	t.Helper()
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func backupFileReply(t *testing.T, script string) backupFileResult {
	t.Helper()
	const prefix = `window.dispatchEvent(new CustomEvent("zwai:backup-file-result",{detail:`
	start := strings.LastIndex(script, prefix)
	end := strings.LastIndex(script, `}));`)
	if start < 0 || end < start {
		t.Fatalf("backup result never reaches the page: %q", script)
	}
	var got backupFileResult
	if err := json.Unmarshal([]byte(script[start+len(prefix):end]), &got); err != nil {
		t.Fatal(err)
	}
	return got
}
