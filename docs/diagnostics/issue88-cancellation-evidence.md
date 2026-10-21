# Issue #88：worker 模型调用取消来源

## 结论与范围

2026-10-21 只读核对 `vol14-a-229` 截至 14:01:48 已记录的最近六条
`context canceled` 模型调用：五条由引擎交接主动取消，一条由定时检查失败后的
worker 清理主动取消。六条均不能据错误字符串或耗时归类为 provider 超时。

本记录仅说明取消来源，不改变 worker 生命周期、定时完成判定、交接或重试契约。
它不包含任务正文、模型输入、工具参数、凭据或生产数据库副本。
时间均为 **Asia/Shanghai（UTC+08:00）**；证据读取于 2026-10-21。
代码因果按调查基线 `ba4beb6` 核对；该 SHA 不表示历史运行二进制已被证明来自此提交。

## 六条调用证据

`created_at` 是模型调用记录写入的时间，接近流结束时间；`duration_ms` 是该模型调用
的实测持续时间。调用可能跨多个 manager turn，不能把它当成某个 scheduled turn 的耗时。
事件 seq 均属于同一对话，只有与对话身份一起使用才有唯一含义。

| 模型调用 ID | 记录时间 | duration_ms | 触发事件与确认范围 |
|---|---|---:|---|
| 151585 | 12:51:16.282 | 418044 | `restart_preparing` 93739 → 检查点 93740、93747 → `restart_ready` 93748；93750 以原 worker ID 恢复。确认交接主动取消。 |
| 151593 | 12:52:42.095 | 19184 | `restart_preparing` 93755 → 检查点 93756、93758 → `restart_ready` 93759；93761 以原 ID 恢复。确认交接主动取消。 |
| 151645 | 13:02:50.048 | 386965 | `restart_preparing` 93785 → 检查点 93786、93793 → `restart_ready` 93794；93796 以原 ID 恢复。确认交接主动取消。 |
| 151704 | 13:09:39.136 | 366266 | `cleanup` 93820 停止 5 个运行中 worker；`error` 93821 为定时检查无执行证据，`finished` 93827 保留本 worker 的取消错误。确认失败清理主动取消。 |
| 151873 | 13:46:46.287 | 1334406 | `restart_preparing` 93927 → 检查点 93928、93934 → `restart_ready` 93935；93937 以原 ID 恢复。确认交接主动取消。 |
| 151959 | 14:01:48.007 | 891322 | `restart_preparing` 93990 → 检查点 93991、93992 → `restart_ready` 93993；93994 以原 ID 恢复。确认交接主动取消。 |

五组检查点中的该 worker 均为 `finished:false`、`timeout_failures:0`，且没有
`in_flight` 工具。交接保留的是待恢复状态，不能从取消记录推导任务完成或 watchdog 超时。
六条调用各自的完整时间区间内，均未发现该对话的 `user_message`、`steer` 或
`user_stop_requested` 事件；该事件检查不能识别是谁从进程外发起引擎替换。

调用记录与取消事件的 turn 归属如下；跨 turn 查证时须同时读取两者。

| 调用 ID | 模型调用归属 turn | 取消事件归属 turn |
|---|---|---|
| 151585 | `tn_dfa3708f9e7d8628` | `tn_b26be32a1d87d630` |
| 151593 | `tn_b26be32a1d87d630` | `tn_b26be32a1d87d630` |
| 151645 | `tn_b26be32a1d87d630` | `tn_b26be32a1d87d630` |
| 151704 | `tn_b26be32a1d87d630` | `tn_fef7e3bb4627808b` |
| 151873 | `tn_35f000e4a20b0b80` | `tn_41cdfcce518d77be` |
| 151959 | `tn_41cdfcce518d77be` | `tn_304ddd4251dfa1e5` |

## 代码因果与契约

### 引擎交接的五条取消

1. [HTTP 交接入口](../../internal/server/restart.go) 给交接保存阶段一个总截止时间。
2. [Engine.prepareRestart](../../internal/engine/restart.go) 先写
   `restart_preparing` 和确定性检查点，再取消 manager；为最终保存预留时间后，
   调用各 registry 的 `PrepareRestart`。
