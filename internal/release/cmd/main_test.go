package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPublisherFlagsFailBeforeAnyUpload(t *testing.T) {
	for _, args := range [][]string{{"-unknown"}, {"-version", "dirty", "-check"}, {"-version", "1.2.3", "-dir", t.TempDir(), "-platform", "android", "-target", "source"}} {
		if err := publish(args); err == nil {
			t.Fatal(args)
		}
	}
}

func TestPublisherPreflightWithGH(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte("#!/bin/sh\nexit 99\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	if err := publish([]string{"-version", "1.2.3", "-check"}); err != nil {
		t.Fatal(err)
	}
}
