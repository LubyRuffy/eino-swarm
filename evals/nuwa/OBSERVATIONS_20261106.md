# Nuwa development calibration, 2026-11-06

This completed development calibration does **not** establish a Nuwa benefit.
The default arm passed both tasks; same-model Nuwa passed one, and automatic
Nuwa passed neither. Quality takes priority over efficiency. These observations
do not validate the requested GLM planner / DS executor comparison: all actual
model requests used GLM.

## Candidate and scope

The product source was `b2f7010c80713be189041b0cedddfc9fd59c2dd1`, with
mandatory planner/executor tool separation and a transient shared-state preview
at each model entry. The clean Linux ARM64 ELF had SHA-256
`345098a38d1656c4cf2e4fed388dae1512d44ef7e0a56c0551eb6db8636c8bfd`;
the actual agent image was
`sha256:bd583f8062f390dfd7ff72a688a271a3f86faf50e25ce4f81e3755caeb6d6985`.

The frozen schedule contained two already-exposed public AppWorld development
tasks, three arms, and one replicate: six assigned executions. Each task ran
default, same-model Nuwa, then automatic Nuwa. Task bytes, official grading,
order and budgets were unchanged during execution. All six assignments ran
once; none was replaced or regraded. Earlier source cohorts remain separate.
This small, fixed-order development observation is not a confirmatory heldout
result or a statistical framework ranking.

The initial model was `glm-5.3-flash` in every arm. Default and same-model Nuwa
offered only GLM; automatic Nuwa offered GLM and `dsv41-flash-1017`, leaving
selection to the model. Roles, subtasks and worker count remained AI decisions.
The common per-request output cap was 65,536, with explicit high reasoning for
the manager and its resolved workers, a 7,200-second task allowance and an
1,800-second post-turn review allowance. Existing auxiliary model settings
remained unchanged; auxiliary requests can use low or unspecified reasoning.

## Complete observations

| Public task | Arm | Official success | Task seconds | Reported total tokens | Model requests |
|---|---|---|---:|---:|---:|
| `6bdbc26_1` | `default-glm` | yes | 28.827931 | 545,540 | 41 |
| `6bdbc26_1` | `nuwa-glm-only` | yes | 17.827844 | 168,985 | 20 |
| `6bdbc26_1` | `nuwa-auto` | no | 37.556703 | 389,742 | 46 |
| `37a8675_1` | `default-glm` | yes | 17.779644 | 326,438 | 24 |
| `37a8675_1` | `nuwa-glm-only` | no | 46.477607 | 471,380 | 43 |
| `37a8675_1` | `nuwa-auto` | no | 72.595067 | 634,421 | 51 |

| Arm | Official success / assigned | Reported tokens, all attempts | Model requests |
|---|---:|---:|---:|
| `default-glm` | 2/2 | 871,978 | 65 |
| `nuwa-glm-only` | 1/2 | 640,365 | 63 |
| `nuwa-auto` | 0/2 | 1,024,163 | 97 |

All six harness executions and their cleanup completed. The gateway recorded
225 request starts and 225 matching ends with complete usage; 450 phase records
are not 450 requests. Complete reported token volume was 2,536,506, including
failed attempts and observed auxiliary/review requests. Cached prompt tokens
are included in prompt totals and must not be added again. No resource
censoring or output-limit truncation was recorded. Token volume is not billed
cost or equal compute across models.

`task_s` measures turn submission through conversation settlement. Startup,
fixture preparation, post-turn review, grading and cleanup have separate timing
scopes. The first same-model pair was correct in both arms and descriptively
favored Nuwa on time and tokens. The second pair did not preserve correctness;
the lower aggregate same-model token count therefore does not satisfy the joint
objective. A failed task cannot establish a successful speedup.

Actual Nuwa worker requests and their GLM bindings were observed. Automatic
Nuwa created three workers in each task, but selected DS in neither. The mode
checker reports source-policy conformance; it did not observe the root's live
tool schema or independently establish worker collection. Those unknowns are
not promoted to runtime proof. Official business outcomes remain unchanged.

## What this does not prove

This calibration measures task-local execution in fresh environments. It does
not test knowledge accumulation across tasks, later fact corrections,
cross-conversation reuse or process handover. Higher difficulty correctness,
persistent-project benefit and heterogeneous-model benefit remain unproved.
The existing heldout schedule has not been executed for this source. Native
desktop acceptance and a public installer for this source also remain pending.

Failure diagnosis is separate from scoring. Any later repair or diagnostic
replay must retain these original failures and use a separately identified
candidate; it cannot replace this cohort or retroactively count a missing
environment completion as success.

A read-only audit of the environment's API logs found a formal completion call
in all three passing executions and none in the three failures. In the failed
answer task, the reported answer matched the observed value; in the two failed
state-changing executions, all five business-state checks passed. Their official
completion/answer records still contained `<<not_given>>`, so the original
failures stand. Actual `wait_agents` receipts collected the matching worker
generations without timeout. Subsequent verification checked business values,
state or files but omitted the external completion obligation.

The retained Trace and gateway metadata do not contain actual worker request
messages. They cannot prove whether the full original project requirements
reached those workers or establish missing context as the cause. The audit's
SHA-256 is `91c93d33e816cd9edfdd3d7b8ad04ac989179be895e7ab617d6725bcc2b6d78e`.

## Evidence identity

Private raw records retain the six exact task identities, gateway phases,
official results, source guards and actual cleanup receipts. The completed
derived record `COMPLETED-CALIBRATION-OBSERVATION.json` has SHA-256
`0c58814912d4fec2ff220b528a98d4f03d4eb43a3c37bc31c54e68850cad65d5`.
Its frozen candidate SHA-256 is
`c78fba86083a2b54fb498fc501e9fc45313455fd56d7144bfadfd4256b6b4960`.
Routes, credentials, raw prompts and local runtime paths are not published.

This is a dated addition to the historical [results](RESULTS.md). It establishes
no CyberGym score and does not pool earlier development observations.
