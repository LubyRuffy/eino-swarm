# Generic Nuwa observation: 2026-11-02

This cohort did not demonstrate higher task success or a consistent efficiency
gain from Nuwa or the offered executor preference. It is a small descriptive
observation of the configured models and tasks, not a general framework ranking.

The frozen schedule contained three engineering/document tasks, three repeats
and three arms: 27 task executions. Every arm used `glm-5.3-flash` as planner;
`nuwa-mixed` additionally preferred `dsv41-flash-1017` for useful delegation.
Roles, delegation and model choice remained the manager's decisions. The arms
shared the configured catalog, base tool configuration and budgets: 600 seconds
per task, 16,384 output tokens per model request and a 30-second post-turn review
allowance. Nuwa adds its collaboration/model tools.

## Observed outcomes

| Arm | Raw grading | Uniformly corrected success | Complete token measurements | Reported total tokens |
|---|---:|---:|---:|---:|
| `default-glm` | 3/9 | 6/9 | 9/9 | 1,712,845 |
| `nuwa-glm` | 4/9 | 6/9 | 8/9 | unavailable |
| `nuwa-mixed` | 3/9 | 6/9 | 9/9 | 1,998,749 |

All arms passed all three repeats of `nuwa-source-reconcile` and
`multi-module-bugfix`, and none passed `spec-edges`. There were no harness-error
exclusions in the final cohort. Failed model/task attempts remain in the data.

Compared with `default-glm`, the median paired task-time differences were
**+5.01 seconds** for `nuwa-glm` and **+0.99 seconds** for `nuwa-mixed`, using the
six pairs where both arms passed. Positive means the candidate took longer.
`task_s` runs from turn submission through conversation settlement; it excludes
engine startup, fixture preparation, post-turn review, grading and shutdown.
These differences do not measure end-to-end completion cost. An arm's median
over all graded tasks is a different statistic and does not establish a paired
speedup.

Token totals include failed graded attempts and all model calls captured in
their Traces, including observed auxiliary/review calls. Complete comparison
requires known usage and a settled review without shutdown cutoff. One
`nuwa-glm` run had an unsettled review: its captured 234,590 tokens remain an
observed lower bound, so the arm's complete total and token comparison are
unavailable. The mixed arm's aggregate total exceeded the default arm's; its
median paired token difference was -4,026 across nine pairs. Both statistics
are retained and do not establish general token savings. Different models'
reported tokens are not equal compute or money.

The DS executor preference was realized in **1/9** mixed-arm runs
(`multi-module-bugfix`, repeat 3, with 37 DS worker-call records). The other
**8/9** ran solo GLM. This is an offered-preference comparison, not nine
executed GLM-plus-DS teams. It does not isolate the benefit of heterogeneous
delegation when delegation actually occurs.

## Recovery and grading corrections

The schedule completed as **7 + 6 + 14** executions with two controller
interruptions. Python grader caches first invalidated an overly broad task
hash. The first continuation then stopped after a completed sample because a
runtime tuple and its durable JSON array were compared directly. The completed
sample was recovered from its durable result. Original identities, order,
prompt/fixture bytes, model configuration and budgets were retained; completed
model requests were not repeated. Generated-cache hashes were normalized only
in derived task identities, retaining each original raw hash.
The initial full-configuration byte hash was not recorded; later checks of the
retained copy and model summary cannot provide a retroactive initial attestation.

The record task's prompt allowed a component map or an array with unique
component names, but the original checks accepted only a map. A single semantic
grader was applied to every arm/repeat, deriving all requested revisions,
statuses, evidence, totals and readiness from the trusted fixture. The four
original unchanged-source guards remained binding. Raw scores/checks and all
recorded usage/timings were retained. Correcting success changes which pairs
qualify for successful-time comparisons; check-count pass ratios are not
comparable across the two graders.

This was not one uninterrupted successful batch. Provider load, caches and
time separation between segments remain uncontrolled variables; three repeats
per task do not support a significance or general superiority claim. A separate
strict-delegation check is described below and is outside this cohort. No
CyberGym score is established by these runs.

## Separate strict-delegation observation

One additional headless record-reconciliation case produced a semantically
correct decision, ten GLM manager calls, five DS worker calls and the correct
canonical model binding. The DS worker wrote `decision.json` and executed its
validation.
The GLM manager also used `exec` for read-only `ls`/`cat`, violating the frozen
policy that all commands belong to DS and the manager must not use `exec`.
Therefore `strict_policy_compliance=false`: this case demonstrates observed
delegation and artifact work, but is not a complete strict-policy pass. It is
excluded from the 27-run outcome, timing and token comparisons. Separate
completion and cleanup receipts confirm the tool acknowledgements, owned
engine exit, closed port and removed runtime. They do not establish native UI
acceptance.

## Separate assignment and reasoning diagnostics

