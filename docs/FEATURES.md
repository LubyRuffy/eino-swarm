# Feature List

This is the current user-facing capability map. The code and tests named below are the authority for details; [README](../README.md), [API](API.md), [CLI](CLI.md), and [mobile README](../mobile/README.md) give usage.

- `F-000` zwai
  - `F-100` PC conversations
    - `F-110` Desktop, browser, and terminal shells
    - `F-120` Conversations and projects
    - `F-125` Unread completion attention
    - `F-130` Agent runs, tools, and live control
    - `F-135` Independent task coordination
    - `F-140` Files and attachments
    - `F-150` Goals and scheduled waits
    - `F-160` Trace and diagnostics
    - `F-170` Settings and providers
    - `F-180` PC text quoting
    - `F-190` Desktop updates
  - `F-200` Phone
    - `F-210` Pair a phone with a PC
    - `F-220` Read and control PC conversations
    - `F-221` Quote PC conversation text
    - `F-222` Inspect sub-agents on the phone
    - `F-223` Inspect local client tasks on the phone
    - `F-224` Browse and preview PC conversation files on the phone
    - `F-230` Direct model chat
    - `F-240` Phone updates
  - `F-300` Programmatic access
    - `F-310` HTTP API and live events
    - `F-320` CLI commands
    - `F-330` Swarm library
  - `F-400` Persistence and operations
    - `F-410` Local conversation storage and search
    - `F-420` Configuration and project memory
    - `F-430` Build and release paths

### F-110 Desktop, browser, and terminal shells

- 目的：从原生窗口、网页或终端使用同一引擎；使用者：PC 用户；入口：`zwai desktop`, `zwai web`, `zwai tui`；输入：本机配置和对话；输出：同源的对话和状态；前置条件：本机引擎可启动；失败表现：启动或连接错误；关联契约：`C-001`, `C-005`, `C-027`；实现证据：`cmd/zwai`, `internal/app`, `internal/desktop`, `frontend/src`。

- 自动交接重启：新版本／不同二进制启动时，在统一 30 秒启动预算内请求旧引擎交接、退出并启动新引擎；无需输入确认。manager 及所有活跃／后台 worker 保存检查点，模型摘要尽力完成，不以摘要标记任务完成；原 turn／worker 身份、历史、未读消息、超时预算及人类暂停保留。存储失败不确认安全退出；旧引擎缺少交接 API 时采用有界兼容恢复。关联契约：`C-001`、`C-027`、`C-012`；证据：`cmd/zwai/engine_restart.go`、`internal/engine/restart.go`、`handoff.go`、`cmd/zwai/engine_restart_acceptance_test.go`、`frontend/src/lib/transcript-restart.test.ts`。

### F-120 Conversations and projects

- 目的：创建、组织、搜索和继续对话；使用者：PC 用户；入口：侧边栏、项目、`/api/threads`；输入：项目和消息；输出：可恢复的对话；前置条件：本机数据库可写；失败表现：错误提示或不存在的对话；关联契约：`C-001`, `C-008`, `C-026`；实现证据：`internal/engine`, `internal/store`, `frontend/src/store/app.ts`。
- 项目工作目录：原生桌面新建／编辑项目时可用「选择目录」打开系统目录窗口，将单个现存目录的绝对路径填入字段；取消保留原值，选择失败在字段下显示错误。手动输入与留空使用 zwai 管理目录仍可用；浏览器提示桌面支持选择目录，不提供无法返回本机绝对路径的目录上传。保存仍经现有工作目录校验。实现／验证证据：`frontend/src/components/app/project-dialog.tsx`, `frontend/src/components/app/workdir-input.tsx`, `frontend/src/lib/workdir.ts`, `internal/desktop/directory.go`, `internal/desktop/directory_test.go`, `internal/engine/project_test.go`, `frontend/src/components/app/project-dialog.test.tsx`, `frontend/src/components/app/workdir-input.test.tsx`, `frontend/src/lib/workdir.test.ts`, `frontend/e2e/workdir.spec.ts`。
- 目录身份：非空目录路径不裁剪首尾空白；路径标准化仍使用 filepath.Clean。全空白保留托管目录行为。保存、编辑和实际运行解析必须指向同一所选目录；路径失效时拒绝并保留草稿，不回退到去空白后的同名目录。兼容变化：手动输入非空路径也按实际路径验证，不再猜测修正外围空白；已保存的规范路径无需迁移。证据：`internal/engine/project_workdir_identity_test.go`。

### F-125 Unread completion attention

- 手机当前状态：同一对话 ID 正在运行、等待或待答时，顶部完成未读列表暂不显示其旧结果；未读水位与已读回执保持不变，空闲后仍可显示。同名不同 ID 独立保留；搜索使用未过滤的 live 集合判定，非搜索视图中全部已加载结果暂隐藏时仍可继续 More。实现／验证：`mobile/src/components/home-screen.tsx`、`mobile/src/components/home-screen-unread.test.tsx`、`mobile/e2e/active-unread.spec.ts`；关联契约：`C-023`、`C-007`；来源：Issue #83。
- 兼容性：旧 PC 的 `open.detail.completed_seq` 缺失或为 `undefined` 时，手机跳过可选完成回执，正常对话及定时等待继续可用；现代 PC 的数值 `0` 仍允许首次可见终态回执。明确拒绝或网络失败保留未读并显示错误，不伪造成功；证据：`mobile/src/lib/completion-read-compatibility.test.tsx`、`mobile/e2e/unread-completion.spec.ts`；关联契约：`C-006`、`C-023`。

