package engine

import "github.com/LubyRuffy/eino-swarm/internal/store"

// reviewHumanEvents excludes generated continuations, peer messages and
// tool data from the requirements-only path after an operational error.
func reviewHumanEvents(events []store.Event) []store.Event {
	retracted := retractedSteerSeqs(events)
	revised := revisedSteerText(events)
	var out []store.Event
	for _, ev := range events {
		if ev.Kind != KindUser && ev.Kind != KindSteer {
			continue
		}
		if ev.Kind == KindSteer {
			if _, ok := retracted[ev.Seq]; ok {
				continue
			}
			if text, ok := revised[ev.Seq]; ok {
				ev.Text = text
			}
		}
		if role, _, ok := reviewEventLine(ev); ok && role == "human" {
			out = append(out, ev)
		}
	}
	return out
}
