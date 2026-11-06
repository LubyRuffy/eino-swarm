package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// A copied cadence_type is a fact even when the answer omits its duration.
// It does not on its own introduce a concrete schedule reference.
func TestScheduleClaimDecoderRetainsCadenceTypeWithoutInventingANumericCadence(t *testing.T) {
	const text = "The schedule wake remains armed."
	for _, tc := range []struct {
		name, fields string
		valid        bool
		claims       int
	}{
		{"delay", `,"schedule_id":"wake","cadence_type":"delay"`, true, 1},
		{"every", `,"schedule_id":"wake","cadence_type":"every"`, true, 1},
		{"cron", `,"schedule_id":"wake","cadence_type":"cron"`, true, 1},
		{"omitted", `,"schedule_id":"wake"`, true, 1},
		{"empty", `,"schedule_id":"wake","cadence_type":""`, true, 1},
		{"invalid", `,"schedule_id":"wake","cadence_type":"daily"`, false, 0},
		{"type-alone", `,"cadence_type":"every"`, true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := `{"claims":[{"quote":"` + text + `","stance":"affirmed","temporal":"current","attribution":"assistant","scope":"unspecified","new":false` + tc.fields + `}]}`
			claims, err := decodeScheduleAnswer(schema.AssistantMessage(body, nil), []string{text})
			if (err == nil) != tc.valid || len(claims) != tc.claims {
				t.Fatalf("cadence type valid=%v claims=%d, want valid=%v claims=%d: %v", err == nil, len(claims), tc.valid, tc.claims, err)
			}
			if !tc.valid {
				var failure *scheduleAnswerError
				if !errors.As(err, &failure) || failure.Detail != "claim_shape" {
					t.Fatalf("invalid cadence type changed the public diagnostic vocabulary: %v", err)
				}
			}
		})
	}
}

func TestScheduleClaimCadenceTypeMustMatchItsReferencedPlan(t *testing.T) {
	turn := &store.Turn{ID: "current", ThreadID: "owner"}
	row := store.Schedule{ID: "wake", Kind: store.ScheduleThread, ThreadID: turn.ThreadID,
		Status: store.ScheduleActive, EveryS: 90, NextRunAt: time.Now().UTC()}
	for _, cadence := range []string{"delay", "every", "cron", ""} {
		t.Run(cadence, func(t *testing.T) {
			var claim scheduleAnswerClaim
			body := fmt.Sprintf(`{"schedule_id":"wake","scope":"unspecified","new":false,"cadence_type":%q}`, cadence)
			if err := json.Unmarshal([]byte(body), &claim); err != nil {
				t.Fatal(err)
			}
			want := cadence == "" || cadence == "every"
			if got := scheduleClaimMatches(claim, row, turn, nil); got != want {
				t.Fatalf("cadence_type=%q matched=%v want=%v", cadence, got, want)
			}
			if cadence != "" {
				diagnostic := diagnoseScheduleClaim(claim, []store.Schedule{row}, turn, nil)
				if !slices.Contains(diagnostic.Stated, "cadence_type") {
					t.Fatalf("diagnostic lost the stated cadence type: %+v", diagnostic)
				}
			}
		})
	}
}