- 目的：让多任务完成后的结果持续可见，直到使用者实际查看；使用者：PC 与配对手机用户；入口：桌面任务前的静态状态灯、收起项目标题右侧的汇总状态、手机顶部完成未读列表；输入：主任务完成事件与已观察的 `completed_seq`；输出：独立于运行呼吸灯的完成提示，查看后在同一 PC 的客户端同步清除；前置条件：同一 host 的未归档对话；失败表现：读取回执失败保留提示并显示错误；关联契约：`C-023`、`C-003`、`C-006`、`C-007`；实现证据：`internal/store/completion.go`, `internal/server/completion.go`, `internal/remote/rpc.go`, `frontend/src/components/app/completion-reader.tsx`, `mobile/src/lib/completion-read.ts`, `mobile/src/lib/inbox-refresh.ts`；验证：`internal/store/completion_test.go`, `frontend/e2e/unread-completion.spec.ts`, `mobile/e2e/unread-completion.spec.ts`, `mobile/src/lib/inbox-refresh.test.tsx`。

### F-130 Agent runs, tools, and live control

- 自动重启的任务交接沿用 `F-110`／`C-027`：已接受的回答、继续／停止决定不会因交接被忽略；已确认终态不重复执行；完成 manager 下的后台 worker 直接按原 ID 恢复。证据：`internal/engine/restart_recovery_test.go`、`internal/engine/restart_parked_test.go`。
- PC 人工停止与清理：未完成的子代理显示在独立的 Stopped／已停止分组，停止后的刷新与重开仍保留该状态；不把资源清理当成成功完成，也不把人工停止显示为模型失败。真实完成的代理继续显示完成。关联契约：`C-002`、`C-003`；证据：`frontend/src/lib/transcript-stop.test.ts`、`frontend/src/components/app/panel-stop.test.tsx`、`frontend/e2e/restart.spec.ts`。

- PC 用户视图：连续工作区块只有一段思考或一个可见工具时，直接显示该条自身的折叠，一次点击即可查看内容；流式思考沿用默认展开与手动折叠。多项保留聚合，回答分隔工作区块。关联契约：`C-022`；实现证据：`frontend/src/components/app/work-fold.tsx`, `frontend/src/components/app/transcript-fold.test.tsx`, `frontend/e2e/work-fold.spec.ts`。

- 目的：运行 manager/sub-agent、查看工具并发送跟进或插入；使用者：PC 用户；入口：对话输入框和 Agent 面板；输入：消息、工具结果、排队消息的修改、未读 steer 的修改；输出：实时事件和最终回答；前置条件：配置可用 provider；失败表现：运行错误和 Trace；关联契约：`C-002`, `C-003`, `C-012`；实现证据：`internal/engine`, `internal/tools`, `frontend/src/components/app`。
- 停止后的排队行点击注入，作为下一轮发送；活动轮次仍注入当前回合。Stop 不自动清空其余队列。证据：`internal/engine/followup_test.go`, `frontend/e2e/conversation.spec.ts`。
- 极快完成的轮次在发送 ACK 晚于终态事件时仍保持空闲，迟到 ACK 不改变后来选择的对话；证据：`frontend/src/store/app-send.test.ts`, `frontend/e2e/api-error.spec.ts`。
- 轮次开始后响应区立即显示旋转的“工作中”，首个服务端进度脉冲到达后切换为计时；轮次结束即消失，不等待本地模型首字节。实现证据：`frontend/src/components/app/transcript.tsx`, `frontend/src/components/app/transcript.test.tsx`。
- 每个已结束轮次在桌面/Web 对话中都有横线及本地日期时间，持续目标折叠的轮次也保留该分界；进行中不提前标结束。实现证据：`frontend/src/components/app/transcript-session.tsx`, `frontend/src/components/app/transcript-session.test.tsx`, `frontend/e2e/schedules.spec.ts`。
- 代理名单：行上是 `resume_agent` 使用的 `agent_id`，不是 `#n`。进行中与已完成各自成组。组内默认按最后更新从新到旧；可改为创建时间或行上的名称，并在从新到旧 / 从旧到新（名称则是 A 到 Z / Z 到 A）之间切换。没有时间的行留在底部。实现证据：`frontend/src/lib/agent-label.ts`, `frontend/src/lib/agent-roster-sort.ts`, `frontend/src/components/app/panel.tsx`。
- 历史 worker 续接：manager 通过 System JSON 状态快照接收历史 worker 的 ID、角色及运行／完成／失败状态；已结束 worker 仍可查询或显式续办，不把历史名册重放为本轮新建调用。持续目标、定时检查、压缩、模型重试及重启使用同一状态语义。关联契约：`C-019`、`C-012`；实现／测试证据：`internal/workerstate/snapshot.go`、`internal/engine/autocompact_roster.go`、`internal/engine/resume_workers.go`、`internal/engine/worker_roster_test.go`、`frontend/e2e/worker-roster-history.spec.ts`。
- 排队与未读引导：工作中的 Enter 进入队列。点开一条排队消息，或点未读引导上的铅笔，原文进输入框（引用变成卡片），原来的那条先拿掉。再按 Enter，和新建一条排队一样，排到队尾。已读引导不会被拉进输入框。只有图片的未读引导没有可排队的文字，只能删除。
- 主动委派：共享运行时提示词要求 manager 在能提效或提高质量时主动调用 subagents，执行中重新评估；单个 worker 也可承担独立审查或第二种思路，不要求用户显式提出。普通对话与持续目标遵循相同策略；无收益时自行处理，遵守用户限制与并发上限，由 manager 核验和整合结果。关联契约：`C-019`；实现证据：`internal/engine/prompt.go`, `internal/engine/prompt_test.go`, `cmd/zwai/tui_prompt_test.go`。

