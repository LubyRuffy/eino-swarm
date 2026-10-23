package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

var (
	ErrDraftConflict = errors.New("store: the draft changed in another window")
	ErrInvalidDraft  = errors.New("store: invalid composer draft")
)

const (
	MaxDraftBytes     = 256 << 20
	MaxDraftFiles     = 32
	MaxDraftTextBytes = 1 << 20
)

// DraftQuote preserves the quoted source separately from the human's text.
type DraftQuote struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// DraftFile identifies immutable original bytes. Object URLs are never stored.
type DraftFile struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	MIME         string `json:"mime"`
	Kind         string `json:"kind"`
	Size         int64  `json:"size"`
	LastModified int64  `json:"last_modified"`
}

// ComposerDraft is one versioned snapshot, independent of the HTTP port.
type ComposerDraft struct {
	Key       string       `gorm:"column:draft_key;primaryKey;size:128" json:"key"`
	Revision  int64        `json:"revision"`
	Text      string       `json:"text"`
	Quotes    []DraftQuote `gorm:"serializer:json" json:"quotes"`
	Files     []DraftFile  `gorm:"serializer:json" json:"files"`
	State     string       `json:"state"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// ComposerDraftBlob is scoped to its draft so a foreign file ID grants no access.
type ComposerDraftBlob struct {
	DraftKey string `gorm:"primaryKey;size:128"`
	FileID   string `gorm:"primaryKey;size:128"`
	Data     []byte
}

func emptyComposerDraft(key string) ComposerDraft {
	return ComposerDraft{Key: key, State: "editing", Quotes: []DraftQuote{}, Files: []DraftFile{}}
}

func (s *Store) GetComposerDraft(ctx context.Context, key string) (ComposerDraft, error) {
	d := emptyComposerDraft(key)
	err := s.db.WithContext(ctx).First(&d, "draft_key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return emptyComposerDraft(key), nil
	}
	if err != nil {
		return ComposerDraft{}, fmt.Errorf("store: read composer draft: %w", err)
	}
	return d, nil
}

// SaveComposerDraft commits the manifest and new bytes together. Later text edits
// only reference existing bytes; a failed save cannot leave a partial snapshot.
func (s *Store) SaveComposerDraft(ctx context.Context, in ComposerDraft, uploads map[string][]byte) (ComposerDraft, error) {
	if in.State == "" {
		in.State = "editing"
	}
	if err := validateComposerDraft(in, uploads); err != nil {
		return ComposerDraft{}, err
	}
	if in.Files == nil {
		in.Files = []DraftFile{}
	}
	if in.Quotes == nil {
		in.Quotes = []DraftQuote{}
	}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := composerDraftOwnerExists(tx, in.Key); err != nil {
			return err
		}
		old := emptyComposerDraft(in.Key)
		err := tx.First(&old, "draft_key = ?", in.Key).Error
		exists := err == nil
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if old.Revision != in.Revision {
			return ErrDraftConflict
		}
		oldFiles := make(map[string]DraftFile, len(old.Files))
		for _, file := range old.Files {
			oldFiles[file.ID] = file
		}
		ids := make([]string, 0, len(in.Files))
		for _, file := range in.Files {
			ids = append(ids, file.ID)
			data, added := uploads[file.ID]
			prior, known := oldFiles[file.ID]
			if known {
				if file != prior || added {
					return fmt.Errorf("%w: an existing attachment is immutable", ErrInvalidDraft)
				}
				// A manifest is not proof that its original bytes still exist.
				// Count matching lengths without loading a potentially large blob.
				var persisted int64
				if err := tx.Model(&ComposerDraftBlob{}).Where("draft_key = ? AND file_id = ? AND coalesce(length(data), 0) = ?", in.Key, file.ID, file.Size).Count(&persisted).Error; err != nil {
					return fmt.Errorf("verify existing draft attachment: %w", err)
				}
				if persisted != 1 {
					return fmt.Errorf("%w: saved attachment bytes are missing or changed", ErrInvalidDraft)
				}
				continue
			}
			if !added {
				return fmt.Errorf("%w: missing attachment bytes", ErrInvalidDraft)
			}
			if err := tx.Create(&ComposerDraftBlob{DraftKey: in.Key, FileID: file.ID, Data: data}).Error; err != nil {
				return err
			}
		}
		in.Revision++
		in.UpdatedAt = time.Now().UTC()
		if !exists {
			if err := tx.Create(&in).Error; err != nil {
				return err
			}
		} else {
			result := tx.Model(&ComposerDraft{}).Where("draft_key = ? AND revision = ?", in.Key, old.Revision).Select("revision", "text", "quotes", "files", "state", "updated_at").Updates(&in)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return ErrDraftConflict
			}
		}
		q := tx.Where("draft_key = ?", in.Key)
		if len(ids) != 0 {
			q = q.Where("file_id NOT IN ?", ids)
		}
		return q.Delete(&ComposerDraftBlob{}).Error
	})
	if err != nil {
		return ComposerDraft{}, fmt.Errorf("store: save composer draft: %w", err)
	}
	return in, nil
}

func composerDraftOwnerExists(tx *gorm.DB, key string) error {
	if key == "new:home" {
		return nil
	}
	var model any
	var id string
	switch {
	case strings.HasPrefix(key, "thread:"):
		model, id = &Thread{}, strings.TrimPrefix(key, "thread:")
	case strings.HasPrefix(key, "new:"):
		model, id = &Project{}, strings.TrimPrefix(key, "new:")
	default:
		return fmt.Errorf("%w: invalid scope", ErrInvalidDraft)
	}
	var count int64
	if err := tx.Model(model).Where("id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrNotFound
	}
	return nil
}

// ClearComposerDraft keeps a versioned tombstone: an old page cannot recreate
// the sent snapshot by finishing a delayed save after the acknowledgement.
func (s *Store) ClearComposerDraft(ctx context.Context, key string, revision int64) (ComposerDraft, error) {
	d := emptyComposerDraft(key)
	d.Revision = revision
	return s.SaveComposerDraft(ctx, d, nil)
}

func (s *Store) GetComposerDraftFile(ctx context.Context, key, id string) ([]byte, error) {
	var blob ComposerDraftBlob
	err := s.db.WithContext(ctx).First(&blob, "draft_key = ? AND file_id = ?", key, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("store: read draft attachment: %w", err)
	}
	return blob.Data, nil
}

func validateComposerDraft(d ComposerDraft, uploads map[string][]byte) error {
	invalid := func(reason string) error { return fmt.Errorf("%w: %s", ErrInvalidDraft, reason) }
	if d.Key == "" || len(d.Key) > 128 || strings.ContainsAny(d.Key, "/\\\x00") || d.Revision < 0 {
		return invalid("invalid key or revision")
	}
	if d.State != "editing" && d.State != "sending" && d.State != "uncertain" {
		return invalid("invalid send state")
	}
	if len(d.Text) > MaxDraftTextBytes || len(d.Quotes) > 32 || len(d.Files) > MaxDraftFiles {
		return invalid("draft exceeds the text or attachment limit")
	}
	totalText := len(d.Text)
	for _, quote := range d.Quotes {
		totalText += len(quote.Text)
		if quote.ID == "" || len(quote.ID) > 128 {
			return invalid("invalid quote identity")
		}
	}
	if totalText > MaxDraftTextBytes {
		return invalid("draft text and quotes exceed 1 MiB")
	}
	seen := make(map[string]bool, len(d.Files))
	var total int64
	images := 0
	for _, file := range d.Files {
		if !validDraftFileID(file.ID) || seen[file.ID] || file.Name == "" || len(file.Name) > 4096 || strings.ContainsRune(file.Name, 0) || len(file.MIME) > 256 || file.LastModified < 0 {
			return invalid("invalid attachment metadata")
		}
		seen[file.ID] = true
		if file.Kind != "file" && file.Kind != "image" {
			return invalid("invalid attachment kind")
		}
		if file.Size < 0 || file.Size > MaxDraftBytes {
			return invalid("invalid attachment size")
		}
		total += file.Size
		if total > MaxDraftBytes {
			return invalid("draft attachments exceed 256 MiB")
		}
		if file.Kind == "image" {
			images++
			if file.Size > 8<<20 || images > 8 {
				return invalid("draft images exceed 8 images of 8 MiB")
			}
		}
		if data, ok := uploads[file.ID]; ok && int64(len(data)) != file.Size {
			return invalid("attachment size does not match its bytes")
		}
	}
	for id := range uploads {
		if !seen[id] {
			return invalid("unreferenced attachment bytes")
		}
	}
	return nil
}

func validDraftFileID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func deleteComposerDraftScopes(tx *gorm.DB, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	if err := tx.Where("draft_key IN ?", keys).Delete(&ComposerDraftBlob{}).Error; err != nil {
		return err
	}
	return tx.Where("draft_key IN ?", keys).Delete(&ComposerDraft{}).Error
}
