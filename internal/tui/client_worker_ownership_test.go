package tui

import "testing"

func TestWorkerRetentionStaysInTraceInsteadOfBecomingATUINotice(t *testing.T) {
	for _, event := range []string{"workers_retained", "message"} {
		body := `{"kind":"workers_retained","agent_id":"manager","text":"{\"reason\":\"scheduled_check_failed\",\"owner_kind\":\"goal\",\"owner_id\":\"thread-7\",\"worker_ids\":[\"worker-7\"]}"}`
		if got := wireMessages(event, body); len(got) != 0 {
			t.Fatalf("internal ownership JSON became a visible notice: %+v", got)
		}
	}
	if got := wireMessages("future_notice", `{"text":"A visible notice."}`); len(got) != 1 || got[0].notice != "A visible notice." {
		t.Fatalf("ordinary unknown notices lost compatibility: %+v", got)
	}
}
