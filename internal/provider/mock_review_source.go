package provider

import (
	"strings"

	"github.com/cloudwego/eino/schema"
)

// The review envelope carries generated evidence and catalog context ahead of
// the actual conversation. Derive the scripted subject from the human event,
// not those repeated headers, so different tasks do not collapse to one skill.
func mockReviewSubject(msgs []*schema.Message) string {
	src := firstUserText(msgs)
	if _, conversation, ok := strings.Cut(src, "Conversation to review:\n\n"); ok {
		src = conversation
	}
	for _, line := range strings.Split(src, "\n") {
		for _, role := range []string{"human: ", "user: "} {
			if text, ok := strings.CutPrefix(line, role); ok {
				return text
			}
		}
	}
	return src
}
