package update

import (
	"archive/zip"
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
