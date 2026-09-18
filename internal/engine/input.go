package engine

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// Caps on pasted images. A screenshot is small; a raw dump of a disk image
// is not, and it would land in the data directory and in every later turn's
// prompt. Tests lower these so they do not have to allocate megabytes.
var (
	maxImageBytes = 8 << 20
	maxImages     = 8
)

// RawImage is one pasted image as the HTTP body carries it: base64 pixels
// plus the type the browser claimed. DecodeImages is what makes that safe.
type RawImage struct {
	Name string
	MIME string
	Data string
}

// ImageInput is decoded pixels ready to store and to send as vision input.
type ImageInput struct {
	Name string
	MIME string
	Data []byte
}

// UserInput is one send from the composer: a caption, pasted images, files
// just attached to this message, or a mix.
type UserInput struct {
	Text   string
	Images []ImageInput
	// Files are workspace-relative paths of uploads attached to THIS send.
	// They already live in uploads/; naming them here is what stops the
	// model treating an older leftover as "the" file.
	Files []string
	// ContinueGoal is an engine-started turn that keeps pursuing an open
	// standing objective. The transcript records goal_continued, not a
	// human user_message.
	ContinueGoal bool
	// ContinueSchedule is an engine-started turn because a wait fired.
	// The transcript records schedule_fired, not a human user_message.
	// The ticker (later) is what sets this; tools still read the flag on
	// the stored turn.
	ContinueSchedule bool
	// ScheduleID is the wait that started this turn when ContinueSchedule
	// is set.
	ScheduleID string
	// FromEventSeq, when set, truncates the conversation at that user_message
	// and starts again from there. It is a rewind, not a new turn on top.
	FromEventSeq int64
	// ImplementPlan is an engine-started turn that executes an accepted plan.
	// The transcript records plan_implemented, not a human user_message.
	ImplementPlan bool
}

// DecodeImages turns the wire payload into pixels. It refuses anything that
// is not a real image: a renamed HTML file must not become inline content
// the app then serves back from its own origin.
func DecodeImages(in []RawImage) ([]ImageInput, error) {
	if len(in) == 0 {
		return nil, nil
	}
	if len(in) > maxImages {
		return nil, fmt.Errorf("engine: at most %d images can be sent with a message", maxImages)
	}
	out := make([]ImageInput, 0, len(in))
	for _, raw := range in {
		img, err := decodeImage(raw)
		if err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	return out, nil
}

func decodeImage(raw RawImage) (ImageInput, error) {
	payload := strings.TrimSpace(raw.Data)
	if i := strings.Index(payload, ","); i >= 0 && strings.Contains(strings.ToLower(payload[:i]), "base64") {
		payload = payload[i+1:]
	}
	if payload == "" {
		return ImageInput{}, fmt.Errorf("engine: an image was attached with no data")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return ImageInput{}, fmt.Errorf("engine: image data was not valid base64")
	}
	if len(data) == 0 {
		return ImageInput{}, fmt.Errorf("engine: an image was attached with no data")
	}
	if len(data) > maxImageBytes {
		return ImageInput{}, fmt.Errorf("engine: an image is larger than the %d MiB limit", maxImageBytes>>20)
	}
	sniffed := sniffImageMIME(data)
	if sniffed == "" {
		return ImageInput{}, fmt.Errorf("engine: that file is not a supported image")
	}
	if claimed := strings.ToLower(strings.TrimSpace(raw.MIME)); claimed != "" {
		if normalizeImageMIME(raw.MIME) != sniffed {
			return ImageInput{}, fmt.Errorf("engine: that file is not a supported image")
		}
	}
	return ImageInput{
		Name: sanitizeImageName(raw.Name, sniffed),
		MIME: sniffed,
		Data: data,
	}, nil
}

func normalizeImageMIME(mime string) string {
	switch strings.ToLower(strings.TrimSpace(mime)) {
	case "image/png":
		return "image/png"
	case "image/jpeg", "image/jpg":
		return "image/jpeg"
	case "image/gif":
		return "image/gif"
	case "image/webp":
		return "image/webp"
	default:
		return ""
	}
}

func sniffImageMIME(b []byte) string {
	switch {
	case len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}):
		return "image/png"
	case len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
		return "image/jpeg"
	case len(b) >= 6 && (bytes.Equal(b[:6], []byte("GIF87a")) || bytes.Equal(b[:6], []byte("GIF89a"))):
		return "image/gif"
	case len(b) >= 12 && bytes.Equal(b[:4], []byte("RIFF")) && bytes.Equal(b[8:12], []byte("WEBP")):
		return "image/webp"
	default:
		return ""
	}
}

