# Current Contracts

Only current behavior is listed here. See [FEATURES.md](FEATURES.md) for user entry points and [CHANGELOG.md](../CHANGELOG.md) for history. Contract IDs are stable.

## C-033 Composer drafts are durable versioned snapshots

- 状态：active；类型：data / consistency / lifecycle / security；作用范围：PC 输入框、草稿 API、SQLite 和首发迁移；关联功能：`F-145`, `F-140`, `F-180`。
- 契约内容：草稿以 `thread:<id>`、`new:<project-id>` 或 `new:home` 为身份，不依赖 HTTP 端口或浏览器 origin。正文、引用、附件 manifest 和新增原始 bytes 原子提交；已保存附件使用同 scope 的不可变 ID，后续键入不重传 bytes。保存与清理均以 expected revision 做 CAS，冲突返回 `409 draft_conflict`，不覆盖另一窗口。
- 引用校验：保存事务必须确认既存附件 bytes 仍存在且长度与 manifest 一致；缺失或长度变化拒绝保存，正文、revision 和同批新增 bytes 全部回滚。长度校验不宣称能检测等长存储损坏。
- 允许行为：保留未确认输入、显式解决冲突、发送结果未知时先检查对话；禁止行为：保存失败仍称已保存、发送 ACK 前清稿、自动重发未知结果、按文字相似度推断发送成功或跨 scope 取附件。
- 失败语义：恢复、保存、上传、发送或 CAS 清理失败必须可见并保留当前内容。只能保证已收到持久化 ACK 的版本可恢复；最后尚未 ACK 的修改不得伪称已落盘。`sending` 在重开后按结果未知处理。首发复制保留源稿直到发送确认，旧原生归属也能找到完整副本。
- 不变量：发送固定目标会话；每个成功 ACK 只清对应 revision，tombstone 防止旧保存复活。scope 所属会话／项目必须在保存事务内存在；删除所属对象同时清理其草稿与 bytes，迟到保存不得复活它。
- 文本动作：PC `/goal`、`/plan` 不携带附件或引用；含这些内容的草稿必须在发送前可见拒绝并完整保留，不能用仅文本动作的 ACK 清除整份快照。
- 缓存生命周期：无订阅、操作或待写入且已确认保存的闲置 scope 释放 File 缓存和预览 URL，返回时从 SQLite 重读；未保存、冲突或发送中的唯一副本不能被淘汰。首发迁移和发送必须持有跨导航所有权，不能依赖组件及时挂载。
- 边界条件：正文与引用合计 1 MiB；32 个附件、总量 256 MiB；图片最多 8 张且每张最多 8 MiB。原始 bytes、MIME、名称、最后修改时间保留；blob URL 不持久化。附件下载强制 attachment、nosniff、no-store。工作区上传原有单文件限制不变。
- 证据：实现 `internal/store/composer_draft.go`, `internal/server/composer_draft.go`, `frontend/src/lib/composer-draft.ts`, `frontend/src/lib/use-composer-draft.ts`, `frontend/src/components/app/composer.tsx`, `frontend/src/store/app.ts`；测试 `internal/store/composer_draft_test.go`, `internal/server/composer_draft_test.go`, `frontend/src/lib/composer-draft.test.ts`, `frontend/src/components/app/composer-draft.test.tsx`。
- 变更规则：同步 API、DATA_MODEL、README、FEATURES、TESTING、CHANGELOG；回归 ACK 丢失、跨 scope／首发迁移、附件逐字节、可解码图片、CAS 和实际原生恢复。
- 缓存证据：`frontend/src/lib/composer-draft-cache.test.ts`、`frontend/e2e/composer-draft.spec.ts`。
- 来源：GitHub Issue #94；Added，修复白屏恢复丢失未发送内容及发送前清稿。

## C-034 Native renderer recovery is bounded and preserves the engine

- 状态：active；类型：lifecycle / consistency / security；作用范围：macOS 窗口、启动器、原生快照和恢复诊断；关联功能：`F-115`, `F-110`, `F-130`, `F-160`, `F-430`。
- 契约内容：健康探测必须验证当前 64 位随机挑战对应的完整有序像素图案；相近颜色的旧图案、HTTP、JS 心跳、动画帧或截图回调本身不足以证明本次绘制。raw 消息只接受同源主 frame、当前 session／generation／challenge，迟到回调不得更新新代际健康。
- 恢复边界：启动宽限 30 秒，活动窗口健康期限 20 秒；先一次重载，等待最多 15 秒，仍失败才启动继任窗口。每个连续故障最多两次替换，预算跨进程传递；至少连续健康 5 分钟后可重置。继任启动与确认总截止 30 秒，失败必须可见并保留诊断，禁止循环复活。休眠、最小化、失焦暂停检测，返回时给予宽限；长时间定时器停顿不记为故障。
- 不变量：继任仅附着原 PID／URL／租约／模式的引擎，不调用引擎交接、重跑任务或改变 turn／worker 身份。原窗口在继任真实绘制就绪、commit 和 committed 回执完成后才退出；取消、失败与用户正常退出只处理本次拥有的子进程及私有副本。新窗口必须登记自己的真实 PID。
- 身份保护：未打包的 checkout 启动使用按源可执行文件 SHA-256 的不可变缓存包，锁内完整构建后原子发布；复用不覆盖或重签 live bundle。恢复继任使用本会话私有副本和继承管道，不因另一次构建改变源码身份。
- 诊断：同一 session／generation／attempt 记录阶段、原因、shell／engine PID、耗时和真实 thread／turn 归属；本机 journal 有 64 KiB 上限。有效回合的 `desktop_recovery` 事件进入同一 Trace 与 Full log，保留原事件身份及 rewind 规则，不创建 worker、改变任务状态或把内部 JSON 显示为主转录正文；写失败不得确认持久回执，无回合仅写元数据日志（可关联已存在会话），不编造任务。原因最多 256 字节有效 UTF-8；日志不含正文、附件、截图像素或握手令牌。
- 允许行为：在系统 WebKit 尚有故障时有界恢复界面；禁止行为：把恢复机制称作系统缺陷已消除、只用进程／API 就绪接受窗口、在宿主桌面做原生故障验收。
- 证据：实现 `internal/desktop/recovery.go`, `internal/desktop/recovery_state.go`, `internal/desktop/recovery_probe.go`, `internal/desktop/recovery_snapshot_darwin.m`, `internal/desktop/bundle_darwin.go`, `cmd/zwai/desktop_recovery_darwin.go`, `internal/engine/desktop_recovery.go`, `internal/server/desktop_recovery.go`；测试 `internal/desktop/recovery_test.go`, `cmd/zwai/desktop_recovery_test.go`, `internal/engine/desktop_recovery_test.go`, `internal/server/desktop_recovery_test.go`, `tools/test_native_macos_resilience.py`。
- 变更规则：同步 ARCHITECTURE、CLI、API、FEATURES、TESTING、CHANGELOG；正式 ZIP 在 `ci-macOS` 验证渲染器／GPU 活挂、草稿／附件、后台任务连续性、真实睡眠唤醒和至少 18 小时长开，保留准确源码／ZIP 身份及独立 cleanup 回执。
- 来源：GitHub Issue #94；Added，弥补仍存活的 WebContent 绘图挂起不能由终止回调恢复的缺口。

## C-022 Single PC work rows use one disclosure

- 状态：active；类型：lifecycle / compatibility；作用范围：PC 主对话用户视图；关联功能：`F-130`。
- 契约内容：连续工作区块只有一段思考或一个可见工具调用时，直接渲染该条自身的折叠，不增加外层聚合；多项继续聚合，回答保持可见并分隔工作区块。单段流式思考默认展开，用户可手动收起；单个工具沿用自身的加载、折叠与失败表现。
- 允许行为：开发者视图继续逐条显示；禁止行为：单项需要先打开聚合再打开内容；失败语义：单个失败工具保留错误标记与可展开结果；不变量：事件顺序、Trace、正文可见性与 Find 展开不变；边界条件：忽略隐藏元数据后计数，单项增长为多项时聚合；自身没有可见工具行的 pending `spawn_agent` / `close_agent` 保留活动 ticker；手机折叠不在本契约范围内。
- 证据：实现 `frontend/src/components/app/work-fold.tsx`；测试 `frontend/src/components/app/transcript-fold.test.tsx`, `frontend/src/components/app/work-fold.test.tsx`, `frontend/e2e/work-fold.spec.ts`；变更规则：同步用户文档、渲染回归和 UI E2E；来源：用户 2026-10-08 单项工作区块无需两次展开的要求。

## C-001 One engine per data directory

- 状态：active；类型：lifecycle；作用范围：desktop、web、tui、engine；关联功能：`F-110`, `F-120`, `F-320`。
- 契约内容：同一数据目录只由一个 engine 进程持有；客户端连接同一 loopback 服务；不同版本或开发二进制身份替换引擎时先进行有界持久化交接并自动恢复。持锁但尚未发布地址的引擎必须在地址出现后经过同一身份和活动 turn 检查；子进程启动输给竞争者时也不得直接接受竞争者的 URL。身份/状态检查或检查点保存失败时不得假称已安全交接；明确报告错误并保留可重试的恢复现场。
- 就绪和失败边界：前端准备后的普通冷启动最多等待 2 分钟，每 10 秒报告进度；替换引擎仍受 `C-027` 的统一 30 秒预算约束。同步元数据探测后必须重新检查取消，迟到的成功或失败都不能覆盖调用方的取消类别；证据：`cmd/zwai/engine_startup_cancel_test.go`。启动失败必须保留实际子进程退出错误，日志仅取本次启动后追加的部分，不得用旧模型错误充当本次原因。取消／超时必须回收本次启动的子进程，不得终止竞争 lease 的其他持有者。本地就绪不得等待可选手机 hub 注册，turn／worker 恢复与调度仍有序完成。证据：`cmd/zwai/engine_boot_test.go`、`cmd/zwai/engine_restart_budget_test.go`、`internal/app/startup_remote_test.go`。
- 单次健康探测边界：`lease.Discover` 在 `ErrUnreachable` 时保留已解析的未验证租约，仅用于将 PID 与本次启动的自有 child 关联，或在 presence 握手失败后判断原租约记录已经替换；错误记录不能作为附着地址、版本或模式已验证的证据。仅当 PID 属于本次 child 且尚未观察其退出时，启动器可在原总截止时间内继续探测；成功元数据校验仍是附着前提。竞争 holder 的不可达与模式冲突保持可判断错误并立即返回，不能借此终止竞争进程。证据：`internal/lease/lease.go`、`internal/lease/lease_test.go`、`cmd/zwai/engine_boot.go`、`cmd/zwai/engine_boot_test.go`。
- 前端准备边界：当前 checkout 的 `frontend.Load` 在引擎发现／交接／启动前、同一可取消启动上下文内执行；准备失败或取消不得冻结／停止旧引擎或启动新 child。准备耗时不消耗随后 30 秒交接／恢复预算，后续进程仍须完成自身构建／启动校验，不保证准备后不会发生新的故障。证据：`cmd/zwai/engine.go`、`cmd/zwai/engine_frontend_test.go`。
- 允许行为：多个 shell 同时连接；禁止行为：多个进程同时写同一数据库；失败语义：引擎不可达或租约冲突需明确报告；不变量：一个目录一把 lease；边界条件：进程异常结束后可恢复。
- 原生窗口启动：返回窗口 URL 前必须得到真实 presence SSE 的确认，由 native 进程持有到退出；页面 reload 不释放该连接，前端不登记第二个 desktop 客户端。最终 idle 停止使用与 watcher 相同的 `Engine.ActiveWork`，复核 manager、parked worker 及重启交接归属，不能只看运行中的 manager 列表；与 presence 接入按同一互斥锁决策：接入先成功则不得停止；停止先提交则 POST/GET 返回 `503` 和 `engine_stopping`。握手及原持有人退出等待有界；仅证明原租约记录消失或其进程／启动／二进制身份变化后重新走完整身份/单写入者检查；不同 PID 复用相同 URL 也必须识别为替换，不能用端口代替持有人身份。不能用延长 idle grace、等待用户手动重开或杀掉仍持锁但不响应的引擎替代该边界。证据：`cmd/zwai/desktop_attach.go`、`cmd/zwai/desktop_attach_test.go`、`cmd/zwai/desktop_attach_errors_test.go`、`cmd/zwai/desktop_attach_identity_test.go`、`internal/server/presence_shutdown_test.go`、`internal/server/presence_shutdown_http_test.go`、`frontend/e2e/presence.spec.ts`；来源：Issue #77 正式 Mac 关闭后立即重开失败。
- 已确认的原生 presence 断流：native owner 仅在同一引擎 URL 重新 POST 并确认 SSE，连续最多三次恢复，单次完整握手和等待合计受 idle grace 内的上限约束。稳定持有至少一个 idle grace 才恢复预算，短流的确认不能无限重置预算；成功握手的超时不得延伸到健康 SSE 生命周期。关闭原生应用可中断请求和等待。Wails 启动后耗尽时，必须在请求原生 Quit 前通过配置的桌面 logger 同步记录一次可判断的连接原因；操作系统可能直接终止进程，不得依赖 Run 返回后才报告。若 Run 返回，必须保留原有错误类别；启动前失败直接返回且不得开窗口，正常释放或取消不得误报终止失败。不得后台 ensure 新引擎、换窗口 URL 或回加前端 hold。URL 中的 presence 是初次交接标识，恢复后的新 slot 仍登记同一 native PID。证据：`cmd/zwai/desktop_presence.go`、`cmd/zwai/desktop_attach_stream_recovery_test.go`、`cmd/zwai/desktop_presence_test.go`、`internal/desktop/lifecycle.go`、`internal/desktop/lifecycle_test.go`、`internal/desktop/lifecycle_terminal_test.go`。
- 证据：实现 `internal/lease`, `internal/app`, `cmd/zwai`；测试 `internal/lease`, `internal/app`, `cmd/zwai/engine_startup_test.go`；变更规则：同步架构、CLI 和恢复测试；来源：`ARCHITECTURE.md` 和 Git 历史。

## C-002 One turn per conversation

- 状态：active；类型：consistency；作用范围：engine、server、desktop、phone；关联功能：`F-130`, `F-150`, `F-220`, `F-222`, `F-230`, `F-310`。
- 契约内容：同一对话同时只有一个活动 turn；第二条普通消息进入 follow-up，活动中的 steer 指向该 turn；排队行在 turn 已经结束后点击注入，改为作为下一轮发送，Stop 仍不自动清空其余队列；排队消息和尚未被 manager 读取的 steer 可以拉进输入框后重新排队，已读 steer 不能改；手机在等待队列上执行“中断插入”时，先将队首消息转为本轮 steer，再请求中断当前 manager 步骤，其他消息继续排队；并发 API 冲突返回可识别的状态。
- 队列消费：只有实际删除成功的一方取得该排队记录的投递权，返回的内容必须属于这次删除的记录；自动出队丢失消费权或遇到编辑时重新选择队首，不投递旧内容。PC 对同一会话／排队 ID 的进行中注入请求去重；旧会话的迟到响应不得改动当前队列或错误。已离队记录返回 `404 followup_unavailable`，刷新后按真实刷新结果提示，不冒称注入成功，不按旧文本再发。证据：`internal/store/followups_claim_test.go`、`internal/server/missing_record_test.go`、`frontend/src/store/app-followup.ts`、`frontend/src/store/app-followup.test.ts`、`frontend/e2e/followup-error.spec.ts`。
- 提问选项：`ask_user` 的 PC 与手机表单必须遵循主机已接受的选项 ID 规则：先归一化显式 ID，结果为空则从 label 生成。不得因省略 ID 而丢弃合法选项或隐藏整张表单；实时、刷新及历史重放保持一致，提交保留原 label 与 call_id 并继续同一 turn。证据：`internal/engine/ask.go`、`frontend/src/lib/transcript-ask.test.ts`、`mobile/src/lib/ask.test.ts`、`frontend/e2e/plan.spec.ts`。
- 手机主按钮：真实运行且无可发送草稿（包括附件或引用）时显示可点击停止图标，连接既有停止请求；有内容仍可点击跟进／插入，提问时保持回答，定时等待不显示停止。直连流式回复期间停止按钮可中止请求并保留已收到的文本；停止不发送空消息。
- PC 停止交互：桌面和 Web 保留输入框的明确 Stop 按钮；Esc 不得调用停止请求，无弹层、输入框聚焦、关闭弹层后及连续按键均遵守该边界。Esc 继续关闭当前图片预览、Find 或其他局部弹层；图片预览优先于底层 Find。证据：实现 `frontend/src/App.tsx`；测试 `frontend/src/App.test.tsx`、`frontend/e2e/shell.spec.ts`。来源：用户 2026-10-21 避免 Esc 误停任务的要求。
- PC 与手机停止展示：`cleanup` 只证明尚在运行的子代理已停止，不证明其任务成功完成；其派生状态为 `cancelled`，与真实完成及失败分组区分。绑定当前 `agent_id` 且有非空原因的引擎取消 `finished` 回执，无论早于清理、晚于清理或晚于主管终态，实时与历史回放都显示已停止，Trace 保留原始原因；普通取消字样、其他代理 ID、ID 前缀碰撞、provider／watchdog 失败不能据此变成停止。PC 已确认为 `cancelled` 的代理，迟到的正文、流式文本、工具调用通知及后续取消或成功 `finished` 不得恢复运行或改变停止活动、停止时间；实时、完整历史及 roster 回放遵守相同规则。稳定的人工 `interrupted` 终态不显示模型失败／重试。真实成功及失败结果不得被清理回退，显式 `spawned` 续办同一 ID 后按新执行结果更新。该派生展示不增加 wire event，也不改变存储／广播顺序。证据：`frontend/src/lib/transcript.ts`、`frontend/src/lib/transcript-stop.test.ts`、`frontend/src/lib/transcript-cancelled-finish.test.ts`、`frontend/src/components/app/panel.tsx`、`frontend/e2e/restart.spec.ts`、`mobile/src/lib/transcript.ts`、`mobile/src/lib/transcript-stop.test.ts`、`mobile/src/components/thread-stop-roster.test.tsx`、`mobile/e2e/worker-stop-replay.spec.ts`。
- 允许行为：排队和插入；禁止行为：在同一对话并行启动两个 turn；失败语义：`ErrBusy`/`ErrIdle` 对应 409 与 code；不变量：turn 状态可恢复；边界条件：中断和等待。
- 证据：实现 `internal/engine`, `internal/server`, `mobile/src/app.tsx`, `mobile/src/lib/phone-turn.ts`, `mobile/src/components/composer.tsx`, `mobile/src/components/direct-chat-screen.tsx`；测试 `internal/engine`, `frontend/src/store/app-steer.test.ts`, `mobile/src/lib/phone-turn.test.ts`, `mobile/e2e/walkthrough.spec.ts`, `mobile/src/components/thread-stop.test.tsx`, `mobile/src/components/direct-chat-screen.test.tsx`, `mobile/e2e/composer-layout.spec.ts`；变更规则：同步 API、手机 RPC 和竞态测试；来源：`AGENTS.md`、GitHub Issues #31、#69。

