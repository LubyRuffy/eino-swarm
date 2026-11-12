package update

import (
	"archive/zip"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// ZipApp writes a release zip whose root is zwai.app.
func ZipApp(appDir, dest string) error {
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	err = filepath.Walk(appDir, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(filepath.Dir(appDir), path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if info.IsDir() {
			_, err := zw.Create(name + "/")
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = name
		hdr.Method = zip.Deflate
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(w, in)
		closeErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	closeZip := zw.Close()
	closeOut := out.Close()
	if err != nil {
		return err
	}
	if closeZip != nil {
		return closeZip
	}
	return closeOut
}

// ZipWindowsExe writes a release zip whose only entry is zwai.exe.
func ZipWindowsExe(exePath, dest string) error {
	info, err := os.Stat(exePath)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return errors.New("windows executable is a directory")
	}
	in, err := os.Open(exePath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(out)
	hdr, err := zip.FileInfoHeader(info)
	if err != nil {
		_ = zw.Close()
		_ = out.Close()
		return err
	}
	hdr.Name = "zwai.exe"
	hdr.Method = zip.Deflate
	writer, err := zw.CreateHeader(hdr)
	if err != nil {
		_ = zw.Close()
		_ = out.Close()
		return err
	}
	_, copyErr := io.Copy(writer, in)
	closeZip := zw.Close()
	closeOut := out.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeZip != nil {
		return closeZip
	}
	return closeOut
}
