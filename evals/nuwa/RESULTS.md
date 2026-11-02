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

## Separate explicit-low generic cohort

A later frozen cohort used three generic tasks, three repeats and the same
three policy arms, with explicit `low` reasoning, a 16,384-token per-request cap
and a 60-second review allowance. Its 9 + 18 executions completed without
harness errors; every arm passed 8/9. This is a new protocol, not a regrading or
replacement of the earlier cohort.

| Arm | Success | Reported total tokens |
|---|---:|---:|
| `default-glm` | 8/9 | 777,779 |
| `nuwa-auto` | 8/9 | 1,645,177 |
| `nuwa-ds-preferred` | 8/9 | 3,240,778 |

The paired successful-time medians were below the two-second polling
resolution and do not establish a speed advantage. The DS preference produced
actual DS worker requests in 2/9 runs. Neither Nuwa arm established the combined
success, speed and token objective. All 27 owned cleanup observations passed.
Its private report is `generic-autonomy-low-cohort-v3/combined-report.json`,
SHA-256 `8c8193dc17b53933f1e3390be950143d14e4ab39e03062410e25a54a0f1ad185`.

## Separate AppWorld dev cohort: 2026-11-03

This independent non-security observation used AppWorld `v0.1.3.post1`
(upstream source `66ad8099e12188ece0d3fe45e661dbc01880813b`). Three dev
scenarios were selected by a frozen seed over scenario IDs, before inspecting
their task contents. Every scenario contributed all three variations. All
three arms ran all nine tasks in fresh controller/agent state; arm positions
were balanced within each scenario. No reference solution was executed and
no test-split task content or task-wise test evaluation was inspected.

The three arms shared the two-model catalog, GLM planner, explicit `low`
reasoning, 900-second task deadline, 16,384-token per-request cap and 60-second
review allowance. Only mode and the DS executor preference differed. The
official evaluator's boolean was kept independently of manager completion.
All 27 attempts obtained known official grades and clean harness/cleanup
results, without repeated model attempts or grading corrections.

| Arm | Official success / assigned | Normally completed success | Official scenario success | Complete reported tokens |
|---|---:|---:|---:|---:|
| `default-glm` | 1/9 | 1/9 | 0/3 | 5,163,656 |
| `nuwa-auto` | 2/9 | 2/9 | 0/3 | 9,350,374 |
| `nuwa-ds-preferred` | 1/9 | 1/9 | 0/3 | unavailable |

There were **zero same-task pairs where both compared arms completed
successfully**, for either candidate against default. A successful paired
speed comparison is therefore unavailable; faster failed attempts do not
establish a speedup. Auto used 4,186,718 more reported tokens than default.
These totals include failed attempts and auxiliary calls observed by the
gateway after its writer stopped. One DS-preferred run had incomplete cost
evidence: its arm's known subtotal of 4,561,609 is not a complete total and
cannot establish token savings. This does not invalidate the complete
default-versus-auto token comparison.

All **982 gateway model requests** used GLM (319 default, 392 auto and 271
DS-preferred); Trace contained zero worker requests. The Nuwa-specific
`list_models`, `read_work` and `record_work` tools, and the existing
`spawn_agent` tool, were not called. Existing tool counts were nonempty and
matched each Trace's actual `tool_call` events, so empty metrics were not
treated as proof of non-use.
Mode snapshots matched all 27 assignments. A separate real-engine/canned-model
input fixture confirmed delivery of the Nuwa instruction and these tool schemas
at the upstream HTTP boundary; that fixture measures no model capability.
The observation establishes that the configured preference did not produce
heterogeneous execution here, not that those framework tools are unavailable.

The onboarding supplied public task context, API docs and the persistent remote
Python interface. It did not explicitly remind agents to call the environment's
task-completion API. Dev error analysis found both a missing completion call
after substantive work and incomplete collection pagination in the first fixed
task block. Original official failures remain unchanged. Any later clarified
onboarding or assignment experiment is a separate protocol, not evidence that
this cohort improved.

