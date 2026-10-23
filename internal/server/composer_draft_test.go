package server_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func draftPut(t *testing.T, h *harness, key string, draft store.ComposerDraft, files map[string][]byte, want int) store.ComposerDraft {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	raw, err := json.Marshal(draft)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.WriteField("snapshot", string(raw)); err != nil {
		t.Fatal(err)
	}
	for id, data := range files {
		p, err := w.CreateFormFile("file."+id, "input")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := p.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPut, h.srv.URL+"/api/composer/drafts/"+url.PathEscape(key), &body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != want {
		t.Fatalf("draft save status=%d want=%d body=%s", resp.StatusCode, want, data)
	}
	if want != http.StatusOK {
		return store.ComposerDraft{}
	}
	var out struct {
		Draft store.ComposerDraft `json:"draft"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out.Draft
}

func TestComposerDraftHTTPPreservesBinaryAndDecodedImageAcrossOrigins(t *testing.T) {
	h := newHarness(t)
	key := "thread:" + h.newThread()
	var pixels bytes.Buffer
	if err := png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 3, 2))); err != nil {
		t.Fatal(err)
	}
	draft := store.ComposerDraft{Key: key, Text: "未发送草稿  ", State: "editing", Quotes: []store.DraftQuote{{ID: "q1", Text: "quoted evidence"}}, Files: []store.DraftFile{
		{ID: "file1", Name: "opaque.bin", MIME: "application/octet-stream", Kind: "file", Size: 3, LastModified: 42},
		{ID: "image1", Name: "screen.png", MIME: "image/png", Kind: "image", Size: int64(pixels.Len()), LastModified: 43},
	}}
	draft = draftPut(t, h, key, draft, map[string][]byte{"file1": {0, 255, 1}, "image1": pixels.Bytes()}, http.StatusOK)
	if draft.Revision != 1 || draft.Text != "未发送草稿  " {
		t.Fatalf("wrong saved snapshot: %+v", draft)
	}
	draft.Text = "edited without re-upload"
	draft = draftPut(t, h, key, draft, nil, http.StatusOK)
	if draft.Revision != 2 {
		t.Fatalf("revision=%d", draft.Revision)
	}
	for id, expected := range map[string][]byte{"file1": {0, 255, 1}, "image1": pixels.Bytes()} {
		resp := h.do(http.MethodGet, "/api/composer/drafts/"+url.PathEscape(key)+"/files/"+id, nil)
		got, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !bytes.Equal(got, expected) {
			t.Fatalf("changed attachment %s status=%d", id, resp.StatusCode)
		}
		if resp.Header.Get("X-Content-Type-Options") != "nosniff" || resp.Header.Get("Content-Disposition") == "" {
			t.Fatal("draft download can execute user content")
		}
		if id == "image1" {
			cfg, err := png.DecodeConfig(bytes.NewReader(got))
			if err != nil || cfg.Width != 3 || cfg.Height != 2 {
				t.Fatalf("image not decodable: %+v %v", cfg, err)
			}
		}
	}
	// A new HTTP origin uses the same durable authority, unlike browser storage.
	resp := h.json(http.MethodGet, "/api/composer/drafts/"+url.PathEscape(key), nil, http.StatusOK)
	if resp["draft"].(map[string]any)["text"] != draft.Text {
		t.Fatal("draft was tied to the old page")
	}
	h.json(http.MethodDelete, "/api/composer/drafts/"+url.PathEscape(key)+"?revision=1", nil, http.StatusConflict)
	draftPut(t, h, key, store.ComposerDraft{Text: "stale", Revision: 1}, nil, http.StatusConflict)
	h.json(http.MethodDelete, "/api/composer/drafts/"+url.PathEscape(key)+"?revision=2", nil, http.StatusOK)
	h.json(http.MethodGet, "/api/composer/drafts/"+url.PathEscape(key)+"/files/file1", nil, http.StatusNotFound)
}

func TestComposerDraftHTTPRejectsForeignScopeAndMalformedUploads(t *testing.T) {
	h := newHarness(t)
	for _, key := range []string{"other", "new:", "thread:missing", "new:missing"} {
		want := http.StatusBadRequest
		if key == "thread:missing" || key == "new:missing" {
			want = http.StatusNotFound
		}
		h.json(http.MethodGet, "/api/composer/drafts/"+url.PathEscape(key), nil, want)
	}
	h.json(http.MethodGet, "/api/composer/drafts/new:home", nil, http.StatusOK)
	h.json(http.MethodPut, "/api/composer/drafts/new:home", map[string]string{"text": "not multipart"}, http.StatusBadRequest)
	h.json(http.MethodDelete, "/api/composer/drafts/new:home?revision=wrong", nil, http.StatusBadRequest)
	h.json(http.MethodDelete, "/api/composer/drafts/new:home?revision=-1", nil, http.StatusBadRequest)
	draftPut(t, h, "new:home", store.ComposerDraft{Key: "new:other"}, nil, http.StatusBadRequest)
	draftPut(t, h, "new:home", store.ComposerDraft{Files: []store.DraftFile{{ID: "f", Name: "a", Kind: "file", Size: 4}}}, map[string][]byte{"f": []byte("123")}, http.StatusBadRequest)
	draftPut(t, h, "new:home", store.ComposerDraft{Files: []store.DraftFile{{ID: "f", Name: "a", MIME: "image/png", Kind: "image", Size: 4}}}, map[string][]byte{"f": []byte("HTML")}, http.StatusBadRequest)
	draftPut(t, h, "new:home", store.ComposerDraft{}, map[string][]byte{"extra": []byte("unreferenced")}, http.StatusBadRequest)
	req, _ := http.NewRequest(http.MethodDelete, h.srv.URL+"/api/composer/drafts/new:home?revision=0", nil)
	req.Header.Set("Origin", "https://foreign.invalid")
	resp, err := h.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Fatal("foreign origin can clear a draft")
	}
}

func TestComposerDraftHTTPDoesNotHideStorageLossOrInvalidLimits(t *testing.T) {
	h := newHarness(t)
	for _, key := range []string{"new:" + strings.Repeat("a", 65), "new:bad.id"} {
		h.json(http.MethodGet, "/api/composer/drafts/"+url.PathEscape(key), nil, http.StatusBadRequest)
	}
	for _, files := range [][]store.DraftFile{
		make([]store.DraftFile, 33),
		{{ID: "f", Name: "a", Kind: "file", Size: -1}},
		{{ID: "f", Name: "a", Kind: "file", Size: store.MaxDraftBytes + 1}},
		{{ID: "f", Name: "a", Kind: "file", Size: store.MaxDraftBytes}, {ID: "f2", Name: "b", Kind: "file", Size: 1}},
	} {
		draftPut(t, h, "new:home", store.ComposerDraft{Files: files}, nil, http.StatusBadRequest)
	}
	for _, snapshot := range []string{"{", strings.Repeat(" ", 2<<20+1)} {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if err := writer.WriteField("snapshot", snapshot); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req, _ := http.NewRequest(http.MethodPut, h.srv.URL+"/api/composer/drafts/new:home", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		resp, err := h.srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("invalid snapshot status=%d", resp.StatusCode)
		}
	}
	draft := store.ComposerDraft{Files: []store.DraftFile{{ID: "f", Name: "a", Kind: "file", Size: 1}}}
	draftPut(t, h, "new:home", draft, map[string][]byte{"f": []byte("x")}, http.StatusOK)
	if err := h.app.Store.DB().Where("draft_key = ?", "new:home").Delete(&store.ComposerDraftBlob{}).Error; err != nil {
		t.Fatal(err)
	}
	h.json(http.MethodGet, "/api/composer/drafts/new:home/files/f", nil, http.StatusNotFound)
	if err := h.app.Store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/composer/drafts/new:home", "/api/composer/drafts/new:home/files/f"} {
		h.json(http.MethodGet, path, nil, http.StatusBadRequest)
	}
	h.json(http.MethodDelete, "/api/composer/drafts/new:home?revision=1", nil, http.StatusBadRequest)
	draftPut(t, h, "new:home", store.ComposerDraft{Revision: 1}, nil, http.StatusBadRequest)
}

func TestComposerDraftTextLimitCountsOriginalUTF8RatherThanJSONEscapes(t *testing.T) {
	h := newHarness(t)
	text := strings.Repeat("\"", store.MaxDraftTextBytes)
	draft := draftPut(t, h, "new:home", store.ComposerDraft{Text: text}, nil, http.StatusOK)
	if draft.Text != text || draft.Revision != 1 {
		t.Fatal("valid text was changed by transport encoding")
	}
}
