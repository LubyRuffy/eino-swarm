# Nuwa project evaluation

These opt-in evaluations measure task outcomes. They do not presume that more
workers, Nuwa mode or mixed models are better. They are separate from offline
tests and do not establish CyberGym performance.

The [retained observations](RESULTS.md) record separate generic and AppWorld dev
cohorts, their grading, actual model routing and measurement limits. Results
from different protocols are not pooled.

The current Nuwa contract separates planning from execution: the manager plans,
delegates, coordinates and accepts worker evidence, while workers perform the
required operations. The manager does not mount workspace/MCP execution tools;
workers retain the configured optional tool catalog. Pure discussion needs no
mandatory spawn. Role names, task division and model choices remain model
decisions, without a fixed domain workflow. Earlier cohorts that allowed manager
execution remain historical observations and do not validate this contract.

The separate required-assignment AppWorld dev comparison completed all 27
attempts with complete usage. It observed 4/9 successes for baseline, 4/9 for
autonomous Nuwa and 7/9 for assigned Nuwa. The assigned arm made actual DS
worker requests, but used more reported tokens and was slower on its three
successful paired tasks; the joint objective was not established. Its preceding
accounting interruption, full replication and role-compliance limits remain
separate in the retained observations. These are measurements, not routing
guarantees or product defaults.

`tools/agent_eval.py` accepts `--project-mode default|nuwa`, `--planner-ref` and
`--executor-ref`. These CLI choices use the existing provider ChoiceID, with a
tab between provider ID and model name. Model names may contain slashes; never
split a ChoiceID on `/`. Structured protocol files avoid shell tab quoting.
The agent tool's `model_ref` is a separate opaque token returned by `list_models`.
The project preference names provider/model and tells the model to call that
tool and copy its exact returned ref. Do not synthesize, escape or decode a tool
ref from the CLI ChoiceID, label or model-name string.

For supported configurations, the harness copies every enabled, configured
provider, its actual `api_mode` (including Responses) and saved model
catalog/context metadata into a new private engine configuration. It never
edits the source config. The snapshot is identified as configured data; it is
not proof that each upstream service currently answers. The planner selection
is acknowledged by the actual thread API. An executor preference is placed in
the project's prompt. No role name forces a worker model. In Nuwa, operations
must be delegated, while the manager chooses roles, tasks and available models;
a discussion that needs no operations can finish without workers. Per-call Trace
identities show what actually happened, and
`routing.executor_preference_realized` records the result.

The lightweight config reader accepts the engine's serialized provider shape;
it is not a general YAML parser. Nonempty structured `builtin_tools` cannot
currently be copied accurately and cause the evaluation configuration to be
rejected before a run, rather than silently dropping provider tools. Empty
`builtin_tools` and ordinary providers remain supported. This harness therefore
does not claim to preserve every possible provider configuration.

## Freeze and run a paired protocol

Build the candidate first, then copy `protocol.example.json` to private evidence
storage and choose tasks, models and repetition count before observing results.
Model names in this example are evaluation inputs, not product defaults.

```bash
python3 tools/agent_eval_pair.py --protocol /absolute/private/protocol.json \
  --zwai /absolute/candidate/zwai --out /absolute/private/eval --dry-run
python3 tools/agent_eval_pair.py --protocol /absolute/private/protocol.json \
  --zwai /absolute/candidate/zwai --out /absolute/private/eval
```

`--dry-run` starts no engine and makes no model inference requests. It freezes
the protocol, binary hash, task hashes, model choices and interleaved schedule.
The actual run freezes its own receipt. Do not run paid evaluations merely to
validate this harness; use its offline tests or `--mock` for that purpose.

The three example arms separate ordinary behavior, the Nuwa framework change,
and an offered mixed-model configuration. All arms share the base tool
configuration, configured model directory, per-task deadline, output cap and
review allowance. Nuwa adds its mode-specific collaboration and model tools,
with execution tools on workers rather than on its manager.

Each arm may provide an optional `project_instruction` string, empty by default.
It is appended as the last part of that arm's user project instruction, after
the task's project prompt and any executor preference. Use it to describe the
collaboration and resource constraints to evaluate; roles, task division and
model selection remain model decisions. Project instructions cannot relax the
Nuwa manager/worker execution boundary.
The complete arm text is frozen in the protocol and contributes to its hash.
It does not modify the common task or fixture fingerprints, so differing arm
instructions do not invalidate otherwise matching pairs. Inspect actual tool
calls and model identities to assess whether the instruction was followed.

The retained f3 six-row calibration had official successes A 1/2, B 2/2 and
C 1/2, with zero worker and DS requests. It does not satisfy the current
separation requirement. New source-boundary tests, an identified ELF and image,
and a fresh calibration must precede conclusions about this mode. Formal hard54
has executed zero rows. Its new source freeze and root admission remain pending;
offline harness tests and old calibration cannot substitute for that acceptance.

An arm may also freeze `reasoning_effort` as `low`, `medium`, `high` or `""`
(the model default). Omitting it leaves the thread default untouched and sends
no reasoning override. An explicit value is PATCHed onto the created thread
and must be acknowledged before a turn starts. Each result records the actual
thread API value as `reasoning_effort`; missing API metadata is `null`, not an
assumed default. The arm value changes the protocol hash, not the common task
or fixture identity. This selects the existing thread setting; upstream support
and its effect on model behavior require separate evidence. Freeze other inputs
when comparing reasoning levels so their effect is distinguishable from project
instruction changes.

