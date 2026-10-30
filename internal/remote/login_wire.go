package remote

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const desktopAuthPrefix = "/admin/api/v1/auth/desktop/"
const desktopVerificationPath = "/admin/api/v1/auth/sso/nosec/start"
const loginLifetime = 10 * time.Minute
const loginInterval = 5 * time.Second

type loginStartRequest struct {
	Name      string `json:"name"`
	PublicKey string `json:"host_pub"`
	Version   string `json:"version"`
	Candidate string `json:"candidate_token"`
	Previous  string `json:"previous_token,omitempty"`
	Challenge string `json:"code_challenge"`
}

type loginStartResponse struct {
	DeviceCode      string    `json:"device_code"`
	UserCode        string    `json:"user_code"`
	VerificationURI string    `json:"verification_uri"`
	ExpiresAt       time.Time `json:"expires_at"`
	Interval        int       `json:"interval"`
}

type loginPollResponse struct {
	Status string `json:"status"`
	HostID string `json:"host_id"`
	User   *struct {
		ID       string `json:"id"`
		Username string `json:"username"`
	} `json:"user"`
	Approval string `json:"approval_status"`
}

type loginHTTPError struct {
	code  int
	retry time.Duration
}

func (e *loginHTTPError) Error() string { return "remote: gateway login request failed" }

func loginHTTP(ctx context.Context, client *http.Client, target string, request, result any, expectedStatus int) error {
	raw, err := json.Marshal(request)
	if err != nil {
		return errors.New("remote: cannot encode login request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return errors.New("remote: invalid gateway login URL")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return errors.New("remote: gateway login transport unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != expectedStatus {
		retry := loginInterval
		if n, e := strconv.Atoi(response.Header.Get("Retry-After")); e == nil && n >= 5 && n <= 60 {
			retry = time.Duration(n) * time.Second
		}
		return &loginHTTPError{code: response.StatusCode, retry: retry}
	}
	raw, err = io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return errors.New("remote: gateway login response unavailable")
	}
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || len(envelope.Data) == 0 || json.Unmarshal(envelope.Data, result) != nil {
		return errors.New("remote: invalid gateway login response")
	}
	return nil
}

func newLoginClient() *http.Client {
	return &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return errors.New("remote: gateway redirects are prohibited")
	}}
}

func randomLoginValue() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errors.New("remote: secure random source unavailable")
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}
func newCandidateToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", errors.New("remote: secure random source unavailable")
	}
	return hex.EncodeToString(raw[:]), nil
}
func tokenShape(token string) bool {
	raw, err := hex.DecodeString(token)
	return err == nil && len(raw) == 32 && token == strings.ToLower(token)
}
func storedTokenShape(token string) bool {
	return token != "" && len(token) <= 256 && strings.TrimSpace(token) == token && !strings.ContainsAny(token, "\r\n\x00")
}

func resourceID(id string) bool {
	raw, err := hex.DecodeString(id)
	return err == nil && len(raw) == 16
}

func verifyLoginResponse(origin string, response loginStartResponse) error {
	u, err := url.Parse(response.VerificationURI)
	if err != nil || u.User != nil || u.Fragment != "" || u.Path != desktopVerificationPath || u.RawPath != "" {
		return errors.New("remote: unsafe gateway verification URL")
	}
	if u.Scheme+"://"+u.Host != origin || len(u.Query()) != 1 || u.Query().Get("desktop_code") != response.UserCode || len(u.Query()["desktop_code"]) != 1 {
		return errors.New("remote: gateway verification origin or code mismatch")
	}
	if len(response.DeviceCode) < 32 || len(response.DeviceCode) > 256 || response.UserCode == "" || len(response.UserCode) > 128 || strings.ContainsAny(response.UserCode, "\r\n\x00") || response.Interval != 5 || !response.ExpiresAt.After(time.Now()) || response.ExpiresAt.After(time.Now().Add(loginLifetime+5*time.Second)) {
		return errors.New("remote: invalid gateway authorization lifetime")
	}
	return nil
}
func pkceChallenge(verifier string) string {
	digest := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(digest[:])
}
func waitLogin(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
