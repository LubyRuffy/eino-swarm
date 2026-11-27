package speech

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/audioparams"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

const audioCatalog = `{"data":[{"id":"speech","model_type":"speech","audio":{"version":1,"revision":"r1","status":"configured","parameters":{"voice":{"values":["calm","bright"],"default":"calm","configurable":true},"response_format":{"values":["pcm"],"default":"pcm","configurable":false},"stream_format":{"values":["audio"],"default":"audio","configurable":false},"speed":{"min":0.1,"max":10,"default":2,"configurable":true},"instructions":{"max_characters":2,"default":"好","configurable":true},"max_input_characters":4}}},{"id":"transcription","model_type":"transcription","audio":{"version":1,"revision":"r1","status":"configured","parameters":{"response_format":{"values":["json"],"default":"json","configurable":false},"language":{"values":["en","zh"],"default":"zh","configurable":true},"prompt":{"max_characters":2,"configurable":true},"max_file_bytes":4096}}}]}`

func managedAudio(t *testing.T, endpoint string) (*config.Config, *Service, *provider.AccountSource) {
	t.Helper()
	cfg, svc := configuredSpeech(t, endpoint)
	if err := cfg.Update(func(c *config.Config) error {
		c.Product.Mode = config.ProductModeFOFA
		c.Speech.Input.GatewayDisabled, c.Speech.Output.GatewayDisabled = false, false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	source, err := provider.NewAccountSource("gateway:voice", "Gateway", endpoint, "private", provider.Catalog{Names: []string{"speech", "transcription"}, Types: map[string]string{"speech": "speech", "transcription": "transcription"}})
	if err != nil {
		t.Fatal(err)
	}
	source.SetAccountOwner("owner")
	source.SetModelPolicy(false, map[string]string{"speech": "speech", "transcription": "transcription"})
	source.SetRoles([]provider.AccountRole{{ID: "speech", Enabled: true}, {ID: "transcription", Enabled: true}})
	source.SetCapabilities(&provider.SourceCapabilities{Authorized: true, Available: true, Capabilities: map[string]provider.RoleCapability{
		"speech": {Supported: true, Available: true, Endpoints: []string{"audio_speech"}, AudioParametersVersion: 1}, "transcription": {Supported: true, Available: true, Endpoints: []string{"audio_transcriptions"}, AudioParametersVersion: 1},
	}})
	source.SetAudioParameters([]byte(audioCatalog))
	svc.pool.SetAccountSource(source)
	return cfg, svc, source
}
func TestGatewayRequestsOmitDefaultsAndKeepExplicitPreferenceAcrossContractChange(t *testing.T) {
	var calls atomic.Int32
	var received = make(chan map[string]any, 4)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		received <- body
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write([]byte{0, 1})
	}))
	defer up.Close()
	cfg, svc, _ := managedAudio(t, up.URL+"/v1")
	view := svc.Snapshot()
	if !view.Output.Ready || view.Output.Speed != 2 || view.Output.Voice != "calm" || view.Limits.ChunkChars != 4 || view.Limits.InputBytes != 4096 {
		t.Fatalf("snapshot=%+v", view)
	}
	stream, err := svc.SynthesizeStream(t.Context(), "😀好")
	if err != nil {
		t.Fatal(err)
	}
	_, err = io.ReadAll(stream.Body)
	stream.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	body := <-received
	if len(body) != 2 || body["model"] != "speech" || body["input"] != "😀好" {
		t.Fatalf("defaults became overrides: %v", body)
	}
	voice, speed := "bright", 8.0
	scope := view.Output.Audio.Scope
	if err = cfg.Update(func(c *config.Config) error {
		c.Speech.GatewayParameters = map[string]audioparams.Overrides{scope: {Voice: &voice, Speed: &speed}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err = svc.Synthesize(t.Context(), "ok"); err != nil {
		t.Fatal(err)
	}
	body = <-received
	if body["voice"] != "bright" || body["speed"] != 8.0 || len(body) != 4 {
		t.Fatal(body)
	}
	selected, err := svc.selectTarget(cfg.Snapshot(), false)
	if err != nil {
		t.Fatal(err)
	}
	_, _, source := managedAudio(t, up.URL+"/v1")
	source.SetAudioParameters([]byte(strings.ReplaceAll(audioCatalog, `"bright"`, `"new"`)))
	svc.pool.SetAccountSource(source)
	if svc.unchanged(selected) {
		t.Fatal("contract change retained in-flight ownership")
	}
	if _, err = svc.SynthesizeStream(t.Context(), "ok"); errorCode(err) != "invalid_audio_parameter" {
		t.Fatalf("invalid saved choice accepted: %v", err)
	}
	if *cfg.Snapshot().Speech.GatewayParameters[scope].Voice != "bright" || calls.Load() != 2 {
		t.Fatal("silently replaced voice or retried")
	}
	if _, err = svc.Synthesize(t.Context(), "too long"); err == nil {
		t.Fatal("invalid preference accepted")
	}
}
func TestGatewayTranscriptionDefaultsAreOmittedAndActualBytesAreBounded(t *testing.T) {
	var calls atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if err := r.ParseMultipartForm(8192); err != nil {
			t.Error(err)
		}
		defer r.MultipartForm.RemoveAll()
		if len(r.MultipartForm.Value) != 1 || r.FormValue("model") != "transcription" {
			t.Errorf("default fields leaked: %v", r.MultipartForm.Value)
		}
		_, _ = io.WriteString(w, `{"text":"hello"}`)
	}))
	defer up.Close()
	_, svc, _ := managedAudio(t, up.URL+"/v1")
	if text, err := svc.Transcribe(t.Context(), Audio{Filename: "a.wav", MIME: "audio/wav", Data: wavFixture()}); err != nil || text != "hello" {
		t.Fatalf("%q %v", text, err)
	}
	if _, err := svc.Transcribe(t.Context(), Audio{MIME: "audio/wav", Data: make([]byte, 4097)}); errorCode(err) != "speech_input_size" {
		t.Fatal(err)
	}
	if _, err := svc.SynthesizeStream(t.Context(), "😀😀😀😀😀"); errorCode(err) != "speech_invalid_text" {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("invalid input reached gateway")
	}
}
func TestGatewayParameterErrorRefreshesWithoutRetry(t *testing.T) {
	for _, code := range []string{"invalid_audio_parameter", "audio_parameters_changed"} {
		t.Run(code, func(t *testing.T) {
			var calls, refresh atomic.Int32
			up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(409)
				_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]string{"code": code, "message": "private diagnostic"}})
			}))
			defer up.Close()
			_, svc, _ := managedAudio(t, up.URL+"/v1")
			svc.pool.SetCapabilitiesTrigger(func() { refresh.Add(1) })
			if _, err := svc.SynthesizeStream(context.Background(), "ok"); errorCode(err) != code || strings.Contains(err.Error(), "private") {
				t.Fatal(err)
			}
			if calls.Load() != 1 || refresh.Load() != 1 {
				t.Fatal("did not refresh exactly once without retry")
			}
		})
	}
}

