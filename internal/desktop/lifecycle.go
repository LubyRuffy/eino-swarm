package desktop

import "log/slog"

// AppKit may terminate without returning to Run's caller. Write the cause
// synchronously before asking the native loop to quit.
func nativeFatalQuit(log *slog.Logger, quit func()) func(error) {
	return func(err error) {
		log.Error("desktop engine connection lost", "error", err)
		quit()
	}
}

// Wails ignores Quit before its platform app exists. Keep an early connection
// failure until ApplicationStarted, or return it if Run itself ends first.
func runWithFatal(run func() error, quit func(error), started <-chan struct{}, fatal <-chan error) error {
	if fatal == nil {
		return run()
	}
	select {
	case err, ok := <-fatal:
		if ok && err != nil {
			return err
		}
		if !ok {
			return run()
		}
	default:
	}
	done := make(chan struct{})
	observed := make(chan error, 1)
	go func() {
		select {
		case err, ok := <-fatal:
			// Preserve the cause before requesting Quit: native Run may return
			// concurrently, when its main loop can no longer service that call.
			observed <- err
			if ok && err != nil {
				select {
				case <-started:
					select {
					case <-done:
					default:
						quit(err)
					}
				case <-done:
				}
			}
		case <-done:
			// Prefer a terminal cause already queued when normal exit wins
			// the select, but never wait for a failure after the app has left.
			select {
			case err := <-fatal:
				observed <- err
			default:
				observed <- nil
			}
		}
	}()
	err := run()
	close(done)
	if failure := <-observed; failure != nil {
		return failure
	}
	return err
}
