# Feature List

This is the current user-facing capability map. The code and tests named below are the authority for details; [README](../README.md), [API](API.md), [CLI](CLI.md), and [mobile README](../mobile/README.md) give usage.

- `F-000` zwai
  - `F-100` PC conversations
    - `F-110` Desktop, browser, and terminal shells
    - `F-115` Bounded native renderer recovery
    - `F-120` Conversations and projects
    - `F-125` Unread completion attention
    - `F-130` Agent runs, tools, and live control
    - `F-135` Independent task coordination
    - `F-140` Files and attachments
    - `F-145` Durable composer drafts
    - `F-150` Goals and scheduled waits
    - `F-160` Trace and diagnostics
    - `F-170` Settings and providers
    - `F-175` Provider cloud built-in tools
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

- PC 恢复交互：恢复时显式禁用目标编辑／清除／开始，包括已聚焦编辑框的键盘和失焦保存；目标编辑只在成功 ACK 后关闭，拒绝保留本会话输入，就绪不自动重发，导航后旧 ACK 不影响新会话。页面 reload 按现有首页契约重选原对话后保留其已存草稿并继续显示恢复进度。关联契约：`C-001`；证据：`frontend/e2e/engine-startup.spec.ts`、`frontend/e2e/goal-restoring.spec.ts`、`frontend/src/components/app/goal-banner-restoring.test.tsx`、`frontend/src/components/app/composer-goal-restoring.test.tsx`。

- 启动恢复：数据库和配置可用即发布可连接的 `restoring` 引擎，PC／手机显示恢复进度，成功后自动移除；running／parked 部分失败统一显示数量并保留原 turn 失败诊断，其余工作继续可用；创建日程只有成功 ACK 后关闭，恢复拒绝保留输入；历史、Trace、事件流、草稿、在线登记和 Stop 可用，新工作及日程变更等待 `ready`。ACK 后的替换进程不会被原交接截止时间杀掉；冷等待到期仍活着的本次 child 即使尚未 Acquire 也保留，并以 PID／日志和归属未确认错误报告，不能把竞争者的锁当成自己的；人工取消只回收本次子进程。每对话共用一次恢复快照，包括 manager 纠正预算；保留预算消耗与人工续行回执，新的人类 Continue 读取新回执。原 turn Trace 包含 `engine_restore` 耗时与扫描量。关联契约：`C-001`、`C-027`、`C-012`；实现／测试：`internal/app/startup.go`、`internal/engine/restore.go`、`internal/server/startup_test.go`、`internal/remote/startup_test.go`、`cmd/zwai/engine_restore_process_test.go`。


- 目的：从原生窗口、网页或终端使用同一引擎；使用者：PC 用户；入口：`zwai desktop`, `zwai web`, `zwai tui`；输入：本机配置和对话；输出：同源的对话和状态；前置条件：本机引擎可启动；失败表现：启动或连接错误；关联契约：`C-001`, `C-005`, `C-027`；实现证据：`cmd/zwai`, `internal/app`, `internal/desktop`, `frontend/src`。

- 冷启动就绪：完成当前 checkout 的前端准备后，允许本地初始化最多 2 分钟，每 10 秒报告启动进度；手机 hub 注册不阻塞本地窗口。探测期间取消不得因迟到响应而继续附着或丢失取消类别，失败保留本次子进程退出原因及本次追加日志，取消／普通冷启动超时仅清理自己的启动子进程；不可回退交接后的冷等待截止遵守上述保留规则，保留竞争引擎。替换已有引擎仍遵守准备后的统一 30 秒交接预算。关联契约：`C-001`、`C-006`、`C-027`；实现／验证证据：`cmd/zwai/engine_boot.go`、`cmd/zwai/engine_boot_test.go`、`internal/app/app.go`、`internal/app/startup_remote_test.go`。
- 前端准备在引擎发现／交接前执行，准备失败或取消时保留仍在服务的旧引擎，不消耗其 30 秒交接／新监听预算；后续进程仍须完成自身启动校验。关联契约：`C-001`、`C-027`；证据：`cmd/zwai/engine.go`、`cmd/zwai/engine_frontend_test.go`。
- checkout 前端需要重建时，检查 manifest 直接依赖、开发依赖及命令入口；空／部分 `node_modules` 即使时间戳较新也先用 `npm install --include=dev` 修复并复验。安装／构建／stamp 按 checkout 跨进程串行，等待可取消；已有新鲜 bundle／发布 embed 不新增 Node 要求。关联契约：`C-001`、`C-010`；证据：`frontend/ensure.go`、`frontend/ensure_dependencies_test.go`、`frontend/ensure_build_lock.go`、`frontend/ensure_build_lock_test.go`。
- 自有启动子进程发布租约后，单次元数据探测暂时不可达时继续在原总预算内等待；元数据成功校验后才附着。竞争引擎不可达或模式冲突仍立即报错并保留其进程。关联契约：`C-001`；证据：`internal/lease/lease_test.go`、`cmd/zwai/engine_boot_test.go`。

- 自动交接重启：新版本／不同二进制启动时，在前端准备后的统一 30 秒预算内请求旧引擎交接、退出并启动新引擎；无需输入确认。manager 及所有活跃／后台 worker 保存检查点，模型摘要尽力完成，不以摘要标记任务完成；原 turn／worker 身份、历史、未读消息、超时预算及人类暂停保留。存储失败不确认安全退出，交接后保留冻结及检查点供修复后重试，存储可读时仍可查看历史／Trace／failed 状态，不自动重跑未知操作；旧引擎缺少交接 API 时采用有界兼容恢复。关联契约：`C-001`、`C-027`、`C-012`；证据：`cmd/zwai/engine_restart.go`、`internal/engine/restart.go`、`handoff.go`、`cmd/zwai/engine_restart_acceptance_test.go`、`internal/server/restart_failure_retry_test.go`、`frontend/src/lib/transcript-restart.test.ts`。

- 桌面重开：native 进程在窗口加载前持有真实 presence，页面 reload 不释放连接，也不重复登记窗口；撞上 idle 停机时有界等待原持有人退出或租约身份改变，再完整核验后重新连接；新 PID 复用原 URL 仍识别为替换，不生成第二个数据库写入者；最终 idle 复核同时保留活动 manager、后台 worker 和交接中的执行归属。证据：`cmd/zwai/desktop_attach_test.go`、`cmd/zwai/desktop_attach_errors_test.go`、`cmd/zwai/desktop_attach_identity_test.go`、`frontend/e2e/presence.spec.ts`；契约：`C-001`。
- 桌面连接恢复：已确认的 native presence 断流后，在同一地址有界重新登记并确认；短流不无限重置预算，稳定连接后可恢复后续独立断流。关闭应用中断恢复，耗尽则先通过桌面 logger 同步记录原因再请求原生退出；原生系统终止不保证返回 Go，能返回的平台保留原有错误类别，启动前失败直接返回且不开窗口，正常关闭不误报终止错误。证据：`cmd/zwai/desktop_presence.go`、`cmd/zwai/desktop_attach_stream_recovery_test.go`、`cmd/zwai/desktop_presence_test.go`、`internal/desktop/lifecycle_test.go`、`internal/desktop/lifecycle_terminal_test.go`；契约：`C-001`。

### F-115 Bounded native renderer recovery

- 故障诊断：重载／替换前向原异步诊断通道发出最后探针边界和已知年龄，区分回调缺失、位置待恢复、截图待回、截图错误及像素不匹配；继任错误保留启动前／attach／ready 后提交前阶段和安全 lease HTTP 分类，不记录原生错误正文、URL 或响应内容。该分类不等于已定位 GPU／JS 根因。证据：`internal/desktop/recovery_diagnostic_runtime_test.go`、`internal/lease/discover_diagnostics_test.go`、`cmd/zwai/desktop_recovery_phase_diagnostics_test.go`；契约：`C-034`、`C-001`、`C-012`。
- 目的：在 macOS WebContent 活挂、绘图失效或白屏时恢复可用窗口；使用者：桌面用户；入口：`zwai desktop` 的原生恢复控制循环；输入：同源、主 frame、当前 challenge 响应与实际截图像素；输出：一次重载，必要时附着原引擎的继任窗口；前置条件：引擎租约可验证、恢复副本与诊断目录可写；失败表现：有界尝试耗尽后的原生错误提示及保留诊断；关联契约：`C-034`, `C-001`, `C-012`, `C-033`；实现证据：`internal/desktop/recovery.go`, `internal/desktop/recovery_state.go`, `internal/desktop/recovery_snapshot_darwin.m`, `cmd/zwai/desktop_recovery_darwin.go`, `internal/desktop/recovery_test.go`, `cmd/zwai/desktop_recovery_test.go`。
- 健康条件包含本次随机探针的真实像素；HTTP、JS 响应或动画帧不单独算绘制成功。失焦、最小化、睡眠与唤醒有检测暂停／宽限。
- 继任窗口经私有继承管道与两阶段确认接管，沿用数据目录、离线模式和原引擎 PID／URL／租约，恢复会话／项目与草稿位置；不调用引擎替换或重新执行任务。
- 会话／回合／项目／草稿 key 变化时，前端先发布当前归属，再同步发出不含正文的内部 `zwai:recovery-location` 通知；原生桥独立保存位置，不等待五秒绘制探针。一次 Reload 在原 URL 更新恢复作用域，保留 PID／presence 等字段；初始化 Home 不覆盖尚未匹配的恢复目标，匹配后仍需新挑战的真实像素。位置通知不确认健康或延长任何恢复总期限。证据：`frontend/src/lib/composer-recovery.ts`、`frontend/src/lib/composer-recovery.test.ts`、`frontend/e2e/desktop-recovery.spec.ts`、`internal/desktop/recovery_location.go`、`internal/desktop/recovery_location_test.go`、`internal/desktop/recovery_location_runtime_test.go`、`internal/desktop/recovery_location_script_test.go`；Fixed `C-034`，Issue #110。
- 原生验收在草稿检查退出时保留已有 API／原生观察的摘要及逐项判定，不复制正文、不追加界面操作或放宽原断言；先前失败原因未捕获时仍标为未知。证据：`tools/native_macos_resilience.py`, `tools/test_native_macos_resilience.py`, `docs/TESTING.md`。
- 原生 QA 的 PNG 输入使用一次同进程、前台、唯一启用的 Edit→Paste 菜单动作，先校验剪贴板写入与原 bytes／计数，并保留传输回执；菜单动作回执不替代完整草稿保存与原生 ACK。头less 测试只运行真实 helper 的纯校验，界面副作用仅在隔离来宾验收。证据：`tools/native_macos_resilience_ax.m`, `tools/test_native_macos_paste.py`, `docs/TESTING.md`。

### F-120 Conversations and projects

