//go:build darwin

package desktop

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestImmutableBundlePreparationChild(t *testing.T) {
	source := os.Getenv("ZWAI_TEST_BUNDLE_SOURCE")
	if source == "" {
		return
	}
	path, err := prepareBundle(source, os.Getenv("ZWAI_TEST_BUNDLE_CACHE"))
	if err != nil {
		os.Exit(3)
	}
	if err := os.WriteFile(os.Getenv("ZWAI_TEST_BUNDLE_RESULT"), []byte(path), 0600); err != nil {
		os.Exit(4)
	}
	os.Exit(0)
}

func TestSeparateProcessesPublishOneImmutableBundleForTheSameSource(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "source")
	cache := filepath.Join(dir, "cache")
	if err := os.WriteFile(source, []byte("identical source"), 0755); err != nil {
		t.Fatal(err)
	}
	commands := make([]*exec.Cmd, 2)
	results := make([]string, 2)
	for i := range commands {
		results[i] = filepath.Join(t.TempDir(), "result")
		commands[i] = exec.Command(os.Args[0], "-test.run=^TestImmutableBundlePreparationChild$")
		commands[i].Env = append(os.Environ(), "ZWAI_TEST_BUNDLE_SOURCE="+source, "ZWAI_TEST_BUNDLE_CACHE="+cache, "ZWAI_TEST_BUNDLE_RESULT="+results[i])
		if err := commands[i].Start(); err != nil {
			t.Fatal(err)
		}
	}
	for _, command := range commands {
		if err := command.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	first, err := os.ReadFile(results[0])
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(results[1])
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("same-source processes published different bundles")
	}
	if contents, err := os.ReadFile(string(first)); err != nil || string(contents) != "identical source" {
		t.Fatal("competing preparation corrupted the executable")
	}
}
