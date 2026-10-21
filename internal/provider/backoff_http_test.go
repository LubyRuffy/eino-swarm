package provider

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestHeaderReconnectRetainsTheOriginalFailureWhenPayloadCannotBeReopened(t *testing.T) {
	ctx := context.WithValue(context.Background(), reconnectBackoffKey{}, func() error { return nil })
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://fixture.invalid", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	req.GetBody = func() (io.ReadCloser, error) { return nil, errors.New("payload unavailable") }
	transport := &idleTransport{base: responsesRoundTripper(func(*http.Request) (*http.Response, error) {
		return nil, backoffProviderTimeout{}
	}), firstByte: time.Second}
	resp, err := transport.RoundTrip(req)
	var timeout FirstByteTimeoutError
	if resp != nil || !errors.As(err, &timeout) || timeout.Wait != time.Second {
		t.Fatalf("unreplayable reconnect must retain its original timeout: response=%v err=%v", resp, err)
	}
}

func TestModelHTTPDoesNotSendAPartiallyReadPayload(t *testing.T) {
	boom := errors.New("request body failed")
	body := &backoffBrokenBody{err: boom}
	req, err := http.NewRequest(http.MethodPost, "http://fixture.invalid", body)
	if err != nil {
		t.Fatal(err)
	}
	transport := &idleTransport{base: responsesRoundTripper(func(*http.Request) (*http.Response, error) {
		t.Fatal("a partially read model payload was sent")
		return nil, nil
	})}
	resp, err := transport.RoundTrip(req)
	if resp != nil || !errors.Is(err, boom) || !body.closed {
		t.Fatalf("request read failure lost its cause or body ownership: response=%v err=%v closed=%v", resp, err, body.closed)
	}
}

type backoffBrokenBody struct {
	err    error
	closed bool
}

func (b *backoffBrokenBody) Read([]byte) (int, error) { return 0, b.err }
func (b *backoffBrokenBody) Close() error             { b.closed = true; return nil }
