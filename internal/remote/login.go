package remote

import (
	"context"
	"errors"
	"github.com/LubyRuffy/pairlink/crypto"
	"net/http"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// LoginStatus is safe for the local UI. Device codes, proof and credentials
// never cross the local REST boundary or enter operational diagnostics.
type LoginStatus struct {
	State     string `json:"state"`
	UserCode  string `json:"user_code,omitempty"`
	HostFP    string `json:"host_fp,omitempty"`
	HostID    string `json:"host_id,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Username  string `json:"username,omitempty"`
	ErrorCode string `json:"error_code,omitempty"`
}

type loginAttempt struct {
	ctx       context.Context
	cancel    context.CancelFunc
	done      chan struct{}
	origin    string
	candidate string
	pending   string
	verifier  string
	request   loginStartRequest
	open      func(string) error
	cfg       *config.Config
}

// StartLogin stages a candidate before any remote request. Starting another
// login invalidates ownership of the previous result before cancelling it.
func (h *Host) StartLogin(open func(string) error, newDevice bool) error {
	if open == nil {
		return errors.New("remote: system browser is unavailable")
	}
	h.mu.Lock()
	cfg := h.cfg.Snapshot()
	if !cfg.Remote.Enabled || cfg.Remote.AuthMode != config.RemoteAuthNOSEC {
		h.mu.Unlock()
		return errors.New("remote: enable NOSEC mode before signing in")
	}
	origin, err := config.RemoteOrigin(cfg.Remote.HubURL)
	if err != nil {
		h.mu.Unlock()
		return err
	}
	id, err := loadIdentity(cfg)
	if err != nil {
		h.mu.Unlock()
		return errors.New("remote: private device identity unavailable")
	}
	public := crypto.PublicBase64(id.Public())
	credential, err := cfg.ReadRemoteCredential()
	if err != nil {
		h.mu.Unlock()
		return errors.New("remote: private connection credential unavailable")
	}
	candidate, previous := "", ""
	retained, readErr := cfg.PendingRemoteToken(origin, public)
	if readErr != nil {
		h.mu.Unlock()
		return errors.New("remote: retained login candidate unavailable")
	}
	if retained.Token != "" {
		if newDevice && !retained.ProvenRejected {
			h.mu.Unlock()
			return errors.New("remote: retry the previous login before reconnecting as a new device; its authorization may already have succeeded")
		}
		if !storedTokenShape(retained.Token) {
			h.mu.Unlock()
			return errors.New("remote: retained login candidate unavailable")
		}
		if !newDevice {
			candidate = retained.Token
			previous = candidate
		}
	} else if !newDevice && credential.Origin == origin && storedTokenShape(credential.Token) {
		candidate = credential.Token
		previous = candidate
	}
	if candidate == "" {
		candidate, err = newCandidateToken()
	}
	if err != nil {
		h.mu.Unlock()
		return err
	}
	verifier, err := randomLoginValue()
	if err != nil {
		h.mu.Unlock()
		return err
	}
	pending, err := cfg.StageRemoteToken(candidate, origin, public)
	if err != nil {
		h.mu.Unlock()
		return errors.New("remote: cannot persist login candidate")
	}
	if !newDevice && retained.ProvenRejected {
		if err := cfg.SetPendingRemoteRejected(pending, false); err != nil {
			h.mu.Unlock()
			return errors.New("remote: cannot resume private login candidate")
		}
	}
	if newDevice && retained.ProvenRejected {
		if err := cfg.RemoveRejectedRemoteScope(origin, public); err != nil {
			h.mu.Unlock()
			return errors.New("remote: cannot retire rejected login candidate")
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), loginLifetime)
	attempt := &loginAttempt{ctx: ctx, cancel: cancel, done: make(chan struct{}), origin: origin, candidate: candidate, pending: pending, verifier: verifier, open: open, cfg: cfg,
		request: loginStartRequest{Name: cfg.Remote.DisplayName, PublicKey: public, Version: h.version, Candidate: candidate, Previous: previous, Challenge: pkceChallenge(verifier)}}
	h.id = id
	old := h.cancelLoginLocked()
	stopped := h.stopLocked()
	h.login = LoginStatus{State: "starting"}
	h.loginActive = attempt
	h.mu.Unlock()
	go h.runLogin(attempt)
	finishLogin(old)
	stopped.finish()
	return nil
}

func (h *Host) cancelLoginLocked() *loginAttempt {
	old := h.loginActive
	h.loginActive = nil
	if old != nil {
		old.cancel()
	}
	return old
}
func finishLogin(attempt *loginAttempt) {
	if attempt != nil {
		<-attempt.done
	}
}

func (h *Host) CancelLogin() {
	h.mu.Lock()
	previous := h.cancelLoginLocked()
	h.login = LoginStatus{State: "cancelled"}
	h.mu.Unlock()
	finishLogin(previous)
}

func (h *Host) loginMatchesLocked(attempt *loginAttempt) bool {
	cfg := h.cfg.Snapshot()
	origin, err := config.RemoteOrigin(cfg.Remote.HubURL)
	return h.loginActive == attempt && attempt.ctx.Err() == nil && err == nil && origin == attempt.origin && cfg.Remote.Enabled && cfg.Remote.AuthMode == config.RemoteAuthNOSEC
}

func (h *Host) publishLogin(attempt *loginAttempt, status LoginStatus) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.loginMatchesLocked(attempt) {
		return false
	}
	h.login = status
	return true
}

func (h *Host) runLogin(attempt *loginAttempt) {
	var connect *hostStart
	launch := false
	defer func() {
		attempt.cancel()
		h.mu.Lock()
		if h.loginActive == attempt {
			if errors.Is(attempt.ctx.Err(), context.DeadlineExceeded) {
				h.login = LoginStatus{State: "expired"}
			}
			h.loginActive = nil
		}
		h.mu.Unlock()
		close(attempt.done)
		if connect != nil && launch {
			go h.connect(connect)
		}
	}()
	client := newLoginClient()
	var started loginStartResponse
	if err := loginHTTP(attempt.ctx, client, attempt.origin+desktopAuthPrefix+"start", attempt.request, &started, http.StatusCreated); err != nil {
		var response *loginHTTPError
		if errors.As(err, &response) && response.code == http.StatusConflict {
			h.mu.Lock()
			if h.loginMatchesLocked(attempt) {
				code := "device_conflict"
				if err := attempt.cfg.SetPendingRemoteRejected(attempt.pending, true); err != nil {
					code = "credential_unavailable"
				}
				h.login = LoginStatus{State: "error", ErrorCode: code}
			}
			h.mu.Unlock()
		} else {
			h.loginFailure(attempt, err)
		}
		return
	}
	if err := verifyLoginResponse(attempt.origin, started); err != nil {
		h.publishLogin(attempt, LoginStatus{State: "error", ErrorCode: "unsafe_response"})
		return
	}
	if !h.publishLogin(attempt, LoginStatus{State: "authorization_pending", UserCode: started.UserCode}) {
		return
	}
	if err := attempt.open(started.VerificationURI); err != nil {
		h.publishLogin(attempt, LoginStatus{State: "error", ErrorCode: "browser_unavailable"})
		return
	}
	wait := waitLogin
	if h.loginWait != nil {
		wait = h.loginWait
	}
	delay := loginInterval
	for polls := 0; polls < 120; polls++ {
		if err := wait(attempt.ctx, delay); err != nil {
			return
		}
		if time.Now().After(started.ExpiresAt) {
			h.publishLogin(attempt, LoginStatus{State: "expired"})
			return
		}
		var polled loginPollResponse
		err := loginHTTP(attempt.ctx, client, attempt.origin+desktopAuthPrefix+"poll", map[string]string{"device_code": started.DeviceCode, "code_verifier": attempt.verifier}, &polled, http.StatusOK)
		if err != nil {
			var httpErr *loginHTTPError
			if errors.As(err, &httpErr) && httpErr.code == http.StatusTooManyRequests {
				delay = httpErr.retry
				continue
			}
			if errors.As(err, &httpErr) && httpErr.code >= 400 && httpErr.code < 500 {
				if httpErr.code == http.StatusUnauthorized || httpErr.code == http.StatusForbidden {
					h.publishLogin(attempt, LoginStatus{State: "access_denied"})
				} else {
					h.loginFailure(attempt, err)
				}
				return
			}
			if attempt.ctx.Err() != nil {
				return
			}
			// The same proof can reconcile a response lost after authorization. Keep
			// the private candidate for an explicit retry after this flow expires.
			if !h.publishLogin(attempt, LoginStatus{State: "authorization_pending", UserCode: started.UserCode, ErrorCode: "network_unavailable"}) {
				return
			}
			delay = loginInterval
			continue
		}
		delay = loginInterval
		switch polled.Status {
		case "authorization_pending", "account_pending", "account_rejected":
			if !h.publishLogin(attempt, LoginStatus{State: polled.Status, UserCode: started.UserCode}) {
				return
			}
		case "access_denied", "expired":
			h.publishLogin(attempt, LoginStatus{State: polled.Status})
			return
		case "authorized":
			if polled.User == nil || !resourceID(polled.HostID) || !resourceID(polled.User.ID) || strings.TrimSpace(polled.User.Username) == "" || len(polled.User.Username) > 1024 {
				h.publishLogin(attempt, LoginStatus{State: "error", ErrorCode: "unsafe_response"})
				return
			}
			h.mu.Lock()
			if !h.loginMatchesLocked(attempt) {
				h.mu.Unlock()
				return
			}
			receipt := config.RemoteCredential{Token: attempt.candidate, Origin: attempt.origin, Authorized: true, HostID: polled.HostID, UserID: polled.User.ID, Username: polled.User.Username}
			if err := h.cfg.ActivateRemoteTokenIfCurrent(attempt.pending, receipt); err != nil {
				h.login = LoginStatus{State: "error", ErrorCode: "credential_unavailable"}
				h.mu.Unlock()
				return
			}
			if err := attempt.cfg.CompleteRemoteTokenActivation(attempt.pending, attempt.origin, attempt.request.PublicKey); err != nil {
				h.login = LoginStatus{State: "error", ErrorCode: "credential_unavailable"}
				h.mu.Unlock()
				return
			}
			// Ownership is released before preparing pairlink. Reload/Stop therefore
			// join the pending connection, never this login task waiting on itself.
			h.login = LoginStatus{State: "authorized", HostID: polled.HostID, UserID: polled.User.ID, Username: polled.User.Username}
			h.loginActive = nil
			connect, launch = h.prepareStartLocked(context.Background())
			h.mu.Unlock()
			if h.afterLoginActivation != nil {
				h.afterLoginActivation()
			}
			return
		default:
			h.publishLogin(attempt, LoginStatus{State: "error", ErrorCode: "unsafe_response"})
			return
		}
	}
	h.publishLogin(attempt, LoginStatus{State: "expired"})
}

func (h *Host) loginFailure(attempt *loginAttempt, err error) {
	code := "network_unavailable"
	var response *loginHTTPError
	if errors.As(err, &response) {
		switch response.code {
		case 409:
			code = "device_conflict"
		case 401, 403:
			code = "access_denied"
		default:
			code = "gateway_unavailable"
		}
	}
	h.publishLogin(attempt, LoginStatus{State: "error", ErrorCode: code})
}

func (h *Host) loginStatusLocked(cfg *config.Config) *LoginStatus {
	if cfg.Remote.AuthMode != config.RemoteAuthNOSEC {
		return nil
	}
	current := h.login
	if current.State == "" {
		current.State = "signed_out"
		credential, err := cfg.ReadRemoteCredential()
		origin, originErr := config.RemoteOrigin(cfg.Remote.HubURL)
		if err == nil && originErr == nil && credential.Origin == origin && credential.Authorized {
			current = LoginStatus{State: "authorized", HostID: credential.HostID, UserID: credential.UserID, Username: credential.Username}
		}
	}
	if h.id != nil {
		current.HostFP = h.id.Fingerprint()
	}
	return &current
}
