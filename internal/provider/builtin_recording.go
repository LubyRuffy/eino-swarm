package provider

import (
	"context"
	"sync"
)

// One collector belongs to one request, even when agents share the model.
type builtinToolRecords struct {
	mu     sync.Mutex
	events []BuiltinToolEvent
}

func observeBuiltinTools(ctx context.Context, enabled bool) (context.Context, *builtinToolRecords) {
	if !enabled {
		return ctx, nil
	}
	collected := new(builtinToolRecords)
	return WithBuiltinToolObserver(ctx, func(ev BuiltinToolEvent) {
		collected.mu.Lock()
		defer collected.mu.Unlock()
		collected.events = append(collected.events, ev)
	}), collected
}

func (c *builtinToolRecords) snapshot() []BuiltinToolEvent {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]BuiltinToolEvent(nil), c.events...)
}
