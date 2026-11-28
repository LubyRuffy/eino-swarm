package update

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
)

var (
	// ErrHostUpdateUnsupported indicates the running install cannot self-update.
	ErrHostUpdateUnsupported = errors.New("this host cannot install updates from the phone")
	// ErrHostUpdateUnauthorized indicates the link was replaced before admission.
	ErrHostUpdateUnauthorized = errors.New("the paired connection is no longer current")
)

// HostUpdateOptions connects the process's install-specific implementation to
// the transport-independent host job. Install returns nil after success or an
// acknowledged transfer to a coordinator that will own all subsequent writes.
type HostUpdateOptions struct {
	Check     func(context.Context, bool) Result
	Eligible  func() bool
	Install   func(context.Context, *HostJob) error
	Protected bool // a protected install requires a phone that can verify the trial
}

// HostService owns confirmed paired updates after the requesting link leaves.
type HostService struct {
	ctx     context.Context
	cancel  context.CancelFunc
	dataDir string
	current string
	opts    HostUpdateOptions
	reserve func() (func(), error)
	log     *slog.Logger

	mu   sync.Mutex
	done chan struct{}
	job  *HostJob
}

// NewHostService keeps install work inside the engine's lifetime until it has
// an explicit successor owner. reserve atomically admits work against idle exit.
func NewHostService(ctx context.Context, dataDir, current string, opts HostUpdateOptions, reserve func() (func(), error), log *slog.Logger) *HostService {
	ctx, cancel := context.WithCancel(ctx)
	return &HostService{ctx: ctx, cancel: cancel, dataDir: dataDir, current: Canonical(current), opts: opts, reserve: reserve, log: log}
}

// Supported reports whether this process can offer an installation.
func (s *HostService) Supported() bool {
	return s != nil && s.current != "" && s.opts.Eligible() && s.ctx.Err() == nil
}

// Check looks up a compatible release without performing an installation.
func (s *HostService) Check(ctx context.Context, fresh bool) Result {
	if !s.Supported() {
		return Result{Status: "unsupported", Current: s.current, Message: ErrHostUpdateUnsupported.Error()}
	}
	result := s.opts.Check(ctx, fresh)
	if result.Status == "error" {
		s.log.Warn("could not check Linux releases", "reason", result.Message)
		result.Message = "Could not check the official Linux releases."
	}
	return result
}

// Start admits one explicit operation. The original receipt wins on replay,
// even after installation or a later engine restart.
func (s *HostService) Start(id, version string, authorize func() bool) (HostUpdate, error) {
	return s.StartFor(id, version, "", authorize)
}

// StartFor binds a new protected attempt to its authenticated initiating phone.
func (s *HostService) StartFor(id, version, peer string, authorize func() bool) (HostUpdate, error) {
	path, err := hostJobPath(s.dataDir, id)
	if err != nil || version == "" || Canonical(version) != version {
		return HostUpdate{}, ErrHostUpdateInput
	}
	id = strings.ToLower(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if previous, err := s.statusLocked(id); err == nil {
		if previous.Version != version {
			return HostUpdate{}, ErrHostUpdateInput
		}
		return previous, nil
	} else if !errors.Is(err, ErrHostUpdateNotFound) {
		return HostUpdate{}, err
	}
	if s.job != nil {
		return HostUpdate{}, ErrBusy
	}
	if !s.Supported() {
		return HostUpdate{}, ErrHostUpdateUnsupported
	}
	if s.opts.Protected && peer == "" {
		return HostUpdate{}, ErrHostUpdateUnsupported
	}
	if !Newer(version, s.current) {
		return HostUpdate{}, ErrHostUpdateInput
	}
	release, err := s.reserve()
	if err != nil {
		return HostUpdate{}, err
	}
	if !authorize() || s.ctx.Err() != nil {
		release()
		return HostUpdate{}, ErrHostUpdateUnauthorized
	}
	receipt := HostUpdate{OperationID: id, Version: version, Stage: "checking"}
	job := &HostJob{path: path, info: receipt, peer: peer}
	if err := job.SetStage(receipt.Stage); err != nil {
		release()
		return HostUpdate{}, fmt.Errorf("save host update receipt: %w", err)
	}
	s.job, s.done = job, make(chan struct{})
	go s.install(job, release, s.done)
	return receipt, nil
}

func (s *HostService) install(job *HostJob, release func(), done chan struct{}) {
	defer close(done)
	defer func() {
		s.mu.Lock()
		s.job = nil
		s.mu.Unlock()
		release()
	}()
	if err := s.opts.Install(s.ctx, job); err != nil {
		info := job.Snapshot()
		s.log.Warn("Linux host update failed", "operation", info.OperationID, "stage", info.Stage, "err", err)
		if writeErr := job.SetError("failed", hostInstallFailure(info.Stage, err)); writeErr != nil {
			s.log.Warn("could not persist host update failure", "operation", info.OperationID, "err", writeErr)
		}
	}
}

func hostInstallFailure(stage string, err error) error {
	switch {
	case errors.Is(err, ErrBusy):
		return ErrBusy
	case errors.Is(err, context.Canceled):
		return errors.New("The host stopped before its update finished.")
	}
	switch stage {
	case "checking":
		return errors.New("The confirmed Linux update is no longer available or could not be checked.")
	case "downloading":
		return errors.New("Could not download or verify the official Linux update.")
	case "saving":
		return errors.New("Could not save the running tasks for a safe restart.")
	default:
		return errors.New("Could not install or restart the Linux update.")
	}
}

// Status reads one attempt, using live byte counts only while this engine
// owns it. A lost pre-handoff job cannot still be installing: transferring
// ownership to the coordinator requires a durable restarting receipt.
func (s *HostService) Status(id string) (HostUpdate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked(strings.ToLower(id))
}

func (s *HostService) statusLocked(id string) (HostUpdate, error) {
	if s.job != nil {
		if snapshot := s.job.Snapshot(); snapshot.OperationID == id {
			return snapshot, nil
		}
	}
	job, err := OpenHostJob(s.dataDir, id)
	if err != nil {
		return HostUpdate{}, err
	}
	info := job.Snapshot()
	switch info.Stage {
	case "checking", "downloading", "installing", "saving":
		info.Stage = "failed"
		info.Error = "The previous host update stopped before starting the replacement."
	}
	return info, nil
}

// Close cancels and joins this process's installer before tearing down the
// engine. It has no authority over a coordinator that took ownership.
func (s *HostService) Close() {
	s.cancel()
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}
