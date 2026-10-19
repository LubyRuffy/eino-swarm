package provider

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/schema"
)

// The review envelope includes generated lifecycle facts ahead of the source.
// Only actual transcript references may become the offline batch evidence.
var mockReviewSourceLine = regexp.MustCompile(`(?m)^\[seq=([0-9]+)[^\n]*\] (human|tool): ([^\n]+)`)

// Select an actual source excerpt so offline acceptance exercises the same
// evidence checks as a hosted provider. Invalid or absent references produce
// an empty batch rather than invented source evidence.
func mockReviewSource(msgs []*schema.Message) (int64, string) {
	humanOnly := mockReviewFailed(msgs)
	for _, m := range msgs {
		if m == nil || m.Role != schema.User {
			continue
		}
		source := plainUserText(m)
		if _, conversation, ok := strings.Cut(source, "Conversation to review:\n\n"); ok {
			source = conversation
		}
		for _, match := range mockReviewSourceLine.FindAllStringSubmatch(source, -1) {
			seq, err := strconv.ParseInt(match[1], 10, 64)
			if err != nil || seq <= 0 || humanOnly && match[2] != "human" {
				continue
			}
			quote := strings.TrimSuffix(strings.TrimSpace(match[3]), " …")
			if quote == "" {
				continue
			}
			runes := []rune(quote)
			if len(runes) > 96 {
				quote = string(runes[:96])
			}
			return seq, quote
		}
	}
	return 0, ""
}
