# Work graph

One graph kernel replaces the work-record ledger, the project knowledge base and
the wiki builder. While it works, the model keeps a graph of what the work is
about (**subjects** and **links**) and an ordered list of **to-dos**, through two
tools. The host supplies the method and the storage; the task itself, and every
to-do, is whatever the model decides. The panel is read-only.

Code: `internal/graph` (catalog, normalizers, request validation),
`internal/store/graph_*.go` (storage, merge, revisions), `internal/engine/graph_*.go`
(tools, digest, prompt), `internal/server/graph.go`, `internal/remote/graph.go`,
`frontend/src/components/app/graph` and `mobile/src/components/work-records.tsx`
(panels). The write protocol is model-checked in `models/graph/GraphStore.tla`.

## Owner

A graph belongs to the **project** when the conversation is in one, otherwise to
the **conversation**. The owner is frozen when a turn starts, so moving a
conversation into a project mid-turn cannot split its record. Every conversation
of a project shares the project's graph; the panel and the tools show the same
one. Deleting the owner deletes its graph. Old `nuwa_nodes`, work-record and
knowledge-base data are not imported: they stay on disk, are not shown, and are
removed with their conversation or project. Backups still carry the legacy tables, and leave a conversation's legacy rows out when that conversation is left out.

## Subjects

A subject is a thing the work is about: an address, a domain, a company, a
repository, a person, a query. It has a `kind`, `attrs`, a `status` and a
`version`.

| status | meaning |
|---|---|
| `lead` | recorded, not yet shown to be right (the default) |
| `confirmed` | shown to be right; the write must carry evidence |
| `retracted` | shown to be wrong; kept, greyed, so nobody repeats the dead end |

**Identity.** Each kind declares one or more identity keys (a key may combine
several attributes), each with a normalizer. Writing a subject whose normalized
key matches an existing one merges into it; a write that matches two different
subjects is refused, and the model decides. An identity key's value never
changes: a different value is a different subject, written separately and
linked to the first. A revised FOFA query is therefore a new `fofa_query`
subject linked to its draft, and the failed draft is marked `retracted`.
A lead is still current. When one call links a lead to a confirmed subject of
the same kind, the host stores that lead as retracted; two leads, or two
confirmed subjects, stay as the call wrote them.

**Merge.** Without an id a write merges additively: new attributes are added and
existing ones are kept. Overwriting an attribute, or changing status, needs the
subject's `id` and current `version`; a stale version is a conflict the model
repairs by reading and retrying. Replaying a tool call id with a different body
is refused; the same body returns the original receipt.

**Mechanical repair.** The host repairs what only costs the model a resend: an id
that is not host-minted (`sj_` / `td_` prefix) or repeats the ref is a new
subject or to-do, `null` and undeclared attributes are omitted, an empty
`own_tool_call_ids` means "cite my own results", and a named call that does not
resolve to this caller's completed call falls back to the same citation. Literal
`evidence` wins over named call ids. Wrong-type values and unknown kinds still
refuse.

**Credentials.** A kind may mark an attribute `secret`. Its value is stored and
returned only to the model's by-id `read_graph`. The digest, HTTP, phone, panel,
logs and Trace show the attribute's name, never its value.

## Kinds

The catalog is `frontend/src/graph/kinds.json`, shared by Go, desktop and phone.
Built-in kinds: `ip`, `domain`, `url`, `asset`, `app`, `cert`, `company`, `icp`,
`person`, `address`, `vuln`, `repo`, `fofa_query`, and a generic `other`. The
kind list appears only in the `update_graph` tool definition, never in the
prompt.

An entry declares: `id`, localized `name` and `description`, `attrs` (name, type,
localized label, optional `max` and `secret`), `keys` (name, normalizer, the
attributes it combines) and `display` (the attributes that make the subject's
name). Normalizers are in `internal/graph/normalize.go`: `ip`, `domain`, `url`,
`host`, `repo`, `repo_path`, `query`, `email`, `identifier`, `text`.

**To add a kind**, add an entry to `kinds.json` and, if it needs a normalizer
that does not exist, add it to `normalize.go` with a test. The panel colours a
kind by its catalog slot (`--easm-*` tokens in `index.css`). `GET /api/graph/kinds`
serves the catalog; `internal/graph/crosscheck_test.go` checks the Go and
front-end copies agree.

## Links

A link records how one subject led to another: `from`, `to`, a short `rel`, and
optional attributes. Both ends are subject ids, or refs to subjects written in
the same batch. Links are unique per `(from, to, rel)`; repeating one merges.

## To-dos

An ordered list, not graph nodes. Statuses are `open`, `doing`, `done`, `failed`.
Position is priority: a write places an item with `before`, naming the to-do it
goes in front of. `done` needs evidence and `failed` needs a `result`. A to-do
may be `about` one subject. Subjects and links written in the same call that
settles a to-do are credited to it (`found_by_todo`), which is how the panel
highlights what a to-do touched.

