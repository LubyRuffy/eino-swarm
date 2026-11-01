package main

import (
	"net/http"
	"time"
)

// Deliberately separate native URLSession callbacks: the first complete frame
// advances Foundation Data's startIndex; the short second packet and incomplete
// Chinese/emoji bytes must remain valid through every later append and read.
// The caller holds f.mu, which is released between writes for fixture observers.
func (f *fixture) chunkedModel(w http.ResponseWriter, r *http.Request) {
	chunks := [][]byte{
		[]byte("data: {\"choices\":[{\"delta\":{\"content\":\"Native streamed \"}}]}\n\n"),
		[]byte("d"),
		[]byte("ata: {\"choices\":[{\"delta\":{\"content\":\""),
		{0xe4}, {0xb8, 0xad}, // 中: one byte, then two bytes.
		{0xe6, 0x96}, {0x87}, // 文: two bytes, then one byte.
		{0xf0}, {0x9f, 0x99}, {0x82}, // 🙂: one, two, then one byte.
		[]byte(" complete\"},\"finish_reason\":\"stop\"}]}\n\n"),
		[]byte("data: [DONE]\n\n"),
	}
	for index, chunk := range chunks {
		if _, err := w.Write(chunk); err != nil {
			f.counts["model_chunk_failed"]++
			return
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			f.counts["model_chunk_failed"]++
			return
		}
		f.counts["model_chunk_flushes"]++
		if index == len(chunks)-1 {
			break
		}
		f.mu.Unlock()
		timer := time.NewTimer(150 * time.Millisecond)
		select {
		case <-r.Context().Done():
			timer.Stop()
			f.mu.Lock()
			f.counts["model_chunk_cancelled"]++
			return
		case <-timer.C:
		}
		f.mu.Lock()
	}
	f.counts["model_successes"]++
	f.counts["model_chunked_completed"]++
}
