package provider

import (
	"strings"
	"testing"
)

func TestMockCodeAndMathSourceIsGeneric(t *testing.T) {
	got := mockCodeAndMathSource()
	if !strings.Contains(got, "```go") || !strings.Contains(got, "$n$") {
		t.Fatalf("offline extras must include a tagged fence and inline math:\n%s", got)
	}
	if !strings.Contains(got, "func ") {
		t.Fatal("the go fence must include a keyword the highlighter can paint")
	}
	for _, leak := range []string{"revenue", "sales", "month", "katex", "knapsack"} {
		if strings.Contains(strings.ToLower(got), leak) {
			t.Fatalf("offline extras leaked %q:\n%s", leak, got)
		}
	}
}

func TestMockCodeAndMathBlockStaysQuietInGoTest(t *testing.T) {
	if mockCodeAndMathBlock() != "" {
		t.Fatal("go test must not stream the extras; they blow the engine budget")
	}
	got := mockAnswer("the task", []string{"aa", "bbbb"})
	if strings.Contains(got, "```go") || strings.Contains(got, "$n$") {
		t.Fatalf("go-test mockAnswer must stay without extras:\n%s", got)
	}
}