- 无效委派恢复：同一模型回复内缺少 role/task 或参数不可读的 spawn 调用按错误类别合并为一条拒绝回执，保留数量和原始代表 ID；有效调用仍正常执行，manager 可补全参数在原轮次继续。拒绝不等于创建或取消 worker。关联契约：`C-019`；实现／测试证据：`spawn_batch.go`, `spawn_batch_test.go`, `tools.go`, `internal/engine/prompt.go`。

- 有界 worker 超时恢复：同一 worker 连续三次 watchdog 失败后停止 manager 的 resume／同角色 spawn，持续目标停止自动推进，失败周期计划暂停；成功或显式人类重试恢复预算。错误、worker ID 和次数保留在 Trace 与手机/桌面对话；普通 provider 超时和取消不计入。关联契约：`C-019`、`C-009`；实现／测试：`worker_timeout.go`、`internal/engine/worker_timeout.go`、`internal/engine/worker_timeout_test.go`、`frontend/e2e/worker-timeout.spec.ts`。

- 错误提示：模型 HTTP/流响应无法解析时，桌面和手机区分响应解码与真实工具参数错误，并保留同 turn Trace 和最多两次重试；证据：`internal/engine/api_error_integration_test.go`, `frontend/e2e/api-error.spec.ts`。
- 请求前工具参数检查：压缩 middleware 返回状态后检查当前尾部调用的非空 JSON 参数；无效参数在实际模型 HTTP 前拒绝，沿既有有界重试清理，不清除人类 steer 或伪造 worker 创建。不是全历史 JSON 扫描。关联契约：`C-012`；证据：`internal/engine/autocompact.go`、`internal/engine/model_retry.go`、`internal/engine/model_retry_http_test.go`。

### F-135 Independent task coordination

- 目的：让不同 PC 对话的管理 Agent 发现活跃任务、协商共享资源的处理顺序；使用者：PC 用户和配对手机用户；入口：manager 工具 `list_tasks`、`send_task_message`、`read_task_messages`，对话内的协作消息通知；输入：真实 conversation ID、消息和可选收件游标；输出：活跃任务的工作目录/请求/状态、持久化发送回执、来源可追溯的消息和明确回复；前置条件：同一 host 的未归档对话；失败表现：未知/归档目标、非法参数或数据库错误返回工具错误，不伪造送达；关联契约：`C-021`、`C-012`；实现证据：`internal/engine/task_coordination.go`、`internal/engine/task_coordination_flow_test.go`、`internal/store/event_kind.go`、`frontend/e2e/task-coordination.spec.ts`、`mobile/src/lib/task-message.test.ts`。
- 这是通信协商能力。发现结果不是资源锁，发送成功不是接收方已同意；双方明确回复后才可依赖分工或执行顺序。消息不会代替用户授权、自动启动空闲对话、解除暂停或打断另一个任务。

### F-140 Files and attachments

- 目的：让对话使用工作区文件和附件；使用者：PC 用户；入口：文件面板、上传及下载 API；输入：文件或图片；输出：文件、预览或模型视觉输入；前置条件：工作区和权限可用；失败表现：上传或工具错误；关联契约：`C-004`, `C-005`；实现证据：`internal/server`, `internal/tools`, `frontend/src/components/app`。
- 图片输入：png/jpeg/gif/webp 文件在空 MIME、通用 binary 或 UTI 标签下仍进入视觉输入；显式 text/PDF 保持文件附件。客户端只归一化 MIME 声明，不改原字节，服务端继续执行真实类型与声明匹配校验。关联契约：`C-028`；证据：`frontend/src/lib/paste-image.ts`、`frontend/src/lib/paste-image.test.ts`、`frontend/src/components/app/composer-image-drop.test.tsx`、`frontend/e2e/image-input.spec.ts`。

### F-150 Goals and scheduled waits

- 目的：持续目标及未来时间继续执行；使用者：PC 用户；入口：`/goal`、Scheduled 页、`schedule_wake`；输入：目标和时间；输出：状态、唤醒、运行结果；进行中或已暂停的任务可在编辑里改下次落点（每次新开对话，或某个未归档会话，列表可搜索），已完成的不能；已认领的那一轮留在原会话，安静归档看认领时的类型；打开会话时右侧列出仍指向该会话的唤醒，以及某次运行创建了该会话的任务，点一条打开该任务；有下一次已安排唤醒时，成功轮次中的未完成 sub-agent 保持原 ID 继续运行，下次轮次可接回；取消或暂停最后一次唤醒会停止无人接手的 worker；无工具结果或显式报告的定时检查原轮继续一次，仍无证据显示错误；周期检查的无计费重放内容按多语言文本和消息结构估算，在达到阈值时先自动压缩；若仍因上下文越界而终止，对话写出稳定原因并暂停该检查，用户调整后可恢复；进程重启后，若最近一次派出的 sub-agent 已因进程停止而失败，且会话停在未来的 thread wake 上，启动把这次唤醒提前到现在，并在 resume 这些 id 或新 spawn 之前拒绝 `schedule_wake` 和 `report_schedule` 的 `next_in_s`；前置条件：引擎运行；失败表现：跳过、失败或等待状态；关联契约：`C-002`, `C-003`, `C-009`；实现证据：`internal/engine/goal_session.go`, `internal/engine/autocompact.go`, `internal/engine/schedule.go`, `internal/engine/schedule_dest.go`, `internal/engine/schedule_context_overflow_test.go`, `internal/engine/context_overflow_schedule_test.go`, `internal/store/schedule_thread.go`, `frontend/e2e/schedules.spec.ts`, `frontend/src/components/app/conversation-schedules.tsx`。

