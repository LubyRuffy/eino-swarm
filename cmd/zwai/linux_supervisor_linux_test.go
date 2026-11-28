package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/hostrecovery"
	"github.com/LubyRuffy/eino-swarm/internal/update"
	"golang.org/x/sys/unix"
)

const supervisorTestOperation = "11111111-2222-4333-8444-555555555555"

func TestLinuxSupervisorAndStandaloneCannotOwnTheSameData(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "alias")
	if err := os.Symlink(data, alias); err != nil {
		t.Fatal(err)
	}
	standalone, err := holdStandaloneEngineAdmission(alias)
	if err != nil {
		t.Fatal(err)
	}
	denied, err := hostrecovery.Lock(hostrecovery.StateDir(data))
	standalone()
	if denied != nil {
		_ = denied.Close()
	}
	if err == nil || hostrecovery.Managed(data) {
		t.Fatal("setup took ownership while a standalone engine already held an alias")
	}
	supervised, err := hostrecovery.Lock(hostrecovery.StateDir(data))
	if err != nil {
		t.Fatal(err)
	}
	if duplicate, err := holdStandaloneEngineAdmission(alias); err == nil {
		duplicate()
		_ = supervised.Close()
		t.Fatal("standalone launch bypassed the current supervisor's guard")
	}
	if err := supervised.Close(); err != nil {
		t.Fatal(err)
	}
	// A supervisor crash releases its kernel lock. The durable ownership
	// marker still prevents the standalone engine from opening trial data.
	if duplicate, err := holdStandaloneEngineAdmission(alias); err == nil {
		duplicate()
		t.Fatal("standalone launch could open unexamined data after a supervisor crash")
	}
}

func TestLinuxSupervisorBootReconcilesBothSidesOfDurableCommit(t *testing.T) {
	for _, committed := range []bool{false, true} {
		t.Run(strconv.FormatBool(committed), func(t *testing.T) {
			root := t.TempDir()
			var fs unix.Statfs_t
			if err := unix.Statfs(root, &fs); err != nil {
				t.Fatal(err)
			}
			if fs.Type != unix.EXT4_SUPER_MAGIC && fs.Type != unix.BTRFS_SUPER_MAGIC {
				t.Skip("this recovery test needs a private ext4 or Btrfs filesystem")
			}
			data := filepath.Join(root, "data")
			if err := os.Mkdir(data, 0o700); err != nil {
				t.Fatal(err)
			}
			state := hostrecovery.StateDir(data)
			hold, err := hostrecovery.Lock(state)
			if err != nil {
				t.Fatal(err)
			}
			defer hold.Close()
			previous := supervisorFixtureImage(t, root, "previous", "1.0.0", "exit 0")
			candidate := supervisorFixtureImage(t, state, "incoming", "1.0.1", "exit 0")
			writeSupervisorFixture(t, filepath.Join(data, "notes"), "original")
			writeSupervisorJob(t, data)
			request := hostrecovery.Request{OperationID: supervisorTestOperation, Peer: "bound-phone", Nonce: hostrecovery.Nonce(), Candidate: candidate}
			if err := hostrecovery.SaveRequest(state, request); err != nil {
				t.Fatal(err)
			}
			identity, err := hostrecovery.PrepareData(t.Context(), data)
			if err != nil {
				t.Fatal(err)
			}
			journal := hostrecovery.Journal{Active: previous, Trial: &hostrecovery.Trial{Request: request, Data: &identity}}
			if err := hostrecovery.SaveJournal(state, journal); err != nil {
				t.Fatal(err)
			}
			if err := hostrecovery.ActivateData(identity); err != nil {
				t.Fatal(err)
			}
			writeSupervisorFixture(t, filepath.Join(data, "notes"), "candidate")
			if committed {
				journal = hostrecovery.Journal{Active: candidate, Retired: &identity, RetiredImage: &previous, Completed: request.OperationID}
				if err := hostrecovery.SaveJournal(state, journal); err != nil {
					t.Fatal(err)
				}
			}

			// Boot uses the persisted decision and actual data slot identity.
			// It does not infer commitment from finding candidate bytes live.
			supervisor := linuxHostSupervisor{dataDir: data, state: state}
			reopened, err := supervisor.bootJournal(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			job, err := update.OpenHostJob(data, request.OperationID)
			if err != nil {
				t.Fatal(err)
			}
			notes, err := os.ReadFile(filepath.Join(data, "notes"))
			if err != nil {
				t.Fatal(err)
			}
			if committed {
				if reopened.Active != candidate || reopened.Trial != nil || reopened.Completed != request.OperationID ||
					reopened.RetiredImage == nil || *reopened.RetiredImage != previous ||
					string(notes) != "candidate" || job.Snapshot().Stage != "restarting" {
					t.Fatalf("committed boot changed selection, data or receipt: journal=%+v notes=%q job=%+v", reopened, notes, job.Snapshot())
				}
				if _, err := hostrecovery.LoadRequest(state); err != nil {
					t.Fatalf("committed boot must finish the pending acceptance: %v", err)
				}
			} else {
				if reopened.Active != previous || reopened.Trial != nil || reopened.Completed != "" ||
					string(notes) != "original" || job.Snapshot().Stage != "failed" {
					t.Fatalf("uncommitted boot did not restore the predecessor: journal=%+v notes=%q job=%+v", reopened, notes, job.Snapshot())
				}
				if _, err := hostrecovery.LoadRequest(state); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("uncommitted boot left a replayable request: %v", err)
				}
				if reopened.Preparing == nil {
					t.Fatal("boot lost ownership of the detached trial")
				}
				if _, err := os.Stat(identity.SnapshotDir); err != nil {
					t.Fatalf("rollback waited for trial cleanup before allowing the original engine to start: %v", err)
				}
				if err := supervisor.finishPreparing(&reopened); err != nil {
					t.Fatal(err)
				}
				if reopened.Preparing != nil {
					t.Fatal("finished cleanup still blocks an update")
				}
				if _, err := os.Stat(identity.SnapshotDir); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("detached trial not removed after recovery: %v", err)
				}
			}
		})
	}
}

