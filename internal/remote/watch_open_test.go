package remote

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// A phone needs the header and the live edge in one round trip, while older
// phones must retain the existing watch response.
func TestPhoneWatchCanOpenTheConversationInOneReply(t *testing.T) {
	for _, include := range []bool{false, true} {
		t.Run(fmt.Sprint(include), func(t *testing.T) {
			e := testEngine(t)
			th, err := e.CreateThread("one round trip", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.Store().AppendEvent(&store.Event{ThreadID: th.ID, Kind: "user_message", Text: "opening request"}); err != nil {
				t.Fatal(err)
			}
			pump, _ := testPump(t, e)
			var req Request
			if err := json.Unmarshal([]byte(fmt.Sprintf(`{"id":"open","op":"watch","thread_id":%q,"include_detail":%t}`, th.ID, include)), &req); err != nil {
				t.Fatal(err)
			}
			got := pump.startWatch(req)
			if !got.OK || got.Op != OpReady || got.ThreadID != th.ID || len(got.Events) != 1 || got.Seq != 1 {
				t.Fatalf("watch lost its existing snapshot: %+v", got)
			}
			if include {
				if got.Detail == nil || got.Detail.ID != th.ID || got.Detail.Title != "one round trip" {
					t.Fatalf("opening still needs a separate detail request: %+v", got)
				}
			} else if got.Detail != nil {
				t.Fatal("an old phone received unsolicited detail")
			}
		})
	}
}

func TestPhoneOpeningFitsDetailsAndTailWithinTheSealedFrame(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("bounded opening", "", "")
	if err != nil {
		t.Fatal(err)
	}
	goal := strings.Repeat("g", 32<<10)
	if err := e.Store().UpdateThread(th.ID, map[string]any{"goal": goal}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 24; i++ {
		if err := e.Store().AppendEvent(&store.Event{ThreadID: th.ID, Kind: "agent_message", Text: strings.Repeat("x", 3000)}); err != nil {
			t.Fatal(err)
		}
	}
	pump, _ := testPump(t, e)
	pump.cfg = config.RemoteConfig{EventChars: 4000}
	var req Request
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"id":"open","op":"watch","thread_id":%q,"include_detail":true}`, th.ID)), &req); err != nil {
		t.Fatal(err)
	}
	got := pump.startWatch(req)
	if !got.OK || got.Detail == nil || got.Detail.Goal != goal || !got.More || got.Seq != 24 {
		t.Fatalf("opening lost metadata or the older-page boundary: ok=%v detail=%v more=%v seq=%d", got.OK, got.Detail != nil, got.More, got.Seq)
	}
	if len(got.Events) == 0 || got.Events[len(got.Events)-1].Seq != 24 {
		t.Fatal("frame packing discarded the live edge")
	}
	raw, err := json.Marshal(withHost(got, config.RemoteConfig{DisplayName: strings.Repeat("界", 40)}))
	if err != nil || len(raw) > MaxPushPayload-24 {
		t.Fatalf("combined reply cannot leave the PC: bytes=%d err=%v", len(raw), err)
	}
}

func TestPhoneOpeningRejectsUnencodableMetadata(t *testing.T) {
	resp := okBase("open", "relay", "session")
	resp.Events = []EventView{{CreatedAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}
	got := packWatchDetail(resp, &ThreadDetail{ID: "thread"})
	if got.OK || got.ID != resp.ID || got.Error == "" {
		t.Fatal("an unencodable reply cannot be acknowledged")
	}
}

func TestPhoneCombinedOpeningCrossesTheEncryptedRelay(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("relay opening", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().AppendEvent(&store.Event{ThreadID: th.ID, Kind: "user_message", Text: "one encrypted request"}); err != nil {
		t.Fatal(err)
	}
	r := newFollowupRelay(t, e)
	got := r.rpc(t, Request{ID: "one", Op: OpWatch, ThreadID: th.ID, IncludeDetail: true})
	if !got.OK || got.Detail == nil || got.Detail.ID != th.ID || got.Op != OpReady || len(got.Events) != 1 {
		t.Fatalf("one encrypted RPC did not carry both header and transcript: ok=%v detail=%v events=%d", got.OK, got.Detail != nil, len(got.Events))
	}
}

func TestPhoneOpeningRefusesOversizedMetadataWithoutLeavingAWatch(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("oversized opening", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Store().UpdateThread(th.ID, map[string]any{"goal": strings.Repeat("g", MaxPushPayload)}); err != nil {
		t.Fatal(err)
	}
	pump, _ := testPump(t, e)
	var req Request
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"id":"open","op":"watch","thread_id":%q,"include_detail":true}`, th.ID)), &req); err != nil {
		t.Fatal(err)
	}
	got := pump.startWatch(req)
	if got.OK || got.Code != "too_large" || got.ID != "open" {
		t.Fatalf("oversized metadata was not rejected: ok=%v code=%q", got.OK, got.Code)
	}
	pump.mu.Lock()
	watching, cancel := pump.watching, pump.cancel
	pump.mu.Unlock()
	if watching != "" || cancel != nil {
		t.Fatal("failed opening retained a live watch")
	}
}