- 目标编辑：历史 fire 类型冻结、目标与状态编辑及活动上限原子提交；已领取旧 wake 保持归属，移走最后 wake 后恢复原目标或收尾 worker。证据：`internal/engine/schedule_retarget_test.go`, `internal/store/schedule_retarget_test.go`。

- 完成范围：定时轮次自己的 manager thread wake 仍有后续触发，或本轮新排了这类等待时，`complete_goal` 返回可恢复冲突；完成该等待后才能完成长期目标。同轮先完成目标再重新排等待也返回冲突；人工和独立任务的等待保持独立。证据：`internal/engine/goal_schedule_completion_test.go`, `frontend/e2e/goal-schedule.spec.ts`；关联契约：`C-009`, `C-012`。
- 续轮 worker 身份和状态沿用 `F-130` 的历史状态快照，不把历史总数当成本轮创建量。关联契约：`C-019`；证据：`internal/engine/resume_workers.go`、`internal/engine/worker_roster_test.go`、`frontend/e2e/worker-roster-history.spec.ts`。

### F-160 Trace and diagnostics

- 目的：用 turn id 还原一次运行；使用者：开发者和用户；入口：`zwai trace`, `/api/trace/:turn`, Trace 面板；输入：turn id；输出：事件、模型和工具记录，桌面/手机公共错误保留模型 HTTP 状态及具体接口/解码原因，同 ID 可查看原始调用和重试；前置条件：该 turn 已记录；失败表现：找不到记录；关联契约：`C-012`；实现证据：`internal/engine/runerr.go`, `internal/engine/api_error_integration_test.go`, `internal/server`, `internal/store`, `frontend/src/components/app`, `mobile/e2e/api-error.spec.ts`。
- Worker 创建核验：按真实 turn 的调用、拒绝回执和生命周期事件区分新建、续办／恢复、完成及清理，不从历史名册或模型自述推断本轮创建量。证据：`internal/engine/worker_lifecycle_facts.go`、`internal/engine/worker_lifecycle_facts_test.go`、`frontend/e2e/worker-roster-history.spec.ts`。

### F-170 Settings and providers

- 目的：配置模型、外观、工具和远程连接；使用者：PC 用户；入口：Settings；输入：配置字段，包括每个提供商在输入框模型列表里的开关；输出：持久化设置和可用模型，关掉的提供商不出现在输入框和已配对手机的目录里，已经选中它的会话继续用；前置条件：本机数据目录；失败表现：校验或连接错误；关联契约：`C-006`, `C-009`；实现证据：`internal/config`, `internal/provider`, `frontend/src`。
- 输出额度用尽：桌面端这条错误下面有提示，链到设置 → 集群的「单次输出上限」，并聚焦该输入。关掉设置后，同一条错误上出现重试，按原来的请求再跑一轮。手机不能改这项，只显示错误原文。实现证据：`frontend/src/components/app/transcript-error.tsx`, `frontend/src/lib/output-budget.ts`。

### F-180 PC text quoting

- 目的：把选中的对话原文与提问分开提交；使用者：PC 用户；入口：选中 transcript 文本 → Add to chat；输入：选区和可选请求；输出：可编辑引用与带标签的消息；前置条件：选区在对话来源内；失败表现：无有效选区时不显示操作；关联契约：`C-011`；实现证据：`frontend/src/components/app/selection-menu.tsx`, `frontend/src/lib/quote.ts`, `frontend/src/components/app/selection-menu.test.tsx`。

### F-190 Desktop updates

- 目的：在 Mac 桌面应用里自动发现并确认安装新版本；使用者：Mac 桌面用户；入口：启动、每六小时、到期后的恢复前台／联网检查，以及菜单 Check for updates；输入：本仓库最近一个含同架构 `zwai-<version>-darwin-<arch>.zip` 的 Release；输出：确认后替换当前应用，以同一数据目录和离线模式打开新实例，关闭已登记的旧窗口，解压后 Finder 显示与 Dock 一致的 ZWAI 图标；前置条件：运行的是带数字版本号的 `zwai.app`；失败表现：自动检查保持安静，手动检查与安装显示可重试错误，启动命令失败恢复旧应用；关联契约：`C-010`, `C-024`；实现证据：`internal/update`, `frontend/src/lib/desktop-update.ts`, `frontend/src/components/app/desktop-update.tsx`, `frontend/e2e/desktop-update.spec.ts`, `internal/desktop/pack`, `internal/desktop/bundle_darwin.go`。

- 更新安装以应用包为互斥范围，跨不同数据目录的引擎/进程返回 busy；启动失败保留并恢复旧包，延迟清理仅处理该次安装独有的备份。证据：`internal/update`, `internal/update/bundle_lock_test.go`；契约：`C-024`。

### F-210 Pair a phone with a PC