func sanitizeImageName(name, mime string) string {
	name = filepath.Base(strings.TrimSpace(name))
	name = strings.Map(func(r rune) rune {
		if r == os.PathSeparator || r == '/' || r == '\\' || r == ':' {
			return -1
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, name)
	if name == "" || name == "." || name == ".." {
		return "image" + extForMIME(mime)
	}
	return name
}

func extForMIME(mime string) string {
	switch mime {
	case "image/jpeg":
		return ".jpg"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	default:
		return ".png"
	}
}

// BuildUserMessage is the multimodal user turn the model actually sees.
// OpenAI-compatible clients refuse to marshal Content and MultiContent on
// the same message, so a vision send keeps the caption in a text part only.
// Traces and the offline script read it via userMessageText. Image-only
// sends have no text part.
func BuildUserMessage(text string, images []ImageInput) *schema.Message {
	text = strings.TrimSpace(text)
	if len(images) == 0 {
		return schema.UserMessage(text)
	}
	parts := make([]schema.MessageInputPart, 0, len(images)+1)
	if text != "" {
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeText,
			Text: text,
		})
	}
	for _, img := range images {
		b64 := base64.StdEncoding.EncodeToString(img.Data)
		mime := img.MIME
		parts = append(parts, schema.MessageInputPart{
			Type: schema.ChatMessagePartTypeImageURL,
			Image: &schema.MessageInputImage{
				MessagePartCommon: schema.MessagePartCommon{
					Base64Data: &b64,
					MIMEType:   mime,
				},
				Detail: schema.ImageURLDetailAuto,
			},
		})
	}
	return &schema.Message{Role: schema.User, UserInputMultiContent: parts}
}

func userMessageText(m *schema.Message) string {
	if m == nil {
		return ""
	}
	if t := strings.TrimSpace(m.Content); t != "" {
		return t
	}
	for _, p := range m.UserInputMultiContent {
		if p.Type == schema.ChatMessagePartTypeText {
			if t := strings.TrimSpace(p.Text); t != "" {
				return t
			}
		}
	}
	return ""
}

func (e *Engine) saveInputImages(threadID string, in []ImageInput) ([]store.ImageRef, error) {
	if len(in) == 0 {
		return nil, nil
	}
	dir := e.cfg.ThreadInputsDir(threadID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("engine: store images: %w", err)
	}
	refs := make([]store.ImageRef, 0, len(in))
	for _, img := range in {
		id := store.NewID("img_")
		if err := os.WriteFile(filepath.Join(dir, id), img.Data, 0o600); err != nil {
			return nil, fmt.Errorf("engine: store image: %w", err)
		}
		refs = append(refs, store.ImageRef{ID: id, Name: img.Name, MIME: img.MIME})
	}
	return refs, nil
}

func safeImageID(id string) bool {
	if !strings.HasPrefix(id, "img_") {
		return false
	}
	rest := id[len("img_"):]
	if rest == "" {
		return false
	}
	for _, c := range rest {
		if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') {
			continue
		}
		return false
	}
	return true
}

// ReadInputImage returns the pasted pixels so the transcript can render them
// and a later turn can send them again. The id is the only path component;
// anything else is not found rather than a traversal.
func (e *Engine) ReadInputImage(threadID, id string) ([]byte, string, error) {
	if _, err := e.store.GetThread(threadID); err != nil {
		return nil, "", err
	}
	if !safeImageID(id) {
		return nil, "", store.ErrNotFound
	}
	root := e.cfg.ThreadInputsDir(threadID)
	path := filepath.Join(root, id)
	if rel, err := filepath.Rel(root, path); err != nil || strings.HasPrefix(rel, "..") {
		return nil, "", store.ErrNotFound
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", store.ErrNotFound
		}
		return nil, "", fmt.Errorf("engine: read image: %w", err)
	}
	mime := sniffImageMIME(data)
	if mime == "" {
		return nil, "", store.ErrNotFound
	}
	return data, mime, nil
}

func (e *Engine) removeInputImages(threadID string) {
	dir := e.cfg.ThreadInputsDir(threadID)
	if err := os.RemoveAll(dir); err != nil {
		e.log.Warn("could not remove pasted images", "thread", threadID, "err", err)
	}
}

func (e *Engine) schemaUser(row store.Message) *schema.Message {
	if len(row.Images) == 0 {
		return schema.UserMessage(row.Content)
	}
	images := make([]ImageInput, 0, len(row.Images))
	for _, ref := range row.Images {
		data, mime, err := e.ReadInputImage(row.ThreadID, ref.ID)
		if err != nil {
			e.log.Warn("could not reload a pasted image", "thread", row.ThreadID, "image", ref.ID, "err", err)
			continue
		}
		if ref.MIME != "" {
			mime = ref.MIME
		}
		images = append(images, ImageInput{Name: ref.Name, MIME: mime, Data: data})
	}
	if len(images) == 0 {
		return schema.UserMessage(row.Content)
	}
	return BuildUserMessage(row.Content, images)
}

func imagesFromMessage(m *schema.Message) []ImageInput {
	if m == nil {
		return nil
	}
	var out []ImageInput
	for _, p := range m.UserInputMultiContent {
		if p.Type != schema.ChatMessagePartTypeImageURL || p.Image == nil || p.Image.Base64Data == nil {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(*p.Image.Base64Data)
		if err != nil {
			continue
		}
		out = append(out, ImageInput{MIME: p.Image.MIMEType, Data: raw})
	}
	return out
}

func titleFromInput(text string, images []ImageInput, files []store.Attachment) string {
	if t := titleFrom(text); t != "" {
		return t
	}
	if len(images) > 0 {
		return titleFrom(images[0].Name)
	}
	if len(files) > 0 {
		return titleFrom(files[0].Name)
	}
	return ""
}
