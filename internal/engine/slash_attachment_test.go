package engine

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestGoalCommandCannotAcknowledgeAttachmentsOrMutateThePreviousGoal(t *testing.T) {
	for _, kind := range []string{"image", "file"} {
		t.Run(kind, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("mixed goal", "", "")
			if err := e.SetThreadGoal(th.ID, "previous objective"); err != nil {
				t.Fatal(err)
			}
			if err := e.CompleteThreadGoal(th.ID, ""); err != nil {
				t.Fatal(err)
			}
			in := UserInput{Text: "Inspect the next operation /goal"}
			if kind == "image" {
				pixels, err := os.ReadFile("../slash/testdata/goal-attachment.png")
				if err != nil {
					t.Fatal(err)
				}
				in.Images = []ImageInput{{Name: "original.png", MIME: "image/png", Data: pixels}}
			} else {
				saved, err := e.SaveUpload(th.ID, "original.bin", func(dst string) error { return os.WriteFile(dst, []byte{0, 255, 32}, 0o600) })
				if err != nil {
					t.Fatal(err)
				}
				in.Files = []string{saved.RelPath}
			}
			turn, err := e.StartTurnInput(th.ID, in)
			if err == nil || !strings.Contains(err.Error(), "text only") || turn != nil {
				t.Fatalf("mixed goal acknowledged: turn=%+v error=%v", turn, err)
			}
			got, err := e.store.GetThread(th.ID)
			if err != nil || got.Goal != "previous objective" || !got.GoalComplete {
				t.Fatalf("refused command changed goal: %+v err=%v", got, err)
			}
			turns, err := e.store.ListTurns(th.ID)
			if err != nil || len(turns) != 0 {
				t.Fatalf("refused goal started work: %+v err=%v", turns, err)
			}
		})
	}
}

func TestEngineContinuationTextCannotSetANewHumanGoal(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("continuation goal", "", "")
	for _, in := range []*UserInput{nil, {Text: "/goal replace", ContinueGoal: true}, {Text: "/goal replace", ContinueSchedule: true}} {
		turn, done, err := e.applySlashGoal(th.ID, in)
		if err != nil || done || turn != nil {
			t.Fatalf("engine continuation became a human command: turn=%+v done=%v err=%v", turn, done, err)
		}
	}
	got, _ := e.store.GetThread(th.ID)
	if got.Goal != "" {
		t.Fatal("engine text changed persistent goal")
	}
	if _, _, err := e.applySlashGoal("missing", &UserInput{Text: "/goal inspect"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("goal failure lost missing-thread category: %v", err)
	}
}
