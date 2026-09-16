package server_test

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A 1×1 PNG. The HTTP tests only care that the pixels that went in are the
// pixels that come back, and that a renamed HTML file never does.
var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
	0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, 0x54,
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01,
	0x0d, 0x0a, 0x2d, 0xb4,
	0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func pngBody() map[string]any {
	return map[string]any{
		"name": "clip.png",
		"mime": "image/png",
		"data": base64.StdEncoding.EncodeToString(tinyPNG),
	}
}

func eventImages(t *testing.T, h *harness, threadID, kind string) []store.ImageRef {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		events, err := h.app.Store.ListEvents(threadID, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range events {
			if ev.Kind == kind && len(ev.Images) > 0 {
				return ev.Images
			}
		}
		if kind == "user_message" {
			msgs, err := h.app.Store.ListMessages(threadID)
			if err != nil {
				t.Fatal(err)
			}
			for _, m := range msgs {
				if m.Role == "user" && len(m.Images) > 0 && !strings.HasPrefix(m.Content, "[steer]") {
					return m.Images
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s event carried image refs", kind)
	return nil
}

func TestPastedImageIsVisionInputTheTranscriptCanFetch(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()

	got := h.json(http.MethodPost, "/api/threads/"+id+"/turns", map[string]any{
		"text":   "what is on this",
		"images": []any{pngBody()},
	}, http.StatusAccepted)
	if got["turn"] == nil {
		t.Fatal("a caption plus a paste still has to start a turn")
	}

	refs := eventImages(t, h, id, "user_message")
	if refs[0].Name != "clip.png" || refs[0].MIME != "image/png" || refs[0].ID == "" {
		t.Fatalf("the event must carry a handle, not the pixels: %+v", refs)
	}

	resp := h.do(http.MethodGet, "/api/threads/"+id+"/input-images/"+refs[0].ID, nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("thumbnail status %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type=%q, the <img> tag needs a real image type", ct)
	}
	if resp.Header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("nosniff belongs on every image we serve from this origin")
	}
	if !strings.Contains(resp.Header.Get("Content-Disposition"), "inline") {
		t.Fatalf("must be inline so the transcript can render it: %q",
			resp.Header.Get("Content-Disposition"))
	}
	if string(body) != string(tinyPNG) {
		t.Fatalf("the pixels that went in have to be the pixels that come back")
	}

	listing := h.json(http.MethodGet, "/api/threads/"+id+"/files", nil, http.StatusOK)
	for _, raw := range listing["files"].([]any) {
		path, _ := raw.(map[string]any)["path"].(string)
		if strings.Contains(strings.ToLower(path), "clip") {
			t.Fatalf("a pasted image is not a workspace file: %s", path)
		}
	}

	h.json(http.MethodPost, "/api/threads/"+id+"/interrupt", nil, http.StatusAccepted)
	h.waitTurnDone(id)
}

func TestATurnWithOnlyAnImageIsAccepted(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/turns", map[string]any{
		"images": []any{pngBody()},
	}, http.StatusAccepted)
	if refs := eventImages(t, h, id, "user_message"); len(refs) != 1 {
		t.Fatalf("image-only send must still record the paste: %+v", refs)
	}
	h.json(http.MethodPost, "/api/threads/"+id+"/interrupt", nil, http.StatusAccepted)
	h.waitTurnDone(id)
}

func TestANonImagePayloadIsRejectedOnTheTurnEndpoint(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	html := base64.StdEncoding.EncodeToString([]byte("<!doctype html><script></script>"))
	h.json(http.MethodPost, "/api/threads/"+id+"/turns", map[string]any{
		"images": []any{map[string]any{
			"name": "x.png",
			"mime": "image/png",
			"data": html,
		}},
	}, http.StatusBadRequest)
}

func TestInputImageRejectsAMissingOrTraversalId(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodGet, "/api/threads/"+id+"/input-images/img_deadbeef", nil, http.StatusNotFound)
	h.json(http.MethodGet, "/api/threads/"+id+"/input-images/not-an-id", nil, http.StatusNotFound)
	resp := h.do(http.MethodGet, "/api/threads/"+id+"/input-images/../passwd", nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("a traversal id must not serve pixels: %s", body)
	}
}

func TestSteerCarriesAPastedImage(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	h.json(http.MethodPost, "/api/threads/"+id+"/turns",
		map[string]any{"text": "look into this"}, http.StatusAccepted)

	var steered bool
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp := h.do(http.MethodPost, "/api/threads/"+id+"/steer", map[string]any{
			"text":   "look here",
			"images": []any{pngBody()},
		})
		raw, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusAccepted {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Fatal(err)
		}
		if body["steered"] == true {
			steered = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !steered {
		t.Fatal("never managed to land a steer while the turn was running")
	}
	refs := eventImages(t, h, id, "steer")
	resp := h.do(http.MethodGet, "/api/threads/"+id+"/input-images/"+refs[0].ID, nil)
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || string(body) != string(tinyPNG) {
		t.Fatalf("steered paste must be fetchable: status=%d", resp.StatusCode)
	}
	h.json(http.MethodPost, "/api/threads/"+id+"/interrupt", nil, http.StatusAccepted)
	h.waitTurnDone(id)
}
