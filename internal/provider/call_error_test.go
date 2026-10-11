package provider

import (
	"context"
	"errors"
	"fmt"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"testing"
)

func TestModelCallErrorRetainsTheOriginalCauseAndText(t *testing.T) {
	if modelCallError(nil) != nil {
		t.Fatal("a successful call must stay successful")
	}
	cause := errors.New("unexpected end of JSON input")
	err := fmt.Errorf("model receive: %w", modelCallError(cause))
	var call *CallError
	if !errors.As(err, &call) || !errors.Is(err, cause) || call.Error() != cause.Error() || call.Unwrap() != cause {
		t.Fatalf("model provenance or decoder cause lost: %v", err)
	}
}

func TestRecordedModelFailuresCarryProvenanceWithOrWithoutTelemetry(t *testing.T) {
	for _, watched := range []bool{false, true} {
		t.Run(fmt.Sprintf("watched_%t", watched), func(t *testing.T) {
			cause := errors.New("invalid character '<' looking for beginning of value")
			s := &sink{}
			var rec Recorder
			if watched {
				rec = s.rec()
			}
			m := wrap(&fakeModel{err: cause}, "manager", config.Provider{ID: "fixture"}, "", 1, rec)
			_, generateErr := m.Generate(context.Background(), nil)
			_, streamErr := m.Stream(context.Background(), nil)
			for _, err := range []error{generateErr, streamErr} {
				var call *CallError
				if !errors.As(err, &call) || !errors.Is(err, cause) || err.Error() != cause.Error() {
					t.Fatalf("provider returned unclassified or rewritten error: %v", err)
				}
			}
			if watched {
				rows := s.all()
				if len(rows) != 2 {
					t.Fatalf("records=%d", len(rows))
				}
				for _, row := range rows {
					if row.Err != cause {
						t.Fatal("Trace must preserve the raw decoder cause")
					}
				}
			}
		})
	}
}
