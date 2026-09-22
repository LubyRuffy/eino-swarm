package remote

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
)

// PutChunkRaw is the decoded size of one put frame. Base64 expands by 4/3
// and the JSON envelope has to stay under pairlink's 64KiB plaintext cap.
// The phone chunks at the same size; a test pins a full frame under the cap.
const PutChunkRaw = 36 << 10

// maxStagedPuts bounds how many unfinished uploads one link may hold.
const maxStagedPuts = 32

// maxStagedBytes is the sum of decoded bytes staged on one link. Tests lower
// it so the cap does not require allocating the real upload limit.
var maxStagedBytes = engine.MaxUploadBytes

// Staging holds chunked uploads for one phone link. They are not a thread's
// files until start/send/steer consumes them: a new conversation has no id yet.
type Staging struct {
	mu    sync.Mutex
	puts  map[string]*partial
	bytes int
}

type partial struct {
	name  string
	mime  string
	parts int
	got   map[int][]byte
	size  int
}

// StagedBlob is one finished upload, still in memory.
type StagedBlob struct {
	Name string
	MIME string
	Data []byte
}

func NewStaging() *Staging {
	return &Staging{puts: map[string]*partial{}}
}

func (s *Staging) Accept(req Request) (PutView, error) {
	if s == nil {
		return PutView{}, fmt.Errorf("remote: upload is not available")
	}
	id := strings.TrimSpace(req.PutID)
	if !putIDOK(id) {
		return PutView{}, fmt.Errorf("remote: upload id is not usable")
	}
	name, err := cleanPutName(req.Name)
	if err != nil {
		return PutView{}, err
	}
	mime := strings.TrimSpace(req.MIME)
	if len(mime) > 128 {
		return PutView{}, fmt.Errorf("remote: upload type is not usable")
	}
	if req.Parts < 1 || req.Parts > maxPutParts() {
		return PutView{}, fmt.Errorf("remote: upload part count is not usable")
	}
	if req.Part < 1 || req.Part > req.Parts {
		return PutView{}, fmt.Errorf("remote: upload part is out of range")
	}
	raw, err := base64.StdEncoding.DecodeString(req.Data)
	if err != nil {
		return PutView{}, fmt.Errorf("remote: upload data was not valid base64")
	}
	if len(raw) == 0 || len(raw) > PutChunkRaw {
		return PutView{}, fmt.Errorf("remote: upload chunk is empty or over the frame cap")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.puts == nil {
		s.puts = map[string]*partial{}
	}
	p := s.puts[id]
	if p == nil {
		if len(s.puts) >= maxStagedPuts {
			return PutView{}, fmt.Errorf("remote: too many uploads are waiting on this link")
		}
		p = &partial{name: name, mime: mime, parts: req.Parts, got: map[int][]byte{}}
		s.puts[id] = p
	} else if p.name != name || p.mime != mime || p.parts != req.Parts {
		return PutView{}, fmt.Errorf("remote: upload %s changed shape mid-transfer", id)
	}
	if prev, ok := p.got[req.Part]; ok {
		if !bytes.Equal(prev, raw) {
			return PutView{}, fmt.Errorf("remote: upload %s resent part %d with different bytes", id, req.Part)
		}
		return p.view(id), nil
	}
	if s.bytes+len(raw) > maxStagedBytes {
		if len(p.got) == 0 {
			delete(s.puts, id)
		}
		return PutView{}, fmt.Errorf("remote: staged uploads are over the %d MiB limit", maxStagedBytes>>20)
	}
	p.got[req.Part] = raw
	p.size += len(raw)
	s.bytes += len(raw)
	return p.view(id), nil
}

// Take returns finished uploads and drops them. An unknown or unfinished id
// takes nothing, so a failed send can retry the same ids.
func (s *Staging) Take(ids []string) ([]StagedBlob, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	if s == nil {
		return nil, fmt.Errorf("remote: upload is not available")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]StagedBlob, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			return nil, fmt.Errorf("remote: upload id is not usable")
		}
		seen[id] = true
		p := s.puts[id]
		if p == nil || !p.complete() {
			return nil, fmt.Errorf("remote: upload %s is not complete", id)
		}
		out = append(out, StagedBlob{Name: p.name, MIME: p.mime, Data: p.bytes()})
	}
	for id := range seen {
		s.bytes -= s.puts[id].size
		delete(s.puts, id)
	}
	return out, nil
}

func (p *partial) complete() bool {
	if p == nil || p.parts < 1 || len(p.got) != p.parts {
		return false
	}
	for i := 1; i <= p.parts; i++ {
		if len(p.got[i]) == 0 {
			return false
		}
	}
	return true
}

func (p *partial) bytes() []byte {
	out := make([]byte, 0, p.size)
	for i := 1; i <= p.parts; i++ {
		out = append(out, p.got[i]...)
	}
	return out
}

func (p *partial) view(id string) PutView {
	ready := p.complete()
	v := PutView{ID: id, Name: p.name, Ready: ready}
	if ready {
		if visionClaim(p.mime, p.name) {
			v.Kind = "image"
		} else {
			v.Kind = "file"
		}
	}
	return v
}

func putIDOK(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func cleanPutName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, `/\`) || name == "." || name == ".." {
		return "", fmt.Errorf("remote: upload has no usable file name")
	}
	if filepath.Base(name) != name {
		return "", fmt.Errorf("remote: upload has no usable file name")
	}
	return name, nil
}

func maxPutParts() int {
	n := engine.MaxUploadBytes / PutChunkRaw
	if engine.MaxUploadBytes%PutChunkRaw != 0 {
		n++
	}
	if n < 1 {
		return 1
	}
	return n
}

func visionClaim(mime, name string) bool {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(mime)), "image/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp":
		return true
	default:
		return false
	}
}
