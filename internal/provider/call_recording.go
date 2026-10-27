package provider

import "context"

type callRecordingKey struct{}

type callRecording struct {
	begin   func() Recorder
	backoff Recorder
}

// WithCallRecorders lets a long-lived worker select its request's recorder
// after admission and before the model starts. That recorder remains fixed
// through the response stream. Backoff observations use their own recorder,
// because a wait may cross an ownership boundary before HTTP is admitted.
func WithCallRecorders(ctx context.Context, begin func() Recorder, backoff Recorder) context.Context {
	return context.WithValue(ctx, callRecordingKey{}, callRecording{begin: begin, backoff: backoff})
}

func (r *recordingModel) requestRecorder(ctx context.Context) Recorder {
	if recording, ok := ctx.Value(callRecordingKey{}).(callRecording); ok && recording.begin != nil {
		return recording.begin()
	}
	return r.rec
}

func (r *recordingModel) backoffRecorder(ctx context.Context) Recorder {
	if recording, ok := ctx.Value(callRecordingKey{}).(callRecording); ok {
		return recording.backoff
	}
	return r.rec
}
