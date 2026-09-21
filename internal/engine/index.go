package engine

// ThreadIndex updates conversation search after the transcript changes.
// Optional: tests leave it nil so a unit test is not a search test.
type ThreadIndex interface {
	Index(threadID string)
	Remove(threadID string)
}

// SetThreadIndex installs the search indexer. App.New is the production caller.
func (e *Engine) SetThreadIndex(idx ThreadIndex) {
	e.index = idx
}

func (e *Engine) reindex(threadID string) {
	if e.index == nil || threadID == "" {
		return
	}
	e.index.Index(threadID)
}

func (e *Engine) dropIndex(threadID string) {
	if e.index == nil || threadID == "" {
		return
	}
	e.index.Remove(threadID)
}
