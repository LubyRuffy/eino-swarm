package engine

import (
	"errors"
	"testing"
)

func TestHandoffRefusesAnAdmittedDataTransferBeforeFreezing(t *testing.T) {
	for _, test := range []struct {
		name string
		hold func(*Engine) (func(), error)
		want error
	}{
		{"backup", func(e *Engine) (func(), error) { done, _, err := e.holdBackup(false); return done, err }, ErrBackupBusy},
		{"source switch", (*Engine).holdSourceSwitch, ErrBackupBusy},
		{"restore on startup", func(e *Engine) (func(), error) { e.BeginRestore(); return e.FinishRestore, nil }, ErrRestoring},
	} {
		t.Run(test.name, func(t *testing.T) {
			e := newTestEngine(t)
			release, err := test.hold(e)
			if err != nil {
				t.Fatal(err)
			}
			status, err := e.PrepareRestart(t.Context())
			release()
			if !errors.Is(err, test.want) || status.Stage != "" || e.GetRestartStatus().Stage != "" {
				t.Fatalf("active transfer was interrupted: %+v, %v", status, err)
			}
			if status, err := e.PrepareRestart(t.Context()); err != nil || !status.Ready {
				t.Fatalf("safe retry after transfer was refused: %+v, %v", status, err)
			}
		})
	}
}
