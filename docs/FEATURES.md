# Feature List

This is the current user-facing capability map. The code and tests named below are the authority for details; [README](../README.md), [API](API.md), [CLI](CLI.md), and [mobile README](../mobile/README.md) give usage.

- `F-000` zwai
  - `F-100` PC conversations
    - `F-110` Desktop, browser, and terminal shells
    - `F-120` Conversations and projects
    - `F-130` Agent runs, tools, and live control
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

- 目的：从原生窗口、网页或终端使用同一引擎；使用者：PC 用户；入口：`zwai desktop`, `zwai web`, `zwai tui`；输入：本机配置和对话；输出：同源的对话和状态；前置条件：本机引擎可启动；失败表现：启动或连接错误；关联契约：`C-001`, `C-005`；实现证据：`cmd/zwai`, `internal/app`, `internal/desktop`, `frontend/src`。

### F-120 Conversations and projects

- 目的：创建、组织、搜索和继续对话；使用者：PC 用户；入口：侧边栏、项目、`/api/threads`；输入：项目和消息；输出：可恢复的对话；前置条件：本机数据库可写；失败表现：错误提示或不存在的对话；关联契约：`C-001`, `C-008`；实现证据：`internal/engine`, `internal/store`, `frontend/src/store/app.ts`。

### F-130 Agent runs, tools, and live control

- 目的：运行 manager/sub-agent、查看工具并发送跟进或插入；使用者：PC 用户；入口：对话输入框和 Agent 面板；输入：消息、工具结果；输出：实时事件和最终回答；前置条件：配置可用 provider；失败表现：运行错误和 Trace；关联契约：`C-002`, `C-003`, `C-012`；实现证据：`internal/engine`, `internal/tools`, `frontend/src/components/app`。

### F-140 Files and attachments

- 目的：让对话使用工作区文件和附件；使用者：PC 用户；入口：文件面板、上传及下载 API；输入：文件或图片；输出：文件、预览或模型视觉输入；前置条件：工作区和权限可用；失败表现：上传或工具错误；关联契约：`C-004`, `C-005`；实现证据：`internal/server`, `internal/tools`, `frontend/src/components/app`。

### F-150 Goals and scheduled waits

- 目的：持续目标及未来时间继续执行；使用者：PC 用户；入口：`/goal`、Scheduled 页、`schedule_wake`；输入：目标和时间；输出：状态、唤醒、运行结果；前置条件：引擎运行；失败表现：跳过、失败或等待状态；关联契约：`C-002`, `C-003`, `C-009`；实现证据：`internal/engine`, `internal/store`, `frontend/src`。

### F-160 Trace and diagnostics

- 目的：用 turn id 还原一次运行；使用者：开发者和用户；入口：`zwai trace`, `/api/trace/:turn`, Trace 面板；输入：turn id；输出：事件、模型和工具记录；前置条件：该 turn 已记录；失败表现：找不到记录；关联契约：`C-012`；实现证据：`internal/server`, `internal/store`, `frontend/src/components/app`。

### F-170 Settings and providers

- 目的：配置模型、外观、工具和远程连接；使用者：PC 用户；入口：Settings；输入：配置字段；输出：持久化设置和可用模型；前置条件：本机数据目录；失败表现：校验或连接错误；关联契约：`C-006`, `C-009`；实现证据：`internal/config`, `internal/provider`, `frontend/src`。

### F-180 PC text quoting

- 目的：把选中的对话原文与提问分开提交；使用者：PC 用户；入口：选中 transcript 文本 → Add to chat；输入：选区和可选请求；输出：可编辑引用与带标签的消息；前置条件：选区在对话来源内；失败表现：无有效选区时不显示操作；关联契约：`C-011`；实现证据：`frontend/src/components/app/selection-menu.tsx`, `frontend/src/lib/quote.ts`, `frontend/src/components/app/selection-menu.test.tsx`。

