# Phone binds multiple PCs

> Product decisions are locked. This is the implementation design.

**Goal:** One phone holds many PC tickets. One WebSocket at a time. Inbox chips switch host. The PC names itself.

## Locked

- List of tickets, keyed by pairlink fingerprint. Re-scan of the same `hostPub` replaces the ticket.
- One live `DeviceLink`. Chip tap closes the old socket, connects the new one, resumes that PC's last/live thread.
- No `All` chip. Unselected hosts do not fake an online dot.
- PC name is `remote.display_name` (blank seeds `os.Hostname()`), editable in Settings → Phone. `hello` / `list` replies carry `host`. Phone caches it as `SavedLink.label`. Missing `host` → short fingerprint, never the hub hostname.
- Inbox title stays `zwai`. Chips sit under it (Codex remote, minus All). `…` / menu: add PC, unlink current, language. Unlink of the last PC returns to scan.
- Legacy `zwai.remote.link` migrates into `zwai.remote.links` on first read.

## Out of scope

Simultaneous sockets, merged inbox, name in the QR, a `whoami` op.

## Failure

Switch fail: stay on the chosen chip, reconnect that ticket, do not silently roll back. Add fail: list unchanged, sheet stays open. Rename lands on the next `hello`/`list`.
