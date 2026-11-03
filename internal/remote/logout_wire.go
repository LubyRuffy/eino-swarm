package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

var (
	errLogoutDenied   = errors.New("remote: Gateway refused device logout; retry with the current device credential")
	errLogoutNetwork  = errors.New("remote: Gateway is unreachable; the device credential was retained for retry")
	errLogoutGateway  = errors.New("remote: Gateway could not confirm device logout; retry")
	errLogoutResponse = errors.New("remote: Gateway returned an invalid logout acknowledgement")
)

func logoutCredential(ctx context.Context, client *http.Client, origin, token string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+desktopAuthPrefix+"logout", bytes.NewBufferString("{}"))
	if err != nil {
		return errLogoutNetwork
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(req)
	if err != nil {
		return errLogoutNetwork
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusNotFound, http.StatusMethodNotAllowed:
		return ErrGatewayLogoutUnsupported
	case http.StatusUnauthorized, http.StatusForbidden:
		return errLogoutDenied
	case http.StatusOK:
	default:
		return errLogoutGateway
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (64<<10)+1))
	if err != nil || len(raw) > 64<<10 {
		return errLogoutResponse
	}
	var envelope struct {
		Data struct {
			LoggedOut bool `json:"logged_out"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &envelope) != nil || !envelope.Data.LoggedOut {
		return errLogoutResponse
	}
	return nil
}

func logoutErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrGatewayLogoutUnsupported):
		return "logout_unsupported"
	case errors.Is(err, errLogoutDenied):
		return "logout_denied"
	case errors.Is(err, errLogoutNetwork):
		return "network_unavailable"
	case errors.Is(err, errLogoutGateway):
		return "gateway_unavailable"
	case errors.Is(err, errLogoutResponse):
		return "unsafe_response"
	default:
		return "credential_unavailable"
	}
}