## C-003 Stored events and streamed order

- 状态：active；类型：data / consistency；作用范围：engine、store、SSE、PC transcript、phone；关联功能：`F-120`, `F-130`, `F-150`, `F-220`, `F-310`, `F-330`, `F-410`。
- 契约内容：完成事件按同一锁分配 seq、保存、广播；流式 delta 包含截至当前的完整文本，不逐 token 持久化；慢订阅者通过数据库追赶，事件 kind 是回放格式的一部分。桌面运行态在首个进度脉冲前显示动态等待，不伪造服务端耗时；脉冲到达后显示服务端计时，轮次结束即移除。桌面与手机的轮次分界仅由真实 `done` / `error` 的 `created_at` 标记，不以最后一条回答或设备当前时间推断；隐藏定时输入也不能抹去已完成轮次的时间边界。
- PC 对话切片：渲染保留可见事件原顺序；同一 turn 可因穿插后续 turn 或跨越自身终态而形成多个连续片段，不把迟到事件搬回旧位置。每个已结束、非 quiet turn 只在最后一片终态前可见内容后显示一次结束栏，quiet turn 不显示；仅有隐藏 `title`／`session_memory` 的片段不生成空行或结束栏。迟到 `memory_review` 提示仍可见，且不被已结束 goal session 的工作折叠隐藏；实时活动仅归属最新 running turn 的最后一片，旧回合反馈不得抹去当前进度提示。回答与工具详情保留。该规则只改变前端投影，不删除或改写数据库事件、turn 终态和 Trace。证据：实现 `frontend/src/lib/turn-boundary.ts`；测试 `frontend/src/lib/turn-boundary.test.ts`, `frontend/src/components/app/transcript-boundary.test.tsx`, `frontend/e2e/late-review-footer.spec.ts`；来源：GitHub Issue #87。
- 桌面发送 ACK 仅确认服务器接受请求，不能把同一 turn 已收到的 `done` / `error` 回退为运行中；迟到 ACK 不得改变后来选择的其他对话。正常新 turn 和实时 steer 仍遵循真实事件及 ACK 身份。证据：`frontend/src/store/app.ts`, `frontend/src/store/app-send.test.ts`, `frontend/e2e/api-error.spec.ts`；来源：GitHub Issue #74。
- PC 打开时的异步归属：尾页日志与详情并行，尾页成功后即结束骨架，详情、轮次和跟进只补各自字段；切换、倒带或实时推进后的状态不得被较早响应覆盖，尤其不能将另一会话或已经终结的 turn 改为 running。工具事件尾页仍沿用可见历史补页；尾页分页、roster、SSE since 与事件 kind 不变。证据：`frontend/src/store/app-open.ts`、`frontend/src/store/app-open.test.ts`、`frontend/src/store/thread-history.ts`；变更规则：同步打开流程单元测试、UI E2E 和 API 说明；来源：长会话打开优化。
- 允许行为：delta 丢失后下一帧恢复；禁止行为：丢弃已存储事件或随意重命名 kind；失败语义：落后订阅者转追赶；不变量：存储顺序与流顺序一致；边界条件：重连和回放。
- 证据：实现 `internal/engine`, `internal/store`, `internal/remote`, `frontend/src/components/app/transcript.tsx`, `frontend/src/components/app/transcript-session.tsx`, `mobile/src/lib/transcript.ts`, `mobile/src/components/thread-blocks.tsx`；测试 `internal/engine`, `internal/remote`, `frontend/src/components/app/transcript.test.tsx`, `frontend/src/components/app/transcript-session.test.tsx`, `mobile/src/lib/transcript.test.ts`, `mobile/src/components/thread-blocks.test.tsx`, `mobile/e2e/walkthrough.spec.ts`；变更规则：同步 API、数据模型和 wire 测试；来源：`AGENTS.md`、GitHub Issues #59、#61。

- PC 输出与事件身份：同一流式模型输出的 `message_id` 在 worker 跨 manager 轮次继续运行时不变；同 agent、同 kind、同输出身份的累计快照更新首次出现的区块，不伪装成新思考。真正的新模型输出、恢复后的新流使用不同身份；旧事件缺少字段时不按文字推断身份。迟到的短快照不能回退正文或重开已结束输出。原始 checkpoint 事件仍各自按 seq 保存于 Trace，不改写历史；回退编辑时从保留的原始事件恢复撤回前的快照。
- PC 历史与实时重叠：以实际已加载的 `(thread_id, seq)` 去重，并在 reducer 与业务副作用前执行；不能用最高 seq 拒绝未加载的较旧记录。roster sidecar 进入完整历史页时仍需补齐其完整展示，重连、分页、按需 Agent 日志和切换对话遵守相同规则。证据：`frontend/src/store/thread-history.ts`、`frontend/src/store/app-stream.ts`、`frontend/src/store/app-stream-history.test.ts`、`frontend/src/lib/transcript-message.test.ts`、`frontend/e2e/agent-history-overlap.spec.ts`；来源：GitHub Issue #88。
- 后台清理顺序：停止 parked registry 时，在同一 runtime 锁内完成会话级 `cleanup` 的记录／发布尝试，再开放下一 registry 的 take／occupy；不能先释放旧归属，再让迟到的全局 cleanup 误停新轮次的 worker。正常存储路径先持久化再广播；沿用 `record` 的 best-effort 失败语义，写入失败报告 WARN，不能声称有 durable cleanup 回执，也不凭旧名册恢复当前已无有效归属的 worker。真正 Cancel／Close 在锁外执行。Stop、撤销最后 wake 和结束收尾均遵守此边界。证据：实现 `internal/engine/worker_ownership.go`、`internal/engine/goal_session.go`、`internal/engine/restart_stop.go`；测试 `internal/engine/worker_ownership_order_test.go`；来源：GitHub Issue #89。
- 巡检归属回放：live-edge roster sidecar 还须携带视口外的 `workers_retained` 和 `error`，按原 seq 重建 worker 是否仍在运行；仅生命周期投影，不把旧 error、内部归属 JSON 或轮次分界插入当前聊天。完整页面到达后仍按同一事件身份补齐正文，不能用 sidecar 已见为由丢失可见错误。证据：实现 `internal/store/store.go`、`internal/server/log.go`、`frontend/src/lib/transcript-worker-ownership.ts`；测试 `internal/store/store_test.go`、`frontend/src/lib/transcript-worker-ownership.test.ts`；来源：GitHub Issue #89。

## C-004 Workspace is an anchor; HTTP paths are untrusted

- 状态：active；类型：security；作用范围：tools、server、files；关联功能：`F-140`, `F-224`。
- 契约内容：agent 工作区是定位锚点，不限制 agent 文件权限；HTTP 和手机远程文件入口提供的路径不得穿越允许的工作区。
- 允许行为：agent 在其被授权环境中访问路径；禁止行为：把外部文件路径穿越交给文件工具；失败语义：拒绝不安全 HTTP／远程文件路径；不变量：两种信任边界不混同；边界条件：符号链接和相对路径。
- 证据：实现 `internal/tools`, `internal/server`, `internal/remote/files.go`；测试 `internal/server`, `internal/remote/files_test.go`；变更规则：同步 API 和安全测试；来源：`AGENTS.md`、GitHub Issue #48。

## C-005 Loopback and same-origin HTTP

- 状态：active；类型：security；作用范围：PC 服务；关联功能：`F-110`, `F-140`, `F-310`。
- 契约内容：默认只在 loopback 提供服务，Web 客户端同源访问；不为持有对话和命令权限的服务添加开放 CORS。
- 允许行为：同源 shell 连接；禁止行为：无认证的非 loopback 默认服务；失败语义：跨源请求被拒绝；不变量：默认网络暴露为本机；边界条件：用户显式配置监听地址。
- 证据：实现 `internal/app`, `internal/server`；测试 `internal/server`；变更规则：同步配置、API 和安全测试；来源：`AGENTS.md`。

## C-006 Phone remote protocol

- PC 超时配置：显式 `get_timeouts`／`set_timeouts` 只传窄 `timeout_settings` DTO，包含 worker 时长、提供商 ID／标签、独立的首字节／流空闲秒数及只读兼容字段 `effective_first_byte_timeout_seconds`；不传端点、密钥或完整配置。该兼容字段等于归一化首字节时长，不与 idle 取最小值。写请求只含提供商 ID 和编辑过的正整数时长，空修改、未知／重复 ID、溢出或只读／未知字段整体拒绝。帧预算检查与写回执绑定同一已保存候选，超限必须在持久化前拒绝。失败保留编辑与错误，不能用回填默认值冒充加载成功；旧主机 `unknown_op` 与请求等待超时只在面板提示，不解除连接。旧链接或旧读取／保存代次的回调不得覆盖当前页面；关闭不撤回已发送保存，重开必须重读。关联 `C-009`；证据：`internal/config/timeouts.go`、`internal/remote/timeouts.go`、`internal/remote/timeouts_test.go`、`mobile/src/lib/use-pc-timeouts.ts`、`mobile/src/lib/use-pc-timeouts.test.ts`、`mobile/e2e/pc-timeouts.spec.ts`；变更类型：Added；来源：Issue #82。
- 模型目录刷新是显式只读的 `refresh_catalog`：普通 `catalog` 仍读本地配置快照；刷新只返回启用且就绪提供商的实时发现结果，不补回已下线默认模型，不保存目录、默认项或凭据，也不改写显式选择。全部发现成功才替换手机目录，失败保留旧目录并在选择器内提供错误与重试；旧 PC 的 `unknown_op` 提示更新 PC，不能伪装为刷新成功或断开聊天。每条链接最多一个刷新，发现并发且受单次总时限约束，关闭链接取消并等待任务结束；响应继续遵守帧预算。手机只能应用当前链接、当前刷新代次的结果和清理回调。证据：`internal/remote/catalog_refresh.go`, `internal/remote/catalog_refresh_test.go`, `internal/remote/pump_catalog.go`, `internal/remote/pump_catalog_test.go`, `mobile/src/lib/use-model-catalog.ts`, `mobile/src/lib/use-model-catalog.test.ts`, `mobile/src/components/composer.test.tsx`, `mobile/e2e/model-refresh.spec.ts`；来源：Issue #81。
- 状态：active；类型：compatibility / security；作用范围：pairlink、phone、PC remote；关联功能：`F-125`, `F-170`, `F-210`, `F-220`, `F-222`, `F-224`, `F-230`。
- 本地生命周期：hub 注册在可取消后台任务中执行，不阻塞 PC 本地启动或状态查询；Stop／Reload 取消进行中的注册，并拒绝旧生命周期迟到的成功结果，不得复活已停用 host。注册错误仍通过远程状态／日志暴露；任务恢复与调度的顺序不因此改成裸异步。证据：`internal/remote/host.go`、`internal/remote/host_start.go`、`internal/remote/host_start_test.go`、`internal/app/app.go`、`internal/app/startup_remote_test.go`；变更规则：同步本地启动、取消／重载和无网络回归。
- 契约内容：手机通过 pairlink 加密 RPC 与 PC 通信，不直接调用 PC 的 `/api`；已持久化事件的 kind 和正 seq 可在手机回放；断线重连不解除绑定。回答 `ask_user` 时客户端必须使用 PC 规范化后的 question ID（如 `test-window` → `test_window`），RPC 拒绝必须显示原因并保留草稿，接受后不得重复提交。子 Agent 的事件按 `agent_id` 与主 Agent 分开回放，`spawned` 启动指令正文不离开 PC；手机只显示协议允许并按 `event_chars` 裁剪的活动。当前页只有手机隐藏的子任务调用及孤立工具回执时，`log` 必须继续向前查找可显示历史，不能让用户逐页点击隐藏记录；当前轮次没有可显示消息时必须说明原因并保留「更早」入口。
- 允许行为：relay/direct 路径切换及手机查看子 Agent 角色、状态和截断记录；手机页头计数仅包含运行中的子 Agent，列表按进行中／已结束分组，完成和失败记录仍可查看；禁止行为：把本地 PC API 或子 Agent 启动指令直接暴露给手机，或把子 Agent 回答混作主 Agent 回答；失败语义：连接错误与 host offline 可区分；不变量：一个 PC 对话跨端一致，同一 agent 的流式文本只更新自己的记录；边界条件：ticket 过期、掉线、历史翻页、子 Agent 续办和重连。
- 证据：实现 `internal/remote/clip.go`, `internal/remote/log.go`, `internal/engine/ask.go`, `frontend/src/lib/transcript-ask.ts`, `mobile/src/lib/link.ts`, `mobile/src/lib/ask.ts`, `mobile/src/lib/phone-turn.ts`, `mobile/src/lib/transcript.ts`, `mobile/src/lib/session.ts`, `mobile/src/components/ask-card.tsx`, `mobile/src/components/thread-screen.tsx`；测试 `internal/remote/watch_test.go` 的 `TestSpawnedInstructionNeverLeavesTheHost`, `internal/remote/log_test.go` 的 `TestLogFindsEarlierAnswerPastHiddenSubagentBookkeeping`, `frontend/src/lib/transcript-ask.test.ts`, `mobile/src/lib/ask.test.ts`, `mobile/src/lib/phone-turn.test.ts`, `mobile/src/components/ask-card.test.tsx`, `mobile/e2e/ask-answer.spec.ts`, `mobile/src/components/thread-screen.test.tsx`, `mobile/src/lib/transcript.test.ts`, `mobile/src/lib/session.test.ts`, `mobile/e2e/walkthrough.spec.ts`；变更规则：同步 API、手机测试和数据模型；来源：`ARCHITECTURE.md`、GitHub Issue #30 的 A 方案确认、GitHub Issue #47 的混合状态截图、GitHub Issue #51 的提问提交失败、GitHub Issue #58 的长会话空白。

- 不变量：序号为 0 的实时 delta 不可重放，也不能单独充当界面块身份。每个新实时块在当前对话内具有唯一身份，后续片段及完成消息保留它；历史分页或重连不会留下旧节点。子 Agent 列表仅显示分组和工作 Agent 卡片，不含主管正文或工作折叠。
- 身份与切页回归证据：`mobile/src/lib/transcript.test.ts`、`mobile/src/lib/session.test.ts`、`mobile/src/components/agent-page.test.tsx`、`mobile/e2e/agent-roster.spec.ts`。
- 完成未读列表按序列化后的实际字节预算装页，不得因累计结果超过 Pairlink 的 64 KiB 帧上限而无法加载。第一页返回 `attention_more` / `attention_cursor`，独立 More 使用 `group: "attention"`；边界行被读取或删除后游标仍须前进。单行或原有列表元数据本身超限时明确返回 `too_large`，不静默截断或反复发送同一页。证据：`internal/remote/attention.go`、`internal/remote/attention_test.go`；来源：Issue #76。

## C-007 Phone inbox pagination

