package graph

import (
	"errors"
	"fmt"
	"testing"
)

func TestGraphBatchRefusesMoreThanTheEvidenceLimit(t *testing.T) {
	evidence := make([]Evidence, MaxEvidence+1)
	for i := range evidence {
		evidence[i].URL = fmt.Sprintf("https://example.test/evidence/%d", i)
	}
	batch := Batch{
		Todos:    []TodoInput{{Title: "Review the collected evidence", Status: TodoDone}},
		Evidence: evidence[:MaxEvidence],
	}
	if err := batch.Valid(); err != nil {
		t.Fatalf("evidence at the limit: %v", err)
	}
	batch.Evidence = evidence
	if err := batch.Valid(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("evidence over the limit was not refused: %v", err)
	}
}