- 目的：创建、组织、搜索和继续对话；使用者：PC 用户；入口：侧边栏、项目、`/api/threads`；输入：项目和消息；输出：可恢复的对话；前置条件：本机数据库可写；失败表现：错误提示或不存在的对话；关联契约：`C-001`, `C-003`, `C-008`, `C-026`；实现证据：`internal/engine`, `internal/store`, `frontend/src/store/app.ts`。
- 长会话打开：PC 并行读取尾页与详情，尾页到达即移除骨架并显示历史；上下文字符用量、运行态、轮次和跟进分别合并，迟到响应不得覆盖切换、倒带或实时推进后的稿面。仅工具活动的尾页仍补读到可见历史。打开时的 `context_chars` 与下一回合共用历史回放：保留压缩水位后的 user／assistant 正文、排除撤回 steer、补齐未落库主管回答；查询不读既往工具正文及无关事件正文。关联契约：`C-003`、`C-008`；实现／验证证据：`frontend/src/store/app-open.ts`、`frontend/src/store/app-open.test.ts`、`frontend/src/store/thread-history.ts`、`internal/engine/replay_history.go`、`internal/engine/replay_history_test.go`、`internal/engine/compact.go`、`internal/store/replay.go`、`internal/store/replay_test.go`。
- PC 历史呈现：可见内容按事件原顺序显示，不为聚合同一 turn 而重排。每个已结束且非 quiet 的 turn 仅在最后一片终态前可见内容后显示一次耗时和真实完成时间；隐藏标题／会话摘要不产生空片或结束栏。迟到复盘提示保留在实际到达位置，独立于已结束持续目标的工作折叠，不打断当前回合的实时活动提示；原回答与工具详情仍可查看，数据库和 Trace 记录不变。关联契约：`C-003`；实现／验证证据：`frontend/src/lib/turn-boundary.ts`, `frontend/src/lib/turn-boundary.test.ts`, `frontend/src/components/app/transcript-boundary.test.tsx`, `frontend/e2e/late-review-footer.spec.ts`。
- 项目工作目录：原生桌面新建／编辑项目时可用「选择目录」打开系统目录窗口，将单个现存目录的绝对路径填入字段；取消保留原值，选择失败在字段下显示错误。手动输入与留空使用 zwai 管理目录仍可用；浏览器提示桌面支持选择目录，不提供无法返回本机绝对路径的目录上传。保存仍经现有工作目录校验。实现／验证证据：`frontend/src/components/app/project-dialog.tsx`, `frontend/src/components/app/workdir-input.tsx`, `frontend/src/lib/workdir.ts`, `internal/desktop/directory.go`, `internal/desktop/directory_test.go`, `internal/engine/project_test.go`, `frontend/src/components/app/project-dialog.test.tsx`, `frontend/src/components/app/workdir-input.test.tsx`, `frontend/src/lib/workdir.test.ts`, `frontend/e2e/workdir.spec.ts`。
- 目录身份：非空目录路径不裁剪首尾空白；路径标准化仍使用 filepath.Clean。全空白保留托管目录行为。保存、编辑和实际运行解析必须指向同一所选目录；路径失效时拒绝并保留草稿，不回退到去空白后的同名目录。兼容变化：手动输入非空路径也按实际路径验证，不再猜测修正外围空白；已保存的规范路径无需迁移。证据：`internal/engine/project_workdir_identity_test.go`。

### F-125 Unread completion attention

- 手机当前状态：同一对话 ID 正在运行、等待或待答时，顶部完成未读列表暂不显示其旧结果；未读水位与已读回执保持不变，空闲后仍可显示。同名不同 ID 独立保留；搜索使用未过滤的 live 集合判定，非搜索视图中全部已加载结果暂隐藏时仍可继续 More。实现／验证：`mobile/src/components/home-screen.tsx`、`mobile/src/components/home-screen-unread.test.tsx`、`mobile/e2e/active-unread.spec.ts`；关联契约：`C-023`、`C-007`；来源：Issue #83。
- 兼容性：旧 PC 的 `open.detail.completed_seq` 缺失或为 `undefined` 时，手机跳过可选完成回执，正常对话及定时等待继续可用；现代 PC 的数值 `0` 仍允许首次可见终态回执。明确拒绝或网络失败保留未读并显示错误，不伪造成功；证据：`mobile/src/lib/completion-read-compatibility.test.tsx`、`mobile/e2e/unread-completion.spec.ts`；关联契约：`C-006`、`C-023`。

- 目的：让多任务完成后的结果持续可见，直到使用者实际查看；使用者：PC 与配对手机用户；入口：桌面任务前的静态状态灯、收起项目标题右侧的汇总状态、手机顶部完成未读列表；输入：主任务完成事件与已观察的 `completed_seq`；输出：独立于运行呼吸灯的完成提示，查看后在同一 PC 的客户端同步清除；前置条件：同一 host 的未归档对话；失败表现：读取回执失败保留提示并显示错误；关联契约：`C-023`、`C-003`、`C-006`、`C-007`；实现证据：`internal/store/completion.go`, `internal/server/completion.go`, `internal/remote/rpc.go`, `frontend/src/components/app/completion-reader.tsx`, `mobile/src/lib/completion-read.ts`, `mobile/src/lib/inbox-refresh.ts`；验证：`internal/store/completion_test.go`, `frontend/e2e/unread-completion.spec.ts`, `mobile/e2e/unread-completion.spec.ts`, `mobile/src/lib/inbox-refresh.test.tsx`。

### F-130 Agent runs, tools, and live control

- 普通回合完成门禁：没有当前可执行 goal 或活动同对话 wake 时，主管必须用真实 `wait_agents` 结果收取每个 worker 当前 generation，或用 `close_agent` 明确放弃，才可发布完成答复；已完成未收取仍待处理，wait 超时、未知 ID 和旧 generation 回执不结清。提前结束只自动纠正一次，再次提前结束则保持原 turn 与 worker 请求，等待人工 Continue／Stop；Continue 再授予一次尝试，Stop／拒绝取消。重启保留收取状态、纠正额度及人工暂停。Trace 与 PC／手机提示保留 `workers_uncollected` 的真实原因。关联契约：`C-019`、`C-002`、`C-012`、`C-027`；实现证据：`settlement.go`、`internal/engine/worker_completion.go`、`internal/engine/iterations.go`；测试：`settlement_http_test.go`、`internal/engine/ordinary_worker_completion_test.go`、`internal/engine/worker_completion_budget_test.go`、`internal/engine/worker_completion_replay_test.go`。

- 结构化提问：PC 与配对手机在实时和历史重放中按主机规则解析选项 ID；省略或归一化后为空的 ID 从 label 生成，保留选项文字，选择提交后继续同一 turn。关联契约：`C-002`；证据：`internal/engine/ask.go`、`frontend/src/lib/transcript-ask.test.ts`、`mobile/src/lib/ask.test.ts`、`frontend/e2e/plan.spec.ts`。
- 未答提问的交接恢复：重启保留原 turn/call 和人类等待，回答前不调用主管模型；匹配未答检查点的旧未知结果可恢复，真实答案优先。PC 与手机按明确的 `ask_recovered` 通知恢复同一表单，重复回放和迟到未知结果不会覆盖已确认答案。关联契约：`C-002`、`C-003`、`C-027`、`C-012`；验证：`internal/server/restart_ask_http_test.go`、`cmd/zwai/engine_restart_ask_acceptance_test.go`。

- 生成中断：桌面、Web 和配对手机按模型输出身份保留中断原因及恢复状态，旧思考结束流式动画，新生成使用独立身份；恢复中的 worker 保留原 ID，不冒充失败或任务完成。明确区分交接重启、进程退出、取消、超时、模型错误与输出额度；未记录的具体原因保持未知。实时、重开与历史重放保留说明，原 turn 的 Trace 可查。关联契约：`C-035`、`C-003`、`C-027`；实现证据：`signal.go`、`internal/engine`、`frontend/src/lib/transcript.ts`、`mobile/src/lib/transcript.ts`。

- 巡检失败后的 Agent 状态：按 `F-150` 的有效任务归属回执保留指定 worker 的 running、思考流和待完成工具；父巡检错误仍可见，不派生为这些 worker 的失败原因，也不把内部归属 JSON 显示为正文。实时、完整历史和 roster 重放保持一致，实际完成／Stop／cleanup 仍生效。关联契约：`C-009`、`C-012`、`C-003`；实现证据：`frontend/src/lib/transcript-worker-ownership.ts`、`internal/engine/worker_ownership.go`；测试：`frontend/src/lib/transcript-worker-ownership.test.ts`、`frontend/e2e/schedule-worker-ownership.spec.ts`。
- PC 输出身份与回放：同一 worker 模型输出跨 manager 巡检轮次时，累计思考／回答快照更新同一条记录；独立输出即使文字相同也保留。历史页、按需 Agent 日志和实时事件重叠时，同一持久事件只应用一次，重放不重复触发终态刷新等副作用。旧记录缺少输出身份时不按相似文字推断合并。关联契约：`C-003`；证据：`signal.go`、`internal/engine/accumulator.go`、`frontend/src/lib/transcript-blocks.ts`、`frontend/src/store/thread-history.ts`、`frontend/src/store/app-stream.ts`、`signal_identity_test.go`、`internal/engine/accumulator_identity_test.go`、`frontend/src/lib/transcript-message.test.ts`、`frontend/src/store/app-stream-history.test.ts`、`frontend/e2e/agent-history-overlap.spec.ts`。
- PC 停止操作：桌面和 Web 使用输入框中的 Stop 按钮停止正在运行的任务；Esc 只关闭图片预览、Find 或其他当前弹层，未打开弹层和连续按 Esc 时也不会停止任务。关联契约：`C-002`、`C-028`；实现／验证证据：`frontend/src/App.tsx`、`frontend/src/App.test.tsx`、`frontend/e2e/shell.spec.ts`。
- 排队注入：同一会话／排队 ID 的进行中请求去重；已离队时刷新队列并明确提示，刷新失败保留独立错误；跨会话迟到响应不污染当前会话。后台自动出队以实际删除行作为唯一消费权，避免竞争重复投递或丢失并发编辑。关联契约：`C-002`、`C-012`；证据：`internal/store/followups_claim_test.go`、`internal/server/missing_record_test.go`、`frontend/src/store/app-followup.ts`、`frontend/src/store/app-followup.test.ts`、`frontend/e2e/followup-error.spec.ts`。

- 自动重启的任务交接沿用 `F-110`／`C-027`：已接受的回答、继续／停止决定不会因交接被忽略；已确认终态不重复执行；完成 manager 下的后台 worker 直接按原 ID 恢复。证据：`internal/engine/restart_recovery_test.go`、`internal/engine/restart_parked_test.go`。
- PC 人工停止与清理：未完成的子代理显示在独立的 Stopped／已停止分组，停止后的刷新与重开仍保留该状态。绑定当前代理身份且有非空原因的取消 `finished` 回执无论出现在清理前、清理后或主管终态后，都保留已停止；迟到的正文、流式文本、工具调用通知及后续取消或成功回执不能改变已确认的停止状态、活动或停止时间。不把资源清理当成成功，也不把普通 provider／watchdog 失败改成停止。真实完成或失败的结果保留，显式续办同一 ID 后重新接受运行和完成结果；原始事件仍在 Trace。关联契约：`C-002`、`C-003`；证据：`frontend/src/lib/transcript-stop.test.ts`、`frontend/src/lib/transcript-cancelled-finish.test.ts`、`frontend/src/components/app/panel-stop.test.tsx`、`frontend/e2e/restart.spec.ts`。
- Fork 上下文：新 worker 可接收 manager 的历史对话证据，但不接收 manager 的 System/personality/编排身份；仍使用当前分配任务。入口：`spawn_agent(fork_context=true)` / `Registry.SpawnForked`；关联契约：`C-019`；证据：`resume.go`、`swarm.go`、`fork_context_test.go`。

- PC 用户视图：连续工作区块只有一段思考或一个可见工具时，直接显示该条自身的折叠，一次点击即可查看内容；流式思考沿用默认展开与手动折叠。多项保留聚合，回答分隔工作区块。关联契约：`C-022`；实现证据：`frontend/src/components/app/work-fold.tsx`, `frontend/src/components/app/transcript-fold.test.tsx`, `frontend/e2e/work-fold.spec.ts`。

