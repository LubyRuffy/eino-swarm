package remote

import (
	"errors"
	"fmt"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

var (
	// ErrAccountOperationInProgress prevents overlapping account mutations while
	// leaving Host status and explicit lifecycle cancellation responsive.
	ErrAccountOperationInProgress = errors.New("remote: another account operation is in progress")
	// ErrAccountEnableUnavailable identifies a failed account configuration save.
	ErrAccountEnableUnavailable = errors.New("remote: account connection settings could not be saved")
)

// StartLogin requires an already-enabled account connection.
func (h *Host) StartLogin(open func(string) error, newDevice bool) error {
	return h.startAccountLogin(open, newDevice, false)
}

// StartAccountLogin atomically reserves account mutation ownership before an
// explicit desktop entry enables configuration, reloads and starts its flow.
func (h *Host) StartAccountLogin(open func(string) error, newDevice bool) error {
	return h.startAccountLogin(open, newDevice, true)
}

func (h *Host) startAccountLogin(open func(string) error, newDevice, enable bool) error {
	if !h.accountMu.TryLock() {
		return ErrAccountOperationInProgress
	}
	defer h.accountMu.Unlock()
	if open == nil {
		return errors.New("remote: system browser is unavailable")
	}
	current := h.cfg.Snapshot().Remote
	if enable && (!current.Enabled || current.AuthMode != config.RemoteAuthAccount) {
		if h.eng == nil {
			return ErrAccountEnableUnavailable
		}
		if err := h.eng.UpdateConfig(func(next *config.Config) error {
			next.Remote.Enabled = true
			next.Remote.AuthMode = config.RemoteAuthAccount
			return nil
		}); err != nil {
			return fmt.Errorf("%w: %v", ErrAccountEnableUnavailable, err)
		}
		h.Reload()
	}
	return h.startLogin(open, newDevice)
}
