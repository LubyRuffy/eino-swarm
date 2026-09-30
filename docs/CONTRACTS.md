# Current Contracts

Only current behavior is listed here. See [FEATURES.md](FEATURES.md) for user entry points and [CHANGELOG.md](../CHANGELOG.md) for history. Contract IDs are stable.

## C-001 One engine per data directory

- 状态：active；类型：lifecycle；作用范围：desktop、web、tui、engine；关联功能：`F-110`, `F-120`, `F-320`。
- 契约内容：同一数据目录只由一个 engine 进程持有；客户端连接同一 loopback 服务；不同版本替换引擎时先等待安全时机。
- 允许行为：多个 shell 同时连接；禁止行为：多个进程同时写同一数据库；失败语义：引擎不可达或租约冲突需明确报告；不变量：一个目录一把 lease；边界条件：进程异常结束后可恢复。
- 证据：实现 `internal/lease`, `internal/app`, `cmd/zwai`；测试 `internal/lease`, `internal/app`；变更规则：同步架构、CLI 和恢复测试；来源：`ARCHITECTURE.md` 和 Git 历史。

## C-002 One turn per conversation

- 状态：active；类型：consistency；作用范围：engine、server、desktop、phone；关联功能：`F-130`, `F-150`, `F-220`, `F-310`。
- 契约内容：同一对话同时只有一个活动 turn；第二条普通消息进入 follow-up，steer 指向活动 turn；排队消息和尚未被 manager 读取的 steer 可以拉进输入框后重新排队，已读 steer 不能改；手机在等待队列上执行“中断插入”时，先将队首消息转为本轮 steer，再请求中断当前 manager 步骤，其他消息继续排队；并发 API 冲突返回可识别的状态。
- 允许行为：排队和插入；禁止行为：在同一对话并行启动两个 turn；失败语义：`ErrBusy`/`ErrIdle` 对应 409 与 code；不变量：turn 状态可恢复；边界条件：中断和等待。
- 证据：实现 `internal/engine`, `internal/server`, `mobile/src/app.tsx`, `mobile/src/lib/phone-turn.ts`；测试 `internal/engine`, `frontend/src/store/app-steer.test.ts`, `mobile/src/lib/phone-turn.test.ts`, `mobile/e2e/walkthrough.spec.ts`；变更规则：同步 API、手机 RPC 和竞态测试；来源：`AGENTS.md`、GitHub Issue #31。

## C-003 Stored events and streamed order

- 状态：active；类型：data / consistency；作用范围：engine、store、SSE、phone；关联功能：`F-130`, `F-150`, `F-220`, `F-310`, `F-330`, `F-410`。
- 契约内容：完成事件按同一锁分配 seq、保存、广播；流式 delta 包含截至当前的完整文本，不逐 token 持久化；慢订阅者通过数据库追赶，事件 kind 是回放格式的一部分。
- 允许行为：delta 丢失后下一帧恢复；禁止行为：丢弃已存储事件或随意重命名 kind；失败语义：落后订阅者转追赶；不变量：存储顺序与流顺序一致；边界条件：重连和回放。
- 证据：实现 `internal/engine`, `internal/store`, `internal/remote`；测试 `internal/engine`, `internal/remote`；变更规则：同步 API、数据模型和 wire 测试；来源：`AGENTS.md`。

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