- 目的：运行 manager/sub-agent、查看工具并发送跟进或插入；使用者：PC 用户；入口：对话输入框和 Agent 面板；输入：消息、工具结果、排队消息的修改、未读 steer 的修改；输出：实时事件和最终回答；前置条件：配置可用 provider；失败表现：运行错误和 Trace；关联契约：`C-002`, `C-003`, `C-012`；实现证据：`internal/engine`, `internal/tools`, `frontend/src/components/app`。
- 人工排队消息在当前轮次成功或错误结束后按 FIFO 启动独立新轮次；定时检查失败也遵守，原 turn/run 错误、未读结果及未来唤醒保持。无排队消息不因此自动重试；Stop、取消和引擎重启不自动出队。停止后的排队行点击注入，作为下一轮发送；活动轮次仍注入当前回合。关联契约：`C-002`、`C-009`。证据：`internal/engine/followup_test.go`, `internal/engine/followup_after_error_test.go`, `frontend/e2e/api-error.spec.ts`, `frontend/e2e/conversation.spec.ts`。
- 极快完成的轮次在发送 ACK 晚于终态事件时仍保持空闲，迟到 ACK 不改变后来选择的对话；证据：`frontend/src/store/app-send.test.ts`, `frontend/e2e/api-error.spec.ts`。
- 轮次开始后响应区立即显示旋转的“工作中”，首个服务端进度脉冲到达后切换为计时；轮次结束即消失，不等待本地模型首字节。实现证据：`frontend/src/components/app/transcript.tsx`, `frontend/src/components/app/transcript.test.tsx`。
- 桌面/Web 的结束分界沿用 `F-120`／`C-003` 的单次结束栏规则；持续目标折叠也保留该分界，进行中不提前标结束。实现证据：`frontend/src/components/app/transcript-session.tsx`, `frontend/src/components/app/transcript-session.test.tsx`, `frontend/e2e/schedules.spec.ts`。
- 代理名单：行上是 `resume_agent` 使用的 `agent_id`，不是 `#n`。进行中与已完成各自成组。组内默认按最后更新从新到旧；可改为创建时间或行上的名称，并在从新到旧 / 从旧到新（名称则是 A 到 Z / Z 到 A）之间切换。没有时间的行留在底部。实现证据：`frontend/src/lib/agent-label.ts`, `frontend/src/lib/agent-roster-sort.ts`, `frontend/src/components/app/panel.tsx`。
- 子代理失败原因：桌面/Web 的 failed 行提供「查看失败原因」入口，详情错误卡固定在日志滚动区外，保留记录的错误；已知错误按界面语言解释，旧记录缺失原因时明确说明未记录。父 turn 的 error 到达时仍在运行、且没有本轮有效归属保留回执的子代理保留父错误并标明来自所属轮次；子代理自己的已记录失败原因不被覆盖，先前 cleanup 已停止的代理不倒推成失败。精确的本代理 watchdog 错误可跳到设置 → Swarm 的 `agent_timeout_seconds`；精确输出额度错误跳到 `max_completion_tokens`，worker 迭代上限错误跳到 `max_turns`，聚焦对应输入；已识别的 provider 流空闲超时打开设置 → Models，不猜测具体 provider。普通 deadline、取消和未知错误不猜测配置修复；保存配置不会自动续办子代理。关联契约：`C-012`、`C-009`；实现／测试证据：`frontend/src/components/app/agent-failure.tsx`、`frontend/src/components/app/agent-failure.test.tsx`、`frontend/src/lib/agent-failure.ts`、`frontend/src/lib/agent-failure.test.ts`、`frontend/src/lib/transcript-failure.test.ts`、`frontend/e2e/agent-failure.spec.ts`、`frontend/e2e/worker-timeout.spec.ts`。
- 历史 worker 续接：manager 通过 System JSON 状态快照接收历史 worker 的 ID、角色及运行／完成／失败状态；已结束 worker 仍可查询或显式续办，不把历史名册重放为本轮新建调用。持续目标、定时检查、压缩、模型重试及重启使用同一状态语义。关联契约：`C-019`、`C-012`；实现／测试证据：`internal/workerstate/snapshot.go`、`internal/engine/autocompact_roster.go`、`internal/engine/resume_workers.go`、`internal/engine/worker_roster_test.go`、`frontend/e2e/worker-roster-history.spec.ts`。
- 排队与未读引导：工作中的 Enter 进入队列。点开一条排队消息，或点未读引导上的铅笔，原文进输入框（引用变成卡片），原来的那条先拿掉。再按 Enter，和新建一条排队一样，排到队尾。已读引导不会被拉进输入框。只有图片的未读引导没有可排队的文字，只能删除。
- 主动委派：共享运行时提示词要求 manager 在能提效或提高质量时主动调用 subagents，执行中重新评估；单个 worker 也可承担独立审查或第二种思路，不要求用户显式提出。普通对话与持续目标遵循相同策略；无收益时自行处理，遵守用户限制与并发上限，由 manager 核验和整合结果。关联契约：`C-019`；实现证据：`internal/engine/prompt.go`, `internal/engine/prompt_test.go`, `cmd/zwai/tui_prompt_test.go`。

- 无效委派恢复：同一模型回复内缺少 role/task 或参数不可读的 spawn 调用按错误类别合并为一条拒绝回执，保留数量和原始代表 ID；有效调用仍正常执行，manager 可补全参数在原轮次继续。拒绝不等于创建或取消 worker。关联契约：`C-019`；实现／测试证据：`spawn_batch.go`, `spawn_batch_test.go`, `tools.go`, `internal/engine/prompt.go`。

- 有界 worker 超时恢复：同一 worker 连续三次 watchdog 失败后停止 manager 的 resume／同角色 spawn，持续目标停止自动推进，失败周期计划暂停；成功或显式人类重试恢复预算。错误、worker ID 和次数保留在 Trace 与手机/桌面对话；普通 provider 超时和取消不计入。关联契约：`C-019`、`C-009`；实现／测试：`worker_timeout.go`、`internal/engine/worker_timeout.go`、`internal/engine/worker_timeout_test.go`、`frontend/e2e/worker-timeout.spec.ts`。

- 错误提示：模型 HTTP/流响应无法解析时，桌面和手机区分响应解码与真实工具参数错误，并保留同 turn Trace 和最多两次重试；证据：`internal/engine/api_error_integration_test.go`, `frontend/e2e/api-error.spec.ts`。
- 首字节超时恢复：错误保留实际等待时长；桌面/Web 建议到设置 → 模型调大首字节等待时间，可从错误直接打开失败轮次的提供商并聚焦字段。设置能力和提供商缺失时的行为见 `F-170`；关联契约：`C-009`、`C-012`。
- 请求前工具参数检查：压缩 middleware 返回状态后检查当前尾部调用的非空 JSON 参数；无效参数在实际模型 HTTP 前拒绝，沿既有有界重试清理，不清除人类 steer 或伪造 worker 创建。不是全历史 JSON 扫描。关联契约：`C-012`；证据：`internal/engine/autocompact.go`、`internal/engine/model_retry.go`、`internal/engine/model_retry_http_test.go`。

- 模型故障退避：同一上游 origin 的 manager、subagent、模型及 provider 别名共享 5→10→20→30 秒封顶冷却，到期仅放行一个恢复探测；完整成功才恢复并发，Chat Completions HTTP 流缺少结束标记及 finish reason 时作为截断错误，保留故障及已收到的 usage。Stop 可取消等待及请求，换轮次或清缓存不能绕过。等待记录进入原 turn 的 Trace，不增加自动重试次数；quiet 报告后的迟到等待不重新显示 Chat 通知。关联契约：`C-029`；实现／测试证据：`internal/provider/backoff.go`、`internal/provider/model_backoff.go`、`internal/provider/stream_completion.go`、`internal/provider/stream_completion_test.go`、`internal/provider/backoff_integration_test.go`、`internal/engine/model_backoff_test.go`、`frontend/src/lib/transcript-backoff.test.ts`。

### F-135 Independent task coordination

- 目的：让不同 PC 对话的管理 Agent 发现活跃任务、协商共享资源的处理顺序；使用者：PC 用户和配对手机用户；入口：manager 工具 `list_tasks`、`send_task_message`、`read_task_messages`，对话内的协作消息通知；输入：真实 conversation ID、消息和可选收件游标；输出：活跃任务的工作目录/请求/状态、持久化发送回执、来源可追溯的消息和明确回复；前置条件：同一 host 的未归档对话；失败表现：未知/归档目标、非法参数或数据库错误返回工具错误，不伪造送达；关联契约：`C-021`、`C-012`；实现证据：`internal/engine/task_coordination.go`、`internal/engine/task_coordination_flow_test.go`、`internal/store/event_kind.go`、`frontend/e2e/task-coordination.spec.ts`、`mobile/src/lib/task-message.test.ts`。
- 这是通信协商能力。发现结果不是资源锁，发送成功不是接收方已同意；双方明确回复后才可依赖分工或执行顺序。消息不会代替用户授权、自动启动空闲对话、解除暂停或打断另一个任务。

### F-140 Files and attachments

- 目的：让对话使用工作区文件和附件；使用者：PC 用户；入口：文件面板、上传及下载 API；输入：文件或图片；输出：文件、预览或模型视觉输入；前置条件：工作区和权限可用；失败表现：上传或工具错误；关联契约：`C-004`, `C-005`；实现证据：`internal/server`, `internal/tools`, `frontend/src/components/app`。
- 图片输入：png/jpeg/gif/webp 文件在空 MIME、通用 binary 或 UTI 标签下仍进入视觉输入；显式 text/PDF 保持文件附件。客户端只归一化 MIME 声明，不改原字节，服务端继续执行真实类型与声明匹配校验。关联契约：`C-028`；证据：`frontend/src/lib/paste-image.ts`、`frontend/src/lib/paste-image.test.ts`、`frontend/src/components/app/composer-image-drop.test.tsx`、`frontend/e2e/image-input.spec.ts`。
- 已发送图片预览：桌面与 Web 对话中的图片缩略图可点击或通过 Enter／Space 打开放大层，多图时打开点选的图片。预览沿用已保存图片地址，按原比例适配窗口；显示加载及失败状态，可通过 Esc、遮罩或关闭按钮关闭并回到缩略图焦点，刷新重载后仍可打开。关闭预览不停止正在运行的任务。关联契约：`C-028`；证据：`frontend/src/components/app/input-thumbs.tsx`、`frontend/src/components/app/input-thumbs.test.tsx`、`frontend/e2e/image-input.spec.ts`。

### F-145 Durable composer drafts

- 目的：在刷新、窗口恢复及引擎换端口后保留未发送内容；使用者：PC 桌面和 Web 用户；入口：输入框、附件／粘贴图片／引用，以及 `/api/composer/drafts/:key`；输入：正文、引用、原始文件与图片；输出：按会话或新会话项目隔离的完整草稿及保存／失败／冲突状态；前置条件：本机 SQLite 可写；失败表现：保留当前输入并显示恢复／保存／发送未确认或版本冲突，不自动重发；关联契约：`C-033`, `C-028`, `C-011`；实现证据：`frontend/src/lib/composer-draft.ts`, `frontend/src/lib/use-composer-draft.ts`, `frontend/src/components/app/composer-draft-status.tsx`, `internal/store/composer_draft.go`, `internal/server/composer_draft.go`, `internal/store/composer_draft_test.go`, `internal/server/composer_draft_test.go`, `frontend/src/components/app/composer-draft.test.tsx`。
- 正文、引用、附件引用在同一事务中保存；原始 bytes 只在新增附件时写入，后续键入仅更新快照。保存 ACK 前不能显示“已保存”；发送 ACK 后才清理对应版本，旧页面迟到保存不能复活已清草稿。首发迁移保留来源副本至发送确认。
- 后续编辑引用的附件 bytes 缺失或长度不符时拒绝保存，保留原版本，不能仅凭 manifest 返回保存成功。
- 离开已保存且无待处理操作的 scope 后释放内存中的附件缓存和预览 URL，返回时重读原始 bytes；未保存、冲突和发送中内容继续保留。
- `/goal`、`/plan` 仅接收文本；同时带附件、粘贴图片或引用时，输入框拒绝动作并保留完整草稿，不能用只覆盖文本的确认清理其他内容。
- 每份草稿正文与引用合计最多 1 MiB，最多 32 个附件、附件总量 256 MiB；视觉图片另遵守最多 8 张、单张 8 MiB。此限制约束草稿保存，不改变工作区上传的单文件上限。

