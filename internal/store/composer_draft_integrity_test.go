package store

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"gorm.io/gorm"
)

func TestComposerDraftCannotAcknowledgeMissingOrChangedOriginalBytes(t *testing.T) {
	for _, damage := range []string{"missing", "truncated", "extended"} {
		t.Run(damage, func(t *testing.T) {
			s := open(t)
			ctx := context.Background()
			original := ComposerDraft{Key: "new:home", Text: "original", Files: []DraftFile{
				{ID: "old", Name: "original.bin", Kind: "file", Size: 3},
				{ID: "other", Name: "other.bin", Kind: "file", Size: 4},
			}}
			original, err := s.SaveComposerDraft(ctx, original, map[string][]byte{"old": []byte("old"), "other": []byte("keep")})
			if err != nil {
				t.Fatal(err)
			}
			query := s.db.Model(&ComposerDraftBlob{}).Where("draft_key = ? AND file_id = ?", original.Key, "old")
			if damage == "missing" {
				err = query.Delete(&ComposerDraftBlob{}).Error
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
			changed := original
			changed.Text = "new text"
			changed.Files = append([]DraftFile{{ID: "new", Name: "new.bin", Kind: "file", Size: 3}}, original.Files...)
			if _, err := s.SaveComposerDraft(ctx, changed, map[string][]byte{"new": []byte("new")}); !errors.Is(err, ErrInvalidDraft) {
				t.Fatalf("unavailable original bytes were acknowledged: %v", err)
			}
			got, err := s.GetComposerDraft(ctx, original.Key)
			if err != nil || got.Revision != original.Revision || got.Text != original.Text || len(got.Files) != len(original.Files) {
				t.Fatalf("failed integrity check changed the snapshot: %+v %v", got, err)
			}
			if _, err := s.GetComposerDraftFile(ctx, original.Key, "new"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("failed integrity check left newly uploaded bytes: %v", err)
			}
			if data, err := s.GetComposerDraftFile(ctx, original.Key, "other"); err != nil || !bytes.Equal(data, []byte("keep")) {
				t.Fatalf("failed integrity check changed another attachment: %q %v", data, err)
			}
		})
	}
}

func TestComposerDraftReferenceReadFailureCannotAcknowledgeASave(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	original, err := s.SaveComposerDraft(ctx, ComposerDraft{Key: "new:home", Text: "original", Files: []DraftFile{{ID: "old", Name: "original.bin", Kind: "file", Size: 3}}}, map[string][]byte{"old": []byte("old")})
	if err != nil {
		t.Fatal(err)
	}
	failure := errors.New("attachment metadata query unavailable")
	if err := s.db.Callback().Query().Before("gorm:query").Register("draft_reference_read_failure", func(tx *gorm.DB) {
		if tx.Statement.Table == "composer_draft_blobs" {
			tx.AddError(failure)
		}
	}); err != nil {
		t.Fatal(err)
	}
	changed := original
	changed.Text = "new text"
	_, saveErr := s.SaveComposerDraft(ctx, changed, nil)
	if err := s.db.Callback().Query().Remove("draft_reference_read_failure"); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(saveErr, failure) {
		t.Fatalf("attachment read failure was acknowledged or lost its category: %v", saveErr)
	}
	got, err := s.GetComposerDraft(ctx, original.Key)
	if err != nil || got.Revision != original.Revision || got.Text != original.Text {
		t.Fatalf("failed reference read changed the snapshot: %+v %v", got, err)
	}
}

func TestComposerDraftRetainsZeroByteFilesAcrossTextEdits(t *testing.T) {
	s := open(t)
	ctx := context.Background()
	draft, err := s.SaveComposerDraft(ctx, ComposerDraft{Key: "new:home", Files: []DraftFile{{ID: "empty", Name: "empty.bin", Kind: "file", Size: 0}}}, map[string][]byte{"empty": nil})
	if err != nil {
		t.Fatal(err)
	}
	draft.Text = "later edit"
	saved, err := s.SaveComposerDraft(ctx, draft, nil)
	if err != nil || saved.Revision != draft.Revision+1 || len(saved.Files) != 1 {
		t.Fatalf("an empty original file was mistaken for missing bytes: %+v %v", saved, err)
	}
	if data, err := s.GetComposerDraftFile(ctx, draft.Key, "empty"); err != nil || len(data) != 0 {
		t.Fatalf("empty file identity was lost: %q %v", data, err)
	}
}
