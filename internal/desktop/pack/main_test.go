//go:build darwin

package main

import (
	"strings"
	"testing"
)

func TestRunRejectsAVersionThatIsNotARelease(t *testing.T) {
	if err := run("dev", t.TempDir(), "arm64"); err == nil || !strings.Contains(err.Error(), "numeric") {
		t.Fatal(err)
	}
	if err := run("1.2.3-dirty", t.TempDir(), "arm64"); err == nil {
		t.Fatal("a dirty describe is not a release")
	}
	if err := run("1.2.3", t.TempDir(), "386"); err == nil || !strings.Contains(err.Error(), "arch") {
		t.Fatal(err)
	}
}

func TestModuleRootFindsThisCheckout(t *testing.T) {
	root, err := moduleRoot()
	if err != nil || root == "" {
		t.Fatal(err)
	}
}