- 状态：active；类型：compatibility / security；作用范围：pairlink、phone、PC remote；关联功能：`F-170`, `F-210`, `F-220`, `F-222`, `F-224`, `F-230`。
- 契约内容：手机通过 pairlink 加密 RPC 与 PC 通信，不直接调用 PC 的 `/api`；相同事件 kind 和 seq 可在手机回放；断线重连不解除绑定。回答 `ask_user` 时客户端必须使用 PC 规范化后的 question ID（如 `test-window` → `test_window`），RPC 拒绝必须显示原因并保留草稿，接受后不得重复提交。子 Agent 的事件按 `agent_id` 与主 Agent 分开回放，`spawned` 启动指令正文不离开 PC；手机只显示协议允许并按 `event_chars` 裁剪的活动。
- 允许行为：relay/direct 路径切换及手机查看子 Agent 角色、状态和截断记录；手机页头计数仅包含运行中的子 Agent，列表按进行中／已结束分组，完成和失败记录仍可查看；禁止行为：把本地 PC API 或子 Agent 启动指令直接暴露给手机，或把子 Agent 回答混作主 Agent 回答；失败语义：连接错误与 host offline 可区分；不变量：一个 PC 对话跨端一致，同一 agent 的流式文本只更新自己的记录；边界条件：ticket 过期、掉线、历史翻页、子 Agent 续办和重连。
- 证据：实现 `internal/remote/clip.go`, `internal/engine/ask.go`, `frontend/src/lib/transcript-ask.ts`, `mobile/src/lib/link.ts`, `mobile/src/lib/ask.ts`, `mobile/src/lib/phone-turn.ts`, `mobile/src/lib/transcript.ts`, `mobile/src/lib/session.ts`, `mobile/src/components/ask-card.tsx`, `mobile/src/components/thread-screen.tsx`；测试 `internal/remote/watch_test.go` 的 `TestSpawnedInstructionNeverLeavesTheHost`, `frontend/src/lib/transcript-ask.test.ts`, `mobile/src/lib/ask.test.ts`, `mobile/src/lib/phone-turn.test.ts`, `mobile/src/components/ask-card.test.tsx`, `mobile/e2e/ask-answer.spec.ts`, `mobile/src/components/thread-screen.test.tsx`, `mobile/src/lib/transcript.test.ts`, `mobile/src/lib/session.test.ts`, `mobile/e2e/walkthrough.spec.ts`；变更规则：同步 API、手机测试和数据模型；来源：`ARCHITECTURE.md`、GitHub Issue #30 的 A 方案确认、GitHub Issue #47 的混合状态截图、GitHub Issue #51 的提问提交失败。

## C-007 Phone inbox pagination

- 状态：active；类型：consistency；作用范围：phone inbox；关联功能：`F-220`。
- 契约内容：定期 poll 是第一页增量更新，已用 More 加载的旧行保留；离开第一页的行不被重复塞回。
- 允许行为：第一页内容更新；禁止行为：整表替换抹掉已翻页数据；失败语义：失败保留已显示列表；不变量：翻页成果不因 poll 消失；边界条件：同一行跨分组或状态变化。
- 证据：实现 `mobile/src/lib/inbox-window.ts`, `mobile/src/app.tsx`；测试 `mobile/src/lib/inbox-window.test.ts`, `mobile/e2e/walkthrough.spec.ts`；变更规则：同步手机走测；来源：`AGENTS.md`。

## C-008 Durable local data

- 状态：active；类型：data；作用范围：store、search；关联功能：`F-120`, `F-410`。
- 契约内容：对话、turn、完成事件和计划保存在本机数据目录；测试使用临时目录，不写用户数据目录。
- 允许行为：搜索索引后台更新；禁止行为：测试污染用户目录；失败语义：存储错误需显式显示；不变量：事件可按 turn 恢复；边界条件：进程重启和索引延后。
- 证据：实现 `internal/store`, `internal/search`；测试 `internal/store`, `internal/search`；Schema `docs/DATA_MODEL.md`；变更规则：同步 schema、迁移和恢复测试；来源：`AGENTS.md`。

## C-009 Editable configuration and scheduled waits

- 状态：active；类型：compatibility / lifecycle；作用范围：config、provider、engine；关联功能：`F-150`, `F-170`, `F-230`, `F-420`。
- 契约内容：端点、模型等配置有默认值且可在 Settings 编辑；`OPENAI_*` 只用于首次空字段填充；计划触发或取消需保留状态。定时触发与立即执行没有任何工具结果或显式 `report_schedule` 时，只在原 turn 继续一次；仍无证据则 turn/run 为 error、unread=true，不把开场话当 findings 或空回答当 quiet。显式空 findings 保持 quiet；真实工具结果后的省略报告兜底保持兼容，模型错误清理生成的虚拟工具结果不算执行证据。判断不得依赖回答长度、标点、语言、任务样本或模型名。重试原因 `scheduled_no_activity` 进入同一 turn 的 Trace。
- 允许行为：用户更改设置；禁止行为：生产代码硬编码用户端点或模型；失败语义：配置或计划错误明确报告；不变量：用户设置优先于环境种子；边界条件：首次运行与重启。
- 续办展示：`scheduled_no_activity` 的桌面提示是继续执行定时检查，不冒称发生了模型错误；旧模型错误提示保持兼容。
- 证据：实现 `internal/config`, `internal/engine`, `internal/store`, `frontend/src/lib/transcript-notices.ts`；测试 `internal/config`, `internal/engine/schedule_completion_test.go`, `frontend/src/lib/transcript-goal.test.ts`, `frontend/e2e/schedules.spec.ts`；变更规则：同步 CONFIG、API 和计划测试；来源：`AGENTS.md`。

