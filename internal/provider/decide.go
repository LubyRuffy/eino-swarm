package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

const decisionsPath = "/decisions"

type decisionQuestion struct {
	Type         string `json:"type"`
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
}

type decisionRequest struct {
	Model     string             `json:"model"`
	Input     string             `json:"input"`
	Questions []decisionQuestion `json:"questions"`
}

// Decide calls the account decisions role. The question is fixed; the caller
// supplies only the conversation text.
func (p *Pool) Decide(ctx context.Context, providerID, modelName, input string) (string, error) {
	prov, err := p.Resolve(providerID)
	if err != nil {
		return "", err
	}
	body, err := json.Marshal(decisionRequest{
		Model: strings.TrimSpace(modelName),
		Input: input,
		Questions: []decisionQuestion{{
			Type: "predicate", Name: "proceed",
			Instructions: "Should the assistant proceed with the request as stated?",
		}},
	})
	if err != nil {
		return "", fmt.Errorf("provider: encode decisions: %w", err)
	}
	base := strings.TrimRight(strings.TrimSpace(prov.BaseURL), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+decisionsPath, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("provider: decisions: %w", err)
	}
	if key := strings.TrimSpace(prov.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	resp, err := (&http.Client{Timeout: prov.Timeout()}).Do(req)
	if err != nil {
		return "", fmt.Errorf("provider: decisions: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("provider: decisions: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("provider: decisions failed (%s)", resp.Status)
	}
	return formatDecision(raw)
}

func formatDecision(raw []byte) (string, error) {
	var parsed struct {
		Answers []struct {
			Name        string  `json:"name"`
			Probability float64 `json:"probability"`
		} `json:"answers"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil || len(parsed.Answers) == 0 {
		return "", fmt.Errorf("provider: decisions returned no answer")
	}
	answer := parsed.Answers[0]
	name := strings.TrimSpace(answer.Name)
	if name == "" {
		name = "proceed"
	}
	return fmt.Sprintf("%s %.2f", name, answer.Probability), nil
}
