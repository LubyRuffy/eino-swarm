package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/gin-gonic/gin"
)

const (
	selectionTranslationBodyLimit   = 96 << 10
	selectionTranslationConcurrency = 2
)

func (s *Server) translateSelection(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	body := http.MaxBytesReader(c.Writer, c.Request.Body, selectionTranslationBodyLimit)
	defer body.Close()
	raw, err := io.ReadAll(body)
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{
			"error": "The selection is too long to translate.",
			"code":  "selection_translation_too_long",
		})
		return
	}
	var input struct {
		Text string `json:"text"`
	}
	if err != nil || json.Unmarshal(raw, &input) != nil {
		s.fail(c, engine.ErrInvalidSelectionTranslation)
		return
	}
	if s.rejectSignedOut(c) {
		return
	}
	select {
	case s.translations <- struct{}{}:
		defer func() { <-s.translations }()
	default:
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": "Too many translations are running.",
			"code":  "selection_translation_busy",
		})
		return
	}
	text, err := s.engine.TranslateSelection(c.Request.Context(), c.Param("id"), input.Text)
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"text": text})
		return
	}
	if errors.Is(err, context.Canceled) && c.Request.Context().Err() != nil {
		return
	}
	switch {
	case errors.Is(err, engine.ErrInvalidSelectionTranslation),
		errors.Is(err, engine.ErrSelectionTranslationTooLong),
		errors.Is(err, store.ErrNotFound):
		s.fail(c, err)
	default:
		status, payload := selectionTranslationFailure(err)
		c.JSON(status, payload)
	}
}

func selectionTranslationFailure(err error) (int, gin.H) {
	if errors.Is(err, engine.ErrSelectionTranslationDisabled) {
		return http.StatusForbidden, gin.H{
			"error": "Selection translation is switched off.",
			"code":  "selection_translation_disabled",
		}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, gin.H{
			"error": "The translation timed out.",
			"code":  "selection_translation_timeout",
		}
	}
	// Keep provider response bodies out of the public response.
	return http.StatusBadGateway, gin.H{
		"error": "The translation could not be completed.",
		"code":  "selection_translation_unavailable",
	}
}
