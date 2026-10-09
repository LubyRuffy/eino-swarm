package provider

import (
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestOfflineTaskCoordinationUsesRealToolNamesAndPeerIDs(t *testing.T) {
	t.Setenv("ZWAI_MOCK_TASK_COORDINATION", "")
	if got := mockTaskCoordination(nil); got != nil {
		t.Fatal("normal offline turns changed")
	}
	t.Setenv("ZWAI_MOCK_TASK_COORDINATION", "1")
	list := mockTaskCoordination(nil)
	if len(list.ToolCalls) != 1 || list.ToolCalls[0].Function.Name != "list_tasks" {
		t.Fatalf("discover=%+v", list)
	}
	msgs := []*schema.Message{list, nil, schema.ToolMessage(`{"tasks":[]}`, "peer-list")}
	if got := mockTaskCoordination(msgs); len(got.ToolCalls) != 0 {
		t.Fatal("empty discovery invented a peer")
	}
	msgs[2] = schema.ToolMessage(`{"tasks":[{"thread_id":"discovered-peer"}]}`, "peer-list")
	send := mockTaskCoordination(msgs)
	if len(send.ToolCalls) != 1 || send.ToolCalls[0].Function.Name != "send_task_message" || !strings.Contains(send.ToolCalls[0].Function.Arguments, "discovered-peer") {
		t.Fatalf("send=%+v", send)
	}
	msgs = append(msgs, send, schema.ToolMessage(`{"ok":true}`, "peer-send"))
	read := mockTaskCoordination(msgs)
	if len(read.ToolCalls) != 1 || read.ToolCalls[0].Function.Name != "read_task_messages" {
		t.Fatalf("read=%+v", read)
	}
	msgs = append(msgs, read, schema.ToolMessage(`{"messages":[]}`, "peer-read"))
	if got := mockTaskCoordination(msgs); !strings.Contains(got.Content, "agreement is still required") || len(got.ToolCalls) != 0 {
		t.Fatalf("final=%+v", got)
	}
	if got := managerScript(1, nil); got.ToolCalls[0].Function.Name != "list_tasks" {
		t.Fatal("manager bypassed the coordination script")
	}
}