- 目的：安全绑定手机和 PC；使用者：手机用户；入口：Settings → Phone QR、扫码或粘贴；输入：pairlink URI；输出：加密连接和设备列表；前置条件：hub 和 PC 在线；失败表现：网络、过期或 host offline；关联契约：`C-006`；实现证据：`internal/remote`, `mobile/src/components/scan-screen.tsx`, `mobile/src/lib/link.ts`。

### F-220 Read and control PC conversations

- 手机在 PC 对话或新建对话的模型选择器里主动刷新上游模型目录，显示进行中和可重试错误；成功后可选目录移除下线模型并加入新模型，但保留显式选择，不改写 PC 配置或默认模型。旧 PC 不支持时在选择器内提示更新。关联契约：`C-006`, `C-009`；实现证据：`internal/remote/catalog_refresh.go`, `internal/remote/pump_catalog.go`, `mobile/src/lib/use-model-catalog.ts`, `mobile/src/components/model-picker.tsx`；测试：`internal/remote/catalog_refresh_test.go`, `internal/remote/pump_catalog_test.go`, `mobile/src/lib/use-model-catalog.test.ts`, `mobile/src/components/model-picker.test.tsx`, `mobile/e2e/model-refresh.spec.ts`。
- 目的：在手机查看同一对话并发送、跟进、回答问题、插入及翻页；使用者：手机用户；入口：手机收件箱和对话页；输入：消息与操作，包括切换已绑定 PC、对 PC 的提问选择选项或填写 Other、把队列首条等待消息中断插入当前轮次；输出：切换 PC 后停留在该 PC 收件箱，进入对话后看到 PC 上同一对话的事件，提问回答经 PC 确认后继续，窄屏长输入仍可直接提交；当前轮次只有被隐藏的后台协调记录时说明空白原因，「更早」一次跨过连续隐藏记录到达可显示历史；前置条件：已绑定且在线；失败表现：重连提示或 RPC 错误，提问提交被拒绝时显示原因并保留草稿重试；关联契约：`C-002`, `C-003`, `C-006`, `C-015`, `C-016`, `C-017`；实现证据：`internal/remote/log.go`, `internal/remote/log_test.go`, `mobile/src/app.tsx`, `mobile/src/app.test.tsx`, `mobile/src/components/thread-screen.tsx`, `mobile/src/components/thread-screen.test.tsx`, `mobile/src/components/ask-card.tsx`, `mobile/src/lib/ask.ts`, `mobile/src/lib/phone-turn.ts`, `mobile/src/lib/phone-turn.test.ts`, `mobile/e2e/ask-answer.spec.ts`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/composer-layout.spec.ts`。
- 手机在每个已结束轮次后显示完成事件的本地日期时间和分界线；定时输入隐藏、失败轮次及历史翻页不丢掉边界。实现证据：`mobile/src/lib/transcript.ts`, `mobile/src/components/thread-blocks.tsx`, `mobile/src/lib/transcript.test.ts`, `mobile/src/components/thread-blocks.test.tsx`, `mobile/e2e/walkthrough.spec.ts`。
- iOS 从左屏幕边缘向右完成滑动，按当前可见详情／弹层逐层返回；根页面保持打开。手势沿用 Android 已有页面返回处理器，不依赖浏览器历史。证据：`mobile/ios/App/App/BridgeViewController.swift`, `mobile/e2e/ios-wait-layout.swift`；关联契约：`C-015`。
- 手机空输入框在真实运行时提供停止图标；输入文字、附件或引用后仍可跟进／插入，提问时回答、定时等待时发送。停止沿用 PC 的 interrupt 操作，不发送空消息。证据：`mobile/src/components/thread-stop.test.tsx`, `mobile/e2e/composer-layout.spec.ts`。

### F-221 Quote PC conversation text

- 目的：从手机对话选择原文并附在下一条消息；使用者：手机用户；入口：选中对话正文 → 选区附近的 Add to chat／加入对话，系统复制／全选菜单仍可用；输入：选区及可选请求；输出：可查看、编辑、移除引用，发送或插入后显示独立的引用块；前置条件：打开 PC 对话，选区在正文中；失败表现：无效选区不显示操作，发送失败恢复草稿；关联契约：`C-011`, `C-017`；实现证据：`mobile/src/components/thread-screen.tsx`, `mobile/src/components/composer.tsx`, `mobile/src/lib/quote.ts`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/composer-layout.spec.ts`。

### F-222 Inspect sub-agents on the phone

- 目的：在手机查看 PC 对话中的子 Agent 而不混入主 Agent 回答；使用者：已配对手机用户；入口：对话页 → 子 Agent／Agents → 子 Agent 行；输入：Pairlink 已允许的事件及更早历史页；输出：页头仅统计运行中的子 Agent，列表按进行中／已结束分组，保留完成与失败历史的角色、ID、状态、活动摘要及独立思考、工具、回答记录；页面返回、Android 系统返回或 iOS 左边缘右滑按详情→列表→对话逐级返回；列表在实时多段输出与历史分页后仍不显示主管正文或工作折叠；前置条件：PC 端有可回放的子 Agent 事件；失败表现：离线时沿用重连提示，未加载到启动事件前角色暂以 ID 显示；关联契约：`C-003`, `C-006`, `C-015`；实现证据：`mobile/src/lib/transcript.ts`, `mobile/src/lib/session.ts`, `mobile/src/components/thread-screen.tsx`, `mobile/src/components/thread-screen.test.tsx`, `mobile/src/lib/transcript.test.ts`, `mobile/src/lib/session.test.ts`, `mobile/e2e/walkthrough.spec.ts`, `mobile/src/components/agent-page.test.tsx`, `mobile/e2e/agent-roster.spec.ts`。

