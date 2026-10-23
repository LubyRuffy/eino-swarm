package server

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// Multipart uploads may spill to disk. A disappearing or truncated temporary
// file must fail the whole snapshot rather than acknowledge lost user bytes.
func TestComposerDraftUploadRejectsLostOrTruncatedTemporaryBytes(t *testing.T) {
	for _, removed := range []bool{true, false} {
		t.Run(map[bool]string{true: "removed", false: "truncated"}[removed], func(t *testing.T) {
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file.f", "binary")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := part.Write([]byte("original")); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest("PUT", "/", &body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			if err := req.ParseMultipartForm(1); err != nil {
				t.Fatal(err)
			}
			defer req.MultipartForm.RemoveAll()
			src, err := req.MultipartForm.File["file.f"][0].Open()
			if err != nil {
				t.Fatal(err)
			}
			path := src.(*os.File).Name()
			if err := src.Close(); err != nil {
				t.Fatal(err)
			}
			if removed {
				err = os.Remove(path)
			} else {
				err = os.Truncate(path, 1)
			}
			if err != nil {
				t.Fatal(err)
			}
			draft := store.ComposerDraft{Files: []store.DraftFile{{ID: "f", Size: 8}}}
			if uploads, err := readDraftUploads(draft, req.MultipartForm.File); err == nil || uploads != nil {
				t.Fatal("lost bytes were acknowledged", uploads, err)
			} else if !removed && !errors.Is(err, store.ErrInvalidDraft) {
				t.Fatal("truncation classification lost", err)
			}
		})
	}
}