### F-150 Goals and scheduled waits

- 显式命令参数：PC 目标与计划仅发送一次完整命令，命令参数中的 `/goal`、`/plan` 字面量保持原文，空闲到运行／待答的竞争不把命令当成跟进或回答。目标可更新并保留原待答问题；计划只创建新轮次，转忙时拒绝且保留草稿；裸计划与忙碌更新保持原有边界。关联契约：`C-002`、`C-009`、`C-033`；实现／验证：`frontend/src/store/app.ts`、`frontend/src/store/app-plan.ts`、`frontend/src/store/app-goal.test.ts`、`frontend/src/store/app-plan.test.ts`、`internal/engine/turn.go`、`internal/engine/slash_command_once_http_test.go`、`frontend/e2e/trailing-goal.spec.ts`、`frontend/e2e/plan.spec.ts`。
- 显式目标命令：`<objective> /goal` 使用前置正文激活新持续目标，清除旧目标的完成状态；PC、HTTP 和手机发送／插话共用命令语义，普通消息、含中文的路径、引用及代码中的字面命令保持普通内容。关联契约：`C-009`；实现／验证：`internal/slash/trailing.go`、`internal/slash/trailing_goal_test.go`、`internal/slash/testdata/trailing-goal.json`、`internal/engine/slash_steer_test.go`、`internal/remote/slash_goal_test.go`、`frontend/src/lib/slash.ts`、`frontend/src/components/app/composer-trailing-goal.test.tsx`、`frontend/e2e/trailing-goal.spec.ts`。
- 目标完成展示：桌面/Web 与配对手机在目标 `complete` 后立即隐藏输入框上方目标条，刷新或重新进入已完成会话仍不显示；保留持久化目标、完成状态、结果和历史记录，不自动清空或重新执行。桌面与手机对话历史保留 `goal_complete` 完成通知；桌面/Web 的完成通知置于已折叠目标会话外，早于或晚于最终回答到达都可见；新设 `/goal` 后重新显示目标条。未完成的暂停、受阻和计划等待继续保留状态及相应控制；已完成目标不再通过目标条显示 Start。关联契约：`C-009`、`C-003`；实现／验证证据：`frontend/src/components/app/goal-banner.tsx`、`frontend/src/components/app/goal-banner.test.tsx`、`frontend/src/lib/transcript.ts`、`frontend/src/components/app/transcript-session.tsx`、`frontend/src/components/app/transcript-session.test.tsx`、`frontend/e2e/goal-resume.spec.ts`、`frontend/e2e/goal-schedule.spec.ts`、`mobile/src/components/status-banners.tsx`、`mobile/src/components/thread-screen.test.tsx`、`mobile/src/lib/transcript.ts`、`mobile/src/lib/transcript-goal.test.ts`、`mobile/e2e/goal-completion.spec.ts`。
- 调度成功答复：主管正文发布前通过无工具语义抽取识别调度成功声明，再由代码核对持久化计划、当前有效状态、本轮归属和声明的周期。未调用、被拒或存储失败的安排、已取消的计划、把旧等待说成本轮新建，均不能作为成功依据；最多一次原轮纠正，仍不成立则明确失败。工具、思考及 worker 活动继续显示，正文需等待校验；分类沿用提供商首字节／响应体空闲预算和轮次取消，不另设 30 秒总时限。关联契约：`C-009`、`C-012`；实现／验证：`internal/engine/schedule_answer.go`、`internal/engine/manager_answer_buffer.go`、`internal/engine/schedule_answer_http_test.go`、`internal/engine/schedule_answer_timeout_test.go`。

- PC 恢复提示：人工恢复目标后，紧接启动的新轮次只显示一条继续提示；新轮次尚未启动时保留恢复提示。迟到的类型化复盘反馈保持既有可见性与原顺序，不中断同次恢复配对；普通提示、输入、实质 manager 活动与普通错误仍中断配对。后续自动续轮、独立恢复及 manager `reopen_goal` 仍分别可见，不按提示文字去重。原始 `goal_resumed`、`goal_continued` 和区块保留，Trace 可逐事件查看。关联契约：`C-003`、`C-012`；实现／验证证据：`frontend/src/lib/goal-notice.ts`、`frontend/src/lib/goal-notice.test.ts`、`frontend/src/components/app/transcript.tsx`、`frontend/src/components/app/transcript-goal-notice.test.tsx`、`frontend/src/store/goal-notice-history.test.ts`、`frontend/e2e/goal-resume-notice.spec.ts`；来源：GitHub Issue #96。
- 巡检与 worker 生命周期隔离：定时触发或 Run now 的常规失败，包括无执行证据和有界重试后的模型错误，不取消仍归属有效持续目标或活动同对话 wake 的 worker；原 registry、ID 和正在进行的调用继续供后续轮次接回。有效目标不含 blocked、complete、capped、idle 或自动续轮预算耗尽；plan mode 中目标与 wake 均不能提供可执行归属；已消费 one-shot 不能单独充当归属。turn/run 保留 error、run unread，失败不自动 block goal 或立即开启新的目标轮次。无归属、人工 Stop、上下文预算耗尽及 worker watchdog 失败上限仍按原停止边界清理。持久化 `workers_retained` 在 error 前记录精确归属和 worker IDs，写入失败不得保留执行；重启恢复还需该回执、检查点及当前有效归属，不改变原失败终态。关联契约：`C-009`、`C-027`、`C-012`；实现证据：`internal/engine/worker_ownership.go`、`internal/engine/turn.go`、`internal/engine/restart_parked.go`；测试：`internal/engine/schedule_worker_ownership_test.go`、`internal/engine/worker_ownership_test.go`、`frontend/e2e/schedule-worker-ownership.spec.ts`。
- 人类等待边界：用户明确禁止挂起当前会话时，原始有效人话经只读策略模型提取为按来源事件 seq、事件身份和文本指纹缓存的状态；`schedule_wake` 与 thread `report_schedule(next_in_s)` 返回 `thread_wake_prohibited`，未完成策略提取返回 `thread_wake_policy_pending`。拒绝不更新等待行、不产生新的等待卡；已分类禁令的 Trace 保留 source_seq、source_text、source_quote；待分类拒绝保留已知 pending_seq。压缩、重启及模型自写的唤醒正文不能解除，仅后来明确允许等待的人话可放行；不带 `next_in_s` 的报告（包括 `keep:false` 取消）继续可用。关联契约：`C-009`、`C-012`；实现／验证证据：`internal/engine/thread_wake_policy.go`、`internal/engine/thread_wake_policy_test.go`、`internal/store/wake_policy.go`、`internal/store/wake_policy_guard_test.go`、`frontend/e2e/human-wake-policy.spec.ts`。

- 目的：持续目标及未来时间继续执行；使用者：PC 用户；入口：`/goal`、Scheduled 页、`schedule_wake`；输入：目标和时间；输出：状态、唤醒、运行结果；进行中或已暂停的任务可在编辑里改下次落点（每次新开对话，或某个未归档会话，列表可搜索），已完成的不能；已认领的那一轮留在原会话，安静归档看认领时的类型；打开会话时右侧只列出指向该会话的 active/paused 唤醒，以及某次运行创建了该会话的来源任务（终态来源仍保留），点一条打开该任务；完成/取消的普通唤醒移出右栏，记录仍在定时任务的已完成列表，无关联项时隐藏该区；暂停/完成/取消行按真实状态显示，不把历史触发时间标为即将运行；有下一次已安排唤醒时，成功轮次中的未完成 sub-agent 保持原 ID 继续运行，下次轮次可接回；取消或暂停最后一次唤醒会停止无人接手的 worker；工具选择保持 `auto`，定时检查可直接返回非空文本并记为 findings，不要求调用工具或报告；仅空白终答且没有真实工具结果或报告时原轮继续一次，仍无输出显示错误，显式 quiet 报告保持优先；文本结束本次检查不替代长期目标完成或外部工作核验；周期检查的无计费重放内容按多语言文本和消息结构估算，在达到阈值时先自动压缩；若仍因上下文越界而终止，对话写出稳定原因并暂停该检查，用户调整后可恢复；进程重启后，若最近一次派出的 sub-agent 已因进程停止而失败，且会话停在未来的 thread wake 上，启动把这次唤醒提前到现在，并在 resume 这些 id 或新 spawn 之前拒绝 `schedule_wake` 和 `report_schedule` 的 `next_in_s`；前置条件：引擎运行；失败表现：跳过、失败或等待状态；关联契约：`C-002`, `C-003`, `C-009`；实现证据：`internal/engine/goal_session.go`, `internal/engine/autocompact.go`, `internal/engine/iterations.go`, `internal/engine/schedule_completion.go`, `internal/engine/schedule_completion_http_test.go`, `internal/engine/schedule.go`, `internal/engine/schedule_dest.go`, `internal/engine/schedule_context_overflow_test.go`, `internal/engine/context_overflow_schedule_test.go`, `internal/store/schedule_thread.go`, `frontend/e2e/schedules.spec.ts`, `frontend/src/components/app/conversation-schedules.tsx`。

- 目标编辑：历史 fire 类型冻结、目标与状态编辑及活动上限原子提交；已领取旧 wake 保持归属，移走最后 wake 后恢复原目标或收尾 worker。证据：`internal/engine/schedule_retarget_test.go`, `internal/store/schedule_retarget_test.go`。

- 完成范围：定时轮次自己的 manager thread wake 仍有后续触发，或本轮新排了这类等待时，`complete_goal` 返回可恢复冲突；完成该等待后才能完成长期目标。同轮先完成目标再重新排等待也返回冲突；人工和独立任务的等待保持独立。证据：`internal/engine/goal_schedule_completion_test.go`, `frontend/e2e/goal-schedule.spec.ts`；关联契约：`C-009`, `C-012`。
- 续轮模型请求遵循 `F-130` 的共享服务退避（`C-029`），新 turn 不重置故障档位。
- 续轮 worker 身份和状态沿用 `F-130` 的历史状态快照，不把历史总数当成本轮创建量。关联契约：`C-019`；证据：`internal/engine/resume_workers.go`、`internal/engine/worker_roster_test.go`、`frontend/e2e/worker-roster-history.spec.ts`。

### F-160 Trace and diagnostics

- 逐次生成中断与恢复回执见 `F-130`／`C-035`；原 turn 的 Full log 保留输出身份及原因。证据：`internal/engine/generation.go`、`internal/engine/generation_http_test.go`。

- 目的：用 turn id 还原一次运行；使用者：开发者和用户；入口：`zwai trace`, `/api/trace/:turn`, Trace 面板；输入：turn id；输出：事件、模型和工具记录，桌面/手机公共错误保留模型 HTTP 状态及具体接口/解码原因，同 ID 可查看原始调用和重试；前置条件：该 turn 已记录；失败表现：找不到记录；关联契约：`C-012`；实现证据：`internal/engine/runerr.go`, `internal/engine/api_error_integration_test.go`, `internal/server`, `internal/store`, `frontend/src/components/app`, `mobile/e2e/api-error.spec.ts`。
- 调度答复校验的模型调用、提供商超时原因及纠正预算归属原 turn，见 `F-150`／`C-009`；验证：`internal/engine/schedule_answer_timeout_test.go`。
- Worker 创建核验：按真实 turn 的调用、拒绝回执和生命周期事件区分新建、续办／恢复、完成及清理，不从历史名册或模型自述推断本轮创建量。证据：`internal/engine/worker_lifecycle_facts.go`、`internal/engine/worker_lifecycle_facts_test.go`、`frontend/e2e/worker-roster-history.spec.ts`。