func TestLinuxSupervisorKeepsOriginalAvailableWhenInterruptedCopyCannotBeCleaned(t *testing.T) {
	root := t.TempDir()
	var fs unix.Statfs_t
	if err := unix.Statfs(root, &fs); err != nil {
		t.Fatal(err)
	}
	if fs.Type != unix.EXT4_SUPER_MAGIC && fs.Type != unix.BTRFS_SUPER_MAGIC {
		t.Skip("this recovery test needs a private ext4 or Btrfs filesystem")
	}
	data := filepath.Join(root, "data")
	if err := os.Mkdir(data, 0o700); err != nil {
		t.Fatal(err)
	}
	state := hostrecovery.StateDir(data)
	hold, err := hostrecovery.Lock(state)
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Close()
	writeSupervisorJob(t, data)
	previous := supervisorFixtureImage(t, root, "previous", "1.0.0", "exit 0")
	request := hostrecovery.Request{OperationID: supervisorTestOperation, Peer: "bound-phone"}
	plan, err := hostrecovery.PlanData(data)
	if err != nil {
		t.Fatal(err)
	}
	if err := hostrecovery.SaveRequest(state, request); err != nil {
		t.Fatal(err)
	}
	if err := hostrecovery.SaveJournal(state, hostrecovery.Journal{
		Active: previous, Preparing: &hostrecovery.Preparing{Request: request, Data: plan},
	}); err != nil {
		t.Fatal(err)
	}
	identity, err := hostrecovery.ClonePreparedData(t.Context(), plan)
	if err != nil {
		t.Fatal(err)
	}
	// The exact prepared path now holds a different inode without a seal.
	// This must not prevent selecting the original or authorize deleting it.
	detached := filepath.Join(root, "detached")
	if err := os.Rename(identity.SnapshotDir, detached); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(identity.SnapshotDir, 0o700); err != nil {
		t.Fatal(err)
	}
	foreign := filepath.Join(identity.SnapshotDir, "untouchable")
	writeSupervisorFixture(t, foreign, "independent data")
	supervisor := linuxHostSupervisor{dataDir: data, state: state}
	reopened, err := supervisor.bootJournal(t.Context())
	if err != nil || reopened.Active != previous || reopened.Preparing == nil {
		t.Fatalf("cleanup blocked the boot decision: %+v, %v", reopened, err)
	}
	if err := supervisor.finishPreparing(&reopened); err == nil || reopened.Preparing == nil {
		t.Fatalf("cleanup accepted a substituted clone or lost its admission record: %+v, %v", reopened, err)
	}
	if contents, err := os.ReadFile(foreign); err != nil || string(contents) != "independent data" {
		t.Fatalf("cleanup touched unknown data: %q, %v", contents, err)
	}
	job, err := update.OpenHostJob(data, request.OperationID)
	if err != nil || job.Snapshot().Stage != "failed" {
		t.Fatalf("cleanup refusal left the old host reporting a running upgrade: %v", err)
	}
	if err := os.Rename(identity.SnapshotDir, filepath.Join(root, "independent")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(detached, identity.SnapshotDir); err != nil {
		t.Fatal(err)
	}
	if err := supervisor.finishPreparing(&reopened); err != nil || reopened.Preparing != nil {
		t.Fatalf("owned cleanup was not resumable: %+v, %v", reopened, err)
	}
}

func TestLinuxSupervisorTrialNeedsExplicitActivationByte(t *testing.T) {
	// This child reads the same inherited pipe descriptors as the real
	// candidate. An EOF must leave no activation, even on clean parent exit.
	root := t.TempDir()
	image := supervisorFixtureImage(t, root, "candidate", "1.0.1", `
IFS= read -r envelope <&3 || exit 12
if [ "$(dd bs=1 count=1 <&4 2>/dev/null)" = P ]; then printf active; else printf refused; fi
`)
	for _, activate := range []bool{false, true} {
		t.Run(strconv.FormatBool(activate), func(t *testing.T) {
			// t.Run runs in a separate goroutine; the parent-death signal
			// belongs to this goroutine's creating thread.
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			var observed bytes.Buffer
			launch := hostrecovery.Launch{
				DataDir: root, Nonce: hostrecovery.Nonce(), Protected: true,
				Trial: &hostrecovery.Request{OperationID: supervisorTestOperation},
			}
			child, err := startLinuxSupervisorChild(image, "test", false, launch, nil, &observed)
			if err != nil {
				t.Fatal(err)
			}
			defer child.stop()
			if activate {
				err = child.activate()
			} else {
				err = child.permit.Close()
				child.permit = nil
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case <-child.done:
			case <-time.After(5 * time.Second):
				t.Fatal("trial child did not observe the activation decision")
			}
			expected := "refused"
			if activate {
				expected = "active"
			}
			if child.err != nil || observed.String() != expected {
				t.Fatalf("activation=%v, output=%q, exit=%v", activate, observed.String(), child.err)
			}
		})
	}
}

func TestLinuxSupervisorParentDeathStopsItsOwnedEngine(t *testing.T) {
	root := t.TempDir()
	image := supervisorFixtureImage(t, root, "probe", "1.0.0", "IFS= read -r envelope <&3 || exit 12\nexec sleep 45")
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestLinuxSupervisorParentDeathFixture$")
	cmd.Env = append(os.Environ(), "ZWAI_SUPERVISOR_PARENT_TEST_IMAGE="+image.Path)
	stream, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
	}()
	line, err := bufio.NewReader(stream).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil {
		t.Fatal(err)
	}
	fd, err := unix.PidfdOpen(pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	if err := cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("fixture parent did not die from the injected signal")
	}
	events := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	if count, err := unix.Poll(events, 5000); err != nil || count != 1 || events[0].Revents&unix.POLLIN == 0 {
		t.Fatalf("the exact owned engine survived supervisor death: %+v, %d, %v", events, count, err)
	}
}

