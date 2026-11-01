# Super-agent applicability

The design is relevant to reducing coordination and attention costs. Adding a
second supervisor is not an established improvement for this repository.

The [completed 52-round trial](RESULTS.md) did not establish an improvement:
the candidate consumed more tokens and missed one critical notification
decision. The product implementation remains unchanged.

The [referenced article](https://zhuanlan.zhihu.com/p/2099987320443237060)
was not directly retrievable during this investigation. Its
[official companion repository](https://github.com/deepseek-ai/dsh-superagent-prototype/tree/6337fc823ce20251c8de8085948165c81aaa701e)
was inspected at commit `6337fc823ce20251c8de8085948165c81aaa701e`.
The upstream README explicitly calls it a research prototype and distinguishes
the published implementation from its internal system and the article. Public
design directions must not be described as working features merely because
they appear in the article.

| Mechanism | Existing eino-swarm boundary | Assessment |
| --- | --- | --- |
| Supervisor and persistent workers | Per-conversation manager, worker generation ownership and parked registries; `internal/engine/worker_ownership.go` | Existing capability; another router needs separate evidence. |
| Event delivery and background checks | `internal/engine/schedule.go`, `schedule_tool.go`, and persisted wake permission | Existing capability; events cannot grant new authority or revoke a human prohibition. |
| Cross-session context | Project-bound original records through `internal/engine/history_tool.go` | The upstream subscription and recall design is useful context, but does not prove a replacement is better. |
| Useful results with less interruption | Current `report_schedule` accepts empty findings; `prompt.go` already documents quiet checks | Suitable for a narrow, controlled decision trial without changing lifecycle protocols. |

The upstream [philosophy](https://github.com/deepseek-ai/dsh-superagent-prototype/blob/6337fc823ce20251c8de8085948165c81aaa701e/docs/000-philosophy.md)
asks the agent to act within written authority and communicate useful results or
requests for action. Its [architecture](https://github.com/deepseek-ai/dsh-superagent-prototype/blob/6337fc823ce20251c8de8085948165c81aaa701e/docs/007-architecture.md)
keeps ordinary progress on a work item and forwards questions promptly. These
are concrete mechanisms worth comparing with current behavior. They do not
establish an improvement in our model quality, task completion or user attention.

The [trial fixture](README.md) retains two previously executed monitoring tasks'
requirements and constructs thirteen explicit observation states. Both arms use
the same current engine, evidence and output contract. A retains the task's
existing quiet policy; B adds a generic event-triage reminder. The observations
and expected decisions are frozen before model calls. No production operation
is replayed, and this trial cannot accept the actual scheduled quiet/unread
notification boundary.

The adoption gate requires no quality regression, no missed critical notice,
unsupported completion or invented evidence, plus at least a 20% reduction in
all-caller tokens or excess notification decisions with a paired, case-level
95% interval excluding no improvement. Missing costs do not count as zero.
Repetitions remain nested within thirteen cases from two task families.
Even a passing decision trial still requires the affected actual scheduler
boundary before product adoption. Without a passing measurement, existing
product behavior stays in place.
