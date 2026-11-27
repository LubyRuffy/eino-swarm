package config

import (
	"errors"
	"fmt"
	"math"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/LubyRuffy/eino-swarm/internal/audioparams"
)

// ErrInvalidSpeech identifies invalid voice settings without inspecting prose.
var ErrInvalidSpeech = errors.New("invalid speech settings")

const (
	SpeechSourceAccount            = "account"
	SpeechSourceProvider           = "provider"
	SpeechSourceCustom             = "custom"
	DefaultSpeechDurationSeconds   = 120
	DefaultSpeechTimeoutSeconds    = 120
	DefaultSpeechInputBytes        = 25 << 20
	DefaultSpeechOutputBytes       = 32 << 20
	DefaultSpeechChunkChars        = 1000
	DefaultSpeechStagingTTLSeconds = 300
)

// SpeechEndpoint selects a voice service independently of conversation models.
// APIKey is private: settings expose only whether a credential is stored.
type SpeechEndpoint struct {
	Enabled    bool   `yaml:"enabled" json:"enabled"`
	Source     string `yaml:"source" json:"source"`
	ProviderID string `yaml:"provider_id,omitempty" json:"provider_id"`
	BaseURL    string `yaml:"base_url,omitempty" json:"base_url"`
	APIKey     string `yaml:"api_key,omitempty" json:"-"`
	Model      string `yaml:"model,omitempty" json:"model"`
}

// SpeechInputConfig controls recording and OpenAI-compatible transcription.
type SpeechInputConfig struct {
	SpeechEndpoint     `yaml:",inline"`
	GatewayDisabled    bool   `yaml:"gateway_disabled,omitempty" json:"gateway_disabled"`
	Language           string `yaml:"language,omitempty" json:"language"`
	Prompt             string `yaml:"prompt,omitempty" json:"prompt"`
	DeviceID           string `yaml:"device_id,omitempty" json:"device_id,omitempty"`
	RecordingFormat    string `yaml:"recording_format" json:"recording_format"`
	MaxDurationSeconds int    `yaml:"max_duration_seconds" json:"max_duration_seconds"`
	TimeoutSeconds     int    `yaml:"timeout_seconds" json:"timeout_seconds"`
}

// SpeechOutputConfig controls on-demand spoken answers.
type SpeechOutputConfig struct {
	SpeechEndpoint  `yaml:",inline"`
	GatewayDisabled bool    `yaml:"gateway_disabled,omitempty" json:"gateway_disabled"`
	Voice           string  `yaml:"voice,omitempty" json:"voice"`
	Speed           float64 `yaml:"speed" json:"speed"`
	Format          string  `yaml:"format" json:"format"`
	Instructions    string  `yaml:"instructions,omitempty" json:"instructions"`
	TimeoutSeconds  int     `yaml:"timeout_seconds" json:"timeout_seconds"`
}

// SpeechLimits bounds independent voice requests and ephemeral phone buffers.
type SpeechLimits struct {
	InputBytes        int64 `yaml:"input_bytes" json:"input_bytes"`
	OutputBytes       int64 `yaml:"output_bytes" json:"output_bytes"`
	ChunkChars        int   `yaml:"chunk_chars" json:"chunk_chars"`
	StagingTTLSeconds int   `yaml:"staging_ttl_seconds" json:"staging_ttl_seconds"`
}

// SpeechConfig is PC-owned; phones receive only a safe runtime projection.
type SpeechConfig struct {
	GatewayParameters map[string]audioparams.Overrides `yaml:"gateway_parameters,omitempty" json:"gateway_parameters,omitempty"`
	Input             SpeechInputConfig                `yaml:"input" json:"input"`
	Output            SpeechOutputConfig               `yaml:"output" json:"output"`
	Limits            SpeechLimits                     `yaml:"limits" json:"limits"`
}

// DefaultSpeech leaves both personal and Gateway speech off.
func DefaultSpeech() SpeechConfig {
	return SpeechConfig{
		Input:  SpeechInputConfig{SpeechEndpoint: SpeechEndpoint{Source: SpeechSourceAccount}, GatewayDisabled: true, RecordingFormat: "auto", MaxDurationSeconds: DefaultSpeechDurationSeconds, TimeoutSeconds: DefaultSpeechTimeoutSeconds},
		Output: SpeechOutputConfig{SpeechEndpoint: SpeechEndpoint{Source: SpeechSourceAccount}, GatewayDisabled: true, Speed: 1, Format: "mp3", TimeoutSeconds: DefaultSpeechTimeoutSeconds},
		Limits: SpeechLimits{InputBytes: DefaultSpeechInputBytes, OutputBytes: DefaultSpeechOutputBytes, ChunkChars: DefaultSpeechChunkChars, StagingTTLSeconds: DefaultSpeechStagingTTLSeconds},
	}
}

