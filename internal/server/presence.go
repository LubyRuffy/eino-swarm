package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Surfaces a shell may register. The title bar prints these; anything else
// is a client inventing a channel.
var presenceSurfaces = map[string]bool{
	"desktop": true,
	"web":     true,
	"tui":     true,
}

// PresenceClient is one shell holding a connection open. The engine stays up
// while this list is non-empty.
type PresenceClient struct {
	ID      string `json:"id"`
	Surface string `json:"surface"`
	PID     int    `json:"pid,omitempty"`
}

type presenceSlot struct {
	client  PresenceClient
	gen     int
	hold    chan struct{}
	created time.Time
}

// presence tracks shells connected to this engine. A POST reserves an id; the
// GET holds it until the socket drops. A reserve that never connects does not
// count, or a crashed launcher would pin the process forever.
type presence struct {
	mu    sync.Mutex
	slots map[string]*presenceSlot
}

func newPresence() *presence {
	return &presence{slots: map[string]*presenceSlot{}}
}

func (p *presence) reserve(surface string, pid int) (string, error) {
	p.gc()
	id, err := newPresenceID()
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	p.slots[id] = &presenceSlot{
		client:  PresenceClient{ID: id, Surface: surface, PID: pid},
		created: time.Now(),
	}
	p.mu.Unlock()
	return id, nil
}

// attach marks the reserve as a live client. A second attach for the same id
// (EventSource reconnect) replaces the first so the shell is not counted twice.
// The returned channel is the one this caller waits on; a later attach closes
// it. release drops the slot only if this generation is still current.
func (p *presence) attach(id string) (<-chan struct{}, func(), bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	slot, ok := p.slots[id]
	if !ok {
		return nil, nil, false
	}
	if slot.hold != nil {
		close(slot.hold)
	}
	slot.gen++
	slot.hold = make(chan struct{})
	gen := slot.gen
	hold := slot.hold
	return hold, func() { p.detach(id, gen, hold) }, true
}

func (p *presence) detach(id string, gen int, hold chan struct{}) {
	p.mu.Lock()
	defer p.mu.Unlock()
	slot, ok := p.slots[id]
	if !ok || slot.gen != gen || slot.hold != hold {
		return
	}
	delete(p.slots, id)
}

func (p *presence) list() []PresenceClient {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]PresenceClient, 0, len(p.slots))
	for _, slot := range p.slots {
		if slot.hold == nil {
			continue
		}
		out = append(out, slot.client)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Surface != out[j].Surface {
			return out[i].Surface < out[j].Surface
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func (p *presence) desktopPIDs() []int {
	if p == nil {
		return nil
	}
	var out []int
	for _, client := range p.list() {
		if client.Surface == "desktop" && client.PID > 0 {
			out = append(out, client.PID)
		}
	}
	return out
}

func (p *presence) gc() {
	p.mu.Lock()
	defer p.mu.Unlock()
	cut := time.Now().Add(-time.Minute)
	for id, slot := range p.slots {
		if slot.hold == nil && slot.created.Before(cut) {
			delete(p.slots, id)
		}
	}
}

func newPresenceID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "pc_" + hex.EncodeToString(b[:]), nil
}

func (s *Server) Clients() []PresenceClient {
	if s == nil || s.presence == nil {
		return nil
	}
	return s.presence.list()
}

func (s *Server) postPresence(c *gin.Context) {
	var req struct {
		Surface string `json:"surface"`
		PID     int    `json:"pid"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "presence body must be JSON")
		return
	}
	if !presenceSurfaces[req.Surface] {
		badRequest(c, "unknown presence surface")
		return
	}
	id, err := s.presence.reserve(req.Surface, req.PID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"id": id})
}

// holdPresence is the shell's heartbeat. The handler stays open until the
// client drops; that drop is what lets an idle engine exit.
func (s *Server) holdPresence(c *gin.Context) {
	hold, release, ok := s.presence.attach(c.Param("id"))
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "unknown presence"})
		return
	}
	defer release()

	w := c.Writer
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(": held\n\n"))
	w.Flush()

	ctx := c.Request.Context()
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-hold:
			return
		case <-ticker.C:
			if _, err := w.Write([]byte(": ping\n\n")); err != nil {
				return
			}
			w.Flush()
		}
	}
}