### F-223 Inspect local client tasks on the phone

- 目的：在已配对手机上只读查看 PC 的本地客户端任务；使用者：手机用户；入口：收件箱 → Clients → 工具分组和 More；输入：客户端任务页；输出：任务状态、记录及加载更多时的动效，任务详情覆盖手机视口且标题与返回按钮保持可用；前置条件：PC 已启用 Clients 且在线；失败表现：分页失败保留当前列表、显示错误并恢复 More；关联契约：`C-006`, `C-014`, `C-015`, `C-018`；实现证据：`mobile/src/lib/client-poll.ts`, `mobile/src/components/client-groups.tsx`, `mobile/src/app.test.tsx`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/composer-layout.spec.ts`, `mobile/e2e/ios-wait-layout.swift`。

- 主机 Clients 刷新复用未变化文件的解析行，按最新时间稳定排序一次；桌面和配对手机分页只复制可见行，追加、删除和文件替换在下一次刷新反映。实现／测试：`internal/clients/catalog_cache.go`、`internal/clients/catalog_cache_test.go`、`internal/clients/catalog_performance_test.go`、`frontend/e2e/clients.spec.ts`；关联契约：`C-014`。

### F-224 Browse and preview PC conversation files on the phone

- 目的：在已配对手机上查看 PC 对话的工作区产物；使用者：手机用户；入口：PC 对话页 → Outputs／产出物；输入：工作区列表、文件选择与搜索；输出：可折叠文件树、按后缀渲染的 Markdown／源码高亮／纯文本／常见图片／PDF 预览及其他文件的下载入口；前置条件：PC 在线且该对话存在；失败表现：RPC 错误、文件过大或不支持的预览明确显示；关联契约：`C-004`, `C-006`, `C-015`, `C-020`；实现证据：`internal/remote/files.go`, `internal/remote/files_test.go`, `mobile/src/components/file-browser.tsx`, `mobile/src/lib/remote-files.ts`, `mobile/src/lib/remote-files.test.ts`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/ios-wait-layout.swift`。
- 手机产出物初次加载与刷新后只显示第一层文件和目录；每层目录手动展开，搜索可直接找到折叠层级内的文件。证据：`mobile/src/components/file-browser.tsx`, `mobile/src/components/file-browser.test.tsx`, `mobile/e2e/walkthrough.spec.ts`。

- 手机文本产出物按后缀选择 Markdown 文档或现有源码语言高亮；未知文本与超过 256 Ki 字符的文本完整显示为纯文本。HTML/SVG、MDX 组件不执行，原文件下载保持不变。证据：`mobile/src/components/text-file-preview.tsx`, `mobile/src/components/text-file-preview.test.tsx`, `mobile/src/components/file-browser.test.tsx`。

### F-230 Direct model chat

- 目的：手机在没有 PC 时直接连接模型，并在应用内按服务商选择模型；使用者：手机用户；入口：Models、Chat 和输入框模型按钮；输入：兼容端点、所选模型、消息及附件，Models 里可把某个接口从输入框列表里关掉；输出：流式回答，长模型名和长输入不挤出发送按钮，关掉的接口不出现在列表里，已经选中它的对话继续用；前置条件：端点可访问；失败表现：连接或模型错误；关联契约：`C-002`, `C-006`, `C-009`, `C-013`, `C-015`, `C-017`；实现证据：`mobile/src/components/direct-chat-screen.tsx`, `mobile/src/components/model-picker.tsx`, `mobile/src/components/composer.test.tsx`, `mobile/e2e/composer-layout.spec.ts`, `mobile/src/lib/openai-client.ts`。
- 直连流式回复期间输入框主按钮显示可点击停止图标，取消后保留已收到的回复并恢复发送。证据：`mobile/src/components/direct-chat-screen.tsx`, `mobile/src/components/direct-chat-screen.test.tsx`。

### F-240 Phone updates

- 目的：发现并安装 Android 可用版本；使用者：Android 手机用户；入口：菜单 Check for updates 和更新提示；输入：最近一个含匹配 APK 的 GitHub Release；输出：Android 安装请求；前置条件：网络和新版本；失败表现：手动检查显示错误。iOS 测试版由 TestFlight 分发和更新，应用内不显示 GitHub 检查入口；关联契约：`C-010`；实现证据：`mobile/src/components/host-chrome.tsx`, `mobile/src/components/update-notice.tsx`, `mobile/src/lib/app-update.ts`, `mobile/e2e/version-check.spec.ts`, `mobile/ios/App/AppUITests/BindFlowTests.swift`。

### F-310 HTTP API and live events

- 目的：给 PC 客户端提供相同服务；使用者：内置客户端及本机调用方；入口：`/api`、SSE、WebSocket；输入：同源请求；输出：状态和事件；前置条件：引擎在线；失败表现：HTTP 状态和 code；关联契约：`C-002`, `C-003`, `C-005`；实现证据：`internal/server`, `docs/API.md`。

### F-320 CLI commands

- 目的：从 shell 启动、诊断和管理对话；使用者：终端用户；入口：`zwai` 子命令；输入：命令与参数；输出：终端文本或客户端；前置条件：可运行二进制；失败表现：命令错误；关联契约：`C-001`, `C-012`；实现证据：`cmd/zwai`, `docs/CLI.md`。

