package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/hostrecovery"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/update"
)

const linuxTrialWait = 90 * time.Second
const linuxSnapshotWait = 30 * time.Second
const linuxSupervisorPoll = 250 * time.Millisecond
const linuxSupervisorRestartWait = time.Second

type linuxHostSupervisor struct {
	dataDir  string
	state    string
	addr     string
	unit     string
	mock     bool
	output   io.Writer
	listener *net.TCPListener
}

func runLinuxHostSupervisor(args []string) error {
	flags := flag.NewFlagSet("host-supervise", flag.ContinueOnError)
	var supervisor linuxHostSupervisor
	flags.StringVar(&supervisor.dataDir, "data-dir", "", "absolute path to the host data directory")
	flags.StringVar(&supervisor.addr, "addr", config.Default().Server.Addr, "fixed listen address")
	flags.StringVar(&supervisor.unit, "unit", "", "enabled systemd service owning this supervisor")
	flags.BoolVar(&supervisor.mock, "mock", false, "use the scripted offline provider")
	if err := flags.Parse(args); err != nil {
		return err
	}
	_, port, err := net.SplitHostPort(supervisor.addr)
	numericPort, portErr := strconv.Atoi(port)
	if flags.NArg() != 0 || !filepath.IsAbs(supervisor.dataDir) || filepath.Clean(supervisor.dataDir) != supervisor.dataDir ||
		err != nil || portErr != nil || numericPort < 1 || numericPort > 65535 {
		return errors.New("host-supervise requires an absolute data directory and a fixed listen port")
	}
	supervisor.dataDir, err = dataPathForHostRecovery(supervisor.dataDir)
	if err != nil {
		return err
	}
	supervisor.state = hostrecovery.StateDir(supervisor.dataDir)
	supervisor.output = os.Stderr

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Linux parent-death signals are attached to the creating thread. Pin
	// that thread across every fork until this restart owner is done.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	return supervisor.run(ctx)
}

