package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type mcpResultFixture string

func (mcpResultFixture) Info(context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "mcp_fixture"}, nil
}
func (f mcpResultFixture) InvokableRun(context.Context, string, ...tool.Option) (string, error) {
	return string(f), nil
}

func TestMCPResultLimitPreservesFailedStatusAndValidJSON(t *testing.T) {
	for _, code := range []string{"connection_failed", "session_expired", "cancelled"} {
		raw, _ := json.Marshal(map[string]any{"error": map[string]string{"code": code, "server_id": "external"}})
		out, err := WithResultLimit(mcpResultFixture(raw), 1).(tool.InvokableTool).InvokableRun(t.Context(), "{}")
		var result struct {
			IsError bool `json:"isError"`
			Error   struct {
				Code     string `json:"code"`
				ServerID string `json:"server_id"`
			} `json:"error"`
		}
		if err != nil || json.Unmarshal([]byte(out), &result) != nil || !result.IsError || result.Error.Code != code || result.Error.ServerID != "external" {
			t.Fatalf("adapter failure became success: %s %v", out, err)
		}
	}
	for _, failed := range []bool{true, false} {
		raw, _ := json.Marshal(map[string]any{"isError": failed, "content": []any{map[string]any{"type": "text", "text": strings.Repeat("multilingual 文本\n", 300)}}})
		for _, limit := range []int{1, 200, 1000, 20000} {
			wrapped := WithResultLimit(mcpResultFixture(raw), limit).(tool.InvokableTool)
			out, err := wrapped.InvokableRun(t.Context(), "{}")
			var got struct {
				IsError   bool `json:"isError"`
				Truncated bool `json:"truncated"`
			}
			if err != nil || json.Unmarshal([]byte(out), &got) != nil || got.IsError != failed {
				t.Fatalf("lost MCP failure %s %v", out, err)
			}
			if limit >= 200 && utf8.RuneCountInString(out) > limit {
				t.Fatalf("result exceeds budget %d > %d", utf8.RuneCountInString(out), limit)
			}
			if limit > utf8.RuneCount(raw) && out != string(raw) {
				t.Fatal("fitting MCP result changed")
			}
		}
	}
	if got := capMCPResult(strings.Repeat("not json", 100), 10); !strings.Contains(got, "truncated") {
		t.Fatal("malformed output was not bounded")
	}
}
