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
	"slices"
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
	AvatarURL   string `json:"avatar_url,omitempty"`
	Models      int    `json:"models"`
	// SourceID is the runtime provider id (gateway:<hex>). It is not an endpoint.
	SourceID string               `json:"source_id,omitempty"`
	Kinds    *provider.ModelKinds `json:"kinds,omitempty"`
	// Profile is set only while the server published an official-model agreement.
	// It carries no instruction text.
	Profile *accountProfileState `json:"profile,omitempty"`
	// Roles is set only for a FOFA install whose server published fixed roles.
	// The controls are read-only. An old server omits the field.
	Roles []provider.AccountRole `json:"roles,omitempty"`
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
	// publishedToken is the credential the published catalog was fetched with.
	// Empty means nothing is published. Seal compares it with the credential
	// that is current after login or logout.
	publishedToken  string
	publishedOrigin string
	// lastKey identifies the credential the latest refresh used, so Reconcile
	// can tell a changed account from an unrelated settings save.
	lastKey string
	// beforePublish yields between the fetch and the publish. Tests use it to
	// put a credential change in that gap; production leaves it nil.
	beforePublish func()

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
		a.state = AccountModelsState{State: AccountPending, Username: id.username, DisplayName: id.displayName, AvatarURL: id.avatarURL}
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
	state := a.state
	a.mu.RUnlock()
	if state.Profile != nil && a.cfg != nil {
		copied := *state.Profile
		copied.Presentation = a.cfg.ModelsSnapshot().Presentation()
		if copied.Tools != nil {
			copied.Tools = append([]string(nil), copied.Tools...)
		}
		state.Profile = &copied
	}
	return state
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
	origin, token, username, displayName, userID, avatarURL string
}

// identity is the credential this device may use for model access: an
// authorized gateway account login issued by the configured server.
func (a *AccountModels) identity() (accountIdentity, bool) {
	cfg := a.cfg.Snapshot()
	if cfg.Remote.AuthMode != config.RemoteAuthAccount {
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
	return accountIdentity{
		origin: origin, token: credential.Token, username: credential.Username, displayName: credential.DisplayName,
		userID: credential.UserID, avatarURL: config.AcceptedAvatarURL(credential.AvatarURL),
	}, true
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
		return a.publish(id, accountOutcome{state: AccountModelsState{State: AccountSignedOut}})
	}
	outcome := a.fetch(ctx, id)
	if ctx.Err() != nil {
		return a.State()
	}
	// The fetch observed id. Login or logout can commit before we publish,
	// so publish checks the credential again instead of trusting id.
	if a.beforePublish != nil {
		a.beforePublish()
	}
	return a.publish(id, outcome)
}

// Seal makes the published catalog match the credential that just changed.
// Login and logout call it before they return: a refresh that already passed
// its check can still be inside publish, and this waits for that publish, then
// drops a catalog that belongs to a credential which is no longer current.
func (a *AccountModels) Seal() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	now, ok := a.identity()
	if !ok {
		a.apply(accountIdentity{}, accountOutcome{state: AccountModelsState{State: AccountSignedOut}})
		return
	}
	if a.publishedToken == now.token && a.publishedOrigin == now.origin {
		return
	}
	a.pool.SetAccountSource(nil)
	a.published = false
	a.publishedToken, a.publishedOrigin = "", ""
	a.state = AccountModelsState{State: AccountPending, Username: now.username, DisplayName: now.displayName, AvatarURL: now.avatarURL}
}

func (a *AccountModels) setLastKey(key string) {
	a.mu.Lock()
	a.lastKey = key
	a.mu.Unlock()
}

func (a *AccountModels) publish(fetched accountIdentity, o accountOutcome) AccountModelsState {
	a.mu.Lock()
	defer a.mu.Unlock()
	now, ok := a.identity()
	switch {
	case !ok:
		// The credential is gone. Publish that, whatever the fetch observed.
		o = accountOutcome{state: AccountModelsState{State: AccountSignedOut}}
		fetched = accountIdentity{}
	case fetched.token == "" || now.token != fetched.token || now.origin != fetched.origin:
		// A newer login owns the pool now. Leave its pending or ready catalog
		// alone; the refresh triggered for it publishes its own models.
		return a.state
	case o.transient && a.published:
		// A brief outage must not take working models away mid-conversation.
		a.log.Warn("account models refresh failed; keeping the last working catalog", "reason", o.state.Reason)
		return a.state
	}
	return a.apply(fetched, o)
}