Neither candidate met the preregistered joint objective. These are descriptive
results for three dev scenarios, not a universal or statistically significant
comparison, a full AppWorld result or a CyberGym score. Case cleanup was
independently checked by run identity across world, proxy and agent namespaces;
all owned containers/networks were absent and the frozen source hashes matched.

| Private evidence within `20261102-01a199cc/public-appworld` | SHA-256 |
|---|---|
| `dev-protocol-frozen-v1.json` | `1b82c75d02287814ab3ba92e1ba036a76b83dba4245f4cd0af73eb0c25fb16c9` |
| `dev-report-v1/report.json` | `fb654638c7e39f669a749581d5b66b0a577e80570b7eb061e6f8ef974d5400bd` |
| `adoption-audit.json` | `5a9443d7aa9c4c2089fc75a8a1a0604ab23b58cb370e9039ae2cee10c02923bd` |
| `dev-report-v1/independent-resources-and-source.json` | `1d8ddab478bb84d8c1ef8c6e7647309666e23facaeac5784985bfde457c37a5b` |
| `full-chain-input-proof-r2/actual-manager-input-metadata.json` | `883a4deebc1cefd441ac441fb711b0a3f943517c15a85e4db30a5a60efd46642` |

The harness source was `90e92ab8afd2db82f8e3e40cf10568406ba1baf0`; its
Linux ARM64 engine was built from `f8d0ae42` with unchanged Go inputs, ELF
SHA-256 `6f2da7cb86c5af873fee03f85ba03bc7043ae2dc68448882bc3f9b571970daeb`.
The full controller/agent image IDs, public fixture hashes, bundle hash and
per-row evidence are retained in the protocol/report. This headless evidence
does not replace native desktop acceptance or change the accepted macOS
v0.1.57 installer.

## Separate configured-assignment pilot

One subsequent dev case checked the user's requested planner/executor split
through project instructions. It used the first task in the original fixed-seed
selection list, a fresh world/engine and unchanged product/adapter code. The
instruction clarified documented completion/pagination conventions and required
application execution to use the configured DS executor. Worker roles, count,
decomposition and order remained model decisions. Manager static read-only
inspection was permitted; this differs from the older diagnostic that banned
every manager `exec`.

The official task passed. Actual inputs/results showed five GLM manager
requests, twenty DS worker requests and 419,387 reported tokens with complete
cost evidence. The manager's one `exec` only read static local inputs. All
sixteen remote application-code executions belonged to the DS-bound worker;
its remaining tool work inspected local inputs. The exact catalog ref reached
`spawn_agent`, the worker retained its generation/model binding, and the
manager collected its actual finished result with `wait_agents` before ending.
Independent run-identity checks found no surviving owned container/network.

This demonstrates one successful configured assignment. Both interface
onboarding and the assignment instruction changed, so it neither isolates a
cause nor establishes success, time or token superiority over the old cohort.
It is not pooled with those 27 attempts. A new comparison must give every arm
the same clarified interface information and freeze its task selection and
instructions before inference.

| Private evidence within `20261102-01a199cc/public-appworld` | SHA-256 |
|---|---|
| `assigned-execution-pilot-protocol-v2.json` | `e5a470d93ebebd054d1709be39c140e9f8201ebba0d657831eeb45c485fb6638` |
| `assigned-execution-pilot-run-v1/benchmark-result.json` | `04600880a451b514f64052519efdd8fb44dd8e86d4f8848376a36c24d5b6d415` |
| `assigned-execution-pilot-role-audit.json` | `3043110f85a5583123f8d3211dc72079b59c7f4f797cca6ee5b6e7959132521d` |
| `assigned-execution-pilot-independent.json` | `2ae4fd3d06452e040feaa1691f7cbce7cea5f341671936f47c218f38b2a9ba3f` |

## Evidence identity for the original generic cohort

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
