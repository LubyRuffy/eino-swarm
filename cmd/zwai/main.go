// Command zwai runs a swarm of agents that work together on a task, in
// whichever shell you prefer:
//
//	zwai desktop   native window (the default)
//	zwai web       the same UI in your browser
//	zwai tui       terminal UI: composer, --task one-shot, --goal starts immediately
//	zwai trace     print everything that happened in one turn
//	zwai config    show or create the configuration file
//
// Every shell runs the same engine over the same data directory, so a
// conversation started in one is there in the others.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// version is stamped at build time:
//
//	go build -ldflags "-X main.version=$(git describe --tags)" ./cmd/zwai
var version = "dev"

func main() {
	os.Exit(dispatch(os.Args[1:], os.Stdout, os.Stderr))
}

// dispatch runs one command and returns the process exit code. main is a
// one-liner around it so the command table can be tested without a subprocess.
func dispatch(args []string, stdout, stderr io.Writer) int {
	args = withDefaultCommand(args)

	var err error
	switch args[0] {
	case "desktop":
		err = runDesktop(args[1:])
	case "web":
		err = runWeb(args[1:])
	case "tui":
		err = runTUI(args[1:])
	case "trace":
		err = runTrace(args[1:])
	case "config":
		err = runConfig(args[1:])
	case "version", "--version", "-v":
		fmt.Fprintln(stdout, "zwai", version)
	case "help", "--help", "-h":
		usage(stdout)
	default:
		fmt.Fprintf(stderr, "zwai: unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
	if err != nil {
		fmt.Fprintln(stderr, "zwai:", err)
		var ue usageError
		if errors.As(err, &ue) {
			return 2
		}
		return 1
	}
	return 0
}

// withDefaultCommand fills in the subcommand people leave out. Opening the app
// is the obvious thing, and requiring `desktop` in front of it is a papercut on
// a desktop shortcut — including one that carries --data-dir or --mock.
func withDefaultCommand(args []string) []string {
	if len(args) == 0 {
		return []string{"desktop"}
	}
	if strings.HasPrefix(args[0], "-") && !isGlobalFlag(args[0]) {
		return append([]string{"desktop"}, args...)
	}
	return args
}

// isGlobalFlag reports whether a leading flag answers a question about the
// program itself, rather than configuring the command that is about to run.
func isGlobalFlag(arg string) bool {
	switch arg {
	case "--version", "-v", "--help", "-h":
		return true
	}
	return false
}

// usageError is a command invoked wrongly rather than a command that failed.
// Shells and scripts read the difference off the exit code, so keep it: 2 for
// "you typed it wrong", 1 for "it did not work".
type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

// reorderFlags moves positional arguments behind the flags.
//
// Go's flag package stops parsing at the first non-flag argument, so
// `zwai trace <id> --data-dir X` would silently ignore the flag and read the
// wrong database. People write the id first because it is the subject of the
// command, so accept it.
func reorderFlags(args []string, takesValue map[string]bool) []string {
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(arg, "-") || arg == "-" {
			positional = append(positional, arg)
			continue
		}
		flags = append(flags, arg)
		name := strings.TrimLeft(arg, "-")
		if strings.Contains(name, "=") || !takesValue[name] {
			continue
		}
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return append(flags, positional...)
}

func usage(w io.Writer) {
	fmt.Fprintln(w, strings.TrimSpace(`
zwai — a swarm of agents that work together on your tasks

usage:
  zwai [desktop] [--data-dir DIR] [--mock]
        open the app in a native window

  zwai web [--addr HOST:PORT] [--no-open] [--data-dir DIR] [--mock]
        serve the same app in your browser

  zwai tui [--task "..."] [--goal "..."] [--model NAME] [--reasoning LEVEL] [--workspace DIR] [--data-dir DIR] [--mock]
        terminal swarm: omit --task for a composer; --goal starts that objective immediately

  zwai trace <turn-id|conversation-id> [--data-dir DIR] [--full]
        print a turn's timeline and every model call it made

  zwai config [path|init|show] [--data-dir DIR]
        where the configuration lives, and what is in it (default: show)

  zwai version | help
        also -v / --version and -h / --help

common flags:
  --data-dir DIR   use another data directory (default: $ZWAI_HOME or ~/.zwai-swarm)
  --mock           run on the scripted offline provider; no model is called
`))
}
