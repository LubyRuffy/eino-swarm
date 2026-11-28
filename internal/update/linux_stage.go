package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"debug/elf"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	maxLinuxArchive        = 512 << 20
	maxLinuxExecutable     = 512 << 20
	maxLinuxTarOverhead    = 2 << 20
	maxLinuxTarPadding     = 10 << 10
	linuxVersionCheckLimit = 10 * time.Second
)

// Stage resolves the confirmed version, verifies its official archive and
// returns a verified executable staged in the installer's private directory.
// It never modifies the installed executable.
func (f *LinuxFeed) Stage(ctx context.Context, version, dir string, report func(downloaded, total int64)) (string, error) {
	offer, err := f.resolve(ctx, version)
	if err != nil {
		return "", err
	}
	archive, err := os.CreateTemp(dir, ".zwai-update-*.tar.gz")
	if err != nil {
		return "", fmt.Errorf("stage Linux download: %w", err)
	}
	defer os.Remove(archive.Name())
	defer archive.Close()
	if err := f.download(ctx, offer, archive, report); err != nil {
		return "", err
	}
	if _, err := archive.Seek(0, io.SeekStart); err != nil {
		return "", fmt.Errorf("read verified Linux archive: %w", err)
	}
	return f.stageExecutable(ctx, archive, offer.Version, dir)
}

func (f *LinuxFeed) stageExecutable(ctx context.Context, archive io.Reader, version, dir string) (_ string, err error) {
	exe, err := os.CreateTemp(dir, ".zwai-update-executable-*")
	if err != nil {
		return "", fmt.Errorf("stage Linux executable: %w", err)
	}
	defer func() {
		if err != nil {
			_ = exe.Close()
			_ = os.Remove(exe.Name())
		}
	}()
	if err := extractLinuxExecutable(ctx, archive, exe, linuxMachine(f.Arch)); err != nil {
		return "", err
	}
	if err := exe.Chmod(0o755); err != nil {
		return "", err
	}
	if err := exe.Sync(); err != nil {
		return "", err
	}
	if err := exe.Close(); err != nil {
		return "", err
	}
	verify := f.verifyVersion
	if verify == nil {
		verify = verifyLinuxVersion
	}
	if err := verify(ctx, exe.Name(), version); err != nil {
		return "", err
	}
	return exe.Name(), nil
}

func (f *LinuxFeed) download(ctx context.Context, offer *linuxOffer, dest io.Writer, report func(downloaded, total int64)) error {
	res, err := fetchRelease(ctx, offer.AssetURL, f.Fetch)
	if err != nil {
		var requestError *url.Error
		if errors.As(err, &requestError) {
			err = requestError.Err
		}
		return fmt.Errorf("download Linux update: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download Linux update: HTTP %d", res.StatusCode)
	}
	if res.ContentLength > maxLinuxArchive {
		return errors.New("the Linux release archive is too large")
	}
	sum := sha256.New()
	meter := linuxDownloadMeter{
		Writer: io.MultiWriter(dest, sum),
		total:  max(0, res.ContentLength),
		report: report,
	}
	if report != nil {
		report(0, meter.total)
	}
	body := linuxContextReader{ctx, res.Body}
	n, err := io.Copy(&meter, io.LimitReader(body, maxLinuxArchive+1))
	if err != nil {
		return fmt.Errorf("read Linux update download: %w", err)
	}
	if n > maxLinuxArchive {
		return errors.New("the Linux release archive is too large")
	}
	if meter.total > 0 && n != meter.total {
		return errors.New("the Linux release download size did not match its length")
	}
	if !bytes.Equal(sum.Sum(nil), offer.digest[:]) {
		return errors.New("the Linux release archive did not match its SHA-256 digest")
	}
	return nil
}

type linuxDownloadMeter struct {
	io.Writer
	downloaded, total int64
	report            func(downloaded, total int64)
}

