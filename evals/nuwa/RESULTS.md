# Generic Nuwa observation: 2026-11-02

This cohort did not demonstrate higher task success or a consistent efficiency
gain from Nuwa or the offered executor preference. It is a small descriptive
observation of the configured models and tasks, not a general framework ranking.

The observations retained here predate mandatory Nuwa planning/execution
separation. They preserve each cohort's original policy and do not validate a
manager that delegates every required operation. The current boundary requires
new source tests, an identified ELF/image and fresh calibration; see the
[legacy f3 calibration](#legacy-f3-calibration-2026-11-06).

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
results, without repeated task executions or grading corrections. Internal
model retries remain included in the request and token observations.

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

## Interrupted AppWorld assigned cohort v2

A separate frozen comparison selected the next three unused dev scenarios in
the same ID-only order, including all three variations. All arms received the
same clarified completion/pagination onboarding. Here the retained
`nuwa-ds-preferred` arm ID denotes a required project-instruction assignment:
remote application execution belongs to DS workers, while the GLM manager may
inspect static local inputs. Roles, scopes and worker count remain model
decisions. This policy differs from the earlier soft executor preference;
the observations are not pooled with the preceding cohort or assignment pilot.

The launcher accepted eight rows, preserved the ninth result and then stopped
under its frozen incomplete-observation rule. **Nine task executions occurred;
18 assignments were not executed**, with no continuation or replacement tasks.

| Arm | Official success / known grades | Executed / assigned | Not executed |
|---|---:|---:|---:|
| `default-glm` | 1/3 | 3/9 | 6 |
| `nuwa-auto` | 2/3 | 3/9 | 6 |
| `nuwa-ds-preferred` | 2/3 | 3/9 | 6 |

The fixed assigned denominator remains nine per arm; unexecuted assignments
are unknown, not official failures. Row 9 retained official success and a
`done` manager, but its gateway had **26 starts and 25 ends**. Its nominal
60-second review window took 61.6 seconds; review remained unsettled and the
engine warned that a memory review was still running at shutdown. The launcher
stopped on incomplete model-request observations. This is an accounting stop,
not a rewritten business-task failure. Gateway receipts alone do not identify
the unmatched request's agent role.

Complete cohort cost is unavailable: 18 assignments never started, row 2's
review wait expired without a settled review, and row 9 was cut off. Known
token subtotals are not complete totals. This partial observation cannot
establish the frozen full-cohort joint objective. Independent role assessment of the three attempted
DS-assigned cases found actual DS workers in each, with **two policy passes and
one unknown**.
The unknown case included a manager-side remote-query attempt without a
validated response. It is not counted as compliant; role verdicts were formed
before joining the official outcomes.

A separate controlled fixture kept the frozen Go/SDK/proxy chain and delayed
only the fake reviewer's final usage/DONE by 13 seconds after its first chunk.
With a 0.1-second review allowance, it reproduced cutoff and 4 starts/3 ends,
leaving complete cost unknown. With 30 seconds, it obtained a terminal
`memory_review` event, reviewer usage 5 and 4/4 paired requests (synthetic total
20). The terminal review reported `proposal_missing`; settled accounting does
not mean successful memory learning. No real model was called, and these
fixed synthetic counters establish no capability or efficiency result.
Independent cleanup confirmed the fixture's six containers, four networks and
host process absent.

A subsequent v3 protocol was frozen for a fresh full comparison, changing only
the common review allowance from **60 to 300 seconds** among experimental
controls. Task selection, business prompts, assignment policy, models, grading
and other budgets are unchanged. This is a separate cohort, not a continuation
or selective retry of v2. Its results are reported separately below; the
controlled fixture does not guarantee that 300 seconds will cover real reviews.

| Private evidence within `20261102-01a199cc/public-appworld` | SHA-256 |
|---|---|
| `assigned-dev-protocol-frozen-v2.json` | `0909099c907a412a3aab882b04e23173c354b81d44d0c2c2b1d12efe115a32ad` |
| `assigned-dev-report-v2-partial/report.json` | `fb87a8f5ea37a457883bbe44e0e8daa5987cfbd0ea24c82bfa549b18f3eb39ff` |
| `assigned-v2-partial-role-audit.json` | `b0086a937ecebf15367f40798945cb9958fc5e3785d4c5fd7609889f4d23a9a0` |
| `aux-review-budget-fixture.report.json` | `75251dcf2a721dcd27fdfd44c0c01c047b605d0bc94f6d060d7217442cfd3d9b` |
| `aux-review-budget-fixture/independent-cleanup-v2.json` | `9c14defb3c1d1b0a6cc7b029aa9562f6508cd9e74dacee42c5a76a2e7c9bf146` |
| `assigned-dev-protocol-frozen-v3-review300.json` | `7f0dd0d726d19b964e972f546c54519cd3a3c5618eecb255f8198ba96cb71db1` |

## Complete AppWorld assigned cohort v3

The fresh v3 schedule executed all 27 assignments: nine dev tasks per arm,
covering all variations of three fixed scenarios. The same frozen public
fixtures, onboarding, model selections, assignment policy and official grading
from v2 were retained, with a common 300-second review allowance. All attempts
obtained known official grades and complete provider-reported usage, including
failed attempts and auxiliary calls. There were no infrastructure, evidence or
cleanup errors, replacement tasks or repeated task executions. The interrupted
v2 rows were not resumed, regraded or pooled into this comparison.

| Arm | Official and completed success / assigned | Official scenario success | Complete reported tokens | Actual DS worker requests |
|---|---:|---:|---:|---:|
| `default-glm` | 4/9 | 1/3 | 4,232,714 | 0 |
| `nuwa-auto` | 4/9 | 1/3 | 5,665,544 | 0 |
| `nuwa-ds-preferred` (required assignment) | 7/9 | 1/3 | 7,599,624 | 258 |

The assigned arm observed three more successful tasks than baseline, but did
not meet the joint objective. Across its three same-task pairs where both arms
completed successfully, the median paired task-time difference was **+15.260087
seconds**, with **+15,186 ms** in the persisted engine duration on that same
pair set. Its full nine-attempt token total was **3,366,910 higher** than
baseline. The successful pair IDs were `6bdbc26_1`, `6bdbc26_3` and `4fab96f_3`.
These pairs establish neither faster completion nor fewer reported tokens.

The autonomous arm tied baseline success. Its three successful pairs were
`6bdbc26_1`, `6bdbc26_2` and `6bdbc26_3`; median differences were -0.008587
seconds and -339 ms. The task-time difference is below the frozen polling
resolution threshold and cannot establish a speed advantage. Its token total
was 1,432,830 higher than baseline. Neither candidate met the frozen joint
success/speed/token rule; its conditions were not changed after observation.

The gateway recorded 268 GLM requests for baseline, 315 for autonomous Nuwa, and
114 GLM plus 258 DS requests for assigned Nuwa. Baseline and autonomous Nuwa
made no worker requests. These are observed choices despite the available
collaboration tools, not evidence of a missing tool surface. Model bindings and
instruction compliance are separate: actual DS use does not prove that every
application execution was delegated as requested.

A separate role audit found **eight policy passes, one failure and no unknowns**
across the nine assigned cases. In the failed case, the manager performed two
remote application executions with validated responses, contrary to the frozen
division of work. All 258 worker requests used the expected DS binding; all 14
worker generations retained their model configuration and had their actual
finished result collected before manager completion. Role verdicts preceded the
join with official outcomes, and no performance row was excluded for a role
failure. One initial metadata-inspection error exposed pre-grade Trace content;
the audit records that incident and does not claim complete content blinding.
It did not inspect official grader outcomes before classifying roles. Prompt
instructions alone therefore did not guarantee the requested routing policy.

Subsequent metadata-only cost attribution retained all 955 requests and their
original grades. The assigned arm added 6,458,645 worker tokens while manager
tokens decreased by 2,983,330 and all auxiliary tokens decreased by 108,405.
Memory review itself decreased by 164,392, so it was not the source of the
cohort's higher total. Prompt tokens accounted for 3,329,330 of the 3,366,910
increase (98.88%); that comprises 3,142,144 more provider-reported cached prompt
tokens and 187,186 more uncached prompt tokens. Completion increased by 37,580.
Cached tokens are a subset of prompt tokens, not an additional total or a
verified monetary discount.

All nine spawns omitted `fork_context`, so these workers did not inherit the
manager conversation through that option. Their nine initial generations and
five normal resumes showed growing per-generation message counts and prompt
usage. There were zero restart-checkpoint events. These observations do not
prove abnormal repeated context, and the separately reproduced checkpoint-tail
recovery bug cannot explain this cohort. Attribution joins each gateway's own
start/end request ID, then uniquely matches its usage vector to a same-case
Trace call with timing corroboration; those two sources have no shared request
ID. No performance sample was excluded or regraded.

Independent run-identity inventories made 162 container/network queries across
all 27 attempts and found no surviving owned resources. All 13 frozen source
hashes matched. This is a descriptive result for nine tasks from three selected
dev scenarios, not a full AppWorld benchmark, a significance claim, billing
comparison or CyberGym result. Provider tokenizers differ. The accepted native
macOS v0.1.57 installer remains unchanged.

| Private evidence within `20261102-01a199cc/public-appworld` | SHA-256 |
|---|---|
| `assigned-dev-protocol-frozen-v3-review300.json` | `7f0dd0d726d19b964e972f546c54519cd3a3c5618eecb255f8198ba96cb71db1` |
| `assigned-dev-report-v3-review300/report.json` | `560ac7595d7be14b4ba43e1dd48e8660641b94329671db42d13f344fbb172e62` |
| `assigned-dev-report-v3-review300/independent-resources-and-source.json` | `af9cca304f533b142a1239997e2cee3108f9dd4250acf487395d4e7400209bcf` |
| `assigned-v3-independent-statistics.json` | `68224c2ca2d61f1fa07461fea7cb5ceab857424b3179b72717f9157414568396` |
| `assigned-v3-role-audit-v2.json` | `a9ec179ea2293aac2aeed845555b47dfc6f13d67523ccc8c7a50aae9f0cb430a` |
| `assigned-v3-cost-attribution.json` | `e344bdba69e1bd578178b22dae58344d06373ff807c27a3762142516582f422f` |
| `assigned-v3-cost-attribution-independent.json` | `5ed3dfbbb2da3f2468429d822b3352a478499d8be061c33ae2b56951dc0e739b` |

## Separate AppWorld shared-instruction dev cohort

This 27-run cohort used three selected AppWorld dev scenarios with three tasks
each. The baseline used GLM alone. The two Nuwa arms requested a GLM planner
with DS execution. One Nuwa arm used the candidate's shared-instruction
forwarding syntax and the other kept the same excerpt as ordinary prompt text.
The manager could see the syntax difference.

| Arm | Official passes / assigned | DS worker requests | Reported total tokens | Median paired task time vs default |
|---|---:|---:|---:|---:|
| `default-glm` | 8/9 | 0 | 3,216,484 | — |
| `nuwa-ds-preferred` | 9/9 | 184 | 4,912,524 | +46.16 s (8 successful pairs) |
| `nuwa-ds-shared` | 8/9 | 163 | 4,352,500 | +32.70 s (7 successful pairs) |

Against the preferred arm, the shared arm took 14.55 seconds less at the
median over eight common successes and used 560,024 fewer reported tokens
across all nine assignments. It passed one fewer task. Both Nuwa arms used
more tokens and took longer on common successful tasks than default. The
candidate did not meet the joint quality, speed and token criteria.

| Private evidence within `worker-efficiency-input-audit-01/v8-shared-instruction-dev` | SHA-256 |
|---|---|
| `live/protocol.json` | `19158f31714c7d9541effa42bed432b7e2c3c193c2e95e733f13d94d5641fd43` |
| `report/report.json` | `2d2b374f34d5518568f9efedec289227972f2ec927a17214643eaadb9e58a2d3` |
| `launch/cleanup-verified.json` | `5e98de280a471cfdf65b9a5f8a6bc567e3d081529be9031c20815fc6d3487fb4` |

## Separate cross-module specification diagnostic

This fixed-order pair reused one development task with four independently
implementable modules and 17 original checks. Both arms received the same
public fixture, GLM planner, model catalog and limits. The Nuwa project
instruction asked the planner to delegate independently verifiable work to
DS and select the worker's tools. Each solver ran in a fresh container.
An independent evaluator received the public files and four designated
deliverables only after the solver exited.

| Arm | Checks passed | Terminal task time | Gateway requests | Reported total tokens |
|---|---:|---:|---:|---:|
| `default-glm` | 15/17 | 101.92 s | 72 | 961,586 |
| `nuwa-auto` | 15/17 | 91.23 s | 46 | 625,219 |

Three DS workers made 29 model requests and wrote deliverables. The planner
collected each worker's result from the same generation that did the work.
Both arms missed the same two original checks. The 10.69-second terminal-time
reduction and 336,367-token reduction
describe this pair. Neither arm passed the whole task, so successful paired
speed is unavailable. This pair used an experimental engine from before the
shared-work receipt-reference change.

An earlier attempt ended after the default arm. Its session-memory request
exceeded the 35-second provider deadline and yielded incomplete token usage.
Its 15/17 score remains separate. The completed pair gave both arms the same
75-second provider deadline and retained the original whole-arm budgets.
Separate probes of the saved deliverables found representation-limit
violations against the public specifications in both arms. Those probes
did not modify the original scoring or enter the solver inputs.

| Private evidence within `worker-efficiency-input-audit-01` | SHA-256 |
|---|---|
| `independent-spec-container-pair-timeout-01/inputs.json` | `3631ded072fcb288a7d15138a8e250cdad61a353c0184e8662c59fdeac88de89` |
| `independent-spec-container-pair-timeout-01/launch-real-01/audit.json` | `8018b88662876884dbc43f7f403351a6cc6762d7d678cb76563e76b74548e61e` |
| `independent-spec-container-pair-timeout-01/original-timeout-attribution.json` | `8b0fd4116f6ccf25698c4e3344b2d12a814686458ce70bcb246921460d65d2f5` |
| `independent-spec-public-boundary-audit-02/audit.json` | `96aedc2a33e70c8d4fa348619d91e8db4eb994364ea8a8647c611aad320039e2` |

### Public boundary acceptance instruction

A fresh pair on the same development task added the same acceptance criterion
to both project prompts. Where an implementation limit could narrow a public
requirement, the model was asked to find the limit, run a source-derived
counterexample and keep that check through any correction. Both arms used the
same engine, fixture, budgets and independent evaluator as the preceding
completed pair. The DS delegation preference remained specific to the Nuwa arm.

| Arm | Checks passed | Terminal task time | Gateway requests | Reported total tokens |
|---|---:|---:|---:|---:|
| `default-glm` | 15/17 | 76.00 s | 36 | 783,070 |
| `nuwa-auto` | 14/17 | 95.51 s | 46 | 875,832 |

One DS worker implemented duration and made eight model requests. The GLM
planner received its completed result for the same worker generation and
then edited the deliverable. The reviewed tool calls covered finite examples
and semantic bounds. Neither trace established a source-derived check that
crossed the runtime conversion limit. The final files in both arms still
violated the three public representation-limit probes. The two separate
invalid-ISO probes passed in both arms. These probes ran after grading, using
the exact scored file bytes. They were separate from the 17 original checks.

Nuwa passed one fewer check and used 92,762 more reported tokens in this pair.
Its terminal task time was 19.51 seconds longer. Neither arm passed the task,
so successful paired speed is unavailable. This repeated development task
does not establish a causal effect of the instruction across runs. The
request-level evidence is complete and both arms cleaned up their resources.

| Private evidence within `worker-efficiency-input-audit-01/independent-spec-falsification-pair-01` | SHA-256 |
|---|---|
| `inputs.json` | `c70047edbcefa0f431a3831a567ee01c2d8d2614fe2c823fdd4ef1b6110b9d17` |
| `scoring-scope.json` | `986da757c9fa8f101e445a8031dd046f050ef2025003ea48385e8d774f3cf4a4` |
| `launch-real-01/audit.json` | `8163ca150f28d007ea223270e7b9d9902ec8fe72f7cf95644660102455e75609` |
| `launch-audit/mechanism-review.json` | `a8b58c511393c9dca7f43d28309c54d6dff39803c682e423b92641a2b2ebbd26` |
| `public-audit-real-01/public-boundary.json` | `890901a094e971984dc345a9c6c3ad75066e4f321ba3f171ec1704f9255ca05d` |

### Routing and coordination limits

A retrospective selection over the 27 AppWorld shared-instruction cohort
rows evaluated all 19,683 assignments of the three observed routes to the
nine tasks. Of these, 840 improved both the completed pass count and total
tokens. None reached the successful paired speed threshold. This calculation
uses the recorded results of each route and gives the selector advance
knowledge of all outcomes. It does not predict a policy's future results.

On the same successful pair sets, an idealized overlap of the recorded
manager and worker model intervals, with all other time removed, still left
the preferred arm 27.30 seconds slower and the shared arm 12.63 seconds
slower at the median. Keeping the recorded manager intervals, the preferred
arm would require about 82% less worker interval time to cross the speed
gate under these assumptions. The shared arm would still miss the 4-second
gate even if all worker model time vanished.

A separate scripted HTTP comparison held parallel dispatch, executor calls
and both stored artifacts fixed. Batching two independent single-worker
`wait_agents` calls in one response reduced planner requests from five to
four. Each result was collected under its actual worker generation. This
checks the request count and collection contract. Successful wall time and
provider token savings require real-model measurements.

| Private evidence within `worker-efficiency-input-audit-01` | SHA-256 |
|---|---|
| `v8-route-oracle-audit-01/audit.json` | `3738f432557cd70960b90aaabe2227ca7bee0a7a653fc63be612216f3c8cb0a0` |
| `v8-runtime-floor-audit-01/audit.json` | `2452e9e0dd92e40bc2e54cc2f9aa1c2650fdadc7867d8bdc5e39c4c49009ddf0` |
| `isolated-wait-batch-http-01/targeted.log` | `8842acfe06bdf364d76dcc615db8535415e3aa4093f98143f735c8396eaad8be` |

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

See [development observations dated 2026-11-05](OBSERVATIONS_20261105.md) for the separately scoped larger-budget logistics, Observatory, Drone and workbook results.

## Legacy f3 calibration: 2026-11-06

The six old-dev calibration assignments on the f3 source retained these official
outcomes. A is ordinary mode, B is Nuwa with the GLM-only catalog, and C is Nuwa
with the optional two-model catalog.

| Arm | Official success / assigned | Worker requests | DS requests |
|---|---:|---:|---:|
| A | 1/2 | 0 | 0 |
| B | 2/2 | 0 | 0 |
| C | 1/2 | 0 | 0 |

These are historical results under a policy that allowed manager execution.
The Nuwa attempts therefore do not satisfy the new mandatory separation of
planning and execution, and the cohort cannot support quality, efficiency or
budget conclusions about that new mode. Selecting no DS remains a valid model
choice; it supplies no evidence about DS execution support. The original
results and identities remain retained rather than being reclassified as a
successful separation test.

New source-boundary tests must inspect actual manager/worker model inputs and
execution. A newly identified ELF and image, followed by fresh calibration, are
required before accepting the changed mode. No such acceptance is recorded here.
The formal hard54 cohort has executed **zero rows**.
