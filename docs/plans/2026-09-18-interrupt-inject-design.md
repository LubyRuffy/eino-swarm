# Same-turn interrupt injection

> Product decisions are locked. This is the implementation design.

**Goal:** Unread steering can abort the current manager tool (or in-flight generate) and land in the **same** turn, without killing sub-agents and without cancelling the turn.

## Modes

| Action | Landing |
|---|---|
| queue (Enter) | `followups` → next turn after a clean finish |
| steer (⌘Enter) | `mgrInbox` → next `BeforeModel` after the current tool/generate finishes |
| Interrupt (unread steer pin) | cancel epoch ctx → synthetic tool result → drain **all** unread steers |
| Delete (one unread steer) | drop that inbox item + `steer_retracted`; model never sees it |
| Stop (Esc) | cancel **turn** ctx → `cancelled` |

Interrupt is not a third draft. The text is already a `steer` event.

## Context

```
turnCtx          ← Stop
  └── epochCtx   ← Interrupt (one per in-flight manager tool/generate)
        └── Generate / manager tool
```

Workers stay on their own contexts. `wait_agents` abort ≠ `Handle.Cancel`.

Middleware converts epoch cancel into `InterruptedToolResult` (`err = nil`) so ToolsNode continues. If Generate dies the graph, `runManager` re-enters like max-iterations, stitching orphaned tool results and pending steers.

Events stay append-only. Retract never DELETE-s a `steer` row.
