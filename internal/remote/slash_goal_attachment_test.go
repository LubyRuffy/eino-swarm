package remote

import (
	"bytes"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// A goal command acknowledges text only. In particular a busy command must not
// return success before the image/file reaches any model input or saved turn.
func TestPhoneGoalCommandRefusalPreservesTheGoalAndRetryAttachmentBytes(t *testing.T) {
	pixels, err := os.ReadFile("../slash/testdata/goal-attachment.png")
	if err != nil {
		t.Fatal(err)
	}
	for _, busy := range []bool{false, true} {
		for _, op := range []string{OpSend, OpSteer} {
			for _, kind := range []string{"image", "file"} {
				t.Run(op+"/"+kind+map[bool]string{false: "/idle", true: "/busy"}[busy], func(t *testing.T) {
					e := testEngine(t)
					th, _ := e.CreateThread("goal attachment", "", "")
					if err := e.SetThreadGoal(th.ID, "previous completed objective"); err != nil {
						t.Fatal(err)
					}
					if err := e.CompleteThreadGoal(th.ID, ""); err != nil {
						t.Fatal(err)
					}
					if busy {
						if _, err := e.StartTurn(th.ID, "Inspect the current state."); err != nil {
							t.Fatal(err)
						}
					}
					payload, name, mime := pixels, "original.png", "image/png"
					if kind == "file" {
						payload = []byte{0, 255, 32, 13, 10}
						name = "original.bin"
						mime = "application/octet-stream"
					}
					stage := NewStaging()
					if err := stageAll(stage, "original", name, mime, payload); err != nil {
						t.Fatal(err)
					}
					refused := HandleWith(e, config.RemoteConfig{}, stage, Request{ID: "goal-with-bytes", Op: op, ThreadID: th.ID, Text: "Inspect the assigned operation /goal", Puts: []string{"original"}}, "relay", "fixture-session")
					if refused.OK || !strings.Contains(refused.Error, "text only") {
						t.Fatalf("mixed goal command falsely acknowledged attachment: %+v", refused)
					}
					got, err := e.Store().GetThread(th.ID)
					if err != nil || got.Goal != "previous completed objective" || !got.GoalComplete {
						t.Fatalf("rejected attachment command mutated the goal: %+v err=%v", got, err)
					}
					// A phone retains its File after refusal and stages the same original bytes
					// again when the human removes the goal marker and sends an ordinary message.
					if err := stageAll(stage, "retry", name, mime, payload); err != nil {
						t.Fatal(err)
					}
					accepted := HandleWith(e, config.RemoteConfig{}, stage, Request{ID: "ordinary-retry", Op: op, ThreadID: th.ID, Text: "Inspect the assigned operation.", Puts: []string{"retry"}}, "relay", "fixture-session")
					if !accepted.OK {
						t.Fatalf("ordinary attachment retry refused: %+v", accepted)
					}
					var saved []byte
					if kind == "image" {
						entries, err := os.ReadDir(e.Config().ThreadInputsDir(th.ID))
						if err != nil || len(entries) != 1 {
							t.Fatalf("retry image not durably saved: entries=%v err=%v", entries, err)
						}
						saved, err = os.ReadFile(filepath.Join(e.Config().ThreadInputsDir(th.ID), entries[0].Name()))
						if err != nil {
							t.Fatal(err)
						}
						decoded, err := png.Decode(bytes.NewReader(saved))
						if err != nil || decoded.Bounds().Dx() != 2 || decoded.Bounds().Dy() != 3 || http.DetectContentType(saved) != "image/png" {
							t.Fatalf("saved image is not the selected visual input: image=%v err=%v", decoded, err)
						}
					} else {
						files, err := e.Store().ListAttachments(th.ID)
						if err != nil || len(files) == 0 {
							t.Fatalf("retry file not saved: %+v err=%v", files, err)
						}
						saved, err = os.ReadFile(filepath.Join(e.WorkspaceDir(th.ID), files[len(files)-1].RelPath))
						if err != nil {
							t.Fatal(err)
						}
					}
					if !bytes.Equal(saved, payload) {
						t.Fatal("attachment bytes changed after refusal/retry")
					}
					if err := e.Interrupt(th.ID); err != nil {
						t.Fatal(err)
					}
				})
			}
		}
	}
}