## C-010 Build artifacts and update channel

- 状态：active；类型：compatibility / lifecycle；作用范围：build、mobile update、macOS desktop update；关联功能：`F-190`, `F-240`, `F-430`。
- 契约内容：`make build` 先更新嵌入的前端；Android sideload 使用 GitHub Release 中版本匹配的 `zwai-<version>-android.apk`；iOS 手动检查说明 GitHub APK 不适用。手机新版本从 package 版本派生 Android versionCode 和 Xcode marketing/build 设置。macOS 桌面发布物是 `zwai-<version>-darwin-<arch>.zip`，根目录为 `zwai.app`。新版本仅在未分配待发布修复存在、主线自上一个公开源码 SHA 后提交超过三次，或最早待发布验收记录超过 24 小时时分配；跨日期、跨运行累计。`make release` 按台账只发布本批实际受影响且尚未交付的平台：desktop 为 macOS，mobile 为 Android 与 iOS；混合批次共享 tag 和源码 SHA，桌面单端发布无需增加手机 package/build number。各端门禁独立，最终缺任一必需端则非零。附件同名同 SHA-256 跳过，不允许覆盖；已发布 tag 必须对应冻结完整主线 SHA。TestFlight 只有 VALID、审核 APPROVED、内测 READY_FOR_BETA_TESTING、外测 IN_BETA_TESTING 且全部既定组关联时才完成。上传前查 builds/buildUploads、当前构建声明和北京日成功账本，每日最多一新构建成功；已接受上传、处理或审核等待均保存恢复信息，不重复上传。Mac 与 Android 更新器从本仓库最近的非 draft、非 prerelease 且包含本平台安全安装包的 Release 升级，不假定 `/releases/latest` 含每个平台；下载只跟随 github.com 与 GitHub 的 release 资源主机。
- 允许行为：不同渠道单独验收；禁止行为：把本地 APK/AAB 或未上传的 zip 称为远端发布，或从其他仓库安装；失败语义：构建、签名、下载错误可观察；不变量：用户可获取与显示版本一致的产物；边界条件：安装签名、新旧版本按数字比较、未打版本号的构建不提供升级。
- 修复结案与发布分开：需求行为、必要测试、文档及 review 通过，完整修复集成并推送 main 后关闭 Issue；提交数与等待时长未触发新版本也不延迟结案。关闭评论分别记录已发布端和待发布端，交付台账保留未发布记录（包含 closed Issues）。审计合并全部已验收代码记录的实际平台缺口，包括没有 iOS 台账行的 Mac-only 修复，去重且排除重复报告别名。已分配版本的旧批次只计入恢复集合，不借给下一版的提交数与等待时长；已公开交付的批次锁定版本和源码 SHA，仅恢复其未交付端。Issue 关闭不代表全端发布，发布失败仍须告警。
- 混合平台批次的公共 Release 状态按平台记录；每个 Issue 的台账只把实际必需的平台标为已发布，无关平台保留 `not_required_by_behavior_change`，并按该 Issue 的全部必需端独立计算交付完成。首个经远端验证的平台公开后立即将统一版本和源码 SHA 锁定到全部批次行，部分交付不得再次取得新版本资格。
- 批次平台范围是各 Issue 必需端与完整主线中其他经验证的桌面／移动行为变化的并集。额外变化必须记录从上一公开 SHA 到当前完整主线之间的提交 SHA 与平台门禁，不得改写无关 Issue 的平台状态。新批次分配时，仍缺平台的旧公开批次保留在历史索引；冻结版本恢复完成须更新旧 Issue 和旧批次，不得回退更新版本的公开源码基线。
- 证据：实现 `AGENTS.md`, `tools/audit_pending_batch.py`, `tools/release.py`, `tools/release_ios.py`, `Makefile`, `internal/release`, `internal/desktop/pack`, `internal/update`, `mobile/scripts/android-release.ts`, `mobile/scripts/sync-ios-version.ts`, `mobile/src/lib/app-update.ts`；测试 `tools/test_audit_pending_batch.py`, `tools/test_release.py`, `internal/release`, `internal/update`, `internal/desktop/pack`, `mobile/scripts/android-release.test.ts`, `mobile/scripts/sync-ios-version.test.ts`, `mobile/src/lib/app-update.test.ts`；变更规则：同步发布规则、mobile README、CLI、发布门禁和版本测试；来源：用户 2026-09-26 修复结案与发布独立约定、`mobile/README.md`, `docs/CLI.md`。