- 状态：active；类型：consistency；作用范围：phone inbox；关联功能：`F-125`, `F-220`。
- 契约内容：定期 poll 是第一页增量更新，已用 More 加载的旧行保留；离开第一页的行不被重复塞回。
- 允许行为：第一页内容更新；禁止行为：整表替换抹掉已翻页数据；失败语义：失败保留已显示列表；不变量：翻页成果不因 poll 消失；边界条件：同一行跨分组或状态变化。
- 证据：实现 `mobile/src/lib/inbox-window.ts`, `mobile/src/lib/inbox-refresh.ts`, `mobile/src/app.tsx`；测试 `mobile/src/lib/inbox-window.test.ts`, `mobile/src/lib/inbox-refresh.test.tsx`, `mobile/e2e/walkthrough.spec.ts`；变更规则：同步手机走测；来源：`AGENTS.md`。
- 完成未读页独立于普通分组和正在运行集合；More 仅合并完成未读页，不覆盖普通页、分组或 live。首页轮询保留已展开的完成未读尾页，空首页仍有续页时必须保留 More；已读 ACK 仅移除对应完成，不能清空未读列表或自动恢复该任务。
- 已展开的完成集合刷新须读到原已加载窗口的最旧时间／ID 边界，再一次性对账；不能只重复首页而永久保留另一客户端已经读过的尾页。新增首页结果不缩短原窗口，刷新失败保留已有内容，刷新期间继续 More 的新增尾页及游标也保留。同一宿主不重叠刷新，旧宿主的迟到结果不能更新当前列表。排序须保留服务端时间的小数精度。
- 当前宿主已确认读取的完成编号抑制同编号或更旧的迟到列表提示，较新完成仍须显示。刷新须比较请求开始时与当前所有已加载集合的完成身份；刷新期间经服务端回执确认的较新完成，即使已移出旧完成集合，也不能被旧列表删除或覆盖；切换宿主清空该客户端确认记录。不能从 quiet／cancel 的观察编号伪造服务端完成水位。

## C-008 Durable local data

- 状态：active；类型：data；作用范围：store、search、engine 会话摘要与记忆复核；关联功能：`F-120`、`F-130`、`F-410`、`F-420`。
- 契约内容：对话、turn、完成事件和计划保存在本机数据目录；测试使用临时目录，不写用户数据目录。
- 上下文回放：打开会话的 `context_chars` 与下一回合模型历史共用 `replayHistory`，不另建计数公式。字符数为去首尾空白的目标、压缩摘要 rune 数，加上有效压缩水位之后、去除撤回 steer、补齐未落库 manager 回答后的 user／assistant 回放消息 `Content` rune 数；不等同 token 或 `usage.context_tokens`。撤回支持事件 seq 身份及旧记录 caption；压缩水位以下已保存的 assistant 回答不得重新补写越过水位。既往工具调用／结果不进入回放，工具正文和无关事件正文不进入其查询；缺回答补写保持幂等。事件补答的有效 turn 仍包括仅含工具或空正文的存量消息 turn。证据：`internal/engine/replay_history.go`、`internal/engine/replay_history_test.go`、`internal/store/replay.go`、`internal/store/replay_test.go`、`internal/engine/turn.go`、`internal/engine/compact.go`、`internal/engine/preempt.go`；变更规则：同步真实 Store 回放等价、字符计数、迁移与补答回归，以及 API 说明；来源：长会话打开优化。
- Worker 事实优先级：会话摘要、目标收尾直写和记忆复核从完整持久化历史确定对应 turn 的生命周期事实。`source: persisted_worker_lifecycle` 的 JSON 行保留 `turn_id`、`through_seq`、`new_workers`、`resumed_workers`、`finished_workers`、`rejected_spawn_calls`、`rejected_resume_calls`、`cleanup_events`；新建／续办／完成按各 turn 内不同 worker ID 计数，历史已出现或重复的 spawned ID 属续办／进程恢复，拒绝调用不算创建，cleanup 计事件数。拒绝数量须有 manager 调用与错误回执配对，合并拒绝使用 `rejected_calls`。
- 摘要预算与污染恢复：输入和落库摘要优先保留最多最近四个相关 turn 的事实，最新在前；目标收尾也走同一证据路径。旧摘要、回答、worker 正文与文件是待核对叙述，不能代替生命周期证据；不得通过匹配用户业务词语删除历史或改写记忆。读取完整历史失败时复核跳过并记录原因，不从缺失数据伪造零创建。该边界不承诺任意真实模型都遵从提示词，也不自动删除既有笔记或技能。
- 展示边界：手动和自动压缩的 `compacted` 事件 `summary` 仅包含摘要正文，剥离自己开头的完整内部事实块；`session_memory`、`compact_summary`、模型输入及 session-memory Trace 保留证据。展示裁剪不得回写删除内部事实，也不处理引用或损坏的标记。证据：`internal/engine/compact.go`、`internal/engine/autocompact.go`、`internal/engine/compact_evidence_test.go`、`internal/server/compact_evidence_test.go`。
- 允许行为：搜索索引后台更新；禁止行为：测试污染用户目录；失败语义：存储错误需显式显示；不变量：事件可按 turn 恢复；边界条件：进程重启和索引延后。
- 证据：实现 `internal/store`, `internal/search`；测试 `internal/store`, `internal/search`；Schema `docs/DATA_MODEL.md`；变更规则：同步 schema、迁移和恢复测试；来源：`AGENTS.md`。
- 摘要／复核证据：实现 `internal/engine/worker_lifecycle_facts.go`、`internal/engine/session_memory.go`、`internal/engine/review_source.go`、`internal/engine/review.go`；测试 `internal/engine/worker_lifecycle_facts_test.go`、`internal/engine/session_memory_bound_test.go`、`internal/engine/review_source_test.go`；变更规则：同步摘要、目标收尾、复核及事实裁剪回归；来源：GitHub Issue #78。

## C-009 Editable configuration and scheduled waits

- 超时保存边界：worker 未配置或非正时默认 `3600` 秒，已有显式正值保持；provider 首字节／idle 预算遵守下述独立等待边界。手机与桌面保存从锁内最新配置生成独立候选，原子持久化成功后才发布配置、更新 registry 与清理 provider cache，失败不得改变当前值或缓存；超时 patch 不覆盖其他字段。运行中的模型客户端与已创建 worker deadline 保持，后续创建客户端及 spawn／resume／restore 才使用新值。保存不重置 `C-019` 连续失败预算、不恢复暂停任务。快照与候选深拷贝既有 provider 的嵌套云工具参数，校验拒绝不得污染 live 配置，协议及云端执行来源仍遵守 `C-030`；worker 归属判断与 parked 恢复通过配置快照读取所需预算，不因超时保存产生竞态或改变原归属。证据：`internal/config/update.go`、`internal/config/timeouts.go`、`internal/config/timeouts_test.go`、`internal/config/timeout_contract_test.go`、`internal/remote/timeout_adapter_test.go`、`internal/engine/config_update.go`、`internal/engine/timeout_settings_test.go`、`internal/engine/worker_ownership.go`、`internal/engine/restart_parked.go`、`internal/engine/timeout_worker_ownership_test.go`；变更类型：Changed；来源：Issue #82。
- 桌面并发编辑：Settings 以首次读取／最后成功提交的本地草稿为比较基线，在串行写真正执行时计算变更；失败不推进基线，先改后改回也必须保存。自动保存与模型目录刷新不得重提未编辑的旧超时。API 省略 worker 时长或提供商 idle／first-byte 时长时，按锁内最新配置及 provider ID 保留值；显式值仍按原替换／规范化规则处理，不把全部 section 变成任意字段 patch。证据：`frontend/src/lib/settings-persist.ts`、`frontend/src/store/app.ts`、`internal/server/settings.go`、`frontend/src/lib/settings-persist-delta.test.ts`、`frontend/e2e/settings-timeout-concurrency.spec.ts`、`internal/server/timeout_settings_test.go`；变更类型：Fixed；来源：Issue #82。
- 状态：active；类型：compatibility / lifecycle；作用范围：config、provider、engine、store、PC transcript；关联功能：`F-130`, `F-150`, `F-170`, `F-220`, `F-230`, `F-330`, `F-420`。
- 展示边界：桌面/Web 会话右栏只列出指向本会话的 `active`/`paused` 唤醒，以及实际创建该会话的独立任务来源；完成/取消的普通唤醒移出右栏，历史仍在 Scheduled → Completed，空集合隐藏该区。来源任务即使完成/取消仍可打开。只有 `active` 行能把 `next_run_at` 展示为下一次运行，`paused`/`done`/`cancelled` 必须显示对应状态，不能把历史时间标成即将运行。关联 API 继续保留完成历史；实现：`frontend/src/lib/schedule-view.ts`、`frontend/src/components/app/schedule-inbox-row.tsx`；验证：`frontend/src/lib/schedule-view.test.ts`、`frontend/src/components/app/conversation-schedules.test.tsx`、`frontend/src/components/app/schedule-inbox-row.test.tsx`、`frontend/e2e/schedules.spec.ts`。
- 等待策略资源边界：普通工作只在尝试挂起时请求策略分类；单次验证上下文总截止时间为 30 秒，包含可取消的串行门禁等待。每 turn 最多分类 16 条未缓存 human 输入，成功结果跨 turn 持久缓存，后续 turn 可继续补齐；同 turn 同一有效输入提取失败后不再次请求，返回 pending 并继续当前工作。运行时 System 只注入来源事件 seq 与最多 256 rune 的引句，防止长原文在压缩后反复回填；工具拒绝 Trace 仍保留完整 source_text 与 source_quote。实现／验证：`internal/engine/thread_wake_policy.go`、`internal/engine/thread_wake_policy_test.go`；来源：当前会话等待禁止修复。
- 人类等待禁止：从原始有效 human `user_message`／`steer` 的语义提取 `deny`／`allow`／`unchanged`，以来源事件 seq、事件身份、文本指纹和精确引句持久化；不按业务词面匹配，也不接受 manager 参数声明人类已经许可。`deny` 盖过 Waiting 与定时检查开场白；只有后来明确 human `allow` 可解除，摘要、压缩、重启、技能、工具结果、模型写入的 schedule prompt 与 peer 数据均不能解除。`schedule_wake` 和 thread `report_schedule` 的 `next_in_s` 在真实工具边界及等待变更事务内检查；已禁止返回 `ok:false,code:"thread_wake_prohibited"`，来源未分类或策略提取失败返回 `ok:false,code:"thread_wake_policy_pending"`。拒绝保持原等待行完整不变、不发出 `schedule` armed 事件、不生成新的等待卡，Trace 的同 turn 工具结果对已分类禁令保留 source_seq、source_text、source_quote，待分类时保留已知 pending_seq。不带 `next_in_s` 的报告（包括 `keep:false` 取消）仍合法；带 `next_in_s` 的拒绝不得部分取消等待；`schedule_task` 的既有限制保持。证据：`internal/engine/thread_wake_policy.go`、`internal/engine/thread_wake_policy_test.go`、`internal/store/wake_policy.go`、`internal/store/wake_policy_guard_test.go`、`frontend/e2e/human-wake-policy.spec.ts`；变更规则：同步等待提示词、来源/策略存储、工具及事务回归、API、DATA_MODEL 与 TESTING。
- 契约内容：端点、模型等配置有默认值且可在 Settings 编辑；`OPENAI_*` 只用于首次空字段填充；已知模型窗口的自动压缩阈值必须同时预留固定输出空间和配置的最大输出 token，不得只按输入占比判断；缺失计费信息的重放消息和工具结果须保守估算非 ASCII 文本及每条消息的结构开销，不能按 ASCII 的每四字符一 token 低估；未知窗口仍用固定 token 阈值。无法压缩的上下文越界须给出降低输出上限或缩短请求的提示；周期检查因此终止时保留失败记录并暂停该计划，避免相同错误循环触发，调整后可手动恢复。计划触发或取消需保留状态。成功轮次若有下一次已安排的同对话唤醒，未完成的 sub-agent 保持原 ID 和运行状态供下次轮次续办；无后续唤醒且无有效持续目标时清理；取消或暂停最后一次唤醒不能留下无人承接的 worker，无下一轮的独立任务不保留 worker。巡检错误的隔离与停止边界遵守下述任务归属规则，不能仅凭父轮次 error 判定 worker 已结束。定时触发与立即执行没有任何工具结果或显式 `report_schedule` 时，只在原 turn 继续一次；仍无证据则 turn/run 为 error、unread=true，不把开场话当 findings 或空回答当 quiet。显式空 findings 保持 quiet；真实工具结果后的省略报告兜底保持兼容，模型错误清理生成的虚拟工具结果不算执行证据。判断不得依赖回答长度、标点、语言、任务样本或模型名。重试原因 `scheduled_no_activity` 进入同一 turn 的 Trace。
- 允许行为：用户更改设置；禁止行为：生产代码硬编码用户端点或模型；失败语义：配置或计划错误明确报告；不变量：用户设置优先于环境种子；边界条件：首次运行与重启。
- 模型等待边界：每个提供商的 `first_byte_timeout_seconds` 独立约束响应头等待，默认 30 秒，缺失、零和负值归默认；`timeout_seconds` 只约束流开始后的空闲，持续产生数据的流不因总时长被切断，首字节等待可以大于该空闲值。旧配置无需迁移；旧配置的空闲值小于 30 秒时，首字节不再由它缩短，需显式设置首字节值以保留较短等待。PUT 省略首字节字段保留同一提供商原值，显式非正值重置默认；保存后下一轮使用新值，无需重启，既有 turn 的模型客户端保持原值。响应头超时仍最多换新连接重试一次。证据：实现 `internal/config/provider.go`, `internal/provider/http.go`, `internal/server/settings.go`；测试 `internal/config/provider_timeout_test.go`, `internal/provider/http_test.go`, `internal/server/settings_timeout_test.go`。
- 目标编辑：legacy 空/NULL `schedule_runs.kind` 在目标编辑前按真实旧计划 kind 冻结；冻结、目标、可选 pause/resume 和活动数量上限检查在同一事务，失败无部分副作用。已领取 fire 按冻结 kind 与原 thread 保持 Waiting/worker 归属；成功移动最后一个 wake 后才清理原会话 parked workers 或续办 goal，其他 wake、已领取 fire 与 live turn 保持原有边界。
- 续办展示：`scheduled_no_activity` 的桌面提示是继续执行定时检查，不冒称发生了模型错误；旧模型错误提示保持兼容。
- 巡检失败与任务归属：定时触发和 Run now 的常规 turn error，包括无执行证据、模型 HTTP 400 和有界重试耗尽的 429，不能单独终止仍有有效归属的 worker。有效归属为当前可执行的同对话持续目标，或仍 active 的同对话 thread wake；目标须未完成、未 blocked／capped／idle，且自动续轮预算未耗尽，目标优先于 wake。会话处于 plan mode 时不能执行巡检，因此目标与活动 wake 均不授予后台 worker 归属。消费完的 one-shot、standalone 的来源会话关系、暂停或取消的 wake 都不能单独提供归属。存在归属时保持 registry、原 worker ID 及正在进行的模型／工具调用；失败 turn/run 仍为 error、run unread=true，不伪造报告或成功，不自动 block goal，也不立即启动新的目标轮次或重置既有重试预算。下一次计划触发或显式人类操作可接回同一 worker。
- 归属回执：停泊前必须在原 turn 持久化 manager 的 `workers_retained`，且其 seq 先于最终 error；`text` 是 JSON `{reason:"scheduled_check_failed",owner_kind:"goal"|"thread_wake",owner_id,worker_ids}`。goal 的 owner_id 是 thread ID，thread_wake 的 owner_id 是 schedule ID，worker_ids 只列本次仍运行的 worker。写入失败不授予保留执行；进入 idle 后重新检查最后归属的撤销及 Stop，避免取消等待与收尾交错留下无主 worker。人工 Stop／中断、无有效归属、上下文越界耗尽、worker watchdog 三次失败上限仍保留原有停止、清理及计划暂停语义；常规非 scheduled 错误不获得此例外。PC 实时／历史／roster 仅据有效回执保留列出的 worker，不能据错误文字猜测存活，也不能把普通 error 恢复成成功。内部 JSON 留在同 turn Trace。证据：实现 `internal/engine/worker_ownership.go`、`internal/engine/turn.go`、`frontend/src/lib/transcript-worker-ownership.ts`；测试 `internal/engine/schedule_worker_ownership_test.go`、`internal/engine/worker_ownership_test.go`、`frontend/src/lib/transcript-worker-ownership.test.ts`、`frontend/e2e/schedule-worker-ownership.spec.ts`；变更类型：Changed；来源：GitHub Issue #89。
- 完成范围：定时工具以精确 turn 与 claimed run 绑定。该 run 认领的是 manager 创建的 thread wake、当前仍指向同会话且 active，或本 turn 新排了仍 active 的 manager thread wake 时，不得把单次检查记为长期目标完成，返回 `ok:false,code:"goal_schedule_conflict"`；`report_schedule(keep:false)` 或取消当前等待后可正常完成，已消费且未重排的一次性等待不阻止完成。同轮成功完成后再用 `report_schedule` 或 `schedule_wake` 重排该续办也拒绝；调用顺序和并行调用不得绕过。人工、独立任务、已改投其他会话的等待不自动取消，不普遍禁止完成目标与独立等待共存。完成字段与 `goal_complete` 事件必须原子提交，存储失败不得回报成功或只落一半。历史完成记录不因存在等待而自动恢复；仍使用显式 Start／`reopen_goal`。证据：`internal/engine/goal_schedule_completion_test.go`, `frontend/e2e/goal-schedule.spec.ts`。`schedules.armed_turn_id` 在创建、替换、续期事务内记录精确工具轮次，不暴露为 JSON、不按时间或文本推测旧记录。即使旧一次性等待已消费、新等待换了 ID，也不得绕过。本规则是完成工具边界的 Changed，保留独立调度语义；来源：Issue #79。
- 证据：实现 `internal/config/compact_trigger.go`, `internal/engine/autocompact.go`, `internal/engine/session_memory.go`, `internal/engine/runerr.go`, `internal/engine/goal_session.go`, `internal/engine/schedule.go`, `internal/store`, `frontend/src/lib/transcript-notices.ts`；测试 `internal/config/compact_trigger_test.go`, `internal/engine/runerr_test.go`, `internal/engine/schedule_context_overflow_test.go`, `internal/engine/context_overflow_flow_test.go`, `internal/engine/schedule_retarget_test.go`, `internal/store/schedule_retarget_test.go`, `internal/engine/goal_session_test.go`, `internal/engine/schedule_completion_test.go`, `internal/engine/schedule_goal_test.go`, `frontend/src/lib/transcript-goal.test.ts`, `frontend/e2e/schedules.spec.ts`；变更规则：同步 CONFIG、API 和计划测试；来源：`AGENTS.md`、GitHub Issues #52、#55。

