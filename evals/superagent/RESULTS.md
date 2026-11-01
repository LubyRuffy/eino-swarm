# Monitoring decision trial: retain current behavior

The additional low-interruption reminder did not clear the adoption gate.
Product prompts, scheduling, workers and notification behavior remain unchanged.

The current engine ran 52 real-model rounds on `glm-5.3-flash`: thirteen frozen
observation states, two repetitions in each arm. Requirements came from two
previously executed monitoring tasks; the observations were explicitly
constructed, sanitized decision fixtures. A retained the existing task quiet
policy. B added only the generic triage reminder. This was not a production
operation replay or an evaluation of the complete upstream architecture.

| Measurement | A: current instruction | B: additional triage reminder |
| --- | ---: | ---: |
| Rounds | 26 | 26 |
| Frozen automatic checks passed | 23 | 24 |
| Independent semantic reviews passed | 19 | 19 |
| Missed critical notification decisions | 0 | 1 |
| Missed completion notification decisions | 2 | 0 |
| Routine progress in a supposedly quiet visible answer | 4 | 4 |
| All-caller tokens | 951,872 | 1,093,476 |
| Median foreground turn duration | 8.56 s | 10.26 s |
| Harness failures / incomplete cost records | 0 / 0 | 0 / 0 |

B used **14.9% more tokens**. The prespecified paired bootstrap first averages
the two repetitions per case, then resamples the thirteen paired cases 10,000
times with seed `20261101`. Its conditional 95% interval for token increase is
**3.6% to 28.1%**. Cases share two source task families, so this interval is a
description of this constructed collection, not a population estimate across
independent real tasks. Time medians are descriptive; no latency interval or
first-useful-result improvement was established. Tokens include manager,
title-namer, schedule-answer-policy, memory-reviewer and session-memory calls;
they are not a monetary cost estimate.

The critical counterexample is `deployment-02`, B repetition 2. Its evidence
shows 35 minutes without progress against a 30-minute task threshold. The model
correctly chose `investigate`, but wrote `decision: quiet` and an empty
notification. Its explanation treated the absence of a confirmed failure or
needed human action as a reason to stay quiet. That contradicts this task's
explicit requirement to notify when further diagnosis becomes necessary.
It is a decision-file miss; actual production notifications were not exercised.

Higher automatic pass counts alone would conceal that regression. Both arms
also selected `escalate` once where the frozen `refresh-03` action label was
`verify`: explicit negative freshness/closure readbacks admit a reasonable
interpretation as a failed completion condition. Those automatic failures stay
in the results, while independent review records the action ambiguity separately
from unsupported facts or missed alerts. No oracle was changed after outputs.

Independent review found one unsupported data-date assertion in each arm, one
constructed-to-production provenance overclaim in B, and routine visible-answer
status reports in both arms despite quiet decision files. There were no observed
external operations, invented evidence IDs or unsupported successful completion.
Neither arm produced excess notification text in the decision file, leaving no
baseline excess to reduce. File fields cannot prove real quiet/unread behavior.

All 52 planned identities completed and were independently reviewed. Actual
model tools were `read` (62 calls) and `write` (52 calls); every case read its
complete fixture before writing its decision. Every owned engine exited normally
and its private data was removed. Final call records were read from each exited
engine's private database, including calls arriving after the foreground Trace.
No memory review was cut off. Persisted cache zeros remain distinct from proof
of an upstream explicit zero.

The [sanitized aggregate](results.json) records per-round outcomes, hashes,
caller totals and review flags. Complete answers, traces, original source
mapping, frozen plans, analysis and independent review evidence remain in the
private experiment directory outside the checkout. The engine source is
`17e555f12ab5f5bb8a72f2e132e14cd4314011b5`; binary and instrument digests are in
the aggregate. The runner and oracle stayed unchanged throughout the primary
measurement. Analysis implementation review closed incomplete-cost, cleanup,
zero-denominator and duplicate-identity acceptance paths without relaxing the
predeclared gate.

Keep the current product behavior. This result rejects this particular reminder
as an improvement under the tested conditions; it does not establish that the
upstream routing, subscription or recall designs are ineffective. Those already
overlap substantially with current capabilities, and would require a separate
demonstrated bottleneck and controlled task comparison before adoption.
