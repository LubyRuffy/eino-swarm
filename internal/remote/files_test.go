package remote

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestPhoneCanListAndPreviewConversationFiles(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("outputs", "", "")
	if err != nil {
		t.Fatal(err)
	}
	root := e.WorkspaceDir(th.ID)
	if err := os.MkdirAll(filepath.Join(root, "reports"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "reports", "result.txt"), []byte("verified result"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AddAttachment(&store.Attachment{ThreadID: th.ID, Name: "result.txt", RelPath: "reports/result.txt"}); err != nil {
		t.Fatal(err)
	}

	listed := Handle(e, config.RemoteConfig{}, Request{ID: "list", Op: "files", ThreadID: th.ID}, "relay", "session")
	if !listed.OK {
		t.Fatalf("phone cannot list conversation outputs: %+v", listed)
	}
	if len(listed.Files) != 2 || listed.Files[0].Path != "reports" || !listed.Files[0].Dir ||
		listed.Files[1].Path != "reports/result.txt" || listed.Files[1].Dir || !listed.Files[1].Uploaded {
		t.Fatalf("workspace tree changed across PC/phone: %+v", listed.Files)
	}
	preview := Handle(e, config.RemoteConfig{}, Request{
		ID: "preview", Op: "file_chunk", ThreadID: th.ID, FilePath: "reports/result.txt",
	}, "relay", "session")
	if !preview.OK {
		t.Fatalf("phone cannot preview an output: %+v", preview)
	}
	chunk := preview.FileChunk
	if chunk == nil || chunk.Size != int64(len("verified result")) || chunk.More || chunk.MIME != "text/plain" {
		t.Fatalf("bad preview metadata: %+v", chunk)
	}
	data, err := base64.StdEncoding.DecodeString(chunk.Data)
	if err != nil || string(data) != "verified result" {
		t.Fatalf("preview bytes = %q, %v", data, err)
	}
}

func TestPhoneFileReadHandlesMissingWorkspaceAndUnknownType(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("outputs", "", "")
	if err != nil {
		t.Fatal(err)
	}
	root := e.WorkspaceDir(th.ID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "artifact.unknown_extension_48"), []byte("plain output"), 0o600); err != nil {
		t.Fatal(err)
	}
	preview := Handle(e, config.RemoteConfig{}, Request{
		Op: OpFileChunk, ThreadID: th.ID, FilePath: "artifact.unknown_extension_48",
	}, "relay", "session")
	if !preview.OK || preview.FileChunk == nil || preview.FileChunk.MIME != "text/plain" {
		t.Fatalf("unknown extension should still preview ordinary text: %+v", preview)
	}
	if resp := Handle(e, config.RemoteConfig{}, Request{Op: OpFileChunk, ThreadID: "missing", FilePath: "artifact"}, "relay", "session"); resp.OK {
		t.Fatalf("missing conversation read succeeded: %+v", resp)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if resp := Handle(e, config.RemoteConfig{}, Request{Op: OpFileChunk, ThreadID: th.ID, FilePath: "artifact"}, "relay", "session"); resp.OK {
		t.Fatalf("missing workspace read succeeded: %+v", resp)
	}
	if err := os.WriteFile(root, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if resp := Handle(e, config.RemoteConfig{}, Request{Op: OpFiles, ThreadID: th.ID}, "relay", "session"); resp.OK {
		t.Fatalf("unusable workspace listing succeeded: %+v", resp)
	}
}

func TestPhoneFileChunksStayInsideFramesAndWorkspace(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("outputs", "", "")
	if err != nil {
		t.Fatal(err)
	}
	root := e.WorkspaceDir(th.ID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	content := bytes.Repeat([]byte("x"), FileChunkRaw+9)
	if err := os.WriteFile(filepath.Join(root, "long.txt"), content, 0o600); err != nil {
		t.Fatal(err)
	}
	first := Handle(e, config.RemoteConfig{}, Request{Op: OpFileChunk, ThreadID: th.ID, FilePath: "long.txt"}, "relay", "session")
	if !first.OK || first.FileChunk == nil || !first.FileChunk.More || first.FileChunk.NextOffset != FileChunkRaw {
		t.Fatalf("first chunk = %+v", first)
	}
	raw, err := json.Marshal(first)
	if err != nil || len(raw) > plaintextBudget() {
		t.Fatalf("file chunk exceeded sealed frame: %d, %v", len(raw), err)
	}
	second := Handle(e, config.RemoteConfig{}, Request{
		Op: OpFileChunk, ThreadID: th.ID, FilePath: "long.txt", Before: first.FileChunk.NextOffset,
	}, "relay", "session")
	if !second.OK || second.FileChunk == nil || second.FileChunk.More || second.FileChunk.NextOffset != int64(len(content)) {
		t.Fatalf("last chunk = %+v", second)
	}
	for _, req := range []Request{
		{Op: OpFileChunk, ThreadID: th.ID, FilePath: "../outside.txt"},
		{Op: OpFileChunk, ThreadID: th.ID, FilePath: "long.txt", Before: -1},
		{Op: OpFileChunk, ThreadID: th.ID, FilePath: "long.txt", Before: int64(len(content)) + 1},
		{Op: OpFileChunk, ThreadID: th.ID, FilePath: "missing.txt"},
		{Op: OpFiles, ThreadID: th.ID, Cursor: "bad"},
		{Op: OpFiles, ThreadID: "missing"},
	} {
		resp := Handle(e, config.RemoteConfig{}, req, "relay", "session")
		if resp.OK {
			t.Fatalf("unsafe file request accepted: %+v", req)
		}
	}
	outside := filepath.Join(t.TempDir(), "private.txt")
	if err := os.WriteFile(outside, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape.txt")); err == nil {
		resp := Handle(e, config.RemoteConfig{}, Request{
			Op: OpFileChunk, ThreadID: th.ID, FilePath: "escape.txt",
		}, "relay", "session")
		if resp.OK {
			t.Fatal("workspace symlink escaped to a host file")
		}
	}
}

func TestPhoneFileListingPagesBeforeTheSealedFrameCap(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("outputs", "", "")
	if err != nil {
		t.Fatal(err)
	}
	root := e.WorkspaceDir(th.ID)
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 700; i++ {
		name := "report-" + strconv.Itoa(i) + "-long-descriptive-artifact-name.txt"
		if err := os.WriteFile(filepath.Join(root, name), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cursor := ""
	seen := map[string]bool{}
	for {
		resp := Handle(e, config.RemoteConfig{}, Request{Op: OpFiles, ThreadID: th.ID, Cursor: cursor}, "relay", "session")
		if !resp.OK || len(resp.Files) == 0 {
			t.Fatalf("file page failed: %+v", resp)
		}
		raw, err := json.Marshal(resp)
		if err != nil || len(raw) > plaintextBudget() {
			t.Fatalf("file page exceeded sealed frame: %d, %v", len(raw), err)
		}
		for _, file := range resp.Files {
			if seen[file.Path] {
				t.Fatalf("repeated file %s", file.Path)
			}
			seen[file.Path] = true
		}
		if !resp.More {
			break
		}
		cursor = resp.Next
	}
	if len(seen) != 700 {
		t.Fatalf("paged listing omitted outputs: %d of 700", len(seen))
	}
}