## C-010 Build artifacts and update channel

- TestFlight 组关联使用本应用的配置组白名单，完整分页核对 `GET betaGroups/{group}/builds` 与公开 `GET betaGroups?filter[builds]={build}` 的两个查询方向；不把重复同源列表伪造为反向响应，不调用未支持的 build→betaGroups GET，也不组合 app/build 两个关系过滤器。首次公开证明继续使用 schema 1，旧证明不迁移或重写；VALID／未过期、全部配置组、内外测、审核及源码/build/IPA 身份门禁不变。证据：`tools/release_ios.py`, `tools/test_release_ios_groups.py`, `tools/test_release_public_recovery.py`；变更类型：Fixed；来源：GitHub Issue #92。

- TestFlight 可以先于 GitHub tag 完成首次公开。无 tag 的冻结批次恢复必须匹配原版本、完整源码 SHA 和收口工作集，交叉核对成功 iOS 交付账本、官方状态中的精确 build、营销版本、构建 ID、IPA 哈希及首次严格公开证明；不能把已公开批次重新计入未分配集合。首次公开证明持久保存当时的时间、VALID／未过期、双向组关联和测试／审核状态，不能被后来的 iOS 过期、撤组或 Apple 不可达撤销。旧状态缺该证明时允许一次只读实时核验，预检不写，实际恢复在现有发布锁内落证；缺失／冲突证明拒绝，不能以布尔 published 代替。实际 iOS 交付仍执行当前门禁。旧冻结源码无需追随较新 main，其他端的原生验收独立生效。冲突 tag/SHA 必须拒绝；未公开批次继续执行实时门槛、最新完整 main 和 active_issue／finalized_workset 检查。证据：`tools/release.py`, `tools/release_ios.py`, `tools/test_release_public_recovery.py`；来源：GitHub Issue #90。

- 原生验收位置和清理遵循 `C-031`；实际 checkout 交付核验不授权在宿主机启动测试窗口。
- 状态：active；类型：compatibility / lifecycle；作用范围：checkout frontend build、mobile update、macOS desktop update；关联功能：`F-110`, `F-190`, `F-240`, `F-430`。
- checkout 构建依赖：需要重建 bundle 时，按 manifest 检查直接 `dependencies`／`devDependencies` 的已安装包 JSON 及所声明的本地 bin 入口，不能只凭 `node_modules` 目录存在或 mtime 判定可构建；不递归审计传递依赖或保证任意包内容完整。缺失时执行 `npm install --include=dev` 并在成功返回后复验，仍不满足则明确失败，不运行 build、不写 source stamp。安装、构建和 stamp 在 checkout 的忽略目录 `.build-lock` 下跨进程互斥，持锁后重查 staleness，等待可取消。新鲜 bundle 和无 checkout 的发布 embed 不新增 Node 要求。证据：`frontend/ensure.go`、`frontend/ensure_build_lock.go`、`frontend/ensure_dependencies_test.go`、`frontend/ensure_build_lock_test.go`。
- 构建进程所有权：脚本退出或取消后，先终止并等待本次构建的进程树，再允许后续 builder 复用 checkout 锁，不能只结束 npm 而留下 Node 写入 `dist`／`node_modules`。取消错误保留可判断的 context 类别，脚本退出状态和清理错误保留，不把清理失败报告为成功。Unix 用进程组，Windows 用 Job Object；平台行为验收分别记录，交叉编译只证明可构建。证据：`frontend/ensure_process_unix.go`、`frontend/ensure_process_windows.go`、`frontend/ensure_process_test.go`、`frontend/ensure_process_windows_test.go`。

- 实际目录交付：用户报告某个 checkout 启动行为时，修复提交必须进入那个目录的 HEAD，并保留无关未提交内容；该目录的源码和已运行服务资源只读核验，原生行为使用可核对 HEAD、脏文件哈希及构建身份的 `ci-macOS` 副本验收。不得在用户宿主机启动测试窗口；来宾验收不能证明宿主已运行实例更新。独立 worktree、远端主线和另一个安装包不能替代实际目录交付。只读核验器的 `source_and_assets_only` 范围明确不包含原生行为。证据：`tools/check_checkout_delivery.py`, `tools/test_check_checkout_delivery.py`；变更规则：同步开发规则及 TESTING；来源：用户 2026-10-21 对实际 desktop 启动仍缺修复及测试不得抢占桌面的纠正。
- 契约内容：`make build` 先更新嵌入的前端；Android sideload 使用 GitHub Release 中版本匹配的 `zwai-<version>-android.apk`；iOS 不显示 GitHub 版本检查菜单，测试版由 TestFlight 分发和更新。手机新版本从 package 版本派生 Android versionCode 和 iOS build number；开发用 Xcode 设置仍从 package 同步，TestFlight archive 默认沿用本应用私有台账中最高 build 的成功交付 marketing version，无成功历史时使用批次版本。私有 `marketing_version` 可显式开启新版本；已存在的候选状态、IPA 或源码回执冻结原 iOS 版本。批次 version/SHA 与 iOS marketing version/build number 分别保存，build number 必须高于远端全部 builds/uploads。macOS 桌面发布物是 `zwai-<version>-darwin-<arch>.zip`，根目录为 `zwai.app`。新版本仅在未分配待发布修复存在、主线自上一个公开源码 SHA 后提交超过三次，或最早待发布验收记录超过 24 小时时分配；跨日期、跨运行累计。`make release` 按台账只发布本批实际受影响且尚未交付的平台：desktop 为 macOS，mobile 为 Android 与 iOS；混合批次共享 tag 和源码 SHA，桌面单端发布无需增加手机 package/build number。各端门禁独立，最终缺任一必需端则非零。附件同名同 SHA-256 跳过，不允许覆盖；已发布 tag 必须对应冻结完整主线 SHA。TestFlight 只有 VALID、内测 READY_FOR_BETA_TESTING、外测 IN_BETA_TESTING 且全部既定组关联时才完成；存在审核记录时还必须 APPROVED，无审核记录不能代替真实外测状态。加组后重新观察，仅 READY_FOR_BETA_SUBMISSION 请求提交审核；同版本后续 build 仍可能被 Apple 要求审核。上传前查 builds/buildUploads、当前构建声明和北京日成功账本，每日最多一新构建成功；已接受上传、处理或审核等待均保存恢复信息，不重复上传。Mac 与 Android 更新器从本仓库最近的非 draft、非 prerelease 且包含本平台安全安装包的 Release 升级，不假定 `/releases/latest` 含每个平台；下载只跟随 github.com 与 GitHub 的 release 资源主机。
- 允许行为：不同渠道单独验收；禁止行为：把本地 APK/AAB 或未上传的 zip 称为远端发布，或从其他仓库安装；失败语义：构建、签名、下载错误可观察；不变量：用户可获取与显示版本一致的产物；边界条件：安装签名、新旧版本按数字比较、未打版本号的构建不提供升级。
- Mac 安装 ZIP 的根目录 `zwai.app` 必须在 `Info.plist` 声明 `CFBundleIconFile=AppIcon.icns`，并包含有效的 Contents/Resources/AppIcon.icns 图标资源；发布入口在首次上传前检查这两处。运行时 Wails/Dock 图标不能代替 Finder 安装包图标。
- 修复结案与发布分开：需求行为、必要测试、文档及 review 通过，完整修复集成并推送 main 后关闭 Issue；提交数与等待时长未触发新版本也不延迟结案。关闭评论分别记录已发布端和待发布端，交付台账保留未发布记录（包含 closed Issues）。审计合并全部已验收代码记录的实际平台缺口，包括没有 iOS 台账行的 Mac-only 修复，去重且排除重复报告别名。已分配版本的旧批次只计入恢复集合，不借给下一版的提交数与等待时长；已公开交付的批次锁定版本和源码 SHA，仅恢复其未交付端。Issue 关闭不代表全端发布，发布失败仍须告警。
- 混合平台批次的公共 Release 状态按平台记录；每个 Issue 的台账只把实际必需的平台标为已发布，无关平台保留 `not_required_by_behavior_change`，并按该 Issue 的全部必需端独立计算交付完成。首个经远端验证的平台公开后立即将统一版本和源码 SHA 锁定到全部批次行，部分交付不得再次取得新版本资格。
- 同一处理轮次先确定由本流程处理的 Issue/worktree 集合，串行完成可推进修复的验收、review、完整主线集成与推送；真实阻塞或需求歧义的项明确暂挂。待本轮工作集合收口后，对最终主线一次审计、分配单一版本；不得因不同 worktree 或端而在同一轮提前发布中间源码。新版本发布前，私有批次的 `finalized_workset` 必须与实时审计的源码 SHA、完整 Issue 集合及必需平台一致，且不得有 `active_issue`。无关、他人负责或尚未验收的 open Issue 不无限阻挡已达提交数／时长门槛的批次。已公开批次仍按冻结版本和 SHA 恢复，其后集成的修复进入下一个合格批次。
- 批次平台范围是各 Issue 必需端与完整主线中其他经验证的桌面／移动行为变化的并集。额外变化必须记录从上一公开 SHA 到当前完整主线之间的提交 SHA 与平台门禁，不得改写无关 Issue 的平台状态。新批次分配时，仍缺平台的旧公开批次保留在历史索引；冻结版本恢复完成须更新旧 Issue 和旧批次，不得回退更新版本的公开源码基线。
- iOS 应用经授权的持续出口合规声明保存在 `mobile/ios/App/App/Info.plist` 的 boolean `ITSAppUsesNonExemptEncryption`；用户于 2026-10-08 确认当前加密用途不变时沿用 false，常规版本/build 递增不得重复询问。预检和归档前读取源码字段，签名 IPA 必须保留同值；涉及加密用途、实现及相关依赖的变化时重新核定。无该字段的冻结旧源码仍要求按源码 SHA/build number 持久保存的私有精确确认；恢复读取该记录，活动签名配置切换不得导致重复询问。不得复制旧构建答复为新构建分类；源码、IPA 或精确确认有冲突时停止发布。证据：`tools/release_ios.py`, `tools/test_release_ios_compliance.py`, `mobile/ios/App/App/Info.plist`；来源：GitHub Issue #62。
- 已验收的 Mac 安装包若有匹配版本、源码 SHA、文件名及 SHA-256 的原生窗口回执，发布恢复必须直接验证并上传该 ZIP；文件缺失或哈希变化时停止该端，不得重新打包后声称沿用原验收。批次标记 `ready_for_publication` 时必须有该回执。
- Mac 原生验收恢复必须区分宿主与来宾会话：宿主锁屏不阻断就绪来宾；待验收工作在用户解锁、会话变化或 VM／控制通道变化后恢复时，调用方先将来宾状态置为 `unknown` 并重新观察。旧不可达／登录结果只作历史，不得跨恢复会话直接沿用；unknown 必须先返回 `observe_guest_session`，旧登录成功或失败均不能覆盖该状态。重新观察后只将本次恢复的登录尝试记录为当前结果；来宾锁屏且既有授权账号可用、尚未尝试登录时，下一步是实际登录，不得直接标阻塞。仅目标来宾本次真实解锁失败且有证据时可标该解锁阻塞；成功解锁后继续检查真实窗口。离线观测审计不执行登录或 SSH，不接受凭据字段；SSH/进程成功及退出码不代表验收。通过须有真实窗口与行为证据，并与候选版本、完整源码 SHA、ZIP 名及 SHA-256 和目标来宾完全一致；已完成的该精确候选验收不因之后的 unknown 或宿主锁屏失效。证据：`tools/audit_macos_acceptance.py`, `tools/test_audit_macos_acceptance.py`, `docs/RELEASE.md`；来源：GitHub Issue #68 及用户 2026-10-20 的恢复流程重复问题纠正。
- 证据：实现 `AGENTS.md`, `tools/audit_pending_batch.py`, `tools/release.py`, `tools/release_ios.py`, `Makefile`, `internal/release`, `internal/desktop/pack`, `internal/update`, `mobile/scripts/android-release.ts`, `mobile/scripts/sync-ios-version.ts`, `mobile/src/lib/app-update.ts`, `mobile/src/components/host-chrome.tsx`；测试 `tools/test_audit_pending_batch.py`, `tools/test_release.py`, `tools/test_release_ios_versions.py`, `internal/release`, `internal/update`, `internal/desktop/pack`, `mobile/scripts/android-release.test.ts`, `mobile/scripts/sync-ios-version.test.ts`, `mobile/src/lib/app-update.test.ts`, `mobile/src/components/host-chrome.test.tsx`, `mobile/e2e/version-check.spec.ts`, `mobile/ios/App/AppUITests/BindFlowTests.swift`；变更规则：同步发布规则、mobile README、CLI、发布门禁和版本测试；来源：用户 2026-09-26 修复结案与发布独立约定、GitHub Issue #53、`mobile/README.md`, `docs/CLI.md`。

## C-011 Quoted conversation payload

