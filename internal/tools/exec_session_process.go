package tools

import "errors"

// Session finalization must report uncertain cleanup even if the process
// deadline or Stop also fired.
var errManagedExecCleanup = errors.New("managed command cleanup could not be confirmed")

// A whole process group can be stopped by the kernel without exiting. Keep
// that cause separate from the caller's deadline and manual interruption.
var errManagedExecStalled = errors.New("managed command process group remained stopped")