func (c *Config) normalizeSpeech() {
	s, d := &c.Speech, DefaultSpeech()
	for _, endpoint := range []*SpeechEndpoint{&s.Input.SpeechEndpoint, &s.Output.SpeechEndpoint} {
		endpoint.Source = strings.TrimSpace(endpoint.Source)
		if endpoint.Source == "" {
			endpoint.Source = SpeechSourceAccount
		}
		endpoint.ProviderID = strings.TrimSpace(endpoint.ProviderID)
		endpoint.BaseURL = strings.TrimRight(strings.TrimSpace(endpoint.BaseURL), "/")
		endpoint.Model = strings.TrimSpace(endpoint.Model)
	}
	s.Input.DeviceID = strings.TrimSpace(s.Input.DeviceID)
	if s.Input.RecordingFormat == "" {
		s.Input.RecordingFormat = d.Input.RecordingFormat
	}
	if s.Input.MaxDurationSeconds == 0 {
		s.Input.MaxDurationSeconds = d.Input.MaxDurationSeconds
	}
	if s.Input.TimeoutSeconds == 0 {
		s.Input.TimeoutSeconds = d.Input.TimeoutSeconds
	}
	if s.Output.TimeoutSeconds == 0 {
		s.Output.TimeoutSeconds = d.Output.TimeoutSeconds
	}
	if s.Output.Format == "" {
		s.Output.Format = d.Output.Format
	}
	if s.Output.Speed == 0 {
		s.Output.Speed = d.Output.Speed
	}
	if s.Limits.InputBytes == 0 {
		s.Limits.InputBytes = d.Limits.InputBytes
	}
	if s.Limits.OutputBytes == 0 {
		s.Limits.OutputBytes = d.Limits.OutputBytes
	}
	if s.Limits.ChunkChars == 0 {
		s.Limits.ChunkChars = d.Limits.ChunkChars
	}
	if s.Limits.StagingTTLSeconds == 0 {
		s.Limits.StagingTTLSeconds = d.Limits.StagingTTLSeconds
	}
}

func (c *Config) validateSpeech() (err error) {
	defer func() {
		if err != nil {
			err = fmt.Errorf("%w: %v", ErrInvalidSpeech, err)
		}
	}()
	c.normalizeSpeech()
	s := c.Speech
	if len(s.GatewayParameters) > 256 {
		return fmt.Errorf("too many gateway audio preferences")
	}
	for scope, o := range s.GatewayParameters {
		if len(scope) > 256 || scope == "" {
			return fmt.Errorf("invalid gateway audio scope")
		}
		for _, value := range o.Values() {
			switch v := value.(type) {
			case string:
				if !utf8.ValidString(v) || len(v) > 400000 {
					return fmt.Errorf("invalid gateway audio preference")
				}
			case float64:
				if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 {
					return fmt.Errorf("invalid gateway audio speed")
				}
			}
		}
	}
	for _, endpoint := range []SpeechEndpoint{s.Input.SpeechEndpoint, s.Output.SpeechEndpoint} {
		if strings.ContainsAny(endpoint.APIKey, "\r\n") {
			return fmt.Errorf("speech API key must not contain line breaks")
		}
		if len(endpoint.Model) > 256 {
			return fmt.Errorf("speech model must not exceed 256 bytes")
		}
		switch endpoint.Source {
		case SpeechSourceAccount, SpeechSourceProvider, SpeechSourceCustom:
		default:
			return fmt.Errorf("speech source must be account, provider, or custom")
		}
		if endpoint.BaseURL != "" {
			u, err := url.Parse(endpoint.BaseURL)
			if err != nil || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") {
				return fmt.Errorf("speech base URL must be an HTTP(S) endpoint without credentials, query or fragment")
			}
		}
	}
	if len(s.Input.Language) > 64 || len(s.Output.Voice) > 256 || len(s.Input.Prompt) > 8192 || len(s.Output.Instructions) > 8192 {
		return fmt.Errorf("speech language, voice or instructions exceed their size limits")
	}
	if s.Input.RecordingFormat != "auto" && s.Input.RecordingFormat != "wav" {
		return fmt.Errorf("speech recording format must be auto or wav")
	}
	if id := s.Input.DeviceID; id != "" && (len(id) > 256 || strings.ContainsAny(id, "\r\n") || !utf8.ValidString(id)) {
		return fmt.Errorf("speech input device is invalid")
	}
	if s.Output.Format != "mp3" && s.Output.Format != "wav" && s.Output.Format != "pcm" {
		return fmt.Errorf("speech output format must be mp3, wav or pcm")
	}
	if math.IsNaN(s.Output.Speed) || math.IsInf(s.Output.Speed, 0) || s.Output.Speed < .25 || s.Output.Speed > 4 {
		return fmt.Errorf("speech speed must be between 0.25 and 4")
	}
	for _, item := range []struct {
		name       string
		value, max int64
	}{
		{"recording duration", int64(s.Input.MaxDurationSeconds), 3600},
		{"input timeout", int64(s.Input.TimeoutSeconds), 3600},
		{"output timeout", int64(s.Output.TimeoutSeconds), 3600},
		{"input bytes", s.Limits.InputBytes, 256 << 20},
		{"output bytes", s.Limits.OutputBytes, 256 << 20},
		{"chunk chars", int64(s.Limits.ChunkChars), 4096},
		{"staging TTL", int64(s.Limits.StagingTTLSeconds), 3600},
	} {
		if item.value < 1 || item.value > item.max {
			return fmt.Errorf("speech %s must be between 1 and %d", item.name, item.max)
		}
	}
	return nil
}