### F-330 Swarm library

- 进程交接 API 与恢复边界见 `F-110`、`C-027`；库使用说明：`docs/LIBRARY.md`；证据：`handoff.go`、`handoff_test.go`。

- 目的：让 Go 程序嵌入 manager 与 sub-agent；使用者：Go 开发者；入口：模块公开 API；输入：registry、provider 和任务；输出：结果与通知；前置条件：依赖可编译；失败表现：Go 错误或结构化工具错误；关联契约：`C-003`；实现证据：`swarm.go`, `run.go`, `docs/LIBRARY.md`, `examples`。

### F-410 Local conversation storage and search

- 目的：持久保存对话、事件与检索索引；使用者：PC 用户；入口：对话列表和搜索；输入：事件与查询；输出：可恢复历史和结果；前置条件：数据目录可写；失败表现：存储或索引错误；关联契约：`C-003`, `C-008`；实现证据：`internal/store`, `internal/search`, `docs/DATA_MODEL.md`。

### F-420 Configuration and project memory

- 目的：保留本机设置、项目说明和技能；使用者：PC 用户；入口：Settings、Memory、技能管理；输入：配置及项目文件；输出：后续运行可用的上下文；前置条件：本机数据目录；失败表现：保存或解析错误；关联契约：`C-009`、`C-012`、`C-025`；实现证据：`internal/config`, `internal/memory`, `docs/CONFIG.md`。
- 会话摘要与成功回合记忆复核接收从完整历史重建、按 turn 归属的持久化 worker 生命周期事实；摘要预算与落库优先保留事实，复核先从源总 rune 预算预留完整事实块并独立前置，再选择事件来源，不把事实或派生摘要当作可引用写入来源。失败回合 human-only 不接收该前置块；旧摘要和模型自述只作待核对叙述，冲突创建量不能提升为执行证据。旧笔记／技能仍可在 Memory 中人工编辑，系统不按用户业务关键词清洗。关联契约：`C-008`、`C-012`；实现／测试证据：`internal/engine/worker_lifecycle_facts.go`、`internal/engine/worker_lifecycle_facts_test.go`、`internal/engine/session_memory.go`、`internal/engine/review_source.go`。
- 压缩提示预览只显示摘要正文；生命周期证据继续保存在内部摘要、模型上下文与诊断中。关联契约：`C-008`；证据：`internal/engine/compact.go`、`internal/engine/autocompact.go`、`internal/engine/compact_evidence_test.go`、`internal/server/compact_evidence_test.go`。
- 技能可以复制到另一个项目，得到的是独立文件。来源以后的修改要在副本上点更新才会过来；副本改过则不会被静默覆盖，也不会写回来源。没改过的副本仍跟踪最初的项目。入口：Memory 里每个技能的复制，以及多于一个技能时的复制全部。实现证据：`internal/memory/skill_copy.go`, `internal/server/skill_copy.go`, `frontend/src/components/app/skill-copy-dialog.tsx`, `frontend/e2e/skill-copy.spec.ts`。
- 不在项目里的会话没有笔记。回合正常结束后，可复用流程写入数据目录下的共享技能库（数据目录中的 library/skills/）。之后的纯对话会带上这份索引，并用 `skill_view` 打开；当场不能改库。Memory 页可以整理目录，也可以把技能复制进某个项目；副本的更新从技能库拉取。副本改过、两边都改过、或来源已删，文案按这个状态说，不把来源是技能库当成唯一信息。点项目文件夹不会把这份列表换成那个项目的笔记。总开关 `memory.enabled` 关掉后，提示词里不再带上，也不再写入。实现证据：`internal/engine/library_test.go`, `internal/memory/library.go`, `internal/server/library.go`, `frontend/e2e/library-skills.spec.ts`。

- Settings → Memory 的 Review thinking 可独立指定回合复盘推理档位，默认跟随会话；只影响自动复盘和 Review now。入口证据：`frontend/src/components/app/memory-settings.tsx`、`frontend/e2e/settings.spec.ts`、`internal/config/memory.go`、`internal/engine/review_profile_test.go`；关联契约 `C-025`。
- 来源身份 framing 超过单源 rune 上限时整组省略，配对工具不拆开，被省略正文不能作为写入证据。技能提炼指令区分常规路径与已发生故障的独立恢复分支，明确有证据的进入、恢复及回归/结束条件，不要求重演失败探测。该指令仍须效果评测验证。关联契约：`C-025`；证据：`internal/engine/review_source_select_test.go`、`internal/memory/prompt_test.go`。
- 自动复盘不再因 manager 曾写入一项记忆而跳过整轮；成功回合复盘完整有效来源，失败项目回合只保留真实人类长期要求，不提炼技能、工具结果或派生摘要；取消不触发自动复盘。当前 notes 在项目复盘锁内、模型调用前读取；源事件优先保留人类纠正，再选择近期完整工具配对，按 rune 限额包含来源 framing；现有 notes、技能目录及系统指令另计。入口：正常/失败回合后的自动复盘、Memory 的 Review now（最近成功回合）；实现证据：`internal/engine/review.go`、`internal/engine/review_human.go`、`internal/engine/review_source_select.go`、`internal/engine/review_quality_test.go`、`internal/engine/review_source_select_test.go`。
- 自动 reviewer 用一次完整批次提交 notes/skills 变更；notes 在内存草稿复用既有校验，最终按首次模型输入的同一 notes revision 一次提交，普通拒绝后不重基；思考期间的并发编辑终止本轮，不追加修复请求，失败或冲突不半写；skills 仍即时落盘，后续失败也记录真实技能变化，不声称跨文件事务。每项变更引用源 seq 与原文，notes 的新增数字须出现在引文或被替换旧条目中；这是来源/拼写约束，不证明语义真值。成功后程序收尾，不再请求模型写结束语；拒绝可在原迭代上限内修复，同 turn Trace 保留可选 evidence/refused。关联契约：`C-025`、`C-012`；实现证据：`internal/engine/review_batch.go`、`internal/engine/review_evidence.go`、`internal/memory/notes_draft.go`、`internal/engine/review_batch_test.go`、`internal/engine/review_evidence_test.go`、`internal/memory/notes_draft_test.go`。

