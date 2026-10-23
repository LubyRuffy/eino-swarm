package server_test

import (
	"net/http"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestComposerDraftHTTPRejectsMetadataWhoseOriginalBytesAreUnavailable(t *testing.T) {
	for _, damage := range []string{"missing", "truncated", "extended"} {
		t.Run(damage, func(t *testing.T) {
			h := newHarness(t)
			draft := draftPut(t, h, "new:home", store.ComposerDraft{Text: "original", Files: []store.DraftFile{{ID: "old", Name: "original.bin", Kind: "file", Size: 3}}}, map[string][]byte{"old": []byte("old")}, http.StatusOK)
			query := h.app.Store.DB().Model(&store.ComposerDraftBlob{}).Where("draft_key = ? AND file_id = ?", draft.Key, "old")
			var err error
			if damage == "missing" {
				err = query.Delete(&store.ComposerDraftBlob{}).Error
			} else {
				data := []byte("x")
				if damage == "extended" {
					data = []byte("longer")
				}
				err = query.Update("data", data).Error
			}
			if err != nil {
				t.Fatal(err)
			}
			draft.Text = "later edit"
			draftPut(t, h, draft.Key, draft, nil, http.StatusBadRequest)
			got := h.json(http.MethodGet, "/api/composer/drafts/new:home", nil, http.StatusOK)["draft"].(map[string]any)
			if got["revision"] != float64(draft.Revision) || got["text"] != "original" {
				t.Fatalf("rejected snapshot changed its durable revision: %+v", got)
			}
		})
	}
}
