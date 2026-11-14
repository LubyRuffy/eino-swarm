//go:build server

package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestServerBuildDefaultsToWebAndRefusesNativeWindow(t *testing.T) {
	for _, args := range [][]string{nil, {"--mock"}} {
		if got := withDefaultCommand(args); got[0] != "web" {
			t.Fatalf("%v", got)
		}
	}
	var help bytes.Buffer
	if dispatch([]string{"help"}, &help, &help) != 0 || !strings.Contains(help.String(), "zwai [web]") {
		t.Fatalf("server help: %s", help.String())
	}
	for _, command := range []string{"desktop", "desktop-attach"} {
		var out, err bytes.Buffer
		if dispatch([]string{command}, &out, &err) != 1 || !strings.Contains(err.String(), "server build") {
			t.Fatalf("%s: %s", command, err.String())
		}
	}
}