### F-190 Desktop updates

- 目的：在 Mac 桌面应用里看到版本，并从 GitHub Release 升级到更新的 `zwai.app`；使用者：Mac 桌面用户；入口：侧边栏版本、菜单 Check for updates；输入：本仓库最新 Release 的 `zwai-<version>-darwin-<arch>.zip`；输出：替换当前应用并重新打开；前置条件：运行的是带数字版本号的 `zwai.app`；失败表现：菜单检查显示错误，自动检查保持安静；关联契约：`C-010`；实现证据：`internal/update`, `frontend/src/components/app/desktop-update.tsx`, `internal/desktop/pack`。

### F-210 Pair a phone with a PC

- 目的：安全绑定手机和 PC；使用者：手机用户；入口：Settings → Phone QR、扫码或粘贴；输入：pairlink URI；输出：加密连接和设备列表；前置条件：hub 和 PC 在线；失败表现：网络、过期或 host offline；关联契约：`C-006`；实现证据：`internal/remote`, `mobile/src/components/scan-screen.tsx`, `mobile/src/lib/link.ts`。

### F-220 Read and control PC conversations

- 目的：在手机查看同一对话并发送、跟进、插入及翻页；使用者：手机用户；入口：手机收件箱和对话页；输入：消息与操作，包括切换已绑定 PC、把队列首条等待消息中断插入当前轮次；输出：切换 PC 后停留在该 PC 收件箱，进入对话后看到 PC 上同一对话的事件；前置条件：已绑定且在线；失败表现：重连提示或 RPC 错误；关联契约：`C-002`, `C-003`, `C-006`, `C-015`, `C-016`；实现证据：`mobile/src/app.tsx`, `mobile/src/app.test.tsx`, `mobile/src/components/thread-screen.tsx`, `mobile/src/lib/phone-turn.ts`, `mobile/src/lib/phone-turn.test.ts`, `mobile/e2e/walkthrough.spec.ts`。

### F-221 Quote PC conversation text

- 目的：从手机对话选择原文并附在下一条消息；使用者：手机用户；入口：选中对话正文 → Add to chat／加入对话；输入：选区及可选请求；输出：可查看、编辑、移除引用，发送或插入后显示独立的引用块；前置条件：打开 PC 对话，选区在正文中；失败表现：无效选区不显示操作，发送失败恢复草稿；关联契约：`C-011`；实现证据：`mobile/src/components/thread-screen.tsx`, `mobile/src/components/composer.tsx`, `mobile/src/lib/quote.ts`, `mobile/e2e/walkthrough.spec.ts`。

### F-222 Inspect sub-agents on the phone

- 目的：在手机查看 PC 对话中的子 Agent 而不混入主 Agent 回答；使用者：已配对手机用户；入口：对话页 → 子 Agent／Agents → 子 Agent 行；输入：Pairlink 已允许的事件及更早历史页；输出：角色、ID、运行／完成／失败状态、活动摘要和独立的思考、工具、回答记录；前置条件：PC 端有可回放的子 Agent 事件；失败表现：离线时沿用重连提示，未加载到启动事件前角色暂以 ID 显示；关联契约：`C-003`, `C-006`；实现证据：`mobile/src/lib/transcript.ts`, `mobile/src/lib/session.ts`, `mobile/src/components/thread-screen.tsx`, `mobile/src/lib/transcript.test.ts`, `mobile/src/lib/session.test.ts`, `mobile/e2e/walkthrough.spec.ts`。

### F-223 Inspect local client tasks on the phone

- 目的：在已配对手机上只读查看 PC 的本地客户端任务；使用者：手机用户；入口：收件箱 → Clients → 工具分组和 More；输入：客户端任务页；输出：任务状态、记录及加载更多时的动效；前置条件：PC 已启用 Clients 且在线；失败表现：分页失败保留当前列表、显示错误并恢复 More；关联契约：`C-006`, `C-014`, `C-015`；实现证据：`mobile/src/lib/client-poll.ts`, `mobile/src/components/client-groups.tsx`, `mobile/src/app.test.tsx`, `mobile/e2e/walkthrough.spec.ts`。

