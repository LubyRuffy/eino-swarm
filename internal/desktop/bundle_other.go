//go:build !darwin

package desktop

// ReexecIfUnbundled is a no-op off macOS: Local Network privacy is an
// Apple TCC rule. Linux/Windows desktop already dials LAN endpoints.
func ReexecIfUnbundled() error { return nil }