- 失败时 transcript 与 Memory 面板显示停止原因、提案次数、拒绝类别及可用容量数字，区分未提交 notes 与已落盘 skills；Trace 记录有界 attempts 安全元数据，不保存被拒提案正文、原文引句或旧条目。容量反馈向模型保留 usage/over_by/current_entries/matched，原容量、来源、数字、revision 校验不变，只能在原预算内先有依据地缩短或替换相关条目再新增，不能清理无关记忆；无法安全提交时明确失败，旧事件缺新增字段仍兼容。失败 Trace 按钮会打开侧栏并加载原回合，历史聚焦不冒用最新 token 用量；切换后返回原会话时恢复手动复盘结果，结果落在已加载 tail 之外时只读一次已知 turn 的 Trace，并校验点击代次、线程、回合和事件起点，不轮询。入口：回合自动复盘、Memory 的 Review now、同 turn Trace；关联契约：`C-025`、`C-012`；实现证据：`internal/engine/review_diagnostics.go`、`internal/engine/review_diagnostics_test.go`、`internal/engine/review_finalize_test.go`、`internal/server/review_diagnostics_test.go`、`frontend/src/lib/review-diagnostics.ts`、`frontend/src/lib/review-diagnostics.test.ts`、`frontend/e2e/memory-review.spec.ts`、`frontend/src/lib/transcript-review.ts`。

### F-430 Build and release paths

- iOS 发布预检读取应用源码中经授权的持续合规声明；加密用途不变的普通更新无需重复问答，签名 IPA 及精确回执发生冲突时拒绝发布。无源码字段的旧批次仍读取私有精确确认。证据：`mobile/ios/App/App/Info.plist`, `tools/release_ios.py`, `tools/test_release_ios_compliance.py`, `docs/RELEASE.md`。
- Mac 原生窗口验收后，发布恢复核对私有批次回执的版本、源码 SHA、文件名与 SHA-256，上传同一 ZIP；文件缺失或变化时保留待发布状态。证据：`tools/release.py`, `tools/test_release.py`, `docs/RELEASE.md`。
- Mac 原生验收恢复的离线入口 `python3 tools/audit_macos_acceptance.py --observation <private.json>` 接收无凭据的来宾状态、登录结果与精确候选窗口回执，输出待解锁、待窗口验收、真实登录阻塞或同包验收通过。恢复待验收工作时先以 `guest.state=unknown` 重新观察来宾；unknown 优先于残留的旧登录成功／失败结果，要求重新观察，不直接阻塞或跳到窗口检查。宿主锁屏不阻断就绪来宾，已完成的同包真实验收不因后续锁屏失效，SSH/进程成功不算窗口验收；工具不执行登录、SSH 或发布，也不替代真实证据检查。证据：`tools/audit_macos_acceptance.py`, `tools/test_audit_macos_acceptance.py`, `docs/RELEASE.md`。
- 同一处理批次先收口各 worktree 的 Issue 主线集成，再为最终完整主线分配一个版本；新版本发布入口核对批次收口回执的源码、Issue 和平台，并在仍有活跃 Issue 时拒绝上传。证据：`AGENTS.md`, `tools/release.py`, `tools/test_release.py`, `docs/RELEASE.md`。
- 目的：从一个已验收、已分配版本的完整主线批次发布实际受影响的 macOS、Android 和 iOS 端；使用者：维护者；入口：`make release`, `make release-check`, `make mobile-ios-release`, `python3 tools/audit_pending_batch.py`；输入：干净源码、统一版本/SHA、签名配置（可选 `marketing_version` 显式开启新 TestFlight 版本）、测试组与源码中的授权应用合规声明（无字段的旧源码使用按 SHA/build 持久保留的精确确认）、交付台账及经验证的额外主线平台变更；输出：所需平台的 GitHub macOS zip/APK、TestFlight 构建与组/审核状态（常规更新沿用最近成功版本，仅增加 build number；已有候选冻结版本；同版本仍可能审核）、逐 Issue 交付结果，以及未分配变更的提交数、等待时长和发布平台；旧版仍待交付时保留冻结批次并独立恢复；前置条件：Go、Node、SDK、gh、私有 iOS CLI/签名资料，主线自上个公开版本超过三次提交或最早待发布修复等待超过 24 小时；失败表现：各端独立报错或等待，未交付时退出非零；关联契约：`C-010`；实现证据：`Makefile`, `tools/release.py`, `tools/release_ios.py`, `tools/test_release.py`, `tools/test_release_ios_versions.py`, `tools/audit_pending_batch.py`, `tools/test_audit_pending_batch.py`, `internal/release`, `internal/desktop/pack`, `mobile/scripts/android-release.ts`, `docs/RELEASE.md`。