**Evidence** is either `evidence` (an event of this conversation by
`thread_id` and `seq`, or an `http(s)` URL) or `own_tool_call_ids` (ids of the
model's own completed tool calls); the two are mutually exclusive in one batch,
and the host drops the ids when both arrive. The model-facing tool advertises only
the URL form of `evidence`; event citations by `thread_id` and `seq` stay accepted
over HTTP and from older callers.
The host validates that evidence is well-formed and that cited own tool calls
completed, never what it means. A batch that carries no well-formed evidence
and no `own_tool_call_ids` is cited to the caller's own completed tool results
from this turn, excluding graph-tool traffic, at most 16. Ill-formed evidence
is dropped and that citation is used instead. Evidence whose event later
disappears (after a rewind, for example) reads as unavailable instead of being
trusted.

A subject's analysis state is not stored. It is derived in the front end from
its to-dos: no to-do → unanalysed (dashed ring), open or doing but not running
→ pending (dashed), a `doing` to-do while the turn runs → flowing dashed,
all settled → solid, `retracted` → greyed. A new to-do about a settled subject
reopens its ring without anyone writing a flag. With reduced motion the flowing
ring is a static dashed one.

## Tools

`update_graph` writes one batch of `subjects`, `links` and `todos`, with
`evidence` or `own_tool_call_ids`. It is atomic: a refused batch writes nothing.
Refusals (`graph: invalid request`, version conflict, ambiguous match) come back
as tool results the model can repair, not as turn failures; storage errors are
failures.

`read_graph` reads a page of subjects (`text`, `offset`, `limit`), the to-do list
(`todos`, `status`), or one subject in full by `id` with its links, the subjects
they reach and the to-dos about it. Planning turns get only `read_graph`.

The model's guidance is one task-agnostic block (`internal/engine/graph_prompt.go`):
record the subjects, links and settled to-dos in one call rather than one
subject per call. Each distinct
thing is its own subject; a count, a range or one label for the set is not a
substitute. A new id that only repeats the ref is dropped, a `ref:` prefix on
a link end is removed, and an attribute the kind does not declare is omitted
and named in the receipt instead of refusing the batch. A ref used earlier in
this process still denotes that subject. The tool keeps the first newly finished to-do as doing when nothing
is already in progress, leaves that frame visible briefly, then marks it done
in the same call. The model is not asked to spend a call on that flip. A later
call that repeats the title of a to-do still in progress settles that to-do.
Confirm only
with evidence. An identity value that turned out wrong is a new subject linked
to the old one, and the old one is retracted, so one current subject remains.
A lead linked in that call to a confirmed subject of the same kind is stored
retracted. The guidance also says not to hand the same check to a worker, and
to stop when no to-do is open or doing. The kind list on the tool names
attributes and identity fields. A successful receipt returns the subject ids
and a link count, not every link.
Cite tool calls instead of copying results, and do not read the graph back
after a write that succeeded. It names no kind and no task.

## Digest

Every model request of a turn (manager and workers) carries a bounded digest as
a system message: the revision, counts by kind and status, the current counts
with retracted subjects left out, up to 12 open to-dos,
up to 12 latest subjects, and how many tool results this turn have not been
recorded since the last accepted `update_graph`. It replaces the previous
digest instead of stacking. It is a record to check work against, not
instruction. The turn is never blocked at its end for an incomplete graph; the
unrecorded count is the only pressure, and it appears in Trace.

## Surfaces

- **HTTP** — see [API.md](API.md): `GET /api/threads/:id/graph`,
  `GET /api/projects/:id/graph`, `GET /api/graph/kinds`, and the `graph_updated`
  SSE frame on a thread's `events` stream with `graph=1`.
- **Phone** — encrypted RPC `graph` with the revision the phone holds; an
  unchanged revision returns one small frame. A graph larger than one frame is
  refused with `graph_too_large`. See [mobile/README.md](../mobile/README.md).
- **Panel** — the 工作记录 / Work records tab: the graph on top (the EASM graph
  components, generalized), the ordered to-dos below. Clicking a to-do
  highlights its subject and the batch that settled it; clicking a subject
  filters the list to its to-dos. Read-only.
- **Trace** — `update_graph` and `read_graph` are ordinary tool events in the
  turn's timeline; `GET /api/trace/:turn` adds `graph_unrecorded` when the turn
  had the graph on.

## Settings

`swarm.graph` (default on) mounts the tools, digest and panel. A turn freezes the
switch when it starts; turning it off stops writes at once. The older
`work_ledger` and `knowledge` keys are ignored. See [CONFIG.md](CONFIG.md).

## Limits

A page or snapshot returns at most 2000 rows and reports `more`. A to-do title is
at most 300 characters and a result 4000; a link relation 64; a link has at most
16 attributes of 2048 bytes each; a batch cites at most 32 evidence items or 16
own tool calls. Attribute `max` values are in the catalog.

## Tests

- `internal/graph`: catalog, normalizers, attribute and batch validation.
- `internal/store`: merge, identity, replay, conflicts, revisions, deletion,
  concurrent writers, on real SQLite.
- `internal/engine`: tools through the real Registry/HTTP adapter, digest, and
  the offline acceptance runs `TestAcceptance*` (exposure analysis; query
  refinement with a failed draft) that observe the progress timeline.
- `frontend` (vitest, Playwright `graph-panel.spec.ts`) and `mobile`
  (vitest, `e2e/work-records.spec.ts`).
- Evals: tasks `graph-exposure-analysis` and `graph-fofa-query` with the
  `graph_count` check; see [TESTING.md](TESTING.md#graph-evals).
- `make model-check`: `models/graph/GraphStore.tla`.
