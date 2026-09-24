package remote

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
)

// Same 1x1 PNG the engine accepts. The test cares that magic bytes decide
// vision input, not the file name.
var phonePNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
	0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, 0x54,
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01,
	0x0d, 0x0a, 0x2d, 0xb4,
	0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestCatalogListsReadyModelsAndHidesEndpointSecrets(t *testing.T) {
	e := testEngine(t)
	cfg := e.Config()
	cfg.Models.Providers = append(cfg.Models.Providers, config.Provider{
		ID: "other", Label: "Other", Model: "side", BaseURL: "http://127.0.0.1:9", APIKey: "sekret",
	})
	resp := Handle(e, config.RemoteConfig{}, Request{ID: "c", Op: OpCatalog}, "relay", "s")
	if !resp.OK {
		t.Fatalf("%+v", resp)
	}
	if strings.Join(resp.ReasoningLevels, ",") != strings.Join(config.ReasoningEfforts(), ",") {
		t.Fatalf("levels %+v", resp.ReasoningLevels)
	}
	var sawMock, sawSide bool
	for _, m := range resp.Models {
		if m.ProviderID == cfg.Models.Default && m.Model == provider.MockModelName {
			sawMock = true
		}
		if m.ProviderID == "other" && m.Model == "side" {
			sawSide = true
		}
	}
	if !sawMock || !sawSide {
		t.Fatalf("models %+v", resp.Models)
	}
	raw, _ := json.Marshal(resp)
	if strings.Contains(string(raw), "sekret") || strings.Contains(string(raw), "127.0.0.1") {
		t.Fatal("catalog leaked an endpoint")
	}
}

func TestCatalogRefusesAFrameThatWouldNotFit(t *testing.T) {
	e := testEngine(t)
	prev := maxCatalogFrame
	maxCatalogFrame = 8
	t.Cleanup(func() { maxCatalogFrame = prev })
	resp := Handle(e, config.RemoteConfig{}, Request{ID: "c", Op: OpCatalog}, "relay", "s")
	if resp.OK || resp.Code != "too_large" {
		t.Fatalf("%+v", resp)
	}
}

