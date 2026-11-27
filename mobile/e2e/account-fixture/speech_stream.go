package main

import (
	"context"
	_ "embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/speech"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

//go:embed speech-observer.js
var speechObserver string

const desktopSpeechToken = "native-desktop-speech-only"

type speechRequest struct {
	ID           int    `json:"id"`
	Route        string `json:"route"`
	Mode         string `json:"mode"`
	StreamFormat string `json:"stream_format"`
	Flushes      int    `json:"flushes"`
	Bytes        int    `json:"bytes"`
	EOF          bool   `json:"eof"`
	Cancelled    bool   `json:"cancelled"`
	Failed       bool   `json:"failed"`
}

type speechObservation struct {
	NodeID       int     `json:"node_id"`
	ContextID    int     `json:"context_id"`
	Sequence     int     `json:"sequence"`
	Event        string  `json:"event"`
	ContextState string  `json:"context_state"`
	SampleRate   int     `json:"sample_rate"`
	Channels     int     `json:"channels"`
	Length       int     `json:"length"`
	Minimum      float64 `json:"minimum"`
	Maximum      float64 `json:"maximum"`
	Nonzero      bool    `json:"nonzero"`
}

type speechFixture struct {
	mu           sync.Mutex
	enabled      bool
	mode         string
	requests     []*speechRequest
	observations []speechObservation
}

// Called before the account handler takes f.mu: held audio cannot block CAS,
// fixture controls or the passive observer that proves pre-EOF playback.
func (f *fixture) speechRoute(w http.ResponseWriter, r *http.Request) bool {
	path := r.URL.Path
	if path != "/v1/audio/speech" && path != "/client/desktop/v1/audio/speech" && path != "/client/desktop/v1/chat/completions" && !strings.HasPrefix(path, "/fixture/speech/") {
		return false
	}
	switch path {
	case "/fixture/speech/observer.js":
		w.Header().Set("Content-Type", "text/javascript")
		_, _ = w.Write([]byte(strings.ReplaceAll(speechObserver, "__ZWAI_SPEECH_FIXTURE_ORIGIN__", f.origin)))
	case "/fixture/speech/state":
		f.speech.mu.Lock()
		defer f.speech.mu.Unlock()
		data(w, map[string]any{"enabled": f.speech.enabled, "mode": f.speech.mode, "requests": f.speech.requests, "observations": f.speech.observations})
	case "/fixture/speech/observe":
		var observed speechObservation
		if r.Method != http.MethodPost || json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&observed) != nil ||
			(observed.Event != "installed" && observed.Event != "start" && observed.Event != "stop" && observed.Event != "ended" && observed.Event != "disconnect" && observed.Event != "context_closed") {
			http.Error(w, "invalid passive observation", 400)
			return true
		}
		f.speech.mu.Lock()
		if len(f.speech.observations) < 512 {
			f.speech.observations = append(f.speech.observations, observed)
		}
		f.speech.mu.Unlock()
		data(w, map[string]bool{"ok": true})
	case "/fixture/speech/setup":
		if r.Method != http.MethodPost {
			http.Error(w, "speech fixture setup requires POST", 503)
			return true
		}
		if err := f.setupSpeech(r.Context()); err != nil {
			http.Error(w, err.Error(), 503)
			return true
		}
		data(w, speech.New(f.engine.Config(), f.engine.Providers()).Snapshot())
	case "/fixture/speech/mode":
		mode := r.URL.Query().Get("value")
		if r.Method != http.MethodPost || (mode != "raw-held" && mode != "sse-held" && mode != "raw-finite" && mode != "sse-finite") {
			http.Error(w, "invalid speech fixture mode", 400)
			return true
		}
		f.speech.mu.Lock()
		f.speech.mode = mode
		f.speech.mu.Unlock()
		data(w, map[string]string{"mode": mode})
	case "/client/desktop/v1/chat/completions":
		var input struct {
			Model    string
			Stream   bool
			Messages []json.RawMessage
		}
		if r.Header.Get("Authorization") != "Bearer "+desktopSpeechToken {
			http.Error(w, "desktop fixture authority required", 401)
			return true
		}
		if r.Method != http.MethodPost || json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&input) != nil || input.Model != config.OfficialModelAlias || len(input.Messages) == 0 {
			http.Error(w, "invalid desktop fixture request", 400)
			return true
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if !input.Stream {
			// The real answer-admission classifier uses Generate, not Stream.
			// The fixed seed answer has no scheduling claim to extract.
			f.counts["speech_policy_calls"]++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": `{"claims":[]}`}, "finish_reason": "stop"}}})
			return true
		}
		w.Header().Set("Content-Type", "text/event-stream")
		f.modelText(w, "Native streaming speech fixture answer.")
	case "/v1/audio/speech", "/client/desktop/v1/audio/speech":
		f.mu.Lock()
		allowed := f.flow != nil && f.flow.Authorized && f.review == "approved" && f.api && r.Header.Get("Authorization") == "Bearer "+f.flow.Candidate
		if path == "/client/desktop/v1/audio/speech" {
			allowed = r.Header.Get("Authorization") == "Bearer "+desktopSpeechToken
		}
		f.mu.Unlock()
		if !allowed || r.Header.Get("Cookie") != "" {
			http.Error(w, "speech authority required", 401)
			return true
		}
		f.streamSpeech(w, r)
	default:
		http.NotFound(w, r)
	}
	return true
}

