// Command workercontext_eval is an opt-in Registry/ADK measurement adapter.
// It never starts the desktop/engine or runs the workers' historical tasks.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"gopkg.in/yaml.v3"
)

type options struct {
	config, provider, model, fixture, out, arm string
	maxCompletion, maxIterations               int
	timeoutSeconds                             float64
}

func parseOptions(arguments []string, stderr io.Writer) (options, error) {
	var o options
	flags := flag.NewFlagSet("workercontext_eval", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&o.config, "config", "", "private one-provider YAML, read only")
	flags.StringVar(&o.provider, "provider", "", "configured provider id")
	flags.StringVar(&o.model, "model", "", "optional model override on that provider")
	flags.StringVar(&o.fixture, "fixture", "", "versioned fixed-worker fixture JSON")
	flags.StringVar(&o.out, "out", "", "new private evidence directory")
	flags.StringVar(&o.arm, "arm", "", "a=full native wait, b=same-request exact duplicate reference")
	flags.IntVar(&o.maxCompletion, "max-completion-tokens", 0, "per-call cap; zero retains configured/default cap")
	flags.IntVar(&o.maxIterations, "max-iterations", 24, "ADK iterations per human round")
	flags.Float64Var(&o.timeoutSeconds, "timeout-seconds", 240, "deadline per human round, in seconds")
	if err := flags.Parse(arguments); err != nil {
		return o, err
	}
	if o.config == "" || o.provider == "" || o.fixture == "" || o.out == "" || (o.arm != "a" && o.arm != "b") ||
		o.maxCompletion < 0 || o.maxIterations <= 0 || o.timeoutSeconds <= 0 || len(flags.Args()) != 0 {
		return o, errors.New("required paths/provider/arm or run budgets are invalid")
	}
	return o, nil
}

func readConfig(o options) (*config.Config, string, error) {
	data, err := os.ReadFile(o.config)
	if err != nil {
		return nil, "", err
	}
	cfg := config.Default()
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, "", err
	}
	selected, ok := cfg.Provider(o.provider)
	if !ok {
		return nil, "", errors.New("selected provider is absent")
	}
	if o.model != "" {
		selected.Model = o.model
	}
	cfg.Models.Default, cfg.Models.Providers = selected.ID, []config.Provider{selected}
	if o.maxCompletion > 0 {
		cfg.Swarm.MaxCompletionTokens = o.maxCompletion
	}
	return cfg, selected.APIKey, nil
}

func saveArtifact(out, name string, value any, secret string) error {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	if len(secret) >= 4 {
		body = []byte(strings.ReplaceAll(string(body), secret, "<redacted>"))
	}
	return privateFile(filepath.Join(out, name), append(body, '\n'))
}

func runCLI(ctx context.Context, arguments []string, stdout, stderr io.Writer) int {
	o, err := parseOptions(arguments, stderr)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	f, err := loadFixture(o.fixture)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	cfg, secret, err := readConfig(o)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	if err := os.Mkdir(o.out, 0o700); err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	log := &audit{}
	result := sessionResult{Arm: o.arm, ScopeViolations: []string{}, Rounds: []roundResult{}, Calls: []callUsage{}}
	var transcript any
	builder, runErr := provider.New(cfg).ModelBuilder(ctx, o.provider, o.model, "", log.recordCall)
	if runErr == nil {
		result, transcript, runErr = executeSession(ctx, f, o.arm,
			builder("manager", "manager"), log, o.maxIterations, time.Duration(o.timeoutSeconds*float64(time.Second)))
	}
	if runErr != nil {
		result.Error = runErr.Error()
	}
	finalAudit := log.snapshot()
	artifacts := []struct {
		name  string
		value any
	}{
		{"actual-model-inputs.json", finalAudit.Inputs}, {"notifications.json", finalAudit.Receipts},
		{"transcript.json", transcript}, {"result.json", result},
	}
	for _, artifact := range artifacts {
		if err := saveArtifact(o.out, artifact.name, artifact.value, secret); err != nil {
			fmt.Fprintln(stderr, "evidence save failed:", err)
			return 2
		}
	}
	fmt.Fprintln(stdout, filepath.Join(o.out, "result.json"))
	if result.Error != "" || len(result.ScopeViolations) > 0 || !result.Cleanup.RegistryClosed || result.Cleanup.LiveWorkers != 0 {
		return 2
	}
	return 0
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	os.Exit(runCLI(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