- 模型冷却以 `model_backoff` 保留真实调用者与等待信息，不计作 LLM 请求；等待状态沿用 `F-130`（`C-029`）。证据：`internal/engine/model_backoff.go`、`internal/engine/model_backoff_test.go`。
- 模型流的错误或 EOF 在本次调用记录回调结束后才交给消费端；部分 usage、调用者及错误类别保持，消费端在保存期间关闭也只记录一次。关联契约：`C-012`；证据：`internal/provider/provider.go`、`internal/provider/stream_terminal_order_test.go`。

### F-170 Settings and providers

- Chat Completions 推理字段兼容：PC 模型调用与手机直连均接收旧 `reasoning_content` 和新版 vLLM `reasoning`，覆盖 JSON message 与 SSE delta；非空旧字段优先，旧字段空或 null 时回退到新字段，不重复拼接。无需字段名配置；PC 思考沿现有流、历史与 Trace 展示，手机直连保留思考及本地历史。关联契约：`C-009`；入口：Settings → Models、手机 Models／Chat；实现证据：`internal/provider/provider.go`、`go.mod`、`mobile/src/lib/openai-wire.ts`；验证：`internal/provider/reasoning_fields_test.go`、`frontend/e2e/reasoning-fields.spec.ts`。
- 已配对手机在 PC 收件箱 **Menu → PC timeouts／电脑超时** 读取并编辑该 PC 的 worker、各提供商首字节和流空闲时限，加载失败、保存失败和旧主机均在面板内提示并可重试。仅提交改动字段，不接收密钥或端点，不覆盖无关设置；首字节与流空闲预算独立。新 worker 默认一小时，已有正值保持，保存不延长运行中的 deadline、不重置 watchdog 次数或恢复暂停任务。使用者：已配对手机用户；前置条件：PC 在线且支持相应 RPC；关联契约：`C-006`、`C-009`、`C-019`、`C-030`；入口说明：`mobile/README.md`、`docs/CONFIG.md`；实现／验证：`internal/config/timeouts.go`、`internal/config/timeout_contract_test.go`、`internal/remote/timeouts.go`、`internal/remote/timeouts_test.go`、`internal/remote/timeout_adapter_test.go`、`mobile/src/components/pc-timeout-sheet.tsx`、`mobile/src/lib/use-pc-timeouts.ts`、`mobile/e2e/pc-timeouts.spec.ts`。
- 桌面 Settings 自动保存和刷新模型目录保留手机并发修改的超时；只有实际编辑的超时才提交替换，重开设置读取最新值。关联契约：`C-009`；实现／测试：`frontend/src/lib/settings-persist.ts`、`frontend/src/store/app.ts`、`frontend/src/lib/settings-persist-delta.test.ts`、`frontend/e2e/settings-timeout-concurrency.spec.ts`、`internal/server/timeout_settings_test.go`。
- 目的：配置模型、外观、工具和远程连接；使用者：PC 用户；入口：Settings；输入：配置字段，包括每个提供商在输入框模型列表里的开关；输出：持久化设置和可用模型，关掉的提供商不出现在输入框和已配对手机的目录里，已经选中它的会话继续用；前置条件：本机数据目录；失败表现：校验或连接错误；关联契约：`C-006`, `C-009`, `C-012`；实现证据：`internal/config`, `internal/provider`, `frontend/src`。
- 首字节等待：设置 → 模型的每个提供商可单独设置「首字节等待时间（秒）」，默认 30 秒，与流开始后的请求空闲超时独立。首字节错误的「配置首字节等待时间」打开失败 turn 的提供商并滚动聚焦输入框，不跟随后来切换的模型；未知或已删除提供商只打开模型页。修改自动保存，关闭设置后用户可重新发送请求。实现证据：`internal/config/provider.go`, `internal/provider/http.go`, `frontend/src/components/app/model-settings.tsx`, `frontend/src/components/app/transcript-error.tsx`；验证：`internal/config/provider_timeout_test.go`, `internal/server/settings_timeout_test.go`, `frontend/src/components/app/transcript-error.test.tsx`, `frontend/e2e/api-error.spec.ts`。
- 输出额度用尽：桌面端主对话这条错误下面有提示，链到设置 → 集群的「单次输出上限」，并聚焦该输入。关掉设置后，同一条主对话错误上出现重试，按原来的请求再跑一轮。子代理详情中的同类错误复用该配置入口，不触发主对话重试；详见 `F-130`。手机不能改这项，只显示错误原文。实现证据：`frontend/src/components/app/transcript-error.tsx`, `frontend/src/components/app/agent-failure.tsx`, `frontend/src/lib/output-budget.ts`。

- 同源 provider 别名沿用 `F-130` 的共享冷却；保存设置不清故障档位（`C-029`）。证据：`internal/provider/backoff_integration_test.go`。

### F-175 Provider cloud built-in tools

- 目的：让支持 Responses 的 provider 在云端执行自带工具；使用者：PC 用户及 API 配置管理者；入口：Settings → Models 的 provider API 模式和工具 JSON、`PUT /api/settings`、`config.yaml`；输入：`api_mode` 和 `builtin_tools`，工具对象包含唯一非空 `type` 及服务商参数；输出：云端工具与本地函数工具共同供模型选择，流式回答含可点击来源，同 turn 的 `builtin_tool` Trace 回执；前置条件：端点、模型及所配置工具支持 Responses；失败表现：无效配置拒绝且保留草稿，服务端拒绝或流错误沿现有模型错误通道显示；关联契约：`C-030`、`C-009`、`C-012`；实现证据：`internal/config/provider_tools.go`、`internal/provider/responses.go`、`internal/engine/builtin_tools.go`、`frontend/src/components/app/model-settings.tsx`；验证：`internal/config/provider_tools_test.go`、`internal/server/settings_provider_tools_test.go`、`internal/engine/builtin_tools_test.go`、`frontend/e2e/builtin-tools.spec.ts`。
- 当前支持 `web_search`、`web_search_preview`、`file_search`、`code_interpreter`；参数原样保留，不另行上传文件或调用资源管理接口；自动容器等选项仍可在服务商执行时创建资源。默认仍为 Chat Completions 且工具为空，不能根据服务商名称自动开工具。PC provider 配置也作用于通过 PC 执行的配对手机会话；手机独立直连配置不在此功能范围。

### F-180 PC text quoting

- 目的：把选中的对话原文与提问分开提交；使用者：PC 用户；入口：选中 transcript 文本 → Add to chat；输入：选区和可选请求；输出：可编辑引用与带标签的消息；前置条件：选区在对话来源内；失败表现：无有效选区时不显示操作；关联契约：`C-011`；实现证据：`frontend/src/components/app/selection-menu.tsx`, `frontend/src/lib/quote.ts`, `frontend/src/components/app/selection-menu.test.tsx`。

### F-190 Desktop updates

- 目的：在 Mac 桌面应用里自动发现并确认安装新版本；使用者：Mac 桌面用户；入口：启动、每六小时、到期后的恢复前台／联网检查，以及菜单 Check for updates；输入：本仓库最近一个含同架构 `zwai-<version>-darwin-<arch>.zip` 的 Release；输出：确认后替换当前应用，以同一数据目录和离线模式打开新实例，关闭已登记的旧窗口，解压后 Finder 显示与 Dock 一致的 ZWAI 图标；前置条件：运行的是带数字版本号的 `zwai.app`；失败表现：自动检查保持安静，手动检查与安装显示可重试错误，启动命令失败恢复旧应用；关联契约：`C-010`, `C-024`；实现证据：`internal/update`, `frontend/src/lib/desktop-update.ts`, `frontend/src/components/app/desktop-update.tsx`, `frontend/e2e/desktop-update.spec.ts`, `internal/desktop/pack`, `internal/desktop/bundle_darwin.go`。

- 更新安装以应用包为互斥范围，跨不同数据目录的引擎/进程返回 busy；启动失败保留并恢复旧包，延迟清理仅处理该次安装独有的备份。证据：`internal/update`, `internal/update/bundle_lock_test.go`；契约：`C-024`。
- 下载显示已接收字节；总量已知时显示百分比，未知时不伪造百分比，随后显示安装与重启阶段。检查和安装错误使用红色警示；安装失败保留已确认版本，重试直接安装该版本，检查失败重试检查。进度读取暂时失败不结束进行中的安装。入口：更新横幅、`GET /api/update/progress`；实现证据：`internal/update`, `internal/server/update.go`, `frontend/src/lib/desktop-update.ts`, `frontend/src/components/app/desktop-update.tsx`, `frontend/e2e/desktop-update.spec.ts`；契约：`C-024`。

### F-210 Pair a phone with a PC

- 目的：安全绑定手机和 PC；使用者：手机用户；入口：Settings → Phone QR、扫码或粘贴；输入：pairlink URI；输出：加密连接和设备列表；前置条件：hub 和 PC 在线；失败表现：网络、过期或 host offline；关联契约：`C-006`；实现证据：`internal/remote`, `mobile/src/components/scan-screen.tsx`, `mobile/src/lib/link.ts`。
- PC 的 hub 注册在受控后台生命周期内执行；网络迟缓只影响手机连接，不阻塞本地程序启动。Stop／Reload 可取消注册，旧结果不得重新启用已停止或已替换的 host。证据：`internal/remote/host.go`、`internal/remote/host_start.go`、`internal/remote/host_start_test.go`、`internal/app/startup_remote_test.go`。

### F-220 Read and control PC conversations

