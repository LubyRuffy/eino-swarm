package engine

import (
	"bytes"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A 1×1 PNG. Magic bytes are the whole point of this fixture: a renamed HTML
// file must not ride in as vision input.
var tinyPNG = []byte{
	0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a,
	0x00, 0x00, 0x00, 0x0d, 0x49, 0x48, 0x44, 0x52,
	0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
	0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4, 0x89,
	0x00, 0x00, 0x00, 0x0a, 0x49, 0x44, 0x41, 0x54,
	0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05, 0x00, 0x01,
	0x0d, 0x0a, 0x2d, 0xb4,
	0x00, 0x00, 0x00, 0x00, 0x49, 0x45, 0x4e, 0x44, 0xae, 0x42, 0x60, 0x82,
}

func TestDecodeImagesAcceptsAPng(t *testing.T) {
	got, err := DecodeImages([]RawImage{{
		Name: "clip.png",
		MIME: "image/png",
		Data: base64.StdEncoding.EncodeToString(tinyPNG),
	}})
	if err != nil {
		t.Fatalf("DecodeImages: %v", err)
	}
	if len(got) != 1 || got[0].MIME != "image/png" || !bytes.Equal(got[0].Data, tinyPNG) {
		t.Fatalf("decoded wrong: %+v", got)
	}
	if got[0].Name != "clip.png" {
		t.Fatalf("kept the display name so the composer can label the chip: %q", got[0].Name)
	}
}

func TestDecodeImagesStripsADataURLPrefix(t *testing.T) {
	got, err := DecodeImages([]RawImage{{
		MIME: "image/png",
		Data: "data:image/png;base64," + base64.StdEncoding.EncodeToString(tinyPNG),
	}})
	if err != nil {
		t.Fatalf("DecodeImages: %v", err)
	}
	if !bytes.Equal(got[0].Data, tinyPNG) {
		t.Fatal("a data URL from the clipboard must still decode")
	}
}

func TestDecodeImagesRejectsANonImage(t *testing.T) {
	html := []byte("<!doctype html><script></script>")
	_, err := DecodeImages([]RawImage{{
		Name: "x.png",
		MIME: "image/png",
		Data: base64.StdEncoding.EncodeToString(html),
	}})
	if err == nil {
		t.Fatal("a renamed HTML file must not become vision input")
	}
}

func TestDecodeImagesRejectsAnUnknownType(t *testing.T) {
	_, err := DecodeImages([]RawImage{{
		MIME: "application/pdf",
		Data: base64.StdEncoding.EncodeToString(tinyPNG),
	}})
	if err == nil {
		t.Fatal("only image types belong on the vision path")
	}
}

func TestDecodeImagesRejectsAnOversizeImage(t *testing.T) {
	old := maxImageBytes
	maxImageBytes = 16
	t.Cleanup(func() { maxImageBytes = old })
	_, err := DecodeImages([]RawImage{{
		MIME: "image/png",
		Data: base64.StdEncoding.EncodeToString(tinyPNG),
	}})
	if err == nil {
		t.Fatal("an oversize paste must be refused before it fills the data dir")
	}
}

func TestDecodeImagesCapsHowManyCanBeSent(t *testing.T) {
	old := maxImages
	maxImages = 1
	t.Cleanup(func() { maxImages = old })
	one := RawImage{MIME: "image/png", Data: base64.StdEncoding.EncodeToString(tinyPNG)}
	_, err := DecodeImages([]RawImage{one, one})
	if err == nil {
		t.Fatal("a pile of pastes must not blow the model's image budget")
	}
}

func TestAttachedFilesNoticeNamesOnlyThisSend(t *testing.T) {
	got := withAttachedFiles("what is this", []store.Attachment{
		{RelPath: "uploads/current.csv"},
	})
	if !strings.Contains(got, "uploads/current.csv") {
		t.Fatalf("missing this send's path: %q", got)
	}
	if strings.Contains(got, "leftover") {
		t.Fatalf("must not invent other files: %q", got)
	}
	if !strings.HasPrefix(got, "what is this") {
		t.Fatalf("caption first: %q", got)
	}
	only := withAttachedFiles("", []store.Attachment{{RelPath: "uploads/current.csv"}})
	if !strings.Contains(only, "uploads/current.csv") || strings.Contains(only, "what is this") {
		t.Fatalf("a file-only send still has to name the path: %q", only)
	}
	if got := withAttachedFiles("hi", nil); got != "hi" {
		t.Fatalf("no files means the caption is untouched: %q", got)
	}
}

func TestResolveAttachedFilesCapsHowManyCanBeSent(t *testing.T) {
	old := maxAttachedFiles
	maxAttachedFiles = 1
	t.Cleanup(func() { maxAttachedFiles = old })
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if _, err := e.resolveAttachedFiles(th.ID, []string{"uploads/a.csv", "uploads/b.csv"}); err == nil {
		t.Fatal("a pile of attachments must not blow the model's path budget")
	}
}

func TestTitleFromInputPrefersTheCaptionThenTheFileName(t *testing.T) {
	files := []store.Attachment{{Name: "current.csv", RelPath: "uploads/current.csv"}}
	if got := titleFromInput("what is this", nil, files); got != "what is this" {
		t.Fatalf("caption wins: %q", got)
	}
	if got := titleFromInput("", nil, files); got != "current.csv" {
		t.Fatalf("file-only send should title from the file name: %q", got)
	}
}

func TestBuildUserMessagePutsImagesOnUserInputMultiContent(t *testing.T) {
	msg := BuildUserMessage("look", []ImageInput{{
		Name: "clip.png", MIME: "image/png", Data: tinyPNG,
	}})
	if msg.Role != schema.User {
		t.Fatalf("role=%s", msg.Role)
	}
	if msg.Content != "" {
		t.Fatalf("Content+MultiContent cannot both be set for OpenAI: %q", msg.Content)
	}
	if userMessageText(msg) != "look" {
		t.Fatalf("caption must still be readable from the text part: %q", userMessageText(msg))
	}
	if len(msg.UserInputMultiContent) != 2 {
		t.Fatalf("want text + image parts, got %+v", msg.UserInputMultiContent)
	}
	if msg.UserInputMultiContent[0].Type != schema.ChatMessagePartTypeText ||
		msg.UserInputMultiContent[0].Text != "look" {
		t.Fatalf("first part should be the caption: %+v", msg.UserInputMultiContent[0])
	}
	img := msg.UserInputMultiContent[1]
	if img.Type != schema.ChatMessagePartTypeImageURL || img.Image == nil ||
		img.Image.MIMEType != "image/png" || img.Image.Base64Data == nil {
		t.Fatalf("second part should be the image: %+v", img)
	}
	raw, err := base64.StdEncoding.DecodeString(*img.Image.Base64Data)
	if err != nil || !bytes.Equal(raw, tinyPNG) {
		t.Fatalf("image bytes did not round-trip: %v", err)
	}
}

func TestBuildUserMessageAllowsAnImageWithNoCaption(t *testing.T) {
	msg := BuildUserMessage("  ", []ImageInput{{MIME: "image/png", Data: tinyPNG}})
	if msg.Content != "" {
		t.Fatalf("blank caption must not become padding: %q", msg.Content)
	}
	if len(msg.UserInputMultiContent) != 1 ||
		msg.UserInputMultiContent[0].Type != schema.ChatMessagePartTypeImageURL {
		t.Fatalf("image-only input is still a request: %+v", msg.UserInputMultiContent)
	}
}

func TestAPastedImageReachesTheModelAsVision(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurnInput(th.ID, UserInput{
		Text:   "look",
		Images: []ImageInput{{Name: "clip.png", MIME: "image/png", Data: tinyPNG}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)

	events, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var saw []store.ImageRef
	for _, ev := range events {
		if ev.Kind == KindUser {
			if ev.Text != "look" {
				t.Fatalf("user_message text should stay the caption, not the bytes: %q", ev.Text)
			}
			saw = ev.Images
		}
	}
	if len(saw) != 1 || saw[0].MIME != "image/png" || saw[0].ID == "" {
		t.Fatalf("user_message must carry image refs, not the pixels: %+v", saw)
	}

	got, mime, err := e.ReadInputImage(th.ID, saw[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if mime != "image/png" || !bytes.Equal(got, tinyPNG) {
		t.Fatalf("stored image mime=%s len=%d", mime, len(got))
	}

	uploads := filepath.Join(e.WorkspaceDir(th.ID), "uploads")
	if entries, _ := os.ReadDir(uploads); len(entries) != 0 {
		t.Fatalf("a pasted image is vision input, not a workspace upload: %v", entries)
	}

	history, err := e.replayHistory(th.ID)
	if err != nil {
		t.Fatal(err)
	}
	var vision int
	for _, m := range history {
		if m != nil && m.Role == schema.User {
			for _, p := range m.UserInputMultiContent {
				if p.Type == schema.ChatMessagePartTypeImageURL {
					vision++
				}
			}
		}
	}
	if vision == 0 {
		t.Fatal("the next turn must still see the image, or follow-up questions go blind")
	}
}

func TestAnImageAloneStillStartsATurn(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurnInput(th.ID, UserInput{
		Images: []ImageInput{{MIME: "image/png", Data: tinyPNG}},
	})
	if err != nil {
		t.Fatalf("a screenshot with no caption is still a request: %v", err)
	}
	waitForTurn(t, e, turn.ID)
	if turn.UserText != "" {
		t.Fatalf("do not invent a caption: %q", turn.UserText)
	}
}

func TestDeletingAConversationRemovesItsInputImages(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurnInput(th.ID, UserInput{
		Text:   "look",
		Images: []ImageInput{{MIME: "image/png", Data: tinyPNG}},
	})
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)
	dir := e.Config().ThreadInputsDir(th.ID)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("images should have been written: %v", err)
	}
	if err := e.DeleteThread(th.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("deleting the conversation must take its pastes with it: %v", err)
	}
}

func TestAPastedImageIsNotConfusedWithAnEmptyMessage(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if _, err := e.StartTurnInput(th.ID, UserInput{}); err == nil {
		t.Fatal("still refuse a blank send")
	}
	if _, err := e.StartTurn(th.ID, "   "); err == nil {
		t.Fatal("whitespace is still empty")
	}
}

func TestSteerWithAnImageKeepsThePixelsOnTheTimeline(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn, err := e.StartTurn(th.ID, "start the work")
	if err != nil {
		t.Fatal(err)
	}
	var steered bool
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := e.SteerInput(th.ID, UserInput{
			Text:   "look here",
			Images: []ImageInput{{Name: "clip.png", MIME: "image/png", Data: tinyPNG}},
		}); err == nil {
			steered = true
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !steered {
		t.Fatal("could not steer with an image while the turn was running")
	}
	waitForTurn(t, e, turn.ID)

	events, _ := e.Replay(th.ID, 0)
	var saw bool
	for _, ev := range events {
		if ev.Kind == KindSteer && ev.Text == "look here" && len(ev.Images) == 1 {
			saw = true
		}
	}
	if !saw {
		t.Fatal("a steer that carried an image must still show up as a steer, with the image attached")
	}
}

func TestUserMessageTextPrefersContentThenParts(t *testing.T) {
	if got := userMessageText(schema.UserMessage("hi")); got != "hi" {
		t.Fatalf("got %q", got)
	}
	msg := &schema.Message{Role: schema.User, UserInputMultiContent: []schema.MessageInputPart{
		{Type: schema.ChatMessagePartTypeText, Text: "from parts"},
	}}
	if got := userMessageText(msg); got != "from parts" {
		t.Fatalf("multimodal captions live in the parts: %q", got)
	}
	if userMessageText(nil) != "" {
		t.Fatal("nil is empty")
	}
}

func TestDecodeImagesRejectsEmptyAndGarbage(t *testing.T) {
	if _, err := DecodeImages([]RawImage{{MIME: "image/png", Data: ""}}); err == nil {
		t.Fatal("empty payload")
	}
	if _, err := DecodeImages([]RawImage{{MIME: "image/png", Data: "!!!!"}}); err == nil {
		t.Fatal("garbage is not base64")
	}
}

func TestDecodeImagesAcceptsJpegMagic(t *testing.T) {
	jpeg := []byte{0xff, 0xd8, 0xff, 0xdb, 0x00}
	got, err := DecodeImages([]RawImage{{MIME: "image/jpg", Data: base64.StdEncoding.EncodeToString(jpeg)}})
	if err != nil {
		t.Fatal(err)
	}
	if got[0].MIME != "image/jpeg" {
		t.Fatalf("jpg is jpeg: %q", got[0].MIME)
	}
}

func TestReadInputImageRejectsACraftedId(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	if _, _, err := e.ReadInputImage(th.ID, "../zwai.db"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
	if _, _, err := e.ReadInputImage("nope", "img_ab"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown thread: %v", err)
	}
}

func TestSchemaUserKeepsTheCaptionWhenTheFileIsGone(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	msg := e.schemaUser(store.Message{
		ThreadID: th.ID,
		Content:  "look",
		Images:   []store.ImageRef{{ID: "img_deadbeef", MIME: "image/png"}},
	})
	if msg.Content != "look" || len(msg.UserInputMultiContent) != 0 {
		t.Fatalf("a missing file must not drop the caption: %+v", msg)
	}
}

func TestDecodeImagesRejectsTraversalNamesQuietly(t *testing.T) {
	got, err := DecodeImages([]RawImage{{
		Name: "../secret.png",
		MIME: "image/png",
		Data: base64.StdEncoding.EncodeToString(tinyPNG),
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got[0].Name, "..") || strings.Contains(got[0].Name, "/") {
		t.Fatalf("display name must not carry a path: %q", got[0].Name)
	}
}