Two later, one-repeat `spec-edges` pilots investigated the failure to act. They
are development diagnostics, outside the original 27 executions; their scores
are not pooled with that cohort or treated as held-out superiority evidence.
Both used the same identified product binary and unchanged task/fixture/grader,
600-second task deadline, 16,384-token output cap and 60-second review allowance.
Project instructions requested incremental progress. The two Nuwa arms also
assigned implementation and execution-based validation to the named executor,
leaving worker roles, count and scopes to the manager.

With default reasoning, all three arms exhausted the output cap before writing
an implementation or starting a worker. The mixed manager explicitly referred
to its assignment and planned delegation, but continued reasoning until the
cap. This is evidence of instruction delivery, not a failed model-ref lookup.

The second pilot changed only the explicit reasoning selection to `low` in
every arm, using the existing thread API. All local selections were acknowledged;
this does not independently prove an upstream serving implementation's internal
reasoning policy. Every turn finished without an output-limit error, but none
passed the complete task.

| Arm | Default-reasoning seconds / tokens | Explicit-low seconds / tokens | Low worker calls |
|---|---:|---:|---:|
| `default-glm` | 94.46 / 80,630 | 20.12 / 168,968 | 0 |
| `nuwa-glm-assigned` | 82.39 / 88,352 | 74.33 / 642,310 | 32 GLM |
| `nuwa-mixed-assigned` | 88.43 / 88,317 | 118.57 / 450,115 | 18 DS |

All reported totals have settled reviews and no observed review cutoff. They
include auxiliary calls and failed attempts. Default reasoning has no successful
task-time comparison, and the low Nuwa arms used more time and tokens than the
low default arm. Provider load, caches, time separation and one repeat prevent
a causal or general efficiency claim across the two pilots.

In the low mixed arm, three DS workers implemented the modules in the required
sequence, copied the exact catalog ref, retained `low` model bindings and had
their generation results collected through actual `wait_agents` calls. The
manager also executed validation commands itself, violating this pilot's frozen
assignment. Actual DS calls therefore do not establish complete policy compliance.
All low arms failed the money rule's 5,000-digit boundary because of Python's
integer/string conversion limit; the default arm additionally failed a headers
rule. The grading and failures remain unchanged. Conversation completion is
not task success, and these pilots establish no speed/token/success advantage.

The private evidence set contains `assigned-model-pilot-v1` and
`assigned-model-pilot-low-v2`. The latter additionally retains per-arm owned
engine exit, closed listener, stopped presence reader and removed runtime
observations. The first pilot did not capture those independent cleanup receipts.

## Evidence identity

The private evidence set is `20261102-01a199cc`. Identifiers below allow a
reviewer to locate the retained reports without publishing provider credentials
or the private configuration.

| Artifact | Identity |
|---|---|
| Clean engine source commit | `c7aa62f72f9148b96e1b63b7196f78b771214303` |
| Source Git tree | `0548c5754fc4db1d53f3080e1f534ab5d9c20f61` |
| Candidate binary SHA-256 | `e2033714dac789652b193377b1f341732fa9da50e239ec9d10bc054eff825dbd` |
| Protocol identity, canonical frozen metadata SHA-256 | `b8e8a1c6420dfe10e63cc0f4f2a5e1e41b8ed5c008e7996dfa39c966a16483f1` |
| Frozen protocol file SHA-256 | `e2772b66e53375b09b72f6c9c21a8b97dcbb38cecde33f25622a80b4e5033f2c` |
| Original interrupted 7-run report SHA-256 | `19e52b970742e8e7e6b0e1b8a05a6990eeed607675b30affdf9eeeeabed1b9a2` |
| Complete 27-run raw-grading input SHA-256 | `295bc4746de8c4edf06ecc2e051ef8dee209ceb48316d4a9907ba681f1568bca` |
| Uniformly corrected 27-run report SHA-256 | `9b2c067b99780ba4415a797ce89e41a649f06957a7c3ae03112e49fc05161674` |

Within that evidence set, the original report is
`generic-live-final/runs/20261102-115109-671187/report.json`; the retained
27-run input and corrected output are
`grading-repair-prep/uniform-posthoc-final/raw-report.json` and
`grading-repair-prep/uniform-posthoc-final/corrected-report.json`.

The separate strict-case artifacts are under `strict-policy-capability`:

| Artifact | SHA-256 |
|---|---|
| `capability-result.json` | `bd132c02b6881fc9f52d9a91c24e2ffa6b45e47ff29ebdc323a1179564c2eecd` |
| `completion-audit.json` | `43b2146ab33034b27d82b17a9d41b8ed5ebad328c5b1118bf9cda1eb10dd5add` |
| `receipt-manifest.json` | `b2cbe2377691bea240950631b9a1df210889f643869af297a56475c26e827596` |
