package engine

import (
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestDigestListsIdentifiersAndNeverAttributeValues(t *testing.T) {
	d := store.GraphDigest{
		Revision: 4, Subjects: 3, Retracted: 1, ByKind: map[string]int{"ip": 2, "repo": 1},
		CurrentByKind: map[string]int{"ip": 1, "repo": 1}, OpenTodoTotal: 20,
		Todos:      []store.GraphTodo{{ID: "td_1", Status: "doing", Version: 2, Title: strings.Repeat("long ", 40), About: "sj_9"}},
		Recent:     []store.GraphSubject{{ID: "sj_9", Kind: "repo", Status: "lead", Version: 1, Name: "acme/app", Attrs: map[string]any{"credential": "must-not-appear"}}},
		Unrecorded: 3,
	}
	text := renderGraphDigest(d)
	for _, want := range []string{"revision 4", "ip 2", "repo 1", "1 retracted", "current ip 1, repo 1", "first 1", "td_1 doing v2", "[about sj_9]", "sj_9 repo lead v1: acme/app", "3 tool results"} {
		if !strings.Contains(text, want) {
			t.Errorf("digest lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "must-not-appear") {
		t.Fatal("the digest carried an attribute value")
	}
	if strings.Contains(text, strings.Repeat("long ", 40)) {
		t.Fatal("a long title was not clipped")
	}
}

// The digest rides on every request but is not an input message. If a stored
// transcript kept it, the tail past the input count would start one message
// early and a retried turn would store its last tool result twice.
func TestTranscriptPersistenceIgnoresTheGraphDigest(t *testing.T) {
	digest := schema.SystemMessage(graphDigestPrefix + "revision 3")
	system := schema.SystemMessage("instructions")
	user := schema.UserMessage("do the work")
	got := withoutGraphDigest([]adk.Message{system, digest, user, digest})
	if len(got) != 2 || got[0] != system || got[1] != user {
		t.Fatalf("digest not removed: %+v", got)
	}
	plain := []adk.Message{system, user}
	if out := withoutGraphDigest(plain); &out[0] != &plain[0] {
		t.Fatal("a transcript without a digest was copied")
	}
	if len(withoutGraphDigest(nil)) != 0 {
		t.Fatal("empty transcript changed")
	}
}