// apply writes the outcome. The caller holds a.mu and has decided that the
// outcome belongs to the current credential.
func (a *AccountModels) apply(fetched accountIdentity, o accountOutcome) AccountModelsState {
	a.pool.SetAccountSource(o.source)
	a.published = o.source != nil
	if o.source != nil {
		a.publishedToken, a.publishedOrigin = fetched.token, fetched.origin
	} else {
		a.publishedToken, a.publishedOrigin = "", ""
	}
	if !sameAccountState(o.state, a.state) {
		a.log.Info("account models", "state", o.state.State, "reason", o.state.Reason, "models", o.state.Models)
	}
	a.state = o.state
	return a.state
}

func sameAccountState(a, b AccountModelsState) bool {
	ar, br := a.Roles, b.Roles
	a.Roles, b.Roles = nil, nil
	return a.State == b.State &&
		a.Reason == b.Reason &&
		a.Label == b.Label &&
		a.Username == b.Username &&
		a.DisplayName == b.DisplayName &&
		a.AvatarURL == b.AvatarURL &&
		a.Models == b.Models &&
		a.SourceID == b.SourceID &&
		a.Kinds == b.Kinds &&
		a.Profile == b.Profile &&
		slices.Equal(ar, br)
}

func (a *AccountModels) fetch(ctx context.Context, id accountIdentity) accountOutcome {
	who := AccountModelsState{Username: id.username, DisplayName: id.displayName, AvatarURL: id.avatarURL}
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
	who = a.withPicture(id, boot, who)
	if !boot.access {
		s := who
		s.State, s.Label = AccountNoAccess, boot.label
		return accountOutcome{state: s}
	}
	if a.cfg != nil && a.cfg.Product.FOFA() && len(boot.roles) > 0 {
		return fetchRoles(boot, id.token, who)
	}
	if boot.profile != nil {
		source, err := provider.NewAccountProfile(boot.sourceID, boot.apiBaseURL, id.token, *boot.profile)
		if err != nil {
			return fail(AccountUnavailable, AccountReasonUnsafe, false)
		}
		s := who
		s.State, s.Label, s.Models = AccountReady, boot.profile.Label, 1
		s.SourceID = source.ID()
		kinds := source.Kinds()
		s.Kinds = &kinds
		view := profileState(*boot.profile, a.cfg.ModelsSnapshot().Presentation())
		s.Profile = &view
		return accountOutcome{state: s, source: source}
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
	s.SourceID = source.ID()
	kinds := source.Kinds()
	s.Kinds = &kinds
	return accountOutcome{state: s, source: source}
}

func fetchRoles(boot accountBootstrap, token string, who AccountModelsState) accountOutcome {
	fail := func(state, reason string) accountOutcome {
		s := who
		s.State, s.Reason = state, reason
		return accountOutcome{state: s}
	}
	conversation := false
	for _, role := range boot.roles {
		if role.ID == "conversation" && role.Enabled {
			conversation = true
		}
	}
	roles := append([]provider.AccountRole(nil), boot.roles...)
	if !conversation {
		s := who
		s.State, s.Reason, s.Label = AccountNoAccess, AccountReasonNoModels, boot.label
		s.Roles = roles
		return accountOutcome{state: s}
	}
	source, err := provider.NewAccountSource(boot.sourceID, boot.label, boot.apiBaseURL, token, provider.Catalog{
		Names: []string{"conversation"},
		Types: map[string]string{"conversation": "chat"},
	})
	if err != nil {
		return fail(AccountUnavailable, AccountReasonUnsafe)
	}
	source.SetRoles(roles)
	s := who
	s.State, s.Label, s.Models = AccountReady, boot.label, 1
	s.SourceID = source.ID()
	kinds := source.Kinds()
	s.Kinds = &kinds
	s.Roles = roles
	return accountOutcome{state: s, source: source}
}

// classifyAccountError maps a bootstrap failure to a public state. It covers
// a rejected sign-in, a server that lacks the feature, and a plain outage.
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
		if kind := strings.TrimSpace(cat.Types[name]); kind != "" {
			if out.Types == nil {
				out.Types = map[string]string{}
			}
			out.Types[name] = kind
		}
	}
	return out
}