func TestDeclaredDecoderCompatibilityAndLegacyBodies(t *testing.T) {
	_, svc, _ := managedAudio(t, "https://gateway.invalid/v1")
	for _, tc := range []struct {
		old, new string
		input    bool
	}{
		{`"response_format":{"values":["pcm"],"default":"pcm"`, `"response_format":{"values":["opus"],"default":"opus"`, false},
		{`"stream_format":{"values":["audio"],"default":"audio"`, `"stream_format":{"values":["other"],"default":"other"`, false},
		{`"response_format":{"values":["json"],"default":"json"`, `"response_format":{"values":["text"],"default":"text"`, true},
	} {
		_, _, source := managedAudio(t, "https://gateway.invalid/v1")
		source.SetAudioParameters([]byte(strings.ReplaceAll(audioCatalog, tc.old, tc.new)))
		svc.pool.SetAccountSource(source)
		if _, err := svc.selectTarget(svc.cfg.Snapshot(), tc.input); errorCode(err) != "speech_input_format" {
			t.Fatalf("decoder compatibility: %v", err)
		}
	}
	selected := selection{settings: config.DefaultSpeech(), target: config.Provider{Model: "voice"}}
	selected.settings.Output.Format = "pcm"
	selected.settings.Output.Voice = "local"
	selected.settings.Output.Instructions = "hint"
	if body := speechBody(selected, "text", true); body["stream_format"] != "audio" || body["voice"] != "local" || body["instructions"] != "hint" {
		t.Fatal(body)
	}
	selected.audio = &audioparams.View{}
	if body := speechBody(selected, "text", true); body["stream_format"] != "sse" {
		t.Fatal(body)
	}
	selected.settings.Output.Format = "wav"
	selected.settings.Output.Voice = ""
	selected.settings.Output.Instructions = ""
	if body := speechBody(selected, "text", false); len(body) != 4 {
		t.Fatal(body)
	}
}
