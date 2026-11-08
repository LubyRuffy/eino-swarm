package server

import (
	"bytes"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

// relaxMultipartBody rewrites part headers before Go parses them.
// WebKit puts the original filename in Content-Disposition and percent-encodes
// only CR, LF and quotes. Other C0 controls and DEL are legal in a file name
// and illegal in a Go MIME header, so ParseMultipartForm rejects the whole
// draft — text included — before the snapshot is read. Replacement is limited
// to header blocks that begin with a real header name. Part bodies, including
// the same bytes inside a document, stay as they were.
func relaxMultipartBody(r *http.Request) {
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return
	}
	boundary := params["boundary"]
	if boundary == "" || strings.ContainsAny(boundary, "\r\n") {
		return
	}
	r.Body = io.NopCloser(&headerRelaxer{in: r.Body, marker: []byte("--" + boundary), begin: true})
}

type headerRelaxer struct {
	in     io.Reader
	marker []byte
	hold   []byte
	out    []byte
	eof    bool
	begin  bool
	mode   int // 0 seek, 1 header, 2 remainder
	failed error
}

func (z *headerRelaxer) Read(p []byte) (int, error) {
	for len(z.out) == 0 && z.failed == nil {
		if z.produce() {
			continue
		}
		if !z.eof && z.failed == nil {
			if z.readMore() {
				continue
			}
		}
		if len(z.hold) > 0 && z.mode != 2 {
			z.out = append(z.out, z.hold...)
			z.hold = nil
			continue
		}
		if z.failed == nil {
			z.failed = io.EOF
		}
	}
	if len(z.out) == 0 {
		return 0, z.failed
	}
	n := copy(p, z.out)
	z.out = z.out[n:]
	return n, nil
}

func (z *headerRelaxer) readMore() bool {
	buf := make([]byte, 8192)
	n, err := z.in.Read(buf)
	if n > 0 {
		z.hold = append(z.hold, buf[:n]...)
	}
	if err == io.EOF {
		z.eof = true
		return n > 0
	}
	if err != nil {
		z.failed = err
		return false
	}
	return n > 0
}

func (z *headerRelaxer) produce() bool {
	switch z.mode {
	case 2:
		if len(z.hold) == 0 {
			return false
		}
		z.out = append(z.out, z.hold...)
		z.hold = nil
		return true
	case 1:
		content, after, ok := splitHeader(z.hold)
		if !ok {
			if z.eof || len(z.hold) >= 64*1024 {
				z.mode = 0
				return true
			}
			return false
		}
		headers := z.hold[:content]
		if mimeHeaderBlock(headers) {
			headers = relaxHeaderBytes(headers)
		}
		z.out = append(z.out, headers...)
		z.out = append(z.out, z.hold[content:after]...)
		z.hold = append([]byte(nil), z.hold[after:]...)
		z.mode = 0
		return true
	default:
		if z.begin {
			if z.takeBoundary(true) {
				return true
			}
			if !z.startSettled() {
				return false
			}
			z.begin = false
		}
		if z.takeBoundary(false) {
			return true
		}
		keep := len(z.marker) + 128
		// The newline that introduces a boundary has to stay with that boundary.
		// Cutting it off leaves "--boundary" at offset 0, which is only a
		// delimiter at the start of the message.
		if !z.eof && len(z.hold) > keep {
			cut := len(z.hold) - keep
			if cut > 0 && z.hold[cut-1] == '\n' {
				cut--
			}
			if cut > 0 {
				z.out = append(z.out, z.hold[:cut]...)
				z.hold = append([]byte(nil), z.hold[cut:]...)
				return true
			}
		}
		return false
	}
}

func (z *headerRelaxer) takeBoundary(atStart bool) bool {
	_, end, closing, ok := boundaryLine(z.hold, z.marker, atStart)
	if !ok {
		return false
	}
	z.out = append(z.out, z.hold[:end]...)
	z.hold = append([]byte(nil), z.hold[end:]...)
	z.begin = false
	if closing {
		z.mode = 2
	} else {
		z.mode = 1
	}
	return true
}

func (z *headerRelaxer) startSettled() bool {
	if bytes.HasPrefix(z.marker, z.hold) && !z.eof {
		return false
	}
	if bytes.HasPrefix(z.hold, z.marker) && bytes.IndexByte(z.hold[len(z.marker):], '\n') < 0 && !z.eof {
		return false
	}
	return z.eof || len(z.hold) > 0
}

// boundaryLine reports a delimiter line. atStart allows the marker at offset 0;
// every later delimiter is preceded by a newline, which belongs to the previous body.
func boundaryLine(buf, marker []byte, atStart bool) (start, end int, closing, ok bool) {
	from := 0
	for {
		rel := bytes.Index(buf[from:], marker)
		if rel < 0 {
			return 0, 0, false, false
		}
		j := from + rel
		if j == 0 {
			if !atStart {
				from = j + 1
				continue
			}
		} else if buf[j-1] != '\n' {
			from = j + 1
			continue
		}
		rest := buf[j+len(marker):]
		k := 0
		for k < len(rest) && (rest[k] == ' ' || rest[k] == '\t') {
			k++
		}
		closing = false
		if k+1 < len(rest) && rest[k] == '-' && rest[k+1] == '-' {
			closing = true
			k += 2
			for k < len(rest) && (rest[k] == ' ' || rest[k] == '\t') {
				k++
			}
		}
		if k == len(rest) {
			return 0, 0, false, false
		}
		if rest[k] == '\r' {
			k++
			if k == len(rest) {
				return 0, 0, false, false
			}
		}
		if rest[k] != '\n' {
			from = j + 1
			continue
		}
		return j, j + len(marker) + k + 1, closing, true
	}
}

func splitHeader(buf []byte) (content, after int, ok bool) {
	crlf := bytes.Index(buf, []byte("\r\n\r\n"))
	lf := bytes.Index(buf, []byte("\n\n"))
	switch {
	case crlf < 0 && lf < 0:
		return 0, 0, false
	case crlf >= 0 && (lf < 0 || crlf <= lf):
		return crlf, crlf + 4, true
	default:
		return lf, lf + 2, true
	}
}

func mimeHeaderBlock(headers []byte) bool {
	line, _, _ := bytes.Cut(headers, []byte("\n"))
	line = bytes.TrimRight(line, "\r")
	name, _, ok := bytes.Cut(line, []byte(":"))
	if !ok || len(name) == 0 || bytes.IndexByte(name, ' ') >= 0 {
		return false
	}
	for _, c := range name {
		if !headerNameByte(c) {
			return false
		}
	}
	return true
}

func headerNameByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || strings.ContainsRune("!#$%&'*+-.^_`|~", rune(c))
}

func relaxHeaderBytes(in []byte) []byte {
	out := make([]byte, len(in))
	for i, c := range in {
		if c == '\r' || c == '\n' || c == '\t' || (c >= 0x20 && c != 0x7f) {
			out[i] = c
			continue
		}
		out[i] = '_'
	}
	return out
}

func draftMultipartReason(err error) string {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		return "draft attachments exceed 256 MiB"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(err.Error(), "unexpected EOF") {
		return "the attachment upload ended before the file was complete"
	}
	return "cannot read draft multipart body"
}
