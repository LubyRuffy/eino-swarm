package update

import "io"

// Progress is the current installation attempt. TotalBytes is zero when the
// release download did not advertise its size; DownloadedBytes is always real
// body data received, never an estimated percentage.
type Progress struct {
	Stage           string `json:"stage"`
	Version         string `json:"version,omitempty"`
	DownloadedBytes int64  `json:"downloaded_bytes"`
	TotalBytes      int64  `json:"total_bytes"`
	Error           string `json:"error,omitempty"`
}

// ProgressReporter optionally supplements Checker without changing the
// synchronous Apply contract used by existing integrations.
type ProgressReporter interface {
	Progress() Progress
}

// Progress returns an independent snapshot safe to poll while Apply runs.
func (s *Service) Progress() Progress {
	s.mu.Lock()
	defer s.mu.Unlock()
	snapshot := s.progress
	if snapshot.Stage == "" {
		snapshot.Stage = "idle"
	}
	return snapshot
}

func (s *Service) setStage(stage string) {
	s.mu.Lock()
	s.progress.Stage = stage
	s.mu.Unlock()
}

type downloadReader struct {
	io.Reader
	service *Service
}

func (r downloadReader) Read(p []byte) (int, error) {
	n, err := r.Reader.Read(p)
	r.service.mu.Lock()
	r.service.progress.DownloadedBytes += int64(n)
	r.service.mu.Unlock()
	return n, err
}
