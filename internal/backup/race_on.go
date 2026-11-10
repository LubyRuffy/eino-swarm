//go:build race

package backup

// backupRace is true when the race detector is running. The injected-failure
// sweep is a long sequential walk; the detector makes each snapshot too slow
// for the package timeout, and the same calls still run in the other tests.
const backupRace = true
