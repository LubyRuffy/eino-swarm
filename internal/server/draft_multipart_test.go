package server

import (
	"bytes"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func TestWebKitFilenameControlsDoNotChangeTheFile(t *testing.T) {
	boundary := "----WebKitFormBoundaryTest0001"
	payload := bytes.Join([][]byte{
		{0x50, 0x4b, 0x03, 0x04},
		bytes.Repeat([]byte{0x01, 0x7f, 0x00, '\n', '\r'}, 3000),
	}, nil)
	raw := webkitDraftBody(t, boundary, "FOFA_\x7f\x01name.xlsx", payload)
	reader := multipart.NewReader(bytes.NewReader(raw), boundary)
	if _, err := reader.ReadForm(1 << 20); err == nil {
		t.Fatal("Go's parser accepted a WebKit filename containing DEL and a control byte")
	}

	relaxed := readRelaxed(t, raw, boundary)
	parsed := multipart.NewReader(bytes.NewReader(relaxed), boundary)
	form, err := parsed.ReadForm(1 << 20)
	if err != nil {
		t.Fatalf("relaxed form: %v", err)
	}
	defer form.RemoveAll()
	headers := form.File["file.f1"]
	if len(headers) != 1 {
		t.Fatalf("file parts=%d", len(headers))
	}
	if headers[0].Filename != "FOFA___name.xlsx" {
		t.Fatalf("filename=%q", headers[0].Filename)
	}
	opened, err := headers[0].Open()
	if err != nil {
		t.Fatal(err)
	}
	defer opened.Close()
	got, err := io.ReadAll(opened)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("file bytes changed: got %d want %d", len(got), len(payload))
	}
	lookalike := []byte("\r\n--" + boundary + "\r\n\x01\x02\r\n\r\nKEEP")
	withLookalike := webkitDraftBody(t, boundary, "notes.txt", append(append([]byte{}, payload...), lookalike...))
	relaxedLookalike := readRelaxed(t, withLookalike, boundary)
	if !bytes.Contains(relaxedLookalike, lookalike) {
		t.Fatal("a boundary-shaped sequence inside the file was rewritten")
	}
}

func TestOrdinaryMultipartIsUnchanged(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("snapshot", `{"text":"分析文档"}`); err != nil {
		t.Fatal(err)
	}
	part, err := writer.CreateFormFile("file.f1", "FOFA_Global_Crypto_Exchange_2026.xlsx")
	if err != nil {
		t.Fatal(err)
	}
	payload := bytes.Repeat([]byte{0, 255, 13, 10, 1, 127}, 5000)
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	raw := body.Bytes()
	relaxed := readRelaxed(t, raw, writer.Boundary())
	if !bytes.Equal(raw, relaxed) {
		t.Fatalf("ordinary multipart changed (%d vs %d)", len(raw), len(relaxed))
	}
}

func TestDraftMultipartReasonDistinguishesAShortUpload(t *testing.T) {
	if draftMultipartReason(io.ErrUnexpectedEOF) != "the attachment upload ended before the file was complete" {
		t.Fatal(draftMultipartReason(io.ErrUnexpectedEOF))
	}
	var tooBig *http.MaxBytesError
	tooBig = &http.MaxBytesError{Limit: 1}
	if !strings.Contains(draftMultipartReason(tooBig), "256 MiB") {
		t.Fatal(draftMultipartReason(tooBig))
	}
}

func webkitDraftBody(t *testing.T, boundary, filename string, payload []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	write := func(s string) {
		t.Helper()
		if _, err := b.WriteString(s); err != nil {
			t.Fatal(err)
		}
	}
	write("--" + boundary + "\r\n")
	write("Content-Disposition: form-data; name=\"snapshot\"\r\n\r\n")
	write("{}\r\n")
	write("--" + boundary + "\r\n")
	write("Content-Disposition: form-data; name=\"file.f1\"; filename=\"")
	if _, err := b.WriteString(filename); err != nil {
		t.Fatal(err)
	}
	write("\"\r\nContent-Type: application/octet-stream\r\n\r\n")
	if _, err := b.Write(payload); err != nil {
		t.Fatal(err)
	}
	write("\r\n--" + boundary + "--\r\n")
	return b.Bytes()
}

func readRelaxed(t *testing.T, raw []byte, boundary string) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1/draft", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	relaxMultipartBody(req)
	got, err := io.ReadAll(req.Body)
	if err != nil {
		t.Fatal(err)
	}
	chunked := readRelaxedChunks(t, raw, boundary, 1)
	if !bytes.Equal(got, chunked) {
		t.Fatalf("1-byte reads differ (%d vs %d)", len(got), len(chunked))
	}
	chunked = readRelaxedChunks(t, raw, boundary, 7)
	if !bytes.Equal(got, chunked) {
		t.Fatalf("7-byte reads differ (%d vs %d)", len(got), len(chunked))
	}
	return got
}

func readRelaxedChunks(t *testing.T, raw []byte, boundary string, size int) []byte {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, "http://127.0.0.1/draft", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	relaxMultipartBody(req)
	var out bytes.Buffer
	buf := make([]byte, size)
	for {
		n, err := req.Body.Read(buf)
		out.Write(buf[:n])
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	return out.Bytes()
}
