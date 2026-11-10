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

	if wantsEventStream(c) {
		s.streamSideChat(c, input)
		return
	}
	answer, err := s.engine.AskSideChat(c.Request.Context(), c.Param("id"), input)
	if err == nil {
		c.JSON(http.StatusOK, gin.H{"answer": answer})
		return
	}
	s.failSideChat(c, err)
}

func (s *Server) streamSideChat(c *gin.Context, input engine.SideChatInput) {
	write := func(kind string, payload gin.H) {
		if !c.Writer.Written() {
			c.Header("Content-Type", "text/event-stream")
			c.Header("X-Accel-Buffering", "no")
		}
		writeSSE(c, kind, payload, 0)
		c.Writer.Flush()
	}
	ctx := c.Request.Context()
	answer, err := s.engine.StreamSideChat(ctx, c.Param("id"), input, func(text string) {
		if ctx.Err() == nil {
			write("delta", gin.H{"text": text})
		}
	})
	if ctx.Err() != nil {
		return
	}
	if err == nil {
		write("done", gin.H{"answer": answer})
	} else if !c.Writer.Written() {
		s.failSideChat(c, err)
	} else {
		_, payload := sideChatFailure(err)
		write("error", payload)
	}
}

func (s *Server) failSideChat(c *gin.Context, err error) {
	switch {
	case errors.Is(err, engine.ErrInvalidSideChat), errors.Is(err, store.ErrNotFound):
		s.fail(c, err)
	case errors.Is(err, context.Canceled) && c.Request.Context().Err() != nil:
		return
	default:
		status, payload := sideChatFailure(err)
		c.JSON(status, payload)
	}
}

func sideChatFailure(err error) (int, gin.H) {
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout, gin.H{"error": "The side question timed out.", "code": "side_chat_timeout"}
	}
	// Keep provider response bodies out of the public response and access log.
	return http.StatusBadGateway, gin.H{"error": "Side chat could not get an answer.", "code": "side_chat_unavailable"}
}
