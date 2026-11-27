package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

const (
	translationChatPath = "/translations"
	namingChatPath      = "/naming"
	maxFixedChatBytes   = 1 << 20
)

type fixedChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type fixedChatRequest struct {
	Model     string             `json:"model"`
	Messages  []fixedChatMessage `json:"messages"`
	Stream    bool               `json:"stream"`
	MaxTokens int                `json:"max_tokens,omitempty"`
}

// CompleteFixedChat posts one non-streaming Chat Completions body to the
// desktop translation or naming route. title uses /naming. The role id is the model. The response
// body stays out of the error.
func (p *Pool) CompleteFixedChat(ctx context.Context, providerID, role, system, user string, maxTokens int) (string, error) {
	if source := p.accountForExecution(providerID); source != nil && source.negotiated {
		var prov config.Provider
		var err error
		if role == "title" {
			prov, err = p.resolveRoleProvider(role, config.ModelRef{ProviderID: source.id, Model: source.titleModelName()})
		} else {
			prov, _, err = p.RoleProvider(role)
		}
		if err != nil {
			return "", err
		}
		release, err := p.beginChatAdmission()
		if err != nil {
			return "", err
		}
		chat, err := p.build(ctx, prov)
		release()
		if err != nil {
			return "", err
		}
		options := []model.Option{}
		if maxTokens > 0 {
			options = append(options, model.WithMaxTokens(maxTokens))
		}
		chat = p.guardCapability(p.trackedModel(chat, prov), prov.ID, role)
		out, err := chat.Generate(ctx, []*schema.Message{schema.SystemMessage(system), schema.UserMessage(user)}, options...)
		if err != nil {
			return "", err
		}
		if out == nil {
			return "", fmt.Errorf("provider: role returned no text")
		}
		return strings.TrimSpace(out.Content), nil
	}
	path, ok := fixedChatPath(role)
	if !ok {
		return "", fmt.Errorf("provider: unsupported fixed chat role")
	}
	release, err := p.BeginModelUse()
	if err != nil {
		return "", err
	}
	defer release()
	modelName := strings.TrimSpace(role)
	if err := p.validateModelSelection(providerID, modelName); err != nil {
		return "", err
	}
	prov, err := p.Resolve(providerID)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(fixedChatRequest{
		Model: modelName,
		Messages: []fixedChatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		MaxTokens: maxTokens,
	})
	if err != nil {
		return "", fmt.Errorf("provider: encode fixed chat role: %w", err)
	}
	base := strings.TrimRight(strings.TrimSpace(prov.BaseURL), "/")
	if base == "" {
		return "", fmt.Errorf("provider: %q has no base_url", prov.ID)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("provider: fixed chat role: %w", err)
	}
	if key := strings.TrimSpace(prov.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: prov.Timeout()}).Do(req)
	if err != nil {
		return "", fmt.Errorf("provider: fixed chat role: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxFixedChatBytes))
	if err != nil {
		return "", fmt.Errorf("provider: fixed chat role: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("provider: fixed chat role failed (%s)", resp.Status)
	}
	return fixedChatContent(raw)
}

func fixedChatPath(role string) (string, bool) {
	switch strings.TrimSpace(role) {
	case "translation":
		return translationChatPath, true
	case "title":
		return namingChatPath, true
	default:
		return "", false
	}
}

func fixedChatContent(raw []byte) (string, error) {
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Choices) == 0 {
		return "", fmt.Errorf("provider: fixed chat role returned no text")
	}
	return strings.TrimSpace(parsed.Choices[0].Message.Content), nil
}
