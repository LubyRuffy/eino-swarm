// Package speech makes independent, cancelable OpenAI-compatible voice calls.
// It never creates a turn, writes a transcript, or persists audio.
package speech

import (
	"errors"
	"net/url"
	"reflect"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/audioparams"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

// InputView is safe to send to a paired phone; it carries no endpoint credential.
type InputView struct {
	Audio              *audioparams.View `json:"audio,omitempty"`
	SourceID           string            `json:"source_id,omitempty"`
	Execution          string            `json:"execution"`
	GatewayOrigin      string            `json:"gateway_origin,omitempty"`
	UserDisabled       *bool             `json:"user_disabled,omitempty"`
	Enabled            bool              `json:"enabled"`
	Ready              bool              `json:"ready"`
	Reason             string            `json:"reason,omitempty"`
	Model              string            `json:"model"`
	Label              string            `json:"label"`
	Language           string            `json:"language"`
	Prompt             string            `json:"prompt"`
	DeviceID           string            `json:"device_id,omitempty"`
	RecordingFormat    string            `json:"recording_format"`
	MaxDurationSeconds int               `json:"max_duration_seconds"`
	TimeoutSeconds     int               `json:"timeout_seconds"`
}

// OutputView describes on-demand playback using the PC's selected service.
type OutputView struct {
	Audio          *audioparams.View `json:"audio,omitempty"`
	SourceID       string            `json:"source_id,omitempty"`
	Execution      string            `json:"execution"`
	GatewayOrigin  string            `json:"gateway_origin,omitempty"`
	UserDisabled   *bool             `json:"user_disabled,omitempty"`
	Enabled        bool              `json:"enabled"`
	Ready          bool              `json:"ready"`
	Reason         string            `json:"reason,omitempty"`
	Model          string            `json:"model"`
	Label          string            `json:"label"`
	Voice          string            `json:"voice"`
	Speed          float64           `json:"speed"`
	Format         string            `json:"format"`
	Instructions   string            `json:"instructions"`
	TimeoutSeconds int               `json:"timeout_seconds"`
}

// SnapshotView is the non-secret, PC-owned configuration used by every client.
// GatewayOrigin binds a phone's own session; it is not an arbitrary upload URL.
type SnapshotView struct {
	Execution     string              `json:"execution"`
	GatewayOrigin string              `json:"gateway_origin,omitempty"`
	Input         InputView           `json:"input"`
	Output        OutputView          `json:"output"`
	Limits        config.SpeechLimits `json:"limits"`
}

// Service reads the live configuration and account at each operation boundary.
type Service struct {
	cfg  *config.Config
	pool *provider.Pool
}

// New shares configuration and provider ownership without caching credentials.
func New(cfg *config.Config, pool *provider.Pool) *Service { return &Service{cfg: cfg, pool: pool} }

// Snapshot returns only client parameters and authoritative configured readiness.
func (s *Service) Snapshot() SnapshotView {
	cfg := s.cfg.Snapshot()
	input, output := cfg.Speech.Input, cfg.Speech.Output
	v := SnapshotView{Execution: "pc_local", Limits: cfg.Speech.Limits,
		Input:  InputView{Execution: "pc_local", DeviceID: input.DeviceID, RecordingFormat: input.RecordingFormat, MaxDurationSeconds: input.MaxDurationSeconds, TimeoutSeconds: input.TimeoutSeconds},
		Output: OutputView{Execution: "pc_local", TimeoutSeconds: output.TimeoutSeconds},
	}
	if s.pool != nil && s.pool.OfficialOnly() {
		v.Execution = "gateway"
	}
	inputDisabled := !input.Enabled && input.GatewayDisabled
	outputDisabled := !output.Enabled && output.GatewayDisabled
	v.Input.UserDisabled, v.Output.UserDisabled = &inputDisabled, &outputDisabled
	for _, isInput := range []bool{true, false} {
		selection, err := s.selectTarget(cfg, isInput)
		ready, reason := err == nil, ""
		if err != nil {
			reason = errorCode(err)
		}
		if isInput {
			v.Input.Enabled = selection.enabled
			v.Input.Audio = selection.audio
			v.Input.Language, v.Input.Prompt = selection.settings.Input.Language, selection.settings.Input.Prompt
			v.Input.SourceID = selection.target.ID
			v.Input.Model = selection.target.Model
			v.Input.Ready, v.Input.Reason, v.Input.Label = ready, reason, selection.target.Label
			if gatewayOrigin := origin(selection.target.BaseURL); strings.HasPrefix(selection.target.ID, config.AccountSourcePrefix) && gatewayOrigin != "" {
				v.Input.Execution, v.Input.GatewayOrigin = "gateway", gatewayOrigin
			}
		} else {
			v.Output.Enabled = selection.enabled
			v.Output.Audio = selection.audio
			v.Output.Voice, v.Output.Speed, v.Output.Format, v.Output.Instructions = selection.settings.Output.Voice, selection.settings.Output.Speed, selection.settings.Output.Format, selection.settings.Output.Instructions
			v.Output.SourceID = selection.target.ID
			v.Output.Model = selection.target.Model
			v.Output.Ready, v.Output.Reason, v.Output.Label = ready, reason, selection.target.Label
			if gatewayOrigin := origin(selection.target.BaseURL); strings.HasPrefix(selection.target.ID, config.AccountSourcePrefix) && gatewayOrigin != "" {
				v.Output.Execution, v.Output.GatewayOrigin = "gateway", gatewayOrigin
			}
		}
		if selection.settings.Limits.InputBytes < v.Limits.InputBytes {
			v.Limits.InputBytes = selection.settings.Limits.InputBytes
		}
		if selection.settings.Limits.ChunkChars < v.Limits.ChunkChars {
			v.Limits.ChunkChars = selection.settings.Limits.ChunkChars
		}
		if v.Execution == "gateway" && selection.target.BaseURL != "" {
			v.GatewayOrigin = origin(selection.target.BaseURL)
		}
	}
	return v
}

type selection struct {
	enabled    bool
	audio      *audioparams.View
	parameters map[string]any
	settings   config.SpeechConfig
	product    string
	target     config.Provider
	timeout    int
	isInput    bool
}

func (s *Service) selectTarget(cfg *config.Config, isInput bool) (selection, error) {
	selected, err := s.endpointTarget(cfg, isInput)
	if err == nil {
		err = s.resolveAudio(&selected)
	}
	selected.enabled = cfg.Speech.Output.Enabled
	gatewayDisabled := cfg.Speech.Output.GatewayDisabled
	if isInput {
		selected.enabled = cfg.Speech.Input.Enabled
		gatewayDisabled = cfg.Speech.Input.GatewayDisabled
	}
	if cfg.Product.FOFA() && strings.HasPrefix(selected.target.ID, config.AccountSourcePrefix) {
		selected.enabled = selected.target.Model != "" && origin(selected.target.BaseURL) != "" && !gatewayDisabled
	}
	if !selected.enabled {
		return selected, failure("speech_disabled", 403)
	}
	if err != nil {
		return selected, err
	}
	if selected.target.Model == "" {
		return selected, failure("speech_not_configured", 400)
	}
	return selected, nil
}

func (s *Service) endpointTarget(cfg *config.Config, isInput bool) (selection, error) {
	endpoint, role, timeout := cfg.Speech.Output.SpeechEndpoint, "speech", cfg.Speech.Output.TimeoutSeconds
	if isInput {
		endpoint, role, timeout = cfg.Speech.Input.SpeechEndpoint, "transcription", cfg.Speech.Input.TimeoutSeconds
	}
	selected := selection{settings: cfg.Speech, product: cfg.Product.Resolved(), timeout: timeout, isInput: isInput}
	var target config.Provider
	if s.pool != nil {
		resolved, configured, err := s.pool.RoleProvider(role)
		if configured {
			if err != nil {
				return selected, failure("speech_role_unavailable", 403)
			}
			selected.target = resolved
			return selected, nil
		}
	}
	if cfg.Product.FOFA() && (s.pool == nil || !s.pool.ModelPolicy().Ready) {
		if s.pool == nil {
			return selected, failure("speech_login_required", 401)
		}
		var err error
		target, _, err = s.pool.AccountAudio(role)
		if errors.Is(err, provider.ErrAccountLoginRequired) {
			return selected, failure("speech_login_required", 401)
		}
		if err != nil {
			return selected, failure("speech_role_unavailable", 403)
		}
	} else {
		switch endpoint.Source {
		case config.SpeechSourceProvider:
			var ok bool
			target, ok = cfg.Provider(endpoint.ProviderID)
			if !ok || !target.Listed() {
				return selected, failure("speech_not_configured", 400)
			}
		case config.SpeechSourceCustom:
			target = config.Provider{BaseURL: endpoint.BaseURL, APIKey: endpoint.APIKey, Model: strings.TrimSpace(endpoint.Model)}
		case config.SpeechSourceAccount:
			if s.pool == nil {
				return selected, failure("speech_login_required", 401)
			}
			resolved, _, err := s.pool.AccountAudio(role)
			if errors.Is(err, provider.ErrAccountLoginRequired) {
				return selected, failure("speech_login_required", 401)
			}
			if err != nil {
				return selected, failure("speech_role_unavailable", 403)
			}
			target = resolved
		default:
			return selected, failure("speech_not_configured", 400)
		}
		if endpoint.Source == config.SpeechSourceProvider {
			target.Model = strings.TrimSpace(endpoint.Model)
		}
	}
	if origin(target.BaseURL) == "" {
		return selected, failure("speech_not_configured", 400)
	}
	selected.target = target
	return selected, nil
}

func origin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func (s *Service) unchanged(old selection) bool {
	now, err := s.selectTarget(s.cfg.Snapshot(), old.isInput)
	return err == nil && reflect.DeepEqual(now.settings, old.settings) && reflect.DeepEqual(now.audio, old.audio) && now.product == old.product && now.target.ID == old.target.ID && now.target.BaseURL == old.target.BaseURL && now.target.APIKey == old.target.APIKey && now.target.Model == old.target.Model
}