type accountBootstrap struct {
	sourceID, label, apiBaseURL, catalogURL string
	access                                  bool
	userID, avatarURL                       string
	profile                                 *provider.AccountProfile
	roles                                   []provider.AccountRole
}

// withPicture adopts a bootstrap avatar only for this same account. A missing
// or rejected address keeps the one already saved for that account.
func (a *AccountModels) withPicture(id accountIdentity, boot accountBootstrap, who AccountModelsState) AccountModelsState {
	picture := id.avatarURL
	if boot.userID != "" && boot.userID == id.userID {
		if next := config.AcceptedAvatarURL(boot.avatarURL); next != "" {
			picture = next
		}
	}
	if picture != "" && picture != id.avatarURL && a.cfg != nil {
		if err := a.cfg.RememberRemoteAvatarURL(id.origin, id.token, id.userID, picture); err != nil {
			a.log.Warn("account avatar was not saved", "err", err)
		}
	}
	who.AvatarURL = picture
	return who
}

func parseAccountBootstrap(raw []byte, origin string) (accountBootstrap, error) {
	var envelope struct {
		Data struct {
			User struct {
				ID        string `json:"id"`
				AvatarURL string `json:"avatar_url"`
			} `json:"user"`
			Models struct {
				SourceID         string          `json:"source_id"`
				SourceKind       string          `json:"source_kind"`
				Label            string          `json:"label"`
				Execution        string          `json:"execution"`
				APIBaseURL       string          `json:"api_base_url"`
				CatalogURL       string          `json:"catalog_url"`
				APIAccessEnabled bool            `json:"api_access_enabled"`
				Profile          json.RawMessage `json:"profile"`
				Roles            json.RawMessage `json:"roles"`
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
	out := accountBootstrap{sourceID: m.SourceID, label: label, access: m.APIAccessEnabled, userID: envelope.Data.User.ID, avatarURL: envelope.Data.User.AvatarURL}
	if !m.APIAccessEnabled {
		return out, nil
	}
	if !sameOriginURL(m.APIBaseURL, origin) || !sameOriginURL(m.CatalogURL, origin) {
		return accountBootstrap{}, errAccountResponse
	}
	out.apiBaseURL, out.catalogURL = strings.TrimRight(m.APIBaseURL, "/"), m.CatalogURL
	profile, err := parseAccountProfile(m.Profile)
	if err != nil {
		return accountBootstrap{}, err
	}
	roles, err := parseAccountRoles(m.Roles)
	if err != nil {
		return accountBootstrap{}, err
	}
	out.profile, out.roles = profile, roles
	return out, nil
}

func parseAccountRoles(raw json.RawMessage) ([]provider.AccountRole, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var rows []provider.AccountRole
	if json.Unmarshal(raw, &rows) != nil || len(rows) > 9 {
		return nil, errAccountResponse
	}
	if len(rows) == 0 {
		return nil, nil
	}
	seen := map[string]bool{}
	for i := range rows {
		role := &rows[i]
		role.ID = strings.TrimSpace(role.ID)
		switch role.ID {
		case "conversation", "title", "compact", "embedding", "ocr", "decisions", "transcription", "speech", "image":
		default:
			return nil, errAccountResponse
		}
		if seen[role.ID] {
			return nil, errAccountResponse
		}
		seen[role.ID] = true
		role.Label = strings.TrimSpace(role.Label)
		if role.Label == "" {
			role.Label = role.ID
		}
		if len(role.Label) > 255 || strings.IndexFunc(role.Label, func(r rune) bool { return r < 0x20 || r == 0x7f }) >= 0 {
			return nil, errAccountResponse
		}
	}
	return rows, nil
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
