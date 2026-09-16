package store

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

// managerAgentID is who the conversation's context belongs to. Sub-agents,
// the namer and the reviewer have their own prompts; counting those would
// make the context meter jump to a number that is not this conversation.
const managerAgentID = "manager"

// TokenTotals is one bucket of billed tokens: a turn, a conversation, or
// whatever the caller summed.
type TokenTotals struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	CachedTokens     int `json:"cached_tokens"`
	ReasoningTokens  int `json:"reasoning_tokens"`
	TotalTokens      int `json:"total_tokens"`
	Calls            int `json:"calls"`
}

// UsageSnapshot is what the composer meter and a trace footer need: how full
// the manager's last prompt was, plus the billed totals for this turn and
// the whole conversation. ContextWindow is filled by the engine from the
// live config — the table does not know the model's limit.
type UsageSnapshot struct {
	ContextTokens int         `json:"context_tokens"`
	ContextWindow int         `json:"context_window"`
	Turn          TokenTotals `json:"turn"`
	Thread        TokenTotals `json:"thread"`
}

type llmCallSum struct {
	Prompt     int
	Completion int
	Cached     int
	Reasoning  int
	Total      int
	Calls      int
}

// SummarizeUsage rolls one conversation's model calls into the snapshot the
// UI shows. An empty turnID still fills the thread totals and the last
// manager prompt; the turn bucket stays zero.
func (s *Store) SummarizeUsage(threadID, turnID string) (UsageSnapshot, error) {
	var snap UsageSnapshot
	thread, err := s.sumLLMCalls("thread_id = ?", threadID)
	if err != nil {
		return snap, err
	}
	snap.Thread = thread
	if turnID != "" {
		turn, err := s.sumLLMCalls("turn_id = ?", turnID)
		if err != nil {
			return snap, err
		}
		snap.Turn = turn
	} else if last, err := s.lastLLMCallTurn(threadID); err != nil {
		return snap, err
	} else if last != "" {
		turn, err := s.sumLLMCalls("turn_id = ?", last)
		if err != nil {
			return snap, err
		}
		snap.Turn = turn
	}
	n, err := s.lastManagerPrompt(threadID)
	if err != nil {
		return snap, err
	}
	snap.ContextTokens = n
	return snap, nil
}

func (s *Store) sumLLMCalls(query string, args ...any) (TokenTotals, error) {
	var row llmCallSum
	err := s.db.Model(&LLMCall{}).
		Select(`COALESCE(SUM(prompt_tokens),0) as prompt,
			COALESCE(SUM(completion_tokens),0) as completion,
			COALESCE(SUM(cached_tokens),0) as cached,
			COALESCE(SUM(reasoning_tokens),0) as reasoning,
			COALESCE(SUM(total_tokens),0) as total,
			COUNT(*) as calls`).
		Where(query, args...).
		Scan(&row).Error
	if err != nil {
		return TokenTotals{}, fmt.Errorf("store: sum llm calls: %w", err)
	}
	total := row.Total
	if total == 0 {
		total = row.Prompt + row.Completion
	}
	return TokenTotals{
		PromptTokens:     row.Prompt,
		CompletionTokens: row.Completion,
		CachedTokens:     row.Cached,
		ReasoningTokens:  row.Reasoning,
		TotalTokens:      total,
		Calls:            row.Calls,
	}, nil
}

func (s *Store) lastManagerPrompt(threadID string) (int, error) {
	var rec LLMCall
	err := s.db.Where("thread_id = ? AND agent_id = ? AND prompt_tokens > 0", threadID, managerAgentID).
		Order("id desc").Limit(1).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("store: last manager prompt: %w", err)
	}
	return rec.PromptTokens, nil
}

func (s *Store) lastLLMCallTurn(threadID string) (string, error) {
	var rec LLMCall
	err := s.db.Where("thread_id = ?", threadID).Order("id desc").Limit(1).Take(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("store: last llm call: %w", err)
	}
	return rec.TurnID, nil
}