3. [Registry.PrepareRestart / Handle.stopForHandoff](../../handoff.go)
   在共享截止时间到达后，将未完成 worker 标记为交接取消并调用其 `cancel`。
   这对应本次五组中 preparing 后约 18 秒发生的模型流取消。
4. [Handle.completeWorker](../../worker_timeout.go) 保留 `ErrHandoff`；
   [worker 收尾](../../worker.go) 不将该状态发成任务 `finished`。
   最终检查点保留未完成状态，后续恢复继续使用原 worker ID。
5. [recordingModel.Stream](../../internal/provider/provider.go) 仍按原始流接收错误写入
   模型调用记录。因此单条 `llm_calls.err` 可显示 `context canceled`，不能单独表达业务原因。

这符合 [C-027](../CONTRACTS.md#c-027-automatic-bounded-restart-handoff)：
允许截止后取消尚未收尾的模型或工具，使用已保存检查点继续；交接不等于任务失败、完成或人类 Stop。

### 定时检查失败清理的一条取消

`tn_fef7e3bb4627808b` 没有工具结果或显式 `report_schedule`。
`model_retry` 93812 记录一次 `scheduled_no_activity` 原 turn 续办；续办后仍无执行证据。

代码链条为：

1. [runtime.runManager](../../internal/engine/iterations.go) 通过
   [scheduledWorkRecorded](../../internal/engine/schedule_completion.go) 检查执行证据，
   一次续办后返回 `scheduled check ended without executing a tool or reporting its result`。
2. [runtime.turnOutcome / runtime.shouldPark](../../internal/engine/goal_session.go)
   将其判为 `TurnError`，错误轮次不保留 parked registry。
3. [turn 收尾](../../internal/engine/turn.go) 调用 `Registry.Cleanup`；
   [Cleanup / Handle.Cancel](../../swarm.go) 主动取消仍在运行的 worker。
4. 模型记录先保存 `context canceled`；worker 后续 `finished` 带取消错误，原始证据保留在 Trace。

这符合 [C-009](../CONTRACTS.md#c-009-editable-configuration-and-scheduled-waits)
现有的“轮次错误或中断时清理”与定时完成证据规则。
若以后要求定时检查失败仍保留独立运行的 worker，须明确变更该生命周期契约并增加相关验收，
不能作为本次取消来源调查的隐含修复。

## 只读复核

通过**已有运行引擎**的 Trace 页面或 `GET /api/trace/:turn` 读取上表的两类 turn。
先在模型调用中按 worker ID、时间和 `duration_ms` 定位记录，再按同一对话的事件 seq 对照
交接检查点、恢复与 cleanup。检查点只需读取 worker 的身份、`finished`、
`timeout_failures` 及 `in_flight` 数量，不必复制其任务、历史或工具参数。
接口定义见 [API 文档](../API.md)。不要为复核启动、替换或停止生产引擎。

严格只读现场使用已有 HTTP 入口；CLI `trace` 的当前实现会执行 `config.Load` 和
`store.Open`，不能仅因命令名称是诊断就将它当作 SQLite 只读连接。
直接查询已有数据库时必须使用 SQLite `mode=ro` 并设置 `PRAGMA query_only=ON`，
只读取核对所需字段；本次调查未保存数据库副本。

## 证据限制

- 现存引擎启动日志没有历史 PID、替换请求发起者或唯一启动尝试身份，不能据此将替换归因于某个任务、用户操作或工作区。
- 最后一组的租约发布时间、目标 PID 启动时间和 `database_ready` / `local_handlers_ready`
  日志时间相互吻合，能佐证当时发生进程替换；这不能反推更早每次替换的发起者或源码 SHA。
- 一些 preparing 到后续恢复事件的间隔超过 30 秒；缺少同一启动尝试的完整记录，
  不能把跨失败、重试的观测间隔当成某次请求的预算验收结果。
- `context canceled` 是底层请求结果。判断 provider 超时、worker watchdog、人工停止、
  父轮次失败清理或引擎交接，必须结合调用时段、生命周期事件和检查点；仅靠错误文字或模型叙述不足。
