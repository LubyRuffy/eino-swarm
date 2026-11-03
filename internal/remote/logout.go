package remote

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/pairlink/crypto"
)

var (
	// ErrAccountLogoutUnavailable distinguishes anonymous connection authority.
	ErrAccountLogoutUnavailable = errors.New("remote: account logout requires NOSEC mode")
	// ErrAccountLogoutInProgress prevents new login ownership crossing logout.
	ErrAccountLogoutInProgress = ErrAccountOperationInProgress
	// ErrGatewayLogoutUnsupported preserves the only recovery credential on old servers.
	ErrGatewayLogoutUnsupported = errors.New("remote: upgrade the Gateway to support device logout, then retry")
)

type logoutAttempt struct {
	cancel  context.CancelFunc
	done    chan struct{}
	invalid bool // protected by Host.mu; cancellation retains admission until join.
}

func (h *Host) cancelLogoutLocked() *logoutAttempt {
	previous := h.logoutActive
	if previous != nil {
		previous.invalid = true
		previous.cancel()
	}
	return previous
}

func finishLogout(attempt *logoutAttempt) {
	if attempt != nil {
		<-attempt.done
	}
}

// Logout revokes this device at its exact Gateway before removing local proof.
func (h *Host) Logout() error { return h.LogoutContext(context.Background()) }

// LogoutContext fences login, stops owned transports and confirms remote
// revocation outside the Host lock. Its whole network phase has a 15s budget.
func (h *Host) LogoutContext(parent context.Context) error {
	if !h.accountMu.TryLock() {
		return ErrAccountOperationInProgress
	}
	defer h.accountMu.Unlock()
	h.mu.Lock()
	cfg := h.cfg.Snapshot()
	if cfg.Remote.AuthMode != config.RemoteAuthNOSEC {
		h.mu.Unlock()
		return ErrAccountLogoutUnavailable
	}
	if h.logoutActive != nil {
		h.mu.Unlock()
		return ErrAccountLogoutInProgress
	}
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	attempt := &logoutAttempt{cancel: cancel, done: make(chan struct{})}
	h.logoutActive = attempt
	previous := h.cancelLoginLocked()
	stopped := h.stopLocked()
	h.login = LoginStatus{State: "signing_out"}
	h.err = ""
	origin, err := config.RemoteOrigin(cfg.Remote.HubURL)
	public := ""
	if err == nil {
		if h.id != nil {
			public = crypto.PublicBase64(h.id.Public())
		} else {
			var raw []byte
			raw, err = cfg.RemoteIdentity()
			if err == nil && len(raw) != 0 {
				var id *crypto.Identity
				id, err = crypto.FromPrivate(raw)
				if err == nil {
					public = crypto.PublicBase64(id.Public())
				}
			}
		}
	}
	var tokens []string
	var account config.RemoteCredential
	if err == nil {
		tokens, err = h.cfg.RemoteAccountLogoutTokens(origin, public)
	}
	if err == nil {
		account, err = cfg.ReadRemoteCredential()
		if !slices.Contains(tokens, account.Token) {
			account = config.RemoteCredential{}
		}
		h.login = retainedLogoutIdentity(cfg, origin, account)
		h.login.State = "signing_out"
	}
	h.mu.Unlock()
	defer func() {
		cancel()
		h.mu.Lock()
		if h.logoutActive == attempt {
			h.logoutActive = nil
		}
		h.mu.Unlock()
		close(attempt.done)
	}()
	finishLogin(previous)
	stopped.finish()
	if err == nil {
		client := newLoginClient()
		for _, token := range tokens {
			if err = logoutCredential(ctx, client, origin, token); err != nil {
				break
			}
		}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.logoutActive != attempt || attempt.invalid || ctx.Err() != nil {
		if err == nil {
			err = errors.New("remote: account logout was cancelled; retry")
		}
		// Reload/Stop owns the replacement status after cancelling this attempt.
		if h.logoutActive != attempt || attempt.invalid {
			return err
		}
	}
	if err == nil {
		err = h.cfg.ClearRemoteAccountTokens(origin, public, tokens)
	}
	if err != nil {
		h.login = retainedLogoutIdentity(h.cfg.Snapshot(), origin, account)
		h.login.State, h.login.ErrorCode = "error", logoutErrorCode(err)
		return fmt.Errorf("remote: account logout failed: %w", err)
	}
	h.login = LoginStatus{State: "signed_out"}
	return nil
}

// A retained local receipt identifies the account whose logout needs retry; it
// does not prove a current remote grant. Exact token and owner checks prevent a
// late failure from relabelling a replacement receipt with the previous user.
func retainedLogoutIdentity(cfg *config.Config, origin string, receipt config.RemoteCredential) LoginStatus {
	currentOrigin, err := config.RemoteOrigin(cfg.Remote.HubURL)
	if err != nil || currentOrigin != origin || cfg.Remote.AuthMode != config.RemoteAuthNOSEC || !receipt.Authorized || receipt.Token == "" || receipt.HostID == "" || receipt.UserID == "" {
		return LoginStatus{}
	}
	current, err := cfg.ReadRemoteCredential()
	if err != nil || !current.Authorized || current.Origin != origin || current.Token != receipt.Token || current.HostID != receipt.HostID || current.UserID != receipt.UserID {
		return LoginStatus{}
	}
	return LoginStatus{HostID: current.HostID, UserID: current.UserID, Username: current.Username, DisplayName: current.DisplayName}
}
