package config

import (
	"encoding/json"
	"fmt"
	"github.com/LubyRuffy/eino-swarm/internal/audioparams"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestSpeechStaysOffUntilTheUserEnablesEachDirection(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Speech.Input.Enabled || cfg.Speech.Output.Enabled {
		t.Fatal("voice must be opt-in")
	}
	if cfg.Speech.Input.MaxDurationSeconds != 120 || cfg.Speech.Output.Speed != 1 || cfg.Speech.Output.Format != "mp3" {
		t.Fatalf("defaults=%+v", cfg.Speech)
	}
	if cfg.Speech.Limits.InputBytes != 25<<20 || cfg.Speech.Limits.OutputBytes != 32<<20 {
		t.Fatalf("limits=%+v", cfg.Speech.Limits)
	}
	if err := cfg.Update(func(next *Config) error {
		next.Speech.Input.Enabled = true
		next.Speech.Input.Source = SpeechSourceCustom
		next.Speech.Input.Model = "recognizer"
		next.Speech.Input.BaseURL = "http://localhost.invalid/v1"
		next.Speech.Input.APIKey = "private-voice-key"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if !cfg.Snapshot().Speech.Input.Enabled || cfg.Speech.Output.Enabled {
		t.Fatal("save must publish only the edited voice switch")
	}
	reloaded, err := Load(cfg.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reloaded.Speech, cfg.Snapshot().Speech) {
		t.Fatalf("voice settings did not survive reload: %+v", reloaded.Speech)
	}
	raw, err := json.Marshal(reloaded.Speech)
	if err != nil || strings.Contains(string(raw), "private-voice-key") {
		t.Fatalf("public configuration leaked credentials: %s (%v)", raw, err)
	}
	stored, err := os.ReadFile(cfg.Path())
	if err != nil || !strings.Contains(string(stored), "private-voice-key") {
		t.Fatal("private on-disk credential was not preserved")
	}
}

func TestSpeechRejectsInvalidSettingsWithoutPublishingThem(t *testing.T) {
	for name, edit := range map[string]func(*SpeechConfig){
		"source":            func(s *SpeechConfig) { s.Input.Source = "unknown" },
		"multiline key":     func(s *SpeechConfig) { s.Input.APIKey = "private\nsecret" },
		"model size":        func(s *SpeechConfig) { s.Input.Model = strings.Repeat("m", 257) },
		"language size":     func(s *SpeechConfig) { s.Input.Language = strings.Repeat("l", 65) },
		"voice size":        func(s *SpeechConfig) { s.Output.Voice = strings.Repeat("v", 257) },
		"prompt size":       func(s *SpeechConfig) { s.Input.Prompt = strings.Repeat("p", 8193) },
		"instructions size": func(s *SpeechConfig) { s.Output.Instructions = strings.Repeat("i", 8193) },
		"output source":     func(s *SpeechConfig) { s.Output.Source = "unknown" },
		"URL":               func(s *SpeechConfig) { s.Input.BaseURL = "file:///secret" },
		"URL credentials":   func(s *SpeechConfig) { s.Output.BaseURL = "https://user:pass@example.invalid/v1" },
		"format":            func(s *SpeechConfig) { s.Output.Format = "executable" },
		"recording":         func(s *SpeechConfig) { s.Input.RecordingFormat = "unknown" },
		"device id":         func(s *SpeechConfig) { s.Input.DeviceID = strings.Repeat("d", 257) },
		"device break":      func(s *SpeechConfig) { s.Input.DeviceID = "mic\nid" },
		"speed":             func(s *SpeechConfig) { s.Output.Speed = 8 },
		"nonfinite speed":   func(s *SpeechConfig) { s.Output.Speed = math.NaN() },
		"duration":          func(s *SpeechConfig) { s.Input.MaxDurationSeconds = -1 },
		"input timeout":     func(s *SpeechConfig) { s.Input.TimeoutSeconds = -1 },
		"output timeout":    func(s *SpeechConfig) { s.Output.TimeoutSeconds = -1 },
		"input bytes":       func(s *SpeechConfig) { s.Limits.InputBytes = -1 },
		"output bytes":      func(s *SpeechConfig) { s.Limits.OutputBytes = -1 },
		"chunk chars":       func(s *SpeechConfig) { s.Limits.ChunkChars = 90000 },
		"TTL":               func(s *SpeechConfig) { s.Limits.StagingTTLSeconds = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg, err := Load(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			before := cfg.Speech
			if err := cfg.Update(func(next *Config) error { edit(&next.Speech); return nil }); err == nil {
				t.Fatal("invalid voice configuration was accepted")
			}
			if !reflect.DeepEqual(cfg.Speech, before) {
				t.Fatal("a failed update changed live voice configuration")
			}
		})
	}
}

func TestSpeechInputDeviceSurvivesReloadAndBlankMeansTheSystemDefault(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Speech.Input.DeviceID != "" {
		t.Fatal("a new install must follow the system input")
	}
	if err = cfg.Update(func(next *Config) error {
		next.Speech.Input.DeviceID = " camera-mic "
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if cfg.Snapshot().Speech.Input.DeviceID != "camera-mic" {
		t.Fatalf("device=%q", cfg.Snapshot().Speech.Input.DeviceID)
	}
	reloaded, err := Load(cfg.DataDir())
	if err != nil || reloaded.Speech.Input.DeviceID != "camera-mic" {
		t.Fatalf("device did not reload: %v %+v", err, reloaded.Speech.Input.DeviceID)
	}
}

func TestOldConfigsGetSpeechDefaultsWithoutEnablingVoice(t *testing.T) {
	cfg := Default()
	cfg.Speech = SpeechConfig{}
	cfg.normalizeSpeech()
	want := DefaultSpeech()
	want.Input.GatewayDisabled, want.Output.GatewayDisabled = false, false
	if !reflect.DeepEqual(cfg.Speech, want) {
		t.Fatalf("normalized=%+v", cfg.Speech)
	}
	for _, source := range []string{SpeechSourceAccount, SpeechSourceProvider, SpeechSourceCustom} {
		cfg.Speech.Input.Source, cfg.Speech.Output.Source = source, source
		if err := cfg.validateSpeech(); err != nil {
			t.Fatal(err)
		}
	}
	cfg.Speech.Input.RecordingFormat = "wav"
	cfg.Speech.Output.Format = "wav"
	if err := cfg.validateSpeech(); err != nil {
		t.Fatal(err)
	}
	cfg.Speech.Output.Format = "pcm"
	if err := cfg.validateSpeech(); err != nil {
		t.Fatal(err)
	}
}

func TestGatewayAudioPreferencesSurviveReloadAndSnapshotWithoutLegacySpeedBounds(t *testing.T) {
	cfg, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	voice, speed := "saved-voice", 8.0
	if err = cfg.Update(func(c *Config) error {
		c.Speech.GatewayParameters = map[string]audioparams.Overrides{"audio:one": {Voice: &voice, Speed: &speed}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	snapshot := cfg.Snapshot()
	*snapshot.Speech.GatewayParameters["audio:one"].Voice = "changed"
	if *cfg.Snapshot().Speech.GatewayParameters["audio:one"].Voice != "saved-voice" {
		t.Fatal("snapshot mutated published preferences")
	}
	loaded, err := Load(cfg.DataDir())
	if err != nil || !reflect.DeepEqual(cfg.Snapshot().Speech, loaded.Speech) {
		t.Fatalf("preferences did not reload: %v", err)
	}
	for _, invalid := range []map[string]audioparams.Overrides{
		{"": {}}, {strings.Repeat("a", 257): {}}, {"one": {Voice: ptrAudio(string([]byte{255}))}}, {"one": {Prompt: ptrAudio(strings.Repeat("x", 400001))}}, {"one": {Speed: ptrAudio(math.NaN())}}, {"one": {Speed: ptrAudio(0.0)}},
	} {
		if err = cfg.Update(func(c *Config) error { c.Speech.GatewayParameters = invalid; return nil }); err == nil {
			t.Fatal("invalid preference accepted")
		}
	}
	tooMany := map[string]audioparams.Overrides{}
	for i := 0; i < 257; i++ {
		tooMany[fmt.Sprint(i)] = audioparams.Overrides{}
	}
	if err = cfg.Update(func(c *Config) error { c.Speech.GatewayParameters = tooMany; return nil }); err == nil {
		t.Fatal("unbounded preference map")
	}
}
func ptrAudio[T any](v T) *T { return &v }
