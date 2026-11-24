package store

import (
	"errors"
	"fmt"
	"testing"
)

func TestWikiScanAndReviewCheckpointsReuseOnlyAcknowledgedSources(t *testing.T) {
	st := openTestStore(t)
	ctx := t.Context()
	project := Project{Name: "Project"}
	if err := st.CreateProject(&project); err != nil {
		t.Fatal(err)
	}
	thread := Thread{ProjectID: project.ID}
	if err := st.CreateThread(&thread); err != nil {
		t.Fatal(err)
	}
	addTurn := func(status string) Turn {
		t.Helper()
		turn := Turn{ThreadID: thread.ID, Status: status}
		if err := st.CreateTurn(&turn); err != nil {
			t.Fatal(err)
		}
		return turn
	}
	addEvent := func(turn Turn, kind, body string) Event {
		t.Helper()
		e := Event{ThreadID: thread.ID, TurnID: turn.ID, Kind: kind, Text: body}
		if err := st.AppendEvent(&e); err != nil {
			t.Fatal(err)
		}
		return e
	}
	late := addTurn(TurnRunning)
	lateEvent := addEvent(late, "user_message", "Eligible only after this turn ends.")
	turn := addTurn(TurnDone)
	immutable := addEvent(turn, "user_message", "Observed fact.")
	steer := addEvent(turn, "steer", "Original steering instruction.")
	uncommitted := addEvent(turn, "user_message", "Never acknowledged.")
	if err := st.AppendMessages(thread.ID, turn.ID, []Message{{Role: "user", EventSeq: steer.Seq, Content: "[steer] Current instruction."}}); err != nil {
		t.Fatal(err)
	}
	start := func(id string) ProjectDistillation {
		t.Helper()
		row := ProjectDistillation{ProjectID: project.ID, ID: id, State: "running", WikiGraph: true}
		if _, err := st.StartDistillationQueue(ctx, row, "", func(int) ([]byte, error) { return []byte(`{}`), nil }); err != nil {
			t.Fatal(err)
		}
		return row
	}
	first := start("first")
	first.Receipt = []byte(fmt.Sprintf(`{"wiki_scan_cursor":%d}`, steer.ID))
	if err := st.CheckpointWikiScan(ctx, first, 0, []uint{immutable.ID, steer.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckpointWikiScan(ctx, first, 0, []uint{uncommitted.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("stale local cursor admitted a write: %v", err)
	}
	first.CursorEventID = steer.ID
	if err := st.CheckpointWikiReview(ctx, first, 0, []uint{immutable.ID, steer.ID, immutable.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.CheckpointWikiReview(ctx, first, 0, []uint{uncommitted.ID}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("failed semantic receipt installed an acknowledgement: %v", err)
	}
	if err := st.DB().Model(&Turn{}).Where("id = ?", late.ID).Update("status", TurnDone).Error; err != nil {
		t.Fatal(err)
	}

	second := start("second")
	scan, err := st.WikiScanWindow(ctx, second.ID, 0, 4)
	if err != nil || len(scan) != 4 {
		t.Fatalf("incremental scan: %+v %v", scan, err)
	}
	if scan[0].Position != lateEvent.ID || scan[0].LocallyIndexed || !scan[0].Available ||
		scan[1].Position != immutable.ID || !scan[1].LocallyIndexed || !scan[1].Available || scan[1].Part.Text != "" ||
		scan[2].Position != steer.ID || scan[2].LocallyIndexed || scan[2].Part.Text != "Current instruction." ||
		scan[3].Position != uncommitted.ID || scan[3].LocallyIndexed {
		t.Fatalf("incorrect local reuse or a late-finishing turn was missed: %+v", scan)
	}
	review, err := st.WikiReviewMetadataWindow(ctx, second.ID, 0, 4)
	if err != nil || len(review) != 4 || review[0].WikiReviewed || !review[1].WikiReviewed || review[2].WikiReviewed || review[3].WikiReviewed {
		t.Fatalf("incorrect semantic reuse: %+v %v", review, err)
	}
	if err := st.DB().Model(&Thread{}).Where("id = ?", thread.ID).Update("project_id", "").Error; err != nil {
		t.Fatal(err)
	}
	scan, err = st.WikiScanWindow(ctx, second.ID, 0, 4)
	if err != nil || len(scan) != 4 || scan[1].LocallyIndexed || scan[1].Available {
		t.Fatalf("moved evidence retained its reusable status: %+v %v", scan, err)
	}
	review, err = st.WikiReviewMetadataWindow(ctx, second.ID, 0, 4)
	if err != nil || len(review) != 4 || review[1].WikiReviewed {
		t.Fatalf("moved evidence retained its semantic acknowledgement: %+v %v", review, err)
	}
	second.Receipt = []byte(fmt.Sprintf(`{"wiki_scan_cursor":%d}`, immutable.ID))
	if err := st.CheckpointWikiScan(ctx, second, 0, []uint{immutable.ID}); !errors.Is(err, ErrHistorySourceUnavailable) {
		t.Fatalf("moved evidence checkpoint returned %v", err)
	}
	unchanged, err := st.ProjectWikiBuildReceipt(ctx, project.ID)
	if err != nil || unchanged == nil || string(unchanged.Receipt) != `{}` || unchanged.CursorEventID != 0 {
		t.Fatalf("ineligible acknowledgement advanced its cursor: %+v %v", unchanged, err)
	}
	// Same-ID changes must invalidate both acknowledgements without a
	// fingerprint pass over every other immutable history body.
	if err := st.DB().Model(&Thread{}).Where("id = ?", thread.ID).Update("project_id", project.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := st.DB().Model(&Event{}).Where("id = ?", immutable.ID).Update("text", "Corrected observation.").Error; err != nil {
		t.Fatal(err)
	}
	scan, err = st.WikiScanWindow(ctx, second.ID, lateEvent.ID, 2)
	if err != nil || len(scan) != 2 || scan[0].LocallyIndexed || scan[0].Part.Text != "Corrected observation." {
		t.Fatalf("changed evidence reused old local scan: %+v %v", scan, err)
	}
	review, err = st.WikiReviewMetadataWindow(ctx, second.ID, lateEvent.ID, 2)
	if err != nil || len(review) != 2 || review[0].WikiReviewed {
		t.Fatalf("changed evidence reused old semantic acceptance: %+v %v", review, err)
	}
	if err := st.CheckpointWikiScan(ctx, second, 0, []uint{immutable.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().Delete(&Event{}, immutable.ID).Error; err != nil {
		t.Fatal(err)
	}
	var retained int64
	if err := st.DB().Model(&WikiSourceScan{}).Where("event_id = ?", immutable.ID).Count(&retained).Error; err != nil || retained != 0 {
		t.Fatalf("removed event retained a scan acknowledgement: %d %v", retained, err)
	}
	// A project deletion also clears acknowledgements if the original
	// conversation has already left and its events are kept.
	second.Receipt = []byte(fmt.Sprintf(`{"wiki_scan_cursor":%d}`, uncommitted.ID))
	if err := st.CheckpointWikiScan(ctx, second, immutable.ID, []uint{uncommitted.ID}); err != nil {
		t.Fatal(err)
	}
	if err := st.DB().Model(&Thread{}).Where("id = ?", thread.ID).Update("project_id", "").Error; err != nil {
		t.Fatal(err)
	}
	if err := st.DeleteProject(project.ID); err != nil {
		t.Fatal(err)
	}
	var count int64
	if err := st.DB().Model(&WikiSourceScan{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("project deletion left scan acknowledgements: %d %v", count, err)
	}
}
