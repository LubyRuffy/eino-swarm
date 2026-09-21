package search

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const (
	DefaultLimit = 20
	MaxLimit     = 50
)

// Bounded wakeup buffer. Overflow lives in `overflow` so a first-enable
// backfill of a big library is not silently dropped until the next Reload.
var queueSize = 256

// Embedder turns texts into vectors. The provider pool is the production
// implementation; tests pin known vectors.
type Embedder interface {
	Embed(ctx context.Context, providerID, model string, texts []string) ([][]float32, error)
}

// Result is what GET /api/search returns.
type Result struct {
	Query     string `json:"query"`
	Embedding bool   `json:"embedding"`
	Hits      []Hit  `json:"hits"`
}

// Service is keyword search always, semantic search when the user pinned a
// model. Indexing embeddings is a background queue so a turn is not blocked
// on /embeddings.
type Service struct {
	store *store.Store
	embed Embedder
	cfg   *config.Config
	log   *slog.Logger

	mu       sync.Mutex
	queue    chan string
	overflow map[string]struct{}
	stop     chan struct{}
	wg       sync.WaitGroup
	inflight sync.WaitGroup
	pin      string
}

// New builds a service. Start the worker with Start.
func New(st *store.Store, embed Embedder, cfg *config.Config, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{
		store:    st,
		embed:    embed,
		cfg:      cfg,
		log:      log,
		queue:    make(chan string, queueSize),
		overflow: make(map[string]struct{}),
		stop:     make(chan struct{}),
	}
}

// Start drains the embedding queue. Safe to call once.
func (s *Service) Start() {
	s.wg.Add(1)
	go s.loop()
	s.Reload()
}

// Stop finishes the embed already running, then drops anything still
// queued. Call before closing the store.
func (s *Service) Stop() {
	s.mu.Lock()
	select {
	case <-s.stop:
		s.mu.Unlock()
		return
	default:
		close(s.stop)
	}
	s.mu.Unlock()
	s.wg.Wait()
}

func (s *Service) loop() {
	defer s.wg.Done()
	for {
		s.promoteOverflow()
		select {
		case <-s.stop:
			s.abandonQueued()
			return
		case id := <-s.queue:
			if err := s.embedThread(context.Background(), id); err != nil {
				s.log.Warn("could not embed a conversation", "thread", id, "err", err)
			}
			s.inflight.Done()
		}
	}
}

// Index refreshes keyword search (cheap) and queues embeddings when enabled.
func (s *Service) Index(threadID string) {
	if strings.TrimSpace(threadID) == "" {
		return
	}
	if err := s.store.IndexThread(threadID); err != nil {
		s.log.Warn("could not index a conversation", "thread", threadID, "err", err)
	}
	if s.semantic() {
		s.enqueue(threadID)
	}
}

// Remove is a no-op for rows: DeleteThread already dropped the index. It
// exists so the engine can talk to one interface.
func (s *Service) Remove(threadID string) {}

// Reload reapplies the live pin: a model change drops old vectors and
// backfills. Turning the switch off leaves keyword search running.
func (s *Service) Reload() {
	if !s.semantic() {
		s.mu.Lock()
		s.pin = ""
		s.mu.Unlock()
		return
	}
	model := strings.TrimSpace(s.cfg.Search.EmbeddingModel)
	s.mu.Lock()
	changed := s.pin != model
	s.pin = model
	s.mu.Unlock()
	if changed {
		if err := s.store.DeleteSearchChunksNotModel(model); err != nil {
			s.log.Warn("could not drop stale embedding vectors", "err", err)
		}
	}
	ids, err := s.store.ListThreadIDs()
	if err != nil {
		s.log.Warn("could not list conversations to embed", "err", err)
		return
	}
	for _, id := range ids {
		s.enqueue(id)
	}
}

func (s *Service) semantic() bool {
	return s.cfg != nil && s.cfg.Search.SemanticEnabled() && s.embed != nil
}

func (s *Service) enqueue(id string) {
	s.inflight.Add(1)
	select {
	case <-s.stop:
		s.inflight.Done()
		return
	case s.queue <- id:
	default:
		s.mu.Lock()
		if _, ok := s.overflow[id]; ok {
			s.inflight.Done()
		} else {
			s.overflow[id] = struct{}{}
		}
		s.mu.Unlock()
	}
}

func (s *Service) promoteOverflow() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id := range s.overflow {
		select {
		case s.queue <- id:
			delete(s.overflow, id)
		default:
			return
		}
	}
}

func (s *Service) abandonQueued() {
	s.mu.Lock()
	for id := range s.overflow {
		delete(s.overflow, id)
		s.inflight.Done()
	}
	s.mu.Unlock()
	for {
		select {
		case <-s.queue:
			s.inflight.Done()
		default:
			return
		}
	}
}

func (s *Service) pinModel() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.pin != "" {
		return s.pin
	}
	return strings.TrimSpace(s.cfg.Search.EmbeddingModel)
}

func (s *Service) providerID() string {
	return s.cfg.Search.ResolveProvider(s.cfg.Models.Default)
}

func clampLimit(n int) int {
	if n <= 0 {
		return DefaultLimit
	}
	if n > MaxLimit {
		return MaxLimit
	}
	return n
}

// WaitIdle is for tests: drain the queue without shutting the worker down.
func (s *Service) WaitIdle() {
	s.inflight.Wait()
}