- 首屏一次 `watch include_detail` 同时取得详情与最新事件；旧 PC 自动补读详情。摘要与轮次在 SQLite 中有界读取，跨隐藏协调记录仍能找到原有可见摘要；重连替换旧订阅，Stop／Reload 收口自有链接。关联契约：`C-006`；实现：`internal/remote/watch_snapshot.go`、`internal/remote/host_links.go`、`internal/store/phone_reads.go`、`mobile/src/lib/open-thread.ts`；验证：`internal/remote/watch_open_test.go`、`internal/remote/host_links_test.go`、`internal/remote/snapshot_reads_test.go`、`mobile/src/lib/open-thread.test.ts`、`mobile/e2e/conversation-open.spec.ts`。
- Ask Other 使用与消息编辑器相同的 16px 聚焦字号；关闭 iOS 键盘、提交被拒绝及重试成功后保留窗口内的导航和内容。关联契约：`C-017`；实现：`mobile/src/components/ask-card.tsx`；验证：`mobile/e2e/ask-answer.spec.ts`, `mobile/e2e/ios-ask-viewport.swift`。
- 已绑定 PC 的收件箱菜单提供电脑超时入口；读取、编辑与保存的主定义见 `F-170`，不复用手机直连提供商设置。关联契约：`C-006`、`C-009`、`C-015`；实现／UI 回归：`mobile/src/components/pc-timeout-sheet.tsx`、`mobile/src/lib/use-pc-timeouts.ts`、`mobile/e2e/pc-timeouts.spec.ts`。
- 手机配对对话中的用户与回答代码围栏按已知语言使用主题语法色，C++ 别名沿用现有词法器；未知或无语言标签保持纯文本，流式追加和复制保持代码原文。直连聊天复用同一渲染能力，详见 `F-230`。关联契约：`C-032`；实现证据：`mobile/src/components/markdown.tsx`, `frontend/src/lib/source-highlight.ts`；测试：`mobile/src/components/markdown-highlight.test.tsx`, `mobile/e2e/markdown-highlight.spec.ts`。
- 手机在 PC 对话或新建对话的模型选择器里主动刷新上游模型目录，显示进行中和可重试错误；成功后可选目录移除下线模型并加入新模型，但保留显式选择，不改写 PC 配置或默认模型。旧 PC 不支持时在选择器内提示更新。关联契约：`C-006`, `C-009`；实现证据：`internal/remote/catalog_refresh.go`, `internal/remote/pump_catalog.go`, `mobile/src/lib/use-model-catalog.ts`, `mobile/src/components/model-picker.tsx`；测试：`internal/remote/catalog_refresh_test.go`, `internal/remote/pump_catalog_test.go`, `mobile/src/lib/use-model-catalog.test.ts`, `mobile/src/components/model-picker.test.tsx`, `mobile/e2e/model-refresh.spec.ts`。
- 目的：在手机查看同一对话并发送、跟进、回答问题、插入及翻页；使用者：手机用户；入口：手机收件箱和对话页；输入：消息与操作，包括切换已绑定 PC、对 PC 的提问选择选项或填写 Other、把队列首条等待消息中断插入当前轮次；输出：切换 PC 后停留在该 PC 收件箱，进入对话后看到 PC 上同一对话的事件，提问回答经 PC 确认后继续，窄屏长输入仍可直接提交；当前轮次只有被隐藏的后台协调记录时说明空白原因，「更早」一次跨过连续隐藏记录到达可显示历史；前置条件：已绑定且在线；失败表现：重连提示或 RPC 错误，提问提交被拒绝时显示原因并保留草稿重试；关联契约：`C-002`, `C-003`, `C-006`, `C-015`, `C-016`, `C-017`；实现证据：`internal/remote/log.go`, `internal/remote/log_test.go`, `mobile/src/app.tsx`, `mobile/src/app.test.tsx`, `mobile/src/components/thread-screen.tsx`, `mobile/src/components/thread-screen.test.tsx`, `mobile/src/components/ask-card.tsx`, `mobile/src/lib/ask.ts`, `mobile/src/lib/phone-turn.ts`, `mobile/src/lib/phone-turn.test.ts`, `mobile/e2e/ask-answer.spec.ts`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/composer-layout.spec.ts`。
- 手机在每个已结束轮次后显示完成事件的本地日期时间和分界线；定时输入隐藏、失败轮次及历史翻页不丢掉边界。实现证据：`mobile/src/lib/transcript.ts`, `mobile/src/components/thread-blocks.tsx`, `mobile/src/lib/transcript.test.ts`, `mobile/src/components/thread-blocks.test.tsx`, `mobile/e2e/walkthrough.spec.ts`。
- iOS 从左屏幕边缘向右完成滑动，按当前可见详情／弹层逐层返回；根页面保持打开。手势沿用 Android 已有页面返回处理器，不依赖浏览器历史。证据：`mobile/ios/App/App/BridgeViewController.swift`, `mobile/e2e/ios-wait-layout.swift`；关联契约：`C-015`。
- 手机空输入框在真实运行时提供停止图标；输入文字、附件或引用后仍可跟进／插入，提问时回答、定时等待时发送。停止沿用 PC 的 interrupt 操作，不发送空消息。证据：`mobile/src/components/thread-stop.test.tsx`, `mobile/e2e/composer-layout.spec.ts`。

### F-221 Quote PC conversation text

- 目的：从手机对话选择原文并附在下一条消息；使用者：手机用户；入口：选中对话正文 → 选区附近的 Add to chat／加入对话，系统复制／全选菜单仍可用；输入：选区及可选请求；输出：可查看、编辑、移除引用，发送或插入后显示独立的引用块；前置条件：打开 PC 对话，选区在正文中；失败表现：无效选区不显示操作，发送失败恢复草稿；关联契约：`C-011`, `C-017`；实现证据：`mobile/src/components/thread-screen.tsx`, `mobile/src/components/composer.tsx`, `mobile/src/lib/quote.ts`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/composer-layout.spec.ts`。

### F-222 Inspect sub-agents on the phone

- 代理详情的生成中断说明沿用 `F-130`／`C-035`，配对推送与重开均保留原因和恢复状态。证据：`mobile/src/lib/transcript-generation.ts`、`mobile/e2e/generation-interruption.spec.ts`。

- 目的：在手机查看 PC 对话中的子 Agent 而不混入主 Agent 回答；使用者：已配对手机用户；入口：对话页 → 子 Agent／Agents → 子 Agent 行；输入：Pairlink 已允许的事件及更早历史页；输出：页头仅统计运行中的子 Agent，列表按进行中／已停止／已结束分组，保留完成与失败历史的角色、ID、状态、活动摘要及独立思考、工具、回答记录；页面返回、Android 系统返回或 iOS 左边缘右滑按详情→列表→对话逐级返回；列表在实时多段输出与历史分页后仍不显示主管正文或工作折叠；前置条件：PC 端有可回放的子 Agent 事件；失败表现：离线时沿用重连提示，未加载到启动事件前角色暂以 ID 显示；关联契约：`C-002`, `C-003`, `C-006`, `C-015`；实现证据：`mobile/src/lib/transcript.ts`, `mobile/src/lib/session.ts`, `mobile/src/components/thread-screen.tsx`, `mobile/src/components/thread-screen.test.tsx`, `mobile/src/lib/transcript.test.ts`, `mobile/src/lib/session.test.ts`, `mobile/e2e/walkthrough.spec.ts`, `mobile/src/components/agent-page.test.tsx`, `mobile/e2e/agent-roster.spec.ts`。
- 人工停止沿用 `C-002`：清理与当前代理的取消回执都显示已停止，重新打开和更早历史分页不改成完成或失败；详情保留诊断原因但不显示失败样式。真实成功／失败与显式续办保持独立。证据：`mobile/src/lib/transcript-stop.test.ts`、`mobile/src/components/thread-stop-roster.test.tsx`、`mobile/e2e/worker-stop-replay.spec.ts`；来源：Issue #85。

### F-223 Inspect local client tasks on the phone

- 目的：在已配对手机上只读查看 PC 的本地客户端任务；使用者：手机用户；入口：收件箱 → Clients → 工具分组和 More；输入：客户端任务页；输出：任务状态、记录及加载更多时的动效，任务详情覆盖手机视口且标题与返回按钮保持可用；前置条件：PC 已启用 Clients 且在线；失败表现：分页失败保留当前列表、显示错误并恢复 More；关联契约：`C-006`, `C-014`, `C-015`, `C-018`；实现证据：`mobile/src/lib/client-poll.ts`, `mobile/src/components/client-groups.tsx`, `mobile/src/app.test.tsx`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/composer-layout.spec.ts`, `mobile/e2e/ios-wait-layout.swift`。

- 主机 Clients 刷新复用未变化文件的解析行，按最新时间稳定排序一次；桌面和配对手机分页只复制可见行，追加、删除和文件替换在下一次刷新反映。实现／测试：`internal/clients/catalog_cache.go`、`internal/clients/catalog_cache_test.go`、`internal/clients/catalog_performance_test.go`、`frontend/e2e/clients.spec.ts`；关联契约：`C-014`。

### F-224 Browse and preview PC conversation files on the phone

- 目的：在已配对手机上查看 PC 对话的工作区产物；使用者：手机用户；入口：PC 对话页 → Outputs／产出物；输入：工作区列表、文件选择与搜索；输出：可折叠文件树、按后缀渲染的 Markdown／源码高亮／纯文本／常见图片／PDF 预览及其他文件的下载入口；前置条件：PC 在线且该对话存在；失败表现：RPC 错误、文件过大或不支持的预览明确显示；关联契约：`C-004`, `C-006`, `C-015`, `C-020`；实现证据：`internal/remote/files.go`, `internal/remote/files_test.go`, `mobile/src/components/file-browser.tsx`, `mobile/src/lib/remote-files.ts`, `mobile/src/lib/remote-files.test.ts`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/ios-wait-layout.swift`。
- 手机产出物初次加载与刷新后只显示第一层文件和目录；每层目录手动展开，搜索可直接找到折叠层级内的文件。证据：`mobile/src/components/file-browser.tsx`, `mobile/src/components/file-browser.test.tsx`, `mobile/e2e/walkthrough.spec.ts`。

- 手机文本产出物按后缀选择 Markdown 文档或现有源码语言高亮；未知文本与超过 256 Ki 字符的文本完整显示为纯文本。HTML/SVG、MDX 组件不执行，原文件下载保持不变。证据：`mobile/src/components/text-file-preview.tsx`, `mobile/src/components/text-file-preview.test.tsx`, `mobile/src/components/file-browser.test.tsx`。

### F-230 Direct model chat

- 新旧 Chat Completions 推理字段兼容沿用 `F-170`／`C-009`；通过直连的同一思考区块显示，字段名称不影响正文。证据：`mobile/src/lib/openai-wire.ts`、`mobile/e2e/direct-chat-reasoning.spec.ts`。
- 用户与流式回答复用 `F-220` 的手机 Markdown 代码围栏渲染及原文复制，遵守 `C-032`；证据：`mobile/src/components/direct-chat-screen.tsx`, `mobile/e2e/markdown-highlight.spec.ts`。
- 目的：手机在没有 PC 时直接连接模型，并在应用内按服务商选择模型；使用者：手机用户；入口：Models、Chat 和输入框模型按钮；输入：兼容端点、所选模型、消息及附件，Models 里可把某个接口从输入框列表里关掉；输出：流式回答，长模型名和长输入不挤出发送按钮，关掉的接口不出现在列表里，已经选中它的对话继续用；前置条件：端点可访问；失败表现：连接或模型错误；关联契约：`C-002`, `C-006`, `C-009`, `C-013`, `C-015`, `C-017`；实现证据：`mobile/src/components/direct-chat-screen.tsx`, `mobile/src/components/model-picker.tsx`, `mobile/src/components/composer.test.tsx`, `mobile/e2e/composer-layout.spec.ts`, `mobile/src/lib/openai-client.ts`。
- 直连流式回复期间输入框主按钮显示可点击停止图标，取消后保留已收到的回复并恢复发送。证据：`mobile/src/components/direct-chat-screen.tsx`, `mobile/src/components/direct-chat-screen.test.tsx`。

### F-240 Phone updates

- 目的：发现并安装 Android 可用版本；使用者：Android 手机用户；入口：菜单 Check for updates 和更新提示；输入：最近一个含匹配 APK 的 GitHub Release；输出：Android 安装请求；前置条件：网络和新版本；失败表现：手动检查显示错误。iOS 测试版由 TestFlight 分发和更新，应用内不显示 GitHub 检查入口；关联契约：`C-010`；实现证据：`mobile/src/components/host-chrome.tsx`, `mobile/src/components/update-notice.tsx`, `mobile/src/lib/app-update.ts`, `mobile/e2e/version-check.spec.ts`, `mobile/ios/App/AppUITests/BindFlowTests.swift`。

### F-310 HTTP API and live events

- 缺失记录诊断：404 区分会话不存在、排队消息已离队和通用记录缺失；附请求 ID、方法、路由和可用的记录身份，使用同 ID 在默认 WARN 日志定位，避免裸存储哨兵。关联契约：`C-012`；证据：`internal/server/missing_record.go`、`internal/server/missing_record_test.go`。

- 目的：给 PC 客户端提供相同服务；使用者：内置客户端及本机调用方；入口：`/api`、SSE、WebSocket；输入：同源请求；输出：状态和事件；前置条件：引擎在线；失败表现：HTTP 状态和 code；关联契约：`C-002`, `C-003`, `C-005`；实现证据：`internal/server`, `docs/API.md`。

