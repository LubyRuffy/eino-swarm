package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const cloudWebResponseLimit = 16 << 20

var cloudErrorCode = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type accountWebTool struct {
	cfg       *config.Config
	local     tool.InvokableTool
	name      string
	execution string
}

func (t *accountWebTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return t.local.Info(ctx)
}

func (t *accountWebTool) InvokableRun(ctx context.Context, args string, opts ...tool.Option) (string, error) {
	cfg := t.cfg.Snapshot()
	if t.execution != "" {
		cfg.Tools.WebExecution = t.execution
	}
	origin, token, cloud, err := accountWebRoute(cfg)
	if err == nil && !cloud {
		return t.local.InvokableRun(ctx, args, opts...)
	}
	var out string
	if err == nil {
		out, err = t.runCloud(ctx, cfg, origin, token, args)
	}
	if err != nil && ctx.Err() == nil {
		// Workspace tools return ordinary failures to the model as tool text.
		// A denied cloud request never falls through to the local executor.
		return "error: " + err.Error(), nil
	}
	return out, err
}

func accountWebRoute(cfg *config.Config) (origin, token string, cloud bool, err error) {
	mode := cfg.Tools.WebExecution
	if mode == "" {
		mode = config.WebExecutionAuto
	}
	if mode == config.WebExecutionLocal {
		return "", "", false, nil
	}
	if mode != config.WebExecutionAuto && mode != config.WebExecutionCloud {
		return "", "", false, errors.New("cloud web tools: invalid execution setting")
	}
	origin, token, err = cloudWebAccount(cfg)
	if errors.Is(err, errCloudLoginRequired) && mode == config.WebExecutionAuto {
		return "", "", false, nil
	}
	return origin, token, true, err
}

func cloudWebAccount(cfg *config.Config) (origin, token string, err error) {
	if cfg.Remote.AuthMode != config.RemoteAuthNOSEC {
		return "", "", cloudLoginRequired()
	}
	credential, readErr := cfg.ReadRemoteCredential()
	if readErr != nil {
		return "", "", errors.New("cloud web tools: private account credential is unavailable")
	}
	origin, err = config.RemoteOrigin(cfg.Remote.HubURL)
	if err != nil {
		return "", "", errors.New("cloud web tools: invalid server origin")
	}
	if !credential.Authorized || credential.Origin != origin || credential.HostID == "" || credential.UserID == "" {
		return "", "", cloudLoginRequired()
	}
	return origin, credential.Token, nil
}

var errCloudLoginRequired = errors.New("cloud web tools: sign in to the server again to enable cloud web tools")

func cloudLoginRequired() error {
	return errCloudLoginRequired
}

func (t *accountWebTool) runCloud(ctx context.Context, cfg *config.Config, origin, token, args string) (string, error) {
	var input struct {
		Query  string `json:"query"`
		URL    string `json:"url"`
		Render bool   `json:"render"`
	}
	if json.Unmarshal([]byte(args), &input) != nil {
		return "", errors.New("cloud web tools: invalid tool arguments")
	}
	var body any
	path := "/client/desktop/v1/web/fetch"
	if t.name == "web_search" {
		if strings.TrimSpace(input.Query) == "" {
			return "", errors.New("cloud web tools: query is required")
		}
		path = "/client/desktop/v1/web/search"
		body = map[string]any{"query": input.Query, "max_results": cfg.Tools.WebSearchMaxResults}
	} else {
		if strings.TrimSpace(input.URL) == "" {
			return "", errors.New("cloud web tools: URL is required")
		}
		request := map[string]any{"url": input.URL, "formats": []string{"markdown"}}
		if input.Render {
			request["actions"] = []map[string]any{{"type": "wait", "milliseconds": 1}}
		}
		body = request
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", errors.New("cloud web tools: cannot encode request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, origin+path, bytes.NewReader(raw))
	if err != nil {
		return "", errors.New("cloud web tools: invalid server request")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	client := &http.Client{Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("cloud web tools: server transport is unavailable")
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(response.Body, cloudWebResponseLimit+1))
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New("cloud web tools: server response is unavailable")
	}
	if len(raw) > cloudWebResponseLimit {
		return "", errors.New("cloud web tools: server response exceeds the size limit")
	}
	var envelope struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	decodeErr := json.Unmarshal(raw, &envelope)
	if response.StatusCode != http.StatusOK || !envelope.Success {
		if response.StatusCode == http.StatusUnauthorized {
			return "", cloudLoginRequired()
		}
		code := envelope.Error.Code
		if !cloudErrorCode.MatchString(code) || strings.Contains(code, token) {
			code = "request_failed"
		}
		return "", fmt.Errorf("cloud web tools: server rejected the request (HTTP %d, %s)", response.StatusCode, code)
	}
	if decodeErr != nil || len(envelope.Data) == 0 || bytes.Equal(envelope.Data, []byte("null")) {
		return "", errors.New("cloud web tools: invalid server response")
	}
	if t.name == "web_search" {
		return cloudSearchResult(envelope.Data)
	}
	var data struct {
		Markdown *string `json:"markdown"`
	}
	if json.Unmarshal(envelope.Data, &data) != nil || data.Markdown == nil {
		return "", errors.New("cloud web tools: invalid page response")
	}
	return *data.Markdown, nil
}

func cloudSearchResult(raw json.RawMessage) (string, error) {
	var data struct {
		Provider string `json:"provider"`
		Query    string `json:"query"`
		Results  []struct {
			Title       string `json:"title"`
			URL         string `json:"url"`
			Snippet     string `json:"snippet"`
			Position    int    `json:"position"`
			PublishedAt string `json:"published_at"`
		} `json:"results"`
	}
	if json.Unmarshal(raw, &data) != nil {
		return "", errors.New("cloud web tools: invalid search response")
	}
	results := make([]map[string]any, 0, len(data.Results))
	for _, result := range data.Results {
		row := map[string]any{"title": result.Title, "url": result.URL, "summary": result.Snippet, "position": result.Position}
		if result.PublishedAt != "" {
			row["published_at"] = result.PublishedAt
		}
		results = append(results, row)
	}
	out, err := json.Marshal(map[string]any{"provider": data.Provider, "query": data.Query, "results": results})
	if err != nil {
		return "", errors.New("cloud web tools: cannot encode search results")
	}
	return string(out), nil
}