Each execution owns a fresh engine, private data and workspace. Initial fixture
hashes, task hashes, budget and pair identity are saved. Scheduling uses a seeded
random task-block order and rotates arm order across repeats. It runs serially
to avoid one arm competing with another for local resources. Provider load and
remote caches remain external variables; no remote-cache reset is claimed.

Success is primary. Reports preserve failed attempts and infrastructure errors;
there are no automatic retries or selective removal of failures. Time deltas
use only pairs in which both arms passed, alongside every arm's success count.
Token deltas include all valid graded pairs, including failures. A missing
usage measurement, unsettled post-turn review or review interrupted at shutdown
makes token comparison unavailable. A negative delta means a
lower observed value, not a universal advantage or a significance claim.

Evidence includes actual model identities and usage observations. Older Trace
rows lose the distinction between absent usage and explicit zero; their zeros
are marked unknown. Raw provider usage or explicit presence metadata preserves
absent/zero/positive states. Known positives are retained as a lower bound, never
used to assert complete token savings when another call is unknown. Token counts
from different tokenizers are provider-reported quantities, not equal compute or
money; no cost estimate is invented. Post-turn review and other auxiliary calls
remain attributed. Full redacted Trace is saved privately for inspection.

Before stopping its owned engine, the runner writes `pre-cleanup.json` as a
provisional snapshot, not a completed result. `cleanup.json` records the cleanup
outcome separately from `result.json`. If a request or grading failure is
followed by a stop failure, the original `harness_error` remains intact and
`cleanup_error` records the later failure. Stop and early evidence failures
retain the private runtime; cleanup or finalization-evidence failure aborts the
batch without automatically repeating completed model work. If a late receipt
write fails after confirmed runtime removal, that directory cannot be retained;
the successfully written provisional snapshot and logs remain available.

`agent_eval_proxy.py` is a separate model-only measurement gateway. Its offline
HTTP tests check unchanged stream bytes, usage presence and credential/prompt
redaction. The ordinary runner does not automatically place live model traffic
through it. A gateway unit test is not an observation of production billing.
Mock reports explicitly identify their responses and usage as scripted and
cannot support model capability or token-saving claims.

Task fingerprints ignore only generated `__pycache__/*.pyc` files. All
other task sources remain guarded; initial fixture and workspace byte hashes
continue to include every regular file. This avoids an ordinary Python grader
import invalidating its own task for the next sample.

The record-reconciliation task accepts a component map or an array with unique
component names, as its prompt permits. A semantic command derives every
required revision/status/evidence, total and readiness value from the trusted
fixture; all four original source-record byte guards remain binding. A grading
correction applied to past evidence must be uniform across all arms/repeats and
retain raw scores, identities, timings and usage separately.

## External benchmark adapter boundary

`--task-root` selects a separate task directory using the existing task format:
prompt/project-prompt files, a deterministic initial fixture or generator, and
trusted post-run `command` checks. This is the extension point for an external
benchmark. A wrapper must retain official task ID/revision, environment/image
digests, the selected final artifact, raw evaluator output and its verdict.
Generic file checks or a model saying it succeeded cannot stand in for an
official benchmark verdict. Provision/reset external environments separately;
do not give the agent a directory containing hidden evaluators or patched code.

The separate AppWorld dev observation used the official SDK with a private
controller, an execution-only bridge and a model-only measurement gateway. The
agent received public task context and API documentation; evaluator/data files
and original provider credentials remained outside its container. Its wrapper
used the existing runner and `exec` client boundary, without a product MCP client
or domain-specific engine roles. This private experimental wrapper is not a new
`--benchmark` option of the generic runner. SDK/interface onboarding, actual
model assignment and task success must each be checked separately. A configured
executor preference is not an executed heterogeneous team.

AppWorld's official development restrictions permit dev error analysis and
require test splits to be used only for final aggregate evaluation. The retained
cohort used three fixed-seed dev scenarios and all three variations of each;
it is not a full benchmark or leaderboard result. See its
[upstream restrictions](https://github.com/StonyBrookNLP/appworld/blob/v0.1.3.post1/README.md#no_entry_sign-agent-development-restrictions).

For the public [sunblaze-ucb/CyberGym](https://github.com/sunblaze-ucb/cybergym),
freeze the upstream revision, task ID and difficulty, vulnerable source and
vulnerable/fixed image digests. Generate the official agent-facing task and use
the official evaluator on exactly one final selected submission. Report the
final-submission metric, as required by its [FAQ](https://github.com/sunblaze-ucb/cybergym/blob/main/FAQ.md).
Keep the submission service private and the fixed image/evaluator inaccessible
to the agent. Docker architecture/emulation, egress policy and environment reset
must be identical across paired arms. The official README offers a ten-task
subset and uses `arvo:10400` at `level1` in its smoke example. A container launch
or arbitrary-byte submission checks plumbing only; it is not a successful task.

CyberGym execution for this work was stopped by platform safety review. No
official task reproduction, hold-out grading, paid model benchmark or CyberGym
score was produced. Source/data downloads, image availability, an architecture
smoke and a headless preparation build are environment evidence only. They do
not establish task success. Preserve those receipts; do not retry this blocked
execution through another route. Continue independent generic engineering and
document fixtures through the ordinary protocol above.
The unaccepted specialized adapter and its dedicated runner hookups were
removed from this checkout and retained privately with hashes for review. The
CyberGym comparison remains an unmet requirement, not a reduced generic target.