// Opt-in only. Ordinary account cases keep their original configuration.
func (f *fixture) setupSpeech(ctx context.Context) error {
	f.speech.mu.Lock()
	defer f.speech.mu.Unlock()
	if f.speech.enabled {
		return nil
	}
	if f.engine == nil {
		return fmt.Errorf("speech fixture: engine unavailable")
	}
	if f.engine.Providers().IsMock() {
		return fmt.Errorf("speech fixture: start the owned fixture with --speech-stream for a real account model source")
	}
	origin, err := url.ParseRequestURI(f.origin)
	if err != nil || origin.Scheme != "http" || origin.Hostname() != "127.0.0.1" || origin.Port() == "" || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return fmt.Errorf("speech fixture: account source requires its owned loopback origin")
	}
	source, err := provider.NewAccountProfile(fixtureModelSourceID, f.origin+"/client/desktop/v1", desktopSpeechToken, provider.AccountProfile{Label: "Native speech gateway", ContextWindow: 32768})
	if err != nil {
		return fmt.Errorf("speech fixture: account source: %w", err)
	}
	source.SetRoles([]provider.AccountRole{{ID: "conversation", Label: "Conversation", Enabled: true}, {ID: "speech", Label: "Speech", Enabled: true}})
	if err := f.engine.Config().Update(func(cfg *config.Config) error {
		cfg.Product.Mode = ""
		cfg.Swarm.AutoTitle = false
		cfg.Swarm.ComposerPredictions = false
		cfg.Memory.Enabled = false
		cfg.Speech.Output.Enabled = true
		cfg.Speech.Output.GatewayDisabled = false
		cfg.Speech.Output.Source = config.SpeechSourceAccount
		cfg.Speech.Output.Format = "pcm"
		cfg.Speech.Output.Voice = "native-fixture-voice"
		cfg.Speech.Output.TimeoutSeconds = 45
		return nil
	}); err != nil {
		return fmt.Errorf("speech fixture: configure PC: %w", err)
	}
	f.engine.Providers().SetAccountSource(source)
	thread, err := f.engine.CreateThread("Native speech stream", source.ID(), "")
	if err != nil {
		return fmt.Errorf("speech fixture: create playback thread: %w", err)
	}
	turn, err := f.engine.StartTurn(thread.ID, "Report the current task status.")
	if err != nil {
		return fmt.Errorf("speech fixture: generate playback answer: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		latest, err := f.engine.Store().GetTurn(turn.ID)
		if err != nil {
			return fmt.Errorf("speech fixture: read playback turn: %w", err)
		}
		if latest.Status != store.TurnRunning {
			if latest.Status != store.TurnDone || strings.TrimSpace(latest.Final) == "" {
				return fmt.Errorf("speech fixture: playback turn status=%s final_bytes=%d error=%s", latest.Status, len(latest.Final), latest.Error)
			}
			break
		}
		select {
		case <-ctx.Done():
			_ = f.engine.Interrupt(thread.ID)
			return fmt.Errorf("speech fixture: wait for playback answer: %w", ctx.Err())
		case <-ticker.C:
		}
	}
	f.mu.Lock()
	f.pcThreadID = thread.ID
	f.mu.Unlock()
	f.speech.enabled, f.speech.mode = true, "raw-held"
	return nil
}

