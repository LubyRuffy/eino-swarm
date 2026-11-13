package engine

import (
	"context"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// roleModel picks the fixed role for a FOFA account provider. ok is false when
// this install or provider is not in role mode, and the caller keeps its own
// model. An enabled role returns its id. Title and compact stay on when their
// switch is off, using conversation. Other roles return an empty model so the
// caller does not make the call.
func (e *Engine) roleModel(providerID, role string) (modelName string, ok bool) {
	if e == nil || e.cfg == nil || e.pool == nil || !e.cfg.Product.FOFA() {
		return "", false
	}
	enabled, configured := e.pool.AccountRole(providerID, role)
	if !configured {
		return "", false
	}
	if enabled {
		return role, true
	}
	if role == "title" || role == "compact" {
		return "conversation", true
	}
	return "", true
}

func (e *Engine) applyRoleModel(providerID, role, fallbackProvider, fallbackModel string) (string, string) {
	modelName, ok := e.roleModel(providerID, role)
	if !ok {
		return fallbackProvider, fallbackModel
	}
	return providerID, modelName
}

// readRoleImages sends user images through the ocr role and returns text for
// the conversation. used is false when the role is off, so the images stay on
// the original message.
func (e *Engine) readRoleImages(providerID, text string, images []ImageInput) (string, []ImageInput, bool, error) {
	modelName, ok := e.roleModel(providerID, "ocr")
	if !ok || modelName == "" || len(images) == 0 {
		return text, images, false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	builder, err := e.pool.ModelBuilder(ctx, providerID, modelName, "", nil)
	if err != nil {
		return "", nil, true, err
	}
	out, err := ocrGenerate(ctx, builder("ocr", "ocr"), []*schema.Message{
		schema.SystemMessage("Read the text in the images. Reply with only that text."),
		BuildUserMessage(text, images),
	})
	if err != nil {
		return "", nil, true, err
	}
	read := ""
	if out != nil {
		read = strings.TrimSpace(out.Content)
	}
	if read == "" {
		read = strings.TrimSpace(text)
	} else if strings.TrimSpace(text) != "" {
		read = strings.TrimSpace(text) + "\n" + read
	}
	return read, nil, true, nil
}

var ocrGenerate = func(ctx context.Context, m model.BaseChatModel, msgs []*schema.Message) (*schema.Message, error) {
	return m.Generate(ctx, msgs)
}
