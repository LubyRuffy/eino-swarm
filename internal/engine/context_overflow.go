package engine

import (
	"regexp"
	"strconv"
	"strings"
)

// A context-length rejection names the ceiling when the provider feels like
// it. The number after "maximum" is the window; "requested N" is the call
// that already did not fit, so it is not the window.
var contextLimitPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)maximum context length is (\d+)`),
	regexp.MustCompile(`(?i)>\s*(\d+)\s+maximum`),
	regexp.MustCompile(`(?i)context[_ ]length(?: of| is|:)?\s*(\d+)`),
}

func isContextOverflow(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	if strings.Contains(s, "context canceled") || strings.Contains(s, "context deadline") {
		return false
	}
	switch {
	case strings.Contains(s, "maximum context length"),
		strings.Contains(s, "context length exceeded"),
		strings.Contains(s, "context_length_exceeded"),
		strings.Contains(s, "prompt is too long"),
		strings.Contains(s, "input is too long"),
		strings.Contains(s, "too many tokens"):
		return true
	default:
		return false
	}
}

func contextLimitFromError(err error) int {
	if err == nil {
		return 0
	}
	for _, re := range contextLimitPatterns {
		m := re.FindStringSubmatch(err.Error())
		if len(m) != 2 {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// compactThreshold is the prompt-token line for this conversation's model.
// The lookup is live: a context-length rejection may have just lowered the
// window, and the retry must compact against that ceiling.
func (e *Engine) compactThreshold(threadID string) int {
	window := 0
	if th, err := e.store.GetThread(threadID); err == nil {
		window = e.pool.WindowFor(th.ProviderID, th.Model)
	}
	return e.cfg.Swarm.CompactTrigger(window)
}

// learnContextCeiling records a smaller window for this model after a
// context-length rejection. A stated maximum wins; otherwise the prompt
// that was rejected is an upper bound. An existing window is never raised.
// False means the retry would send the same oversized prompt.
func (e *Engine) learnContextCeiling(providerID, model string, err error, promptTokens int) bool {
	ceiling := contextLimitFromError(err)
	if ceiling <= 0 {
		ceiling = promptTokens
	}
	if ceiling <= 0 {
		return false
	}
	return e.lowerModelWindow(providerID, model, ceiling)
}

func (e *Engine) lowerModelWindow(providerID, model string, ceiling int) bool {
	if e == nil || e.cfg == nil || e.pool == nil || ceiling <= 0 {
		return false
	}
	if current := e.pool.WindowFor(providerID, model); current > 0 && ceiling >= current {
		return false
	}
	idx := -1
	for i := range e.cfg.Models.Providers {
		if e.cfg.Models.Providers[i].ID == providerID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return false
	}
	p := &e.cfg.Models.Providers[idx]
	name := strings.TrimSpace(model)
	if name == "" {
		name = strings.TrimSpace(p.Model)
	}
	// WindowFor already refused a ceiling that is not strictly lower.
	// The write is the same number the next call will read back.
	if name != "" {
		if p.ModelContext == nil {
			p.ModelContext = map[string]int{}
		}
		p.ModelContext[name] = ceiling
	} else {
		p.ContextWindow = ceiling
	}
	if err := e.cfg.Save(); err != nil {
		e.log.Warn("could not store the learned context window", "model", name, "err", err)
	}
	return true
}
