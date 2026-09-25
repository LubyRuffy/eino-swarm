# Current Contracts

Only current behavior is listed here. See [FEATURES.md](FEATURES.md) for user entry points and [CHANGELOG.md](../CHANGELOG.md) for history. Contract IDs are stable.

## C-001 One engine per data directory

- 状态：active；类型：lifecycle；作用范围：desktop、web、tui、engine；关联功能：`F-110`, `F-120`, `F-320`。
- 契约内容：同一数据目录只由一个 engine 进程持有；客户端连接同一 loopback 服务；不同版本替换引擎时先等待安全时机。
- 允许行为：多个 shell 同时连接；禁止行为：多个进程同时写同一数据库；失败语义：引擎不可达或租约冲突需明确报告；不变量：一个目录一把 lease；边界条件：进程异常结束后可恢复。
- 证据：实现 `internal/lease`, `internal/app`, `cmd/zwai`；测试 `internal/lease`, `internal/app`；变更规则：同步架构、CLI 和恢复测试；来源：`ARCHITECTURE.md` 和 Git 历史。

## C-002 One turn per conversation

- 状态：active；类型：consistency；作用范围：engine、server、desktop、phone；关联功能：`F-130`, `F-150`, `F-220`, `F-310`。
- 契约内容：同一对话同时只有一个活动 turn；第二条普通消息进入 follow-up，steer 指向活动 turn；手机在等待队列上执行“中断插入”时，先将队首消息转为本轮 steer，再请求中断当前 manager 步骤，其他消息继续排队；并发 API 冲突返回可识别的状态。
- 允许行为：排队和插入；禁止行为：在同一对话并行启动两个 turn；失败语义：`ErrBusy`/`ErrIdle` 对应 409 与 code；不变量：turn 状态可恢复；边界条件：中断和等待。
- 证据：实现 `internal/engine`, `internal/server`, `mobile/src/app.tsx`, `mobile/src/lib/phone-turn.ts`；测试 `internal/engine`, `frontend/src/store/app-steer.test.ts`, `mobile/src/lib/phone-turn.test.ts`, `mobile/e2e/walkthrough.spec.ts`；变更规则：同步 API、手机 RPC 和竞态测试；来源：`AGENTS.md`、GitHub Issue #31。

## C-003 Stored events and streamed order

- 状态：active；类型：data / consistency；作用范围：engine、store、SSE、phone；关联功能：`F-130`, `F-150`, `F-220`, `F-310`, `F-330`, `F-410`。
- 契约内容：完成事件按同一锁分配 seq、保存、广播；流式 delta 包含截至当前的完整文本，不逐 token 持久化；慢订阅者通过数据库追赶，事件 kind 是回放格式的一部分。
- 允许行为：delta 丢失后下一帧恢复；禁止行为：丢弃已存储事件或随意重命名 kind；失败语义：落后订阅者转追赶；不变量：存储顺序与流顺序一致；边界条件：重连和回放。
- 证据：实现 `internal/engine`, `internal/store`, `internal/remote`；测试 `internal/engine`, `internal/remote`；变更规则：同步 API、数据模型和 wire 测试；来源：`AGENTS.md`。

## C-004 Workspace is an anchor; HTTP paths are untrusted

- 状态：active；类型：security；作用范围：tools、server、files；关联功能：`F-140`。
- 契约内容：agent 工作区是定位锚点，不限制 agent 文件权限；HTTP 提供的路径不得穿越允许的文件入口。
- 允许行为：agent 在其被授权环境中访问路径；禁止行为：把 HTTP 路径穿越交给文件工具；失败语义：拒绝不安全 HTTP 路径；不变量：两种信任边界不混同；边界条件：符号链接和相对路径。
- 证据：实现 `internal/tools`, `internal/server`；测试 `internal/server`；变更规则：同步 API 和安全测试；来源：`AGENTS.md`。

## C-005 Loopback and same-origin HTTP

- 状态：active；类型：security；作用范围：PC 服务；关联功能：`F-110`, `F-140`, `F-310`。
- 契约内容：默认只在 loopback 提供服务，Web 客户端同源访问；不为持有对话和命令权限的服务添加开放 CORS。
- 允许行为：同源 shell 连接；禁止行为：无认证的非 loopback 默认服务；失败语义：跨源请求被拒绝；不变量：默认网络暴露为本机；边界条件：用户显式配置监听地址。
- 证据：实现 `internal/app`, `internal/server`；测试 `internal/server`；变更规则：同步配置、API 和安全测试；来源：`AGENTS.md`。

## C-006 Phone remote protocol

