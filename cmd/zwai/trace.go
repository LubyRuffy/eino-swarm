package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// runTrace is the one-id troubleshooting path: copy the turn id out of the UI
// and get the whole run back — every event in order, and every model call with
// its cost in time and characters.
func runTrace(args []string) error {
	fs := flag.NewFlagSet("trace", flag.ExitOnError)
	dataDir := fs.String("data-dir", "", "data directory")
	full := fs.Bool("full", false, "print event text in full instead of one line each")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"data-dir": true})); err != nil {
		return err
	}
	id := strings.TrimSpace(fs.Arg(0))
	if id == "" {
		return errors.New("trace needs a turn id or a conversation id")
	}

	cfg, err := config.Load(*dataDir)
	if err != nil {
		return err
	}
	st, err := store.Open(cfg.DBPath())
	if err != nil {
		return err
	}
	defer st.Close()

	turns, err := turnsFor(st, id)
	if err != nil {
		return err
	}
	if len(turns) == 0 {
		return fmt.Errorf("no turn or conversation with id %q in %s", id, cfg.DataDir())
	}
	for i, turn := range turns {
		if i > 0 {
			fmt.Println()
		}
		if err := printTurn(os.Stdout, st, turn, *full); err != nil {
			return err
		}
	}
	return nil
}

// turnsFor accepts either id, because the one the user has at hand depends on
// where they copied it from: the turn id from the timeline, the conversation
// id from the address bar.
func turnsFor(st *store.Store, id string) ([]store.Turn, error) {
	if turn, err := st.GetTurn(id); err == nil {
		return []store.Turn{*turn}, nil
	} else if !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	turns, err := st.ListTurns(id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	return turns, err
}

func printTurn(w io.Writer, st *store.Store, turn store.Turn, full bool) error {
	fmt.Fprintf(w, "turn %s  conversation %s\n", turn.ID, turn.ThreadID)
	fmt.Fprintf(w, "  status   %s", turn.Status)
	if turn.Error != "" {
		fmt.Fprintf(w, "  (%s)", turn.Error)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  model    %s via %s\n", turn.Model, turn.ProviderID)
	if turn.ReasoningEffort != "" {
		fmt.Fprintf(w, "  thinking %s\n", turn.ReasoningEffort)
	}
	fmt.Fprintf(w, "  started  %s\n", turn.StartedAt.Local().Format(time.RFC3339))
	if turn.DurationMS > 0 {
		fmt.Fprintf(w, "  took     %s\n", (time.Duration(turn.DurationMS) * time.Millisecond).Round(time.Millisecond))
	}
	fmt.Fprintf(w, "  asked    %s\n", oneLine(turn.UserText, full))

	events, err := st.ListTurnEvents(turn.ID)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\n  timeline (%d events)\n", len(events))
	start := turn.StartedAt
	for _, ev := range events {
		offset := ev.CreatedAt.Sub(start).Round(time.Millisecond)
		who := ev.AgentID
		if who == "" {
			who = "-"
		}
		fmt.Fprintf(w, "    %8s  %-14s %-18s %s\n",
			offset, ev.Kind, who, oneLine(eventText(ev), full))
	}

	calls, err := st.ListLLMCalls(turn.ID)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "\n  model calls (%d)\n", len(calls))
	var totalMS int64
	for _, c := range calls {
		totalMS += c.DurationMS
		line := fmt.Sprintf("    %-18s %-22s %5d msgs  in %6d ch  out %6d ch  %6dms",
			c.AgentID, c.Model, c.InputMsgs, c.InputChars, c.OutputChars, c.DurationMS)
		if c.Err != "" {
			line += "  ERROR: " + c.Err
		}
		fmt.Fprintln(w, line)
	}
	if len(calls) > 0 {
		fmt.Fprintf(w, "    %-18s %s\n", "total",
			(time.Duration(totalMS) * time.Millisecond).Round(time.Millisecond))
	}
	return nil
}

func eventText(ev store.Event) string {
	if ev.Err != "" {
		return "error: " + ev.Err
	}
	return ev.Text
}

// oneLine keeps the timeline scannable: a wall of streamed markdown hides the
// one line that explains the failure.
func oneLine(s string, full bool) string {
	s = strings.TrimSpace(s)
	if full {
		return s
	}
	s = strings.ReplaceAll(s, "\n", " ⏎ ")
	// Cut by runes: slicing bytes turns the last CJK character of a truncated
	// line into a replacement glyph.
	r := []rune(s)
	if len(r) > 110 {
		return string(r[:109]) + "…"
	}
	return s
}
