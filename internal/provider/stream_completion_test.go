package provider

import (
	"io"
	"strings"
	"testing"
	"testing/iotest"
)

func TestStreamCompletionObservesOnlyTheSSETerminatorWithoutChangingBytes(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		done bool
	}{
		{"complete", "data: [DONE]\n\n", true},
		{"CRLF and whitespace", " \tdata:\t [DONE] \r\n", true},
		{"delta then complete", backoffChunk + "data:[DONE]\n", true},
		{"delta only", backoffChunk, false},
		{"content is not framing", "data: {\"content\":\"data: [DONE]\"}\n", false},
		{"incomplete line", "data: [DONE]", false},
		{"incomplete marker", "data: [DON]\n", false},
		{"trailing content", "data: [DONE]x\n", false},
		{"spaces inside data", "da ta: [DONE]\n", false},
		{"spaces inside marker", "data: [DO NE]\n", false},
		{"large frame then complete", "data: " + strings.Repeat("x", 1<<18) + "\ndata:[DONE]\n", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, oneByte := range []bool{false, true} {
				var reader io.Reader = strings.NewReader(tc.body)
				if oneByte {
					reader = iotest.OneByteReader(reader)
				} else {
					reader = iotest.DataErrReader(reader)
				}
				completion := &streamCompletion{}
				body := &streamCompletionBody{ReadCloser: io.NopCloser(reader), completion: completion}
				got, err := io.ReadAll(body)
				if err != nil || string(got) != tc.body {
					t.Fatalf("observer changed response bytes: oneByte=%v got=%q err=%v", oneByte, got, err)
				}
				if completion.done.Load() != tc.done {
					t.Fatalf("terminal status: oneByte=%v got=%v want=%v", oneByte, completion.done.Load(), tc.done)
				}
				if err := body.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
