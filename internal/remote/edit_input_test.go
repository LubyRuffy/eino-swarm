package remote

import (
	"bytes"
	"image"
	"image/png"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestPhoneEditsTheOriginalInputOverPairlink(t *testing.T) {
	t.Setenv("ZWAI_MOCK_WORKER_DELAY_MS", "1200")
	e := testEngine(t)
	th, err := e.CreateThread("historical input", "", "")
	if err != nil {
		t.Fatal(err)
	}
	r := newFollowupRelay(t, e)
	if got := r.rpc(t, Request{ID: "watch", Op: OpWatch, ThreadID: th.ID}); !got.OK || got.Op != OpReady {
		t.Fatalf("watch: %+v", got)
	}
	caption := strings.Repeat("完整的原始输入 ", 90) + "last line"
	var inputPNG bytes.Buffer
	if err := png.Encode(&inputPNG, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	old, err := e.StartTurnInput(th.ID, engine.UserInput{
		Text: caption, Images: []engine.ImageInput{{Name: "input.png", MIME: "image/png", Data: inputPNG.Bytes()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	preview := r.userEvent(t, truncate(caption, 400))
	if !e.Status(th.ID).Running || len(preview.ImageIDs) != 1 {
		t.Fatalf("expected a clipped image prompt in a running turn: %+v", preview)
	}
	req := Request{ID: "draft", Op: "edit_input", ThreadID: th.ID, FromEventSeq: preview.Seq}
	draft := r.rpc(t, req)
	if !draft.OK || draft.Event == nil || draft.Event.Text != caption || !draft.Event.HasImages {
		t.Fatalf("editor did not read the complete persisted input: %+v", draft)
	}
	queued := r.rpc(t, Request{ID: "followup", Op: OpDeliver, DeliveryMode: "queue", ThreadID: th.ID, Text: "discard this queued followup"})
	if !queued.OK || queued.Followups == nil || len(*queued.Followups) != 1 {
		t.Fatalf("could not stage a followup: %+v", queued)
	}
	req.ID = "edit"
	replacement := "Recheck the original image"
	req.Replacement = &replacement
	sent := r.rpc(t, req)
	if !sent.OK {
		t.Fatalf("edit: %+v", sent)
	}
	edited := r.userEvent(t, replacement)
	if edited.TurnID == old.ID || edited.Seq <= preview.Seq || len(edited.ImageIDs) != 1 || edited.ImageIDs[0] != preview.ImageIDs[0] {
		t.Fatalf("replacement identity or image changed incorrectly: %+v", edited)
	}
	rewound := false
	for _, resp := range r.seen {
		if resp.Event == nil {
			continue
		}
		if resp.Event.Seq == edited.Seq {
			break
		}
		if resp.Event.Kind == engine.KindRewound && resp.Event.Text == strconv.FormatInt(preview.Seq, 10) && resp.Event.Seq > preview.Seq {
			rewound = true
		}
	}
	if !rewound {
		t.Fatal("the phone did not see a stored rewind before the replacement input")
	}
	imageBytes, mime, err := e.ReadInputImage(th.ID, edited.ImageIDs[0])
	if err != nil || mime != "image/png" || !bytes.Equal(imageBytes, inputPNG.Bytes()) {
		t.Fatalf("original image bytes not retained: %s %v", mime, err)
	}
	pixels, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil || pixels.Bounds().Dx() != 2 || pixels.Bounds().Dy() != 3 {
		t.Fatalf("original image is no longer a decodable 2×3 image: %v", err)
	}
	turns, err := e.Store().ListTurns(th.ID)
	if err != nil || len(turns) != 1 || turns[0].ID != edited.TurnID {
		t.Fatalf("history still contains the replaced turn: %+v %v", turns, err)
	}
	if followups, err := e.ListFollowups(th.ID); err != nil || len(followups) != 0 {
		t.Fatalf("rewind kept followups: %+v %v", followups, err)
	}
	req.ID = "stale-edit"
	if got := r.rpc(t, req); got.OK || got.Code != "not_found" {
		t.Fatalf("replaying an acknowledged edit must refuse its old sequence: %+v", got)
	}
}

func TestPhoneInputEditorRefusesInvalidSourcesAndPartialDrafts(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("editor validation", "", "")
	if err != nil {
		t.Fatal(err)
	}
	seed := func(kind, content string) int64 {
		t.Helper()
		ev := store.Event{ThreadID: th.ID, Kind: kind, Text: content, CreatedAt: time.Now()}
		if err := e.Store().AppendEvent(&ev); err != nil {
			t.Fatal(err)
		}
		return ev.Seq
	}
	answer := seed("agent_message", "The assistant's response")
	huge := seed(engine.KindUser, strings.Repeat("\n", MaxPushPayload))
	human := seed(engine.KindUser, "original text")
	command := "／goal a different objective"
	for _, tc := range []struct {
		seq         int64
		replacement *string
		code        string
	}{
		{0, nil, "bad_request"},
		{answer, nil, "bad_request"},
		{huge, nil, "too_large"},
		{human, &command, "edit_input_command"},
	} {
		got := Handle(e, config.RemoteConfig{}, Request{ID: "edit", Op: "edit_input", ThreadID: th.ID, FromEventSeq: tc.seq, Replacement: tc.replacement}, "relay", "session")
		if got.OK || got.Code != tc.code || got.Event != nil {
			t.Errorf("seq %d: wanted %s, got %+v", tc.seq, tc.code, got)
		}
	}
}
