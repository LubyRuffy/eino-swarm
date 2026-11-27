package remote

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

// Editable account models can use the PC-local buffered RPC route. Its binary
// contract must not accidentally inherit the HTTP player's incremental SSE wire.
func TestBufferedPhonePCMRPCStillRequestsAudioForAnAccountModel(t *testing.T) {
	pcm := []byte{1, 2, 3, 4}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["response_format"] != "pcm" || body["stream_format"] != "audio" || body["model"] != "configured-voice" || r.Header.Get("Authorization") != "Bearer account-token" {
			t.Errorf("buffered account PCM request=%v decode=%v", body, err)
		}
		w.Header().Set("Content-Type", "audio/pcm")
		_, _ = w.Write(pcm)
	}))
	defer upstream.Close()
	e := localVoiceEngine(t, upstream.URL+"/v1")
	if err := e.Config().Update(func(c *config.Config) error {
		c.Product.Mode = config.ProductModeFOFA
		c.Speech.Output.Format = "pcm"
		c.Speech.Output.GatewayDisabled = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	source, err := provider.NewAccountSource("gateway:voice", "Gateway", upstream.URL+"/v1", "account-token", provider.Catalog{Names: []string{"configured-voice"}})
	if err != nil {
		t.Fatal(err)
	}
	source.SetModelPolicy(true, map[string]string{"speech": "configured-voice"})
	e.Providers().SetAccountSource(source)
	p, replies := catalogPump(t, e)
	sendVoice(p, Request{ID: "config", Op: OpSpeechConfig})
	configuration := voiceReply(t, replies, "config")
	if result := configuration; !result.OK || result.Speech == nil || result.Speech.Execution != "pc_local" || !result.Speech.Output.Ready {
		t.Fatalf("editable account buffered projection=%+v", result)
	}
	sendVoice(p, Request{ID: "speak", Op: OpAudioSpeech, AudioID: "pcm", Text: "answer"})
	if result := voiceReply(t, replies, "speak"); !result.OK || result.Audio == nil || result.Audio.MIME != "audio/pcm" || result.Audio.Size != len(pcm) {
		t.Fatalf("buffered account PCM=%+v", result)
	}
	sendVoice(p, Request{ID: "chunk", Op: OpAudioChunk, AudioID: "pcm"})
	result := voiceReply(t, replies, "chunk")
	if !result.OK || result.Audio == nil {
		t.Fatalf("buffered PCM chunk=%+v", result)
	}
	data, err := base64.StdEncoding.DecodeString(result.Audio.Data)
	if err != nil || !bytes.Equal(data, pcm) || result.Audio.More {
		t.Fatalf("buffered PCM bytes=%v error=%v", data, err)
	}
	sendVoice(p, Request{ID: "cancel", Op: OpAudioCancel, AudioID: "pcm"})
	if result := voiceReply(t, replies, "cancel"); !result.OK {
		t.Fatalf("buffered PCM release=%+v", result)
	}
	// A new phone can route TTS directly using this same actual RPC projection,
	// while old phones retain the unchanged global PC-local buffered contract.
	raw, err := json.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	var projected struct {
		Speech struct {
			Output map[string]any `json:"output"`
		} `json:"speech"`
	}
	if err := json.Unmarshal(raw, &projected); err != nil || projected.Speech.Output["execution"] != "gateway" || projected.Speech.Output["gateway_origin"] != upstream.URL {
		t.Fatalf("RPC omitted the new phone's output-specific gateway route: %+v error=%v", projected, err)
	}
}
