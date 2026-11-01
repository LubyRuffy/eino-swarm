# Task-derived monitoring decision trial

See [the applicability assessment](ASSESSMENT.md) for the upstream sources and
the adoption gate. This opt-in measurement uses a configured real provider:

```bash
go build -o /absolute/private/zwai-eval ./cmd/zwai
python3 tools/superagent_eval.py --provider <provider-id> \
  --zwai /absolute/private/zwai-eval --runs 2 --review-wait 240 \
  --plan-only --out /absolute/private/new-round
python3 tools/superagent_eval.py --provider <provider-id> \
  --execute-plan /absolute/private/new-round/manifest.json
python3 -m unittest discover -s tools -p test_superagent_eval.py
```

The output directory must be new and outside the checkout. The execution plan
binds the binary, runner, fixtures, generated prompts and effective provider
configuration; changing one refuses execution. An already executed plan cannot
be resumed or overwritten. Keep its failures and create a new identified round
if another condition needs measurement. Any unverified process cleanup stops
the run before another case; that is an execution-evidence failure, not a
triage-quality conclusion. `--mock` is infrastructure-only and its scripted
behavior does not establish decision quality or satisfy this trial's restricted
tool-use contract.

This fixture tests whether one generic, low-interruption triage instruction
improves read-only decisions made by the current engine. It does not test the
whole Super-agent architecture, a persistent worker, an event filter, actual
scheduled notifications, or completion of the original operational work.

## Source and construction

Two previously executed monitoring task families supplied the task requirements:
model-service deployment and a daily data refresh. Their first human request,
the latest monitoring instructions and three recent completed scheduled turns
were inspected through a read-only database connection on 2026-11-01. Source
material and its private identity mapping remain outside this repository.
Addresses, accounts, command paths, business table names and live task identifiers
have been removed. Numerical observations, timings, nodes and service names in
this fixture are constructed for the trial.

Every `observations` object explicitly declares
`constructed_observation_from_real_task`. No observation below is an exported
infrastructure result. Historical assistant answers were not used as ground
truth: some deployment answers claimed probes and schedule reports without
matching tool or schedule-report events. Those claims do not establish actual
execution, actual deployment state or correct monitoring decisions.

Each family has six cases: normal progress, stalled or overdue work, apparent
success with required verification missing, verified completion, confirmed
failure and unavailable observation. A seventh deployment case crosses the
task's 30-minute no-progress threshold while the probe's raw body hash remains
unchanged. This is a counterexample to treating byte equality as no event.
The deadline matters because the source task explicitly requires action at
that threshold; no universal deadline-notification rule is assumed.

## Files and output contract

`cases.json` contains only the task instructions and constructed observations.
Each case has `id`, `family`, `instruction` and `observations`. Observations have
the construction marker, the current clock and evidence records with distinct
`id` values. Optional `context` supplies the task's existing deadline or cadence.
The model receives the observations as `evidence.json` and must actually read
that file. A completed decision file alone does not prove this read; the harness
must retain and check the corresponding Trace tool receipt.

Both arms write exactly one `decision.json` with the same schema:

```json
{
  "decision": "notify",
  "action": "verify",
  "evidence_ids": ["an-existing-evidence-id"],
  "notification": "A supported finding and the next step."
}
```

`decision` is `quiet`, `notify` or `unknown`. `quiet` means no notification is
required for this check, and its notification text must be empty. `notify`
means the observed event warrants a useful notification. `unknown` specifically
means the monitored state cannot be established because the current observation
failed; it does **not** mean that the notification disposition is unknown.
These two source tasks require a nonempty, actionable observation-failure notice
for their unknown cases. Thus `unknown` counts as a notification, never as quiet.

`action` is `continue`, `investigate`, `verify`, `complete`, `escalate` or
`unknown`. Continuing preserves the existing cadence. Investigating requests
additional evidence for a stall or deadline event without fabricating failure.
Verifying obtains the named outstanding completion evidence. Completing requires
all named evidence and closure readbacks. Escalating reports an already confirmed
failure and recommends evaluating the task's existing recovery plan; it does not
automatically request new human approval. Unknown reports unavailable observation
and the need to recover it. A notification need not imply that a human must act.

The task instructions preserve their source's low-interruption contract. Neither
source asks for routine progress notices. If a different human instruction did
require such notices, ordinary progress would not be quiet under that contract.
Changed human directives and unrelated noise are not covered by this small
fixture; no robustness claim is made for those boundaries.

## Independent expectations and review

`expectations.json` is the independently authored oracle and must never enter
either arm's prompt or workspace. It maps each case to expected `decision`,
`action`, required supporting evidence IDs and whether notification text must be
nonempty. `expected_notify` counts `notify` and `unknown` as notifications.
`critical` marks stalls, failures, incomplete success verification and observation
failures; missing one cannot count as an efficiency gain.
`requires_fixture_read` requires the actual read receipt. `semantic_rubric`
states the findings and unsupported claims to check against the decision and
the complete visible answer. IDs existing in the fixture do not by themselves
prove that their contents entail the decision.

Automatic checks must reject missing or malformed decisions, wrong types or
enums, invented evidence, wrong actions, missing required notifications and
nonempty quiet notifications. Independent semantic review checks support,
usefulness, unsupported completion, claimed side effects and any notification
outside `decision.json`. Record a verdict for every rubric item, including failed
runs; structural checks alone are not semantic acceptance.

All actions in this trial are recommendations written to `decision.json`.
The supplied tasks authorize no network probes, database refreshes, service or
network changes, process termination or real schedule mutation. Each model run
must use a private workspace, data directory, HOME and owned engine process.
These private defaults do not confine the product's full-access tools; review
the actual Trace paths before accepting the trial.

## Comparison and interpretation

Arm A uses the current engine, its ordinary task instruction and the shared
output contract. Arm B changes only the additional generic triage instruction.
The same case's evidence bytes, model, tools, budgets and output schema must be
identical in both arms. Counterbalance paired order, freeze the observations,
labels and analysis policy before outputs are inspected, and retain all failures.
Repetitions are reruns of the same 13 constructed states from two task families,
not additional independent real tasks.

Report decision, action and semantic quality by family and case. Gate critical
misses and unsupported completion separately. Compare excess notifications
against these frozen labels; reducing required notifications is a regression.
Report tokens and time for every attributable caller, retaining unavailable
usage as unavailable. A local decision-file improvement would still require
validation through the actual scheduled report, quiet, unread and failure paths
before any product change. A non-improvement retains the current product.