func TestLinuxSupervisorParentDeathFixture(t *testing.T) {
	image := os.Getenv("ZWAI_SUPERVISOR_PARENT_TEST_IMAGE")
	if image == "" {
		t.Skip("owned subprocess fixture")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	child, err := startLinuxSupervisorChild(hostrecovery.Image{Path: image, Version: "1.0.0"},
		"test", false, hostrecovery.Launch{DataDir: filepath.Dir(image), Nonce: hostrecovery.Nonce()}, nil, os.Stderr)
	if err != nil {
		t.Fatal(err)
	}
	defer child.stop()
	fmt.Println(child.process.Pid)
	<-child.done
}

func TestLinuxSupervisorKeepsListenerAcrossTrialAndRollback(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	root := t.TempDir()
	image := supervisorFixtureImage(t, root, "candidate", "1.0.1", `
IFS= read -r envelope <&3 || exit 12
case "$envelope" in
  *'"ListenerFD":4'*) test -S /proc/self/fd/4 ;;
  *'"ListenerFD":5'*) test -p /proc/self/fd/4 && test -S /proc/self/fd/5 ;;
  *) exit 13 ;;
esac
`)
	bound, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Close()
	addr := bound.Addr().String()
	for _, trial := range []bool{false, true, false} {
		launch := hostrecovery.Launch{DataDir: root, Nonce: hostrecovery.Nonce(), Protected: true}
		if trial {
			launch.Trial = &hostrecovery.Request{OperationID: supervisorTestOperation}
		}
		child, err := startLinuxSupervisorChild(image, addr, false, launch, bound, os.Stderr)
		if err != nil {
			t.Fatal(err)
		}
		select {
		case <-child.done:
			if child.err != nil {
				t.Fatalf("child trial=%v never received the inherited listener: %v", trial, child.err)
			}
		case <-time.After(5 * time.Second):
			_ = child.stop()
			t.Fatal("child did not exit")
		}
		_ = child.stop()
		// A separate process could take the port exactly here if the stable
		// owner dropped its descriptor during the engine turnover.
		if stranger, err := net.Listen("tcp4", addr); !errors.Is(err, unix.EADDRINUSE) {
			if stranger != nil {
				_ = stranger.Close()
			}
			t.Fatalf("port became available between verified children: %v", err)
		}
	}
}

func supervisorFixtureImage(t *testing.T, dir, name, version, script string) hostrecovery.Image {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	image, err := hostrecovery.ImageAt(path, version)
	if err != nil {
		t.Fatal(err)
	}
	return image
}

func writeSupervisorFixture(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeSupervisorJob(t *testing.T, data string) {
	t.Helper()
	dir := filepath.Join(data, "host-updates")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(update.HostUpdate{OperationID: supervisorTestOperation, Version: "1.0.1", Stage: "restarting"})
	if err != nil {
		t.Fatal(err)
	}
	writeSupervisorFixture(t, filepath.Join(dir, supervisorTestOperation+".json"), string(raw))
}
