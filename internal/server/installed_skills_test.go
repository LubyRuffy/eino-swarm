package server_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestInstalledSkillPackageHTTPContract(t *testing.T) {
	h := newHarness(t)
	project := h.newProject(map[string]any{"name": "Skill consumer", "memory_enabled": false})
	other := h.newProject(map[string]any{"name": "Unrelated project"})
	id := project["id"].(string)
	path := "/api/projects/" + id + "/installed-skills"
	otherPath := "/api/projects/" + other["id"].(string) + "/installed-skills"

	post := func(body []byte, contentType string, want int) map[string]any {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, h.srv.URL+path, bytes.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", contentType)
		response, err := h.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var got map[string]any
		if err := json.NewDecoder(response.Body).Decode(&got); err != nil || response.StatusCode != want {
			t.Fatalf("install status=%d want=%d response=%v decode=%v", response.StatusCode, want, got, err)
		}
		return got
	}

	archive := portableSkillArchive(t)
	post(archive, "application/json", http.StatusUnsupportedMediaType)
	post([]byte("invalid archive"), "application/zip", http.StatusBadRequest)
	post(make([]byte, (16<<20)+1), "application/zip", http.StatusRequestEntityTooLarge)

	installed := post(archive, "application/zip", http.StatusCreated)
	if installed["name"] != "verify-input" || installed["body"] != nil {
		t.Fatalf("install receipt=%v", installed)
	}
	post(archive, "application/zip", http.StatusConflict)
	got := h.json(http.MethodGet, path, nil, http.StatusOK)["skills"].([]any)
	if len(got) != 1 || got[0].(map[string]any)["name"] != "verify-input" {
		t.Fatalf("installed skills=%v", got)
	}
	detail := h.json(http.MethodGet, path+"/verify-input", nil, http.StatusOK)
	if detail["base_dir"] != nil || detail["name"] != "verify-input" || !strings.Contains(detail["body"].(string), "Read the supplied guide.") {
		t.Fatalf("skill detail=%v", detail)
	}
	if got := h.json(http.MethodGet, otherPath, nil, http.StatusOK)["skills"].([]any); len(got) != 0 {
		t.Fatalf("another project could list skills=%v", got)
	}
	h.json(http.MethodGet, otherPath+"/verify-input", nil, http.StatusNotFound)
	h.json(http.MethodDelete, otherPath+"/verify-input", nil, http.StatusNotFound)
	h.json(http.MethodGet, "/api/projects/"+id+"/skills/verify-input", nil, http.StatusNotFound)

	learned, err := h.app.Engine.ProjectMemory(id).WriteSkill("verify-input", "Use the project's learned procedure.", "Learned steps.")
	if err != nil {
		t.Fatal(err)
	}
	if learned.Body == detail["body"] {
		t.Fatal("the external and learned skills were merged")
	}
	h.json(http.MethodDelete, path+"/verify-input", nil, http.StatusNoContent)
	h.json(http.MethodGet, path+"/verify-input", nil, http.StatusNotFound)
	if got := h.readSkill(id, "verify-input"); got["body"] != "Learned steps." {
		t.Fatalf("removing the package changed the learned skill: %v", got)
	}
	h.json(http.MethodGet, "/api/projects/pj_missing/installed-skills", nil, http.StatusNotFound)
}

func portableSkillArchive(t *testing.T) []byte {
	t.Helper()
	var archive bytes.Buffer
	zw := zip.NewWriter(&archive)
	for name, content := range map[string]string{
		"verify-input/SKILL.md":            "---\nname: verify-input\ndescription: Review input for accuracy.\n---\n\nRead the supplied guide.\n",
		"verify-input/references/guide.md": "A private package reference.",
	} {
		file, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return archive.Bytes()
}

type gatedSkillBody struct {
	*bytes.Reader
	started chan struct{}
	release chan struct{}
}

func (b *gatedSkillBody) Read(p []byte) (int, error) {
	if b.started != nil {
		close(b.started)
		b.started = nil
		<-b.release
	}
	return b.Reader.Read(p)
}

func TestSkillUploadFinishingAfterProjectDeletionReturnsNotFound(t *testing.T) {
	h := newHarness(t)
	p := h.newProject(map[string]any{"name": "Project being deleted"})
	id := p["id"].(string)
	body := &gatedSkillBody{
		Reader:  bytes.NewReader(portableSkillArchive(t)),
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
	started := body.started
	req := httptest.NewRequest(http.MethodPost, "/api/projects/"+id+"/installed-skills", body)
	req.Header.Set("Content-Type", "application/zip")
	reply := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.app.Server.Handler().ServeHTTP(reply, req)
	}()
	released := false
	defer func() {
		if !released {
			close(body.release)
		}
		<-done
	}()
	select {
	case <-started:
	case <-done:
		t.Fatalf("upload returned before reading the ZIP: %d %s", reply.Code, reply.Body)
	case <-time.After(5 * time.Second):
		t.Fatal("upload never started reading the ZIP")
	}

	h.json(http.MethodDelete, "/api/projects/"+id, nil, http.StatusNoContent)
	close(body.release)
	released = true
	<-done
	var got map[string]any
	if err := json.Unmarshal(reply.Body.Bytes(), &got); err != nil || reply.Code != http.StatusNotFound ||
		got["code"] != "not_found" {
		t.Fatalf("stale upload status=%d response=%s decode=%v", reply.Code, reply.Body, err)
	}
	projectDir := h.app.Engine.Config().ProjectDir(id)
	if strings.Contains(reply.Body.String(), projectDir) {
		t.Fatalf("stale upload exposed its internal path: %s", reply.Body)
	}
	if _, err := os.Lstat(projectDir); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("stale upload recreated the project directory: %v", err)
	}
}
