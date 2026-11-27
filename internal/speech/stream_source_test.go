package speech

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

func TestSpokenOutputRoutingFollowsItsResolvedRoleIndependentlyOfRecognition(t *testing.T) {
	cfg, svc := configuredSpeech(t, "https://personal.invalid/v1")
	if err := cfg.Update(func(c *config.Config) error {
		c.Product.Mode = config.ProductModeFOFA
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	source, err := provider.NewAccountSource("gateway:voice", "Gateway", "https://account.invalid/client/desktop/v1", "account-token", provider.Catalog{Names: []string{"configured-voice"}})
	if err != nil {
		t.Fatal(err)
	}
	source.SetModelPolicy(true, map[string]string{"speech": "configured-voice"})
	svc.pool.SetAccountSource(source)
	for _, personal := range []bool{false, true} {
		if personal {
			scope := svc.pool.ModelPolicy().Scope
			if err := cfg.Update(func(c *config.Config) error {
				c.Models.Providers = append(c.Models.Providers, config.Provider{ID: "personal", BaseURL: "https://account.invalid/client/desktop/v1", APIKey: "personal-key", Model: "speech"})
				c.Models.Bindings = map[string]map[string]config.ModelRef{scope: {"speech": {ProviderID: "personal", Model: "speech"}}}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		view := svc.Snapshot()
		if view.Execution != "pc_local" || view.GatewayOrigin != "" || view.Input.Model != "recognizer" || !view.Input.Ready {
			t.Fatalf("output routing changed the legacy recognition projection: %+v", view)
		}
		raw, err := json.Marshal(view.Output)
		if err != nil {
			t.Fatal(err)
		}
		var output map[string]any
		if err := json.Unmarshal(raw, &output); err != nil {
			t.Fatal(err)
		}
		if personal {
			if output["execution"] != "pc_local" || output["gateway_origin"] != nil || output["model"] != "speech" {
				t.Fatalf("personal output was mistaken for its same-origin gateway: %s", raw)
			}
		} else if output["execution"] != "gateway" || output["gateway_origin"] != "https://account.invalid" || output["model"] != "configured-voice" {
			t.Fatalf("editable gateway output lacks its own direct route: %s", raw)
		}
		if strings.Contains(string(raw), "account-token") || strings.Contains(string(raw), "personal-key") || strings.Contains(string(raw), "/client/desktop/v1") {
			t.Fatalf("output routing exposed private endpoint data: %s", raw)
		}
	}
}

func TestPCMStreamStopsWhenTheSameEndpointChangesFromAccountToPersonal(t *testing.T) {
	next := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "first")
		w.(http.Flusher).Flush()
		select {
		case <-next:
			_, _ = io.WriteString(w, "next")
			w.(http.Flusher).Flush()
		case <-r.Context().Done():
		}
		<-r.Context().Done()
	}))
	defer upstream.Close()
	cfg, svc := configuredSpeech(t, upstream.URL+"/v1")
	setPCM(t, cfg)
	if err := cfg.Update(func(c *config.Config) error {
		c.Product.Mode = config.ProductModeFOFA
		c.Speech.Output.GatewayDisabled = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	source, err := provider.NewAccountSource("gateway:voice", "Gateway", upstream.URL+"/v1", "same-token", provider.Catalog{Names: []string{"configured-voice"}})
	if err != nil {
		t.Fatal(err)
	}
	source.SetModelPolicy(true, map[string]string{"speech": "configured-voice"})
	svc.pool.SetAccountSource(source)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := svc.SynthesizeStream(ctx, "answer")
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Body.Close()
	if _, err := io.ReadFull(stream.Body, make([]byte, 5)); err != nil {
		t.Fatal(err)
	}
	scope := svc.pool.ModelPolicy().Scope
	if err := cfg.Update(func(c *config.Config) error {
		c.Models.Providers = append(c.Models.Providers, config.Provider{ID: "personal", BaseURL: upstream.URL + "/v1", APIKey: "same-token", Model: "configured-voice"})
		c.Models.Bindings = map[string]map[string]config.ModelRef{scope: {"speech": {ProviderID: "personal", Model: "configured-voice"}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	close(next)
	if _, err := stream.Body.Read(make([]byte, 4)); errorCode(err) != "speech_configuration_changed" {
		t.Fatalf("old stream survived a resolved source change at the same endpoint: %v", err)
	}
}

// A gateway can buffer its audio response even though it incrementally emits
// SSE. The resolved source, including editable role bindings, selects that wire.
func TestPCMStreamRequestsTheResolvedSourcesIncrementalProtocol(t *testing.T) {
	for _, name := range []string{"managed account", "editable account", "personal role override", "personal named speech"} {
		t.Run(name, func(t *testing.T) {
			gateway := name == "managed account" || name == "editable account"
			wantFormat, wantModel, wantToken := "audio", "speech", "private-key"
			if gateway {
				wantFormat, wantModel, wantToken = "sse", "configured-voice", "account-token"
			}
			first := []byte{1, 2}
			mime := "audio/pcm"
			if gateway {
				first = []byte("event: speech.audio.delta\ndata: {\"type\":\"speech.audio.delta\",\"audio\":\"AQI=\"}\n\n")
				mime = "text/event-stream"
			}
			canceled := make(chan struct{})
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["response_format"] != "pcm" || body["stream_format"] != wantFormat || body["model"] != wantModel || r.Header.Get("Authorization") != "Bearer "+wantToken {
					t.Errorf("resolved PCM request=%v decode=%v", body, err)
					http.Error(w, "wrong resolved speech protocol", http.StatusBadRequest)
					return
				}
				w.Header().Set("Content-Type", mime)
				_, _ = w.Write(first)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(canceled)
			}))
			defer upstream.Close()
			cfg, svc := configuredSpeech(t, upstream.URL+"/v1")
			setPCM(t, cfg)
			if err := cfg.Update(func(c *config.Config) error {
				c.Speech.Output.Model = "speech"
				if name != "personal named speech" {
					c.Product.Mode = config.ProductModeFOFA
					c.Speech.Output.GatewayDisabled = false
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if name != "personal named speech" {
				source, err := provider.NewAccountSource("gateway:voice", "Gateway", upstream.URL+"/v1", "account-token", provider.Catalog{Names: []string{"configured-voice"}})
				if err != nil {
					t.Fatal(err)
				}
				if name == "managed account" {
					source.SetRoles([]provider.AccountRole{{ID: "speech", Enabled: true}})
				}
				source.SetModelPolicy(name != "managed account", map[string]string{"speech": "configured-voice"})
				svc.pool.SetAccountSource(source)
				if name == "personal role override" {
					scope := svc.pool.ModelPolicy().Scope
					if err := cfg.Update(func(c *config.Config) error {
						c.Models.Providers = append(c.Models.Providers, config.Provider{ID: "personal", BaseURL: upstream.URL + "/v1", APIKey: "private-key", Model: "speech"})
						c.Models.Bindings = map[string]map[string]config.ModelRef{scope: {"speech": {ProviderID: "personal", Model: "speech"}}}
						// The legacy endpoint cannot classify a resolved role binding.
						c.Speech.Output.Source = config.SpeechSourceAccount
						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			stream, err := svc.SynthesizeStream(ctx, "answer")
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Body.Close()
			got := make([]byte, len(first))
			if _, err := io.ReadFull(stream.Body, got); err != nil || string(got) != string(first) || stream.MIME != mime {
				t.Fatalf("first incremental bytes=%q type=%q error=%v", got, stream.MIME, err)
			}
			if release, ok := svc.pool.TryHoldModelPolicy(); ok {
				release()
				t.Fatal("the first samples released model admission before stream Close")
			}
			if err := stream.Body.Close(); err != nil {
				t.Fatal(err)
			}
			select {
			case <-canceled:
			case <-time.After(time.Second):
				t.Fatal("closing the incremental stream did not cancel its real HTTP request")
			}
			if release, ok := svc.pool.TryHoldModelPolicy(); !ok {
				t.Fatal("stream Close retained model admission")
			} else {
				release()
			}
		})
	}
}