## C-011 Quoted conversation payload

- 状态：active；类型：data / compatibility；作用范围：desktop、phone、message wire；关联功能：`F-180`, `F-221`。
- 契约内容：只选对话正文可加入草稿；手机上的加入操作跟随有效选区并留在会话视口内，不替换或遮挡系统复制／全选菜单；引用可查看、编辑和移除；每段原文单独以 `<selected_text>` 包裹，输入的请求以 `<user_request>` 包裹；引用可单独发送，发送失败恢复引用和正文；显示用户消息时不把标签当正文。
- 允许行为：多段引用、空正文发送；禁止行为：把输入框或 UI 文本当作引用，或让正文被误解为引用；失败语义：无效选区不提供操作；不变量：PC/手机同一 wire 格式；边界条件：空白选区、换行和文本中有关闭标签。
- 证据：实现 `frontend/src/lib/quote.ts`, `mobile/src/lib/quote.ts`, `mobile/src/components/thread-screen.tsx`, `mobile/src/components/composer.tsx`；测试 `frontend/src/lib/quote.test.ts`, `mobile/src/lib/quote.test.ts`, `mobile/src/components/thread-screen.test.tsx`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/composer-layout.spec.ts`, `mobile/e2e/ios-wait-layout.swift`；变更规则：同步双端解析、发送测试和用户说明；来源：`README.md`、GitHub Issues #29、#44。

## C-012 One-ID troubleshooting

- 状态：active；类型：compatibility；作用范围：engine、server、CLI、Trace；关联功能：`F-130`, `F-160`, `F-320`。
- 契约内容：一个 turn id 可在 CLI、API 和 Trace 面板还原输入、事件、模型与工具调用、错误及结果；新增事件类型需进入这一路径。
- 允许行为：敏感值脱敏；禁止行为：丢掉诊断所需事件或输出令牌/完整 Cookie；失败语义：不存在的 id 报不存在；不变量：同 id 贯穿运行；边界条件：部分失败和重启恢复。
- 证据：实现 `internal/engine`, `internal/server`, `internal/store`；测试 `internal/engine`, `internal/server`；变更规则：同步 CLI、API 和 Trace 测试；来源：`AGENTS.md`。

## C-013 Phone model picker

- 状态：active；类型：compatibility / lifecycle；作用范围：phone composer；关联功能：`F-230`。
- 契约内容：手机输入框的模型按钮打开应用内分组列表，列表沿用应用字号并允许长模型名换行；选择后保留服务商和模型的配对关系，关闭后发送使用新选择。返回键先关闭列表，不退出对话或应用。
- 允许行为：点击遮罩、关闭按钮或 Escape 关闭列表；禁止行为：依赖不受应用样式控制的系统原生选项弹窗，或把不同服务商的同名模型混为一个选项；失败语义：列表关闭后保留原选择；不变量：选择只改变当前输入框的服务商和模型；边界条件：长模型名、多服务商和 Android 系统返回键。
- 证据：实现 `mobile/src/components/model-picker.tsx`, `mobile/src/components/composer.tsx`；测试 `mobile/src/components/composer.test.tsx`, `mobile/src/components/direct-chat-screen.test.tsx`, `mobile/e2e/walkthrough.spec.ts`；变更规则：同步手机模型选择测试和使用文档；来源：GitHub Issue #34。

## C-014 Phone client pagination feedback

- 状态：active；类型：lifecycle；作用范围：phone Clients 分页；关联功能：`F-223`。
- 契约内容：点击 Clients 的 More 后立即显示加载动效和文字，等待 RPC 时禁用重复点击；成功时追加较旧任务，失败时保留现有列表、显示错误并恢复按钮。
- 允许行为：不同工具分组独立显示任务；禁止行为：无反馈的重复分页、失败后永久禁用或静默丢弃异常；失败语义：已有任务继续可见，错误显示在手机页面；不变量：一个分页请求在途时不重复发起；边界条件：慢响应、RPC 拒绝、切换 PC。
- 证据：实现 `mobile/src/lib/client-poll.ts`, `mobile/src/components/client-groups.tsx`；测试 `mobile/src/app.test.tsx`, `mobile/src/components/client-groups.test.tsx`, `mobile/e2e/walkthrough.spec.ts`；变更规则：同步手机 UI 测试和使用文档；来源：GitHub Issue #35。

## C-015 Android Back follows visible phone layers

- 状态：active；类型：lifecycle；作用范围：Android 手机壳、手机页面及覆盖层；关联功能：`F-220`, `F-223`, `F-224`。
- 契约内容：系统返回先关闭当前可见的最上层详情或弹层；只有手机首页（收件箱、直接聊天列表或未绑定扫码页）返回时才允许 Android Activity 退出。Clients 任务详情、文件预览→文件列表→对话及普通会话遵守相同层级。
- 允许行为：详情关闭后留在原列表，再次从首页返回退出；禁止行为：详情可见时直接退出、迟到的任务读取结果在返回后重新打开详情；失败语义：返回动作不依赖网络请求成功；不变量：当前最上层优先消费返回；边界条件：异步任务读取中返回、多层覆盖、组件卸载。
- 证据：实现 `mobile/src/lib/android-back.ts`, `mobile/src/components/client-groups.tsx`, `mobile/src/components/file-browser.tsx`, `mobile/src/app.tsx`；测试 `mobile/src/lib/android-back.test.ts`, `mobile/src/app.test.tsx`, `mobile/src/components/client-groups.test.tsx`, `mobile/src/components/file-browser.test.tsx`, `mobile/e2e/walkthrough.spec.ts`；变更规则：新增手机覆盖层须同步测试系统返回和首页退出；来源：GitHub Issue #36、#48。

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

- 状态：active；类型：compatibility；作用范围：engine 普通对话和持续目标、共享 manager 提示词的终端入口；关联功能：`F-130`。
- 契约内容：运行时提示词必须要求 manager 在能节省时间或提高质量时主动使用 subagents，并随任务进展重新评估，无需等待用户提出；时间与质量是独立理由。只有一个 worker、需要等待或任务较小不得成为拒绝有质量收益的独立审查／第二思路的绝对条件。
- 允许行为：无实质收益时自行完成；禁止行为：仅显式要求才委派、为满足调用比例强制创建 worker、持续目标提示词重新引入单 worker 等待禁令；失败语义：worker 失败或超时需如实说明，不能视为成功；不变量：遵守用户显式限制与并发上限，分离写入路径，manager 核验并整合结果；边界条件：并发上限为一、先自行执行后发现委派机会、用户限制委派。
- 验证边界：该契约约束实际生成的模型指令，不保证任一真实模型的调用次数；离线 provider 的固定调用脚本只能验证委派链路，不能证明真实模型的调用比例提升。
- 证据：实现 `internal/engine/prompt.go`, `internal/engine/iterations.go`, `internal/tui/plan.go`；测试 `internal/engine/prompt_test.go`, `cmd/zwai/tui_prompt_test.go`, `internal/engine/engine_test.go`, `frontend/e2e/conversation.spec.ts`；变更规则：同步共享提示词、持续目标提示词、入口测试与使用说明；来源：用户 2026-09-28 对运行时主动使用 subagents 的要求。

## C-020 Phone workspace file reads

- 状态：active；类型：security / compatibility；作用范围：PC remote 文件 RPC、手机预览；关联功能：`F-224`。
- 契约内容：`files` 只列出指定对话与 PC 文件面板相同的工作区树，分帧分页；`file_chunk` 只读取该对话工作区内的普通文件，按字节偏移返回不超过 36 KiB 的 base64 分块及文件大小、MIME 和下一偏移。PC 必须拒绝路径穿越和通过符号链接逃出工作区。手机组装文件不超过 32 MiB，文本／图片／PDF 预览不超过 8 MiB；HTML 和 SVG 只作为源码文本显示。
- 允许行为：浏览目录、读取已上传或 agent 生成的文件、下载其他可读取文件；禁止行为：从手机直接调用 PC loopback API、在应用同源执行 agent 产出的 HTML/SVG、把超限文件截断后冒充完整内容；失败语义：缺失或不安全路径返回 RPC 错误，手机显示读取错误或过大提示；旧 PC 返回 `unknown_op` 时提示更新 PC；不变量：列表和内容均绑定同一对话 ID，单帧不超过 Pairlink 密文上限；边界条件：目录、空文件、分页、大文件、并发文件变化和符号链接。
- 证据：实现 `internal/remote/files.go`, `mobile/src/lib/remote-files.ts`, `mobile/src/components/file-browser.tsx`；测试 `internal/remote/files_test.go`, `mobile/src/lib/remote-files.test.ts`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/ios-wait-layout.swift`；变更规则：同步 RPC 文档、手机原生／浏览器走测和安全边界测试；来源：GitHub Issue #48。
