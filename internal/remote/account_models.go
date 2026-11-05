package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

// What the signed-in account's model access looks like to the UI.
const (
	AccountSignedOut   = "signed_out"
	AccountPending     = "pending"
	AccountReady       = "ready"
	AccountNoAccess    = "no_access"
	AccountUnavailable = "unavailable"
)

// Why an account is not ready. Stable codes: the UI maps each to its own text.
const (
	AccountReasonRejected    = "rejected"
	AccountReasonUnsupported = "unsupported"
	AccountReasonNetwork     = "network"
	AccountReasonGateway     = "gateway"
	AccountReasonUnsafe      = "unsafe_response"
	AccountReasonNoModels    = "no_models"
)

const (
	defaultAccountRefresh = 10 * time.Minute
	accountRequestTimeout = 20 * time.Second
	accountResponseLimit  = 1 << 20
	accountMaxModels      = 1000
	accountMaxNameLen     = 256
	accountMaxLabelLen    = 64
	desktopModelsPrefix   = "/client/desktop/v1/"
)

var accountSourceID = regexp.MustCompile(`^` + regexp.QuoteMeta(config.AccountSourcePrefix) + `[0-9a-f]{32}$`)

// AccountModelsState is what the local UI may know about the account's model
// access. It carries no credential and no endpoint.
type AccountModelsState struct {
	State       string `json:"state"`
	Reason      string `json:"reason,omitempty"`
	Label       string `json:"label,omitempty"`
	Username    string `json:"username,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Models      int    `json:"models"`
}

// AccountModels projects the signed-in server account's models into the
// provider pool. The account credential is read from private storage and used
// only on requests to the origin it was issued for; nothing it fetches is
// written to config.yaml.
type AccountModels struct {
	cfg  *config.Config
	pool *provider.Pool
	log  *slog.Logger

	client   *http.Client
	interval time.Duration
	trigger  chan struct{}

	refreshMu sync.Mutex // serializes Refresh; never held while reading State
	mu        sync.RWMutex
	state     AccountModelsState
	published bool
	// lastKey identifies the credential the latest refresh used, so Reconcile
	// can tell a changed account from an unrelated settings save.
	lastKey string

	runMu  sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

// NewAccountModels starts in the state the stored credential implies, so the
// first read after startup does not claim "signed out" for a signed-in user.
func NewAccountModels(cfg *config.Config, pool *provider.Pool, log *slog.Logger) *AccountModels {
	if log == nil {
		log = slog.Default()
	}
	a := &AccountModels{
		cfg: cfg, pool: pool, log: log, interval: defaultAccountRefresh, trigger: make(chan struct{}, 1),
		lastKey: "\x00never refreshed",
		client: &http.Client{Timeout: accountRequestTimeout, CheckRedirect: func(*http.Request, []*http.Request) error {
			// A redirect would carry the credential to wherever it points.
			return http.ErrUseLastResponse
		}},
	}
	if id, ok := a.identity(); ok {
		a.state = AccountModelsState{State: AccountPending, Username: id.username, DisplayName: id.displayName}
	} else {
		a.state = AccountModelsState{State: AccountSignedOut}
	}
	return a
}

// State is the latest known state; it never blocks on the network.
func (a *AccountModels) State() AccountModelsState {
	if a == nil {
		return AccountModelsState{State: AccountSignedOut}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.state
}

// Trigger asks the background loop to refresh now. It never blocks and may be
// called while holding any other lock.
func (a *AccountModels) Trigger() {
	if a == nil {
		return
	}
	select {
	case a.trigger <- struct{}{}:
	default:
	}
}

// Reconcile refreshes only if the stored account differs from the one the
// latest refresh used. Settings saves reload the host constantly; an account
// that has not changed must not cost a server round trip each time.
func (a *AccountModels) Reconcile() {
	if a == nil {
		return
	}
	a.mu.RLock()
	last := a.lastKey
	a.mu.RUnlock()
	id, _ := a.identity()
	if identityKey(id) != last {
		a.Trigger()
	}
}

func identityKey(id accountIdentity) string {
	if id.token == "" {
		return ""
	}
	return id.origin + "\x00" + id.token
}

// Start runs the background refresh. It is idempotent.
func (a *AccountModels) Start() {
	if a == nil {
		return
	}
	a.runMu.Lock()
	defer a.runMu.Unlock()
	if a.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	a.cancel, a.done = cancel, done
	go func() {
		defer close(done)
		for {
			a.Refresh(ctx)
			select {
			case <-ctx.Done():
				return
			case <-a.trigger:
			case <-time.After(a.interval):
			}
		}
	}()
}

// Stop ends the background refresh and waits for it. It is safe to call twice,
// and Start may be called again afterwards.
func (a *AccountModels) Stop() {
	if a == nil {
		return
	}
	a.runMu.Lock()
	cancel, done := a.cancel, a.done
	a.cancel, a.done = nil, nil
	a.runMu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

type accountIdentity struct {
	origin, token, username, displayName string
}

// identity is the credential this device may use for model access: an
// authorized NOSEC login issued by the configured server.
func (a *AccountModels) identity() (accountIdentity, bool) {
	cfg := a.cfg.Snapshot()
	if cfg.Remote.AuthMode != config.RemoteAuthNOSEC {
		return accountIdentity{}, false
	}
	origin, err := config.RemoteOrigin(cfg.Remote.HubURL)
	if err != nil {
		return accountIdentity{}, false
	}
	credential, err := cfg.ReadRemoteCredential()
	if err != nil || !credential.Authorized || credential.Origin != origin || credential.HostID == "" || credential.UserID == "" || credential.Token == "" {
		return accountIdentity{}, false
	}
	return accountIdentity{origin: origin, token: credential.Token, username: credential.Username, displayName: credential.DisplayName}, true
}

type accountOutcome struct {
	state     AccountModelsState
	source    *provider.AccountSource
	transient bool
}

// Refresh reads the account's bootstrap and model catalog and publishes the
// result. A cancelled context changes nothing.
func (a *AccountModels) Refresh(ctx context.Context) AccountModelsState {
	if a == nil {
		return AccountModelsState{State: AccountSignedOut}
	}
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	id, ok := a.identity()
	a.setLastKey(identityKey(id))
	if !ok {
		return a.publish(accountOutcome{state: AccountModelsState{State: AccountSignedOut}})
	}
	outcome := a.fetch(ctx, id)
	if ctx.Err() != nil {
		return a.State()
	}
	// Sign-out or a new login can complete while the server is answering.
	if now, ok := a.identity(); !ok || now.token != id.token || now.origin != id.origin {
		return a.publish(accountOutcome{state: AccountModelsState{State: AccountSignedOut}})
	}
	return a.publish(outcome)
}

func (a *AccountModels) setLastKey(key string) {
	a.mu.Lock()
	a.lastKey = key
	a.mu.Unlock()
}

func (a *AccountModels) publish(o accountOutcome) AccountModelsState {
	a.mu.Lock()
	defer a.mu.Unlock()
	if o.transient && a.published {
		// A brief outage must not take working models away mid-conversation.
		a.log.Warn("account models refresh failed; keeping the last working catalog", "reason", o.state.Reason)
		return a.state
	}
	a.pool.SetAccountSource(o.source)
	a.published = o.source != nil
	if o.state != a.state {
		a.log.Info("account models", "state", o.state.State, "reason", o.state.Reason, "models", o.state.Models)
	}
	a.state = o.state
	return a.state
}

func (a *AccountModels) fetch(ctx context.Context, id accountIdentity) accountOutcome {
	who := AccountModelsState{Username: id.username, DisplayName: id.displayName}
	fail := func(state, reason string, transient bool) accountOutcome {
		s := who
		s.State, s.Reason = state, reason
		return accountOutcome{state: s, transient: transient}
	}
	raw, err := a.get(ctx, id.origin+desktopModelsPrefix+"bootstrap", id.token)
	if err != nil {
		return classifyAccountError(err, who)
	}
	boot, err := parseAccountBootstrap(raw, id.origin)
	if err != nil {
		return fail(AccountUnavailable, AccountReasonUnsafe, false)
	}
	if !boot.access {
		s := who
		s.State, s.Label = AccountNoAccess, boot.label
		return accountOutcome{state: s}
	}
	raw, err = a.get(ctx, boot.catalogURL, id.token)
	if err != nil {
		return classifyAccountError(err, who)
	}
	cat, err := provider.ParseCatalog(raw)
	if err != nil {
		return fail(AccountUnavailable, AccountReasonUnsafe, false)
	}
	cat = boundCatalog(cat)
	if len(cat.Names) == 0 {
		s := who
		s.State, s.Reason, s.Label = AccountNoAccess, AccountReasonNoModels, boot.label
		return accountOutcome{state: s}
	}
	source, err := provider.NewAccountSource(boot.sourceID, boot.label, boot.apiBaseURL, id.token, cat)
	if err != nil {
		return fail(AccountUnavailable, AccountReasonUnsafe, false)
	}
	s := who
	s.State, s.Label, s.Models = AccountReady, boot.label, len(cat.Names)
	return accountOutcome{state: s, source: source}
}

// classifyAccountError separates a login that must be redone, access the
// account does not have, a server that lacks the feature, and plain outages.
func classifyAccountError(err error, who AccountModelsState) accountOutcome {
	state := func(state, reason string, transient bool) accountOutcome {
		s := who
		s.State, s.Reason = state, reason
		return accountOutcome{state: s, transient: transient}
	}
	var status *accountStatusError
	switch {
	case errors.As(err, &status):
		switch {
		case status.code == http.StatusUnauthorized:
			return accountOutcome{state: AccountModelsState{State: AccountSignedOut, Reason: AccountReasonRejected}}
		case status.code == http.StatusForbidden:
			return state(AccountNoAccess, "", false)
		case status.code == http.StatusNotFound:
			return state(AccountUnavailable, AccountReasonUnsupported, false)
		default:
			return state(AccountUnavailable, AccountReasonGateway, true)
		}
	case errors.Is(err, errAccountResponse):
		return state(AccountUnavailable, AccountReasonUnsafe, false)
	default:
		return state(AccountUnavailable, AccountReasonNetwork, true)
	}
}

func boundCatalog(cat provider.Catalog) provider.Catalog {
	out := provider.Catalog{Names: make([]string, 0, len(cat.Names))}
	for _, name := range cat.Names {
		if len(name) > accountMaxNameLen || len(out.Names) >= accountMaxModels {
			continue
		}
		out.Names = append(out.Names, name)
		if n := cat.Windows[name]; n > 0 {
			if out.Windows == nil {
				out.Windows = map[string]int{}
			}
			out.Windows[name] = n
		}
	}
	return out
}

type accountBootstrap struct {
	sourceID, label, apiBaseURL, catalogURL string
	access                                  bool
}

func parseAccountBootstrap(raw []byte, origin string) (accountBootstrap, error) {
	var envelope struct {
		Data struct {
			Models struct {
				SourceID         string `json:"source_id"`
				SourceKind       string `json:"source_kind"`
				Label            string `json:"label"`
				Execution        string `json:"execution"`
				APIBaseURL       string `json:"api_base_url"`
				CatalogURL       string `json:"catalog_url"`
				APIAccessEnabled bool   `json:"api_access_enabled"`
			} `json:"models"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return accountBootstrap{}, errAccountResponse
	}
	m := envelope.Data.Models
	if !accountSourceID.MatchString(m.SourceID) || m.SourceKind != "gateway" || m.Execution != "desktop" {
		return accountBootstrap{}, errAccountResponse
	}
	label := strings.TrimSpace(m.Label)
	if len(label) > accountMaxLabelLen {
		label = ""
	}
	out := accountBootstrap{sourceID: m.SourceID, label: label, access: m.APIAccessEnabled}
	if !m.APIAccessEnabled {
		return out, nil
	}
	if !sameOriginURL(m.APIBaseURL, origin) || !sameOriginURL(m.CatalogURL, origin) {
		return accountBootstrap{}, errAccountResponse
	}
	out.apiBaseURL, out.catalogURL = strings.TrimRight(m.APIBaseURL, "/"), m.CatalogURL
	return out, nil
}

// sameOriginURL accepts only an absolute URL on exactly the credential's
// origin, without userinfo, query or fragment.
func sameOriginURL(raw, origin string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return false
	}
	return u.Scheme+"://"+u.Host == origin && strings.HasPrefix(u.Path, "/")
}

var errAccountResponse = errors.New("remote: unexpected account model response")

type accountStatusError struct{ code int }

func (e *accountStatusError) Error() string {
	return fmt.Sprintf("remote: account model request returned HTTP %d", e.code)
}

func (a *AccountModels) get(ctx context.Context, target, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, errAccountResponse
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		// Transport errors can quote the URL; the caller reports only a class.
		return nil, errors.New("remote: account model server is unreachable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &accountStatusError{code: resp.StatusCode}
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, accountResponseLimit+1))
	if err != nil {
		return nil, errors.New("remote: account model server is unreachable")
	}
	if len(raw) > accountResponseLimit {
		return nil, errAccountResponse
	}
	return raw, nil
}