### F-320 CLI commands

- 目的：从 shell 启动、诊断和管理对话；使用者：终端用户；入口：`zwai` 子命令；输入：命令与参数；输出：终端文本或客户端；前置条件：可运行二进制；失败表现：命令错误；关联契约：`C-001`, `C-012`；实现证据：`cmd/zwai`, `docs/CLI.md`。

### F-330 Swarm library

- worker 收取状态：`Registry.PendingAgents` 非消费地列出运行中及已完成未收取的 worker，保留精确执行 `generation`；`wait_agents`／`close_agent` 的回执及 checkpoint 的 `Generation`、`Collected`、`CloseRequested` 绑定本次执行，查询与归档不得丢失待收取结果，resume 开始新 generation；缺 generation 身份的旧 finished roster 保持已结束兼容，旧 running 仍需收取。关联契约：`C-019`、`C-027`；证据：`settlement.go`、`tools.go`、`handoff.go`、`restore.go`、`settlement_http_test.go`。

- 逐次生成生命周期通知沿用 `F-130`／`C-035`；公开通知与输出身份见 `docs/LIBRARY.md`，验证：`generation_test.go`。

- worker lifetime 默认一小时，已有显式正值优先；live registry 用同步 `SetAgentTimeout`、`SetMaxTurns`、`SetMaxConcurrent` 更新后续执行，运行中 deadline 与三次 watchdog 恢复预算不变。关联契约：`C-009`、`C-019`；实现／验证：`swarm.go`、`worker.go`、`timeout_settings_test.go`、`worker_timeout_test.go`、`docs/LIBRARY.md`。
- 模型输出通知提供可选 `MessageID`，同一流的思考、正文与 checkpoint 保留身份，新模型轮次使用不同身份；无身份的旧通知保持兼容。关联契约：`C-003`；证据：`signal.go`、`signal_identity_test.go`、`docs/LIBRARY.md`。
- 进程交接 API 与恢复边界见 `F-110`、`C-027`；库使用说明：`docs/LIBRARY.md`；证据：`handoff.go`、`handoff_test.go`。

- 目的：让 Go 程序嵌入 manager 与 sub-agent；使用者：Go 开发者；入口：模块公开 API；输入：registry、provider 和任务；输出：结果与通知；前置条件：依赖可编译；失败表现：Go 错误或结构化工具错误；关联契约：`C-003`；实现证据：`swarm.go`, `run.go`, `docs/LIBRARY.md`, `examples`。

### F-410 Local conversation storage and search

- 目的：持久保存对话、事件与检索索引；使用者：PC 用户；入口：对话列表和搜索；输入：事件与查询；输出：可恢复历史和结果；前置条件：数据目录可写；失败表现：存储或索引错误；关联契约：`C-003`, `C-008`；实现证据：`internal/store`, `internal/search`, `docs/DATA_MODEL.md`。

### F-420 Configuration and project memory

- 用户触发的“整理记忆”支持原笔记升为技能、无步骤短技能降为笔记；完整保留源文字、唯一匹配、预算与目标冲突校验后先存目标再删源，部分失败记录已保存内容，重试按已有最终状态幂等。自动复盘／worker／共享库不获搬家权限。结果显示升降类计数与流式动作，有既有 turn 时动作和陈旧名单进入同 turn Trace。失败摘要按已提交笔记及完整搬家回执判断保存结果，不将仅创建技能的半完成提升当作笔记已保存。入口：项目 Memory 整理按钮、既有 tidy-skills API；关联契约：`C-025`、`C-012`；实现证据：`internal/memory/reclassify.go`、`internal/engine/review_tidy.go`；测试证据：`internal/engine/review_reclassify_test.go`、`internal/server/memory_reclassify_test.go`、`frontend/e2e/memory-boundary.spec.ts`、`frontend/e2e/memory-tidy-partial.spec.ts`。

- 技能查看统计：manager/worker 的成功 skill_view 累加 usage.json，列表、HTTP 查看及复制不计数；面板显示次数、最近查看与可编辑阈值的陈旧 Badge。merge 累加来源统计、delete 删除边车、copy 从零开始，正文修改时间和摘要不变。整理陈旧候选仅供模型判断，候选名单随结果／原 turn Trace 保留。入口：Memory、Settings → Memory、skill_view；关联契约：`C-025`、`C-012`；实现证据：`internal/memory/skill_usage.go`、`internal/engine/review_tidy.go`、`internal/server/projects.go`；测试证据：`internal/memory/skill_usage_test.go`、`internal/server/skill_usage_copy_test.go`、`internal/engine/review_stale_test.go`。

- 笔记以“不知道会走偏”为判据，常驻注入偏好、事实、硬约束及已有技能指针；技能以“不知道如何做成”为判据，按需加载适用条件、步骤、验证及失败分支。仅某流程相关的约束留在技能前置条件；description 写适用时机。指针仅在原样引用已有技能名且未复述正文时豁免重叠拒绝；纯文本中的不存在名称不能取得豁免。关联契约：`C-025`；实现证据：`internal/memory/prompt.go`、`internal/memory/library.go`、`internal/memory/tools.go`、`internal/memory/skills.go`；测试证据：`internal/memory/prompt_test.go`、`internal/memory/store_test.go`。

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

- 显式紧急发布使用 `tools/release.py --urgent-reason <reason>`，保留当前人类授权原因，仅放宽常规批次等待及 TestFlight 日发布频率；原生与交付验收仍独立生效。关联契约：`C-010`；证据：`tools/test_release_urgent.py`、`tools/test_release_ios_urgent.py`。

- 已验收 Android APK 的统一发布恢复：批次 `release_android_validation` 绑定通过状态、版本、完整源码 SHA、附件名与 SHA-256；上传前核对并直接复用同一 APK，不重新构建。缺文件、变更、非法回执或 ready 状态缺回执拒绝上传，已有远端附件仍按 digest 恢复。入口：`tools/release.py --platform android`；关联契约：`C-010`；实现／验证：`tools/release.py`、`tools/test_release_android_retained.py`、`docs/RELEASE.md`；来源：Issue #109。

- 原生 campaign 控制模块为内部调用方提供可选持锁 admission：真实取得本 campaign 的 `driver.lock` 后、首次写 owner 前运行；检查失败必须抛出异常，返回值不参与准入判定，拒绝不调用场景或改写 owner／terminal，所有退出路径关闭本次锁。旧记录拒绝策略只约束显式使用该 hook 的调用方，默认 full6 launch／resume 行为不变。关联契约：`C-031`；实现／验证：`tools/native_macos_resilience_control.py`、`tools/test_native_macos_driver_admission.py`、`tools/test_native_macos_resilience_control.py`；来源：Issue #94。
- 内部 launch 可在双日志打开后、真实发布 driver 前执行一次 raising admission；实际 main 仅向 launch 传递，默认 CLI 和 run／resume 不变。拒绝保留 claim／日志、不发布进程，不用先前外层视觉检查掩盖最后 I/O 的过期。campaign 还保留 before／after／实际 sent 下载原体与原 HTTP MIME／元数据，accepted_stage 离线重读核验原字节、同 case 作用域与解压图像尺寸；输入证据模块也受 fingerprint 约束。这些 HOST 适配测试不等于原生恢复或 full6 通过。关联契约：`C-031`；实现／验证：`tools/native_macos_resilience_control.py`、`tools/native_macos_input_evidence.py`、`tools/test_native_macos_driver_admission.py`、`tools/test_native_macos_input_evidence.py`；来源：Issue #94。
- 内部 sleepwake 可在原 wake 日志保存后执行可选 `after_wake(context, cycle, deadline)`，与随后恢复检查共用原周期 Deadline；慢 I/O 不累计新预算，snapshot 固定 8 秒叶边界和 pmset／周期数／清理不变。默认无 hook，不据此声称 actor 已接入或原生通过。关联契约：`C-031`；实现／验证：`tools/native_macos_resilience.py`、`tools/test_native_macos_sleepwake.py`；来源：Issue #94。

- 原生恢复的每次 `verify_draft` 草稿恢复检查保留首个不成立观察和最后观察，区分 false、未知错误及未读取；只记录原调用已有值的类型、摘要和匹配判定，不复制正文或增加输入／请求。退出采证不得覆盖原异常，成功缺必要收据仍拒绝。关联契约：`C-031`、`C-034`；实现／验证：`tools/native_macos_resilience.py`、`tools/native_macos_resilience_phases.py`、`tools/test_native_macos_restore_observation.py`；来源：Issue #94。

- 离线恢复 proof 准备逐项尝试保留原命令输出与退出／超时收据；写证失败不能覆盖原退出／超时，次级失败仅以文件名、操作、错误类型和 errno 留在异常属性与 stderr JSON 中，stderr 再失败仍保首因。命令成功但必要证据写入失败仍拒绝，不生成 proof。关联契约：`C-031`；实现／验证：`tools/native_macos_recovery_identity.py`、`tools/test_native_macos_recovery_identity.py`；来源：Issue #94。

- 原生恢复 QA 激活保留原 case bundle 锚，从实际内核 executable 推导原包或同 case 的严格 recovery-cache 继任包。产品 writer 会重签副本；不同原始字节通过离线实际 writer／签名系统生成并冻结的 proof 绑定原 core、派生 core、Info/icon、source/version，不能从待接受 target 反推。动作前核对 session、全文件 SHA、继承 marker、NS PID／bundle，动作后核对 birth／executable 不变。仍仅一次激活、无 AX、原期限；不代表产品恢复期限或整包门禁通过。关联契约：`C-031`、`C-034`；实现／验证：`tools/native_macos_resilience_ax.m`、`tools/native_macos_recovery_identity.py`、`tools/native_macos_recovery_writer_test.go.txt`、`tools/test_native_macos_recovery_identity.py`、`tools/test_native_macos_successor_identity.py`、`tools/test_native_macos_activation.py`；来源：Issue #108。