func (s linuxHostSupervisor) run(ctx context.Context) error {
	if !hostrecovery.Managed(s.dataDir) && lease.Busy(s.dataDir) {
		return errors.New("stop the existing engine before setting up its recovery supervisor")
	}
	address, err := net.ResolveTCPAddr("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("resolve supervised listen address: %w", err)
	}
	s.listener, err = net.ListenTCP("tcp", address)
	if err != nil {
		return fmt.Errorf("reserve supervised listen address: %w", err)
	}
	// Keep ownership even after the engine exits. Inherited duplicates serve
	// the same address without a bind gap during either trial or rollback.
	defer s.listener.Close()
	s.addr = s.listener.Addr().String()
	hold, err := hostrecovery.Lock(s.state)
	if err != nil {
		return fmt.Errorf("lock Linux host supervisor: %w", err)
	}
	defer hold.Close()
	journal, err := s.bootJournal(ctx)
	if err != nil {
		return err
	}
	var child *linuxSupervisorChild
	var launch hostrecovery.Launch
	var retired *hostrecovery.DataIdentity
	for ctx.Err() == nil {
		if child == nil {
			if lease.Busy(s.dataDir) {
				return errors.New("another engine still owns the supervised data directory")
			}
			child, launch, err = s.launch(ctx, journal.Active, nil)
			if err != nil {
				return err
			}
		}
		if err := s.waitActive(ctx, child, &journal, &retired); err != nil {
			return err
		}
		child = nil
		if ctx.Err() != nil {
			return nil
		}
		if journal.Completed == "" {
			request, err := hostrecovery.LoadRequest(s.state)
			if err == nil {
				if journal.Preparing != nil {
					s.reject(request.OperationID, errors.New("previous Linux update snapshot cleanup is pending"))
				} else if err := s.validateRequest(ctx, journal.Active, launch, request); err != nil {
					s.reject(request.OperationID, err)
				} else {
					child, launch, retired, err = s.tryCandidate(ctx, &journal, request)
					if err != nil {
						return err
					}
					if child != nil {
						continue
					}
				}
			} else if !errors.Is(err, os.ErrNotExist) {
				log.Printf("read supervised update request: %v", err)
			}
		}
		timer := time.NewTimer(linuxSupervisorRestartWait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
	return nil
}

func (s linuxHostSupervisor) bootJournal(ctx context.Context) (hostrecovery.Journal, error) {
	journal, err := hostrecovery.LoadJournal(s.state)
	if errors.Is(err, os.ErrNotExist) {
		exe, err := os.Executable()
		if err != nil {
			return journal, err
		}
		exe, err = filepath.EvalSymlinks(exe)
		if err != nil {
			return journal, err
		}
		active, err := hostrecovery.ImageAt(exe, version)
		if err != nil {
			return journal, err
		}
		journal = hostrecovery.Journal{Active: active}
		if err := hostrecovery.SaveJournal(s.state, journal); err != nil {
			return journal, err
		}
	} else if err != nil {
		return journal, fmt.Errorf("recover supervised update journal: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return journal, err
	}
	if journal.Trial != nil {
		if lease.Busy(s.dataDir) {
			return journal, errors.New("cannot reconcile an uncommitted update while another engine owns the data")
		}
		err := s.rollback(&journal, errors.New("the host restarted during an unconfirmed trial"))
		return journal, err
	}
	// In this phase no exchange happened, or rollback already restored
	// the original. Resume the original before touching the discarded clone.
	if journal.Preparing != nil {
		return journal, nil
	}
	if journal.Completed == "" {
		if request, err := hostrecovery.LoadRequest(s.state); err == nil {
			s.reject(request.OperationID, errors.New("the restart owner stopped before starting the trial"))
		} else if !errors.Is(err, os.ErrNotExist) {
			log.Printf("read abandoned Linux update request: %v", err)
		}
	}
	return journal, nil
}

func (s linuxHostSupervisor) protected(ctx context.Context) bool {
	if s.unit == "" || linuxArchiveBuild != "1" {
		return false
	}
	if err := verifiedBootUnit(ctx, s.unit); err != nil {
		log.Printf("Linux host update requires a working boot owner: %v", err)
		return false
	}
	return true
}

func (s linuxHostSupervisor) launch(ctx context.Context, image hostrecovery.Image, trial *hostrecovery.Request) (*linuxSupervisorChild, hostrecovery.Launch, error) {
	env := hostrecovery.Launch{
		DataDir: s.dataDir, Unit: s.unit, Nonce: hostrecovery.Nonce(), Protected: s.protected(ctx), Trial: trial,
	}
	if err := ctx.Err(); err != nil {
		return nil, env, err
	}
	if trial != nil && !env.Protected {
		return nil, env, errors.New("the boot protection disappeared before the upgrade trial")
	}
	if err := hostrecovery.VerifyImage(image); err != nil {
		return nil, env, fmt.Errorf("verify supervised engine image: %w", err)
	}
	child, err := startLinuxSupervisorChild(image, s.addr, s.mock, env, s.listener, s.output)
	return child, env, err
}

func (s linuxHostSupervisor) waitActive(ctx context.Context, child *linuxSupervisorChild, journal *hostrecovery.Journal, retired **hostrecovery.DataIdentity) error {
	tick := time.NewTicker(linuxSupervisorPoll)
	defer tick.Stop()
	cleanupAttempted := false
	for {
		select {
		case <-ctx.Done():
			return child.stop()
		case <-child.done:
			if child.err != nil {
				log.Printf("supervised Linux engine exited: %v", child.err)
			}
			if journal.Preparing != nil && !cleanupAttempted {
				// A full volume can prevent even the previous image from
				// becoming ready. Reclaim its owned clone before retrying.
				if err := s.finishPreparing(journal); err != nil {
					log.Printf("retain interrupted Linux data preparation: %v", err)
				}
			}
			return nil
		case <-tick.C:
			if journal.Preparing != nil && !cleanupAttempted && child.serving(ctx, s.dataDir, s.mock, "ready") {
				cleanupAttempted = true
				if err := s.finishPreparing(journal); err != nil {
					log.Printf("retain interrupted Linux data preparation: %v", err)
				}
			}
			if journal.Completed == "" || !child.serving(ctx, s.dataDir, s.mock, "ready") {
				continue
			}
			job, err := update.OpenHostJob(s.dataDir, journal.Completed)
			if err == nil {
				err = job.SetStage("completed")
			}
			if err == nil {
				err = hostrecovery.RemoveRequest(s.state)
			}
			next := *journal
			next.Completed = ""
			if err == nil {
				err = hostrecovery.SaveJournal(s.state, next)
			}
			if err != nil {
				log.Printf("retain Linux update completion pending: %v", err)
				continue
			}
			*journal = next
			if *retired != nil {
				if err := hostrecovery.DiscardRetiredData(**retired); err != nil {
					log.Printf("retain Linux update snapshot: %v", err)
				}
				*retired = nil
			}
		}
	}
}

func (s linuxHostSupervisor) validateRequest(ctx context.Context, active hostrecovery.Image, owner hostrecovery.Launch, req hostrecovery.Request) error {
	if !owner.Protected || owner.Nonce != req.Nonce || !s.protected(ctx) {
		return errors.New("the update request has no verified predecessor and boot owner")
	}
	if req.Peer == "" || !update.Newer(req.Candidate.Version, active.Version) {
		return errors.New("the update request has no confirming peer or newer target")
	}
	relative, err := filepath.Rel(s.state, req.Candidate.Path)
	if err != nil || !filepath.IsLocal(relative) || relative == "." || req.Candidate.Path == active.Path {
		return errors.New("the update candidate is outside the private recovery directory")
	}
	job, err := update.OpenHostJob(s.dataDir, req.OperationID)
	if err != nil {
		return fmt.Errorf("read confirmed Linux update attempt: %w", err)
	}
	accepted := job.Snapshot()
	if accepted.Stage != "restarting" || accepted.Version != req.Candidate.Version {
		return errors.New("the update request does not match the predecessor's restart receipt")
	}
	return hostrecovery.VerifyImage(req.Candidate)
}

func (s linuxHostSupervisor) tryCandidate(ctx context.Context, journal *hostrecovery.Journal, req hostrecovery.Request) (*linuxSupervisorChild, hostrecovery.Launch, *hostrecovery.DataIdentity, error) {
	var launch hostrecovery.Launch
	if lease.Busy(s.dataDir) {
		return nil, launch, nil, errors.New("a competing engine acquired the data directory before the trial")
	}
	snapshot, cancel := context.WithTimeout(ctx, linuxSnapshotWait)
	defer cancel()
	plan, err := hostrecovery.PlanData(s.dataDir)
	if err != nil {
		s.reject(req.OperationID, err)
		return nil, launch, nil, nil
	}
	next := *journal
	next.Preparing = &hostrecovery.Preparing{Request: req, Data: plan}
	if err := hostrecovery.SaveJournal(s.state, next); err != nil {
		return nil, launch, nil, err
	}
	*journal = next
	data, err := hostrecovery.ClonePreparedData(snapshot, plan)
	if err = errors.Join(err, snapshot.Err()); err != nil {
		log.Printf("discard incomplete Linux update %s: %v", req.OperationID, err)
		if err := s.finishPreparing(journal); err != nil {
			log.Printf("retain interrupted Linux data preparation: %v", err)
		}
		return nil, launch, nil, nil
	}
	next.Preparing = nil
	next.Trial = &hostrecovery.Trial{Request: req, Data: &data}
	if err := hostrecovery.SaveJournal(s.state, next); err != nil {
		// The state write may have been renamed but not synced. No candidate
		// may run here. Reboot will recover the original slot in either case.
		return nil, launch, nil, err
	}
	*journal = next
	if err := hostrecovery.ActivateData(data); err != nil {
		return nil, launch, nil, s.rollback(journal, err)
	}
	child, launch, err := s.launch(ctx, req.Candidate, &req)
	if err != nil {
		return nil, launch, nil, s.rollback(journal, err)
	}
	if err := s.awaitTrial(ctx, child, req, launch); err != nil {
		if stopErr := child.stop(); stopErr != nil {
			return nil, launch, nil, errors.Join(err, stopErr)
		}
		return nil, launch, nil, s.rollback(journal, err)
	}
	if err := ctx.Err(); err != nil {
		if stopErr := child.stop(); stopErr != nil {
			return nil, launch, nil, errors.Join(err, stopErr)
		}
		return nil, launch, nil, s.rollback(journal, err)
	}

	oldRetired := journal.Retired
	previous := journal.Active
	next = hostrecovery.Journal{Active: req.Candidate, Retired: &data, RetiredImage: &previous, Completed: req.OperationID}
	if err := hostrecovery.SaveJournal(s.state, next); err != nil {
		// A failed fsync cannot grant writes. Stop the quarantined child,
		// then leave disk reconciliation to the trusted boot entry point.
		return nil, launch, nil, errors.Join(err, child.stop())
	}
	*journal = next
	if ctx.Err() != nil {
		return nil, launch, nil, child.stop()
	}
	if err := child.activate(); err != nil {
		// The accepted journal is forward-only after this point. A failed
		// activation pipe can be recovered by a normal start of this image.
		log.Printf("restart committed Linux engine after missing activation: %v", err)
		return nil, launch, nil, child.stop()
	}
	return child, launch, oldRetired, nil
}

func (s linuxHostSupervisor) awaitTrial(parent context.Context, child *linuxSupervisorChild, req hostrecovery.Request, launch hostrecovery.Launch) error {
	ctx, cancel := context.WithTimeout(parent, linuxTrialWait)
	defer cancel()
	tick := time.NewTicker(linuxSupervisorPoll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for the initiating paired phone: %w", ctx.Err())
		case <-child.done:
			return errors.Join(errors.New("the Linux upgrade trial exited before acceptance"), child.err)
		case <-tick.C:
			proof, err := hostrecovery.ReadProof(s.state)
			if err == nil && proof.OperationID == req.OperationID && proof.Nonce == launch.Nonce && proof.Peer == req.Peer &&
				child.serving(ctx, s.dataDir, s.mock, "trial") && ctx.Err() == nil {
				return nil
			}
		}
	}
}

func (s linuxHostSupervisor) rollback(journal *hostrecovery.Journal, cause error) error {
	trial := journal.Trial
	if trial == nil || trial.Data == nil {
		return errors.New("the unconfirmed trial has no recoverable data identity")
	}
	if lease.Busy(s.dataDir) {
		return errors.New("refusing to restore trial data while a live engine owns it")
	}
	if err := hostrecovery.RestoreData(*trial.Data); err != nil {
		return fmt.Errorf("restore preserved Linux host data: %w", err)
	}
	next := *journal
	next.Trial = nil
	next.Preparing = &hostrecovery.Preparing{Request: trial.Request, Data: hostrecovery.DataPreparation{
		DataDir: trial.Data.DataDir, Original: trial.Data.Original, TrialToken: trial.Data.Trial.Token,
	}}
	if err := hostrecovery.SaveJournal(s.state, next); err != nil {
		return err
	}
	*journal = next
	s.reject(trial.Request.OperationID, cause)
	return nil
}

func (s linuxHostSupervisor) finishPreparing(journal *hostrecovery.Journal) error {
	preparing := journal.Preparing
	if preparing.Data.DataDir != s.dataDir {
		return errors.New("the pending snapshot belongs to another host data path")
	}
	// Reclaim first in case a full volume is preventing receipt writes. An
	// unrecognized clone still cannot keep the already failed job pending.
	cleanupErr := hostrecovery.DiscardUnreadyData(preparing.Data)
	request, err := hostrecovery.LoadRequest(s.state)
	switch {
	case errors.Is(err, os.ErrNotExist):
		err = nil
	case err == nil && request == preparing.Request:
		err = s.recordRejection(request.OperationID)
	case err == nil:
		err = errors.New("the pending snapshot does not match the interrupted update")
	}
	if err := errors.Join(cleanupErr, err); err != nil {
		return err
	}
	next := *journal
	next.Preparing = nil
	if err := hostrecovery.SaveJournal(s.state, next); err != nil {
		return err
	}
	*journal = next
	return nil
}

func (s linuxHostSupervisor) reject(operationID string, err error) {
	log.Printf("Linux host update %s was rolled back or refused: %v", operationID, err)
	if err := s.recordRejection(operationID); err != nil {
		log.Printf("record failed Linux host update: %v", err)
	}
}

func (s linuxHostSupervisor) recordRejection(operationID string) error {
	job, err := update.OpenHostJob(s.dataDir, operationID)
	if err != nil {
		return err
	}
	if err := job.SetError("failed", errors.New("The supervised update was not accepted. The previous version was retained.")); err != nil {
		return err
	}
	return hostrecovery.RemoveRequest(s.state)
}