- 状态：active；类型：data / compatibility；作用范围：desktop、phone、message wire；关联功能：`F-180`, `F-221`。
- 契约内容：只选对话正文可加入草稿；手机上的加入操作跟随有效选区并留在会话视口内，不替换或遮挡系统复制／全选菜单；引用可查看、编辑和移除；每段原文单独以 `<selected_text>` 包裹，输入的请求以 `<user_request>` 包裹；引用可单独发送，发送失败恢复引用和正文；显示用户消息时不把标签当正文。
- 允许行为：多段引用、空正文发送；禁止行为：把输入框或 UI 文本当作引用，或让正文被误解为引用；失败语义：无效选区不提供操作；不变量：PC/手机同一 wire 格式；边界条件：空白选区、换行和文本中有关闭标签。
- 证据：实现 `frontend/src/lib/quote.ts`, `mobile/src/lib/quote.ts`, `mobile/src/components/thread-screen.tsx`, `mobile/src/components/composer.tsx`；测试 `frontend/src/lib/quote.test.ts`, `mobile/src/lib/quote.test.ts`, `mobile/src/components/thread-screen.test.tsx`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/composer-layout.spec.ts`, `mobile/e2e/ios-wait-layout.swift`；变更规则：同步双端解析、发送测试和用户说明；来源：`README.md`、GitHub Issues #29、#44。

## C-012 One-ID troubleshooting

- 状态：active；类型：compatibility；作用范围：engine、server、CLI、Trace、PC 子代理详情；关联功能：`F-130`, `F-150`, `F-160`, `F-170`, `F-310`, `F-320`, `F-420`。
- 契约内容：一个 turn id 可在 CLI、API 和 Trace 面板还原输入、事件、模型与工具调用、错误及结果；新增事件类型需进入这一路径。
- 模型流终态：错误、取消和正常 EOF 必须在本次调用记录回调结束后才暴露给消费端，避免轮次收尾或关闭存储先于调用记录；保留已接收 usage、调用身份及原始错误类别，消费端在回调期间关闭也不得重复计费或重复记录。证据：`internal/provider/provider.go`、`internal/provider/stream_terminal_order_test.go`。
- 人类等待禁止的拒绝在同 turn 的既有 `tool_result` 中保留稳定拒绝码，以及已分类禁令的 source_seq、source_text、source_quote；待分类拒绝保留已知 pending_seq。原句已从压缩重放中移除时仍可定位原始事件。拒绝不能生成表示已经成功挂起的 `schedule` 事件；模型口头声明不构成等待或 Trace 事实。等待策略与恢复边界由 `C-009` 定义。
- HTTP 缺失记录：服务端返回可判断的缺失类别、请求 ID、方法、路由模板和相关记录身份；同请求 ID 写入默认可见的 WARN 日志，响应头与 JSON 保持一致。不得记录请求正文、查询串或认证头；界面不能将此类操作失败冒充当前模型轮次失败。证据：`internal/server/missing_record.go`、`internal/server/missing_record_test.go`、`frontend/src/lib/api.test.ts`；关联功能：`F-310`。
- PC 子代理错误入口：failed 行须提供可见的查看原因入口；详情错误信息位于日志滚动区外，保留记录的错误并本地化已知错误，历史缺失原因时明确未记录，不伪造诊断。仅父 turn 的 error 到达时仍为 running、且没有本轮有效 `workers_retained` 回执覆盖的子代理派生为 failed 并保留父错误及所属轮次来源，不能冒称子代理独立失败；回执明确保留的 worker 继续 running，思考／回答流和待完成工具不关闭、不继承父巡检错误，原始父错误仍正常显示。该回执只在 Trace 提供归属信息，不将内部 JSON 作为聊天正文；已有子代理终态错误不被覆盖，续办清除上次原因与来源。先前 cleanup 已停止的代理不因后到父错误倒推为失败，人工停止及 cleanup 继续遵守 `C-002`。只有精确匹配本代理 `swarm: agent <id> exceeded timeout <duration>` 的 watchdog 错误可指向 `swarm.agent_timeout_seconds`，精确输出额度错误可指向 `swarm.max_completion_tokens`，已知 worker 迭代上限错误可指向 `swarm.max_turns`；这些入口打开 Settings → Swarm 并聚焦该字段。已识别的 provider 流空闲超时打开 Settings → Models，不猜测具体 provider 或聚焦字段。配置入口不修改设置或自动续办。普通 deadline、取消、其他代理的错误和未知错误不猜测超时配置。证据：`frontend/src/components/app/agent-failure.tsx`、`frontend/src/lib/agent-failure.ts`、`frontend/src/lib/transcript.ts`、`frontend/src/components/app/agent-failure.test.tsx`、`frontend/src/lib/agent-failure.test.ts`、`frontend/src/lib/transcript-failure.test.ts`、`frontend/e2e/agent-failure.spec.ts`、`frontend/e2e/worker-timeout.spec.ts`；来源：用户 2026-10-21 子代理失败原因及配置入口要求。
- Worker 计数证据：创建／续办以该 turn 的真实调用与持久化生命周期事件为准，历史名册和 assistant 自述不能当作本轮创建量。错误回执保留拒绝类别及聚合数量，已恢复 ID 不冒称新 ID；摘要事实按 `turn_id` 和 `through_seq` 归属。仅注入名册快照不得新增 `spawned` 或伪造 `spawn_agent` 工具记录；真实续办／进程恢复保留事件并按已有 ID 区分。证据：`internal/engine/worker_lifecycle_facts.go`、`internal/engine/worker_lifecycle_facts_test.go`、`frontend/e2e/worker-roster-history.spec.ts`；来源：GitHub Issue #78。
- 模型接口失败的公共 turn/error 展示保留可判断的 HTTP 状态码及接口拒绝/响应解码原因。JSON 解码错误本身不证明工具参数无效；SDK/Eino 类型错误经包装仍按接口错误处理。公共文本不包含原始响应正文、图执行路径或认证值，原因最多 512 字符；原始调用与重试仍在同一 turn 的 Trace 内可查，上下文溢出及超时提示保留现有恢复建议。
- 首字节超时保留实际等待时长；桌面/Web 引导用户到设置 → 模型调大该提供商的首字节等待时间。直达按钮使用失败 turn 的 `provider_id` 展开并聚焦字段，不能用当前切换后的提供商代替；提供商未知或已删除时只打开模型页。修改沿现有设置保存链路生效，不因打开或关闭设置自动重发请求。证据：实现 `internal/engine/runerr.go`, `frontend/src/components/app/transcript-error.tsx`, `frontend/src/components/app/model-settings.tsx`；测试 `internal/engine/runerr_timeout_test.go`, `frontend/src/components/app/transcript-error.test.tsx`, `frontend/e2e/api-error.spec.ts`。
- 允许行为：敏感值脱敏；禁止行为：丢掉诊断所需事件或输出令牌/完整 Cookie；失败语义：不存在的 id 报不存在；不变量：同 id 贯穿运行；边界条件：部分失败和重启恢复。
- 证据：实现 `internal/engine/runerr.go`, `internal/engine`, `internal/server`, `internal/store`；测试 `internal/engine/runerr_api_test.go`, `internal/engine/api_error_integration_test.go`, `internal/server`；变更规则：同步 CLI、API 和 Trace 测试；来源：`AGENTS.md`、GitHub Issue #71。

- 模型响应来源：provider model-call 边界保留 SDK 原因和错误类型；没有更具体 HTTP 状态或已解析 API 拒绝类别时的空/HTML 错误响应，以及 HTTP 200 流的 JSON 语法或字段类型解析失败使用 `model_response_invalid`，不冒称工具参数错误。真正工具参数错误保留 `invalid_tool_json`，已解析的 API 拒绝、HTTP 状态、鉴权不重试与有界重试语义不变；原始原因留在同 turn Trace。证据：`internal/provider/call_error.go`, `internal/engine/api_error_integration_test.go`, `internal/engine/model_response_error_test.go`, `frontend/e2e/api-error.spec.ts`, `mobile/e2e/api-error.spec.ts`。

- 请求前拒绝：manager middleware 检查前序状态重写后的当前尾部非空工具参数；无效 JSON 通过 `invalid_tool_json` 进入同 turn 有界重试，拒绝本身不发送 HTTP。普通新 user 之前的旧调用和空参数保持既有边界，不宣称消毒全部历史。实际 ADK/HTTP、压缩状态串联、steer 与 worker 恢复证据：`internal/engine/model_retry_http_test.go`。

## C-013 Phone model picker

- 状态：active；类型：compatibility / lifecycle；作用范围：phone composer；关联功能：`F-230`。
- 契约内容：手机输入框的模型按钮打开应用内分组列表，列表沿用应用字号并允许长模型名换行；选择后保留服务商和模型的配对关系，关闭后发送使用新选择。返回键先关闭列表，不退出对话或应用。
- 允许行为：点击遮罩、关闭按钮或 Escape 关闭列表；禁止行为：依赖不受应用样式控制的系统原生选项弹窗，或把不同服务商的同名模型混为一个选项；失败语义：列表关闭后保留原选择；不变量：选择只改变当前输入框的服务商和模型；边界条件：长模型名、多服务商和 Android 系统返回键。
- 证据：实现 `mobile/src/components/model-picker.tsx`, `mobile/src/components/composer.tsx`；测试 `mobile/src/components/composer.test.tsx`, `mobile/src/components/direct-chat-screen.test.tsx`, `mobile/e2e/walkthrough.spec.ts`；变更规则：同步手机模型选择测试和使用文档；来源：GitHub Issue #34。

## C-014 Phone client pagination feedback

- 状态：active；类型：lifecycle；作用范围：主机 Clients 快照和 phone Clients 分页；关联功能：`F-223`。
- 契约内容：点击 Clients 的 More 后立即显示加载动效和文字，等待 RPC 时禁用重复点击；成功时追加较旧任务，失败时保留现有列表、显示错误并恢复按钮。
- 快照边界：后台刷新按文件身份／大小／修改时间／模式判断是否重新解析；仅保留当前配置根中最新扫描的解析行，不缓存原始记录。发布前按更新时间稳定排序，分页不得修改快照或全量重排；返回行由调用方独立持有。新增、删除和变化的文件在下一次完成刷新反映，详情始终读取实时记录。
- 允许行为：不同工具分组独立显示任务；禁止行为：无反馈的重复分页、失败后永久禁用或静默丢弃异常；失败语义：已有任务继续可见，错误显示在手机页面；不变量：一个分页请求在途时不重复发起；边界条件：慢响应、RPC 拒绝、切换 PC。
- 证据：实现 `mobile/src/lib/client-poll.ts`, `mobile/src/components/client-groups.tsx`；测试 `mobile/src/app.test.tsx`, `mobile/src/components/client-groups.test.tsx`, `mobile/e2e/walkthrough.spec.ts`；补充证据：`internal/clients/catalog_cache.go`、`internal/clients/catalog_cache_test.go`、`internal/clients/catalog_performance_test.go`、`internal/clients/view_test.go`；变更规则：同步主机缓存／并发／性能、手机 UI 测试和使用文档；来源：GitHub Issues #35、#66。

## C-015 Phone Back follows visible layers

- 状态：active；类型：lifecycle；作用范围：Android／iOS 手机壳、手机页面及覆盖层；关联功能：`F-220`, `F-222`, `F-223`, `F-224`, `F-230`。
- 契约内容：Android 系统返回及 iOS 从左边缘向右完成的有效滑动，先关闭当前可见的最上层详情或弹层。子 Agent 详情→子 Agent 列表→对话→收件箱、Clients 任务详情、文件预览→文件列表→对话均遵守该层级。子 Agent 导航取决于已打开的页面，即使刷新历史时暂时找不到该 Agent，也不跳过列表。只有手机首页（收件箱、直接聊天列表或未绑定扫码页）返回时才允许 Android Activity 退出；iOS 首页滑动不退出应用。
- 允许行为：详情关闭后留在原列表，Android 再次从首页返回退出；禁止行为：详情可见时直接退出、迟到的任务读取结果在返回后重新打开详情、iOS 短滑／取消／垂直／非左边缘手势返回、把正文的横向滑动作为返回；失败语义：返回动作不依赖网络请求成功；不变量：一个有效手势只消费当前最上层，iOS 在完成手势时读取当前返回处理器，后来打开的模型选择弹层优先关闭；边界条件：异步任务读取中返回、多层覆盖、组件卸载、根页面。
- 证据：实现 `mobile/src/lib/android-back.ts`, `mobile/ios/App/App/BridgeViewController.swift`, `mobile/src/components/model-picker.tsx`, `mobile/src/components/thread-screen.tsx`, `mobile/src/components/client-groups.tsx`, `mobile/src/components/file-browser.tsx`, `mobile/src/app.tsx`；测试 `mobile/src/lib/android-back.test.ts`, `mobile/src/components/thread-screen.test.tsx`, `mobile/src/app.test.tsx`, `mobile/src/components/client-groups.test.tsx`, `mobile/src/components/file-browser.test.tsx`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/ios-wait-layout.swift`；变更规则：新增手机覆盖层须同步测试 Android 系统返回、iOS 原生边缘滑动及各自首页行为；来源：GitHub Issue #36、#48、#54、#75。

## C-016 Explicit PC selection opens its inbox

- 状态：active；类型：lifecycle；作用范围：手机已绑定 PC 的切换与重连；关联功能：`F-220`。
- 契约内容：用户明确选择另一个 PC 时，手机显示该 PC 的收件箱，不自动打开其运行中或上次浏览的对话；随后重连也保持这一选择。首次绑定或冷启动仍可自动恢复运行中或上次浏览的对话。
- 允许行为：用户点收件箱中的对话行后进入详情；禁止行为：把显式切换 PC 当成首次启动并代用户打开对话；失败语义：连接失败仍显示所选 PC 的连接或错误状态；不变量：显式切换后的首次列表只更新收件箱；边界条件：目标 PC 有运行中对话、上次对话或发生重连。
- 证据：实现 `mobile/src/app.tsx`；测试 `mobile/src/app.test.tsx`, `mobile/e2e/walkthrough.spec.ts`；变更规则：同步手机导航测试和使用文档；来源：GitHub Issue #37。

## C-017 Phone composer focus preserves the viewport

- 状态：active；类型：lifecycle / compatibility；作用范围：手机消息与引用编辑器；关联功能：`F-220`, `F-221`, `F-230`。
- 契约内容：消息和引用的可编辑文字至少为 16 CSS px，避免 iOS 聚焦小字输入框后留下自动放大；关闭键盘后，会话顶部仍在原有安全区域，等待操作及发送／跟进／插入按钮不得超出屏幕右边缘；长输入和长模型名不要求用户横向拖动才能提交。
- 允许行为：用户主动缩放；禁止行为：用禁止用户缩放代替修复，或在键盘关闭后留下自动缩放和顶部偏移；失败语义：布局恢复不依赖网络；不变量：聚焦输入框不改变关闭键盘后的导航、等待操作和提交可达性；边界条件：窄屏、长输入、长模型名、引用编辑、等待中的会话、PC／新建／直连对话。
- 证据：实现 `mobile/src/components/composer.tsx`；测试 `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/composer-layout.spec.ts`, `mobile/e2e/ios-wait-layout.swift`, `mobile/scripts/ios-layout-fixture.py`；变更规则：修改编辑器字号须重验 Web 布局和 iOS 键盘关闭后的真实页面；来源：GitHub Issues #40、#41（同一聚焦放大根因）。

## C-018 Phone client detail stays in the viewport

- 状态：active；类型：lifecycle / compatibility；作用范围：手机 Clients 任务详情；关联功能：`F-223`。
- 契约内容：任务详情覆盖手机视口；下拉收件箱、滚动任务记录或切换安全区域时，详情标题和返回按钮仍在可见视口内，返回按钮回到 Clients 列表。详情内的触摸不得触发底层收件箱下拉刷新。
- 允许行为：底层收件箱继续管理自身下拉刷新；禁止行为：让详情随收件箱的 transform 移动，或露出底层收件箱的导航控件；失败语义：详情读取失败时保留可用的返回入口和错误提示；不变量：详情层的位置不取决于收件箱滚动容器的位置；边界条件：iOS 安全区域、下拉刷新、长任务记录与 Android 系统返回。
- 证据：实现 `mobile/src/components/client-groups.tsx`, `mobile/src/components/pull-to-refresh.tsx`；测试 `mobile/src/components/client-groups.test.tsx`, `mobile/e2e/composer-layout.spec.ts`, `mobile/e2e/ios-wait-layout.swift`；变更规则：修改手机全屏详情或收件箱变换时同步验证 WebKit 和原生 iOS 的标题及返回；来源：GitHub Issue #45。

## C-019 Proactive delegation for time or quality

- Fork 角色边界：新 worker 继承 manager 对话时，保留非 System 的用户、assistant 和工具正文作为明确标注的历史上下文；不继承 manager 的 System 消息（含 personality、编排角色和运行时状态）。worker 使用自己的 system instruction、当前分配任务和工具集合，不把历史 manager 工作流当作自己的角色。不得改写 manager 原消息；同一 worker 的 resume 历史不应用跨角色过滤。证据：`resume.go` 的 `forkContext`、`swarm.go` 的两个 fork 入口、`fork_context_test.go` 的实际 Registry→ADK→模型输入回归。

- 状态：active；类型：compatibility；作用范围：engine 普通对话、持续目标、定时续办、上下文恢复和共享 manager 提示词的终端入口；关联功能：`F-130`、`F-150`、`F-170`、`F-330`。
- 契约内容：运行时提示词必须要求 manager 在能节省时间或提高质量时主动使用 subagents，并随任务进展重新评估，无需等待用户提出；时间与质量是独立理由。只有一个 worker、需要等待或任务较小不得成为拒绝有质量收益的独立审查／第二思路的绝对条件。
- 历史身份边界：完整历史 worker 名册以一个 System JSON 快照传入，`source` 为 `runtime_worker_roster`，`note` 标明是历史创建的当前状态且不授予人类权限，`workers` 保留 `agent_id`、`role` 和 `running`／`done`／`failed`。只有 running 表示仍在运行；已结束 worker 可查询或显式 resume，但恢复名册不得伪造 assistant `spawn_agent` 调用、成功回执或新的生命周期事件。重复恢复替换旧快照，保留真实工具尾及调用／回执配对；goal、schedule、compact、retry、restart 遵循相同语义。实现／测试：`internal/workerstate/snapshot.go`、`internal/workerstate/snapshot_test.go`、`internal/engine/autocompact_roster.go`、`internal/engine/resume_workers.go`、`internal/engine/worker_roster_test.go`、`frontend/e2e/worker-roster-history.spec.ts`；来源：GitHub Issue #78。
- 允许行为：无实质收益时自行完成；禁止行为：仅显式要求才委派、为满足调用比例强制创建 worker、持续目标提示词重新引入单 worker 等待禁令；失败语义：worker 失败或超时需如实说明，不能视为成功；不变量：遵守用户显式限制与并发上限，分离写入路径，manager 核验并整合结果；边界条件：并发上限为一、先自行执行后发现委派机会、用户限制委派。
- 无效调用边界：manager 的同一模型回复中，role/task 缺失、空白或参数不可读的 spawn 调用按同类错误合并，只执行保留代表的拒绝反馈，回执含 `rejected_calls`。保留代表原始 ID 和参数，保证模型历史、通知和 trace 的调用／回执配对；所有有效调用和其他工具均保留。流式参数拼完后判断，取消或流错误不执行未完成批次。拒绝调用未创建、续办、引导或取消任何 worker；不保证真实模型永不产生无效参数，正常修正不受固定调用配额限制。实现／测试：`spawn_batch.go`, `spawn_batch_test.go`, `swarm.go`, `tools.go`；来源：GitHub Issue #64。
- Worker watchdog 恢复边界：同一 worker 连续三次由 `AgentTimeout` 截止后，`resume_agent`、同角色 `spawn_agent` 和进程恢复不得再次启动它；可判断的 `WorkerTimeoutLimitError` 终止 manager，持续目标阻断，仅其失败周期计划暂停。provider idle/network timeout、取消和成功不累计；成功或明确的人类新消息／目标 Start 清除计数。模型恢复、`reopen_goal`、自动触发和注册表归档不得清除原 worker 计数。
- 默认 worker lifetime 为一小时，已有显式正值优先。保存新时长只影响后续生命周期，不延长现有 deadline、清除失败次数或恢复被阻断目标／暂停计划；关联 `C-009`。证据：`swarm.go`、`worker.go`、`timeout_settings_test.go`、`internal/config/timeouts_test.go`、`internal/engine/timeout_settings_test.go`；变更类型：Changed；来源：Issue #82。
- 已存 `finished.err` 的精确 `swarm: agent <id> exceeded timeout ` 前缀恢复旧版本记录，不把普通 deadline 当 watchdog。Trace 保留每次失败和终止原因；暂停写入失败不得宣称计划已暂停。该门禁限制无效恢复，不保证模型完成原任务。实现／测试：`worker_timeout.go`、`worker_timeout_test.go`、`internal/engine/worker_timeout.go`、`internal/engine/worker_timeout_test.go`；来源：GitHub Issue #65。
- 验证边界：该契约约束实际生成的模型指令，不保证任一真实模型的调用次数；离线 provider 的固定调用脚本只能验证委派链路，不能证明真实模型的调用比例提升。
- 证据：实现 `internal/engine/prompt.go`, `internal/engine/iterations.go`, `internal/tui/plan.go`；测试 `internal/engine/prompt_test.go`, `cmd/zwai/tui_prompt_test.go`, `internal/engine/engine_test.go`, `frontend/e2e/conversation.spec.ts`；变更规则：同步共享提示词、持续目标提示词、入口测试与使用说明；来源：用户 2026-09-28 对运行时主动使用 subagents 的要求。