### F-230 Direct model chat

- 目的：手机在没有 PC 时直接连接模型，并在应用内按服务商选择模型；使用者：手机用户；入口：Models、Chat 和输入框模型按钮；输入：兼容端点、所选模型、消息及附件；输出：流式回答；前置条件：端点可访问；失败表现：连接或模型错误；关联契约：`C-006`, `C-009`, `C-013`；实现证据：`mobile/src/components/direct-chat-screen.tsx`, `mobile/src/components/model-picker.tsx`, `mobile/src/components/composer.test.tsx`, `mobile/src/lib/openai-client.ts`。

### F-240 Phone updates

- 目的：发现并安装可用版本；使用者：手机用户；入口：菜单 Check for updates 和更新提示；输入：GitHub Release；输出：Android 安装请求或 iOS 发布页；前置条件：网络和新版本；失败表现：手动检查显示错误；关联契约：`C-010`；实现证据：`mobile/src/components/update-notice.tsx`, `mobile/src/lib/app-update.ts`。

### F-310 HTTP API and live events

- 目的：给 PC 客户端提供相同服务；使用者：内置客户端及本机调用方；入口：`/api`、SSE、WebSocket；输入：同源请求；输出：状态和事件；前置条件：引擎在线；失败表现：HTTP 状态和 code；关联契约：`C-002`, `C-003`, `C-005`；实现证据：`internal/server`, `docs/API.md`。

### F-320 CLI commands

- 目的：从 shell 启动、诊断和管理对话；使用者：终端用户；入口：`zwai` 子命令；输入：命令与参数；输出：终端文本或客户端；前置条件：可运行二进制；失败表现：命令错误；关联契约：`C-001`, `C-012`；实现证据：`cmd/zwai`, `docs/CLI.md`。

### F-330 Swarm library

- 目的：让 Go 程序嵌入 manager 与 sub-agent；使用者：Go 开发者；入口：模块公开 API；输入：registry、provider 和任务；输出：结果与通知；前置条件：依赖可编译；失败表现：Go 错误或结构化工具错误；关联契约：`C-003`；实现证据：`swarm.go`, `run.go`, `docs/LIBRARY.md`, `examples`。

### F-410 Local conversation storage and search

- 目的：持久保存对话、事件与检索索引；使用者：PC 用户；入口：对话列表和搜索；输入：事件与查询；输出：可恢复历史和结果；前置条件：数据目录可写；失败表现：存储或索引错误；关联契约：`C-003`, `C-008`；实现证据：`internal/store`, `internal/search`, `docs/DATA_MODEL.md`。

### F-420 Configuration and project memory

- 目的：保留本机设置、项目说明和技能；使用者：PC 用户；入口：Settings、Memory、技能管理；输入：配置及项目文件；输出：后续运行可用的上下文；前置条件：本机数据目录；失败表现：保存或解析错误；关联契约：`C-009`；实现证据：`internal/config`, `internal/memory`, `docs/CONFIG.md`。

### F-430 Build and release paths

- 目的：构建 PC 与手机产物，并用 `make release` 把 Mac zip 与 Android APK 发到同一个 GitHub Release；使用者：维护者；入口：Makefile；输入：源码、版本号与构建环境；输出：二进制、macOS `zwai.app` zip、Android APK/AAB，以及带这两个安装包的 Release；前置条件：Go、Node、对应 SDK、已登录的 gh；失败表现：构建、签名或上传错误，缺任一安装包则发布失败；关联契约：`C-010`；实现证据：`Makefile`, `internal/release`, `internal/desktop/pack`, `mobile/package.json`, `mobile/scripts/android-release.ts`, `mobile/README.md`。