func (m *linuxDownloadMeter) Write(data []byte) (int, error) {
	n, err := m.Writer.Write(data)
	m.downloaded += int64(n)
	if m.report != nil {
		m.report(m.downloaded, m.total)
	}
	return n, err
}

type linuxContextReader struct {
	ctx context.Context
	io.Reader
}

func (r linuxContextReader) Read(data []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(data)
}

func extractLinuxExecutable(ctx context.Context, source io.Reader, exe *os.File, machine uint16) error {
	compressed := bufio.NewReader(linuxContextReader{ctx, source})
	gz, err := gzip.NewReader(compressed)
	if err != nil {
		return fmt.Errorf("read Linux gzip archive: %w", err)
	}
	defer gz.Close()
	gz.Multistream(false)
	body := &io.LimitedReader{R: linuxContextReader{ctx, gz}, N: maxLinuxExecutable + maxLinuxTarOverhead}
	archive := tar.NewReader(body)
	entry, err := archive.Next()
	if err != nil {
		return fmt.Errorf("read Linux archive entry: %w", err)
	}
	if entry.Name != "zwai" || entry.Typeflag != tar.TypeReg || entry.Linkname != "" ||
		entry.Mode != 0o755 || entry.Size < 64 || entry.Size > maxLinuxExecutable {
		return errors.New("the Linux archive must contain one regular executable zwai")
	}
	// GNU sparse PAX metadata can describe a different logical file than
	// the archive's ordinary data entry. The release writes a dense file.
	for key := range entry.PAXRecords {
		if strings.HasPrefix(key, "GNU.sparse.") {
			return errors.New("the Linux archive contains a sparse executable")
		}
	}
	if _, err := io.Copy(exe, archive); err != nil {
		return fmt.Errorf("extract Linux executable: %w", err)
	}
	if _, err := archive.Next(); err == nil {
		return errors.New("the Linux archive must contain exactly one entry")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("finish reading Linux archive entries: %w", err)
	}
	padding, err := io.ReadAll(io.LimitReader(body, maxLinuxTarPadding+1))
	if err != nil {
		return fmt.Errorf("finish reading Linux archive: %w", err)
	}
	if body.N == 0 || len(padding) > maxLinuxTarPadding || len(bytes.Trim(padding, "\x00")) != 0 {
		return errors.New("the Linux archive has invalid trailing data")
	}
	if _, err := compressed.Peek(1); err == nil {
		return errors.New("the Linux archive has unexpected compressed data")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("finish reading compressed Linux archive: %w", err)
	}
	return verifyLinuxELF(exe, machine)
}

func verifyLinuxELF(exe io.ReaderAt, machine uint16) error {
	var header [64]byte
	if _, err := exe.ReadAt(header[:], 0); err != nil {
		return fmt.Errorf("read Linux ELF executable: %w", err)
	}
	typ := elf.Type(binary.LittleEndian.Uint16(header[16:18]))
	if string(header[:4]) != elf.ELFMAG || header[4] != byte(elf.ELFCLASS64) ||
		header[5] != byte(elf.ELFDATA2LSB) || header[6] != byte(elf.EV_CURRENT) ||
		(typ != elf.ET_EXEC && typ != elf.ET_DYN) || binary.LittleEndian.Uint16(header[18:20]) != machine {
		return errors.New("the Linux archive does not contain a compatible ELF executable")
	}
	return nil
}

func verifyLinuxVersion(parent context.Context, path, version string) error {
	ctx, cancel := context.WithTimeout(parent, linuxVersionCheckLimit)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.Dir = filepath.Dir(path)
	cmd.Env = []string{}
	out, err := cmd.Output()
	if err != nil {
		if cause := ctx.Err(); cause != nil {
			err = cause
		}
		return fmt.Errorf("check staged Linux executable version: %w", err)
	}
	if strings.TrimSpace(string(out)) != "zwai "+version {
		return errors.New("the staged Linux executable reported a different version")
	}
	return nil
}
