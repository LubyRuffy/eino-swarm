package backup

import "context"

// Phases are the stages a backup or restore can be in. The settings page
// reads them while the transfer is still running. The strings are the API.
const (
	PhaseSnapshot Phase = "snapshot"
	PhaseFiles    Phase = "files"
	PhaseArchive  Phase = "archive"
	PhaseRestore  Phase = "restore"
)

// Phase is one step of a backup or restore.
type Phase string

// Progress is how far the current transfer has got. Total is zero while the
// step has no known size, which is the database copy. Done and Total are
// bytes while the archive is being written, and a file count while trees
// are being read.
type Progress struct {
	Running bool  `json:"running"`
	Phase   Phase `json:"phase,omitempty"`
	Done    int64 `json:"done"`
	Total   int64 `json:"total"`
}

// Reporter receives progress. It must return quickly and must not start
// another backup or restore.
type Reporter func(Progress)

type reporterKey struct{}

// WithReporter attaches a progress receiver to ctx. A nil receiver leaves
// ctx unchanged, so callers that do not show progress pay nothing.
func WithReporter(ctx context.Context, report Reporter) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if report == nil {
		return ctx
	}
	return context.WithValue(ctx, reporterKey{}, report)
}

func reportProgress(ctx context.Context, p Progress) {
	if ctx == nil {
		return
	}
	report, _ := ctx.Value(reporterKey{}).(Reporter)
	if report == nil {
		return
	}
	report(p)
}
