package remote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDesktopLoginVerificationRejectsOtherOriginsPathsAndUnboundedLifetime(t *testing.T) {
	origin := "https://gateway.example.invalid"
	baseline := loginStartResponse{DeviceCode: strings.Repeat("a", 64), UserCode: "PUBLIC", VerificationURI: origin + desktopVerificationPath + "?desktop_code=PUBLIC", ExpiresAt: time.Now().Add(time.Minute), Interval: 5}
	if err := verifyLoginResponse(origin, baseline); err != nil {
		t.Fatal(err)
	}
	mutations := []func(*loginStartResponse){
		func(r *loginStartResponse) {
			r.VerificationURI = "https://other.example.invalid" + desktopVerificationPath + "?desktop_code=PUBLIC"
		},
		func(r *loginStartResponse) { r.VerificationURI = origin + "/unexpected?desktop_code=PUBLIC" },
		func(r *loginStartResponse) {
			r.VerificationURI = origin + desktopVerificationPath + "?desktop_code=PUBLIC&desktop_code=PUBLIC"
		},
		func(r *loginStartResponse) {
			r.VerificationURI = origin + desktopVerificationPath + "?desktop_code=OTHER"
		},
		func(r *loginStartResponse) {
			r.VerificationURI = origin + desktopVerificationPath + "?desktop_code=PUBLIC&token=private"
		},
		func(r *loginStartResponse) {
			r.VerificationURI = origin + desktopVerificationPath + "?desktop_code=PUBLIC#fragment"
		},
		func(r *loginStartResponse) { r.Interval = 1 },
		func(r *loginStartResponse) { r.DeviceCode = "short" },
		func(r *loginStartResponse) { r.UserCode = "PUBLIC\n" },
		func(r *loginStartResponse) { r.ExpiresAt = time.Now().Add(-time.Second) },
		func(r *loginStartResponse) { r.ExpiresAt = time.Now().Add(11 * time.Minute) },
	}
	for index, mutate := range mutations {
		candidate := baseline
		mutate(&candidate)
		if err := verifyLoginResponse(origin, candidate); err == nil {
			t.Errorf("unsafe verification variant %d accepted", index)
		}
	}
}

func TestDesktopLoginHTTPHonorsExactStatusRetryAndBoundedPrivateDecoding(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		wantOK bool
		retry  time.Duration
	}{
		{"created", 201, `{"data":{"status":"authorization_pending"}}`, true, 0},
		{"wrong-success-status", 200, `{"data":{}}`, false, 0},
		{"rate-limit", 429, `private upstream diagnostic`, false, 9 * time.Second},
		{"missing-envelope", 201, `{}`, false, 0},
		{"invalid-json", 201, `not-json`, false, 0},
		{"oversized", 201, strings.Repeat("x", (64<<10)+1), false, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Retry-After", "9")
				w.WriteHeader(test.status)
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			var response loginPollResponse
			err := loginHTTP(context.Background(), newLoginClient(), server.URL, map[string]string{"device_code": "private"}, &response, http.StatusCreated)
			if (err == nil) != test.wantOK {
				t.Fatalf("unexpected decoding result: %v", err)
			}
			if test.retry != 0 {
				var limited *loginHTTPError
				if !errors.As(err, &limited) || limited.retry != test.retry {
					t.Fatal("Retry-After not retained")
				}
			}
			if err != nil && strings.Contains(err.Error(), "private") {
				t.Fatal("upstream private diagnostic reached error")
			}
		})
	}
	var forwarded bool
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded = true }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer source.Close()
	var result json.RawMessage
	if err := loginHTTP(context.Background(), newLoginClient(), source.URL, map[string]string{"private": "credential"}, &result, 201); err == nil || forwarded {
		t.Fatal("private authorization POST followed redirect")
	}
}

func TestDesktopLoginProofAndWaitRespectCancellation(t *testing.T) {
	verifier, err := randomLoginValue()
	if err != nil || len(verifier) != 43 || len(pkceChallenge(verifier)) != 43 {
		t.Fatal("invalid PKCE proof")
	}
	token, err := newCandidateToken()
	if err != nil || !tokenShape(token) || tokenShape(strings.ToUpper(token)) || tokenShape("not-hex") {
		t.Fatal("invalid candidate shape")
	}
	if !resourceID(strings.Repeat("a", 32)) || resourceID("bad") {
		t.Fatal("invalid resource ID boundary")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitLogin(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation did not stop wait")
	}
	if err := waitLogin(context.Background(), time.Millisecond); err != nil {
		t.Fatal(err)
	}
}

func TestPrivatePairlinkBindingsUseBoundedSuccessAndFailureSemantics(t *testing.T) {
	for _, body := range []string{`{"bindings":[]}`, `{}`, `not-json`, strings.Repeat("x", (1<<20)+1)} {
		gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer private" {
				t.Error("missing private authentication")
			}
			_, _ = w.Write([]byte(body))
		}))
		bindings, err := privateBindings(context.Background(), gateway.URL+"/", "private")
		valid := strings.HasPrefix(body, "{")
		if (err == nil) != valid || valid && bindings == nil {
			t.Fatal("invalid bindings response handling")
		}
		if err := privateRevokeBinding(context.Background(), gateway.URL, "private", "binding"); err != nil {
			t.Fatal(err)
		}
		gateway.Close()
	}
	if err := privatePairlinkRequest(context.Background(), "GET", "http://[::1", "private", nil); err == nil {
		t.Fatal("invalid target accepted")
	}
	if err := loginHTTP(context.Background(), newLoginClient(), "http://[::1", map[string]string{}, nil, 201); err == nil {
		t.Fatal("invalid login target accepted")
	}
	if err := loginHTTP(context.Background(), newLoginClient(), "http://127.0.0.1", func() {}, nil, 201); err == nil {
		t.Fatal("unencodable login request accepted")
	}
}