- 原生故障 QA 对完整 cohort 已选出的系统 WebContent／GPU 可执行文件执行严格全架构代码验证与 inline `anchor apple`／精确服务 identifier，明确不验证资源或整个 bundle；原发行 App 深度严格验证不变。成功／失败／超时均持久保留命令、case／PID／birth／路径及原始输出，未知或写证失败不得准入或发送信号，之后仍重新核对完整 cohort 与内核身份。入口为 renderer／GPU 原生恢复 campaign；该门禁不代表恢复场景已通过。关联契约：`C-031`；实现／验证：`tools/native_macos_resilience_fault.py`、`tools/test_native_macos_signature_admission.py`、`tools/test_native_macos_resilience_fault.py`、`tools/test_native_macos_fault_cleanup.py`；来源：Issue #107。
- 原生 worker 夹具将引擎的精确 System 调度答复协议与 manager／worker 分开：仅对本夹具三类固定 manager 答复按独立非流式、无工具、两消息请求返回空 claims；未知候选或坏形状返回 HTTP 400，不消费 worker 请求、改变 spawn 回执或释放已有流。入口为原生恢复 campaign 的 keyless loopback 模型；仅接受 QA 协议适配，不代表 App 原生验收或发布。关联契约：`C-031`；实现／验证：`tools/native_macos_resilience_worker.py`、`tools/test_native_macos_schedule_answer.py`、`tools/test_native_macos_resilience_worker.py`、`docs/TESTING.md`；来源：Issue #106。
- ci-macOS 访问分类与来宾维护：宿主入口 `python3 tools/native_macos_access.py --profile <private.json> --evidence <new-directory>` 复用私有身份配置，默认只读检查选定 MAC 的 DHCP 地址／实际路由、严格固定 host key 的 SSH，以及新鲜共享队列 fallback；`--repair-network` 只在当前 peer 盘点及实际 native-case 共锁下续租既有 DHCP 一次，后续 pinned SSH 未验证仍保留失败，不自动重启。来宾入口 `python3 tools/native_macos_access_guest.py inspect|renew-dhcp|hold-maintenance ...` 核对 VM／用户／接口 MAC，提供网络／sshd 状态、当前 controller／child 盘点和有界维护持锁；peer claim、未知历史效果或前例未清理时拒绝。纯分类器按本次 probe 解释状态，不按旧 IP／跨机器墙钟判整体不可控。请求超时保留 ID 和未知效果，不自动重试；连接成功仍需独立原生窗口验收。关联契约：`C-031`、`C-010`；实现／验证证据：`tools/native_macos_access.py`、`tools/native_macos_access_state.py`、`tools/native_macos_access_guest.py`、`tools/test_native_macos_access.py`、`tools/test_native_macos_access_state.py`、`tools/test_native_macos_access_guest.py`、`docs/TESTING.md`；来源：GitHub Issue #97。
- TestFlight 严格可用性读取本应用的配置组白名单，分页交叉核对各组的 builds 与公开 `betaGroups?filter[builds]` 集合；不组合两个关系过滤器，不调用未支持的 build→betaGroups GET。首次公开证明仍为 schema 1，旧冻结证明保持不变；只读预检不写成功交付。关联契约：`C-010`；证据：`tools/release_ios.py`, `tools/test_release_ios_groups.py`, `tools/test_release_public_recovery.py`, `docs/RELEASE.md`。
- smoke 与 resilience 的窗口操作先核对实际 AX 类型、`AXWindow` role 和本例 owner PID，并保持对象的动作期所有权；proxy、未知和错误进程对象不能触发 raise 或窗口输入。resilience application-only 激活不读取或操作 AX 窗口，后续仍须新鲜窗口及像素证据。smoke 的无窗口 snapshot 保留 not ready 语义；仅 snapshot 在内核 case 已核但原生注册尚不可用时返回明确 pending，不激活或访问 AX，沿用原窗口期限；fill／Send 和错误身份仍拒绝。独立附件面板规则不变。关联契约：`C-031`；实现／验证：`tools/native_macos_ax.m`, `tools/native_macos_resilience_ax.m`, `tools/test_native_macos_activation.py`, `tools/test_native_macos_smoke.py`, `docs/TESTING.md`；来源：Issue #105。

- 快速原生场景通过 `make native-smoke-prepare` 预编译直接 AX helper，记录源码／构建参数／二进制身份；计时场景只验证收据，不冷编译。helper 核对本例 PID、WebView origin、desktop shell，先读回完整输入、核对唯一且可用的 Send，再按该控件；截图绑定本例窗口所有者。bundle 以实际存在且解析后相同的目录核验，允许 Darwin 路径别名；每次 helper 返回先保存原始输出及退出码到 `smoke-native-last.json`，再进行解析和身份校验。AX 图按元素身份去重，空节点和重复边计入遍历预算，真实超限仍失败。每次 helper 动作调用内激活仅请求一次，以同一 monotonic deadline 内的主 runloop 刷新确认；失败保留本次谓词和 Press 尝试状态，不自动重发。关联契约：`C-031`；实现证据：`tools/native_macos_ax.m`, `tools/native_macos_resilience_ax.m`, `tools/native_macos_smoke.py`, `tools/test_native_macos_smoke.py`, `tools/test_native_macos_activation.py`, `Makefile`。
- 已完成的隔离生命周期验收及证据范围：[2026-10-21 ci-macOS 记录](diagnostics/native-isolation-20261021.md)，关联 `C-031`。
- 原生附件选择保留本次 helper 的完成输出或超时原始字节、请求身份和退出状态；收尾截图失败不能覆盖首次选择超时，成功证据写入失败拒收，不自动重复输入。面板每个独立扫描节点以固定五属性批量读取，分别校验整体结果、逐项 AX 错误和独立的 CFNull 缺值，不把负数当错误或缺值伪成 NoValue；根枚举、完整图／唯一 Open、5000 访问及 45 深度边界保留。成功字符串提供文件名存在证据，找到后停止消费无关文本，但固定批仍请求这些属性。必需结构／Button Title 的真实读取或类型失败、遍历超限仍拒收，原有明确无值边界保留。辅助文本失败记为保原状态的 unknown，仅在另有成功 witness 且图完整／Open 唯一时允许继续；witness 不等于 selected row，最终原字节及保存 ACK 仍必需。批量调用不保证原生耗时，不延长八秒上限。关联契约：`C-031`；实现／验证：`tools/native_macos_resilience.py`, `tools/native_macos_resilience_ax.m`, `tools/test_native_macos_choose_evidence.py`, `tools/test_native_macos_attachment_scan.py`, `tools/test_native_macos_panel_batch.py`。
- 已完成的日常一分钟验收：[2026-10-22 三轮记录](diagnostics/native-minute-20261022.md)，含独立核验的整轮耗时为 14.50 / 12.30 / 12.06 秒，均有真实窗口、离线 Trace 和清理证据；仅接受记录中的冻结应用及 harness 输入，关联 `C-031`。

- TestFlight 首先公开且 GitHub 尚无 tag 时，发布入口交叉核对冻结批次、精确源码/build/营销版本/IPA 与首次公开时的严格组／审核证明，恢复同版本的其余端。旧状态无该证明时只读核验 Apple，实际恢复持锁落证，预检不写；已有证明不因 iOS 后来过期或 Apple 不可达而阻断其他端。新批次门槛和各端原生门禁仍独立生效。证据：`tools/release.py`, `tools/release_ios.py`, `tools/test_release_public_recovery.py`；契约：`C-010`。

- 日常原生回归入口：来宾已启动、候选已构建和传输后，`make native-smoke APP=<app> EVIDENCE=<new-directory>` 在全新隔离状态中自动等待本地就绪、验证离线 worker/tool/Trace、原生窗口内容和截图，再清理本例。runner 默认整轮 `--budget 60`，`--timeout` 仍为受剩余预算限制的场景上限；`cleanup.json` 保留阶段与总耗时，超预算不得返回成功，清理不得被截止时间截断。冷构建／传输和长恢复另记范围与预算；实际一分钟验收需要来宾窗口及清理证据。关联契约：`C-031`；实现证据：`Makefile`, `tools/native_macos_case.py`, `tools/native_macos_smoke.py`, `tools/test_native_macos_case.py`, `tools/test_native_macos_smoke.py`。
- 原生桌面用例：`python3 tools/native_macos_case.py --app <app> --evidence <new-directory> --timeout <seconds> -- <scenario-command>` 仅允许在 macOS 虚拟机运行；维护者选择 `ci-macOS`，串行运行各场景，每例独立 app/home/cache/data/workspace，场景继承私有环境，在成功、失败、超时和取消后停止本例进程再删除数据及缓存，持久保留日志和 `cleanup.json`；物理宿主、并发或未清理的前例拒绝启动。窗口行为仍由场景及真实来宾证据验收。关联契约：`C-031`；实现证据：`tools/native_macos_case.py`, `tools/test_native_macos_case.py`, `docs/TESTING.md`。
- 实际启动目录核验：`python3 tools/check_checkout_delivery.py --checkout <absolute-path> --required-commit <sha> [--url <loopback-origin>]` 只读检查修复提交是否进入指定目录的 HEAD，可选比较该服务的全部 module script 与该目录 dist 的哈希；允许未提交改动，失败分项非零退出。原生行为在 `ci-macOS` 中按相同 HEAD、脏文件内容及构建身份的私有副本运行报告命令验收，不能以核验器成功或宿主已运行进程替代；宿主源码交付与来宾行为结果分别记录。关联契约：`C-010`, `C-031`；证据：`tools/check_checkout_delivery.py`, `tools/test_check_checkout_delivery.py`, `tools/native_macos_case.py`。
- iOS 发布预检读取应用源码中经授权的持续合规声明；加密用途不变的普通更新无需重复问答，签名 IPA 及精确回执发生冲突时拒绝发布。无源码字段的旧批次仍读取私有精确确认。证据：`mobile/ios/App/App/Info.plist`, `tools/release_ios.py`, `tools/test_release_ios_compliance.py`, `docs/RELEASE.md`。
- Mac 原生窗口验收后，发布恢复核对私有批次回执的版本、源码 SHA、文件名与 SHA-256，上传同一 ZIP；文件缺失或变化时保留待发布状态。证据：`tools/release.py`, `tools/test_release.py`, `docs/RELEASE.md`。
- Mac 原生验收恢复的离线入口 `python3 tools/audit_macos_acceptance.py --observation <private.json>` 接收无凭据的来宾状态、登录结果与精确候选窗口回执，输出待解锁、待窗口验收、真实登录阻塞或同包验收通过。ready 来宾的当前证据同时证明 SSH helper 权限拒绝及既有授权 Terminal 控制路径可用时，精确匹配来宾／候选后输出 pending / `inspect_native_window_via_guest_terminal`，由调用方在原隔离锁下执行。恢复待验收工作时先以 `guest.state=unknown` 重新观察来宾；unknown 优先于残留的旧登录成功／失败结果，要求重新观察，不直接阻塞或跳到窗口检查。宿主锁屏不阻断就绪来宾，已完成的同包真实验收不因后续锁屏失效，SSH/进程成功或管理员身份不算窗口验收／隐私权限；防休眠断言也不替代本次会话和窗口观察；恢复控制的输入 actor 退出与有限 power owner 交接按原 case 锁、进程清单、真实 ACK 和清理规则分别核验，不新增公共模式。工具不执行登录、SSH、权限变更或发布，也不替代真实证据检查。关联契约：`C-031`；证据：`tools/audit_macos_acceptance.py`, `tools/test_audit_macos_acceptance.py`, `docs/RELEASE.md`。
- 同一处理批次先收口各 worktree 的 Issue 主线集成，再为最终完整主线分配一个版本；新版本发布入口核对批次收口回执的源码、Issue 和平台，并在仍有活跃 Issue 时拒绝上传。证据：`AGENTS.md`, `tools/release.py`, `tools/test_release.py`, `docs/RELEASE.md`。
- 目的：从一个已验收、已分配版本的完整主线批次发布实际受影响的 macOS、Android 和 iOS 端；使用者：维护者；入口：`make release`, `make release-check`, `make mobile-ios-release`, `python3 tools/audit_pending_batch.py`；输入：干净源码、统一版本/SHA、签名配置（可选 `marketing_version` 显式开启新 TestFlight 版本）、测试组与源码中的授权应用合规声明（无字段的旧源码使用按 SHA/build 持久保留的精确确认）、交付台账及经验证的额外主线平台变更；输出：所需平台的 GitHub macOS zip/APK、TestFlight 构建与组/审核状态（常规更新沿用最近成功版本，仅增加 build number；已有候选冻结版本；同版本仍可能审核）、逐 Issue 交付结果，以及未分配变更的提交数、等待时长和发布平台；旧版仍待交付时保留冻结批次并独立恢复；前置条件：Go、Node、SDK、gh、私有 iOS CLI/签名资料，主线自上个公开版本超过三次提交或最早待发布修复等待超过 24 小时；失败表现：各端独立报错或等待，未交付时退出非零；关联契约：`C-010`；实现证据：`Makefile`, `tools/release.py`, `tools/release_ios.py`, `tools/test_release.py`, `tools/test_release_ios_versions.py`, `tools/audit_pending_batch.py`, `tools/test_audit_pending_batch.py`, `internal/release`, `internal/desktop/pack`, `mobile/scripts/android-release.ts`, `docs/RELEASE.md`。