- 状态：active；类型：compatibility / security；作用范围：pairlink、phone、PC remote；关联功能：`F-170`, `F-210`, `F-220`, `F-222`, `F-230`。
- 契约内容：手机通过 pairlink 加密 RPC 与 PC 通信，不直接调用 PC 的 `/api`；相同事件 kind 和 seq 可在手机回放；断线重连不解除绑定。子 Agent 的事件按 `agent_id` 与主 Agent 分开回放，`spawned` 启动指令正文不离开 PC；手机只显示协议允许并按 `event_chars` 裁剪的活动。
- 允许行为：relay/direct 路径切换及手机查看子 Agent 角色、状态和截断记录；禁止行为：把本地 PC API 或子 Agent 启动指令直接暴露给手机，或把子 Agent 回答混作主 Agent 回答；失败语义：连接错误与 host offline 可区分；不变量：一个 PC 对话跨端一致，同一 agent 的流式文本只更新自己的记录；边界条件：ticket 过期、掉线、历史翻页、子 Agent 续办和重连。
- 证据：实现 `internal/remote/clip.go`, `mobile/src/lib/link.ts`, `mobile/src/lib/transcript.ts`, `mobile/src/lib/session.ts`, `mobile/src/components/thread-screen.tsx`；测试 `internal/remote/watch_test.go` 的 `TestSpawnedInstructionNeverLeavesTheHost`, `mobile/src/lib/transcript.test.ts`, `mobile/src/lib/session.test.ts`, `mobile/e2e/walkthrough.spec.ts`；变更规则：同步 API、手机测试和数据模型；来源：`ARCHITECTURE.md`、GitHub Issue #30 的 A 方案确认。

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
- 契约内容：端点、模型等配置有默认值且可在 Settings 编辑；`OPENAI_*` 只用于首次空字段填充；计划触发或取消需保留状态。
- 允许行为：用户更改设置；禁止行为：生产代码硬编码用户端点或模型；失败语义：配置或计划错误明确报告；不变量：用户设置优先于环境种子；边界条件：首次运行与重启。
- 证据：实现 `internal/config`, `internal/engine`, `internal/store`；测试 `internal/config`, `internal/engine`；变更规则：同步 CONFIG、API 和计划测试；来源：`AGENTS.md`。

## C-010 Build artifacts and update channel

- 状态：active；类型：compatibility / lifecycle；作用范围：build、mobile update、macOS desktop update；关联功能：`F-190`, `F-240`, `F-430`。
- 契约内容：`make build` 先更新嵌入的前端；Android sideload 使用 GitHub Release 的 `zwai-*-android.apk`；iOS 更新入口打开 Release 页；手机新版本从 package 版本派生 Android versionCode 和 Xcode marketing/build 设置。macOS 桌面发布物是 `zwai-<version>-darwin-<arch>.zip`，根目录为 `zwai.app`。`make release` 把该 zip 与 `zwai-<version>-android.apk` 发到同一个 tag；缺任一文件则发布失败。桌面应用只从本仓库最新的非 draft、非 prerelease Release 安装同架构 zip，并只跟随 github.com 与 GitHub 的 release 资源主机。
- 允许行为：不同渠道单独验收；禁止行为：把本地 APK/AAB 或未上传的 zip 称为远端发布，或从其他仓库安装；失败语义：构建、签名、下载错误可观察；不变量：用户可获取与显示版本一致的产物；边界条件：安装签名、新旧版本按数字比较、未打版本号的构建不提供升级。
- 证据：实现 `Makefile`, `internal/release`, `internal/desktop/pack`, `internal/update`, `mobile/scripts/android-release.ts`, `mobile/scripts/sync-ios-version.ts`, `mobile/src/lib/app-update.ts`；测试 `internal/release`, `internal/update`, `internal/desktop/pack`, `mobile/scripts/android-release.test.ts`, `mobile/scripts/sync-ios-version.test.ts`, `mobile/src/lib/app-update.test.ts`；变更规则：同步 mobile README、CLI、发布门禁和版本测试；来源：`mobile/README.md`, `docs/CLI.md`。

## C-011 Quoted conversation payload

- 状态：active；类型：data / compatibility；作用范围：desktop、phone、message wire；关联功能：`F-180`, `F-221`。
- 契约内容：只选对话正文可加入草稿；引用可查看、编辑和移除；每段原文单独以 `<selected_text>` 包裹，输入的请求以 `<user_request>` 包裹；引用可单独发送，发送失败恢复引用和正文；显示用户消息时不把标签当正文。
- 允许行为：多段引用、空正文发送；禁止行为：把输入框或 UI 文本当作引用，或让正文被误解为引用；失败语义：无效选区不提供操作；不变量：PC/手机同一 wire 格式；边界条件：空白选区、换行和文本中有关闭标签。
- 证据：实现 `frontend/src/lib/quote.ts`, `mobile/src/lib/quote.ts`, `mobile/src/components/thread-screen.tsx`, `mobile/src/components/composer.tsx`；测试 `frontend/src/lib/quote.test.ts`, `mobile/src/lib/quote.test.ts`, `mobile/src/components/thread-screen.test.tsx`, `mobile/e2e/walkthrough.spec.ts`；变更规则：同步双端解析、发送测试和用户说明；来源：`README.md`、GitHub Issue #29。

## C-012 One-ID troubleshooting

- 状态：active；类型：compatibility；作用范围：engine、server、CLI、Trace；关联功能：`F-130`, `F-160`, `F-320`。
- 契约内容：一个 turn id 可在 CLI、API 和 Trace 面板还原输入、事件、模型与工具调用、错误及结果；新增事件类型需进入这一路径。
- 允许行为：敏感值脱敏；禁止行为：丢掉诊断所需事件或输出令牌/完整 Cookie；失败语义：不存在的 id 报不存在；不变量：同 id 贯穿运行；边界条件：部分失败和重启恢复。
- 证据：实现 `internal/engine`, `internal/server`, `internal/store`；测试 `internal/engine`, `internal/server`；变更规则：同步 CLI、API 和 Trace 测试；来源：`AGENTS.md`。
