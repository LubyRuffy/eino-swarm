package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
)

func TestTimeoutFirstByteBudgetIsTypedAndIndependentOfBodySilence(t *testing.T) {
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer s.Close()
	defer close(release)
	_, err := chatHTTPClient(time.Second, 80*time.Millisecond).Get(s.URL)
	var first FirstByteTimeoutError
	if !errors.As(err, &first) || first.Wait != 80*time.Millisecond || first.Unwrap() == nil || !strings.Contains(first.Error(), "80ms") {
		t.Fatalf("lost typed header budget: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.URL, nil)
	_, err = chatHTTPClient(time.Second, 80*time.Millisecond).Do(req)
	if !errors.Is(err, context.Canceled) || errors.As(err, &first) {
		t.Fatalf("cancel mislabeled first byte: %v", err)
	}
	transport := chatHTTPClient(50*time.Millisecond, 100*time.Millisecond).Transport.(*idleTransport).base.(*http.Transport)
	if transport.ResponseHeaderTimeout != 100*time.Millisecond {
		t.Fatal("idle capped the independent header wait")
	}
}

func TestTimeoutDefaultHeaderWaitDoesNotInheritTheIdleBudget(t *testing.T) {
	transport := chatHTTPClient(60*time.Millisecond, 0).Transport.(*idleTransport).base.(*http.Transport)
	if transport.ResponseHeaderTimeout != config.DefaultFirstByteTimeout {
		t.Fatalf("default header wait=%v", transport.ResponseHeaderTimeout)
	}
}

func TestTimeoutSaveCannotLeaveAnOldClientInTheInvalidatedCache(t *testing.T) {
	cfg := configFor(t, true)
	p := New(cfg)
	entered, release := make(chan struct{}), make(chan struct{})
	var built []config.Provider
	p.build = func(_ context.Context, prov config.Provider) (model.BaseChatModel, error) {
		built = append(built, prov)
		if len(built) == 1 {
			close(entered)
			<-release
		}
		return newMockModel(""), nil
	}
	got := make(chan error, 1)
	go func() { _, err := p.Get(t.Context(), "", ""); got <- err }()
	<-entered
	saved, finished := make(chan struct{}), make(chan error, 1)
	go func() {
		err := cfg.Update(func(c *config.Config) error {
			c.Models.Providers[0].TimeoutSeconds = 17
			c.Models.Providers[0].FirstByteTimeoutSeconds = 9
			return nil
		})
		close(saved)
		if err == nil {
			p.Invalidate()
		}
		finished <- err
	}()
	<-saved
	close(release)
	if err := <-got; err != nil {
		t.Fatal(err)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if _, err := p.Get(t.Context(), "", ""); err != nil {
		t.Fatal(err)
	}
	if len(built) != 2 || built[1].TimeoutSeconds != 17 || built[1].FirstByteTimeoutSeconds != 9 {
		t.Fatal("new call retained old timeout client")
	}
}

func TestTimeoutFirstByteOverrideDoesNotBecomeATotalOrIdleDeadline(t *testing.T) {
	release := make(chan struct{})
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		_, _ = io.WriteString(w, "start")
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer s.Close()
	defer close(release)
	resp, err := chatHTTPClient(300*time.Millisecond, 100*time.Millisecond).Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	_, err = io.ReadAll(resp.Body)
	var first FirstByteTimeoutError
	if !errors.Is(err, ErrIdleTimeout) || errors.As(err, &first) {
		t.Fatalf("idle mislabeled first byte: %v", err)
	}
}
