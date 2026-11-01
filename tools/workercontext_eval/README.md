# Fixed-worker context-reference measurement helper

This opt-in helper runs the actual `Registry.PlantFinished` → `Registry.Tools`
and `ManagerConfig` → Eino ADK → configured provider path. It never repeats the
workers' original operations. It does not start an HTTP engine, scheduler,
desktop, native test app or background worker.

Arm A forwards complete native `wait_agents` results. They are **not** subject
to `internal/tools/resultcap.go`: that cap wraps workspace tools, while the
Registry lifecycle tools are wired directly. The notification clipping in
`signal.go` is not a model-input cap. This helper exposes no workspace tools.

Arm B scans the complete outgoing messages afresh at every actual model
Generate/Stream boundary. Only a later `done` result with the same worker ID,
positive generation, role and exact result/error hash can replace its `result`
with a `result_ref` pointing to an earlier full entry **still present in that
same request**. The first result remains complete. Status, errors, undelivered
messages and other report fields remain. No Registry result, collection state,
ADK conversation or durable manager history is rewritten; there is no reference
cache between requests and no external result resource.

Both arms have the same human messages, manager instruction, budgets and tool
catalog. Their common experimental wait-tool description explains the reference
format; it is not the untouched production prompt. Only B performs the input
projection. Failed/unknown/running workers, missing generation, altered result,
existing reference chains, malformed/truncated JSON, duplicate JSON keys and
ambiguous tool-call identities remain complete. No model may spawn/resume a
worker in this prototype. The four other Registry lifecycle tools receive a
refusal, remain recorded as scope violations, and invalidate the session. No
read/write/exec tool is registered in either arm, and no workspace is created.
This common minimal catalog is an experimental boundary, not the complete
production toolset; a passing screen still needs the full-toolset engine trial.

```bash
go build -o /absolute/private/workercontext-eval ./tools/workercontext_eval
/absolute/private/workercontext-eval \
  --config /absolute/private/provider.yaml --provider <provider-id> \
  --fixture /absolute/private/case.json --out /absolute/private/new-evidence \
  --arm a --max-completion-tokens 0 --max-iterations 20 --timeout-seconds 240
go test -race -cover ./tools/workercontext_eval
```

The config is read without writing, default discovery or environment seeding.
Only the selected provider is used; `--model` is an optional override. All paths
come from the CLI. `--out` must not exist. The timeout applies independently to
each human round. The parent orchestrator supplies private HOME/XDG/ZDOTDIR and
cleans its temporary credential directory; this helper does not claim system
confinement.

Fixture schema:

```json
{
  "schema_version": 1,
  "instruction": "A common manager instruction.",
  "messages": [],
  "rounds": [{"user": "First task."}, {"user": "Follow-up task."}],
  "workers": [{
    "agent_id": "fixture-worker-id",
    "role": "fixture-role",
    "generation": 1,
    "status": "done",
    "result": "Full frozen worker result.",
    "error": ""
  }]
}
```

`messages` accepts initial user/assistant messages. Alternatively it can contain
the whole first task when `rounds` is omitted. Workers accept exactly one of
inline `result` or `result_file` (relative to the fixture file, or an explicitly
selected absolute path). `failed` workers require a nonempty error; `done`
workers require an empty error. Positive generations are identities of this new
fixture execution; assigning one must not be described as recovery of an
unrecorded source generation. Unknown fixture fields, including `files`, are
refused; worker results arrive only through actual native wait calls.

`result.json` records all rounds, all completed provider call receipts (backoff
observations are not requests), total input-projection metrics, scope violations
and Registry cleanup. Missing/zero usage is `cost_complete:false`. Input metrics
count message JSON characters, not token billing or tool-schema characters;
`duplicate_chars_removed` is the gross removed result-body count. Recorded
provider `total_tokens` includes the common tool protocol and every recorded
manager call; it is not a monetary billing estimate. Persisted cache zero can
also represent upstream missing usage detail. Failed/cancelled rounds or a
mismatch between captured requests and completed call receipts make cost
incomplete, even when earlier calls had positive usage.

Private artifacts are explicitly created with mode `0600`, without changing the
subprocess umask: `actual-model-inputs.json` has original/forwarded requests;
`notifications.json` has complete actual tool calls/results (not clipped UI
notifications); `transcript.json` has the original full manager history. The
selected provider key
is redacted from JSON evidence before saving. A saved result or successful exit
is not quality acceptance: inspect outputs, usage completeness, errors,
references, worker identity and semantic correctness independently.

Tests include the real Registry/ADK/provider stack with a loopback HTTP spy at
the final SDK request. It proves that A forwards multiple results larger than
the UI clip, B retains one exact full body plus resolvable references, and
Registry tool receipts/history stay full. Those are offline integrity tests,
not real-model effectiveness evidence. Any positive screening result still
requires an actual-engine current-baseline comparison and affected runtime
acceptance before adopting product code.