func TestTuneSetsModelAndThinkingAndOpenShowsThem(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("t", "", "")
	if err != nil {
		t.Fatal(err)
	}
	high := config.ReasoningHigh
	ok := Handle(e, config.RemoteConfig{}, Request{
		ID: "u", Op: OpTune, ThreadID: th.ID,
		ProviderID: e.Config().Models.Default, Model: "side", Reasoning: &high,
	}, "relay", "s")
	if !ok.OK {
		t.Fatalf("%+v", ok)
	}
	got, err := e.Store().GetThread(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "side" || got.ReasoningEffort != config.ReasoningHigh {
		t.Fatalf("model %q effort %q", got.Model, got.ReasoningEffort)
	}
	opened := Handle(e, config.RemoteConfig{}, Request{ID: "o", Op: OpOpen, ThreadID: th.ID}, "relay", "s")
	if !opened.OK || opened.Detail == nil || opened.Detail.Model != "side" || opened.Detail.Reasoning != config.ReasoningHigh {
		t.Fatalf("detail %+v", opened.Detail)
	}
	blank := ""
	cleared := Handle(e, config.RemoteConfig{}, Request{
		ID: "z", Op: OpTune, ThreadID: th.ID, Reasoning: &blank,
	}, "relay", "s")
	if !cleared.OK {
		t.Fatalf("%+v", cleared)
	}
	got, _ = e.Store().GetThread(th.ID)
	if got.ReasoningEffort != "" || got.Model != "side" {
		t.Fatalf("clear effort %q model %q", got.ReasoningEffort, got.Model)
	}
	bogus := "nope"
	bad := Handle(e, config.RemoteConfig{}, Request{
		ID: "b", Op: OpTune, ThreadID: th.ID, Reasoning: &bogus,
	}, "relay", "s")
	if bad.OK {
		t.Fatal("unknown level accepted")
	}
	missing := Handle(e, config.RemoteConfig{}, Request{
		ID: "m", Op: OpTune, ProviderID: "no-such", Model: "side",
	}, "relay", "s")
	if missing.OK || missing.Code != "bad_request" {
		t.Fatalf("%+v", missing)
	}
}

func TestStartHonorsThePickedModelAndATextSendStillQueues(t *testing.T) {
	e := testEngine(t)
	low := config.ReasoningLow
	started := Handle(e, config.RemoteConfig{}, Request{
		ID: "s", Op: OpStart, Text: "begin",
		ProviderID: e.Config().Models.Default, Model: "side", Reasoning: &low,
	}, "relay", "s")
	if !started.OK || len(started.Threads) != 1 {
		t.Fatalf("%+v", started)
	}
	tid := started.Threads[0].ID
	got, err := e.Store().GetThread(tid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "side" || got.ReasoningEffort != config.ReasoningLow {
		t.Fatalf("model %q effort %q", got.Model, got.ReasoningEffort)
	}
	follow := Handle(e, config.RemoteConfig{}, Request{
		ID: "f", Op: OpSend, ThreadID: tid, Text: "later",
	}, "relay", "s")
	if !follow.OK || follow.Followups == nil || len(*follow.Followups) != 1 || (*follow.Followups)[0].Text != "later" {
		t.Fatalf("%+v", follow)
	}
	rows, err := e.ListFollowups(tid)
	if err != nil || len(rows) != 1 || rows[0].Text != "later" {
		t.Fatalf("followups %+v %v", rows, err)
	}
}

func TestAFullPutChunkFitsOnePairlinkFrame(t *testing.T) {
	body := bytes.Repeat([]byte{'a'}, PutChunkRaw)
	raw, err := json.Marshal(Request{
		V: ProtocolV, ID: "p", Op: OpPut, PutID: "chunk",
		Name: "blob.bin", MIME: "application/octet-stream",
		Part: 1, Parts: 1, Data: base64.StdEncoding.EncodeToString(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(raw) > MaxPushPayload {
		t.Fatalf("frame %d exceeds %d", len(raw), MaxPushPayload)
	}
	if len(raw) < MaxPushPayload/2 {
		t.Fatalf("frame %d is too small to be guarding the cap", len(raw))
	}
}

func TestPutRejectsAChunkOverTheFrameAndAWalkOutName(t *testing.T) {
	stage := NewStaging()
	over := bytes.Repeat([]byte{'b'}, PutChunkRaw+1)
	if _, err := stage.Accept(putReq("a", "blob.bin", "application/octet-stream", 1, 1, over)); err == nil {
		t.Fatal("oversize chunk")
	}
	if _, err := stage.Accept(putReq("a", "../blob.bin", "text/plain", 1, 1, []byte("x"))); err == nil {
		t.Fatal("traversal name")
	}
	if _, err := stage.Accept(Request{PutID: "bad id", Name: "a.txt", Part: 1, Parts: 1, Data: base64.StdEncoding.EncodeToString([]byte("x"))}); err == nil {
		t.Fatal("bad id")
	}
}

func TestPutStagesAcrossPartsAndARetryDoesNotDoubleCount(t *testing.T) {
	stage := NewStaging()
	payload := []byte("alpha-bravo")
	first := payload[:5]
	second := payload[5:]
	view, err := stage.Accept(putReq("up", "notes.txt", "text/plain", 1, 2, first))
	if err != nil || view.Ready {
		t.Fatalf("first %+v %v", view, err)
	}
	again, err := stage.Accept(putReq("up", "notes.txt", "text/plain", 1, 2, first))
	if err != nil || again.Ready {
		t.Fatalf("retry %+v %v", again, err)
	}
	done, err := stage.Accept(putReq("up", "notes.txt", "text/plain", 2, 2, second))
	if err != nil || !done.Ready || done.Kind != "file" {
		t.Fatalf("done %+v %v", done, err)
	}
	got, err := stage.Take([]string{"up"})
	if err != nil || len(got) != 1 || !bytes.Equal(got[0].Data, payload) {
		t.Fatalf("%v %+v", err, got)
	}
	if _, err := stage.Take([]string{"up"}); err == nil {
		t.Fatal("taken twice")
	}
}

func TestPutCapAndUnfinishedTakeLeaveTheBytesStaged(t *testing.T) {
	prev := maxStagedBytes
	maxStagedBytes = 4
	t.Cleanup(func() { maxStagedBytes = prev })
	stage := NewStaging()
	if _, err := stage.Accept(putReq("big", "a.txt", "text/plain", 1, 1, []byte("12345"))); err == nil {
		t.Fatal("cap")
	}
	if _, err := stage.Accept(putReq("ok", "a.txt", "text/plain", 1, 2, []byte("12"))); err != nil {
		t.Fatal(err)
	}
	if _, err := stage.Take([]string{"ok"}); err == nil {
		t.Fatal("unfinished take")
	}
	if _, err := stage.Accept(putReq("ok", "a.txt", "text/plain", 2, 2, []byte("34"))); err != nil {
		t.Fatal(err)
	}
	got, err := stage.Take([]string{"ok"})
	if err != nil || string(got[0].Data) != "1234" {
		t.Fatalf("%v %+v", err, got)
	}
}

func TestStartWithAFileAndAnImageUsesTheRightSlot(t *testing.T) {
	e := testEngine(t)
	stage := NewStaging()
	if err := stageAll(stage, "file", "side.txt", "text/plain", []byte("hello side")); err != nil {
		t.Fatal(err)
	}
	if err := stageAll(stage, "pic", "shot.png", "image/png", phonePNG); err != nil {
		t.Fatal(err)
	}
	// A second upload that the send does not name must stay out of the workspace.
	if err := stageAll(stage, "left", "other.txt", "text/plain", []byte("leave me")); err != nil {
		t.Fatal(err)
	}
	started := HandleWith(e, config.RemoteConfig{}, stage, Request{
		ID: "s", Op: OpStart, Text: "look", Puts: []string{"file", "pic"},
	}, "relay", "s")
	if !started.OK || len(started.Threads) != 1 {
		t.Fatalf("%+v", started)
	}
	tid := started.Threads[0].ID
	atts, err := e.Store().ListAttachments(tid)
	if err != nil {
		t.Fatal(err)
	}
	if len(atts) != 1 || atts[0].Name != "side.txt" {
		t.Fatalf("attachments %+v", atts)
	}
	body, err := os.ReadFile(filepath.Join(e.WorkspaceDir(tid), atts[0].RelPath))
	if err != nil || string(body) != "hello side" {
		t.Fatalf("file %q %v", body, err)
	}
	entries, err := os.ReadDir(e.Config().ThreadInputsDir(tid))
	if err != nil || len(entries) != 1 {
		t.Fatalf("images %v %v", entries, err)
	}
	left, err := stage.Take([]string{"left"})
	if err != nil || string(left[0].Data) != "leave me" {
		t.Fatalf("unnamed upload consumed: %v %+v", err, left)
	}
}

func TestPngBytesNamedAsTextStayAFile(t *testing.T) {
	e := testEngine(t)
	stage := NewStaging()
	if err := stageAll(stage, "bin", "notes.txt", "text/plain", phonePNG); err != nil {
		t.Fatal(err)
	}
	started := HandleWith(e, config.RemoteConfig{}, stage, Request{
		ID: "s", Op: OpStart, Puts: []string{"bin"},
	}, "relay", "s")
	if !started.OK {
		t.Fatalf("%+v", started)
	}
	tid := started.Threads[0].ID
	atts, err := e.Store().ListAttachments(tid)
	if err != nil || len(atts) != 1 {
		t.Fatalf("%v %+v", err, atts)
	}
	if _, err := os.Stat(e.Config().ThreadInputsDir(tid)); !os.IsNotExist(err) {
		t.Fatalf("text claim became vision: %v", err)
	}
}

func TestAClaimedImageThatIsNotOneFailsAndDoesNotStart(t *testing.T) {
	e := testEngine(t)
	stage := NewStaging()
	if err := stageAll(stage, "bad", "shot.png", "image/png", []byte("not a png")); err != nil {
		t.Fatal(err)
	}
	resp := HandleWith(e, config.RemoteConfig{}, stage, Request{
		ID: "s", Op: OpStart, Text: "look", Puts: []string{"bad"},
	}, "relay", "s")
	if resp.OK {
		t.Fatal("bad image started a turn")
	}
}

func TestRunningSendWithAnImageInjectsInsteadOfQueueing(t *testing.T) {
	e := testEngine(t)
	started := Handle(e, config.RemoteConfig{}, Request{ID: "s", Op: OpStart, Text: "begin"}, "relay", "s")
	if !started.OK {
		t.Fatalf("%+v", started)
	}
	tid := started.Threads[0].ID
	stage := NewStaging()
	if err := stageAll(stage, "pic", "shot.png", "image/png", phonePNG); err != nil {
		t.Fatal(err)
	}
	sent := HandleWith(e, config.RemoteConfig{}, stage, Request{
		ID: "n", Op: OpSend, ThreadID: tid, Text: "see this", Puts: []string{"pic"},
	}, "relay", "s")
	if !sent.OK {
		t.Fatalf("%+v", sent)
	}
	rows, err := e.ListFollowups(tid)
	if err != nil || len(rows) != 0 {
		t.Fatalf("attachment was queued %+v %v", rows, err)
	}
	entries, err := os.ReadDir(e.Config().ThreadInputsDir(tid))
	if err != nil || len(entries) != 1 {
		t.Fatalf("images %v %v", entries, err)
	}
}

func TestSendNamesPutsWithoutAStage(t *testing.T) {
	e := testEngine(t)
	resp := Handle(e, config.RemoteConfig{}, Request{
		ID: "s", Op: OpStart, Text: "x", Puts: []string{"missing"},
	}, "relay", "s")
	if resp.OK {
		t.Fatal("puts without a stage")
	}
}

func TestSteerOnAnIdleThreadStartsTheTurn(t *testing.T) {
	e := testEngine(t)
	th, err := e.CreateThread("idle", "", "")
	if err != nil {
		t.Fatal(err)
	}
	resp := Handle(e, config.RemoteConfig{}, Request{
		ID: "st", Op: OpSteer, ThreadID: th.ID, Text: "go",
	}, "relay", "s")
	if !resp.OK || !e.Status(th.ID).Running {
		t.Fatalf("%+v running=%v", resp, e.Status(th.ID).Running)
	}
}

func TestABadSelectionDoesNotLeaveAnEmptyConversation(t *testing.T) {
	e := testEngine(t)
	before, err := e.Store().ListThreads(true, "")
	if err != nil {
		t.Fatal(err)
	}
	resp := Handle(e, config.RemoteConfig{}, Request{
		ID: "s", Op: OpStart, Text: "nope", ProviderID: "missing", Model: "nope",
	}, "relay", "s")
	if resp.OK {
		t.Fatal("unknown model started")
	}
	after, err := e.Store().ListThreads(true, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != len(before) {
		t.Fatalf("orphan threads %d -> %d", len(before), len(after))
	}
	if modelViews(nil) != nil {
		t.Fatal("nil engine listed models")
	}
}

func TestSendAndSteerRefuseAMissingThreadAndAMissingUpload(t *testing.T) {
	e := testEngine(t)
	for _, op := range []string{OpSend, OpSteer} {
		resp := Handle(e, config.RemoteConfig{}, Request{ID: "x", Op: op, Text: "hi"}, "relay", "s")
		if resp.OK || resp.Code != "bad_request" {
			t.Fatalf("%s %+v", op, resp)
		}
		resp = Handle(e, config.RemoteConfig{}, Request{
			ID: "p", Op: op, ThreadID: "nope", Text: "hi", Puts: []string{"gone"},
		}, "relay", "s")
		if resp.OK {
			t.Fatalf("%s accepted puts with no staging", op)
		}
	}
	th, err := e.CreateThread("t", "", "")
	if err != nil {
		t.Fatal(err)
	}
	bogus := "nope"
	resp := Handle(e, config.RemoteConfig{}, Request{
		ID: "r", Op: OpSend, ThreadID: th.ID, Text: "hi", Reasoning: &bogus,
	}, "relay", "s")
	if resp.OK {
		t.Fatal("unknown level on send")
	}
	stage := NewStaging()
	resp = HandleWith(e, config.RemoteConfig{}, stage, Request{
		ID: "u", Op: OpSend, ThreadID: th.ID, Text: "hi", Puts: []string{"gone"},
	}, "relay", "s")
	if resp.OK {
		t.Fatal("unknown upload was consumed")
	}
	tuned := Handle(e, config.RemoteConfig{}, Request{
		ID: "t", Op: OpTune, ThreadID: "missing-thread",
		ProviderID: e.Config().Models.Default, Model: "side",
	}, "relay", "s")
	if tuned.OK {
		t.Fatal("tuned a thread that does not exist")
	}
	resp = Handle(e, config.RemoteConfig{}, Request{
		ID: "st", Op: OpSteer, ThreadID: th.ID, Text: "hi", Reasoning: &bogus,
	}, "relay", "s")
	if resp.OK {
		t.Fatal("unknown level on steer")
	}
	resp = HandleWith(e, config.RemoteConfig{}, stage, Request{
		ID: "su", Op: OpSteer, ThreadID: th.ID, Text: "hi", Puts: []string{"gone"},
	}, "relay", "s")
	if resp.OK {
		t.Fatal("steer consumed an unknown upload")
	}
}

func TestPutThroughTheRPCAndTheChunkRulesAPhoneCanBreak(t *testing.T) {
	e := testEngine(t)
	closed := Handle(e, config.RemoteConfig{}, Request{
		ID: "p", Op: OpPut, PutID: "a", Name: "a.txt", MIME: "text/plain",
		Part: 1, Parts: 1, Data: base64.StdEncoding.EncodeToString([]byte("x")),
	}, "relay", "s")
	if closed.OK || closed.Code != "bad_request" {
		t.Fatalf("nil stage %+v", closed)
	}
	stage := NewStaging()
	jpg := putReq("a", "shot.jpg", "", 1, 1, []byte("not-magic"))
	jpg.ID, jpg.Op = "p", OpPut
	ok := HandleWith(e, config.RemoteConfig{}, stage, jpg, "relay", "s")
	if !ok.OK || ok.Put == nil || !ok.Put.Ready || ok.Put.Kind != "image" {
		t.Fatalf("jpg name without a type %+v", ok.Put)
	}
	bad := HandleWith(e, config.RemoteConfig{}, stage, Request{
		ID: "b", Op: OpPut, PutID: "b", Name: "a.txt", Part: 1, Parts: 1, Data: "%%%",
	}, "relay", "s")
	if bad.OK || bad.Code != "bad_request" {
		t.Fatalf("bad base64 %+v", bad)
	}
	if _, err := stage.Accept(putReq("m", "a.txt", strings.Repeat("t", 129), 1, 1, []byte("x"))); err == nil {
		t.Fatal("long type")
	}
	if _, err := stage.Accept(putReq("m", "a.txt", "text/plain", 0, 1, []byte("x"))); err == nil {
		t.Fatal("part 0")
	}
	if _, err := stage.Accept(putReq("m", "a.txt", "text/plain", 1, 0, []byte("x"))); err == nil {
		t.Fatal("no parts")
	}
	if _, err := stage.Accept(putReq(strings.Repeat("a", 65), "a.txt", "text/plain", 1, 1, []byte("x"))); err == nil {
		t.Fatal("long id")
	}
	if _, err := stage.Accept(putReq("m", "", "text/plain", 1, 1, []byte("x"))); err == nil {
		t.Fatal("empty name")
	}
	if _, err := stage.Accept(putReq("m", ".", "text/plain", 1, 1, []byte("x"))); err == nil {
		t.Fatal("dot name")
	}
	if _, err := stage.Accept(putReq("a", "other.txt", "", 1, 1, []byte("x"))); err == nil {
		t.Fatal("shape change")
	}
	if _, err := stage.Accept(putReq("a", "shot.jpg", "", 1, 1, []byte("other"))); err == nil {
		t.Fatal("different bytes")
	}
	bare := &Staging{}
	if _, err := bare.Accept(putReq("z", "a.txt", "text/plain", 1, 1, []byte("x"))); err != nil {
		t.Fatal(err)
	}
	var none *Staging
	if _, err := none.Accept(putReq("z", "a.txt", "text/plain", 1, 1, []byte("x"))); err == nil {
		t.Fatal("nil accept")
	}
	if _, err := none.Take([]string{"z"}); err == nil {
		t.Fatal("nil take")
	}
	if blobs, err := stage.Take(nil); err != nil || blobs != nil {
		t.Fatalf("empty take %v %+v", err, blobs)
	}
	if _, err := bare.Take([]string{"z", "z"}); err == nil {
		t.Fatal("duplicate id")
	}
	full := NewStaging()
	for i := 0; i < maxStagedPuts; i++ {
		id := "p" + strings.Repeat("x", 0) + string(rune('a'+i%26)) + strings.Repeat("q", i)
		if _, err := full.Accept(putReq(id, "a.txt", "text/plain", 1, 1, []byte("x"))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := full.Accept(putReq("overflow", "a.txt", "text/plain", 1, 1, []byte("x"))); err == nil {
		t.Fatal("too many uploads")
	}
	hole := &partial{parts: 1, got: map[int][]byte{1: {}}}
	if hole.complete() {
		t.Fatal("empty part counted as finished")
	}
}

func putReq(id, name, mime string, part, parts int, data []byte) Request {
	return Request{
		PutID: id, Name: name, MIME: mime, Part: part, Parts: parts,
		Data: base64.StdEncoding.EncodeToString(data),
	}
}

func stageAll(stage *Staging, id, name, mime string, data []byte) error {
	mid := len(data) / 2
	if mid == 0 {
		mid = len(data)
	}
	if _, err := stage.Accept(putReq(id, name, mime, 1, 2, data[:mid])); err != nil {
		return err
	}
	_, err := stage.Accept(putReq(id, name, mime, 2, 2, data[mid:]))
	return err
}
