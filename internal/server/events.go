package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// heartbeatInterval keeps the connection warm through proxies and lets the
// client notice a dead server without waiting for TCP to time out.
const heartbeatInterval = 20 * time.Second

// lagCheckInterval is how often a connection checks whether it missed stored
// events while it was busy writing.
const lagCheckInterval = 200 * time.Millisecond

// streamEvents is the conversation's live event stream.
//
// It replays first and then goes live, from one subscription taken before the
// replay: subscribing after replaying would drop everything that happened in
// between, which for a streaming turn is the interesting part. Anything the
// replay and the live stream both carry is filtered by sequence number.
func (s *Server) streamEvents(c *gin.Context) {
	th, ok := s.thread(c)
	if !ok {
		return
	}
	since := parseSince(c)

	sub := s.engine.Subscribe(th.ID)
	defer sub.Close()

	w := c.Writer
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	// Proxies that buffer would defeat the whole point of streaming.
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	w.Flush()

	highest := since
	// catchUp sends everything stored after the last sequence number this
	// connection delivered. It runs for the initial replay and again whenever
	// a burst outran the subscription, which is what keeps a stored event from
	// being lost to a slow client.
	catchUp := func() bool {
		history, err := s.engine.Replay(th.ID, highest)
		if err != nil {
			writeSSE(c, "error", gin.H{"error": err.Error()}, 0)
			return false
		}
		for _, ev := range history {
			writeSSE(c, ev.Kind, ev, ev.Seq)
			if ev.Seq > highest {
				highest = ev.Seq
			}
		}
		return true
	}
	if !catchUp() {
		return
	}
	// Tell the client the replay is over so it can stop showing a loading
	// state and start rendering streamed text.
	writeSSE(c, "ready", gin.H{
		"seq":    highest,
		"status": s.engine.Status(th.ID),
	}, 0)
	w.Flush()

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	// The last dropped event may be the one that says the turn finished, and
	// no further event would come to trigger a catch-up. Checking a flag on a
	// short tick costs nothing; the database is only touched when it is set.
	lagTicker := time.NewTicker(lagCheckInterval)
	defer lagTicker.Stop()
	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-lagTicker.C:
			if sub.Lagged() {
				if !catchUp() {
					return
				}
				w.Flush()
			}
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": keep-alive\n\n"); err != nil {
				return
			}
			w.Flush()
		case ev, open := <-sub.C:
			if !open {
				return
			}
			if sub.Lagged() && !catchUp() {
				return
			}
			// Persisted events carry a sequence number; anything at or below
			// what the replay already delivered is a duplicate. Deltas have no
			// sequence number and are always new.
			if ev.Seq > 0 && ev.Seq <= highest {
				w.Flush()
				continue
			}
			if ev.Seq > highest {
				highest = ev.Seq
			}
			writeSSE(c, ev.Kind, ev, ev.Seq)
			w.Flush()
		}
	}
}

// parseSince reads the resume cursor, preferring the standard Last-Event-ID
// header so an EventSource that reconnects on its own resumes correctly
// without the client having to track anything.
func parseSince(c *gin.Context) int64 {
	if raw := strings.TrimSpace(c.GetHeader("Last-Event-ID")); raw != "" {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	if n, err := strconv.ParseInt(c.Query("since"), 10, 64); err == nil && n > 0 {
		return n
	}
	return 0
}

// writeSSE emits one event. The id field is only set for persisted events,
// because it is what a reconnect resumes from and a delta cannot be resumed
// from — it was never stored.
func writeSSE(c *gin.Context, kind string, payload any, seq int64) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return
	}
	w := c.Writer
	if seq > 0 {
		fmt.Fprintf(w, "id: %d\n", seq)
	}
	fmt.Fprintf(w, "event: %s\n", sseName(kind))
	fmt.Fprintf(w, "data: %s\n\n", raw)
}

// sseName keeps the event name to one line; an event name with a newline in it
// would corrupt the stream framing.
func sseName(kind string) string {
	kind = strings.TrimSpace(strings.ReplaceAll(kind, "\n", " "))
	if kind == "" {
		return "message"
	}
	return kind
}
