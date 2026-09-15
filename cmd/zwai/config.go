package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"gopkg.in/yaml.v3"
)

// runConfig answers the two questions people actually ask: where is the
// config, and what does it currently say.
func runConfig(args []string) error {
	sub := "show"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	dataDir := fs.String("data-dir", "", "data directory")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"data-dir": true})); err != nil {
		return err
	}
	if sub == "show" && fs.NArg() > 0 {
		sub = fs.Arg(0)
	}

	// Load writes a default file when there is none, so `config init` is just
	// a load with a friendlier message.
	cfg, err := config.Load(*dataDir)
	if err != nil {
		return err
	}

	switch sub {
	case "path":
		fmt.Println(cfg.Path())
	case "init":
		fmt.Println("configuration ready at", cfg.Path())
		fmt.Println("data directory:      ", cfg.DataDir())
		if !cfg.Configured() {
			fmt.Println("\nNo model is configured yet. Edit the file above, or open the app")
			fmt.Println("and fill in Settings → Models.")
		}
	case "show":
		raw, err := yaml.Marshal(cfg)
		if err != nil {
			return err
		}
		fmt.Printf("# %s\n%s", cfg.Path(), redactKeys(string(raw)))
	default:
		fmt.Fprintf(os.Stderr, "zwai config: unknown subcommand %q\n", sub)
		fmt.Fprintln(os.Stderr, "usage: zwai config [path|init|show]")
		os.Exit(2)
	}
	return nil
}

// redactKeys keeps `zwai config show` safe to paste into a bug report.
func redactKeys(raw string) string {
	lines := strings.Split(raw, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "api_key:") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "api_key:"))
		if value == "" || value == `""` {
			continue
		}
		indent := line[:len(line)-len(trimmed)]
		lines[i] = indent + "api_key: <set, hidden>"
	}
	return strings.Join(lines, "\n")
}
