// Package release publishes one GitHub Release that carries both the Mac
// desktop zip and the Android sideload APK. Either file missing is a
// failed release, not a partial success.
package release

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/update"
)

// AndroidAPKName is the sideload package the phone installer looks for.
func AndroidAPKName(version string) string {
	return "zwai-" + update.Canonical(version) + "-android.apk"
}

// Options is one publish. Dir is where the zip and apk already sit.
type Options struct {
	Version string
	Dir     string
	// Run executes a command. Nil uses exec.Command.
	Run func(name string, args ...string) (string, error)
	// LookPath finds gh. Nil uses exec.LookPath.
	LookPath func(file string) (string, error)
}

// Preflight rejects a version the two installers cannot share, and a
// machine with no gh. It does not build or upload.
func Preflight(opts Options) error {
	if update.Canonical(opts.Version) == "" {
		return fmt.Errorf("release version must be major.minor.patch, got %q", opts.Version)
	}
	look := opts.LookPath
	if look == nil {
		look = exec.LookPath
	}
	if _, err := look("gh"); err != nil {
		return errors.New("gh is not on PATH; log in with gh auth login")
	}
	return nil
}

// Assets is the apk plus every darwin zip for this version in dir.
// The apk and at least one zip are required.
func Assets(dir, version string) ([]string, error) {
	name := update.Canonical(version)
	if name == "" {
		return nil, fmt.Errorf("release version must be major.minor.patch, got %q", version)
	}
	apk := filepath.Join(dir, AndroidAPKName(name))
	if _, err := os.Stat(apk); err != nil {
		return nil, fmt.Errorf("android apk missing: %s", apk)
	}
	matches, err := filepath.Glob(filepath.Join(dir, "zwai-"+name+"-darwin-*.zip"))
	if err != nil {
		return nil, err
	}
	var zips []string
	for _, match := range matches {
		base := filepath.Base(match)
		if base != update.DarwinAssetName(name, "arm64") && base != update.DarwinAssetName(name, "amd64") {
			continue
		}
		zips = append(zips, match)
	}
	if len(zips) == 0 {
		return nil, fmt.Errorf("macOS zip missing: %s", filepath.Join(dir, "zwai-"+name+"-darwin-*.zip"))
	}
	return append([]string{apk}, zips...), nil
}

// Publish uploads the apk and the darwin zip(s) onto tag vX.Y.Z.
// A missing release is created. An existing one is updated in place.
// Other releases are left alone.
func Publish(opts Options) error {
	if err := Preflight(opts); err != nil {
		return err
	}
	files, err := Assets(opts.Dir, opts.Version)
	if err != nil {
		return err
	}
	tag := "v" + update.Canonical(opts.Version)
	run := opts.Run
	if run == nil {
		run = runCommand
	}
	exists, err := releaseExists(run, tag)
	if err != nil {
		return err
	}
	if exists {
		args := append([]string{"release", "upload", tag, "--clobber"}, files...)
		if _, err := run("gh", args...); err != nil {
			return fmt.Errorf("upload %s: %w", tag, err)
		}
	} else {
		args := append([]string{"release", "create", tag, "--title", tag, "--notes", "zwai " + update.Canonical(opts.Version)}, files...)
		if _, err := run("gh", args...); err != nil {
			return fmt.Errorf("create %s: %w", tag, err)
		}
	}
	return verify(run, tag, files)
}

func releaseExists(run func(string, ...string) (string, error), tag string) (bool, error) {
	out, err := run("gh", "release", "view", tag, "--json", "tagName")
	if err == nil {
		return true, nil
	}
	text := out + "\n" + err.Error()
	if strings.Contains(strings.ToLower(text), "not found") {
		return false, nil
	}
	return false, fmt.Errorf("view %s: %w", tag, err)
}

func verify(run func(string, ...string) (string, error), tag string, files []string) error {
	out, err := run("gh", "release", "view", tag, "--json", "assets,tagName")
	if err != nil {
		return fmt.Errorf("verify %s: %w", tag, err)
	}
	var body struct {
		TagName string `json:"tagName"`
		Assets  []struct {
			Name string `json:"name"`
		} `json:"assets"`
	}
	if err := json.Unmarshal([]byte(out), &body); err != nil {
		return fmt.Errorf("verify %s: %w", tag, err)
	}
	if body.TagName != tag {
		return fmt.Errorf("verify %s: tag is %q", tag, body.TagName)
	}
	have := map[string]bool{}
	for _, asset := range body.Assets {
		have[asset.Name] = true
	}
	for _, file := range files {
		name := filepath.Base(file)
		if !have[name] {
			return fmt.Errorf("verify %s: missing %s", tag, name)
		}
	}
	return nil
}

func runCommand(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	return buf.String(), err
}
