package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func clearEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{"OPENAI_API_KEY", "OPENAI_BASE_URL", "OPENAI_MODEL", "ZWAI_MODEL_API_KEY", "ZWAI_MODEL_BASE_URL", "ZWAI_MODEL_NAME"} {
		t.Setenv(key, "")
	}
}

type seeded struct {
	dir        string
	cfg        *config.Config
	st         *store.Store
	db         interface{ Ping() error }
	project    *store.Project
	other      *store.Project
	thread     *store.Thread
	standalone *store.Thread
	external   string
}

func seed(t *testing.T) seeded {
	t.Helper()
	clearEnv(t)
	dir := t.TempDir()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Models.Providers[0].APIKey = "sk-backup-secret-value"
	cfg.Models.Providers[0].Model = "backup-model"
	cfg.Tools.Proxy.HTTP = "http://proxyuser:proxypass@127.0.0.1:9"
	cfg.MCP.Servers = []config.MCPServer{{
		ID: "tools", Name: "tools", Transport: "stdio", Command: "true", Enabled: true,
		Env: map[string]string{"TOKEN": "mcp-secret-token"},
	}}
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	external := t.TempDir()
	writeText(t, filepath.Join(external, "outside-workdir.txt"), "outside-bytes")
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	project := &store.Project{Name: "alpha", Workdir: external, MemoryEnabled: true}
	if err := st.CreateProject(project); err != nil {
		t.Fatal(err)
	}
	other := &store.Project{Name: "beta", MemoryEnabled: true}
	if err := st.CreateProject(other); err != nil {
		t.Fatal(err)
	}
	thread := &store.Thread{Title: "alpha-thread", ProjectID: project.ID}
	if err := st.CreateThread(thread); err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: thread.ID, ProjectID: project.ID, UserText: "turn-body", Status: store.TurnDone}
	if err := st.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendMessages(thread.ID, turn.ID, []store.Message{{
		Role: "user", Content: "message-body",
		Images: []store.ImageRef{{ID: "img1", Name: "a.txt", MIME: "text/plain"}},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSchedule(&store.Schedule{
		Kind: store.ScheduleThread, ThreadID: thread.ID, ProjectID: project.ID,
		Prompt: "schedule-body", Title: "schedule-title", Status: store.ScheduleActive,
		CreatedBy: store.ScheduleCreatedHuman,
	}); err != nil {
		t.Fatal(err)
	}
	node := store.NuwaNode{ProjectID: project.ID, Kind: "fact", Title: "nuwa-title", ThreadID: thread.ID, TurnID: turn.ID, AgentID: "worker-1", Generation: 1}
	if err := st.UpsertNuwaNode(context.Background(), &node, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveComposerDraft(context.Background(), store.ComposerDraft{
		Key: "thread:" + thread.ID, Text: "draft-text", State: "editing",
		Files: []store.DraftFile{{ID: "file1", Name: "a.txt", MIME: "text/plain", Kind: "file", Size: int64(len("blob-bytes"))}},
	}, map[string][]byte{"file1": []byte("blob-bytes")}); err != nil {
		t.Fatal(err)
	}
	standalone := &store.Thread{Title: "standalone-title"}
	if err := st.CreateThread(standalone); err != nil {
		t.Fatal(err)
	}
	alone := &store.Turn{ThreadID: standalone.ID, UserText: "standalone-turn", Status: store.TurnDone}
	if err := st.CreateTurn(alone); err != nil {
		t.Fatal(err)
	}
	if err := st.AppendMessages(standalone.ID, alone.ID, []store.Message{{Role: "user", Content: "standalone-body"}}); err != nil {
		t.Fatal(err)
	}
	writeText(t, filepath.Join(cfg.ProjectMemoryDir(project.ID), "MEMORY.md"), "memory-alpha")
	writeText(t, filepath.Join(cfg.ProjectMemoryDir(project.ID), "skills", "local", "SKILL.md"), "project-skill")
	writeText(t, filepath.Join(cfg.ProjectDir(project.ID), "installed-skills", "demo", "SKILL.md"), "installed-skill")
	writeText(t, filepath.Join(cfg.ProjectWorkspaceDir(project.ID), "note.txt"), "managed-note")
	writeText(t, filepath.Join(cfg.ProjectMemoryDir(other.ID), "MEMORY.md"), "memory-beta")
	writeText(t, filepath.Join(cfg.ThreadInputsDir(thread.ID), "img1"), "image-bytes")
	writeText(t, cfg.ThreadPlanFile(thread.ID), "plan-body")
	writeText(t, filepath.Join(cfg.WorkspaceDir(thread.ID), "out.txt"), "workspace-out")
	writeText(t, filepath.Join(cfg.LibraryDir(), "skills", "shared", "SKILL.md"), "shared-skill")
	writeText(t, filepath.Join(cfg.RemoteDir(), "identity"), "source-remote")
	writeText(t, filepath.Join(dir, "engine.lock"), "lock")
	writeText(t, filepath.Join(dir, "diagnostics", "note.txt"), "diag")
	writeText(t, filepath.Join(dir, "releases", "note.txt"), "release")
	writeText(t, filepath.Join(dir, "ios-signing", "note.txt"), "signing")
	writeText(t, filepath.Join(dir, generationFile), "5")
	sqlDB, err := st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	return seeded{dir: dir, cfg: cfg, st: st, project: project, other: other, thread: thread, standalone: standalone, external: external, db: sqlDB}
}

func writeText(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func readText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func archiveHas(t *testing.T, path, needle string) bool {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, f := range r.File {
		if strings.Contains(f.Name, needle) {
			return true
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		body, err := bytes.NewBuffer(nil), error(nil)
		_, err = body.ReadFrom(rc)
		_ = rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(body.String(), needle) {
			return true
		}
	}
	return false
}

func TestFullBackupRoundTripKeepsUserData(t *testing.T) {
	src := seed(t)
	out := filepath.Join(t.TempDir(), "full.zwai-backup")
	sqlDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := Create(context.Background(), Options{DataDir: src.dir, DB: sqlDB, Kind: KindFull, Output: out})
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("backup mode %o, want 0600", info.Mode().Perm())
	}
	if manifest.Kind != KindFull || !manifest.IncludesSecrets || manifest.Counts.Projects != 2 || manifest.Counts.Threads != 2 {
		t.Fatalf("manifest %+v", manifest)
	}
	for _, absent := range []string{"engine.lock", "diagnostics", "releases", "ios-signing", generationFile, "outside-workdir.txt"} {
		if archiveHas(t, out, absent) {
			t.Fatalf("archive contains %s", absent)
		}
	}
	if !archiveHas(t, out, "sk-backup-secret-value") || !archiveHas(t, out, "source-remote") || !archiveHas(t, out, "memory-alpha") || !archiveHas(t, out, "shared-skill") {
		t.Fatal("full backup dropped user data or secrets")
	}
	if readText(t, filepath.Join(src.external, "outside-workdir.txt")) != "outside-bytes" {
		t.Fatal("backup changed the external directory")
	}
	reloaded, err := config.Load(src.dir)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Models.Providers[0].APIKey != "sk-backup-secret-value" {
		t.Fatal("creating a backup redacted the live config")
	}

	destDir := t.TempDir()
	destCfg, err := config.Load(destDir)
	if err != nil {
		t.Fatal(err)
	}
	destCfg.Models.Providers[0].Model = "dest-model"
	if err := destCfg.Save(); err != nil {
		t.Fatal(err)
	}
	writeText(t, filepath.Join(destDir, "engine.lock"), "keep-lock")
	writeText(t, filepath.Join(destDir, generationFile), "5")
	writeText(t, filepath.Join(destDir, "projects", "keep.txt"), "dest-only")
	preview, err := Preview(context.Background(), destDir, nil, out)
	if err != nil || preview.Action != ActionReplaceAll || preview.Manifest.Counts.Messages < 1 {
		t.Fatalf("preview %+v err=%v", preview, err)
	}
	if _, err := Restore(context.Background(), RestoreOptions{DataDir: destDir, Path: out, Confirm: true}); !errors.Is(err, ErrConfirm) {
		t.Fatalf("restore without replace: %v", err)
	}
	if readText(t, filepath.Join(destDir, "projects", "keep.txt")) != "dest-only" {
		t.Fatal("unconfirmed restore changed files")
	}
	if _, err := Restore(context.Background(), RestoreOptions{DataDir: destDir, Path: out, Confirm: true, Replace: true}); err != nil {
		t.Fatal(err)
	}
	if readText(t, filepath.Join(destDir, generationFile)) != "6" {
		t.Fatalf("generation = %s, want 6", readText(t, filepath.Join(destDir, generationFile)))
	}
	if readText(t, filepath.Join(destDir, "engine.lock")) != "keep-lock" {
		t.Fatal("restore replaced the engine lease")
	}
	assertRestored(t, destDir, src, true)
	if _, err := os.Stat(filepath.Join(destDir, "projects", "keep.txt")); !os.IsNotExist(err) {
		t.Fatal("full restore left a file that was not in the backup")
	}
	matches, _ := filepath.Glob(filepath.Join(destDir, ".backup-*"))
	if len(matches) != 0 {
		t.Fatalf("snapshot left behind: %v", matches)
	}
}

func assertRestored(t *testing.T, dir string, src seeded, full bool) {
	t.Helper()
	cfg, err := config.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Models.Providers[0].APIKey != "sk-backup-secret-value" || cfg.Models.Providers[0].Model != "backup-model" {
		t.Fatalf("config key=%q model=%q", cfg.Models.Providers[0].APIKey, cfg.Models.Providers[0].Model)
	}
	if cfg.MCP.Servers[0].Env["TOKEN"] != "mcp-secret-token" {
		t.Fatalf("mcp env = %#v", cfg.MCP.Servers[0].Env)
	}
	if !strings.Contains(cfg.Tools.Proxy.HTTP, "proxypass") {
		t.Fatalf("proxy = %s", cfg.Tools.Proxy.HTTP)
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	project, err := st.GetProject(src.project.ID)
	if err != nil || project.Name != "alpha" || project.Workdir != src.external {
		t.Fatalf("project %+v err=%v", project, err)
	}
	if _, err := st.GetProject(src.other.ID); err != nil {
		t.Fatal(err)
	}
	thread, err := st.GetThread(src.thread.ID)
	if err != nil || thread.Title != "alpha-thread" {
		t.Fatalf("thread %+v err=%v", thread, err)
	}
	var message store.Message
	if err := st.DB().Where("thread_id = ? AND content = ?", src.thread.ID, "message-body").First(&message).Error; err != nil {
		t.Fatal(err)
	}
	if len(message.Images) != 1 || message.Images[0].ID != "img1" {
		t.Fatalf("images %+v", message.Images)
	}
	var turn store.Turn
	if err := st.DB().Where("thread_id = ? AND user_text = ?", src.thread.ID, "turn-body").First(&turn).Error; err != nil {
		t.Fatal(err)
	}
	var schedule store.Schedule
	if err := st.DB().Where("project_id = ? AND prompt = ?", src.project.ID, "schedule-body").First(&schedule).Error; err != nil {
		t.Fatal(err)
	}
	var node store.NuwaNode
	if err := st.DB().Where("project_id = ? AND title = ?", src.project.ID, "nuwa-title").First(&node).Error; err != nil {
		t.Fatal(err)
	}
	draft, err := st.GetComposerDraft(context.Background(), "thread:"+src.thread.ID)
	if err != nil || draft.Text != "draft-text" {
		t.Fatalf("draft %+v err=%v", draft, err)
	}
	blob, err := st.GetComposerDraftFile(context.Background(), "thread:"+src.thread.ID, "file1")
	if err != nil || string(blob) != "blob-bytes" {
		t.Fatalf("blob %q err=%v", blob, err)
	}
	for _, pair := range [][2]string{
		{filepath.Join(cfg.ProjectMemoryDir(src.project.ID), "MEMORY.md"), "memory-alpha"},
		{filepath.Join(cfg.ProjectMemoryDir(src.project.ID), "skills", "local", "SKILL.md"), "project-skill"},
		{filepath.Join(cfg.ProjectDir(src.project.ID), "installed-skills", "demo", "SKILL.md"), "installed-skill"},
		{filepath.Join(cfg.ProjectWorkspaceDir(src.project.ID), "note.txt"), "managed-note"},
		{filepath.Join(cfg.ProjectMemoryDir(src.other.ID), "MEMORY.md"), "memory-beta"},
		{filepath.Join(cfg.ThreadInputsDir(src.thread.ID), "img1"), "image-bytes"},
		{cfg.ThreadPlanFile(src.thread.ID), "plan-body"},
		{filepath.Join(cfg.WorkspaceDir(src.thread.ID), "out.txt"), "workspace-out"},
	} {
		if readText(t, pair[0]) != pair[1] {
			t.Fatalf("%s = %q", pair[0], readText(t, pair[0]))
		}
	}
	if full {
		if readText(t, filepath.Join(cfg.LibraryDir(), "skills", "shared", "SKILL.md")) != "shared-skill" {
			t.Fatal("shared library missing")
		}
		if readText(t, filepath.Join(cfg.RemoteDir(), "identity")) != "source-remote" {
			t.Fatal("remote identity missing")
		}
		alone, err := st.GetThread(src.standalone.ID)
		if err != nil || alone.Title != "standalone-title" {
			t.Fatalf("standalone %+v err=%v", alone, err)
		}
		var body store.Message
		if err := st.DB().Where("content = ?", "standalone-body").First(&body).Error; err != nil {
			t.Fatal(err)
		}
	}
}

func TestOmitSecretsKeepsDestinationCredentials(t *testing.T) {
	src := seed(t)
	out := filepath.Join(t.TempDir(), "redacted.zwai-backup")
	sqlDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := Create(context.Background(), Options{DataDir: src.dir, DB: sqlDB, Kind: KindFull, Output: out, OmitSecrets: true})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.IncludesSecrets {
		t.Fatal("omit-secrets archive is marked as containing secrets")
	}
	for _, secret := range []string{"sk-backup-secret-value", "mcp-secret-token", "proxypass", "source-remote"} {
		if archiveHas(t, out, secret) {
			t.Fatalf("redacted archive contains %s", secret)
		}
	}
	destDir := t.TempDir()
	destCfg, err := config.Load(destDir)
	if err != nil {
		t.Fatal(err)
	}
	destCfg.Models.Providers[0].APIKey = "sk-destination-secret-value"
	destCfg.Models.Providers[0].Model = "dest-model"
	destCfg.MCP.Servers = []config.MCPServer{{
		ID: "tools", Name: "tools", Transport: "stdio", Command: "true", Enabled: true,
		Env: map[string]string{"TOKEN": "dest-mcp-token"},
	}}
	destCfg.Tools.Proxy.HTTP = "http://dest-user:dest-pass@127.0.0.1:9"
	if err := destCfg.Save(); err != nil {
		t.Fatal(err)
	}
	writeText(t, filepath.Join(destCfg.RemoteDir(), "identity"), "dest-remote")
	if _, err := Restore(context.Background(), RestoreOptions{DataDir: destDir, Path: out, Confirm: true, Replace: true}); err != nil {
		t.Fatal(err)
	}
	got, err := config.Load(destDir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Models.Providers[0].APIKey != "sk-destination-secret-value" || got.Models.Providers[0].Model != "backup-model" {
		t.Fatalf("key=%q model=%q", got.Models.Providers[0].APIKey, got.Models.Providers[0].Model)
	}
	if got.MCP.Servers[0].Env["TOKEN"] != "dest-mcp-token" {
		t.Fatalf("mcp = %#v", got.MCP.Servers[0].Env)
	}
	if !strings.Contains(got.Tools.Proxy.HTTP, "dest-pass") {
		t.Fatalf("proxy = %s", got.Tools.Proxy.HTTP)
	}
	if readText(t, filepath.Join(got.RemoteDir(), "identity")) != "dest-remote" {
		t.Fatal("omit-secrets restore replaced remote credentials")
	}
}

func TestProjectBackupAddsAndReplaceKeepsTheExternalDirectory(t *testing.T) {
	src := seed(t)
	out := filepath.Join(t.TempDir(), "project.zwai-backup")
	sqlDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := Create(context.Background(), Options{DataDir: src.dir, DB: sqlDB, Kind: KindProject, ProjectID: src.project.ID, Output: out})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Kind != KindProject || manifest.IncludesSecrets || manifest.Counts.Projects != 1 || manifest.Counts.Threads != 1 {
		t.Fatalf("manifest %+v", manifest)
	}
	if len(manifest.ExternalWorkdirs) != 1 || manifest.ExternalWorkdirs[0].Path != src.external {
		t.Fatalf("external %+v", manifest.ExternalWorkdirs)
	}
	for _, absent := range []string{"memory-beta", "shared-skill", "sk-backup-secret-value", "source-remote", "standalone-body", "engine.lock", "outside-workdir.txt", "config.yaml"} {
		if archiveHas(t, out, absent) {
			t.Fatalf("project archive contains %s", absent)
		}
	}
	if !archiveHas(t, out, "memory-alpha") || !archiveHas(t, out, "installed-skill") || !archiveHas(t, out, "image-bytes") {
		t.Fatal("project archive dropped the project's files")
	}

	destDir := t.TempDir()
	destCfg, err := config.Load(destDir)
	if err != nil {
		t.Fatal(err)
	}
	destSt, err := store.Open(destCfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer destSt.Close()
	beta := &store.Project{Name: "dest-beta", MemoryEnabled: true}
	if err := destSt.CreateProject(beta); err != nil {
		t.Fatal(err)
	}
	writeText(t, filepath.Join(destCfg.ProjectMemoryDir(beta.ID), "MEMORY.md"), "dest-beta-memory")
	writeText(t, filepath.Join(destCfg.LibraryDir(), "skills", "shared", "SKILL.md"), "dest-shared")
	destDB, err := destSt.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := Preview(context.Background(), destDir, destDB, out)
	if err != nil || preview.Action != ActionAddProject || len(preview.Conflicts) != 0 {
		t.Fatalf("preview %+v err=%v", preview, err)
	}
	if _, err := Restore(context.Background(), RestoreOptions{DataDir: destDir, DB: destDB, Path: out, Confirm: true}); err != nil {
		t.Fatal(err)
	}
	if readText(t, filepath.Join(destCfg.ProjectMemoryDir(src.project.ID), "MEMORY.md")) != "memory-alpha" {
		t.Fatal("added project memory missing")
	}
	if readText(t, filepath.Join(destCfg.ProjectMemoryDir(beta.ID), "MEMORY.md")) != "dest-beta-memory" {
		t.Fatal("adding a project changed the other project")
	}
	if readText(t, filepath.Join(destCfg.LibraryDir(), "skills", "shared", "SKILL.md")) != "dest-shared" {
		t.Fatal("project restore changed the shared library")
	}
	if readText(t, filepath.Join(src.external, "outside-workdir.txt")) != "outside-bytes" {
		t.Fatal("add deleted the external directory")
	}
	got, err := destSt.GetProject(src.project.ID)
	if err != nil || got.Workdir != src.external {
		t.Fatalf("workdir %+v err=%v", got, err)
	}
	var standalone int64
	if err := destSt.DB().Model(&store.Thread{}).Where("id = ?", src.standalone.ID).Count(&standalone).Error; err != nil || standalone != 0 {
		t.Fatalf("standalone copied into a project backup: %d %v", standalone, err)
	}

	writeText(t, filepath.Join(destCfg.ProjectMemoryDir(src.project.ID), "MEMORY.md"), "local-edit")
	replacePreview, err := Preview(context.Background(), destDir, destDB, out)
	if err != nil || replacePreview.Action != ActionReplaceProject {
		t.Fatalf("replace preview %+v err=%v", replacePreview, err)
	}
	if _, err := Restore(context.Background(), RestoreOptions{DataDir: destDir, DB: destDB, Path: out, Confirm: true}); !errors.Is(err, ErrConflict) {
		t.Fatalf("add over an existing project: %v", err)
	}
	if readText(t, filepath.Join(destCfg.ProjectMemoryDir(src.project.ID), "MEMORY.md")) != "local-edit" {
		t.Fatal("refused add changed the project")
	}
	if _, err := Restore(context.Background(), RestoreOptions{DataDir: destDir, DB: destDB, Path: out, Confirm: true, ReplaceProject: true}); err != nil {
		t.Fatal(err)
	}
	if readText(t, filepath.Join(destCfg.ProjectMemoryDir(src.project.ID), "MEMORY.md")) != "memory-alpha" {
		t.Fatal("replace did not restore project memory")
	}
	if readText(t, filepath.Join(src.external, "outside-workdir.txt")) != "outside-bytes" {
		t.Fatal("replace deleted the external directory")
	}
	if readText(t, filepath.Join(destCfg.ProjectMemoryDir(beta.ID), "MEMORY.md")) != "dest-beta-memory" {
		t.Fatal("replace changed the other project")
	}
	if gen := readText(t, filepath.Join(destDir, generationFile)); gen != "2" {
		t.Fatalf("generation = %s, want 2 after add and replace", gen)
	}
}

func TestProjectRestoreRefusesAForeignIdentity(t *testing.T) {
	src := seed(t)
	out := filepath.Join(t.TempDir(), "project.zwai-backup")
	sqlDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Create(context.Background(), Options{DataDir: src.dir, DB: sqlDB, Kind: KindProject, ProjectID: src.project.ID, Output: out}); err != nil {
		t.Fatal(err)
	}
	destDir := t.TempDir()
	destCfg, err := config.Load(destDir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(destCfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	other := &store.Project{Name: "owner", MemoryEnabled: true}
	if err := st.CreateProject(other); err != nil {
		t.Fatal(err)
	}
	stolen := &store.Thread{ID: src.thread.ID, Title: "already-here", ProjectID: other.ID}
	if err := st.CreateThread(stolen); err != nil {
		t.Fatal(err)
	}
	writeText(t, filepath.Join(destCfg.ProjectMemoryDir(other.ID), "MEMORY.md"), "owner-memory")
	db, err := st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	preview, err := Preview(context.Background(), destDir, db, out)
	if err != nil || preview.Action != ActionBlocked || len(preview.Conflicts) == 0 {
		t.Fatalf("preview %+v err=%v", preview, err)
	}
	if _, err := Restore(context.Background(), RestoreOptions{DataDir: destDir, DB: db, Path: out, Confirm: true, ReplaceProject: true}); !errors.Is(err, ErrConflict) {
		t.Fatalf("foreign identity: %v", err)
	}
	got, err := st.GetThread(src.thread.ID)
	if err != nil || got.Title != "already-here" || got.ProjectID != other.ID {
		t.Fatalf("thread changed %+v err=%v", got, err)
	}
	if readText(t, filepath.Join(destCfg.ProjectMemoryDir(other.ID), "MEMORY.md")) != "owner-memory" {
		t.Fatal("refused restore changed memory")
	}
	if _, err := os.Stat(filepath.Join(destDir, generationFile)); !os.IsNotExist(err) {
		t.Fatal("refused restore advanced the generation")
	}
}

func TestRestoreFailureRollsBackTheDatabaseAndMemory(t *testing.T) {
	src := seed(t)
	out := filepath.Join(t.TempDir(), "full.zwai-backup")
	sqlDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Create(context.Background(), Options{DataDir: src.dir, DB: sqlDB, Kind: KindFull, Output: out}); err != nil {
		t.Fatal(err)
	}
	writeText(t, filepath.Join(src.cfg.ProjectMemoryDir(src.project.ID), "MEMORY.md"), "edited-memory")
	if err := src.st.DB().Model(&store.Message{}).Where("content = ?", "message-body").Update("content", "edited-message").Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { beforeFileSwap = nil })
	beforeFileSwap = func() error { return errors.New("disk full") }
	_, err = Restore(context.Background(), RestoreOptions{DataDir: src.dir, DB: sqlDB, Path: out, Confirm: true, Replace: true})
	if err == nil {
		t.Fatal("restore succeeded despite the failure")
	}
	if readText(t, filepath.Join(src.cfg.ProjectMemoryDir(src.project.ID), "MEMORY.md")) != "edited-memory" {
		t.Fatal("failed restore changed MEMORY.md")
	}
	var content string
	if err := src.st.DB().Model(&store.Message{}).Where("thread_id = ?", src.thread.ID).Select("content").Take(&content).Error; err != nil {
		t.Fatal(err)
	}
	if content != "edited-message" {
		t.Fatalf("content = %q after a failed restore", content)
	}
	if readText(t, filepath.Join(src.dir, generationFile)) != "5" {
		t.Fatal("failed restore changed the generation")
	}
	matches, _ := filepath.Glob(filepath.Join(src.dir, ".backup-*"))
	if len(matches) != 0 {
		t.Fatalf("snapshot left behind: %v", matches)
	}
}

func TestBackupRejectsUnsafeArchives(t *testing.T) {
	dir := t.TempDir()
	if _, err := Create(context.Background(), Options{DataDir: dir, Kind: KindFull, Output: filepath.Join(dir, "inside.zwai-backup")}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("output inside the data directory: %v", err)
	}

	traversal := filepath.Join(t.TempDir(), "traversal.zwai-backup")
	writeZip(t, traversal, func(zw *zip.Writer) {
		w, err := zw.Create("../evil.txt")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("x"))
	})
	if _, err := Preview(context.Background(), t.TempDir(), nil, traversal); !errors.Is(err, ErrInvalid) {
		t.Fatalf("traversal: %v", err)
	}

	link := filepath.Join(t.TempDir(), "link.zwai-backup")
	writeZip(t, link, func(zw *zip.Writer) {
		h := &zip.FileHeader{Name: "files/link", Method: zip.Store}
		h.SetMode(os.ModeSymlink | 0o777)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write([]byte("/tmp"))
	})
	if _, err := Preview(context.Background(), t.TempDir(), nil, link); !errors.Is(err, ErrInvalid) {
		t.Fatalf("symlink entry: %v", err)
	}

	prev := zipBombMinUncompressed
	zipBombMinUncompressed = 64
	t.Cleanup(func() { zipBombMinUncompressed = prev })
	bomb := filepath.Join(t.TempDir(), "bomb.zwai-backup")
	writeZip(t, bomb, func(zw *zip.Writer) {
		h := &zip.FileHeader{Name: databaseName, Method: zip.Deflate}
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(bytes.Repeat([]byte{0}, 8192))
	})
	if _, err := Preview(context.Background(), t.TempDir(), nil, bomb); !errors.Is(err, ErrInvalid) {
		t.Fatalf("zip bomb: %v", err)
	}

}

func TestWorkspaceSymlinkDoesNotFailTheBackup(t *testing.T) {
	src := seed(t)
	lab := filepath.Join(src.cfg.WorkspaceDir(src.standalone.ID), "lab")
	writeText(t, filepath.Join(lab, "notes.txt"), "lab-notes")
	outside := filepath.Join(t.TempDir(), "python")
	writeText(t, outside, "interpreter-bytes")
	pythonLink := filepath.Join(lab, "bin", "python")
	if err := os.MkdirAll(filepath.Dir(pythonLink), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, pythonLink); err != nil {
		t.Fatal(err)
	}
	secretDir := t.TempDir()
	writeText(t, filepath.Join(secretDir, "secret.txt"), "secret-bytes")
	dirLink := filepath.Join(lab, "outside")
	if err := os.Symlink(secretDir, dirLink); err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(lab, "broken")
	if err := os.Symlink(filepath.Join(t.TempDir(), "missing"), broken); err != nil {
		t.Fatal(err)
	}
	sqlDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "links.zwai-backup")
	manifest, err := Create(context.Background(), Options{DataDir: src.dir, DB: sqlDB, Kind: KindFull, Output: out})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.SkippedSymlinks != 3 {
		t.Fatalf("skipped symlinks = %d, want 3", manifest.SkippedSymlinks)
	}
	if !archiveHas(t, out, "lab-notes") || !archiveHas(t, out, `"skipped_symlinks": 3`) {
		t.Fatal("archive dropped the regular file or the skip count")
	}
	for _, needle := range []string{"interpreter-bytes", "secret-bytes", "bin/python", "lab/outside", "lab/broken"} {
		if archiveHas(t, out, needle) {
			t.Fatalf("archive contains %s", needle)
		}
	}
	if target, err := os.Readlink(pythonLink); err != nil || target != outside {
		t.Fatalf("backup changed the link: %v %q", err, target)
	}
	if readText(t, outside) != "interpreter-bytes" {
		t.Fatal("backup changed the link target")
	}

	destDir := t.TempDir()
	if _, err := config.Load(destDir); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), RestoreOptions{DataDir: destDir, Path: out, Confirm: true, Replace: true}); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(destDir, "workspaces", src.standalone.ID, "lab")
	if readText(t, filepath.Join(restored, "notes.txt")) != "lab-notes" {
		t.Fatal("restore dropped the regular file")
	}
	for _, name := range []string{"bin/python", "outside", "broken"} {
		if _, err := os.Lstat(filepath.Join(restored, name)); !os.IsNotExist(err) {
			t.Fatalf("restore created %s: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(destDir, "secret.txt")); !os.IsNotExist(err) {
		t.Fatal("restore wrote the link target")
	}
}

func writeZip(t *testing.T, path string, fill func(*zip.Writer)) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	fill(zw)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestFullBackupLeavesOutTheNamedConversation(t *testing.T) {
	src := seed(t)
	out := filepath.Join(t.TempDir(), "omit.zwai-backup")
	sqlDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := Create(context.Background(), Options{
		DataDir: src.dir, DB: sqlDB, Kind: KindFull, Output: out,
		OmitThreads: []OmittedThread{{ID: src.thread.ID, Title: src.thread.Title, Reason: ReasonRunning}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Counts.Threads != 1 || manifest.Counts.Projects != 2 || len(manifest.OmittedThreads) != 1 || manifest.OmittedThreads[0].ID != src.thread.ID {
		t.Fatalf("manifest %+v", manifest)
	}
	if archiveHas(t, out, "workspace-out") || archiveHas(t, out, "plan-body") || archiveHas(t, out, "image-bytes") {
		t.Fatal("omitted conversation files were copied")
	}
	if !archiveHas(t, out, "standalone-body") || !archiveHas(t, out, "memory-alpha") {
		t.Fatal("backup dropped data that was not part of the running conversation")
	}
	if _, err := src.st.GetThread(src.thread.ID); err != nil {
		t.Fatal(err)
	}
	if readText(t, filepath.Join(src.cfg.WorkspaceDir(src.thread.ID), "out.txt")) != "workspace-out" {
		t.Fatal("creating the backup changed the live workspace")
	}
	destDir := t.TempDir()
	if _, err := config.Load(destDir); err != nil {
		t.Fatal(err)
	}
	if _, err := Restore(context.Background(), RestoreOptions{DataDir: destDir, Path: out, Confirm: true, Replace: true}); err != nil {
		t.Fatal(err)
	}
	dest, err := store.Open(filepath.Join(destDir, "zwai.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dest.Close() })
	if _, err := dest.GetThread(src.thread.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("omitted thread restored: %v", err)
	}
	if _, err := dest.GetThread(src.standalone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "workspaces", src.thread.ID, "out.txt")); !os.IsNotExist(err) {
		t.Fatalf("omitted workspace restored: %v", err)
	}
}

func TestProjectBackupLeavesOutOnlyThatConversation(t *testing.T) {
	src := seed(t)
	out := filepath.Join(t.TempDir(), "project.zwai-backup")
	sqlDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := Create(context.Background(), Options{
		DataDir: src.dir, DB: sqlDB, Kind: KindProject, ProjectID: src.project.ID, Output: out,
		OmitThreads: []OmittedThread{{ID: src.thread.ID, Title: src.thread.Title, Reason: ReasonRunning}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Counts.Projects != 1 || manifest.Counts.Threads != 0 || len(manifest.OmittedThreads) != 1 {
		t.Fatalf("manifest %+v", manifest)
	}
	if archiveHas(t, out, "workspace-out") || !archiveHas(t, out, "memory-alpha") {
		t.Fatal("project backup kept the running conversation or dropped the project")
	}
	if _, err := src.st.GetThread(src.thread.ID); err != nil {
		t.Fatal(err)
	}
}

func TestOmitThreadsRejectsABadID(t *testing.T) {
	if err := omitThreads(context.Background(), nil, "snap", nil); err != nil {
		t.Fatal(err)
	}
	if err := omitThreads(context.Background(), nil, "snap", []string{"../x"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("id err = %v", err)
	}
	if err := omitThreads(context.Background(), nil, "not a schema", []string{"th_ok"}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("schema err = %v", err)
	}
}

func TestCreateTellsTheCallerHowFarTheBackupHasGot(t *testing.T) {
	// A backup that only returns when the zip is finished looks frozen.
	// The page needs the database copy, the file count, and the bytes
	// written, including a tick while a large file is still being copied.
	src := seed(t)
	writeText(t, filepath.Join(src.cfg.WorkspaceDir(src.thread.ID), "large.bin"), strings.Repeat("x", 300<<10))
	sqlDB, err := src.st.DB().DB()
	if err != nil {
		t.Fatal(err)
	}
	reportProgress(nil, Progress{})
	if got := WithReporter(nil, nil); got == nil {
		t.Fatal("nil reporter dropped the context")
	}
	var got []Progress
	ctx := WithReporter(context.Background(), func(p Progress) { got = append(got, p) })
	out := filepath.Join(t.TempDir(), "progress.zwai-backup")
	if _, err := Create(ctx, Options{DataDir: src.dir, DB: sqlDB, Kind: KindFull, Output: out}); err != nil {
		t.Fatal(err)
	}
	var sawSnapshot, sawFiles, sawArchive, sawPartial bool
	var files int64
	var archiveDone, archiveTotal int64
	for _, p := range got {
		switch p.Phase {
		case PhaseSnapshot:
			if sawFiles || sawArchive {
				t.Fatalf("snapshot after a later stage: %+v", got)
			}
			sawSnapshot = true
		case PhaseFiles:
			if !sawSnapshot || sawArchive {
				t.Fatalf("files out of order: %+v", got)
			}
			if p.Done < files {
				t.Fatalf("file count went backwards: %+v", got)
			}
			files = p.Done
			sawFiles = true
		case PhaseArchive:
			if !sawSnapshot {
				t.Fatalf("archive before snapshot: %+v", got)
			}
			if p.Total <= 0 || p.Done < archiveDone || p.Done > p.Total {
				t.Fatalf("archive progress: %+v", p)
			}
			if p.Done > 0 && p.Done < p.Total {
				sawPartial = true
			}
			archiveDone, archiveTotal = p.Done, p.Total
			sawArchive = true
		default:
			t.Fatalf("phase %q", p.Phase)
		}
	}
	if !sawSnapshot || !sawFiles || !sawArchive || !sawPartial || files == 0 || archiveDone != archiveTotal {
		t.Fatalf("snapshot=%v files=%d archive=%d/%d partial=%v reports=%d", sawSnapshot, files, archiveDone, archiveTotal, sawPartial, len(got))
	}
}
