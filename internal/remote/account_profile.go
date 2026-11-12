package remote

import (
	"encoding/json"
	"strings"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
)

const accountProfileInstructionLimit = 8 << 10

// accountProfileState is the part of an official profile the UI may see.
// The instruction text is not included.
type accountProfileState struct {
	Presentation  string   `json:"presentation"`
	Instruction   bool     `json:"instruction"`
	RestrictTools bool     `json:"restrict_tools,omitempty"`
	Tools         []string `json:"tools,omitempty"`
}

func parseAccountProfile(raw json.RawMessage) (*provider.AccountProfile, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var body struct {
		Label         string          `json:"label"`
		Instruction   string          `json:"instruction"`
		Tools         json.RawMessage `json:"tools"`
		ContextWindow *int            `json:"context_window"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return nil, errAccountResponse
	}
	label := strings.TrimSpace(body.Label)
	if label == "" || utf8.RuneCountInString(label) > accountMaxLabelLen || !utf8.ValidString(label) {
		return nil, errAccountResponse
	}
	instruction := strings.TrimSpace(body.Instruction)
	if len(instruction) > accountProfileInstructionLimit || !utf8.ValidString(instruction) {
		return nil, errAccountResponse
	}
	window := 0
	if body.ContextWindow != nil {
		if *body.ContextWindow <= 0 {
			return nil, errAccountResponse
		}
		window = *body.ContextWindow
	}
	restrict, names, err := parseProfileTools(body.Tools)
	if err != nil {
		return nil, err
	}
	return &provider.AccountProfile{
		Label: label, Instruction: instruction, Tools: names, RestrictTools: restrict, ContextWindow: window,
	}, nil
}

func parseProfileTools(raw json.RawMessage) (bool, []string, error) {
	if len(raw) == 0 {
		return false, nil, nil
	}
	if string(raw) == "null" {
		return false, nil, errAccountResponse
	}
	var names []string
	if json.Unmarshal(raw, &names) != nil || names == nil {
		return false, nil, errAccountResponse
	}
	seen := make(map[string]bool, len(names))
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || !tools.KnownWorkspaceTool(name) {
			return false, nil, errAccountResponse
		}
		seen[name] = true
		out = append(out, name)
	}
	return true, out, nil
}

func profileState(profile provider.AccountProfile, presentation string) accountProfileState {
	state := accountProfileState{
		Presentation:  presentation,
		Instruction:   profile.Instruction != "",
		RestrictTools: profile.RestrictTools,
	}
	if profile.RestrictTools {
		state.Tools = append([]string(nil), profile.Tools...)
	}
	return state
}
