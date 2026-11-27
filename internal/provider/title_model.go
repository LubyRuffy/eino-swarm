package provider

import "github.com/LubyRuffy/eino-swarm/internal/config"

// AutomaticTitleModel follows the product's default source. A conversation's
// temporary choice and historical title overrides do not select the namer.
func (p *Pool) AutomaticTitleModel() (ref config.ModelRef, role string, fixed bool, err error) {
	if p.localExecution() || p.mock && p.activeAccount() == nil {
		prov, err := p.ResolveModel("", "")
		return config.ModelRef{ProviderID: prov.ID, Model: prov.Model}, "conversation", false, err
	}
	src := p.activeAccount()
	if src == nil {
		return ref, "", false, ErrAccountLoginRequired
	}
	if !src.allowsRole("title") {
		return ref, "", false, ErrModelPolicy
	}
	if on, _ := src.Role("title"); on && !src.negotiated {
		return config.ModelRef{ProviderID: src.id, Model: "title"}, "title", true, nil
	}
	return config.ModelRef{ProviderID: src.id, Model: src.titleModelName()}, "title", src.negotiated, nil
}

func (s *AccountSource) titleModelName() string {
	if name := s.defaults["title"]; name != "" {
		return name
	}
	return s.provider("").Model
}
