package provider

// CallError identifies an error returned by the model itself. Tool argument
// decoding happens after this boundary and must not be confused with a broken
// HTTP response or stream. Its text and underlying SDK error stay intact.
type CallError struct {
	Cause error
}

func (e *CallError) Error() string { return e.Cause.Error() }
func (e *CallError) Unwrap() error { return e.Cause }

func modelCallError(err error) error {
	if err == nil {
		return nil
	}
	return &CallError{Cause: err}
}
