package provider

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

const (
	modelRequestLimitHeader      = "X-Zwai-Model-Request-Limit"
	modelRequestsRemainingHeader = "X-Zwai-Model-Requests-Remaining"
)

type modelRequestBudgetKey struct {
	providerID string
	baseURL    string
}

func requestBudgetKey(prov config.Provider) modelRequestBudgetKey {
	return modelRequestBudgetKey{prov.ID, strings.TrimSpace(prov.BaseURL)}
}

type modelRequestBudget struct {
	endpoint  *url.URL
	mu        sync.Mutex
	limit     int
	remaining int
}

// ModelRequestBudget returns the lowest remaining count observed for the
// configured provider route since the clients were last invalidated. Only a
// provider that explicitly sends both admission headers supplies this budget.
func (p *Pool) ModelRequestBudget(providerID string) (limit, remaining int, ok bool) {
	prov, err := p.Resolve(providerID)
	if err != nil {
		return 0, 0, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	budget := p.requestBudgets[requestBudgetKey(prov)]
	if budget == nil {
		return 0, 0, false
	}
	budget.mu.Lock()
	defer budget.mu.Unlock()
	return budget.limit, budget.remaining, budget.limit != 0
}

// requestBudgetLocked shares a budget across all model clients using the
// same provider. The caller holds Pool.mu while building a model.
func (p *Pool) requestBudgetLocked(prov config.Provider) *modelRequestBudget {
	key := requestBudgetKey(prov)
	budget := p.requestBudgets[key]
	if budget != nil {
		return budget
	}
	endpoint, err := url.Parse(key.baseURL)
	if err != nil || endpoint.Host == "" {
		return nil
	}
	budget = &modelRequestBudget{endpoint: endpoint}
	if p.requestBudgets == nil {
		p.requestBudgets = make(map[modelRequestBudgetKey]*modelRequestBudget)
	}
	p.requestBudgets[key] = budget
	return budget
}

func (b *modelRequestBudget) observe(req *http.Request, headers http.Header) {
	if req.Method != http.MethodPost || !strings.EqualFold(req.URL.Host, b.endpoint.Host) || !strings.EqualFold(req.URL.Scheme, b.endpoint.Scheme) {
		// A redirected provider request can reach a different origin.
		return
	}
	limit, limitOK := modelRequestBudgetHeader(headers, modelRequestLimitHeader)
	remaining, remainingOK := modelRequestBudgetHeader(headers, modelRequestsRemainingHeader)
	if !limitOK || !remainingOK || limit == 0 || remaining > limit {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.limit == 0 || (b.limit == limit && remaining < b.remaining) {
		// Responses can arrive out of order. A later header cannot restore
		// already-used requests or silently change the route's limit.
		b.limit, b.remaining = limit, remaining
	}
}

func modelRequestBudgetHeader(headers http.Header, name string) (int, bool) {
	values := headers.Values(name)
	if len(values) != 1 {
		return 0, false
	}
	value, err := strconv.ParseUint(values[0], 10, 31)
	return int(value), err == nil
}