// A synthetic, non-silent 440-Hz S16LE mono piece lasting 250ms. No audio is
// persisted; the native observer reports only dimensions and sample extrema.
func speechPCM() []byte {
	raw := make([]byte, 6000*2)
	for index := 0; index < len(raw)/2; index++ {
		value := int16(math.Sin(float64(index)*2*math.Pi*440/24000) * 8192)
		binary.LittleEndian.PutUint16(raw[index*2:], uint16(value))
	}
	return raw
}

func (f *fixture) streamSpeech(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Model, Input   string
		Voice          json.RawMessage
		ResponseFormat string `json:"response_format"`
		StreamFormat   string `json:"stream_format"`
	}
	if r.Method != http.MethodPost || json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&input) != nil {
		http.Error(w, "invalid native PCM request", 400)
		return
	}
	// A Mobile request may omit voice to use its current Gateway role's
	// default. The desktop fixture and explicit selections require the named voice.
	var voice string
	namedVoice := json.Unmarshal(input.Voice, &voice) == nil && voice == "native-fixture-voice"
	phoneDefault := r.URL.Path == "/v1/audio/speech" && len(input.Voice) == 0
	if input.Model != "speech" || input.Input == "" || (!namedVoice && !phoneDefault) || input.ResponseFormat != "pcm" || input.StreamFormat != "sse" {
		http.Error(w, "invalid native PCM request", 400)
		return
	}
	f.speech.mu.Lock()
	if !f.speech.enabled {
		f.speech.mu.Unlock()
		http.Error(w, "speech fixture is disabled", 503)
		return
	}
	request := &speechRequest{ID: len(f.speech.requests) + 1, Route: r.URL.Path, Mode: f.speech.mode, StreamFormat: input.StreamFormat}
	f.speech.requests = append(f.speech.requests, request)
	f.speech.mu.Unlock()
	sse := strings.HasPrefix(request.Mode, "sse")
	mime := "audio/pcm;rate=24000;channels=1"
	if sse {
		mime = "text/event-stream"
	}
	w.Header().Set("Content-Type", mime)
	pcm := speechPCM()
	wire := pcm
	if sse {
		wire = []byte("event: speech.audio.delta\ndata: {\"type\":\"speech.audio.delta\",\"audio\":\"" + base64.StdEncoding.EncodeToString(pcm) + "\",\"sample_rate\":24000,\"channels\":1,\"response_format\":\"pcm\"}\n\n")
	}
	for piece := 0; ; piece++ {
		if _, err := w.Write(wire); err != nil {
			f.finishSpeech(request, false, r.Context().Err() != nil)
			return
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			f.finishSpeech(request, false, r.Context().Err() != nil)
			return
		}
		f.speech.mu.Lock()
		request.Flushes++
		request.Bytes += len(pcm)
		f.speech.mu.Unlock()
		if strings.HasSuffix(request.Mode, "finite") && piece == 3 {
			if sse {
				if _, err := w.Write([]byte("event: speech.audio.done\ndata: {\"type\":\"speech.audio.done\"}\n\n")); err != nil {
					f.finishSpeech(request, false, r.Context().Err() != nil)
					return
				}
			}
			f.finishSpeech(request, true, false)
			return
		}
		timer := time.NewTimer(150 * time.Millisecond)
		select {
		case <-r.Context().Done():
			timer.Stop()
			f.finishSpeech(request, false, true)
			return
		case <-timer.C:
		}
	}
}

func (f *fixture) finishSpeech(request *speechRequest, eof, cancelled bool) {
	f.speech.mu.Lock()
	defer f.speech.mu.Unlock()
	request.EOF, request.Cancelled, request.Failed = eof, cancelled, !eof && !cancelled
}