## C-020 Phone workspace file reads

- 状态：active；类型：security / compatibility；作用范围：PC remote 文件 RPC、手机预览；关联功能：`F-224`。
- 契约内容：`files` 只列出指定对话与 PC 文件面板相同的工作区树，分帧分页；`file_chunk` 只读取该对话工作区内的普通文件，按字节偏移返回不超过 36 KiB 的 base64 分块及文件大小、MIME 和下一偏移。PC 必须拒绝路径穿越和通过符号链接逃出工作区。手机组装文件不超过 32 MiB，文本／图片／PDF 预览不超过 8 MiB；HTML 和 SVG 只作为源码文本显示。
- 手机文件树初次加载与刷新时默认折叠所有目录，只显示第一层；展开一层不自动展开子目录，搜索可越过折叠状态匹配文件。此规则只影响展示，不改变 PC 文件 RPC 返回的完整树。证据：`mobile/src/components/file-browser.tsx`, `mobile/src/components/file-browser.test.tsx`, `mobile/e2e/walkthrough.spec.ts`；来源：GitHub Issue #56。
- 后缀（大小写不敏感）决定文本呈现：`.md`、`.markdown`、`.mdx` 使用安全 Markdown 渲染（MDX 不求值），已知源码后缀复用桌面词法高亮，其余文本保持原样；超过 256 Ki 字符回退完整纯文本，单行超过 4096 字符跳过源码高亮，不截断内容。下载仍为原文件字节，HTML/SVG 始终是转义源码。证据：`mobile/src/components/text-file-preview.tsx`, `mobile/src/components/text-file-preview.test.tsx`, `mobile/e2e/walkthrough.spec.ts`；来源：GitHub Issue #63。
- 允许行为：浏览目录、读取已上传或 agent 生成的文件、下载其他可读取文件；禁止行为：从手机直接调用 PC loopback API、在应用同源执行 agent 产出的 HTML/SVG、把超限文件截断后冒充完整内容；失败语义：缺失或不安全路径返回 RPC 错误，手机显示读取错误或过大提示；旧 PC 返回 `unknown_op` 时提示更新 PC；不变量：列表和内容均绑定同一对话 ID，单帧不超过 Pairlink 密文上限；边界条件：目录、空文件、分页、大文件、并发文件变化和符号链接。
- 证据：实现 `internal/remote/files.go`, `mobile/src/lib/remote-files.ts`, `mobile/src/components/file-browser.tsx`；测试 `internal/remote/files_test.go`, `mobile/src/lib/remote-files.test.ts`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/ios-wait-layout.swift`；变更规则：同步 RPC 文档、手机原生／浏览器走测和安全边界测试；来源：GitHub Issue #48。

## C-021 Independent task messages retain provenance

- 状态：active；类型：lifecycle / consistency / security；作用范围：host manager、事件数据库、桌面和配对手机；关联功能：`F-135`、`F-160`。
- 契约内容：`list_tasks` 返回同 host 其他未归档的运行中、留有 worker 或有计划等待的对话，以及实际 workspace、当前请求和状态；结果只是快照。发送者 conversation/turn 身份由运行时绑定，模型参数不能冒充来源。仅未归档的不同对话可互发消息。
- 发送必须先持久化 `task_message`，再广播和排入 manager 的下次模型边界通知；失败不返回成功回执。`stored`/`notice_queued` 不表示已阅读、已确认或已达成协议。接收方结束、取消、暂停或重启后仍可读取事件收件箱；空闲对话不自动开始新轮次，暂停不被解除。
- `read_task_messages` 默认使用数据库中已保存的读取游标；显式 `after_seq: 0` 可重读。分页 `limit` 为 1–100，默认 20；返回 `messages`、`next_seq`、`more`。成功读页只向前推进默认游标，不改变会话最近活动时间；读取不是协作确认，明确回复才是协议证据。用户重写历史时，同一事务将落入已删除历史的游标收回到剩余 timeline 尾部，重启后新消息仍可读。
- 允许行为：按资源分工、协商等待和以现有 `schedule_wake` 有界等候；禁止行为：把发现当作强制锁，把 peer 消息当作用户权限，自动打断/唤醒对方，或忙循环轮询。
- 失败语义：非法参数、未知或归档目标、数据库失败返回工具错误；不变量：一条消息保留 `source_thread_id`、`source_turn_id` 和收件 timeline `seq`，不会成为 `user_message`/`steer` 或一个假 worker；边界条件：未读通知晚于结束时保留持久化消息，但不发起人类跟进。用户显式重写/删除对话历史仍遵循 `C-008` 的数据生命周期。
- 证据：实现 `internal/engine/task_coordination.go`、`internal/engine/goal_session.go`、`internal/store/event_kind.go`、`internal/remote/clip.go`；测试 `internal/engine/task_coordination_test.go`、`internal/engine/task_coordination_flow_test.go`、`internal/store/event_kind_test.go`、`internal/remote/task_message_test.go`、`frontend/src/lib/task-message.test.ts`、`mobile/src/lib/task-message.test.ts`。
- 变更规则：同步 API、DATA_MODEL、TESTING、桌面/手机事件回放与 CHANGELOG；来源：GitHub Issue #57 的通信协商选择，评论 6071858524。

## C-023 Completion attention is durable until the result is viewed

- 状态：active；类型：lifecycle / consistency / data；作用范围：store、HTTP、配对 RPC、桌面与手机；关联功能：`F-125`、`F-120`、`F-220`、`F-410`。
- 手机兼容性（`C-006`）：只有当前主机 detail 包含数值 `completed_seq` 时才发送可选 `read`；旧主机缺失或 `undefined` 字段时，实时／定时终态不触发不支持的读操作。数值 `0` 表示支持但尚无完成水位，不能禁用首次可见结果回执。切换 PC 后按当前 detail 判断，不继承其他主机能力。已声明支持的主机明确拒绝读或发生网络错误时仍显示错误、保留未读，不把 `unknown_op` 当成功。证据：`mobile/src/lib/completion-read.ts`、`mobile/src/lib/completion-read-compatibility.test.tsx`、`mobile/e2e/unread-completion.spec.ts`；来源：Issue #80。
- 契约内容：主任务成功或错误的终态事件与完成水位在广播前原子保存；子 Agent 终态、用户取消和安静计划结果不新增提示。旧数据不回填为未读。桌面收起项目在标题右侧汇总，手机完成未读置顶集合独立于 running 与普通分页额度，独立按实际字节预算分页并提供 More，已展开页不被首页轮询删除，直到明确打开并渲染结果；自动恢复、列表轮询、后台页面和失败导航不算查看。
- 允许行为：正在查看的主结果实际渲染后确认其编号；跨客户端读取后在后续刷新同步；禁止行为：用当前最新编号代替使用者实际看到的编号，或把未读完成当正在执行以触发自动恢复。
- 失败语义：无效或未来编号返回 `bad_request`，未知对话返回 `not_found`；已存储的取消／安静主终态允许无副作用读取；回执失败保留未读并显示错误；不变量：延迟或重复 ACK 不消费较新完成，同一完成的旧回执不能重新点亮已读提示，回执不改运行状态或标题。手机须区分实际渲染的终态编号与服务端完成水位，不能用 quiet／cancel 事件自行改写完成水位；回退低于请求开始时的水位后，旧回执不得恢复已删除完成。
- 边界条件：quiet 仅撤销对应 turn 的提示；回退重算可见完成与已读水位，但保留持久终态编号上限，重启后不能复用旧终态身份。归档不在提示列表，恢复仍按保存的水位判断；完成后再进入新轮次时运行／问答／等待状态优先，旧未读结果仍可查看。
- 手机展示互斥：按对话 ID 从未搜索过滤的 live roster、普通行或完成行自身的 running／waiting 状态排除顶部完成未读行；roster 的待答也属于 live。完成行单独进入或退出 running／waiting 时，列表轮询指纹必须变化并刷新展示；相同状态的重复轮询保持指纹稳定。只隐藏当前展示，不清除 `unread`／`completed_seq`、不发 `read`，空闲后仍可呈现旧未读。同名不同 ID 不能互相排除；非搜索视图中，More 的完成页游标与入口不因当前页全被隐藏而丢失。证据：`mobile/src/components/home-screen.tsx`、`mobile/src/components/home-screen-unread.test.tsx`、`mobile/src/lib/resume.ts`、`mobile/src/lib/resume.test.ts`、`mobile/src/active-unread-refresh.test.tsx`、`mobile/e2e/active-unread.spec.ts`；来源：Issue #83。
- 证据：实现 `internal/store/completion.go`, `internal/server/completion.go`, `internal/remote/protocol.go`, `frontend/src/components/app/completion-reader.tsx`, `mobile/src/lib/completion-read.ts`；测试 `internal/store/completion_test.go`, `frontend/src/components/app/completion-reader.test.tsx`, `frontend/e2e/unread-completion.spec.ts`, `mobile/e2e/unread-completion.spec.ts`。
- 变更规则：同步 HTTP/RPC、存储模型、客户端读取与回退／重启／竞态测试、功能及 CHANGELOG；来源：Issue #76。

## C-024 Desktop update lifecycle

- 状态：active；类型：lifecycle / consistency / compatibility；作用范围：桌面更新提示、presence、更新服务和原生启动；关联功能：`F-190`。
- 契约内容：桌面启动和持续运行每六小时自动检查；恢复可见、焦点或联网时仅在检查到期或上次检查失败后检查。自动失败保持安静，不清除已显示的可升级版本；已显示的手动检查错误在后续自动检查得到不支持结果时收起。菜单检查绕过缓存，明确显示当前、可用、不支持或具体错误。安装必须经用户确认；安装和重启期间不得自动或手动检查覆盖安装状态，较旧响应不得覆盖较新操作。
- 安装期间可通过只读 `GET /api/update/progress` 查看目标版本、阶段和实际下载字节；总量未知时不得显示虚构百分比。下载完成后区分安装与重启阶段。单次进度观测有界，失败或超时后继续查询，不将待决安装改成失败，安装终态以 `POST /api/update` 结果为准。检查和安装错误使用红色可访问警示；安装失败保留已确认目标版本，显式重试直接重发同版本安装，检查失败重试检查。
- 原生窗口将真实 PID 经启动 URL 和 presence 登记；HTTP 拒绝负数或超过 2147483647 的 PID，信号发送端再次过滤越界值，避免整数截断成为进程组信号。确认安装后，以实际应用目标为范围跨引擎和进程只允许一个安装；不同数据目录、父目录别名和 `.app` 符号链接不能绕过同一包的安装锁，不同应用包互不阻塞。锁不依赖替换中的包 inode，成功后保持锁直到旧引擎退出，重复请求返回 `update.ErrBusy`，HTTP `409`、`code: update_busy`。失败释放锁，允许用户重试。
- 安装保留原应用的绝对数据目录和离线模式；以新实例启动替换后的 `.app`，启动命令成功后才通知旧窗口退出。启动命令失败恢复旧应用文件，不通知旧窗口退出；恢复也失败时保留两类错误和上下文。成功安装的旧备份在锁内转移至该次安装独有的路径；旧引擎退出后的清理不得删除后来安装的备份，路径中的引号、命令替换或反引号只能作为路径字符，不能执行。`restarting` 只表示原生启动请求已接受，不能作为新窗口或新引擎已就绪的证明。
- 允许行为：暂不显示某个版本、手动强制检查、失败后显式重试；禁止行为：未经确认安装、两个窗口并行替换同一应用、旧检查覆盖安装状态、更新后改用另一数据目录；失败语义：自动检查静默，手动检查和安装错误可观察并可重试；不变量：同一实例数据目录不变，原生版本发现与下载仍遵守 `C-010`；边界条件：应用长开、离线、恢复前台、多窗口、启动拒绝、不支持的平台和未打数字版本的构建。
- 证据：实现 `frontend/src/lib/desktop-update.ts`, `frontend/src/components/app/desktop-update.tsx`, `frontend/src/lib/shell.ts`, `frontend/src/lib/api.ts`, `cmd/zwai/serve.go`, `internal/app/app.go`, `internal/update/apply.go`, `internal/update/progress.go`, `internal/server/update.go`, `internal/server/presence.go`；测试 `frontend/src/lib/desktop-update.test.ts`, `frontend/src/lib/desktop-update-progress.test.ts`, `frontend/src/components/app/desktop-update.test.tsx`, `frontend/src/lib/desktop-presence.test.ts`, `frontend/e2e/desktop-update.spec.ts`, `cmd/zwai/engine_cli_test.go`, `internal/app/update_test.go`, `internal/update/apply_recovery_test.go`, `internal/update/progress_test.go`, `internal/update/bundle_lock_test.go`, `internal/server/update_test.go`。
- 变更规则：同步 README、API、FEATURES、TESTING 和 CHANGELOG，并回归安装锁、启动失败恢复和更新提示竞态；来源：用户 2026-10-12 桌面自动更新需求与当前可达更新链路、Issue #77 的同包跨引擎旧备份丢失反例。

## C-025 Memory review evidence and commit

- 状态：active；类型：lifecycle / consistency / data / compatibility；作用范围：自动复盘、notes 存储、skill 写入、复盘 Trace；关联功能：`F-420`、`F-160`；关联契约：`C-012`。
- 契约内容：memory 与 auto-review 开启时，成功回合完整复盘，不因 manager 曾写入任何一项记忆而跳过。失败项目回合只允许有效真实人类要求形成 notes，不读任务工具/助手/运行时续办/派生摘要作为新证据，不写技能或执行技能 fold；取消不自动复盘。手动 Review now 仍选择最近成功回合。
- 输入边界：有效 steer 先应用修订与撤回；源事件优先人类、再近期按 agent/call ID 配对的工具交互、最后助手及派生文本，渲染保持时间顺序。单个事件源 2000、源总量 24000 rune 含完整生命周期前置块、framing、分隔与截断标记；成功回合先预留完整、有界的生命周期块，再从剩余预算选择人类/工具/claims，生成事实不伪造或截短真实身份；身份 framing 本身超出单源上限时省略整个来源组，工具配对不拆分，不截断身份字段或赋予被省略正文来源权限。当前 notes、技能目录和系统指令不在此源预算内，不承诺完整模型输入或费用的同一上限。取得项目复盘串行锁后、模型调用前读取当前 notes。
- Worker 事实边界（`C-008`）：成功回合复盘先从完整持久化历史重建对应 turn 的生命周期事实，独立前置于选取的源转录与有界 briefing；历史恢复 ID、真实创建和拒绝调用不得混同。完整历史读取失败时跳过该成功回合的模型复盘并记录原因，仍保留成功回合确定性 skill fold，不从缺失数据宣称零创建。该事实前置块计入 24000 rune 源总预算，但不进入可引用源事件集合，不可据其内部文本伪造 seq 或写入依据；2000 rune 单项上限约束事件来源。失败回合 human-only 复盘不接收该前置块、briefing 或运行时/任务工具证据。
- 写入边界：自动 reviewer 提交完整 apply_memory_review 批次，公开 manager 的 memory/skill 工具契约不变。notes 在内存草稿复用单条/总量上限、唯一匹配与技能重叠校验，基线与首次模型请求中的 notes 使用同一快照，在取得项目复盘锁后捕获；最终在同 Store 共享锁内比较该 Rev 并一次提交，零净变化不写盘。普通拒绝后的修复仍绑定同一 Rev，不自动刷新或静默重基；批次开始前检测到模型读取后的人类/manager 修改会终止冲突，不追加修复请求；最终 commit 的极短竞态可能追加一次修复请求，随后固定 Rev 仍禁止覆盖或静默重基。输入 notes 不可读时在模型调用前报错。失败不发布半套 notes，不自动重基覆盖并发人类/manager 修改；不承诺与外部直接文件写入之间的跨进程 CAS。
- 推理配置：`memory.review_reasoning_effort` 默认空，自动及手动回合复盘继承原 turn；显式 `low` / `medium` / `high` 仅覆盖该复盘。无效值归空；不改变 manager、catalog tidy、模型、输出 cap 或超时。模型不支持显式档位时保留原 API 错误，不静默冒充降档成功。
- 技能边界：skills 保持即时真实文件写入，不属于 notes 的跨文件事务。后续操作或 notes commit 失败，之前真实技能变化仍必须进入 changes。成功回合的既有 family fold 和独立 catalog tidy 保留，失败回合不 fold。
- 提炼指令：技能须区分常规路径与故障已经发生时可独立执行的恢复分支，写清有证据的进入条件、恢复动作及回归正常或结束条件；失败探测是诊断上下文，不自动成为必须重做的步骤。该指令不构成程序对流程语义正确性的证明，效果仍须独立评测。
- 来源约束：每个操作须引用可用源 seq 和有效原文；工具来源必须有配对调用且非显式拒绝，memory 写入回执及派生总结不能自证事实。notes 新增阿拉伯数字细节须保持引文或被替换旧条目中的拼写；引用存在和数字出现不证明蕴含、因果、长期价值、语义去重或授权。旧 notes 不因缺少引用而被批量删除或冒称正确。
- 容量修复：首次输入包含当前 notes 用量、总容量与单条上限；容量拒绝向模型完整保留结构化 usage/over_by/current_entries/matched，notes 仍按操作顺序逐步校验。只能有依据地先缩短／替换相关条目，再新增；不得清理无关 notes，不放宽容量、来源、数字、唯一匹配或固定 Rev。原迭代预算内无法安全提交时明确失败；空批次仍只表示没有应保留的新变化。
- 完成与失败语义：完整成功批次（可为空）结束 reviewer，由程序生成结果行，不为结束语追加模型调用；重复完成不重复写入。拒绝为可修复工具反馈，在原迭代上限内修正；没有成功提交完整批次而结束（含耗尽上限）明确记录 err，不伪造已完成。stop_reason 区分 completed/iteration_limit/proposal_missing/error，attempt_count 为批次提交次数，不是模型调用次数。冲突/存储失败不改变主回合终态。批次 accepted 后的 skill fold 失败仍记录 stop_reason=error，保留此前已提交 notes/skills，不冒称整轮成功或整体回滚。transcript 和 Memory 面板显示具体停止／拒绝原因、提案次数和可用容量数字，区分未提交 notes 与已保存技能。
- 诊断边界：同 turn 的 memory_review 可附 evidence/refused，以及最多 32 项 attempts，每项最多 32 操作、每操作最多 16 源 seq；attempt 只记录 number、target/action/source seq 和 accepted/refused/error，refused 保存受控 reason/code、操作 index、attempt、源 seq、usage/over_by。总 attempt_count 可超过保留明细。拒绝提案正文、引句、旧 note/name、current_entries/matched 不进入新增诊断字段；既有已提交 changes 与 accepted evidence 语义不变。旧事件缺新增字段仍兼容；失败 Trace 必须打开并定位原回合，历史聚焦不得显示最新回合的用量。手动复盘结果恢复绑定点击代次、线程、回合及起始 seq；初始 tail 不含结果时最多补读一次已知 turn 的 Trace，无结果或读取失败继续等待 SSE，不自动轮询，不接受旧结果终止后续点击。公开诊断继续遵守 `C-012` 的脱敏边界。
- 允许行为：保留当前有用记忆、用 replace 更正失效条目、对不确定可选知识不写入、在拒绝后有界修复；禁止行为：把先增后删的中间 notes 暴露为最终记忆，把 source quote 或模型置信度当真值，或把技能落盘误称整批回滚。
- 不变量：正式 notes 的 changes 仅描述已提交净变化；原 Markdown 和手工编辑兼容；技能已落盘事实不因 notes 失败而消失；边界条件：长 CJK/工具输出、末尾纠正、同回合多项要求、工具拒绝、失败/取消、并发编辑、超限、跨技能写入与重复完成。
- 证据：实现 `internal/engine/review.go`、`internal/engine/review_diagnostics.go`、`frontend/src/lib/review-diagnostics.ts`、`internal/engine/review_batch.go`、`internal/engine/review_evidence.go`、`internal/engine/review_human.go`、`internal/engine/review_source_select.go`、`internal/engine/review_source.go`、`internal/engine/worker_lifecycle_facts.go`、`internal/memory/notes_draft.go`；测试 `internal/engine/review_quality_test.go`、`internal/engine/review_diagnostics_test.go`、`internal/engine/review_finalize_test.go`、`internal/server/review_diagnostics_test.go`、`frontend/src/lib/review-diagnostics.test.ts`、`frontend/e2e/memory-review.spec.ts`、`internal/engine/review_batch_test.go`、`internal/engine/review_cas_test.go`、`internal/engine/review_evidence_test.go`、`internal/engine/review_source_select_test.go`、`internal/engine/worker_lifecycle_facts_test.go`、`internal/memory/notes_draft_test.go`、`internal/memory/notes_draft_bench_unix_test.go`。
- 变更规则：同步 README、ARCHITECTURE、API、DATA_MODEL、FEATURES、TESTING、CHANGELOG 及来源/批次/存储回归；来源：用户 2026-10-19 的记忆实际效果对照及现有链路修复要求。

## C-026 Project working-directory selection

- 状态：active；类型：lifecycle / compatibility / security；作用范围：项目新建／编辑对话框、原生桌面目录桥接、工作目录保存及运行解析；关联功能：`F-120`。
- 契约内容：选择目录入口仅在当前窗口具有真实原生桌面桥接时显示，不以 URL 的 shell 标记或引擎 mode 推断。系统选择器只选择单个现存的绝对目录。原生端校验请求来源和目录路径，选择成功只填入工作目录草稿，保存沿用现有项目接口及工作目录校验。非空目录路径不裁剪首尾空白；路径标准化仍使用 filepath.Clean。全空白保留托管目录行为。保存、编辑和实际运行解析必须指向同一所选目录；路径失效时拒绝并保留草稿，不回退到去空白后的同名目录。
- 允许行为：手动输入绝对路径、取消选择、清空后使用 zwai 管理目录；禁止行为：将文件或不存在的路径当成已接受的工作目录，将目录选择变成上传／复制操作，浏览器提供无法获得本机绝对路径的选择入口。
- 失败语义：取消不清空原值；选择错误在工作目录字段下显示，保留当前草稿并允许重试；不变量：关闭或切换项目后迟到的选择结果不能改变后续对话框，目录权限与项目删除边界不变；边界条件：桌面和浏览器共享同一引擎、目录在选择后被删除、选择器未结束时关闭对话框。
- 证据：实现 `frontend/src/components/app/project-dialog.tsx`, `frontend/src/components/app/workdir-input.tsx`, `frontend/src/lib/workdir.ts`, `internal/desktop/directory.go`, `internal/engine/project.go`；测试 `frontend/src/components/app/project-dialog.test.tsx`, `frontend/src/components/app/workdir-input.test.tsx`, `frontend/src/lib/workdir.test.ts`, `frontend/e2e/workdir.spec.ts`, `internal/desktop/directory_test.go`, `internal/engine/project_test.go`, `internal/engine/project_workdir_identity_test.go`。
- 变更规则：同步 README、FEATURES、TESTING、ARCHITECTURE 和 CHANGELOG，回归原生来源／目录校验、取消／迟到结果、Web 降级与保存；来源：用户 2026-10-13 避免手动输入项目工作目录的要求。

## C-027 Automatic bounded restart handoff

- 状态：active；类型：lifecycle / consistency / compatibility；作用范围：swarm、engine、lease、server、PC shell；关联功能：`F-110`、`F-130`、`F-150`、`F-160`、`F-330`。
- 契约内容：不同构建在当前 checkout 的前端准备成功后自动交接，无终端确认；准备后的引擎发现／交接检查、旧进程退出、新引擎监听共用 30 秒预算，依赖安装／前端构建时间不计入该预算。准备失败或取消时旧引擎继续服务；准备成功不代表后续进程已就绪。交接先冻结新工作，覆盖活动及停泊 registry；先保存确定性检查点，再在统一截止时间内尽力收集 worker 摘要；不能为每个 worker 重新计算 30 秒。证据：`cmd/zwai/engine_frontend_test.go`。
- 启动预算边界：普通冷启动的 2 分钟就绪等待不能延长本项 30 秒预算；启动等待、lease 竞争和子进程回收都遵守调用方剩余截止时间。证据：`cmd/zwai/engine_boot_test.go`、`cmd/zwai/engine_restart_budget_test.go`。
- 恢复不变量：保留原 turn／worker ID、真实完成结果、原任务与历史、未消费消息及 watchdog 预算；交接摘要／停止不等于完成、失败或用户取消。人类问答和迭代上限暂停不能被重启绕过；成功 ACK 的回答、继续／停止决定必须保留；已有权威终态的遗留 running 行只能收敛状态，不能重新执行；后台 worker 不能被会话 `running=false` 漏掉，启动后直接恢复其执行而不提前未来唤醒，且活跃后台 worker 阻止空闲退出。检查点的完整指令／历史不得离开 PC，手机仅保留空正文与序号。
- 失败巡检下的后台 worker：最新 turn 为 scheduled error 时，只有有效 manager `workers_retained` 回执中明确列出的原 worker ID 才可从检查点恢复，且恢复时仍须有 `C-009` 定义的当前有效目标或同对话 wake。回执不是重试许可；不重新执行已失败的 manager，不改变原 turn/run error、错误原因或未读状态，不提前未来唤醒。缺失／无效回执、无当前归属、人工 Stop、cleanup、检查点 HumanStopped 或 pause 均不能据旧错误或旧运行名册恢复。历史未记录回执的 error 不自动补造归属。证据：实现 `internal/engine/restart_parked.go`、`internal/engine/worker_ownership.go`；测试 `internal/engine/worker_ownership_test.go`；变更类型：Changed；来源：GitHub Issue #89。
- 收尾顺序：正常收尾与重启接管必须互斥排序，覆盖终态判断、worker 停泊／清理和终态持久化，不能仅凭 idle 通知报告交接成功；先收尾的 turn 必须完成持久化，先接管的 turn 必须保留 worker 检查点。等待收尾使用同一截止时间，超时不能部分接管，也不能返回 ready，后续可重试。重启取消不能被写成权威的用户 `interrupted` 终态；真实的人类 Stop 仍必须取消。证据：`internal/engine/goal_session.go`、`internal/engine/restart_finish.go`、`internal/engine/restart.go`、`internal/engine/restart_finish_test.go`、`internal/engine/restart_finish_ownership_test.go`。
- 失败语义：有能力的旧引擎必须持久化成功后才返回 `ready:true`；保存失败明确报错并允许重试，不能静默降级为强杀。已释放 runtime 但尚无持久化终态的正常收尾同样必须拒绝替换，不能把 idle 当作保存成功。证据：`internal/engine/restart_finish_storage_test.go`。旧引擎无交接 API 时允许有界自动停止并从已有事件恢复，但不宣称保全旧版本未存储的内存消息。未知工具结果必须标明未确认并先核验副作用，不保证任意外部操作 exactly-once。
- 允许行为：截止后取消仍未收尾的模型／工具，使用已保存的检查点继续；禁止行为：无限等待模型、把摘要当任务完成、重启时重复创建 worker、将未确认外部操作盲目重放。启动／存储／模型依赖异常可明确失败，不能伪造 30 秒内任务已恢复。
- 证据：实现 `handoff.go`、`internal/engine/restart.go`、`cmd/zwai/engine_restart.go`、`internal/server/restart.go`；测试 `handoff_test.go`、`internal/engine/restart_test.go`、`cmd/zwai/engine_restart_acceptance_test.go`、`internal/server/restart_http_test.go`、`frontend/src/lib/transcript-restart.test.ts`。
- 变更规则：同步 CLI、API、持久化格式、恢复及真实进程验收、UI 通知和 CHANGELOG；来源：用户 2026-10-19 的自动交接重启需求。

## C-028 Image intake and sent-image preview retain vision bytes

- 状态：active；类型：data / compatibility；作用范围：PC composer 的粘贴／拖入图片、视觉请求、输入图片存储与桌面／Web 已发送图片预览；关联功能：`F-140`。
- 契约内容：真实 image MIME 继续走视觉输入；空、通用 binary 或 UTI 标签且名称为 png/jpeg/gif/webp 的文件也走视觉输入。显式 text/PDF 不因图片后缀改为视觉输入。客户端归一化 MIME 声明但不改原图片字节；服务端保留内容类型嗅探及声明匹配校验，不信任扩展名替代该校验。
- 预览交互：已发送缩略图是可用鼠标及 Enter／Space 操作的按钮；每次打开所选图片的现有 input-images 地址，刷新重载后仍可打开。大图按原比例适配窗口，Esc、遮罩或关闭按钮关闭后焦点回到打开它的缩略图。预览打开时 Esc 由弹层优先消费，不得关闭底层 Find 搜索；预览关闭后继续按 Esc 也不得停止正在运行的任务，沿用 `C-002` 的 PC 停止边界。
- 允许行为：兼容 Finder/WKWebView 的通用文件类型；禁止行为：图片仅变为 uploads 路径，或客户端 MIME 推断绕过服务端校验。
- 失败语义：不支持的格式或真实类型不匹配沿用既有输入错误；大图加载中有可见状态，加载失败明确显示错误且仍可关闭；不变量：有效图片在预览、请求、存储与重载中保留原字节及可解码尺寸；边界条件：JPEG MIME 别名、大小写扩展名、明确非图片类型、多图所选身份。
- 证据：实现 `frontend/src/lib/paste-image.ts`, `frontend/src/components/app/input-thumbs.tsx`, `frontend/src/App.tsx`, `internal/engine/input.go`；测试 `frontend/src/lib/paste-image.test.ts`, `frontend/src/components/app/composer-image-drop.test.tsx`, `frontend/src/components/app/input-thumbs.test.tsx`, `frontend/src/App.test.tsx`, `frontend/e2e/image-input.spec.ts`；Schema：现有 turn 图片请求。
- 变更规则：同步 Files/attachments 功能、README、API、TESTING、CHANGELOG；使用可解码的有效图片验收，不能只用魔数或 img src。来源：本地图片拖入修复及用户 2026-10-21 的已发送图片放大需求。

## C-029 Shared chat-service recovery backoff

- 状态：active；类型：lifecycle / consistency / compatibility；作用范围：provider pool、manager、subagent、目标续轮及 title／review／compact 等模型调用；关联功能：`F-130`、`F-150`、`F-160`、`F-170`。
- 契约内容：同一进程的 chat pool 按 URL origin 共享故障状态，跨 provider ID、模型、角色和轮次；健康请求保持原并发。暂态服务故障（408、429、5xx）、连接失败、流空闲／中途断流和成功响应解码错误触发 5→10→20→30 秒封顶指数冷却。故障前同代并发请求只计首次失败，过期结果不能改变较新故障。
- 冷却结束仅允许一个恢复探测；只有完整 Generate 成功或正常读完模型流才清零并恢复并发，HTTP 200 或流握手成功本身不足。Chat Completions HTTP 模型流必须读到 SSE `[DONE]` 行或显式 finish reason；缺少两者的 EOF 返回 `io.ErrUnexpectedEOF`，保留故障档位及已收到的 usage。Responses 适配器只在合法 `response.completed` 后输出显式 finish reason，原始 `[DONE]` 不是该协议的完成事件。鉴权、已解析参数错误、context overflow 不新增服务失败次数；取消和非暂态探测错误释放许可但保留故障档位并再次冷却。清客户端缓存不清故障状态；重启不保留进程内冷却。
- 允许行为：正常模型工具往返、独立 origin 并发、用户停止等待；禁止行为：每个 subagent 单独计时造成并发重试洪峰，或内部首字节超时重发绕过冷却；不变量：provider 门只延后请求，不新增自动重试次数，不改变 `C-019` 的 worker watchdog 预算。
- 失败语义：原始模型错误及 `model_retry` 分类保留；等待取消立即返回可判断的 context 错误。冷却等待以 `model_backoff` 事件写入原 turn 和真实 agent，`text` 为 `{attempt,wait_ms}`，不伪造 LLM 请求或生命周期；桌面 Chat、Trace Full log 及重放显示可读等待文字；安静计划轮次的迟到等待遵循 quiet 策略，不重新显示 Chat 通知，原始事件仍保留在 Trace。后续故障可延长已观察的等待，不承诺该毫秒值是最终延迟；等待已有探测结果时 `wait_ms: 0` 表示余时未知。
- 边界条件：已发出的并发请求不会被门自动撤销；仅控制本进程 chat pool，不控制其他客户端、其他进程、catalog discovery 或 embedding。探测中的其他调用继续等结果；成功前不会一齐重发。每次 HTTP 请求的响应头超时从实际发送起计，等待冷却不消耗该预算，但仍受调用者 context 的总截止时间约束。放弃读取的调用者必须取消请求 context；单独关闭 Eino reader 不会中断 SDK 的阻塞读取，不能把它视为恢复成功；Stop 取消该 context。
- 证据：实现 `internal/config/backoff.go`、`internal/provider/backoff.go`、`internal/provider/model_backoff.go`、`internal/provider/provider.go`、`internal/provider/http.go`、`internal/provider/stream_completion.go`、`internal/engine/model_backoff.go`、`frontend/src/lib/transcript-backoff.ts`；测试 `internal/provider/backoff_test.go`、`internal/provider/model_backoff_test.go`、`internal/provider/backoff_integration_test.go`、`internal/provider/stream_completion_test.go`、`internal/provider/backoff_responses_test.go`、`internal/provider/backoff_http_test.go`、`internal/engine/model_backoff_test.go`、`frontend/src/lib/transcript-backoff.test.ts`、`frontend/e2e/api-error.spec.ts`。
- 变更规则：同步 CONFIG、API 事件、README、架构、功能及 CHANGELOG，回归同源多调用者、取消、迟到结果、完整流、内部重发、Trace 与 UI；来源：用户 2026-10-19 对模型及 subagent 高频重试压垮低配置服务的修复要求。

## C-030 Provider cloud tools retain protocol and execution origin

- 状态：active
- 类型：API / compatibility / security / lifecycle
- 作用范围：config、provider、engine、server、PC Settings/Trace
- 关联功能：`F-175`、`F-160`、`F-170`
- 契约内容：
  - 空或 `chat_completions` 的 `api_mode` 保留现有 POST /chat/completions 行为；`responses` 显式选择 POST /responses，不根据名称猜测协议。
  - `builtin_tools` 默认空；非空只允许 Responses。每项为完整对象，`type` 为非空字符串、不得重复，当前支持 web_search/web_search_preview/file_search/code_interpreter，嵌套参数原样保存。
  - 云端工具与实际 Agent 本地函数声明合并；只有 function_call 进入本地工具调度，云端 *_call 不能伪造成本地调用、spawn 或本地成功回执。
  - 云端调用状态同模型请求归属原 turn/agent/provider/model，保存在 `builtin_tool` 事件，并可通过一 ID Trace 重建；同请求中重复的同 ID/状态不重复计数。quiet 定时检查保持 Chat 安静，同时在 Trace Full log 保留收据。流式失败或取消不得伪造完成。
  - 回答文本保持流式；URL 引用保留为可点击 Markdown 来源。无 billed usage 的历史预算取可见消息与实际 Responses 回放 JSON 估算的较大值，纳入云端输出及 encrypted reasoning，不计被剔除的调用。协议错误保留原始模型调用错误类别，不能归咎于本地工具 JSON。
- 允许行为：模型自行选择不调用已声明工具；由服务商验证更具体的参数或模型支持情况。
- 禁止行为：静默启用工具、将云端回执本地再执行、把任意需客户端审批/执行的类型透传为已支持功能。
- 失败语义：非法配置在写入前失败，Settings 保留草稿；服务商/流错误沿现有模型失败与有界重试链路处理。
- 不变量：provider API key 继续仅以 has_api_key 暴露；工具配置不授予本地额外权限。
- 边界条件：同 provider ID 的 PUT 中省略 api_mode/builtin_tools 或发送 JSON null 保留该字段旧值，显式 [] 清空工具；其他 PUT 字段沿用原语义。旧 YAML 无需迁移。云端资源参数必须由用户提供，不保证服务商实际可用。
- 证据：实现 `internal/config/provider_tools.go`、`internal/provider/responses.go`、`internal/engine/builtin_tools.go`；测试 `internal/config/provider_tools_test.go`、`internal/server/settings_provider_tools_test.go`、`internal/engine/builtin_tools_test.go`、`frontend/e2e/builtin-tools.spec.ts`。
- 变更规则：类型支持、协议或执行来源变化必须同步配置/API/功能文档、适配器及 Trace 回归测试。
- 来源：2026-10-21 provider 云端内置工具配置需求；[OpenAI web search](https://developers.openai.com/api/docs/guides/tools-web-search?api-mode=responses)。

## C-031 Native desktop tests own their guest lifecycle

- 状态：active
- 类型：lifecycle / security / consistency
- 作用范围：原生桌面验收、UTM 来宾、测试数据与证据；关联功能：`F-430`。
- 契约内容：
  - macOS 原生测试仅在 UTM `ci-macOS` 来宾运行；宿主只编排、构建、传输和收集证据。不得启动宿主测试窗口、抢前台或修改用户的运行实例、缓存 bundle 和正常数据。
  - 同一来宾用户同时只运行一个用例；每例使用全新私有 app 副本、home/cache、数据库、配置及 workspace，场景子进程继承私有 `HOME` 和 `ZWAI_HOME`；checkout 启动写入的 macOS 缓存 bundle 也属于本例。仅同一恢复场景内部步骤允许复用数据。
  - 用例成功、失败、超时或取消时，先保留证据，再停止本例 app、engine 及子进程并确认退出，然后删除本例临时数据及 home/cache；清理失败必须失败并阻止下一例启动。
  - `cleanup.json` 记录用例身份、清理完成状态、剩余进程和数据删除结果。日志、断言、截图及版本/源码/安装包证据保留在临时数据外。
  - 日常 native 回归在来宾和候选就绪后以整轮 60 秒为成功预算，包含准备、启动、行为断言、证据及清理；runner 的 `--budget` 默认 60 秒，场景 `--timeout` 不能另起一个脱离总预算的时钟。耗尽场景可用预算后不得再启动进程。总耗时越界不能返回成功，但必须继续清理并保留超时和清理结果。
  - 冷构建、首次传输和刻意长时恢复另列耗时与显式预算，不冒充一分钟回归。复用已准备候选不允许复用用例数据。快速场景必须保留原生窗口内容、截图及真实离线 worker/tool/Trace 断言；只有 HTTP 或进程证据时不得验收原生行为。
  - 快速场景的 AX helper 在候选准备阶段编译并记录源码、构建参数及二进制哈希，计时场景拒绝缺失／过期收据，不以冷编译延长周期。输入前核对本例 PID 和 WebView origin／desktop shell／PID，输入完整读回且唯一 Send 可用后才发送；截图必须来自本例 PID 的窗口。
  - helper 返回的 bundle 与目标 bundle 必须实际存在且经 `Path.resolve(strict=True)` 解析后相同；同一目录的 Darwin 路径别名允许通过，其他目录或已缺失目标必须拒绝，origin、case 与 PID 校验不得放宽。helper 的原始 stdout、stderr、退出码及请求身份先写入 `smoke-native-last.json`，再检查返回码、解析 JSON 和验证身份，失败响应也必须留证。
  - AX children 按图遍历，并以 `CFEqual` 判断元素身份；重复引用不得制造重复 composer／Send，不同身份的同文字控件不得合并。所有访问（含空节点和重复边）共享 5000 次上限，新节点的深度上限仍为 45；真实超限立即停止遍历并失败，不得用截断后的控件集合证明唯一性。
  - 访问诊断按本次 probe 区分宿主 VM 网卡／旧地址路由、来宾身份与 MAC、carrier／IPv4／默认路由、sshd loaded／disabled 及实际 SSH 通道；on-demand sshd 暂无监听进程不能据此判为 disabled。旧 IP、UTM started 或旧 ready 文件只作线索；共享通道须有本次请求身份匹配的真实响应，不能用宿主／来宾墙钟差判失效。不支持的 backend 操作不得重复当恢复。纯分类器严格校验输入，不回显私有身份；`restart_admitted` 只是必要准入，`native_acceptance` 始终为 pending，不执行重启或验收窗口。
  - 宿主访问入口复用仅当前用户可读的私有 profile，以选定 MAC 的 DHCP 候选、实际路由及严格 pinned SSH 验证目标；默认只读，SSH 未证时使用匹配当前 probe／nonce／源码哈希／来宾身份的真实队列响应，不把历史 ready 当成功。递归发现历史队列作盘点候选，维护只使用本次 guest 进程／效果证据确认的 current peer roots；未知盘点拒绝维护。发出请求前持久化不含 nonce 的 ID／queue／timeout，超时保留未知效果及未确认取消，不自动重试。`--repair-network` 只触发有 guard 的一次 DHCP，成功仍须新 pinned SSH 验证；不自动 reboot。证据：实现 `tools/native_macos_access.py`；测试 `tools/test_native_macos_access.py`；来源：GitHub Issue #97。
  - 访问维护与原生用例争用同一来宾用户 native-case lock inode；维护不得创建、截断、删除或改写该锁回执，须确认 `cleanup_complete=true`、`remaining_owned_pids=[]` 及列出的 peer 队列均无未完成 claim，仅豁免真实存在的精确当前 queue/request。peer 终态须匹配 id／nonce、`status=exited` 和整数退出码。DHCP 维护核对来宾、UID、接口与 MAC、唯一启用的既有 DHCP service，持锁只续租一次，再读取网络；仍断网返回 `needs_host_recovery`，命令退出码 0 不等于访问恢复。`hold-maintenance` 有界持有实际 flock，持续检查 peer，以本次 token 释放或 TTL 到期终止，记录释放终态；需要 VM 重建时维护归属必须覆盖实际动作，不能只拿到一次探针准入。证据：实现 `tools/native_macos_access_state.py`、`tools/native_macos_access_guest.py`；测试 `tools/test_native_macos_access_state.py`、`tools/test_native_macos_access_guest.py`；变更类型：Changed；来源：GitHub Issue #97、用户对来宾访问重复失败的纠正。
- 允许行为：宿主执行无窗口单元测试、headless 浏览器 E2E 和只读交付核验；来宾不可控时记录原生验收 pending。
- 禁止行为：来宾失败后回退宿主；按程序名称全局终止进程；删除用户或其他用例的数据；把程序启动或 runner 的成功退出当作窗口行为验收。
- 失败语义：物理宿主在启动前拒绝；锁冲突拒绝并发用例；清理未完成返回失败并保留待清理现场。
- 不变量：一个用例的退出不能破坏另一个实例；下一用例不继承上一用例数据。
- 边界条件：硬杀 runner／断电无法运行 finally，后续用例必须识别未清理记录并拒绝启动，核实并完成同一用例清理后才能继续。历史未知来源的残留不得按名称一并清空。
- 场景与辅助进程必须保留继承的 `ZWAI_NATIVE_CASE_ID`，使已脱离父进程／新建 session 的辅助进程仍可归因；不得主动清除标记后创建脱管 daemon。测试 runner 不是 OS 进程容器，不终止共享系统服务。
- 证据：实现 `tools/native_macos_case.py`, `tools/native_macos_smoke.py`, `tools/native_macos_ax.m`, `Makefile`, `AGENTS.md`；测试 `tools/test_native_macos_case.py`, `tools/test_native_macos_smoke.py`；操作说明 `docs/TESTING.md`, `docs/RELEASE.md`。
- 变更规则：同步 runner、生命周期回归、开发规则及原生验收文档；保留源码身份与真实窗口行为的独立证据边界。
- 现场证据：[2026-10-21 ci-macOS 串行窗口与清理验收](diagnostics/native-isolation-20261021.md)；仅接受其冻结候选和明确成功路径。
- 计时证据：[2026-10-22 日常三轮一分钟验收](diagnostics/native-minute-20261022.md)；含独立核验的完整单轮 14.50 / 12.30 / 12.06 秒，真实原生输入、结果、Trace、截图及进程/私有数据清理全部通过，仅接受其冻结输入。
- 来源：用户 2026-10-21 反馈测试抢占桌面及 UTM 中程序、数据残留；Added。

## C-032 Phone fenced source retains its text

- 状态：active
- 类型：compatibility / data
- 作用范围：手机配对对话和直连聊天的 Markdown；关联功能：`F-220`, `F-230`。
- 契约内容：已知语言围栏复用现有源码语言识别、别名与词法器，使用手机明暗主题语法色；空标签、未知标签及普通文本不猜测语言。高亮只添加渲染节点，代码文本与复制内容保持 Markdown 解析后的原文和空白。流式追加不得重建整个围栏或丢失复制状态。
- 允许行为：开放的流式围栏随输入重新分词；公式和有效图表继续使用专用渲染器。
- 禁止行为：复制语言标题或 token 标记；把源码当 HTML 执行；将用户示例内容写成识别规则。
- 失败语义：不支持的语言显示完整纯文本；无效图表沿用代码回退。
- 不变量：高亮前后的代码字符相同，配对与直连聊天使用同一渲染器。
- 边界条件：C++ 大小写与别名、空或未知语言、未闭合围栏、连续追加后闭合、制表符、尾随空格、中文和 HTML 字面量。
- 证据：实现 `mobile/src/components/markdown.tsx`, `frontend/src/lib/source-highlight.ts`, `frontend/src/lib/source-lang.ts`, `mobile/src/index.css`；测试 `mobile/src/components/markdown-highlight.test.tsx`, `mobile/src/components/markdown.test.tsx`, `mobile/e2e/markdown-highlight.spec.ts`, `mobile/e2e/ios-code-highlight.swift`, `mobile/android/app/src/androidTest/java/com/lubyruffy/zwai/CodeHighlightTest.java`。
- 变更规则：同步手机使用文档、关联功能、渲染与复制回归；改动主题色或移动 WebView 行为时验证两端原生应用。
- 来源：GitHub Issue #91；Fixed，恢复手机代码围栏的语法高亮。

## C-035 Interrupted generations retain cause and recovery

- 状态：active；类型：lifecycle / consistency / compatibility；作用范围：模型流、engine 事件、Trace、PC 与配对手机；关联功能：`F-130`、`F-160`、`F-222`、`F-330`。
- 契约内容：每个被中断的模型输出保留独立身份、已记录原因和实际恢复状态；旧思考与不再运行的流式参数必须停止动画。中断原因不能被下一次生成或当前 worker 状态覆盖；恢复说明表示已重新进入生成，不代表任务完成或重试副作用安全。
- 允许行为：交接重启与异常进程退出后恢复同一 worker；用户显式续办更新恢复状态。禁止行为：为中断输出伪造普通 `finished`、将恢复中的 worker 标为失败、把 manager 巡检结束当成 worker 中断、按文字相似度合并不同输出。
- 失败语义：区分交接重启、进程退出、取消、超时、模型错误及输出额度；无法证明具体退出原因时明确未记录。没有已安排恢复的错误不得宣称正在恢复；开始新生成不能宣称任务已经成功。
- 原因来源：已观察到的模型错误或 finish reason 优先于迟到的 Stop／Shutdown；Stop 只能取消恢复安排，不能改写此前原因。运行时放弃标记不等于计划交接；冷启动使用的交接 checkpoint 必须晚于该次生成开始。配对推送裁剪只缩短 detail，不破坏原因及恢复状态。
- 不变量：生命周期证据进入原 turn 的 Trace，跨 manager 检查的同一输出仍使用相同身份；实时、冷启动、分页及按需代理日志按持久事件身份幂等，恢复不抹除历史中断。
- 删除边界：生成缓存仅来自持久事件；删除对话或项目与迟到的生成回调按同一所有权顺序执行。已删除的对话不得被回调重新缓存或写入孤立生成事件。
- 存储失败：无法读取输出归属或持久化回执时必须告警，不发布／缓存假定成功的回执，也不得把旧输出归入当前轮次；后续读取恢复后重新构建归属。
- 边界条件：仅思考、部分正文和未完成工具参数均可中断；输出限额截断的非法工具参数必须在真实执行器之前拒绝，不能只关闭界面动画。正常完成输出不因之后进程重开被倒推为中断。旧事件缺少身份时不臆造身份或原因。
- 证据：实现 `signal.go`、`internal/engine`、`frontend/src/lib/transcript.ts`、`mobile/src/lib/transcript.ts`；测试 `generation_test.go`、`internal/engine/generation_test.go`、`internal/engine/generation_http_test.go`、`frontend/src/lib/transcript-generation.test.ts`、`mobile/src/lib/transcript-generation.test.ts`。
- 变更规则：同步 API、库通知文档、数据模型、功能清单、两端 reducer、Trace 和 UI/原生回归；变更类型：Added；来源：GitHub Issue #95，用户 2026-10-22 要求逐次生成说明原因和恢复状态。
