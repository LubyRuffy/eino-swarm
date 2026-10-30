package remote

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/LubyRuffy/pairlink/client"
)

// The library's convenience REST helpers use the global redirect policy and
// include response bodies in errors. Account-bound credentials require a
// no-redirect client and errors that cannot repeat a remote server's secrets.
func privateBindings(ctx context.Context, origin, token string) ([]client.BindingView, error) {
	var response struct {
		Bindings []client.BindingView `json:"bindings"`
	}
	if err := privatePairlinkRequest(ctx, http.MethodGet, strings.TrimRight(origin, "/")+"/pairlink/v1/bindings", token, &response); err != nil {
		return nil, err
	}
	if response.Bindings == nil {
		response.Bindings = []client.BindingView{}
	}
	return response.Bindings, nil
}
func privateRevokeBinding(ctx context.Context, origin, token, id string) error {
	return privatePairlinkRequest(ctx, http.MethodPost, strings.TrimRight(origin, "/")+"/pairlink/v1/bindings/"+url.PathEscape(id)+"/revoke", token, nil)
}
func privatePairlinkRequest(ctx context.Context, method, target, token string, out any) error {
	request, err := http.NewRequestWithContext(ctx, method, target, strings.NewReader("{}"))
	if err != nil {
		return errors.New("remote: invalid binding request")
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Content-Type", "application/json")
	response, err := newLoginClient().Do(request)
	if err != nil {
		return errors.New("remote: binding request unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return errors.New("remote: binding request denied")
	}
	if out == nil {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil || len(raw) > 1<<20 || json.Unmarshal(raw, out) != nil {
		return errors.New("remote: invalid binding response")
	}
	return nil
}
