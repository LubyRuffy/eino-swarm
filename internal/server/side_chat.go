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

// The JSON envelope may expand UTF-8 text through JSON escaping. The engine
// separately checks the decoded text and history limits.
const (
	sideChatBodyLimit   = 384 << 10
	sideChatConcurrency = 4
)

func (s *Server) askSideChat(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	body := http.MaxBytesReader(c.Writer, c.Request.Body, sideChatBodyLimit)
	defer body.Close()
	raw, err := io.ReadAll(body)
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) {
		c.JSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Side chat request is too large.", "code": "invalid_side_chat"})
		return
	}
	var input engine.SideChatInput
	if err != nil || json.Unmarshal(raw, &input) != nil {
		s.fail(c, engine.ErrInvalidSideChat)
		return
	}

	select {
	case s.sideChats <- struct{}{}:
		defer func() { <-s.sideChats }()
	default:
		c.JSON(http.StatusTooManyRequests, gin.H{"error": "Too many side questions are running.", "code": "side_chat_busy"})
		return
	}

	answer, err := s.engine.AskSideChat(c.Request.Context(), c.Param("id"), input)
	switch {
	case err == nil:
		c.JSON(http.StatusOK, gin.H{"answer": answer})
	case errors.Is(err, engine.ErrInvalidSideChat), errors.Is(err, store.ErrNotFound):
		s.fail(c, err)
	case errors.Is(err, context.Canceled) && c.Request.Context().Err() != nil:
		return
	case errors.Is(err, context.DeadlineExceeded):
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": "The side question timed out.", "code": "side_chat_timeout"})
	default:
		// An upstream error can include request excerpts or credentials. Keep
		// the public response and access log free of provider response bodies.
		c.JSON(http.StatusBadGateway, gin.H{"error": "Side chat could not get an answer.", "code": "side_chat_unavailable"})
	}
}
