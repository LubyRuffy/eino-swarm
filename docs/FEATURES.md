# Feature List

This is the current user-facing capability map. The code and tests named below are the authority for details. [README](../README.md) is the entry, [Using zwai](USAGE.md) is the day-to-day guide, and [API](API.md), [CLI](CLI.md), and [mobile README](../mobile/README.md) are the other usage surfaces.

- `F-000` zwai
  - `F-100` PC conversations
    - `F-110` Desktop, browser, and terminal shells
    - `F-115` Bounded native renderer recovery
    - `F-120` Conversations and projects
    - `F-125` Unread completion attention
    - `F-130` Agent runs, tools, and live control
    - `F-135` Independent task coordination
    - `F-136` Nuwa project collaboration
    - `F-137` HTML/JS task panels
    - `F-138` Project Markdown knowledge base
    - `F-139` Conversation work records
    - `F-140` Files and attachments
    - `F-145` Durable composer drafts
    - `F-146` Next-step composer suggestions
    - `F-147` Temporary task side chat
    - `F-148` Voice input and spoken answers
    - `F-150` Goals and scheduled waits
    - `F-160` Trace and diagnostics
    - `F-170` Settings and providers
    - `F-171` Official server first: sign-in, welcome and account models
    - `F-172` Official model profile
    - `F-173` FOFA install and local model controls
    - `F-175` Provider cloud built-in tools
    - `F-176` External MCP tools
    - `F-180` PC text quoting
    - `F-181` Selection reading and translation
    - `F-182` Generate images in chat
    - `F-190` Desktop updates
  - `F-200` Phone
    - `F-210` Pair a phone with a PC
    - `F-211` Mobile account login and owned devices
    - `F-220` Read and control PC conversations
    - `F-221` Quote PC conversation text
    - `F-222` Inspect sub-agents on the phone
    - `F-225` Manage PC projects from the phone
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
    - `F-421` External Agent Skills packages
    - `F-422` Local backup and restore
    - `F-430` Build and release paths

### F-148 Voice input and spoken answers

- 手机在输入框内显示录音计时和音量，使用图标取消或确认。识别期间恢复可编辑草稿，状态和重试仍在输入框内。证据：`mobile/src/components/speech-flow.test.tsx`、`mobile/e2e/speech.spec.ts`。

- Without a version-1 gateway audio declaration, the phone account’s automatic Gateway output uses streaming PCM/SSE so playback can begin before the segment finishes. Automatic segments are bounded at 200 characters; explicit PC or personal audio settings keep their chosen format and limits. Evidence: `mobile/src/lib/gateway-speech.test.ts`, `mobile/src/lib/phone-gateway-voice.test.tsx`.

- PC Settings → Models (or Speech for a fixed account) and the phone User Center → Speech render only editable gateway audio choices from the selected model's versioned private declaration. Fixed values and gateway limits remain effective. PC recording format, timeouts and client limits live under collapsed Advanced settings. Audio parameter permissions remain independent of model-selection permissions. Unchanged options follow gateway defaults without becoming request overrides; scoped explicit preferences survive contract changes and show validation errors when no longer allowed. An editable voice, format, speed, or instruction returns to the gateway default from that same control, without a second reset row. A saved value that no longer has a control, including an unavailable declaration, keeps one reset action. Personal LOCAL speech retains its existing settings. Gateway declaration failures/unknown versions disable dynamic controls and preserve preferences. Evidence: `internal/remote/account_audio_parameters_test.go`, `internal/speech/audio_parameters_test.go`, `frontend/src/components/app/settings-audio-parameters.test.tsx`, `frontend/src/components/shared/audio-parameters.test.tsx`, `mobile/src/lib/gateway-audio-parameters.test.ts`; contract `C-048`.

- LOCAL voice source `account` uses the signed-in account's enabled recognition or speech role for preview and playback when no functional binding overrides it. A personal binding, provider, or custom endpoint stays on that endpoint. No login or a disabled role stays not ready and is not replaced by a conversation model. Evidence: `internal/speech/failures_test.go`, `internal/provider/account_audio_test.go`.

- Phone voice errors identify the service's HTTP status without exposing its raw error body; transport and microphone permission failures remain distinct. Evidence: `mobile/src/lib/speech-transport.test.ts`, `mobile/src/components/speech-errors.test.ts`.

- Dictation inserts transcript prose without a leading ASR language/control envelope. Plain text and literal markers elsewhere are preserved; metadata-only responses show empty recognition. Evidence: `frontend/src/lib/transcription-text.test.ts`, `mobile/src/lib/phone-transcription-text.test.ts`.

- PC and Web voice input can select a microphone. Visiting Settings only lists inputs the browser exposes. Opening the microphone picker can request access to hidden device names, and recording requests access when needed. If the preferred device is missing, Settings shows System default and new recordings fall back to it. The saved preference lets the device be used again after reconnection. Phones use their own microphone. Evidence: `frontend/src/lib/speech-media.test.ts`, `frontend/src/components/app/settings-speech.test.tsx`, `frontend/e2e/speech.spec.ts`, `mobile/src/lib/speech-cache.test.ts`.
- 目的：在桌面、Web、iOS 和 Android 听写可编辑草稿并朗读完成回复。入口：PC Settings → Models、Send 左侧麦克风及 Copy 旁 Read aloud；输入：录音或完成回复正文；输出：草稿文本或音频；前置条件：对应开关开启且配置就绪；失败表现：保留草稿、显示可重试错误。PC/Web Gateway input and output default on independently for a resolved account model and remember manual opt-out. LOCAL and personal providers use their own switches, off by default. Evidence: `internal/speech/input_route_test.go`, `internal/server/settings_managed_speech_test.go`, `frontend/e2e/gateway-speech-settings.spec.ts`. Dictation never sends automatically or overwrites text edited while recognition was pending. Read aloud skips fenced code and diagrams and sequences long prose in order. The PC selection menu uses that same player for settled prose when spoken output is enabled and ready (`F-181`).
- Phone recognition and playback automatically use the account’s authorized Gateway defaults, independently of PC voice configuration. Personal PC overrides require that PC’s live link; LOCAL phone personal overrides remain independent. Only the phone’s own Mobile login authorizes Gateway audio. A PC metadata refresh or disconnection does not retire unchanged phone-owned speech. Account conversations still require a PC. Model discovery is typed, source-specific and credential-scoped. Evidence: `mobile/src/lib/phone-gateway-voice.test.tsx`, `mobile/src/lib/gateway-speech.test.ts`.
- Audio format **PCM (streaming playback)** starts PC/Web and direct phone playback as synthesis arrives. Raw PCM and OpenAI speech SSE share the cancelable player; MP3/WAV and PC-local encrypted RPC retain buffered playback.
- Recognition and playback independently use direct phone-to-Gateway requests when their resolved models are the Gateway defaults. Personal PC overrides retain PC audio RPC; matching names or URLs do not change source identity. Updated PCs publish explicit routes for both directions. Evidence: `internal/speech/input_route_test.go`, `mobile/src/lib/phone-speech.test.ts`.
- Automatic capture normalizes MP4/AAC, WebM/Opus and Ogg recordings to WAV for ASR compatibility, including newer iPhones that record WebM. Paired-PC dictation and complete-answer playback survive unrelated phone model-catalog refreshes. Evidence: `frontend/src/lib/speech-media.test.ts`, `mobile/e2e/speech-mp4.spec.ts`, `mobile/src/lib/phone-speech.test.ts`.
- `models/mobile/SpeechCapture.tla` and `models/mobile/SpeechPlayback.tla` protect these two protocols through `make model-check`, including known-fault reproduction, guard-removal and reachability cases plus eventual settlement under fair progress. Source behavior is checked by `frontend/src/lib/speech-capture-boundaries.test.ts` and `mobile/src/lib/phone-speech.test.ts`; physical audio acceptance remains separate.
- 关联契约：`C-048`。实现证据：`internal/speech`, `internal/server/speech.go`, `internal/remote/speech.go`, `frontend/src/components/app/settings-speech.tsx`, `mobile/src/lib/phone-speech.ts`。验证证据：`internal/speech/service_test.go`, `internal/server/speech_http_test.go`, `internal/remote/speech_rpc_test.go`, `frontend/src/lib/speech-media.test.ts`, `mobile/src/lib/speech-cache.test.ts`, `mobile/src/lib/speech-transport.test.ts`. Native recording and playback acceptance is distinct from these headless checks.

### F-110 Desktop, browser, and terminal shells

- 集成终端随应用的深浅色和配色方案实时更新，包括隐藏标签页；保留已有输出与 PTY 连接。外观选择 System 时运行中跟随系统，显式 Light／Dark 保持固定。实现／验证：`frontend/src/components/app/terminal-session.tsx`、`frontend/src/lib/system-theme.ts`、`frontend/src/components/app/terminal-session.test.tsx`、`frontend/src/lib/system-theme.test.tsx`、`frontend/e2e/terminal-theme.spec.ts`。

- PC 恢复交互：恢复时显式禁用目标编辑／清除／开始，包括已聚焦编辑框的键盘和失焦保存；目标编辑只在成功 ACK 后关闭，拒绝保留本会话输入，就绪不自动重发，导航后旧 ACK 不影响新会话。页面 reload 按现有首页契约重选原对话后保留其已存草稿并继续显示恢复进度。关联契约：`C-001`；证据：`frontend/e2e/engine-startup.spec.ts`、`frontend/e2e/goal-restoring.spec.ts`、`frontend/src/components/app/goal-banner-restoring.test.tsx`、`frontend/src/components/app/composer-goal-restoring.test.tsx`。

- 启动恢复：数据库和配置可用即发布可连接的 `restoring` 引擎，PC／手机显示恢复进度，成功后自动移除；running／parked 部分失败统一显示数量并保留原 turn 失败诊断，其余工作继续可用；创建日程只有成功 ACK 后关闭，恢复拒绝保留输入；历史、Trace、事件流、草稿、在线登记和 Stop 可用，新工作及日程变更等待 `ready`。ACK 后的替换进程不会被原交接截止时间杀掉；冷等待到期仍活着的本次 child 即使尚未 Acquire 也保留，并以 PID／日志和归属未确认错误报告，不能把竞争者的锁当成自己的；人工取消只回收本次子进程。每对话共用一次恢复快照，包括 manager 纠正预算及已经验证的原人类提问 call 身份；提问恢复等待／Stop 不重读完整 turn 检查点；保留预算消耗与人工续行回执，新的人类 Continue 读取新回执。原 turn Trace 包含 `engine_restore` 耗时与扫描量。关联契约：`C-001`、`C-027`、`C-012`；实现／测试：`internal/app/startup.go`、`internal/engine/restore.go`、`internal/server/startup_test.go`、`internal/remote/startup_test.go`、`cmd/zwai/engine_restore_process_test.go`。


- 目的：从原生窗口、网页或终端使用同一引擎；使用者：PC 用户；入口：`zwai desktop`, `zwai web`, `zwai tui`；输入：本机配置和对话；输出：同源的对话和状态；前置条件：本机引擎可启动；失败表现：启动或连接错误；关联契约：`C-001`, `C-005`, `C-027`；实现证据：`cmd/zwai`, `internal/app`, `internal/desktop`, `frontend/src`。

- 冷启动就绪：完成当前 checkout 的前端准备后，允许本地初始化最多 2 分钟，每 10 秒报告启动进度；手机 hub 注册不阻塞本地窗口。探测期间取消不得因迟到响应而继续附着或丢失取消类别，失败保留本次子进程退出原因及本次追加日志，取消／普通冷启动超时仅清理自己的启动子进程；不可回退交接后的冷等待截止遵守上述保留规则，保留竞争引擎。替换已有引擎仍遵守准备后的统一 30 秒交接预算。关联契约：`C-001`、`C-006`、`C-027`；实现／验证证据：`cmd/zwai/engine_boot.go`、`cmd/zwai/engine_boot_test.go`、`internal/app/app.go`、`internal/app/startup_remote_test.go`。
- 前端准备在引擎发现／交接前执行，准备失败或取消时保留仍在服务的旧引擎，不消耗其 30 秒交接／新监听预算；后续进程仍须完成自身启动校验。关联契约：`C-001`、`C-027`；证据：`cmd/zwai/engine.go`、`cmd/zwai/engine_frontend_test.go`。
- checkout 前端需要重建时，检查 manifest 直接依赖、开发依赖及命令入口；空／部分 `node_modules` 即使时间戳较新也先用 `npm install --include=dev` 修复并复验。安装／构建／stamp 按 checkout 跨进程串行，等待可取消；已有新鲜 bundle／发布 embed 不新增 Node 要求。关联契约：`C-001`、`C-010`；证据：`frontend/ensure.go`、`frontend/ensure_dependencies_test.go`、`frontend/ensure_build_lock.go`、`frontend/ensure_build_lock_test.go`。
- 自有启动子进程发布租约后，单次元数据探测暂时不可达时继续在原总预算内等待；元数据成功校验后才附着。竞争引擎不可达或模式冲突仍立即报错并保留其进程。关联契约：`C-001`；证据：`internal/lease/lease_test.go`、`cmd/zwai/engine_boot_test.go`。

- 自动交接重启：新版本／不同二进制启动时，在前端准备后的统一 30 秒预算内请求旧引擎交接、退出并启动新引擎；无需输入确认。manager 及所有活跃／后台 worker 保存检查点，模型摘要尽力完成，不以摘要标记任务完成；原 turn／worker 身份、历史、未读消息、超时预算及人类暂停保留。存储失败不确认安全退出，交接后保留冻结及检查点供修复后重试，存储可读时仍可查看历史／Trace／failed 状态，不自动重跑未知操作；旧引擎缺少交接 API 时采用有界兼容恢复。关联契约：`C-001`、`C-027`、`C-012`；证据：`cmd/zwai/engine_restart.go`、`internal/engine/restart.go`、`handoff.go`、`cmd/zwai/engine_restart_acceptance_test.go`、`internal/server/restart_failure_retry_test.go`、`frontend/src/lib/transcript-restart.test.ts`。

- 无浏览器 Web：`--no-open` 或配置关闭浏览器时，waiter 在 ready 前持有真实 `web` presence，无标签页也保持服务；断流仅对原地址有界恢复，耗尽返回错误，信号退出释放并等待连接结束，不取消其他客户端或任务。契约：`C-001`；证据：`cmd/zwai/web_presence_test.go`、`cmd/zwai/cli_test.go`。
- 桌面重开：native 进程在窗口加载前持有真实 presence，页面 reload 不释放连接，也不重复登记窗口；撞上 idle 停机时有界等待原持有人退出或租约身份改变，再完整核验后重新连接；新 PID 复用原 URL 仍识别为替换，不生成第二个数据库写入者；最终 idle 复核同时保留活动 manager、后台 worker 和交接中的执行归属。证据：`cmd/zwai/desktop_attach_test.go`、`cmd/zwai/desktop_attach_errors_test.go`、`cmd/zwai/desktop_attach_identity_test.go`、`frontend/e2e/presence.spec.ts`；契约：`C-001`。
- 桌面连接恢复：已确认的 native presence 断流后，在同一地址有界重新登记并确认；短流不无限重置预算，稳定连接后可恢复后续独立断流。关闭应用中断恢复，耗尽则先通过桌面 logger 同步记录原因再请求原生退出；原生系统终止不保证返回 Go，能返回的平台保留原有错误类别，启动前失败直接返回且不开窗口，正常关闭不误报终止错误。证据：`cmd/zwai/desktop_presence.go`、`cmd/zwai/desktop_attach_stream_recovery_test.go`、`cmd/zwai/desktop_presence_test.go`、`internal/desktop/lifecycle_test.go`、`internal/desktop/lifecycle_terminal_test.go`；契约：`C-001`。

### F-115 Bounded native renderer recovery

- 故障诊断：重载／替换前向原异步诊断通道发出最后探针边界和已知年龄，区分回调缺失、位置待恢复、截图待回、截图错误、像素不匹配，以及窗口正文呈现为纯色。继任等待首次绘制时向同一 journal 提前报告自己的探针边界，便于在父进程截止前定位停在哪一步。继任错误保留启动前／attach／ready 后提交前阶段和安全 lease HTTP 分类，不记录原生错误正文、URL 或响应内容。该分类不等于已定位 GPU／JS 根因。证据：`internal/desktop/recovery_diagnostic_runtime_test.go`、`internal/lease/discover_diagnostics_test.go`、`cmd/zwai/desktop_recovery_phase_diagnostics_test.go`；契约：`C-034`、`C-001`、`C-012`。
- 目的：在 macOS WebContent 活挂、绘图失效或白屏时恢复可用窗口；使用者：桌面用户；入口：`zwai desktop` 的原生恢复控制循环；输入：同源、主 frame、当前 challenge 响应与实际截图像素；输出：一次重载，必要时附着原引擎的继任窗口；前置条件：引擎租约可验证、恢复副本与诊断目录可写；失败表现：有界尝试耗尽后的原生错误提示及保留诊断；关联契约：`C-034`, `C-001`, `C-012`, `C-033`；实现证据：`internal/desktop/recovery.go`, `internal/desktop/recovery_state.go`, `internal/desktop/recovery_snapshot_darwin.m`, `cmd/zwai/desktop_recovery_darwin.go`, `internal/desktop/recovery_test.go`, `cmd/zwai/desktop_recovery_test.go`。
- 健康条件包含本次随机探针的真实像素；HTTP、JS 响应或动画帧不单独算绘制成功。窗口可见时，正文呈现为纯色也记为失败并沿用同一次重载与继任；截取失败、窗口不可见或系统拒绝时不把未知画面当成白屏。该判定和前台预算由 `models/desktop/RendererRecovery.tla` 检查。失焦、最小化、睡眠与唤醒有检测暂停／宽限。
- 继任失败后若还有预算，原窗口在重试延迟后重新探测。只有新的前台绘制探针失败才允许再次替换；该探针的 20 秒健康期从实际派发计时，不被已排队 tick 提前耗尽。最后一次失败耗尽预算，只提示一次。回归见 `internal/desktop/recovery_test.go`、`internal/desktop/recovery_runtime_test.go`、`internal/desktop/recovery_dispatch_clock_test.go`。
- 焦点诊断：父进程的协调入口保留有界 guard 原因、已采样父子身份／激活策略与请求结果，未读取谓词保持 unknown。日志不证明系统已激活窗口，不新增恢复 journal 阶段或延长截止；证据：`internal/desktop/recovery_focus.go`、`internal/desktop/recovery_focus_darwin.m`、`internal/desktop/recovery_focus_test.go`、`tools/test_native_macos_recovery_focus.py`；契约：`C-034`、`C-012`。
- 探针不出现在用户画面：首次绘制前在 WebView 同级安装不接收输入、对辅助功能隐藏的原生不透明遮挡，跟随页面左上角背景；WebView 自身截图仍校验完整随机像素。主题、配色和弹窗变化同步底色；已有遮挡时短暂采色失败不停止健康检测。证据：`internal/desktop/recovery_cover.go`、`internal/desktop/recovery_cover_test.go`、`internal/desktop/recovery_snapshot_darwin.m`、`tools/test_native_macos_render_cover.py`、`frontend/e2e/render-probe-cover.spec.ts`；契约：`C-034`。
- 继任窗口经私有继承管道与两阶段确认接管，沿用数据目录、离线模式和原引擎 PID／URL／租约，恢复会话／项目与草稿位置；不调用引擎替换或重新执行任务。发送私有交接后，父进程在原截止内观察本次子进程的原生登记，核对 kernel birth／可执行文件身份，且仍持前台、主窗口未隐藏／最小化、无独立系统模态窗口且未取消时，协调一次子窗口激活；用户切走时不抢焦点。macOS 14 起先让渡激活许可再请求协作激活；旧系统由父进程请求目标激活。观察在真实像素就绪、取消或子进程退出时结束并回收，不阻塞真实就绪回执。请求本身不确认健康，仍需当前 nonce 像素与交接回执；契约：`C-034`；证据：`internal/desktop/recovery_focus.go`、`internal/desktop/recovery_focus_darwin.m`、`internal/desktop/recovery_focus_test.go`、`cmd/zwai/desktop_recovery_focus_test.go`、`tools/test_native_macos_recovery_focus.py`。
- 会话／回合／项目／草稿 key 变化时，前端先发布当前归属，再同步发出不含正文的内部 `zwai:recovery-location` 通知；原生桥独立保存位置，不等待五秒绘制探针。一次 Reload 在原 URL 更新恢复作用域，保留 PID／presence 等字段；初始化 Home 不覆盖尚未匹配的恢复目标，匹配后仍需新挑战的真实像素。位置通知不确认健康或延长任何恢复总期限。证据：`frontend/src/lib/composer-recovery.ts`、`frontend/src/lib/composer-recovery.test.ts`、`frontend/e2e/desktop-recovery.spec.ts`、`internal/desktop/recovery_location.go`、`internal/desktop/recovery_location_test.go`、`internal/desktop/recovery_location_runtime_test.go`、`internal/desktop/recovery_location_script_test.go`；Fixed `C-034`，Issue #110。
- 原生验收在草稿检查退出时保留已有 API／原生观察的摘要及逐项判定，不复制正文、不追加界面操作或放宽原断言；先前失败原因未捕获时仍标为未知。证据：`tools/native_macos_resilience.py`, `tools/test_native_macos_resilience.py`, `docs/TESTING.md`。
- 原生 QA 的 PNG 输入使用一次同进程、前台、唯一启用的 Edit→Paste 菜单动作，先校验剪贴板写入与原 bytes／计数，并保留传输回执；菜单动作回执不替代完整草稿保存与原生 ACK。头less 测试只运行真实 helper 的纯校验，界面副作用仅在隔离来宾验收。证据：`tools/native_macos_resilience_ax.m`, `tools/test_native_macos_paste.py`, `docs/TESTING.md`。

### F-120 Conversations and projects

- PC 历史上滚：用户和开发模式均按上一条用户输入或主管正文翻页。较早思考和工具默认显示摘要，点击展开后按范围读取；最新消息的明细随首屏加载。首次打开即显示会话的子 Agent 名单，选择某个子 Agent 时读取独立日志。关联契约 `C-003`；实现／验证：`internal/server/log_history.go`、`frontend/src/lib/use-history-window.ts`、`frontend/e2e/message-history.spec.ts`。

- 新任务的工作目录在创建时固定；项目选择仍决定共享资料和记忆，移动任务或编辑项目目录不会搬迁任务文件。关联 `C-052`；实现 `internal/engine/task_workspace.go`，验证 `internal/engine/task_workspace_test.go`。

- 失败重试：桌面和 Web 的最新人类失败轮显示「重试」。点击即确认，从该轮失败时已存储的上下文继续，保留用户消息、图片和已完成的工具结果，不回退后续进度，也不打开编辑框。目标续办、定时检查、人工停止和没有可核对用户输入的失败不提供入口。设置打开、回合运行、引擎恢复或本次提交进行时禁用。关联契约 `C-003`。实现 `frontend/src/components/app/transcript-error.tsx`、`frontend/src/components/app/transcript.tsx`、`frontend/src/lib/use-retry-input.ts`、`internal/engine/retry_failed.go`。验证 `frontend/e2e/transcript-error-retry.spec.ts`、`internal/engine/retry_failed_http_test.go`。

- 在项目会话中，可授权 manager 更新项目全局描述。成功回执带回已保存的描述，本回合没有单独的读取工具；后续回合使用新描述，Memory 关闭也可用。并发修改、对话移出项目或写入失败时返回拒绝回执。PC 项目编辑打开时读取当前描述，保存其他设置时省略未改动的描述。契约 `C-038`。实现 `internal/engine/project_description.go`、`internal/store/projects.go`、`frontend/src/components/app/project-dialog.tsx`。验证 `internal/engine/project_description_http_test.go`、`internal/engine/project_description_test.go`、`frontend/e2e/projects.spec.ts`。

- PC 轮次导航：回到底部恢复实时跟随时，撤销旧轮次定位；后续消息、历史分页或已排队的定位帧不得再次拉回旧轮次。点击「跳到最新」、手动滚到底部和发送新消息共用该行为。尚在查看旧轮次时，分页后继续保持所选轮次位置。实现／验证：`frontend/src/lib/use-turn-jump.ts`、`frontend/src/lib/use-turn-jump.test.tsx`、`frontend/src/components/app/transcript-jump-latest.test.tsx`、`frontend/e2e/transcript-jump-latest.spec.ts`。
- Mermaid 图：桌面与 Web 的对话、工具输出和 Markdown 文件预览识别 `mermaid` 代码围栏，按需加载随应用打包的渲染器，跟随明暗主题并保留原文复制。未闭合的流式围栏、无效语法及依赖外部图片／CSS `url()`／`@import` 的图保留可读代码；标识符末尾的 url 不是资源请求。图表作为 SVG 图片显示，默认适应阅读列和有限高度以便看全图；可放大、缩小、回到测量尺寸或重新适应，放大后只在图内滚动。viewBox 边长、面积、元素数、文本或字节超出绘制预算时同样保留源码，避免页面主线程被一帧 SVG 绘制占住。手机配对对话及直连 Chat 复用同一严格渲染、预算、适应和缩放规则，使用触控控件并保留可复制源码。关联契约：`C-005`；证据：`frontend/src/components/app/markdown.tsx`、`frontend/src/components/app/markdown-mermaid.tsx`、`frontend/src/lib/markdown-mermaid.ts`、`frontend/e2e/mermaid.spec.ts`、`mobile/src/components/phone-mermaid.tsx`、`mobile/e2e/markdown-mermaid.spec.ts`、`models/desktop/DiagramPaint.tla`。
- Markdown 表格：桌面、Web 与手机按内容自动分配列宽，各列保留随字体变化的最小可读宽度，避免短标签被长说明挤成逐字竖排；长内容可换行，过多列仅在表格内横向滚动，保留 Markdown 列对齐。文件预览复用相同规则。证据：`frontend/src/index.css`、`mobile/src/index.css`、`frontend/e2e/file-preview.spec.ts`、`mobile/e2e/markdown-table.spec.ts`。
- Markdown 代码围栏：桌面与 Web 的对话、工具输出和 Markdown 文件预览使用 Code Block。已知语言按需加载 Shiki 语法色并跟随明暗主题，其中包括汇编（`asm`，以及 `assembly`、`nasm`、`gas`、`x86asm`、`yasm`）；空标签与未知标签保持纯文本。公式、图表和 Mermaid 仍用各自的渲染。文件列表、差异和命令行继续使用原有词法色。复制和选区保留围栏原文。证据：`frontend/src/components/ui/code-block.tsx`、`frontend/src/components/app/markdown-code.tsx`、`frontend/src/lib/highlighter.tsx`、`frontend/src/components/app/markdown.test.tsx`、`frontend/e2e/markdown.spec.ts`。
- 目的：创建、组织、搜索和继续对话；使用者：PC 用户；入口：侧边栏、项目、`/api/threads`；输入：项目和消息；输出：可恢复的对话；前置条件：本机数据库可写；失败表现：错误提示或不存在的对话；关联契约：`C-001`, `C-003`, `C-008`, `C-026`；实现证据：`internal/engine`, `internal/store`, `frontend/src/store/app.ts`。
- 长会话打开：PC 并行读取尾页与详情，尾页到达即移除骨架并显示历史；上下文字符用量、运行态、轮次和跟进分别合并，迟到响应不得覆盖切换、倒带或实时推进后的稿面。仅工具活动的尾页仍补读到可见历史。打开时的 roster 保留视口外的工人身份、失败原因，以及 cleanup、workers_retained、error 正文；spawned 与 finished 的长正文留在该工人自己的日志里，打开该工人时补回。轮次列表保留人类输入、状态和耗时，不带终稿，也不带目标续办或定时检查的协议提示。Trace 与手机预览仍读取存储的正文。`context_chars` 与下一回合共用历史回放：保留压缩水位后的 user／assistant 正文、排除撤回 steer、补齐未落库主管回答；补答只查水位之后仍有消息的回合，不按 kind 扫描全部历史主管消息。关联契约：`C-003`、`C-008`；实现／验证证据：`frontend/src/store/app-open.ts`、`frontend/src/store/app-open.test.ts`、`frontend/src/store/thread-history.ts`、`internal/engine/replay_history.go`、`internal/engine/replay_history_test.go`、`internal/engine/compact.go`、`internal/store/replay.go`、`internal/store/replay_test.go`、`internal/store/open_list.go`、`internal/store/open_list_test.go`、`internal/server/log.go`、`internal/server/threads.go`、`internal/server/log_test.go`、`internal/server/open_list_http_test.go`。
- PC 历史呈现：可见内容按事件原顺序显示，不为聚合同一 turn 而重排。每个已结束且非 quiet 的 turn 仅在最后一片终态前可见内容后显示一次耗时和真实完成时间；隐藏标题／会话摘要不产生空片或结束栏。迟到复盘提示保留在实际到达位置，独立于已结束持续目标的过程详情，不打断当前回合的实时活动提示；原回答与工具详情仍可查看，数据库和 Trace 记录不变。关联契约：`C-003`；实现／验证证据：`frontend/src/lib/turn-boundary.ts`, `frontend/src/lib/turn-boundary.test.ts`, `frontend/src/components/app/transcript-boundary.test.tsx`, `frontend/e2e/late-review-footer.spec.ts`。
- 项目工作目录：原生桌面新建／编辑项目时可用「选择目录」打开系统目录窗口，将单个现存目录的绝对路径填入字段；取消保留原值，选择失败在字段下显示错误。手动输入与留空使用 zwai 管理目录仍可用；浏览器提示桌面支持选择目录，不提供无法返回本机绝对路径的目录上传。保存仍经现有工作目录校验。实现／验证证据：`frontend/src/components/app/project-dialog.tsx`, `frontend/src/components/app/workdir-input.tsx`, `frontend/src/lib/workdir.ts`, `internal/desktop/directory.go`, `internal/desktop/directory_test.go`, `internal/engine/project_test.go`, `frontend/src/components/app/project-dialog.test.tsx`, `frontend/src/components/app/workdir-input.test.tsx`, `frontend/src/lib/workdir.test.ts`, `frontend/e2e/workdir.spec.ts`。
- 目录身份：非空目录路径不裁剪首尾空白；路径标准化仍使用 filepath.Clean。全空白保留托管目录行为。保存、编辑和实际运行解析必须指向同一所选目录；路径失效时拒绝并保留草稿，不回退到去空白后的同名目录。兼容变化：手动输入非空路径也按实际路径验证，不再猜测修正外围空白；已保存的规范路径无需迁移。证据：`internal/engine/project_workdir_identity_test.go`。

### F-125 Unread completion attention

- 手机当前状态：同一对话 ID 正在运行、等待或待答时，顶部完成未读列表暂不显示其旧结果；未读水位与已读回执保持不变，空闲后仍可显示。同名不同 ID 独立保留；搜索使用未过滤的 live 集合判定，非搜索视图中全部已加载结果暂隐藏时仍可继续 More。实现／验证：`mobile/src/components/home-screen.tsx`、`mobile/src/components/home-screen-unread.test.tsx`、`mobile/e2e/active-unread.spec.ts`；关联契约：`C-023`、`C-007`；来源：Issue #83。
- 兼容性：旧 PC 的 `open.detail.completed_seq` 缺失或为 `undefined` 时，手机跳过可选完成回执，正常对话及定时等待继续可用；现代 PC 的数值 `0` 仍允许首次可见终态回执。明确拒绝或网络失败保留未读并显示错误，不伪造成功；证据：`mobile/src/lib/completion-read-compatibility.test.tsx`、`mobile/e2e/unread-completion.spec.ts`；关联契约：`C-006`、`C-023`。

- 目的：让多任务完成后的结果持续可见，直到使用者实际查看；使用者：PC 与配对手机用户；入口：桌面任务前的静态状态灯、收起项目标题右侧的汇总状态、手机顶部完成未读列表；输入：主任务完成事件与已观察的 `completed_seq`；输出：独立于运行呼吸灯的完成提示，查看后在同一 PC 的客户端同步清除；前置条件：同一 host 的未归档对话；失败表现：读取回执失败保留提示并显示错误；关联契约：`C-023`、`C-003`、`C-006`、`C-007`；实现证据：`internal/store/completion.go`, `internal/server/completion.go`, `internal/remote/rpc.go`, `frontend/src/components/app/completion-reader.tsx`, `mobile/src/lib/completion-read.ts`, `mobile/src/lib/inbox-refresh.ts`；验证：`internal/store/completion_test.go`, `frontend/e2e/unread-completion.spec.ts`, `mobile/e2e/unread-completion.spec.ts`, `mobile/src/lib/inbox-refresh.test.tsx`。

### F-130 Agent runs, tools, and live control

- 桌面／Web、配对手机和终端可为会话选择「关闭思考」（终端 `/reason none`）。会话保存选择，从下一轮开始发送显式关闭指令。模型仍报告思考时显示错误。关联 `C-009`、`F-220`、`F-230`。证据：`frontend/e2e/conversation.spec.ts`、`internal/remote/composer_test.go`、`internal/engine/default_reasoning_http_test.go`、`internal/provider/thinking_off_test.go`。

- 桌面/Web 输入框的「工具」把内置目录收成一组，默认展开；每个已启用的 MCP 连接单独成组，默认折叠。组标题可以折叠。筛选框按工具标题、工具名或连接名缩小列表，并展开有匹配的组。工具名和说明默认不占行，指针停在说明图标上才显示。菜单开在输入框上方，不盖住正在输入的文字，小号开关留在滚动条左侧。打开思考强度菜单会关掉工具菜单，打开工具菜单会关掉思考强度菜单。默认开启的目录项开关打开，`python_runner` 与 `screenshot` 默认关闭；远端工具默认打开。内置开关立即写入与设置页相同的 `tools.disabled`／`tools.enabled`。远端开关写入该服务器的 `disabled_tools`，不改 `enabled`。下一次主管或 worker 的模型请求按内置选择发送，正在进行的回合也一样。直接挂载的远端函数会从下一次请求撤下；渐进披露时该名字会从 `list_tools` 消失，对应 `call_tool` 返回 `tool_switched_off`。两种情况下调用都不执行。本回合已经挂载的内置工具或直接远端工具重新打开后，下一次请求恢复。本回合开始时未挂载的内置工具、服务器或单个远端工具，要到下一回合重建工具表才出现。当前回合的系统提示仍可能提到刚关掉的工具。Settings 里关掉的服务器不出现在这个菜单。菜单底部的「工具设置」打开 Settings → Tools。这一页用「内置工具」和 MCP 两个标签分开目录和连接，搜索只命中一侧时打开对应标签；编辑 MCP JSON 时右侧整栏仍是文档。inspect 失败只显示读取失败，不编造工具，也不关掉服务器。手机没有这个菜单，主机配置仍作用于之后的模型请求。提供商云端 `builtin_tools` 不在此菜单。关联 `C-009`、`C-041`、`F-176`。证据：`internal/engine/tool_offer.go`、`internal/engine/tool_offer_test.go`、`internal/engine/tool_offer_http_test.go`、`internal/engine/mcp_remote_switch_test.go`、`frontend/src/components/app/composer-tools.tsx`、`frontend/src/lib/tool-toggle.ts`、`frontend/src/lib/composer-menu.ts`、`frontend/src/components/app/composer-tools.test.tsx`、`frontend/src/components/app/composer.test.tsx`、`frontend/src/components/app/settings-tools.tsx`、`frontend/src/components/app/settings-tool-tabs.test.tsx`、`frontend/e2e/composer-tools.spec.ts`。

- 目录列举、按文件名查找和内容搜索使用 `exec`。设置不显示 `ls`、`tree`、`glob`、`grep`，工具表也不注册这四项；已保存配置里的同名 `enabled`／`disabled` 不再生效。关联 `C-009`。证据：`internal/tools/tools.go`、`internal/tools/schema.go`、`internal/engine/prompt.go`、`internal/engine/turn.go`。

- 命令托管通过 `tools.enabled: [exec_session]` 显式开启。在 macOS／Linux 的新建普通轮次中，主管通过原 `exec` 启动命令。短等后的原进程继续在后台执行，主管可先处理独立工作。引擎推送增量输出并在下一次模型请求交付已就绪的终态。主管可以按会话 ID 查询最近输出和终态，也可等待指定命令。 可另行开启 `exec_adaptive_observation`，让引擎依据同一轮次的短命令等待证据，减少后续单独命令的额外模型往返。新输出续期观察窗口，观察到期仍在运行的进程继续执行。简单的等待型 `sleep` 由引擎挂起，任一命令退出后唤醒主管，显式延时按原时长执行。相同命令连续产生相同错误时，工具拦截重试并交回原错误，供主管调整做法。Stop 和内核确认的持续进程组停滞按所有权清理。关联契约：`C-009`。证据：`internal/engine/exec_session.go`、`internal/tools/exec_session_process_unix.go`；验证：`internal/engine/exec_session_http_test.go`。

- 普通主管可通过配置 YAML 或 Settings API 开启 `swarm.incremental_execution`，在实施回合加入先做小步实现、逐项验收用户要求的通用指导。默认关闭，真实任务收益需独立评测。关联 `C-009`。实现与验证见 `internal/config/config.go`、`internal/server/settings.go`、`internal/engine/incremental_execution.go`、`internal/engine/iterations.go` 以及 `internal/config/incremental_execution_test.go`、`internal/server/incremental_execution_settings_test.go`、`internal/engine/incremental_execution_http_test.go`。

- 主管在工具前给出的完整说明通过调度声明核对后出现在对应工具之前，分隔主对话的工作折叠。PC 与手机按同一事件顺序显示实时及分页记录。关联 `C-009`、`C-022`；证据 `internal/engine/manager_prose_order_http_test.go`。

- 联网工具执行位置：`web_search`／`web_fetch` 保持普通函数名、参数、开关、结果截断和工具 Trace；默认 `auto` 在当前 Gateway 账号授权后走该服务器云端，匿名走本机。设置可显式选本机或云端；云端拒绝／失败不重跑本地。Host 凭据仅在 Go 后端调用专用云工具路由，不进入 renderer、模型或普通推理 API；旧授权缺少服务端精确 Key 绑定时要求重新登录。关联 `C-006`、`C-009`；证据：`internal/tools/cloud_web.go`、`internal/tools/cloud_web_test.go`、`internal/tools/cloud_web_response_test.go`、`internal/engine/web_execution_test.go`。
- 桌面/Web 的 `memory add/replace/remove` 工具行复用 edit 的默认折叠、增删行数和红绿差异视图，展开只显示本次条目变更。新成功回执保存完整 before/after，旧记录缺快照时明确标注请求片段预览；失败保留错误和请求，重复添加显示未变化。关联 `C-025`；证据：`internal/memory/tool_result_test.go`、`frontend/src/lib/memory-diff.ts`、`frontend/src/components/app/tool-row.test.tsx`、`frontend/e2e/memory-diff.spec.ts`。

- 本地执行的 `web_fetch` 默认先 HTTP；识别带 DataDome 信号的 401/403 后，沿已有浏览器渲染路径尝试一次并保留工具代理。普通授权错误不升级，渲染失败或仍为挑战页返回失败；不保证真实站点可访问。入口与验证：`internal/tools/tools.go`、`internal/tools/webfetch_upgrade_test.go`；配置见 `docs/CONFIG.md`。

- 桌面/Web 的 `skill_manage patch` 工具行默认折叠，显示新增／删除行数；展开后按差异展示原始修改前后文本，支持纯删除和再次收起。实时与历史重放使用该次调用参数，失败仍保留原错误及尝试的修改。证据：`frontend/src/lib/edit-diff.ts`、`frontend/src/lib/tool-view.ts`、`frontend/src/components/app/tool-row.test.tsx`、`frontend/e2e/skill-patch.spec.ts`。

- 收取暂停后选择停止，回合空闲后的实时聊天与 Reload／分页历史保持相同的停止展示；回合仍被服务端标成 running 时，未决暂停保留 Continue／Stop，拒绝后显示仍在结束，而不把空闲文案叠在「工作中」上。内部 `workers_collection_declined` 回执留在 Trace，不额外显示原始 JSON，也不依据其中的历史 running 字段复活子代理。关联 `C-003`、`C-019`；证据：`frontend/src/components/app/iteration-limit-card.test.tsx`、`frontend/src/store/app-stream-worker-collection.test.ts`、`frontend/e2e/worker-completion.spec.ts`、`models/lifecycle/WorkerCollectionView.tla`。

- 主管工具回合暂停：桌面/Web 在待决定和已停止时提供「主管工具回合」，打开设置 → 集群并聚焦 `manager_max_iterations`。已经问出的这次继续仍加上当时的轮数；保存后的上限从下一次暂停读取，不恢复已停止的回合。已经继续的卡片不再重复这个入口。收取委派结果的暂停不提供它。答复核对暂缓（`reason:classification_deferred`）也停在同一张卡片上，但括号里的数字是已经用掉的轮数，上限仍是当前配置；这张卡片不打开主管工具回合设置。关联 `C-009`、`F-170`、`F-220`；证据：`frontend/src/components/app/iteration-limit-card.tsx`、`frontend/src/components/app/iteration-limit-card.test.tsx`、`frontend/src/components/app/settings-swarm.test.tsx`、`frontend/e2e/conversation.spec.ts`。

- 收取等待与工具轮数暂停分别显示原因；精确 wait_id 的 `workers_collection_resumed` 结清对应等待卡片，实时、重开与手机重放保持一致，不能把自动唤醒显示为人工 Continue 或已经收取成功。保留 worker 的模型调用在发起准入后冻结活跃 turn 归属，跨轮单次流仍归开始轮，辅助任务不迁移。关联 `C-012`、`C-019`；证据：`internal/engine/worker_accounting_test.go`、`frontend/e2e/worker-completion.spec.ts`。

- 普通回合完成门禁：没有当前可执行 goal 或活动同对话 wake 时，主管必须用真实 `wait_agents` 结果收取每个 worker 当前 generation，或用 `close_agent` 明确放弃，才可发布完成答复；已完成未收取仍待处理，wait 超时、未知 ID 和旧 generation 回执不结清。提前结束只自动纠正一次，再次提前结束保持原 turn 与 worker 请求；仅全为已知运行代且本 turn 未用自动唤醒时，等全部绑定 generation 结束后自动继续一次收取，否则等待人工 Continue／Stop；Continue 再授予一次尝试，Stop／拒绝取消。重启保留收取状态、纠正额度及人工暂停。Trace 与 PC／手机提示保留 `workers_uncollected` 的真实原因。关联契约：`C-019`、`C-002`、`C-012`、`C-027`；实现证据：`settlement.go`、`internal/engine/worker_completion.go`、`internal/engine/iterations.go`；测试：`settlement_http_test.go`、`internal/engine/ordinary_worker_completion_test.go`、`internal/engine/worker_completion_budget_test.go`、`internal/engine/worker_completion_replay_test.go`。

- 结构化提问：PC 与配对手机在实时和历史重放中按主机规则解析选项 ID；省略或归一化后为空的 ID 从 label 生成，保留选项文字，选择提交后继续同一 turn。关联契约：`C-002`；证据：`internal/engine/ask.go`、`frontend/src/lib/transcript-ask.test.ts`、`mobile/src/lib/ask.test.ts`、`frontend/e2e/plan.spec.ts`。
- 未答提问的交接恢复：重启保留原 turn/call 和人类等待，回答前不调用主管模型；匹配未答检查点的旧未知结果可恢复，真实答案优先。PC 与手机按明确的 `ask_recovered` 通知恢复同一表单，重复回放和迟到未知结果不会覆盖已确认答案。关联契约：`C-002`、`C-003`、`C-027`、`C-012`；验证：`internal/server/restart_ask_http_test.go`、`cmd/zwai/engine_restart_ask_acceptance_test.go`。
- 已答提问的交接恢复：按检查点的原 call 身份确认已回答问题，历史上参数错误或结果未知的其他提问不阻断续跑；当前身份缺失或冲突时仍拒绝。关联契约：`C-027`；验证：`internal/engine/restart_answered_ask_test.go`、`internal/server/restart_answered_ask_http_test.go`。

- 生成中断：桌面、Web 和配对手机按模型输出身份保留中断原因及恢复状态，旧思考结束流式动画，新生成使用独立身份；恢复中的 worker 保留原 ID，不冒充失败或任务完成。明确区分交接重启、进程退出、取消、超时、模型错误与输出额度；未记录的具体原因保持未知。实时、重开与历史重放保留说明，原 turn 的 Trace 可查。关联契约：`C-035`、`C-003`、`C-027`；实现证据：`signal.go`、`internal/engine`、`frontend/src/lib/transcript.ts`、`mobile/src/lib/transcript.ts`。
- 可选思考额度续接：`swarm.recover_reasoning_output_limit` 默认关闭。普通 Chat Completions 主管的某次输出仅有思考且被额度截断时，同一 turn 可持久认领一次续接；已执行工具、原 call ID、系统约束顺序及同进程 worker／后台 exec 所有者保留，额度和显式思考等级不重置。Stop、存储证据不足或已知配额为零时不续接；跨 engine 后旧后台 PID 不宣称仍可用。该能力不把中断变成任务成功，旧 worker 事件缺失代际仍保持未知。关联契约：`C-035`、`C-009`；验证：`internal/engine/output_limit_midturn_http_test.go`、`internal/engine/output_limit_midturn_boundary_http_test.go`、`output_recovery_run_join_http_test.go`。

- 巡检失败后的 Agent 状态：按 `F-150` 的有效任务归属回执保留指定 worker 的 running、思考流和待完成工具；父巡检错误仍可见，不派生为这些 worker 的失败原因，也不把内部归属 JSON 显示为正文。实时、完整历史和 roster 重放保持一致，实际完成／Stop／cleanup 仍生效。关联契约：`C-009`、`C-012`、`C-003`；实现证据：`frontend/src/lib/transcript-worker-ownership.ts`、`internal/engine/worker_ownership.go`；测试：`frontend/src/lib/transcript-worker-ownership.test.ts`、`frontend/e2e/schedule-worker-ownership.spec.ts`。
- PC 输出身份与回放：同一 worker 模型输出跨 manager 巡检轮次时，累计思考／回答快照更新同一条记录；独立输出即使文字相同也保留。历史页、按需 Agent 日志和实时事件重叠时，同一持久事件只应用一次，重放不重复触发终态刷新等副作用。旧记录缺少输出身份时不按相似文字推断合并。关联契约：`C-003`；证据：`signal.go`、`internal/engine/accumulator.go`、`frontend/src/lib/transcript-blocks.ts`、`frontend/src/store/thread-history.ts`、`frontend/src/store/app-stream.ts`、`signal_identity_test.go`、`internal/engine/accumulator_identity_test.go`、`frontend/src/lib/transcript-message.test.ts`、`frontend/src/store/app-stream-history.test.ts`、`frontend/e2e/agent-history-overlap.spec.ts`。
- PC 停止操作：桌面和 Web 使用输入框中的 Stop 按钮停止正在运行的任务；Esc 只关闭图片预览、Find 或其他当前弹层，未打开弹层和连续按 Esc 时也不会停止任务。关联契约：`C-002`、`C-028`；实现／验证证据：`frontend/src/App.tsx`、`frontend/src/App.test.tsx`、`frontend/e2e/shell.spec.ts`。
- 排队注入：同一会话／排队 ID 的进行中请求去重；已离队时刷新队列并明确提示，刷新失败保留独立错误；跨会话迟到响应不污染当前会话。后台自动出队以实际删除行作为唯一消费权，避免竞争重复投递或丢失并发编辑。关联契约：`C-002`、`C-012`；证据：`internal/store/followups_claim_test.go`、`internal/server/missing_record_test.go`、`frontend/src/store/app-followup.ts`、`frontend/src/store/app-followup.test.ts`、`frontend/e2e/followup-error.spec.ts`。

- 自动重启的任务交接沿用 `F-110`／`C-027`：已接受的回答、继续／停止决定不会因交接被忽略；已确认终态不重复执行；完成 manager 下的后台 worker 直接按原 ID 恢复。证据：`internal/engine/restart_recovery_test.go`、`internal/engine/restart_parked_test.go`。
- Worker 检查点后的观察与工具回执在实际恢复模型输入中保留一次；不同事件的同文观察仍分别保留。独立交接摘要、未知操作提示与未读消息不丢失，旧格式 seed 恢复继续可用。关联契约：`C-027`；验证：`internal/engine/recovery_context_test.go`。
- 原调用被历史裁剪的回执仍作为带原身份／正文的历史数据可供模型检查，不补造执行、不打断其他工具配对、不冒充新的用户授权。工具正文不能伪装成交接控制 cue。关联契约：`C-027`；验证：`handoff_orphan_test.go`、`internal/engine/recovery_context_test.go`。
- PC 人工停止与清理：未完成的子代理显示在独立的 Stopped／已停止分组，停止后的刷新与重开仍保留该状态。绑定当前代理身份且有非空原因的取消 `finished` 回执无论出现在清理前、清理后或主管终态后，都保留已停止；迟到的正文、流式文本、工具调用通知及后续取消或成功回执不能改变已确认的停止状态、活动或停止时间。不把资源清理当成成功，也不把普通 provider／watchdog 失败改成停止。真实完成或失败的结果保留，显式续办同一 ID 后重新接受运行和完成结果；原始事件仍在 Trace。关联契约：`C-002`、`C-003`；证据：`frontend/src/lib/transcript-stop.test.ts`、`frontend/src/lib/transcript-cancelled-finish.test.ts`、`frontend/src/components/app/panel-stop.test.tsx`、`frontend/e2e/restart.spec.ts`。
- Fork 上下文：新 worker 可接收 manager 的历史对话证据，但不接收 manager 的 System/personality/编排身份；仍使用当前分配任务。入口：`spawn_agent(fork_context=true)` / `Registry.SpawnForked`；关联契约：`C-019`；证据：`resume.go`、`swarm.go`、`fork_context_test.go`。

- PC 用户视图：连续工作区块只有一段思考或一个可见工具时，直接显示该条自身的折叠，一次点击即可查看内容；思考与工具正文在主聊天用户视图默认收起，保留活动状态和手动展开；开发者与 Agents 检查视图保持原行为。多项保留聚合，每轮回答默认可见并分隔工作区块，结束的持续目标回合也不折叠正文。关联契约：`C-022`；实现证据：`frontend/src/components/app/work-fold.tsx`, `frontend/src/components/app/transcript-fold.test.tsx`, `frontend/e2e/work-fold.spec.ts`。

- 目的：运行 manager/sub-agent、查看工具并发送跟进或插入；使用者：PC 用户；入口：对话输入框和 Agent 面板；输入：消息、工具结果、排队消息的修改、未读 steer 的修改；输出：实时事件和最终回答；前置条件：配置可用 provider；失败表现：运行错误和 Trace；关联契约：`C-002`, `C-003`, `C-012`；实现证据：`internal/engine`, `internal/tools`, `frontend/src/components/app`。
- 人工排队消息在当前轮次成功或错误结束后按 FIFO 启动独立新轮次；定时检查失败也遵守，原 turn/run 错误、未读结果及未来唤醒保持。无排队消息不因此自动重试；Stop、取消和引擎重启不自动出队。停止后的排队行点击注入，作为下一轮发送；活动轮次仍注入当前回合。关联契约：`C-002`、`C-009`。证据：`internal/engine/followup_test.go`, `internal/engine/followup_after_error_test.go`, `frontend/e2e/api-error.spec.ts`, `frontend/e2e/conversation.spec.ts`。
- 极快完成的轮次在发送 ACK 晚于终态事件时仍保持空闲，迟到 ACK 不改变后来选择的对话；证据：`frontend/src/store/app-send.test.ts`, `frontend/e2e/api-error.spec.ts`。
- 轮次开始后响应区立即显示旋转的“工作中”，首个服务端进度脉冲到达后切换为计时；轮次结束即消失，不等待本地模型首字节。实现证据：`frontend/src/components/app/transcript.tsx`, `frontend/src/components/app/transcript.test.tsx`。
- 桌面/Web 的结束分界沿用 `F-120`／`C-003` 的单次结束栏规则；持续目标各轮回答默认展示并保留该分界，进行中不提前标结束。实现证据：`frontend/src/components/app/transcript-session.tsx`, `frontend/src/components/app/transcript-session.test.tsx`, `frontend/e2e/schedules.spec.ts`。
- 代理名单：行上是 `resume_agent` 使用的 `agent_id`，不是 `#n`。进行中与已完成各自成组。组内默认按最后更新从新到旧；可改为创建时间或行上的名称，并在从新到旧 / 从旧到新（名称则是 A 到 Z / Z 到 A）之间切换。没有时间的行留在底部。实现证据：`frontend/src/lib/agent-label.ts`, `frontend/src/lib/agent-roster-sort.ts`, `frontend/src/components/app/panel.tsx`。
- 子代理失败原因：桌面/Web 的 failed 行提供「查看失败原因」入口，详情错误卡固定在日志滚动区外，保留记录的错误；已知错误按界面语言解释，旧记录缺失原因时明确说明未记录。父 turn 的 error 到达时仍在运行、且没有本轮有效归属保留回执的子代理保留父错误并标明来自所属轮次；子代理自己的已记录失败原因不被覆盖，先前 cleanup 已停止的代理不倒推成失败。精确的本代理 watchdog 错误可跳到设置 → Swarm 的 `agent_timeout_seconds`；精确输出额度错误跳到 `max_completion_tokens`，worker 迭代上限错误跳到 `max_turns`，聚焦对应输入；已识别的 provider 流空闲超时打开设置 → Models，不猜测具体 provider。普通 deadline、取消和未知错误不猜测配置修复；保存配置不会自动续办子代理。关联契约：`C-012`、`C-009`；实现／测试证据：`frontend/src/components/app/agent-failure.tsx`、`frontend/src/components/app/agent-failure.test.tsx`、`frontend/src/lib/agent-failure.ts`、`frontend/src/lib/agent-failure.test.ts`、`frontend/src/lib/transcript-failure.test.ts`、`frontend/e2e/agent-failure.spec.ts`、`frontend/e2e/worker-timeout.spec.ts`。
- 历史 worker 续接：manager 通过 System JSON 状态快照接收历史 worker 的 ID、角色及运行／完成／失败状态；已结束 worker 仍可查询或显式续办，不把历史名册重放为本轮新建调用。持续目标、定时检查、压缩、模型重试及重启使用同一状态语义。关联契约：`C-019`、`C-012`；实现／测试证据：`internal/workerstate/snapshot.go`、`internal/engine/autocompact_roster.go`、`internal/engine/resume_workers.go`、`internal/engine/worker_roster_test.go`、`frontend/e2e/worker-roster-history.spec.ts`。
- 排队与未读引导：工作中的 Enter 进入队列。点开一条排队消息，或点未读引导上的铅笔，原文进输入框（引用变成卡片），原来的那条先拿掉。再按 Enter，和新建一条排队一样，排到队尾。已读引导不会被拉进输入框。只有图片的未读引导没有可排队的文字，只能删除。
- 主动委派：共享运行时提示词要求 manager 在能提效或提高质量时主动调用 subagents，执行中重新评估；单个 worker 也可承担独立审查或第二种思路，不要求用户显式提出。普通对话与持续目标遵循相同策略；无收益时自行处理，遵守用户限制与并发上限，由 manager 核验和整合结果。关联契约：`C-019`；实现证据：`internal/engine/prompt.go`, `internal/engine/prompt_test.go`, `cmd/zwai/tui_prompt_test.go`。

- 无效委派恢复：同一模型回复内缺少 role/task 或参数不可读的 spawn 调用按错误类别合并为一条拒绝回执，保留数量和原始代表 ID；有效调用仍正常执行，manager 可补全参数在原轮次继续。拒绝不等于创建或取消 worker。关联契约：`C-019`；实现／测试证据：`spawn_batch.go`, `spawn_batch_test.go`, `tools.go`, `internal/engine/prompt.go`。

- 有界 worker 超时恢复：同一 worker 连续三次 watchdog 失败后停止 manager 的 resume／同角色 spawn，持续目标停止自动推进，失败周期计划暂停；成功或显式人类重试恢复预算。错误、worker ID 和次数保留在 Trace 与手机/桌面对话；普通 provider 超时和取消不计入。关联契约：`C-019`、`C-009`；实现／测试：`worker_timeout.go`、`internal/engine/worker_timeout.go`、`internal/engine/worker_timeout_test.go`、`frontend/e2e/worker-timeout.spec.ts`。

- 错误提示：模型 HTTP/流响应无法解析时，桌面和手机区分响应解码与真实工具参数错误，并保留同 turn Trace 和最多两次重试；证据：`internal/engine/api_error_integration_test.go`, `frontend/e2e/api-error.spec.ts`。
- 流式接收诊断：桌面、Web 和配对手机将已收到响应头后的传输层读取超时显示为“接收模型响应超时”。中途读失败或截断时，Trace 按实际请求保留已接收的响应体字节数、最后收到数据的 UTC 时间及可用的 Gateway 请求 ID。关联契约：`C-012`、`C-029`；证据：`internal/provider/stream_read_test.go`、`internal/engine/api_error_integration_test.go`、`frontend/e2e/model-read-timeout.spec.ts`、`mobile/e2e/api-error.spec.ts`。
- 首字节超时恢复：错误保留实际等待时长；桌面/Web 建议到设置 → 模型调大首字节等待时间，可从错误直接打开失败轮次的提供商并聚焦字段。设置能力和提供商缺失时的行为见 `F-170`；关联契约：`C-009`、`C-012`。
- 请求前工具参数检查：压缩 middleware 返回状态后检查当前尾部调用的非空 JSON 参数；无效参数在实际模型 HTTP 前拒绝，沿既有有界重试清理，不清除人类 steer 或伪造 worker 创建。不是全历史 JSON 扫描。关联契约：`C-012`；证据：`internal/engine/autocompact.go`、`internal/engine/model_retry.go`、`internal/engine/model_retry_http_test.go`。

- 模型故障退避：同一上游 origin 的 manager、subagent、模型及 provider 别名共享 5→10→20→30 秒封顶冷却，到期仅放行一个恢复探测；探测流交出首段已解码非空正文、推理或工具调用增量后即恢复并发，响应头、role、空 delta、usage 和错误帧不能放行。故障档位仍仅由这条探测的最终结果改变：完整成功才清零，后续截断／空闲超时等服务失败推进冷却，期间并行请求的结果不计入且不能迟到改写后继故障代。非流式 Generate 等完整结果；Chat Completions HTTP 流缺少结束标记及 finish reason 时仍为截断错误，保留已收到的 usage。Stop 可取消等待及请求，换轮次或清缓存不能绕过。等待记录进入原 turn 的 Trace，不增加自动重试次数；quiet 报告后的迟到等待不重新显示 Chat 通知。调度答复分类独立重试，不等待或影响此 origin 门。关联契约：`C-029`；实现／测试证据：`internal/provider/backoff.go`、`internal/provider/model_backoff.go`、`internal/provider/stream_completion.go`、`internal/provider/stream_completion_test.go`、`internal/provider/backoff_integration_test.go`、`internal/engine/model_backoff_test.go`、`frontend/src/lib/transcript-backoff.test.ts`。

- 网关无健康 provider 恢复：首字节预算内完整返回的明确选路 503 在原逻辑模型调用内等待同 origin 探测，不消耗两次模型重试，不生成中断／恢复状态；每次真实拒绝请求保留调用记录，成功响应直接续接。同一逻辑调用连续三次确认拒绝后回合失败，不再无限探测，且不记模型重试、不自动续跑目标。Stop、调用者 deadline 和 worker watchdog 仍可提前取消；普通 503、挂起与断流仍按原预算处理。同 origin 仍有选路故障时跳过该回合收尾摘要和自动模型记忆复查，不误报已保存；本地确定性整理保持。摘要、所有记忆复查及标题首次发现故障时保留原单次失败，不借既有 timeout 持续探测；手动 compact 没有活跃回合取消所有者，保留单次失败语义。关联契约：`C-029`；实现／测试证据：`internal/provider/route_recovery.go`、`internal/provider/route_recovery_test.go`、`internal/provider/route_rejection_test.go`、`internal/provider/route_recovery_protocol_test.go`、`internal/engine/route_recovery_http_test.go`、`internal/engine/route_unavailable_test.go`、`frontend/e2e/route-recovery.spec.ts`。

### F-135 Independent task coordination

- 目的：让不同 PC 对话的管理 Agent 发现活跃任务、协商共享资源的处理顺序；使用者：PC 用户和配对手机用户；入口：manager 工具 `list_tasks`、`send_task_message`、`read_task_messages`，对话内的协作消息通知；输入：真实 conversation ID、消息和可选收件游标；输出：活跃任务的工作目录/请求/状态、持久化发送回执、来源可追溯的消息和明确回复；前置条件：同一 host 的未归档对话；失败表现：未知/归档目标、非法参数或数据库错误返回工具错误，不伪造送达；关联契约：`C-021`、`C-012`；实现证据：`internal/engine/task_coordination.go`、`internal/engine/task_coordination_flow_test.go`、`internal/store/event_kind.go`、`frontend/e2e/task-coordination.spec.ts`、`mobile/src/lib/task-message.test.ts`。
- 这是通信协商能力。发现结果不是资源锁，发送成功不是接收方已同意；双方明确回复后才可依赖分工或执行顺序。消息不会代替用户授权、自动启动空闲对话、解除暂停或打断另一个任务。

### F-136 Nuwa project collaboration

- 项目设置保存规划／执行模型，宿主按回合冻结模式和工具范围。项目指令、真实模型选择、Registry 分工及恢复由 `C-037` 约束。缺失模型、越权派工或超额度验收明确拒绝。工作记录另见 `F-139`。入口：项目创建／编辑中的 Nuwa collaboration 和 `list_models` 工具。实现证据：`internal/engine/nuwa_models.go` 和 `internal/engine/nuwa_model_pair_http_test.go`。见 [Nuwa](NUWA.md)。


失效模型恢复：桌面错误行提供“使用当前所选模型修复恢复配置”，仅显式迁移已移除的提供商；保存后继续／重试，保留 worker 身份、历史和工具范围。空闲／无 parked Registry 才可操作；失败不部分更新。契约 `C-037`；证据 `internal/engine/worker_model_recovery_http_test.go`、`frontend/src/components/app/worker-model-recovery.test.tsx`。

- 女娲执行者每次收到当前 generation 已实际绑定的身份及模型；`list_models.current` 显示同一事实，和会话轮次的 `default` 分开。原绑定跨续办和冷恢复保留；旧未绑定执行标为未知。提示是临时运行事实，不改变普通模式、项目指令、工具权限或可委派范围。关联 `C-037`；实际输入及工具验证见 `internal/engine/nuwa_context_worker_http_test.go`、`internal/engine/nuwa_context_restore_http_test.go`。
- Nuwa manager 只负责规划、派工、协调和基于 worker 证据验收，不挂 workspace／MCP 实际执行工具，并禁用其 provider-hosted builtin 执行；worker 和普通模式保留原 provider 配置。需要操作的任务由 worker 执行。无需操作的纯讨论不强制创建 worker。角色、任务拆分和工具选择由 AI 根据目标决定，不固定领域角色表或工作流。
- 勾选女娲时，项目保存一对可修改的规划模型和执行模型，默认来自就绪目录顺序，不按能力排序，也不写死模型名。之后的回合用规划模型；新角色、验收和修复用回合开始时复制的执行模型。冲突的 `model_ref` 在创建 worker 前拒绝。同角色恢复保留原 binding。关闭女娲保留这一对。四列仍为空的旧项目保持原来的显式选择。已保存模型不可用时，下一回合在模型请求前失败。会话输入框在已保存规划模型时改的是项目规划模型；手机端不新增编辑入口。验证：`internal/engine/nuwa_model_pair_http_test.go`、`internal/server/nuwa_model_pair_test.go`、`frontend/src/components/app/project-dialog.test.tsx`、`frontend/e2e/nuwa.spec.ts`。
- Nuwa 执行者通过独立项目上下文直接获得用户的项目目标、约束和验收指令，memory 关闭时仍可读取；不继承规划者完整 System／personality，不改变既有角色和工具权限。普通模式不变；非空已存 Instruction 首次恢复保留原文，旧空 Instruction 保留按配置重建的兼容路径。实际模型 HTTP 输入与已保存 worker 恢复验证：`internal/engine/nuwa_project_instructions_http_test.go`。
- 规划者可查询 worker 的完整可选工具目录，并在派工时为新角色选择所需的 workspace／MCP 工具；manager 不挂执行工具不会缩减 worker 的可选目录。省略选择时使用 worker 可用范围，显式选择随身份恢复，后代可继承或缩小。见 `C-037` 和 [Nuwa](NUWA.md)。既有工具选择验证：`internal/engine/worker_tool_scope_flow_test.go`、`internal/engine/worker_tool_scope_restore_test.go`；规划／执行分离验证：`internal/engine/nuwa_planner_separation_http_test.go`、`internal/engine/nuwa_planner_responses_http_test.go`、`internal/provider/nuwa_planner_builtin_test.go`。
- 女娲验收次数由主机计数：同一回合、同一可见工作记录最多一次 `verify` 和一次 `fix`。超额在创建 worker 之前拒绝，同一次调用重送不重复扣次，重启后额度仍在。普通实现和普通模式不占这个额度，也不按任务原文猜测意图。额度不是设置项。验证：`internal/engine/nuwa_verify_budget_http_test.go`、`internal/store/nuwa_verify_test.go`、`verify_admission_test.go`。

- 升级时回填旧 turn 未记录的项目归属，既有 worker 可跨重启恢复。已记录归属与当前项目不符时，显式继续目标返回冲突并保留 checkpoint，归属修复后可继续。见 [归属迁移](DATA_MODEL.md#turns--one-request-and-everything-done-to-answer-it)。

### F-139 Conversation work records

- 入口：PC／Web 右侧工作记录、宿主 `read_work`／`record_work` 与会话工作接口展示并保存任务、事实、成果及可核验来源。新记录默认归会话，可单向合并到所属项目并保留身份、历史和依赖。规划回合只读。非法共享、版本冲突和存储故障有明确反馈。见 `C-055` 和 [工作记录](WORK.md)。
- 面板默认展示会话私有记录，可切换至项目共享记录。没有自有工作记录的历史会话可提炼。Agent 与模型默认折叠，选择节点打开单独详情。保留 worker、Trace 和文件入口。可展开发现已剪枝记录，状态恢复时自动回显，仍需要重新确认的依赖有独立提示。实现证据：`internal/store/nuwa.go`、`internal/engine/nuwa_tools.go`、`internal/server/nuwa.go`、`frontend/src/components/app/nuwa-panel.tsx` 和 `frontend/e2e/nuwa.spec.ts`。

### F-138 Project Markdown knowledge base

- 目的：在一个项目内用可编辑的 Markdown 页面积累、查找和追溯跨对话的知识。
- 入口：PC／Web 右侧 Knowledge 标签、提炼菜单、对话 wikilink、项目回合的 `read_knowledge`／`record_knowledge` 和项目知识库 HTTP 路由。Agents、Files、Trace、工作记录和任务面板可绑定当前会话，Memory 和 Knowledge 属项目范围，所有标签共用一条标签栏并用浅底色区分。
- 输入与输出：项目 schema、稳定页面 ID、Markdown、历史引用；提供类型或关系树、待处理项、单页、反链、来源、Trace 和宽幅预览。Knowledge 的「关系图」按标题显示项目内的全部页面，通过已解析的关系和 Wiki 链接连线，可搜索、缩放并打开对应页面。Memory 与 Knowledge 共用一次后台提炼，支持最近成功回合、当前任务和当前项目。启用项目记忆、自动复盘和知识库后，成功非计划回合的自动复盘也可写入知识库。当前任务和当前项目按保存的历史范围分批合并笔记、Skills 与知识库变更，可暂停和继续。两个面板显示已处理数、剩余数、同一回执与 Trace。
- 前置与失败：Settings 中的全局开关默认开启。首次非计划回合可初始化项目知识库，也可在面板先手动初始化。关闭后隐藏面板并停止知识库提炼。部分提交保留实际写入回执。无项目对话提示移动到项目。相同名称、旧 revision 和错误来源拒绝写入；已保存来源失效时给出提示。
- 提炼进度条反映已保存的批次进度，区分只读摘录与整条跳过。当前任务和当前项目默认摘录，也可选择全文分段，两个面板显示当前长记录的进度，暂停和重启后继续。引文校验与暂时性服务失败可触发一次未写入批次的重试，配置类失败给出对应的调整提示。
- 「构建 Wiki 图谱」为当前任务或项目本地扫描可引用历史，先发布带原文来源的节点和明确关系。图谱可在后台语义复核期间搜索和浏览，已核对的正文分批补写。面板分别展示扫描、复核和文档进度。重新构建会复用已确认的历史记录，暂停与续跑使用独立回执。
- 契约：`C-053`、`C-054`、`C-036`、`C-044`。
- 实现证据：`internal/knowledge`、`internal/engine/knowledge.go`、`internal/server/project_knowledge.go`、`frontend/src/components/app/knowledge-panel.tsx`、`frontend/e2e/project-knowledge.spec.ts`。

### F-140 Files and attachments

- 新任务按会话拥有独立文件根。PC「文件」默认展开成果，过程和上传独立折叠；可切换只读项目目录、预览和下载。旧会话仍使用原共享路径。关联 `C-052`；证据：`internal/engine/task_workspace.go`、`frontend/e2e/task-workspace.spec.ts`。

- 目的：让对话使用工作区文件和附件；使用者：PC 用户；入口：文件面板、上传及下载 API；输入：文件或图片；输出：文件、预览或模型视觉输入；前置条件：工作区和权限可用；失败表现：上传或工具错误；关联契约：`C-004`, `C-005`；实现证据：`internal/server`, `internal/tools`, `frontend/src/components/app`。
- 图片输入：png/jpeg/gif/webp 文件在空 MIME、通用 binary 或 UTI 标签下仍进入视觉输入；显式 text/PDF 保持文件附件。客户端只归一化 MIME 声明，不改原字节，服务端继续执行真实类型与声明匹配校验。关联契约：`C-028`；证据：`frontend/src/lib/paste-image.ts`、`frontend/src/lib/paste-image.test.ts`、`frontend/src/components/app/composer-image-drop.test.tsx`、`frontend/e2e/image-input.spec.ts`。
- 对话续发：直接发送或排队续发的原图和说明由当前回合的对话模型接收。已发送图片可以从会话历史重开，后续问题也可引用原图。证据：`internal/engine/image_input_http_test.go`。
- 提取文字：桌面和 Web 用户从已发送图片预览发起识别。可复制结果，模型不可用时在预览给出反馈。关联契约：`C-028`、`C-040`。证据：`internal/engine/image_ocr_test.go`、`frontend/e2e/image-input.spec.ts`。
- 已发送图片预览：桌面与 Web 对话中的图片缩略图可点击或通过 Enter／Space 打开放大层，多图时打开点选的图片。预览沿用已保存图片地址，按原比例适配窗口；显示加载及失败状态，可通过 Esc、遮罩或关闭按钮关闭并回到缩略图焦点，刷新重载后仍可打开。关闭预览不停止正在运行的任务。关联契约：`C-028`；证据：`frontend/src/components/app/input-thumbs.tsx`、`frontend/src/components/app/input-thumbs.test.tsx`、`frontend/e2e/image-input.spec.ts`。
- 工作区列表：一次请求只读取一个目录的全部直接子项；展开目录后再读取下一层。筛选使用该工作区最近一次完整扫描，在内存中匹配路径，不为每个命中之外的文件再取大小；结果到达前不把已经加载的局部列表当作查找结果。打开或刷新工作区根、上传和删除会丢掉扫描并开始下一次。查找等待这次扫描结束，失败或取消不返回半成品，失败的扫描也不会留给下一次查找。点目录、`node_modules` 和 `vendor` 仍不列出，agent 仍可读取这些路径。本轮里由工具新写的文件，在回合结束或手动刷新触发根目录列举之后进入查找。证据：`internal/engine/list.go`、`internal/engine/file_index.go`、`internal/engine/list_test.go`、`internal/engine/file_index_test.go`、`internal/server/workspace_test.go`、`frontend/src/components/app/files-tab.tsx`、`frontend/src/store/workspace-files.ts`。

- 选中目录：桌面文件树里当前选中的目录行始终显示“在文件管理器中显示该文件夹”。鼠标移到其他目录上也会出现，移开未选中的目录后隐藏。点击只打开该目录，不展开或收起。工具栏按钮仍打开工作区根目录。浏览器没有 reveal 能力时不显示。证据：`frontend/src/components/app/files-tab.tsx`、`frontend/src/components/app/files-tab.test.tsx`。
- 回答里的文件路径：桌面与 Web 的 Markdown 在回答结束后，把工作区相对路径显示成按钮。点击后打开与文件树相同的预览。已在文件树中的路径按原样打开；否则若该文件在任务的 `work/` 下，打开那个路径。同名文件多于一个时列出这些路径，选中后再预览，不会去读工作区根上不存在的短名字。目录、代码块、已有链接和预览打不开的类型保持原文。流式输出未完成时仍是纯文本。切换对话关闭预览。证据：`frontend/src/lib/workspace-file-ref.ts`、`frontend/src/components/app/workspace-file-open.tsx`、`frontend/src/components/app/workspace-file-preview.tsx`、`frontend/src/components/app/markdown.tsx`、`frontend/src/lib/workspace-file-ref.test.ts`、`frontend/src/components/app/markdown.test.tsx`、`frontend/src/components/app/workspace-file-preview.test.tsx`、`frontend/e2e/conversation.spec.ts`。
- 工作区文件预览：桌面与 Web 文件树单击文件或按 Enter 打开宽幅内容窗；Markdown 默认显示文档，可切换源码，普通 UTF-8 文本与已知源码显示完整文本及适用的语法高亮，png/jpeg/gif/webp 图片按比例显示。图片提供放大、缩小、当前缩放比例、原始大小（100%）和适应窗口按钮；默认适配窗口且不放大小图，手动缩放范围为 10%—400%（超大图初始适配可低于 10%），超出视口时可横纵滚动。刷新、换图或重新打开恢复默认适配，加载中或解码失败时不能缩放；缩放不改变原图和下载字节。提供复制文本、刷新、原文件下载，原生桌面仍可在文件管理器中显示文件。Esc／关闭后焦点回到文件树，切换对话或关闭取消旧读取。预览读取最多 8 MiB，超过 256 Ki 字符回退完整纯文本；文件过大、不支持或读取失败明确显示，保留下载入口。HTML 在不含 `allow-same-origin` 的沙箱 iframe 中显示，可切回源码，iframe 不使用下载地址；同目录和以工作区根为起点的相对资源在字节上限内内联，逃出根目录、读取失败、SVG 的 href／xlink:href，以及本机和其他非公网地址都会去掉。CSV/TSV 默认显示表格并可切回源码。DOCX 显示标题、段落、列表、表格和包内图片。XLSX 按工作表显示已保存的单元格值（日期保持序列值），没有缓存的公式只显示公式文本。EPUB 按阅读顺序显示章节，资源只来自包内且不运行脚本。PPTX 按幻灯片顺序显示文字和图片。不执行宏，不跟随包外链接。SVG 仍是转义源码，MDX 不求值，Markdown 仍不解析相对当前文档目录的资源。解析失败保留下载。关联契约：`C-004`、`C-005`；证据：`frontend/src/components/app/file-preview.tsx`、`frontend/src/components/app/document-preview.tsx`、`frontend/src/lib/file-preview.ts`、`frontend/src/lib/documents/html-artifact.ts`、`frontend/src/lib/documents/delimited.ts`、`frontend/src/lib/documents/docx.ts`、`frontend/src/lib/documents/xlsx.ts`、`frontend/src/lib/documents/epub.ts`、`frontend/src/lib/documents/pptx.ts`、`frontend/src/lib/use-file-preview.ts`、`frontend/src/components/app/file-preview.test.tsx`、`frontend/src/lib/file-preview.test.ts`、`frontend/src/lib/documents/office.test.ts`、`frontend/e2e/file-preview.spec.ts`。

### F-145 Durable composer drafts

- 目的：在刷新、窗口恢复及引擎换端口后保留未发送内容；使用者：PC 桌面和 Web 用户；入口：输入框、附件／粘贴图片／引用，以及 `/api/composer/drafts/:key`；输入：正文、引用、原始文件与图片；输出：按会话或新会话项目隔离的完整草稿，异常时显示恢复指引；前置条件：本机 SQLite 可写；失败表现：保留当前输入并显示恢复／保存失败、发送未确认或版本冲突，不自动重发；关联契约：`C-033`, `C-028`, `C-011`；实现证据：`frontend/src/lib/composer-draft.ts`, `frontend/src/lib/use-composer-draft.ts`, `frontend/src/components/app/composer-draft-status.tsx`, `internal/store/composer_draft.go`, `internal/server/composer_draft.go`, `internal/store/composer_draft_test.go`, `internal/server/composer_draft_test.go`, `frontend/src/components/app/composer-draft.test.tsx`, `frontend/src/components/app/composer-draft-status.test.tsx`。
- 正常加载、保存中、已保存及发送中隐藏草稿状态区和“删除草稿”，不占输入框空间；仅恢复／保存失败、冲突或发送状态未知时显示提示及现有处理按钮，异常解决后自动隐藏。
- 正文、引用、附件引用在同一事务中保存；原始 bytes 只在新增附件时写入，后续键入仅更新快照。保存 ACK 前不能标记为已保存；发送 ACK 后才清理对应版本，旧页面迟到保存不能复活已清草稿。首发迁移保留来源副本至发送确认。
- 后续编辑引用的附件 bytes 缺失或长度不符时拒绝保存，保留原版本，不能仅凭 manifest 返回保存成功。
- 离开已保存且无待处理操作的 scope 后释放内存中的附件缓存和预览 URL，返回时重读原始 bytes；未保存、冲突和发送中内容继续保留。
- `/goal`、`/plan` 仅接收文本；同时带附件、粘贴图片或引用时，输入框拒绝动作并保留完整草稿，不能用只覆盖文本的确认清理其他内容。
- 每份草稿正文与引用合计最多 1 MiB，最多 32 个附件、附件总量 256 MiB；视觉图片另遵守最多 8 张、单张 8 MiB。此限制约束草稿保存，不改变工作区上传的单文件上限。
- 桌面选择或拖入的文件会在当时读入内存，再保存草稿并上传到工作区。WebKit 放进 multipart 头的原始控制字符不再让整份草稿无法保存；文件内容仍按原字节保存。读不出来时提示重新选择，其余草稿保留。

### F-146 Next-step composer suggestions

- 目的：一次可见回复结束后，给空的 PC 输入框一条基于当前会话的下一步；使用者：PC 桌面和 Web 用户；入口：空输入框占位符，设置 → 集群 → 建议下一步；输入：最近的人类消息和可见回复；输出：一条不超过 240 字、可直接发送的下一步，或没有建议；前置条件：功能开启（默认开，旧配置缺键仍开），本轮正常结束、最终回复非空、不是安静完成，且会话已经空闲；失败表现：无建议、超时、模型出错、或下一次对话已经开始时不显示占位符；Tab 只填入，回车才发送；手机空输入框上方显示同一条建议，点击或 Tab 填入，关闭只影响当前页面；关联契约：`C-039`、`C-009`、`C-012`；实现证据：`internal/engine/composer_prediction.go`、`internal/engine/composer_prediction_test.go`、`frontend/src/lib/composer-prediction.ts`、`frontend/src/components/app/composer.tsx`、`frontend/e2e/composer-prediction.spec.ts`、`mobile/src/lib/composer-prediction.ts`、`mobile/e2e/composer-prediction.spec.ts`。
- 建议是 Trace 事件，不是对话消息。下一次模型请求不会把它当成已经发送的人类输入。关闭开关后不再发起这次调用；省略该字段的设置保存保留原值。

### F-147 Temporary task side chat

- 入口：桌面／Web 会话标题栏的侧聊按钮、在主输入框提交 `/side [问题]`，或在对话记录中选中文字后点击「在侧聊中提问」。带问题的命令预填侧聊输入框，由用户单独发送。
- 输入与输出：拖动面板左边界可调整宽度，聚焦边界后也可用左右键调整。侧聊读取当前任务的近期记录、项目笔记和已记录技能，并可继续搜索该任务的工具结果与 worker 正文、打开一条技能。问题依赖这些来源时，提示要求先查找，而不是让用户补交已有记录。回答流式显示，支持连续问答。模型默认跟随任务，也可单独切换。面板关闭、切换会话或重新加载后清除临时记录。发送失败可在面板内重试，取消等待不会停止主任务。
- 前置条件：需要已有会话和可用的模型。关联契约 `C-042`。实现证据： `frontend/e2e/side-chat.spec.ts`，HTTP 流式交付与隔离见 `internal/server/side_chat_http_test.go`。

### F-150 Goals and scheduled waits

- 独立任务的发现复用本任务在所选项目中已有的结果会话。桌面、Web 和配对手机标出「定时结果」、任务 ID 和来源会话。桌面和 Web 可打开 Scheduled 里的任务，三端都能打开仍存在的来源会话。Scheduled 的 Open findings、每次运行的 Trace 和历史未读继续可用。关联 `C-009`；实现／验证：`internal/store/schedule_findings.go`、`frontend/e2e/schedules.spec.ts`、`mobile/e2e/schedule-findings-source.spec.ts`。

- 手机目标模式：未完成目标条明确标注“目标模式”，在主对话、子 Agent 列表、worker 日志与产出物页面保持可见；恢复目标按钮仍仅在主对话提供。完成或清除目标后收起。关联契约：`C-009`；证据：`mobile/src/components/status-banners.tsx`、`mobile/src/components/thread-screen.tsx`、`mobile/src/components/goal-mode.test.tsx`、`mobile/e2e/goal-mode.spec.ts`。

- 暂停状态的含义：PC 与手机目标条明确显示“自动执行已暂停”和自动执行暂停原因；时间标为“目标设立至今”，从 `goal_started_at` 累计，包含暂停期间。后续普通手动答复不会解除既有目标暂停；显式恢复、目标完成、清除或重新设定目标按各自回执更新状态；实际可用的 Start 控件旁才显示恢复操作提示。无恢复入口时仍显示已记录的暂停原因。关联契约：`C-009`；实现／验证：`frontend/src/components/app/goal-banner.tsx`、`frontend/src/components/app/goal-banner.test.tsx`、`frontend/e2e/goal-pause.spec.ts`、`mobile/src/components/status-banners.tsx`、`mobile/src/components/goal-pause-meaning.test.tsx`、`mobile/src/lib/goal-pause.test.ts`、`mobile/e2e/goal-pause.spec.ts`；来源：Issue #131。

- 最后一轮自动续跑且无活动同对话 wake 时，Goal 明确要求本轮收齐或显式放弃 pending worker，不能承诺下一自动轮接管；历史 finished 与 running 分组提示。运行代完成唤醒遵循 `C-019` 的每 turn 一次额度及重启身份边界。证据：`internal/engine/prompt_goal_worker.go`、`internal/engine/worker_completion_wake_test.go`。

- 暂停后的单轮回答：capped／blocked／idle 目标遇普通 PC／手机发送时保留暂停标志和自动续轮计数（包括调度声明连续失败封顶及其原 reason／attempt／cap），仅回答当前消息；System 段携带人类来源与当前 turn 请求，工具限于该回答，暂停目标不授权新调查 worker。显式 Start、HTTP `goal_resume`、RPC `resume_goal` 才恢复追求；未暂停时人话仍归零连续计数。开放目标缺少可观察交付物或停止条件时，提示模型先对同一目标文本用一次 `ask_user` 明确完成判据，不逐轮重问；完成仍需 `complete_goal`。关联契约：`C-009`、`C-006`、`C-008`；实现／验证：`internal/engine/goal.go`、`internal/engine/prompt_goal_pause.go`、`internal/engine/goal_test.go`、`internal/engine/prompt_goal_pause_test.go`、`internal/server/goal_pause_integration_test.go`、`frontend/e2e/goal-pause.spec.ts`。

- 手机引用命令准入：正文经 PC／手机共享的纯解析器识别为 `/goal` 或 `/plan`，且存在至少一条去空白后非空引用时，提交前显示本地拒绝提示；正文、引用和所选文件保留，不先封装引用、清稿、上传或调用发送／插话 RPC。用户修正正文或移除有效引用后自行重试，纯文本继续原有命令语义；未知命令、路径及引用／代码内的字面命令保持普通消息。关联契约：`C-009`、`C-033`、`C-011`；实现／验证：`frontend/src/lib/slash-core.ts`、`mobile/src/components/composer.tsx`、`mobile/src/components/composer-quoted-command.test.tsx`、`mobile/e2e/quoted-goal.spec.ts`；变更类型：Fixed；来源：Issue #111。
- 手机命令入口：PC 对话和新建对话的输入框最前面提供添加菜单，集中添加文件与 `/goal`、`/plan`；输入 `/` 按同一目录过滤。点选写入 `/name `，没有参数时不发送。已有正文时不提供 plan。提问回答和直连模型不列出这些 PC 命令。`/compact` 仍只在电脑端执行。系统返回先关闭菜单。关联契约：`C-009`、`C-015`；实现／验证：`mobile/src/lib/composer-commands.ts`、`mobile/src/components/composer-menu.tsx`、`mobile/src/components/composer.tsx`、`mobile/src/components/composer-slash.test.tsx`、`mobile/e2e/composer-commands.spec.ts`。
- 显式命令参数：PC 目标与计划仅发送一次完整命令，命令参数中的 `/goal`、`/plan` 字面量保持原文，空闲到运行／待答的竞争不把命令当成跟进或回答。目标可更新并保留原待答问题；计划只创建新轮次，转忙时拒绝且保留草稿；裸计划与忙碌更新保持原有边界。关联契约：`C-002`、`C-009`、`C-033`；实现／验证：`frontend/src/store/app.ts`、`frontend/src/store/app-plan.ts`、`frontend/src/store/app-goal.test.ts`、`frontend/src/store/app-plan.test.ts`、`internal/engine/turn.go`、`internal/engine/slash_command_once_http_test.go`、`frontend/e2e/trailing-goal.spec.ts`、`frontend/e2e/plan.spec.ts`。
- 显式目标命令：`<objective> /goal` 使用前置正文激活新持续目标，清除旧目标的完成状态；PC、HTTP 和手机发送／插话共用命令语义，普通消息、含中文的路径、引用及代码中的字面命令保持普通内容。关联契约：`C-009`；实现／验证：`internal/slash/trailing.go`、`internal/slash/trailing_goal_test.go`、`internal/slash/testdata/trailing-goal.json`、`internal/engine/slash_steer_test.go`、`internal/remote/slash_goal_test.go`、`frontend/src/lib/slash.ts`、`frontend/src/components/app/composer-trailing-goal.test.tsx`、`frontend/e2e/trailing-goal.spec.ts`。
- 目标完成展示：桌面/Web 与配对手机在目标 `complete` 后立即隐藏输入框上方目标条，刷新或重新进入已完成会话仍不显示；保留持久化目标、完成状态、结果和历史记录，不自动清空或重新执行。桌面与手机对话历史保留 `goal_complete` 完成通知；桌面/Web 的完成通知置于已折叠目标会话外，早于或晚于最终回答到达都可见；新设 `/goal` 后重新显示目标条。未完成的暂停、受阻和计划等待继续保留状态及相应控制；已完成目标不再通过目标条显示 Start。关联契约：`C-009`、`C-003`；实现／验证证据：`frontend/src/components/app/goal-banner.tsx`、`frontend/src/components/app/goal-banner.test.tsx`、`frontend/src/lib/transcript.ts`、`frontend/src/components/app/transcript-session.tsx`、`frontend/src/components/app/transcript-session.test.tsx`、`frontend/e2e/goal-resume.spec.ts`、`frontend/e2e/goal-schedule.spec.ts`、`mobile/src/components/status-banners.tsx`、`mobile/src/components/thread-screen.test.tsx`、`mobile/src/lib/transcript.ts`、`mobile/src/lib/transcript-goal.test.ts`、`mobile/e2e/goal-completion.spec.ts`。
- 调度成功答复：主管正文发布前通过无工具语义抽取识别调度成功声明，每项显式区分肯定／否定、当前／历史／意图与助手声明／转述，仅当前肯定的本应用调度声明由代码核对计划状态、本轮归属和周期；明确指认外部执行方的周期观察正常发布，纠正答复时保留这类已核实发现；结构合法但没有具体 id、周期、cron 或时刻的提取项不进入匹配，不能单独使终稿失败；一次性 delay 与重复 every／cron 严格区分；完整引句保留否定和转述语境，否认、纯现状、人类禁挂起及已结束／取消历史不成为已武装声明。工具成功回执及 Scheduled 列表提供结构化 kind、cadence_type、完整周期和精确 next_run_at；实际武装回执给出 armed_this_turn:true，纯报告明确 false 且不带下一次时刻，next_in_s 为重复 every_s。主管按这些事实复述，不把现有计划说成本轮新建或把会话等待说成独立任务；纠正提示包含最近当前行和本轮回执支撑的当前归属行。确认具体声明无依据时最多一次原轮主管纠正，纠正稿已移除声明则发布，仍不成立则保持空终稿并明确失败；未执行、失败、已取消或旧安排不能通过正文变成本轮新成功。`no_matching_schedule` 不把持续目标标成外部受阻，既有预算下允许自动续跑；普通目标续轮在同目标连续三次此类失败后以可区分的 goal_capped 原因暂停，持久记录跨重启保留；未封顶追求态的新普通人类回合可中断连续失败链，已封顶后的普通发送只回答且保留暂停、自动续轮计数和失败回执，仅 Start／`goal_resume`／`resume_goal` 显式恢复并重置预算。桌面/Web 的纠正请求与终态错误分行，终态不再显示正在核对。分类请求完全隔离 origin 冷却和 HTTP 隐式重连，仍沿用完整首字节／body idle 预算。请求失败或输出无效只按 5/10/20/30 秒候选退避；连续三次 unavailable、连续三次 invalid 或总尝试六次任一上限即持久化 deferred 并保留未发布正文，两种连续计数分别持久化、互相清零，总预算防止交替无限重试；不改变已成功计划行。原 turn、Registry、worker 保持，有可执行 goal／active 同对话 wake 时携真实工具回执继续下一段用户工作，无归属走 Continue／Stop 暂停，事件 `reason` 为 `classification_deferred`，`limit` 是已用轮数而不是主管回合上限；不重试旧候选、不把未核对当声明为假或消费纠正。Stop 取消校验，新的人类插话淘汰旧候选；重启不能把 deferred 放回关键路径或用其旧 transcript 覆盖新检查点；公开 handoff 检查点仅从副本剔除待校验正文及 Responses 原始文本，保留已接受历史、工具与思考；accepted 与公共发布原子提交，避免双发；终态事务清除私有全文并保留轻量回执。关联契约：`C-008`、`C-009`、`C-012`、`C-029`；实现／验证：`internal/engine/goal_schedule_claim_test.go`、`internal/store/goal_cap.go`、`internal/store/goal_cap_test.go`、`internal/engine/schedule_answer_meaning_test.go`、`internal/engine/schedule_answer_receipt_http_test.go`、`internal/engine/schedule_receipt_test.go`、`internal/engine/prompt_schedule_facts_test.go`、`frontend/src/components/app/transcript-schedule-claim.test.tsx`、`frontend/e2e/schedule-answer.spec.ts`、`internal/engine/schedule_answer.go`、`internal/engine/manager_answer_buffer.go`、`internal/engine/schedule_answer_pending.go`、`internal/engine/schedule_answer_restore.go`、`internal/engine/schedule_answer_http_test.go`、`internal/engine/schedule_answer_recovery_http_test.go`、`internal/engine/schedule_answer_restore_test.go`、`internal/store/schedule_answer_pending_test.go`、`internal/engine/schedule_answer_deferred_http_test.go`、`internal/store/schedule_answer_deferred_test.go`、`internal/provider/isolated_recovery_test.go`。
- 定时报告准入：真实 `report_schedule` 的回执区分“动作已提交、报告待校验”与已发布 findings；原 tool-call 身份及私有报告保存后返回 `report_pending:true`，不持全局调度锁等待模型，恢复已保存的回执不重做动作。每份报告独立核对，具体虚假调度声明只拒绝该报告，继续发布其他通过校验的 findings；后续终稿不通过也不撤回已发布发现。提交的调度动作保持有效，整轮仍仅一次主管纠正。只有校验通过才发布 `schedule_report`；私有保存失败的回执明确 action_committed／report_published，提交动作后尚未保存回执的崩溃窗口需先核对现状态。关联契约：`C-009`；实现／验证：`internal/engine/report_admission.go`、`internal/engine/report_admission_test.go`、`internal/engine/schedule_report_isolation_http_test.go`、`internal/engine/schedule_answer_restore_test.go`。

- PC 恢复提示：人工恢复目标后，紧接启动的新轮次只显示一条继续提示；新轮次尚未启动时保留恢复提示。迟到的类型化复盘反馈保持既有可见性与原顺序，不中断同次恢复配对；普通提示、输入、实质 manager 活动与普通错误仍中断配对。后续自动续轮、独立恢复及 manager `reopen_goal` 仍分别可见，不按提示文字去重。原始 `goal_resumed`、`goal_continued` 和区块保留，Trace 可逐事件查看。关联契约：`C-003`、`C-012`；实现／验证证据：`frontend/src/lib/goal-notice.ts`、`frontend/src/lib/goal-notice.test.ts`、`frontend/src/components/app/transcript.tsx`、`frontend/src/components/app/transcript-goal-notice.test.tsx`、`frontend/src/store/goal-notice-history.test.ts`、`frontend/e2e/goal-resume-notice.spec.ts`；来源：GitHub Issue #96。
- 巡检与 worker 生命周期隔离：定时触发或 Run now 的常规失败，包括无执行证据和有界重试后的模型错误，不取消仍归属有效持续目标或活动同对话 wake 的 worker；原 registry、ID 和正在进行的调用继续供后续轮次接回。有效目标不含 blocked、complete、capped、idle 或自动续轮预算耗尽；plan mode 中目标与 wake 均不能提供可执行归属；已消费 one-shot 不能单独充当归属。turn/run 保留 error、run unread，失败不自动 block goal 或立即开启新的目标轮次。无归属、人工 Stop、上下文预算耗尽及 worker watchdog 失败上限仍按原停止边界清理。持久化 `workers_retained` 在 error 前记录精确归属和 worker IDs，写入失败不得保留执行；重启恢复还需该回执、检查点及当前有效归属，不改变原失败终态。关联契约：`C-009`、`C-027`、`C-012`；实现证据：`internal/engine/worker_ownership.go`、`internal/engine/turn.go`、`internal/engine/restart_parked.go`；测试：`internal/engine/schedule_worker_ownership_test.go`、`internal/engine/worker_ownership_test.go`、`frontend/e2e/schedule-worker-ownership.spec.ts`。
- 人类等待边界：用户明确禁止挂起当前会话时，原始有效人话经只读策略模型提取为按来源事件 seq、事件身份和文本指纹缓存的状态；`schedule_wake` 与 thread `report_schedule(next_in_s)` 返回 `thread_wake_prohibited`，未完成策略提取返回 `thread_wake_policy_pending`。拒绝不更新等待行、不产生新的等待卡；已分类禁令的 Trace 保留 source_seq、source_text、source_quote；待分类拒绝保留已知的 `pending_seq` 和剩余 `pending_count`，同轮可重试并使用已持久化的分类，补齐后才允许挂起。压缩、重启及模型自写的唤醒正文不能解除，仅后来明确允许等待的人话可放行；不带 `next_in_s` 的报告（包括 `keep:false` 取消）继续可用。关联契约：`C-009`、`C-012`；实现／验证证据：`internal/engine/thread_wake_policy.go`、`internal/engine/thread_wake_policy_test.go`、`internal/store/wake_policy.go`、`internal/store/wake_policy_guard_test.go`、`frontend/e2e/human-wake-policy.spec.ts`。
- 手动取消等待：PC 删除／取消等待与配对手机 `cancel_wait` 首次取消 thread wake 时，等待行与目标会话的 `role:human`、`text=等待 ID` 的 `schedule_cancelled` 回执原子提交，直接形成持久等待禁令；已消费的 one-shot、run 仍执行或先前模型取消均不省略人类动作，保留原 turn／run 的真实结果。只有原 active／paused 等待的取消可按原边界续办目标，删除终态历史不启动新工作。普通“继续”不授权模型重新等待，后来明确允许的人话才可解除；重复旧 DELETE 按同目标、同 ID 本地 human 回执幂等，不覆盖后来许可。模型取消、`report_schedule(keep:false)` 及独立任务取消不产生此禁令。目标选择过期则拒绝，来源不同也只限制实际目标；拒绝 Trace 保留 `source_kind:schedule_cancelled`、来源 seq、等待 ID，不伪造引句。关联契约：`C-009`、`C-012`；实现／验证：`internal/server/schedule_cancel_authority_http_test.go`、`internal/engine/schedule_human_cancel_test.go`、`internal/store/schedule_cancel_human_test.go`、`internal/store/wake_policy_timeline_test.go`、`frontend/e2e/human-wake-policy.spec.ts`；协议模型：`models/scheduling/ScheduleAuthorization.tla`、`models/scheduling/WakeConsentTimeline.tla`，各自边界与反例映射见 `docs/TESTING.md`。

- 目的：持续目标及未来时间继续执行；使用者：PC 用户；入口：`/goal`、Scheduled 页、`schedule_wake`；输入：目标和时间；输出：状态、唤醒、运行结果；进行中或已暂停的任务可在编辑里改下次落点（独立结果会话，或某个未归档会话，列表可搜索），已完成的不能；已认领的那一轮留在原会话，安静归档看认领时的类型；打开会话时右侧只列出指向该会话的 active/paused 唤醒，以及某次运行创建了该会话的来源任务（终态来源仍保留），点一条打开该任务；完成/取消的普通唤醒移出右栏，记录仍在定时任务的已完成列表，无关联项时隐藏该区；暂停/完成/取消行按真实状态显示，不把历史触发时间标为即将运行；有下一次已安排唤醒时，成功轮次中的未完成 sub-agent 保持原 ID 继续运行，下次轮次可接回；取消或暂停最后一次唤醒会停止无人接手的 worker；工具选择保持 `auto`，定时检查可直接返回非空文本并记为 findings，不要求调用工具或报告；仅空白终答且没有真实工具结果或报告时原轮继续一次，仍无输出显示错误，显式 quiet 报告保持优先；文本结束本次检查不替代长期目标完成或外部工作核验；周期检查的无计费重放内容按多语言文本和消息结构估算，在达到阈值时先自动压缩；若仍因上下文越界而终止，对话写出稳定原因并暂停该检查，用户调整后可恢复；进程重启后，若最近一次派出的 sub-agent 已因进程停止而失败，且会话停在未来的 thread wake 上，启动把这次唤醒提前到现在，并在 resume 这些 id 或新 spawn 之前拒绝 `schedule_wake` 和 `report_schedule` 的 `next_in_s`；前置条件：引擎运行；失败表现：跳过、失败或等待状态；关联契约：`C-002`, `C-003`, `C-009`；实现证据：`internal/engine/goal_session.go`, `internal/engine/autocompact.go`, `internal/engine/iterations.go`, `internal/engine/schedule_completion.go`, `internal/engine/schedule_completion_http_test.go`, `internal/engine/schedule.go`, `internal/engine/schedule_dest.go`, `internal/engine/schedule_context_overflow_test.go`, `internal/engine/context_overflow_schedule_test.go`, `internal/store/schedule_thread.go`, `frontend/e2e/schedules.spec.ts`, `frontend/src/components/app/conversation-schedules.tsx`。

- 目标编辑：历史 fire 类型冻结、目标与状态编辑及活动上限原子提交；事务内先复查实际 active／paused，拒绝快照读取后进入 done／cancelled 的行，不能部分冻结历史或改投；已领取旧 wake 保持归属，移走最后 wake 后恢复原目标或收尾 worker。证据：`internal/engine/schedule_retarget_test.go`、`internal/store/schedule_retarget_test.go`、`internal/engine/schedule_retarget_cancel_race_test.go`、`internal/store/schedule_retarget_terminal_test.go`。

- 完成范围：定时轮次自己的 manager thread wake 仍有后续触发，或本轮新排了这类等待时，`complete_goal` 返回可恢复冲突；完成该等待后才能完成长期目标。同轮先完成目标再重新排等待也返回冲突；人工和独立任务的等待保持独立。证据：`internal/engine/goal_schedule_completion_test.go`, `frontend/e2e/goal-schedule.spec.ts`；关联契约：`C-009`, `C-012`。
- 续轮模型请求遵循 `F-130` 的共享服务退避（`C-029`），新 turn 不重置故障档位。
- 续轮 worker 身份和状态沿用 `F-130` 的历史状态快照，不把历史总数当成本轮创建量。关联契约：`C-019`；证据：`internal/engine/resume_workers.go`、`internal/engine/worker_roster_test.go`、`frontend/e2e/worker-roster-history.spec.ts`。

- PC 压缩提示：成功压缩显示为次要状态，保留可用 token 数；文字按钮原地展开／收起完整简报。压缩失败与其他通知继续显示原卡片，压缩不删除或隐藏原回答。关联契约：`C-008`、`C-022`；实现／验证：`frontend/src/components/app/compact-notice.tsx`、`frontend/src/components/app/compact-notice.test.tsx`、`frontend/src/lib/transcript-goal.test.ts`、`frontend/e2e/conversation.spec.ts`；来源：Issue #113。

### F-160 Trace and diagnostics

- 逐次生成中断与恢复回执见 `F-130`／`C-035`；原 turn 的 Full log 保留输出身份及原因。证据：`internal/engine/generation.go`、`internal/engine/generation_http_test.go`。

- 目的：用 turn id 还原一次运行；使用者：开发者和用户；入口：`zwai trace`, `/api/trace/:turn`, Trace 面板；输入：turn id；输出：事件、模型和工具记录，桌面/手机公共错误保留模型 HTTP 状态及具体接口/解码原因，同 ID 可查看原始调用和重试；前置条件：该 turn 已记录；失败表现：找不到记录；关联契约：`C-012`；实现证据：`internal/engine/runerr.go`, `internal/engine/api_error_integration_test.go`, `internal/server`, `internal/store`, `frontend/src/components/app`, `mobile/e2e/api-error.spec.ts`。
- 普通回合在答复核对暂缓后停下来等人决定，事件仍是 `max_iterations`，但 `reason` 为 `classification_deferred`：`limit` 是已经用掉的主管轮数，`extend_by` 是当前配置的下一片段，不能写成已经到达主管工具回合上限。有可执行目标或同对话 wake 时仍自动继续。关联 `F-130`、`C-009`；证据：`internal/engine/schedule_answer_deferred.go`、`internal/engine/schedule_answer_deferred_test.go`。
- 调度答复校验以原 turn 的 `model_backoff.scope:schedule_answer` 显示候选退避或 `classification_deferred`，与 origin 服务冷却明确区分；invalid 的封闭 detail 区分具体校验失败，deferred 的 detail 区分 unavailable／invalid／attempts 上限，桌面和手机“重试说明”显示对应原因，旧事件无 detail 保留原通用文案；暂缓不表示声明已证伪或会重试旧候选；分类调用、提供商错误及真实声明纠正归属同一 Trace；无匹配声明的终态错误保持原 code，连续失败停止自动续跑时以独立 goal_capped 原因和 attempt／cap 留痕；待校验候选载荷只在私有存储，不作为公共事件、历史或 Trace 内容曝光；原始工具参数仍沿用既有诊断链。见 `F-150`／`C-009`／`C-012`／`C-029`；实现／验证：`internal/engine/schedule_answer_pending.go`、`internal/engine/schedule_answer_recovery_http_test.go`、`internal/engine/schedule_answer_timeout_test.go`。
- Worker 创建核验：按真实 turn 的调用、拒绝回执和生命周期事件区分新建、续办／恢复、完成及清理，不从历史名册或模型自述推断本轮创建量。证据：`internal/engine/worker_lifecycle_facts.go`、`internal/engine/worker_lifecycle_facts_test.go`、`frontend/e2e/worker-roster-history.spec.ts`。

- 模型冷却以 `model_backoff.scope:origin`、不含端点的 cooldown_id 保留真实调用者与等待信息，不计作 LLM 请求；PC／手机聊天及历史每会话每代只显示一条有时长的等待，wait_ms:0 不另成行，Trace 保留各角色原始观察。wait_ms:0 表示探测尚未交出有效内容、等待余时未知；首段有效内容放行并发不代表故障档位已清零，最终结果规则沿用 `F-130`（`C-029`）。证据：`internal/engine/model_backoff.go`、`internal/engine/model_backoff_test.go`、`frontend/src/lib/transcript-backoff.test.ts`、`mobile/src/lib/transcript-backoff.test.tsx`、`mobile/e2e/api-error.spec.ts`。
- 桌面／Web 等待提示支持展开“重试说明”：显示主管、真实子代理角色／ID 或标题／压缩／记忆／调度核对等实际等待步骤，以及已记录次数／核对诊断；共享冷却同代调用者合并在一条说明中，不将等待等同于步骤失败。关联契约：`C-029`；证据：`frontend/src/components/app/backoff-notice.tsx`、`frontend/src/components/app/backoff-notice.test.tsx`、`frontend/e2e/api-error.spec.ts`。
- 模型流的错误或 EOF 在本次调用记录回调结束后才交给消费端；部分 usage、调用者及错误类别保持，消费端在保存期间关闭也只记录一次。关联契约：`C-012`；证据：`internal/provider/provider.go`、`internal/provider/stream_terminal_order_test.go`。

### F-170 Settings and providers

- YAML 或 Settings API 可通过 `swarm.default_reasoning_effort` 为未显式选择档位的模型调用设置 `none`／`low`／`medium`／`high`，默认空保留原调用行为。显式会话／调用档位优先。省略或 `null` 保存保留已存值，空字符串清除，非法类型拒绝且不写配置。关联 `C-009`。实现与验证见 `internal/config/config.go`、`internal/server/settings.go`、`internal/provider/provider.go`、`internal/provider/worker_model.go`、`internal/provider/default_reasoning_test.go`、`internal/config/default_reasoning_effort_test.go`、`internal/server/default_reasoning_effort_settings_test.go`、`internal/engine/default_reasoning_http_test.go`。

- Settings → Tools 提供联网工具运行位置 `auto`／`local`／`cloud`，默认自动。强制云端时不展示本机代理字段，已保存的代理地址保留；自动或本机时仍可编辑。旧客户端保存其他工具字段且省略／null 该字段时保留显式选择。PC 会话可独立覆盖而不改全局，设置和会话变更从下一回合生效；turn 快照使重启／parked 恢复保持本回合选择。手机默认继承不受手机登录影响，PC 云工具使用自己的 Gateway 授权。Pairlink 元数据仅提供 PC 默认路由与本机回执的离线快照，不宣称实时 grant，不暴露凭据、账号标识或服务器地址。关联 `C-006`、`C-009`；证据：`internal/config/web_execution.go`、`internal/server/settings_web_execution_test.go`、`internal/remote/web_execution_test.go`、`internal/remote/web_execution_status_test.go`、`internal/tools/cloud_web_status.go`、`internal/store/web_execution_test.go`、`frontend/src/components/app/settings-tools.tsx`、`frontend/e2e/web-execution.spec.ts`。

- Chat Completions 推理字段兼容：PC 模型调用与手机直连均接收旧 `reasoning_content` 和新版 vLLM `reasoning`，覆盖 JSON message 与 SSE delta；非空旧字段优先，旧字段空或 null 时回退到新字段，不重复拼接。无需字段名配置；PC 思考沿现有流、历史与 Trace 展示，手机直连保留思考及本地历史。关联契约：`C-009`；入口：Settings → Models、手机 Models／Chat；实现证据：`internal/provider/provider.go`、`go.mod`、`mobile/src/lib/openai-wire.ts`；验证：`internal/provider/reasoning_fields_test.go`、`frontend/e2e/reasoning-fields.spec.ts`。
- 已配对手机在 PC 收件箱 **Menu → PC timeouts／电脑超时** 读取并编辑该 PC 的 worker、各提供商首字节和流空闲时限，加载失败、保存失败和旧主机均在面板内提示并可重试。仅提交改动字段，不接收密钥或端点，不覆盖无关设置；首字节与流空闲预算独立。新 worker 默认一小时，已有正值保持，保存不延长运行中的 deadline、不重置 watchdog 次数或恢复暂停任务。使用者：已配对手机用户；前置条件：PC 在线且支持相应 RPC；关联契约：`C-006`、`C-009`、`C-019`、`C-030`；入口说明：`mobile/README.md`、`docs/CONFIG.md`；实现／验证：`internal/config/timeouts.go`、`internal/config/timeout_contract_test.go`、`internal/remote/timeouts.go`、`internal/remote/timeouts_test.go`、`internal/remote/timeout_adapter_test.go`、`mobile/src/components/pc-timeout-sheet.tsx`、`mobile/src/lib/use-pc-timeouts.ts`、`mobile/e2e/pc-timeouts.spec.ts`。
- 桌面 Settings 自动保存和刷新模型目录保留手机并发修改的超时；只有实际编辑的超时才提交替换，重开设置读取最新值。关联契约：`C-009`；实现／测试：`frontend/src/lib/settings-persist.ts`、`frontend/src/store/app.ts`、`frontend/src/lib/settings-persist-delta.test.ts`、`frontend/e2e/settings-timeout-concurrency.spec.ts`、`internal/server/timeout_settings_test.go`。
- 目的：配置模型、外观、工具和远程连接；使用者：PC 用户；入口：Settings；输入：配置字段，包括每个提供商在输入框模型列表里的开关；输出：持久化设置和可用模型，关掉的提供商不出现在输入框和已配对手机的目录里，已经选中它的会话继续用；前置条件：本机数据目录；失败表现：校验或连接错误；关联契约：`C-006`, `C-009`, `C-012`；实现证据：`internal/config`, `internal/provider`, `frontend/src`。
- 首字节等待：设置 → 模型的每个提供商可单独设置「首字节等待时间（秒）」，默认 30 秒，与流开始后的请求空闲超时独立。首字节错误的「配置首字节等待时间」打开失败 turn 的提供商并滚动聚焦输入框，不跟随后来切换的模型；未知或已删除提供商只打开模型页。修改自动保存，关闭设置后用户可重新发送请求。实现证据：`internal/config/provider.go`, `internal/provider/http.go`, `frontend/src/components/app/model-settings.tsx`, `frontend/src/components/app/transcript-error.tsx`；验证：`internal/config/provider_timeout_test.go`, `internal/server/settings_timeout_test.go`, `frontend/src/components/app/transcript-error.test.tsx`, `frontend/e2e/api-error.spec.ts`。
- 输出额度用尽：桌面端主对话错误保留指向设置 → 集群「单次输出上限」的链接，并聚焦该输入。该错误属于最新一轮时，可按 `F-120` 直接重试，设置打开期间禁用。子代理详情中的同类错误复用该配置入口，不触发主对话重试，详见 `F-130`。手机不能改这项，只显示错误原文。实现证据：`frontend/src/components/app/transcript-error.tsx`, `frontend/src/components/app/agent-failure.tsx`, `frontend/src/lib/output-budget.ts`。
- 主管工具回合：`swarm.manager_max_iterations` 在设置 → 集群可编辑。桌面/Web 的暂停入口见 `F-130`，聚焦该输入。手机不能改这项。

- 设置 → 集群的「策略检查推理」控制排程声明与唤醒许可两个后台策略分类器的推理档位，默认“默认（低）”，可选跟随会话或显式低/中/高；默认档在端点拒绝推理参数时去掉该参数重试一次，显式档位保留原错误。设置 → 工具的「工具结果上限（字符）」限制每个工具结果进入模型上下文的长度，默认 40000，超出时保留首尾并说明省略量。关联契约：`C-009`；实现／测试：`internal/config/auxiliary_reasoning.go`、`internal/config/tool_limits.go`、`internal/engine/policy_effort_test.go`、`internal/tools/resultcap_test.go`、`frontend/e2e/settings.spec.ts`。
- 同源 provider 别名沿用 `F-130` 的共享冷却；保存设置不清故障档位（`C-029`）。证据：`internal/provider/backoff_integration_test.go`。

### F-171 Official server first: sign-in, welcome and account models

- LOCAL 切回第三方网关预置后，当前回合和 worker 的原始网关模型在权限仍有效时继续运行，刷新和重启恢复沿用原绑定；下一新回合使用第三方预置。来源撤权或退出登录后不继续调用。关联 `C-040`；证据：`internal/engine/model_mode_recovery_test.go`、`cmd/zwai/engine_mode_restart_test.go`、`internal/remote/account_local_sources_test.go`。

- 第三方功能目录只显示对话模型：`catalog_group:third_party` 仅提供服务端对话绑定（未显式指定时为 `conversation`），title／compact／translation 等仍用于辅助功能，不作为对话选项。PC 与配对手机共用投影，目录刷新失败保留缓存时规则不变；原始网关、自配模型目录维持按类型过滤。LOCAL 输入框选中这条固定绑定后仍可打开并改选自配模型或原始网关，不因该角色 `configurable` 为 false 而锁死选择器；网关模式在策略不允许改对话模型时继续隐藏。关联 `C-040`；证据：`internal/provider/account_catalog_group_test.go`、`internal/remote/account_conversation_catalog_test.go`、`frontend/src/components/app/model-picker.test.tsx`。
- 目的：默认体验以官方服务器为主，登录后即可使用其模型、手机连接和云端联网工具，无需手填端点或密钥；LOCAL 与普通可编辑 gateway 均支持自配 provider；第三方托管 gateway 只使用预置。默认登录与调试入口见 `F-173`。使用者：PC 用户；入口：首次启动的欢迎对话框、Settings → Models 顶部的官方服务器卡片、未配置横幅、输入框模型选择器的账号分组、`F-210` 的 Account 菜单；输入：Gateway 浏览器登录（沿用 `F-210` 的设备登录）和用户对欢迎的选择；输出：Go 以私有设备凭据调用服务器的桌面模型接口读取目录，账号模型作为仅存内存的临时 provider（id `gateway:<hex>`，`source:account`）排在自配服务之前，`/api/meta.account_models` 向界面提供状态、总数、类型计数、嵌入模型名和原因，不含凭据或端点；前置条件：服务器地址来自 `remote.hub_url`（Go 默认值，Settings 可改），前端不硬编码；失败表现：`pending`／`no_access`／`unavailable`（含 `reason`）分别在卡片和欢迎里说明，不暴露凭据或端点，不影响自配服务。关联契约：`C-040`、`C-009`、`C-006`、`C-013`；实现证据：`internal/config/default_source.go`、`internal/provider/account_source.go`、`internal/remote/account_models.go`、`internal/server/settings.go`、`frontend/src/lib/account-models.ts`、`frontend/src/components/app/welcome-gate.tsx`、`frontend/src/components/app/account-models-card.tsx`、`frontend/src/components/app/account-sign-in.tsx`、`models/account/DefaultSourceChoice.tla`；验证：`internal/config/default_source_test.go`、`internal/provider/account_source_test.go`、`internal/remote/account_models_test.go`、`internal/remote/host_account_models_test.go`、`internal/server/account_models_http_test.go`、`frontend/src/components/app/welcome-gate.test.tsx`、`frontend/src/components/app/account-models-card.test.tsx`、`frontend/src/components/app/configured-banner.test.tsx`、`frontend/e2e/account-welcome.spec.ts`。
- 首次启动欢迎：仅当没有可用模型（`configured=false`）、尚未回答过欢迎且不在 `restoring` 时出现。主操作是登录官方服务器，次要是「使用自己的模型服务」（保存回答后打开 Settings → Models）和「稍后再说」；两种回答都写入 `ui.welcome_dismissed`，刷新或重启不再弹出。登录成功或配置好任一模型服务会自动收起欢迎，无需回答。关掉对话框等同「稍后再说」，不会被困住。回答之后只要仍无可用模型，未配置横幅继续同时提供登录和设置自有模型两个入口。
- 第三方托管对话模型在桌面和配对手机显示网关配置的角色名称，实际请求仍使用固定角色 ID。关联 `C-040`；证据：`internal/remote/composer_model_test.go`。
- 对话模型：除上述第三方功能目录外，服务器或自配目录带 `model_type` 时，输入框和手机选择器只列出 `chat` 与未分类的旧模型；`embedding`、`ocr`、`decisions` 不进入可选对话模型目录，也不会被自动选为新会话默认模型。目录没有该字段时保持原列表，不按名称推断。已经选中被隐藏模型的对话保留原名；显式保存为账号默认的名称被隐藏时，选择器将它标为不可用，请求仍沿用该名称。账号目录仍保留全部分类：官方卡片用类型计数代替模型名列表，但不列出 `decisions`；嵌入模型选择器只列出 `embedding`；只有嵌入等非对话模型时账号仍为 `ready`，新会话回落到自配服务。
- 模型来源：账号目录在登录／注销后及每 10 分钟刷新；登录、重连、进入网关功能／设置入口及模型授权拒绝会重新协商能力。新对话优先使用当前 scope 的对话绑定，再按 `models.default_source` 选择来源。LOCAL 跟随账号时优先使用有对话权限的原始 Gateway 目录，其次使用可用的账号预置。账号没有可对话模型时回落到 `models.default`。可编辑模式的自配服务行可以将该服务设为当前 scope 的对话默认，功能模型中的「使用默认」恢复跟随默认来源。`models.account_model` 记住服务器上的默认对话模型。非空时未指定模型的会话跟随它；名称离开目录后仍显示为不可用，请求继续使用该名称。空值时新建会话记下当时的第一个可对话名称，目录随后重排不再改变这次选择；已有的空模型会话在下次发送时记下输入框上的名称。省略保存时保留原值，过长拒绝。使用旧版来源设置时，账号目录就绪后可展开「自己的模型服务」里的「服务器模型」查看可对话模型；目录给出上下文长度时显示在模型名旁，只读。点选写入该字段，用「默认 / 设为默认」选择新对话来源；自配行的「默认」只在来源是 `provider` 且该行是 `models.default` 时出现。官方卡片上的「新对话默认使用」写同一个来源字段，简介按类型计数。账号模型没有编辑、密钥或端点输入；注销或失去权限后，已选账号模型的会话得到稳定的 `account_login_required`，不会改走同名自配服务。
- 手机 Account 登录后等待同账号 PC，ready 才工作。手机 LOCAL 欢迎页点一次「登录」即启动浏览器授权，返回后在原页显示进度、审核或错误。手机 LOCAL 的个人及授权账号来源直连见 `F-230`。验证见 `mobile/e2e/gateway-account.spec.ts`。
- 同一条一 id 排障路径不变：账号模型的 turn 以 `gateway:` provider id 前缀标识来源，普通目录不新增事件类型，也不记录凭据。服务器下发官方档案时的名称、别名和工具名单见 `F-172`。

- 网关能力协商（`C-040`）：桌面与手机在 bootstrap 后以本设备私有 Bearer 请求同 origin 的 `capabilities_url`，识别版本 1／十角色协议 3。preset 和 gateway 来源分别授权；功能必须同时满足 supported、available 和客户端已实现端点，选择器另需角色 configurable，来源设置入口另需来源 configurable。失败保留选择、展示重新获取入口；API 关闭不注销账号或断开 PC。标题、压缩和翻译选择 Chat 模型；普通图片作为 conversation 的视觉输入，用户在图片预览中主动提取文字时使用 OCR 能力。图片生成尚无客户端适配器，设置行与其他功能使用同一选择器，并说明选择会保留到接入之后；LOCAL 的功能模型菜单按已授权的网关、第三方目录和本机 provider 分组，某一来源没有该功能的模型时不显示该组。第三方目录的分组标题使用网关返回的名称（bootstrap 的 `models.label`，经 `functional_catalogs.label` 与模型行 `provider_label`）；名称为空时才显示「第三方」。语音识别和语音朗读只列出对应目录里 `model_type` 为 `transcription`／`speech` 的名称，这些名称在 `model_policy.functional_catalogs`，不进入输入框目录，也不借用其他目录的对话模型。实现与回归用例：`internal/provider/capabilities.go`、`internal/provider/capabilities_test.go`、`internal/remote/account_capabilities.go`、`internal/remote/account_capabilities_test.go`、`frontend/src/lib/capabilities.ts`、`frontend/src/lib/capabilities.test.ts`、`mobile/src/lib/gateway-capabilities.ts`、`mobile/src/lib/gateway-capabilities.test.ts`、`internal/provider/model_policy_test.go`、`frontend/src/lib/functional-model-choices.test.ts`。 获授权的对话类角色优先使用来源发布的 Responses 端点，仅发布 Chat Completions 时走原协议。

### F-172 Official model profile

- 目的：服务器可以只给一个官方名称，由它按账号解析真实模型，并为本机回合附加指导、收窄工作区工具。使用者：已登录官方服务器的 PC 与手机用户；入口：Gateway bootstrap 的可选 `models.profile` 与 model_policy；输入：服务器名称、可选指导、工作区工具允许名单、上下文长度和配置权限；输出：输入框只显示该名称，发往 Gateway 的模型字段为协议别名 `account`，官方来源的回合在固定主管规则之后、人格和项目指令之前追加指导，并按允许名单与本机开关的交集挂载工作区工具；前置条件：档案通过现有 bootstrap 校验；失败表现：空名称、名称超过 64 个字符、指导超过 8 KiB、未知或无法识别的重复工具名、非正数上下文长度使账号进入 `unavailable` / `unsafe_response`，不发布半份目录。关联契约：`C-046`、`C-040`；关联功能：`F-171`、`F-230`。
- 没有 `profile` 的账号保持 `F-171` 的目录。档案只存在内存，不写入 `config.yaml`，不进入 `models.providers`。`label` 由服务器提供，客户端不写死品牌或模型名。客户端不请求、不保存、不转发对话模型 id。更换服务器上的真实模型不需要新的客户端，也不改已有会话。
- 托管账号不展示模型配置或选择，也没有 Official／All models 开关。旧 account_presentation 只保留兼容解析，不能解锁其他目录。普通 gateway 的功能默认与编辑权限由 model_policy 提供；桌面 LOCAL 忽略账号档案，手机 LOCAL 可显示服务端独立授权的账号模型来源。历史个人选择保留，托管期间执行预置，切回 LOCAL 后可恢复。
- 工具名单是 `read`、`write`、`edit`、`exec`、`web_search`、`web_fetch`、`python_runner`、`screenshot` 的子集。它只作用于官方来源，并与本机已有开关取交集。`spawn_agent`、提问、日程、目标和记忆保持挂载。名单存在时该回合不挂 MCP。缺省该字段表示不额外限制；空数组表示不挂工作区工具。`context_window` 缺省时沿用未知窗口。
- 配对手机在 PC 执行，遵循 PC 过滤后的目录和 model_policy，没有第二个展示开关或网关直连模型。退出清除内存档案；个人设置保留。
- `GET /api/meta` 的 `account_models.profile` 只含 `presentation`、是否有指导、以及在名单存在时的工具名。指导正文不进入 meta、设置、普通历史或单独事件。Trace 的 `account_profile` 记录名称、别名、工具名单和指导长度。实现证据：`internal/remote/account_profile.go`、`internal/provider/account_profile.go`、`internal/engine/prompt.go`、`internal/engine/history_tool.go`、`frontend/src/components/app/account-models-card.tsx`、`frontend/src/components/app/model-picker.tsx`、`mobile/src/lib/gateway-account.ts`、`mobile/src/components/provider-sheet.tsx`、`models/account/DefaultSourceChoice.tla`。验证：`internal/remote/account_profile_test.go`、`internal/provider/account_profile_test.go`、`internal/engine/account_profile_test.go`、`internal/server/account_models_http_test.go`、`frontend/src/components/app/account-models-card.test.tsx`、`frontend/e2e/official-profile.spec.ts`、`mobile/src/lib/gateway-account.test.ts`、`mobile/src/components/gateway-account-shell.test.tsx`、`mobile/src/components/provider-sheet.test.tsx`。

### F-173 FOFA install and local model controls

- 客户端区分 LOCAL 与 gateway，持久默认值仍为 fofa。默认必须登录且账号 ready；仅用户中心头像三击保留 LOCAL 调试入口，没有普通模式配置。桌面调试切换不要求进行中的回合或 worker 空闲：已打开的模型继续打原来的接口，下一次新解析使用新来源。已经登录时，桌面 LOCAL 保留登录，并按服务端授权返回可切换的第三方预置模型和原始网关模型；个人 provider 仍在列表中。未授权或空目录不显示该组，也不要求重新登录。保存的 gateway 选择继续用对应来源，切回 gateway 后恢复托管预置。普通对话请求等待响应时可以切换，已发出的请求使用原模型完成；语音、决策及旧版固定角色调用结束前仍会拒绝切换，语义索引回填不阻塞。备份、还原和重启仍拒绝。手机 LOCAL 同样分别聚合这两类授权目录。普通 gateway 可配置 provider 和功能模型，未覆盖项跟随服务端更新，显式覆盖保持，恢复默认删覆盖。`decisions` 只作后台调用，不出现在功能模型、输入框、账号类型计数或角色清单里，结果也不进入对话。托管第三方账号隐藏所有模型配置，只用预置。接口也拒绝越权，旧 full 偏好不能解锁。手机 gateway 的 PC 工作区需要同账号在线且就绪的 PC；没有 PC 时，PC 页保留连接引导，手机仍可用已授权的网关模型直接聊天。入口：用户中心头像三击、模型设置和网关 bootstrap；关联 `C-040`／`C-046`／`C-013`；实现证据：`internal/provider/model_policy_test.go`、`internal/remote/account_local_sources_test.go`、`internal/engine/account_model_test.go`、`internal/engine/model_policy_test.go`、`internal/server/settings_model_policy_test.go`、`frontend/src/lib/models.test.ts`、`frontend/src/components/app/model-picker.test.tsx`、`frontend/e2e/model-policy.spec.ts`、`frontend/e2e/product-mode.spec.ts`、`mobile/e2e/phone-account-mode.spec.ts`。
- 既有服务端角色保持兼容：title／compact 关闭时使用 conversation；embedding／decisions／transcription／speech 按开关执行。网关的 `ocr` 能力用于已发送图片预览中的主动文字提取。translation、naming 的专用路由按 F-181／C-049 和现有命名实现生效；LOCAL 忽略服务端角色。托管配置不可由客户端替换。

### F-175 Provider cloud built-in tools

- 目的：让支持 Responses 的 provider 在云端执行自带工具；使用者：PC 用户及 API 配置管理者；入口：Settings → Models 的 provider API 模式和工具 JSON、`PUT /api/settings`、`config.yaml`；输入：`api_mode` 和 `builtin_tools`，工具对象包含唯一非空 `type` 及服务商参数；输出：云端工具与本地函数工具共同供模型选择，流式回答含可点击来源，同 turn 的 `builtin_tool` Trace 回执；前置条件：端点、模型及所配置工具支持 Responses；失败表现：无效配置拒绝且保留草稿，服务端拒绝或流错误沿现有模型错误通道显示；关联契约：`C-030`、`C-009`、`C-012`；实现证据：`internal/config/provider_tools.go`、`internal/provider/responses.go`、`internal/engine/builtin_tools.go`、`frontend/src/components/app/model-settings.tsx`；验证：`internal/config/provider_tools_test.go`、`internal/server/settings_provider_tools_test.go`、`internal/engine/builtin_tools_test.go`、`frontend/e2e/builtin-tools.spec.ts`。
- 当前支持 `web_search`、`web_search_preview`、`file_search`、`code_interpreter`；参数原样保留，不另行上传文件或调用资源管理接口；自动容器等选项仍可在服务商执行时创建资源。默认仍为 Chat Completions 且工具为空，不能根据服务商名称自动开工具。PC provider 配置也作用于通过 PC 执行的配对手机会话；手机独立直连配置不在此功能范围。
- PC 主管通过已协商固定指令继承的网关目录调用原生 Responses 时，首轮用开头的 `developer` 消息提交固定指令。相同模型的历史只追加时，续接只上传新增输入。固定指令或历史变化、续接失效时以本地完整输入重建。关联契约 `C-030`，验证 `internal/engine/gateway_responses_continuation_test.go`。
- Responses 请求默认省略 `include`，并保留端点返回的加密推理以供后续回放。关联契约 `C-030`，验证 `internal/provider/responses_test.go`。

### F-176 External MCP tools

- 目的：接入用户配置的外部 MCP 工具并查看工具目录；使用者：PC Desktop/Web 用户；入口：Settings → Tools 的 MCP 标签、`/api/mcp` 管理／inspect API，以及对话工具记录和 Trace。输入框工具菜单的「工具设置」也打开这一页。
- 输入：显示名称、stdio command/args/env 或 Streamable HTTP/SSE URL/headers、启用状态和超时。设置表单不收集 ID，也不改写已输入文本；新建时分配唯一 ID，编辑保持原 ID。配置文件和 API 仍以该 ID 标识服务器。输出：持久化连接配置、脱敏字段、工具原名、描述／高亮的输入 Schema，以及实际调用结果。连接行用颜色分别标记启用状态和最近一次检查结果。前置条件：命令或远程服务可用，模型支持函数工具；本机命令使用会话工作区作为 cwd，保留应用访问权限。
- JSON 文本：Edit JSON 直接编辑与其他客户端相同的 `mcpServers` 文本，也可提交 `servers` 对象或数组。编辑时设置页右侧整栏是这份文档，并使用与工具 Schema 相同的 JSON 语法色，原文仍可直接修改。取消或应用后回到连接列表。表单仍用于单条连接。文本中的项新增或更新；未出现的连接及其凭据保持不变。省略 env／headers 保留，`{}` 清除。省略 `disabled_tools` 保留已保存的远端工具例外，显式数组替换。一行命令拆成程序和参数，不经过 shell。`envFile` 与 `cwd` 拒绝且不读取。非法文本不写入任何服务器。实现和验证：`internal/config/mcpjson.go`、`internal/config/mcpjson_test.go`、`internal/server/mcp.go`、`frontend/src/lib/mcp-settings.ts`、`frontend/src/components/app/mcp-json-editor.tsx`、`frontend/e2e/mcp-settings.spec.ts`。
- 配置读取不连接；显式 inspect 建立临时连接，禁用服务器也可检查，并把成功的目录写入缓存。Settings 在完成一次检查（含失败）之前不展示工具目录。`mcp.direct_load_percent` 缺省 5，Settings → Tools 可改，`0` 始终使用目录代理。回合准入不做外部 I/O。准入之后、第一次模型请求之前，按即将挂载的 MCP schema 占所选模型上下文窗口的比例决定：未超过则每个启用的远端工具是可直接调用的函数，调用时才连接；超过、窗口未知时改用 `swarm.auto_compact_tokens` 仍超过、或列目录失败，则保留带名字索引的 `list_tools`／`call_tool`。`list_tools` 可用 `query` 查名字和描述，精确 `name` 才返回完整 schema。缓存命中不再连接。Stop 取消尚未完成的列表。本回合的决定冻结；中途新出现的远端工具下一回合才进入工具表。Settings 拥有服务器 `enabled`。输入框「工具」写入 `disabled_tools`：直接函数从下一次请求撤下，渐进披露时 list 省略该名字，call 在连接前返回 `tool_switched_off` 且不执行，包括正在进行的回合；这不关闭服务器，也不使会话失效。本回合开始后才启用的服务器或单个远端工具，下一回合才挂上。已挂载的名字重新勾选后，下一次请求恢复。plan、Nuwa 只读和账号工具白名单不挂 MCP。Nuwa 主管不拿这些执行工具，`list_worker_tools` 看到本回合实际挂上的名字。恢复保持 checkpoint 里的原工具名。运行中的 MCP 工具确认旧会话在执行前失效时，用同一连接的最新保存凭据重连一次。Streamable HTTP 工具调用达到本地截止时间时，保留 `timeout` 回执和未知的远端结果，同一回合的后续独立调用仍可复用原会话。重连失败、连接关闭或其他阻断错误发生时，停止该任务的新模型与工具请求，保留失败原因并提示修复后重试。执行结果不明的调用不自动重放。连接／目录失败留下渐进代理并保留内置工具；每个 server/configuration 最多保留 8 个闲置 workspace session，不保证调用间保留。
- MCP 调用参数缺少必填字段或与目录中明确声明的字段／数组元素类型不匹配时，工具记录显示字段错误，模型可在当前回合重新提交修正参数。远端工具报告 HTTP 429 时，工具记录显示具体限流错误，模型可继续完成同一回合中可独立执行的工作。适配器不自动重放被限流的调用。
- 直接挂载和目录代理都在发送调用前检查工具 Schema 明确要求且无默认值的顶层字段。原工具记录显示缺少的字段，模型可在同一回合补齐，同一批其他工具仍可完成。证据为 `internal/engine/mcp_validation_http_test.go`、`frontend/e2e/mcp-progress.spec.ts`。关联 `C-041`。
- 直接挂载时模型调用远端工具本身。渐进披露的代理描述要求先按任务查询目录，不能凭服务器标题判断没有匹配，并遵循用户明确指定的路径。采用技能中保存的命令行或直连 API 备用路径时，以本轮目录或调用结果为依据，并遵守现有授权边界。仅凭过去的失败不能绕过。把未注册的名字，或渐进目录里的远端原名，当作函数调用时，模型收到 `tool_not_registered`，该调用不执行，同一条回复里的其他工具继续。该原名只属于一台渐进服务器时，回执带上对应的 `call_tool`。证据：`unknown_tool_test.go`、`internal/engine/unknown_tool_review_test.go`、`internal/engine/mcp_mount_test.go`。
- env/headers 值通过 HTTP 只写，读取仅返回 key；空编辑保留、`{}` 清除，普通 command/args/URL 可见。仅支持 tools，不含 resources、prompts 或 OAuth 登录。关联契约：`C-041`、`C-009`、`C-012`。
- 长调用可在折叠的工作区和展开的工具记录中查看最新远端进度。收到服务端通知前，Desktop/Web 显示等待提示。配对手机可在工具记录查看收到的进度。调用结束后显示最终结果。实现和验证：`internal/mcpclient/progress.go`、`internal/mcpclient/progress_test.go`、`frontend/e2e/mcp-progress.spec.ts`。
- 兼容服务端上报的结构化阶段耗时可从 engine log 追踪。调用结果与阶段记录用 token 摘要关联。实现和验证：`internal/mcpclient/progress.go`、`internal/mcpclient/diagnostics_test.go`。
- 实现证据：`internal/config/mcp.go`、`internal/config/mcp_test.go`、`internal/server/mcp.go`、`internal/server/mcp_test.go`、`internal/mcpclient/catalog.go`、`internal/mcpclient/direct.go`、`internal/mcpclient/catalog_test.go`、`internal/engine/mcp_mount.go`、`internal/engine/mcp_test.go`、`internal/engine/mcp_mount_test.go`、`frontend/src/store/mcp-settings.ts`、`frontend/src/components/app/settings-mcp.tsx`、`frontend/src/components/app/settings-mcp.test.tsx`、`tools/native_macos_mcp.py`、`tools/native_macos_mcp_startup.py`、`tools/test_native_macos_mcp.py`、`tools/test_native_macos_mcp_startup.py`。测试结果与原生验收需按最终候选单独记录；此功能条目不替代运行证据。来源：GitHub Issue #154。

### F-180 PC text quoting

- 目的：把选中的对话、子代理日志或集成终端原文与提问分开提交；使用者：PC 用户；入口：选中来源文本 → Add to chat；输入：选区和可选请求；输出：可编辑引用与带标签的消息；前置条件：选区在可引用来源内；失败表现：无有效选区时不显示操作；关联契约：`C-011`；实现证据：`frontend/src/components/app/selection-menu.tsx`, `frontend/src/components/app/terminal-session.tsx`, `frontend/src/lib/terminal.ts`, `frontend/src/lib/quote.ts`；验证：`frontend/src/components/app/selection-menu.test.tsx`, `frontend/src/components/app/terminal-session.test.tsx`, `frontend/src/lib/terminal.test.ts`, `frontend/e2e/terminal-quotes.spec.ts`。
- 终端引用在现有 `Quote.text` 中保留 `Source: integrated terminal`、客户端标签页 `Session`、源 `Conversation`／`Project`，以及 ready 帧已知的 `Initial directory`／`Shell`。加入当前输入框，保留原终端来源；正文仅包含用户实际选中的内容，命令须被选中才会带入，不推断执行命令、退出码或 `cd`／`ssh` 后的工作目录。引用沿用现有查看、编辑、删除、草稿保存与刷新恢复，发送仍使用 `<selected_text>`，用户请求单独使用 `<user_request>`。

### F-181 Selection reading and translation

- 目的：在 PC 和配对手机选区菜单中朗读已选文字，并在中文界面把英文选区译成简体中文。使用者：桌面、Web 和配对手机用户。入口：选中可引用来源中的文字后出现的菜单；输入：已经稳定的选区；输出：朗读，或仍留在菜单里的译文；前置条件：朗读需要语音输出已启用且就绪，翻译需要界面语言为中文、当前打开了会话，且选区判定为英文。网关来源必须协商到可用 `translation` 角色，使用该角色发布的 Chat Completions／Responses 协议和预置或显式模型；角色缺失、未知或不可用时隐藏入口并拒绝回退。独立 LOCAL provider 保持原有翻译能力。失败表现：朗读显示可重试错误，翻译在菜单内显示失败。两者都不加入引用，也不创建回合、事件或跟进。关联契约：`C-048`、`C-049`。实现证据：`frontend/src/components/app/selection-menu.tsx`、`frontend/src/lib/selection-language.ts`、`frontend/src/lib/speech-media.ts`、`internal/engine/selection_translation.go`、`internal/server/selection_translation.go`。验证：`frontend/src/components/app/selection-menu.test.tsx`、`frontend/src/lib/selection-language.test.ts`、`internal/engine/selection_translation_test.go`、`internal/server/selection_translation_http_test.go`、`internal/server/selection_translation_limit_test.go`。

### F-182 Generate images in chat

- PC desktop, Web, paired phone and phone-direct chat support a natural-language image request through `generate_image` or a text-only `/image` command. The selected account source needs an authorized image model and the `images` endpoint. A generation card shows pending, completed, stopped or failed state. A completed result supports preview, export and history. 入口：聊天输入框。关联契约：`C-051`。实现证据：`internal/engine/image_generation_http_test.go`、`frontend/e2e/generated-image.spec.ts`、`mobile/e2e/direct-image-generation.spec.ts`。

### F-190 Desktop updates

- 目的：在 Mac 桌面应用里自动发现并确认安装新版本；使用者：Mac 桌面用户；入口：启动、每六小时、到期后的恢复前台／联网检查，以及菜单 Check for updates；输入：本仓库最近一个含同架构 `zwai-<version>-darwin-<arch>.zip` 的 Release；输出：确认后替换当前应用，以同一数据目录和离线模式打开新实例，关闭已登记的旧窗口，解压后 Finder 显示与 Dock 一致的 ZWAI 图标；前置条件：运行的是带数字版本号的 `zwai.app`；失败表现：自动检查保持安静，手动检查与安装显示可重试错误，启动命令失败恢复旧应用；关联契约：`C-010`, `C-024`；实现证据：`internal/update`, `frontend/src/lib/desktop-update.ts`, `frontend/src/components/app/desktop-update.tsx`, `frontend/e2e/desktop-update.spec.ts`, `internal/desktop/pack`, `internal/desktop/bundle_darwin.go`。

- 更新安装以应用包为互斥范围，跨不同数据目录的引擎/进程返回 busy；启动失败保留并恢复旧包，延迟清理仅处理该次安装独有的备份。证据：`internal/update`, `internal/update/bundle_lock_test.go`；契约：`C-024`。
- 下载显示已接收字节；总量已知时显示百分比，未知时不伪造百分比，随后显示安装与重启阶段。检查和安装错误使用红色警示；安装失败保留已确认版本，重试直接安装该版本，检查失败重试检查。进度读取暂时失败不结束进行中的安装。入口：更新横幅、`GET /api/update/progress`；实现证据：`internal/update`, `internal/server/update.go`, `frontend/src/lib/desktop-update.ts`, `frontend/src/components/app/desktop-update.tsx`, `frontend/e2e/desktop-update.spec.ts`；契约：`C-024`。

### F-210 Pair a phone with a PC

- PC model choices refresh after acknowledged mode changes and account-policy changes without reconnecting the PC. Older PCs lacking Gateway capability metadata show an update notice. (`C-006`; `mobile/src/lib/model-source-refresh.test.ts`)

- 多 PC 共用同一 Hub 的手机物理长连接，各自保留独立加密会话、RPC 与任务视图；周期心跳及后台补连不会挤掉当前 PC。未变化的启动元数据不触发整页状态更新。关闭或超时一台 PC 只释放其逻辑会话。关联契约：`C-006`；实现与回归用例：`mobile/src/lib/hub-socket.ts`、`mobile/src/lib/client.ts`、`mobile/src/lib/multi-pc-link.test.ts`。
- 目的：安全绑定手机和 PC；使用者：手机用户；入口：手机登录同一账号后选择这台电脑；输入：账号授权和选中的在线 PC；输出：加密连接和设备列表；前置条件：hub 和 PC 在线；失败表现：网络、过期或 host offline；关联契约：`C-006`；实现证据：`internal/remote`, `mobile/src/components/gateway-account-screen.tsx`, `mobile/src/lib/link.ts`。Settings → Phone 不展示配对 QR。
- PC 的 hub 注册在受控后台生命周期内执行；网络迟缓只影响手机连接，不阻塞本地程序启动。Stop／Reload 可取消注册，旧结果不得重新启用已停止或已替换的 host。证据：`internal/remote/host.go`、`internal/remote/host_start.go`、`internal/remote/host_start_test.go`、`internal/app/startup_remote_test.go`。

- 管理型 Gateway 连接：Settings → Phone 展示账号状态和 Sign in。手机连接未启用且没有已绑定手机时，不展示已绑定列表；已有绑定仍可解除。手机连接未启用时不展示「其他」和保持唤醒；启用后才显示该开关，因为休眠断言只在连接启用时生效。不展示配对 QR、服务器地址，也不提供自建服务器、Hub、认证模式、本机名称或配对限额编辑；搜索同样不露出这些字段。手机登录同一账号后选择这台电脑。空 Hub 的官方默认地址由 Go 配置提供，前端不硬编码；已有自定义地址保留在配置里，设置页不显示也不改写。打开 Phone 不保存、不登录。`independent` 默认和匿名 QR 的服务端行为保持，改地址或认证模式走配置文件或 `PUT /api/settings`。明确登录先排空旧设置保存，再用 `enable_account:true` 启用账号模式，保留自配地址；回执只采纳远程草稿／baseline，不额外 PUT 回退 independent，短操作期间阻止编辑和离开。Go 校验后的公开登录链接可在使用者浏览器打开或复制，无主机浏览器及启动失败不阻断；终态隐藏链接。在浏览器核对公开确认码／PC 指纹和授权；待审／拒审／过期／取消／网络失败分别可见，管理员批准后仍须浏览器确认设备。授权与 PC WebSocket 上线分开。设置页不展示配对 QR；签发接口仍要求两者同时成立。新设备重连保留明确确认，不复活旧凭据或认领其他账号设备。Go 持有凭据和轮询 proof，前端只读本地安全状态；配置切换丢弃旧结果。关联 `C-006`；实现：`internal/config/remote.go`、`internal/config/remote_auth.go`、`internal/remote/login.go`、`internal/remote/login_wire.go`、`internal/server/remote.go`、`frontend/src/lib/use-remote-status.ts`、`frontend/src/components/app/settings-remote.tsx`、`frontend/src/components/app/settings-remote-login.tsx`、`frontend/src/components/app/settings-dialog.tsx`、`frontend/src/lib/settings-persist.ts`；验证：`internal/remote/account_login_flow_test.go`、`internal/remote/login_lifecycle_test.go`、`internal/server/remote_login_test.go`、`frontend/src/components/app/settings-remote-defaults.test.tsx`、`frontend/src/components/app/settings-remote-login.test.tsx`、`frontend/src/components/app/settings-dialog.test.tsx`、`frontend/e2e/phone-defaults.spec.ts`、`frontend/e2e/remote-account.spec.ts`。

- PC 账号入口：左侧 rail 最底部圆形 Account 按钮在列表收起后仍可见，保留原 Settings／应用菜单；匿名显示用户图标。已授权时，主机名为 `fofa.info`、`nosec.org`、`baimaohui.net` 或其子域名的合格 `avatar_url` 以 `no-referrer` 显示在账号按钮上；其它主机名、没有地址、地址不合格或图片加载失败时显示账号名称首字。账号菜单和 Phone 设置优先展示授权回执中的账号显示名，旧回执使用生成账号名的可读部分。单独展开菜单不登录、不改配置；明确选择 Sign in 才打开独立账号对话框并由 Go 保存 `enabled=true`／`auth_mode=account`、保留自配 Hub 后启动已有浏览器流程。对话框保留公开确认码／指纹、新设备确认、连接设置与匿名本地使用说明；待审、拒审、状态未知及已授权但 PC 离线分别呈现。Avatar 与 Phone 共用独立于 App transcript 的状态 owner、轮询与 mutation fence，未保存 Phone 草稿仍须匹配保存回显。关联 `C-006`；实现／验证：`frontend/src/components/app/desktop-account-menu.tsx`、`frontend/src/lib/remote-status-owner.ts`、`frontend/src/lib/avatar-url.ts`、`frontend/src/lib/avatar-url.test.ts`、`frontend/src/components/app/desktop-account-menu.test.tsx`、`frontend/src/lib/remote-status-owner.test.ts`、`internal/server/remote_account_test.go`、`frontend/e2e/desktop-account.spec.ts`。
- PC 设备注销：Account → Sign out 调用 `POST /api/remote/logout {}`，先显示 `signing_out`、停止自有连接并收口登录，再用 Go 私有设备凭据向当前 Gateway 确认撤销，全部确认后才清除本次精确 scope 的本地回执和候选 proof；不能用取消登录或切回 legacy 冒充注销。旧 Gateway `404`／`405` 显示 `logout_unsupported`，拒绝 `401`／`403` 显示 `logout_denied`，远端确认失败保留私有凭据供重试。仅注销这台 PC，保留设备身份、其他 origin／公钥、模型与本地对话；成功移除本机云工具授权，可明确重新登录。关联 `C-006`；实现／验证：`internal/remote/logout.go`、`internal/remote/logout_wire.go`、`internal/config/remote_logout.go`、`internal/server/remote_account.go`、`internal/remote/logout_wire_test.go`、`internal/config/remote_logout_tokens_test.go`、`internal/server/remote_account_test.go`、`frontend/e2e/desktop-account.spec.ts`。

### F-211 Mobile account login and owned devices

- 手机用户中心优先展示 Gateway 的账号与 PC 显示名，并在名称旁显示 `fofa.info`、`nosec.org`、`baimaohui.net` 或其子域名上的合格公开头像。旧 Gateway 的已知合成账号名回退到可读部分，PC 使用可读名称、主机名或未命名提示；保存连接后保留可读 PC 名称。头像地址省略或图片加载失败时保留名称首字，不留空白头像。Host ID、公钥、指纹和账号 ID 仍用于原有身份与重连逻辑。证据：`mobile/src/lib/gateway-names.ts`、`mobile/src/lib/avatar-url.ts`、`mobile/src/lib/gateway-account.test.ts`、`mobile/src/components/gateway-account-shell.test.tsx`、`mobile/e2e/gateway-account.spec.ts`；关联 `C-006`。

- 手机的联网状态与偏好集中在 LOCAL 用户中心，且只在账号已授权后显示。未登录、过期、待审和 Account 模式不展示这些控件。列表通过菜单进入，已打开的会话通过标题栏齿轮进入。关闭用户中心后回到原会话。证据见 `mobile/src/components/web-execution-settings.tsx`、`mobile/src/components/gateway-account-shell.test.tsx` 和 `mobile/e2e/cloud-web-tools.spec.ts`。

- 用户中心分别配置手机直连联网工具 `auto`／`cloud`／`off` 和 PC 会话 `inherit`／`local`／`cloud`，按 Gateway origin 与账号隔离，互不覆盖。手机直连自动模式仅在已批准、启用且未过期会话下声明云端 Search／Fetch，否则普通聊天；显式云端缺会话时在模型请求前报错，关闭时普通聊天。服务器或个人模型都必须支持标准 function calling；不静默换模型、去掉工具重试或本地降级。PC 默认继承其全局配置，手机登录不覆盖；显式偏好只写目标会话，从下一回合生效，PC 云工具用自身授权，不转发手机 Token。旧共享 `local` 读取为手机 `off` 与 PC `local`，旧 `cloud` 保留明确选择。设置摘要是下一回合策略，PC 状态是本机回执快照而非实时权限承诺。Gateway 401 仅清理同一当前会话；403 暂停新数据面但保持当前对话挂载，复核 approved 后恢复，pending／rejected 或复核失败才隔离，不假定待审；拒绝 headers 即释放未读 body，native stream 准备阶段也可取消；个人模型 401 不登出 Gateway；注销、换账号、改偏好和 Stop 取消自有请求。关联 `C-006`、`C-013`；证据：`mobile/src/lib/web-execution.ts`、`mobile/src/lib/web-execution.test.ts`、`mobile/src/lib/cloud-web-tools.ts`、`mobile/src/lib/openai-tool-wire.ts`、`mobile/src/lib/openai-client-web-tools.test.ts`、`mobile/src/lib/pc-web-execution.test.ts`、`mobile/src/lib/gateway-authority-account.test.tsx`、`mobile/src/lib/openai-client-authority.test.ts`、`mobile/src/lib/stream-body-cancel.test.ts`、`internal/remote/web_execution_test.go`、`internal/remote/web_execution_status_test.go`。

- 手机直连遇到可重试的云端抓取故障时，单次工具最多补试一次。明确的搜索或页面获取失败作为工具结果交回原模型，模型可使用已有资料继续回答。工具卡片保留失败状态，先前已经收到的正文和思考继续显示。关联 `C-006`。证据见 `mobile/e2e/cloud-web-tools.spec.ts`。

- 已匿名扫码的 PC 在手机与桌面同账号登录后自动接管：刷新按完整 Hub base endpoint、完整公钥和严格 owner ID 匹配，只更新已有匿名记录，保留顺序、名称、最后会话及当前选择；只重连已选 PC，先关旧连接，不批量连接账号 PC。其他 Hub／账号、缺公钥／owner 的旧服务和歧义身份不自动合并。服务端签发失败保留原记录；签发成功但本地保存失败时暂停旧票据并保留新票据供刷新重试；保存成功后传输失败正常重连受管票据。退出删除接管后的受管票据，不恢复已退休匿名票据。证据：`mobile/src/lib/gateway-link-upgrade.ts`、`mobile/src/lib/gateway-link-upgrade.test.ts`、`mobile/src/lib/use-phone-link-lifecycle.ts`、`mobile/src/app.test.tsx`、`mobile/e2e/gateway-account.spec.ts`；关联 `C-006`。

- 用户中心弹层的标题与关闭按钮保留设备顶部和左右安全区；独立审核页沿用页面安全区，底部内边距不重复增加。布局与实际关闭回归见 `mobile/e2e/gateway-account.spec.ts`，iOS 还需原生布局验收。

- 目的：不手工配置服务器即可登录、发现并连接本人电脑；入口：手机「登录」及菜单「用户中心」；输入：浏览器登录和显式设备授权、用户选择的在线 PC；输出：审核状态、本人 PC 列表、自动获取的连接票据和心跳。用户中心不提供其他服务器地址入口。待审／拒审只显示审核界面，普通 API 权限不决定设备连接权限。匿名扫码独立保留，账号模式选择 PC 无需扫码；保存并重连选中的 Host ID／公钥，不批量连接账号的所有 PC（唯一在线电脑的一次性自动连接见本节「登录优先」条目）。失败可刷新／重新登录，离线 PC 禁用连接，退出删除受管票据而保留匿名票据与自配模型。关联 `C-006`、`C-013`、`F-230`；实现证据：`mobile/src/lib/gateway-account.ts`、`mobile/src/lib/use-gateway-account.ts`、`mobile/src/components/gateway-account-shell.tsx`、`mobile/src/components/gateway-account-screen.tsx`、`mobile/e2e/gateway-account.spec.ts`。

- 登录优先：默认 Account 首页要求登录；登录后未连接 ready PC 时列出本人电脑并显示接入引导。扫码、粘贴配对链接和「自配模型」仅在头像三击切换的 LOCAL 调试模式下通过「其他方式」提供。登录刚完成时，若手机没有任何已保存连接或活动指纹，且账号恰有一台在线且启用的电脑，自动连接这一台一次：该标记只在内存，在失败、退出、重新登录、换服务器后清除，每次登录最多一次，之后重启或刷新不会再自动选；有多台、没有在线电脑或手机已有连接时不猜测，由用户选择。离线电脑仍禁用，自动连接失败回到可选列表。关联 `C-006`；证据：`mobile/src/lib/gateway-auto-connect.ts`、`mobile/src/lib/gateway-auto-connect.test.ts`、`mobile/src/lib/use-gateway-account-fresh-login.test.tsx`、`mobile/src/components/gateway-account-shell.tsx`、`mobile/src/components/account-pc-list.tsx`、`mobile/src/components/scan-screen.tsx`、`mobile/src/components/gateway-account-start.test.tsx`、`mobile/e2e/gateway-account.spec.ts`。

### F-220 Read and control PC conversations

- 手机打开「子 Agent」列表或从子智能体日志返回时从顶部开始，先显示运行中的分组；浏览列表不随聊天贴底。证据：`mobile/src/components/thread-screen.tsx`、`mobile/e2e/agent-roster.spec.ts`。

- 收件箱忽略旧版 PC 随列表返回的本地客户端任务，也不发起目录或详情 RPC。证据：`mobile/src/app.test.tsx`、`mobile/e2e/composer-layout.spec.ts`。关联 `C-006`。

- 配对及手机直连对话从三轮起显示可点击的轮次导航：以最多两行预览用户输入，随阅读位置高亮；宽度足够时让当前轮和上一轮完整可点，横滑浏览时保留导航位置，重新滚动正文或点选后恢复跟随。打开运行中的配对会话时，默认高亮正在执行的最后一轮；实时页没有该轮提问或旧 PC 缺少预览时，有限读取历史补齐。配对会话的跳转会按需跨页找到所选轮次的提问。「更早」定位到上一条主管正文，新版 PC 按需传输折叠的思考与工具。iOS 可右滑逐层返回，顶部「对话」与各 PC 标签共用横向滚动条。证据：`mobile/src/components/turn-navigation.tsx`、`mobile/src/components/host-chrome.tsx`、`mobile/e2e/conversation-open.spec.ts`、`internal/remote/log_compact_test.go`。关联 `C-006`、`C-015`。

- 进入应用后自动连接账号下全部启用的在线 PC，返回用户同样适用；保留当前在线选择，否则自动打开可用 PC。后台连接不轮流跳页，在线设备排在离线设备前，离线未配对设备仍可见。证据：`mobile/src/lib/use-auto-gateway-pcs.ts`、`mobile/src/lib/phone-pcs.ts`。
- 切换 PC 保留已建立的各设备连接，返回时复用存活连接；连接／引擎就绪阶段显示所选 PC，不闪回用户中心。仅当前 PC 更新可见会话，移除、撤权、凭据变化或页面退出释放对应连接。实现与待执行回归：`mobile/src/lib/use-phone-link-lifecycle.ts`、`mobile/src/components/pc-connection-gate.tsx`、`mobile/src/lib/use-phone-link-lifecycle.test.tsx`。
- 手机默认 Account，未登录显示用户中心。已登录时顶部「对话」使用手机登录直接请求网关授权模型。没有就绪 PC 时，PC 页仍提供等待与连接引导及直聊入口。PC 模型菜单由 PC model_policy 控制，托管隐藏，普通 gateway 可选择。头像三击切换 LOCAL 调试模式，保留登录及数据；有当前在线连接时先经 `set_product_mode` 同步切换该 PC 的安装级执行模式，收到精确 ACK 后才提交手机偏好并刷新页面目录。切换后留在用户中心，用户关闭后再显示新模式页面。PC LOCAL 输入框按「原始接口」和网关返回的第三方名称分别分组，同名模型保持来源 ID；PC 使用自身登录，手机凭据不跨 Pairlink。切回 Account 同步 PC `fofa`。busy、旧版不支持、持久化失败或连接变化时保留手机原模式并提示；超时可能已改变 PC，不宣称回滚，也不在重连或换 PC 时重放。没有在线 PC 时只切手机。LOCAL 恢复个人配置与匿名配对。关联 C-006／C-013；证据：`mobile/src/components/gateway-account-shell.tsx`、`mobile/src/components/phone-app.tsx`、`mobile/src/components/gateway-account-start.test.tsx`、`mobile/e2e/phone-account-mode.spec.ts`。

- 手机显示主管工具回合上限与延长轮数，提供「继续／停止」；继续延长原 turn，停止中断原 turn，不另发消息。有上限事件时说明到电脑的设置 → 集群 → 主管工具回合修改，手机不能编辑该项；已经继续后不再重复该说明，仅有 `awaiting_continue`、缺少上限事件的暂停不显示它。`classification_deferred` 显示已用轮数和仍生效的上限，不把已用轮数写成上限，也不指向这项设置。重开／重连通过 `awaiting_continue` 恢复操作，即使最新历史页不含上限事件；请求失败显示原因并保留提示，重复点击在确认期间禁用。手机和 PC 都需更新。关联 `C-006`、`C-009`；证据：`internal/remote/iteration_continue_test.go`、`mobile/src/lib/iteration-limit.test.ts`、`mobile/src/components/iteration-limit-card.test.tsx`、`mobile/e2e/iteration-limit.spec.ts`。

- 手机主管／子 Agent／直连聊天的纯思考组直接显示每条默认收起的「思考」控件，一次点击可查看原文，不再额外包装活动末行摘要；包含工具时保留过程聚合，展开后每条思考仍以独立的「思考」控件默认收起；用户再次展开才挂载完整原文，显式开合选择在同一条实时追加和完成后保持，关闭外层再打开则从收起开始。回答继续可见，工具保持既有独立折叠。子 Agent 详情与直连聊天复用同一日志。关联 `F-222`、`F-230`／`C-006`；证据：`mobile/src/components/thought.tsx`、`mobile/src/components/thread-blocks-reasoning.test.tsx`、`mobile/e2e/reasoning-fold.spec.ts`；来源：Issue #118、#129。

- 首屏一次 `watch include_detail` 同时取得详情与最新事件；旧 PC 自动补读详情。摘要与轮次在 SQLite 中有界读取，跨隐藏协调记录仍能找到原有可见摘要；重连替换旧订阅，Stop／Reload 收口自有链接。关联契约：`C-006`；实现：`internal/remote/watch_snapshot.go`、`internal/remote/host_links.go`、`internal/store/phone_reads.go`、`mobile/src/lib/open-thread.ts`；验证：`internal/remote/watch_open_test.go`、`internal/remote/host_links_test.go`、`internal/remote/snapshot_reads_test.go`、`mobile/src/lib/open-thread.test.ts`、`mobile/e2e/conversation-open.spec.ts`。
- 首次就绪后，主对话无可见项且还有历史时，自动沿 `log` 补到第一个可见项、历史耗尽或游标无法前进。最新 turn、实时序号和 seq=0 输出保持，Back、换会话／链接及新打开代次使旧回包失效。失败保留就绪对话并支持重试。关联 `C-006`；实现／验证：`mobile/src/lib/open-thread.ts`、`mobile/src/lib/open-thread.test.ts`、`mobile/src/app.test.tsx`、`mobile/e2e/conversation-open.spec.ts`；来源 Issue #121。
- Ask Other 使用与消息编辑器相同的 16px 聚焦字号；关闭 iOS 键盘、提交被拒绝及重试成功后保留窗口内的导航和内容。关联契约：`C-017`；实现：`mobile/src/components/ask-card.tsx`；验证：`mobile/e2e/ask-answer.spec.ts`, `mobile/e2e/ios-ask-viewport.swift`。
- 已绑定 PC 的收件箱菜单提供电脑超时入口；读取、编辑与保存的主定义见 `F-170`，不复用手机直连提供商设置。关联契约：`C-006`、`C-009`、`C-015`；实现／UI 回归：`mobile/src/components/pc-timeout-sheet.tsx`、`mobile/src/lib/use-pc-timeouts.ts`、`mobile/e2e/pc-timeouts.spec.ts`。
- 手机配对对话中的用户与回答代码围栏使用与桌面相同的 Code Block 和 Shiki 语法色，并跟随系统明暗主题；C++、汇编等别名映射到同一语法，未知或无语言标签保持纯文本。流式追加和复制保持代码原文。文件预览里的源码列表仍用原有词法色。直连聊天复用同一围栏，详见 `F-230`。关联契约：`C-032`；实现证据：`mobile/src/components/markdown.tsx`, `mobile/src/components/ui/code-block.tsx`, `frontend/src/lib/highlighter.tsx`；测试：`mobile/src/components/markdown-highlight.test.tsx`, `mobile/e2e/markdown-highlight.spec.ts`。
- 手机在 PC 对话或新建对话的模型选择器里主动刷新上游模型目录，显示进行中和可重试错误；成功后可选目录移除下线模型并加入新模型，但保留显式选择，不改写 PC 配置或默认模型。旧 PC 不支持时在选择器内提示更新。关联契约：`C-006`, `C-009`；实现证据：`internal/remote/catalog_refresh.go`, `internal/remote/pump_catalog.go`, `mobile/src/lib/use-model-catalog.ts`, `mobile/src/components/model-picker.tsx`；测试：`internal/remote/catalog_refresh_test.go`, `internal/remote/pump_catalog_test.go`, `mobile/src/lib/use-model-catalog.test.ts`, `mobile/src/components/model-picker.test.tsx`, `mobile/e2e/model-refresh.spec.ts`。
- 目的：在手机查看同一对话并发送、跟进、回答问题、插入及翻页；使用者：手机用户；入口：手机收件箱和对话页；输入：消息与操作，包括切换已绑定 PC、对 PC 的提问选择选项或填写 Other、把队列首条等待消息中断插入当前轮次；输出：切换 PC 后停留在该 PC 收件箱，进入对话后看到 PC 上同一对话的事件，提问回答经 PC 确认后继续，窄屏长输入仍可直接提交；初始页无可见主对话项时自动有限补读历史，「更早」用于继续翻页；前置条件：已绑定且在线；失败表现：重连提示或 RPC 错误，提问提交被拒绝时显示原因并保留草稿重试；关联契约：`C-002`, `C-003`, `C-006`, `C-015`, `C-016`, `C-017`；实现证据：`internal/remote/log.go`, `internal/remote/log_test.go`, `mobile/src/app.tsx`, `mobile/src/app.test.tsx`, `mobile/src/components/thread-screen.tsx`, `mobile/src/components/thread-screen.test.tsx`, `mobile/src/components/ask-card.tsx`, `mobile/src/lib/ask.ts`, `mobile/src/lib/phone-turn.ts`, `mobile/src/lib/phone-turn.test.ts`, `mobile/e2e/ask-answer.spec.ts`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/composer-layout.spec.ts`。
- 手机在每个已结束轮次后显示完成事件的本地日期时间和分界线；定时输入隐藏、失败轮次及历史翻页不丢掉边界。实现证据：`mobile/src/lib/transcript.ts`, `mobile/src/components/thread-blocks.tsx`, `mobile/src/lib/transcript.test.ts`, `mobile/src/components/thread-blocks.test.tsx`, `mobile/e2e/walkthrough.spec.ts`。
- iOS 从左缘右滑，或从普通内容区中间滑到右缘附近，按当前可见详情／弹层逐层返回；横向滚动区保留自身手势，根页面保持打开。两种手势共用页面返回处理器，不依赖浏览器历史。证据：`mobile/ios/App/App/BridgeViewController.swift`, `mobile/src/lib/ios-content-back.ts`, `mobile/e2e/ios-wait-layout.swift`；关联契约：`C-015`。
- iOS 在页面导航前把 CapacitorCookies 与 CapacitorHttp 的静态开关写入启动脚本，不再为这两项调用同步 prompt。结束进程后重开与首次启动走同一条路径；脚本与已固定的 7.6.9 桥不一致时留在原生错误页，不进入无返回的白屏。关联契约：`C-045`；实现／验证：`mobile/ios/App/App/IOSBootstrap.swift`、`mobile/ios/App/App/BridgeViewController.swift`、`mobile/scripts/ios-bootstrap.test.ts`、`models/mobile/IOSStartupPrompt.tla`；来源：GitHub Issue #165。
- 手机在输入区右侧提供当前任务唯一的停止图标；有文字、附件或引用时并排显示跟进，支持插入的草稿也保留发送选项。提问时可回答或停止，定时等待仍可发送。停止沿用 PC 的 interrupt 操作并保留未发草稿。证据：`mobile/src/components/thread-stop.test.tsx`, `mobile/e2e/composer-layout.spec.ts`。
- 手机 PC 对话和新建对话的「+」菜单提供文件、`/goal`、`/plan` 和 `/image`，输入 `/` 可过滤同一命令列表。当前直连来源具备已授权生图能力时，直连聊天也提供 `/image`。已有草稿时，点选 `/plan` 或 `/image` 将草稿作为命令参数保留。证据：`mobile/src/components/composer-slash.test.tsx`、`mobile/e2e/composer-commands.spec.ts`、`mobile/e2e/direct-image-generation.spec.ts`。关联 `F-150`、`F-182`、`C-009`、`C-051`。

- 配对手机和桌面/Web 的普通发送在进行中的轮次默认排队，附件随行保存。发送旁和指定队列行都能插入本轮或中断主管当前步骤并插入。队列状态和模型输入回执由 PC 主机提供。关联 `C-002`。证据：`mobile/e2e/walkthrough.spec.ts`、`frontend/e2e/conversation.spec.ts`、`internal/engine/followup_delivery_test.go`。

### F-221 Quote PC conversation text

- 手机选区操作使用明暗主题下均不透明的高对比面板、44px 触控按钮及关闭按钮；朗读和英文选区翻译复用 `F-181`，译文留在有界面板内，不自动引用或发送。实现：`mobile/src/components/selection-actions.tsx`、`internal/remote/selection_translation.go`；验证：`mobile/src/components/selection-actions.test.tsx`、`mobile/e2e/selection-actions.spec.ts`。

- 点击选区之外收起「加入对话」并释放旧选区；点击该操作仍将原文插入草稿，不发送。证据：`mobile/src/components/thread-screen.test.tsx`、`mobile/e2e/composer-layout.spec.ts`。
- 子 Agent 详情里的回答和已展开思考也显示选区操作：朗读、翻译不离开详情，「加入对话」回到主会话并追加草稿引用。证据：`mobile/src/components/thread-screen.test.tsx`、`mobile/e2e/selection-actions.spec.ts`、`mobile/e2e/ios-wait-layout.swift`。

- 目的：从手机对话选择原文并附在下一条消息；使用者：手机用户；入口：选中对话正文或已展开的思考原文 → 选区附近的 Add to chat／加入对话，系统复制／全选菜单仍可用；输入：选区及可选请求；输出：可查看、编辑、移除引用，发送或插入后显示独立的引用块；前置条件：打开 PC 对话，选区在用户消息、回答或已展开的思考原文中；失败表现：思考标题、工具控件和输入框等无效选区不显示操作，发送失败恢复草稿；关联契约：`C-011`, `C-017`；实现证据：`mobile/src/components/thread-screen.tsx`, `mobile/src/components/thought.tsx`, `mobile/src/components/composer.tsx`, `mobile/src/lib/quote.ts`, `mobile/src/components/thread-screen.test.tsx`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/composer-layout.spec.ts`, `mobile/e2e/selection-actions.spec.ts`。
- 引用与目标／计划命令同时存在时，按 `F-150` 的提交前拒绝与完整保稿边界处理，不通过丢弃引用激活命令。

### F-222 Inspect sub-agents on the phone

- 代理详情的生成中断说明沿用 `F-130`／`C-035`，配对推送与重开均保留原因和恢复状态。证据：`mobile/src/lib/transcript-generation.ts`、`mobile/e2e/generation-interruption.spec.ts`。

- 目的：在手机查看 PC 对话中的子 Agent 而不混入主 Agent 回答；使用者：已配对手机用户；入口：对话页输入框上方 → 子 Agent／Agents → 子 Agent 行；输入：Pairlink 已允许的事件、首屏 ready 的运行中 roster，及更早历史页；输出：只要有子 Agent 仍在运行，打开任务即显示入口和运行中数量，不需要先点「更早」；入口仅统计运行中的子 Agent，列表按进行中／已停止／已结束分组，保留完成与失败历史的角色、ID、状态、活动摘要及独立思考、工具、回答记录；页面返回、Android 系统返回或 iOS 右滑按详情→列表→对话逐级返回；列表在实时多段输出与历史分页后仍不显示主管正文或工作折叠；前置条件：PC 端有可回放的子 Agent 事件；失败表现：离线时沿用重连提示，未加载到启动事件前角色暂以 ID 显示；已结束的工人仍随「更早」出现；关联契约：`C-002`, `C-003`, `C-006`, `C-015`；实现证据：`mobile/src/lib/transcript.ts`, `mobile/src/lib/session.ts`, `mobile/src/components/thread-screen.tsx`, `mobile/src/components/thread-screen.test.tsx`, `mobile/src/lib/transcript.test.ts`, `mobile/src/lib/session.test.ts`, `mobile/e2e/walkthrough.spec.ts`, `mobile/src/components/agent-page.test.tsx`, `mobile/e2e/agent-roster.spec.ts`, `internal/remote/phone_roster.go`, `internal/remote/phone_roster_test.go`, `models/mobile/PhoneRoster.tla`。
- 人工停止沿用 `C-002`：清理与当前代理的取消回执都显示已停止，重新打开和更早历史分页不改成完成或失败；详情保留诊断原因但不显示失败样式。真实成功／失败与显式续办保持独立。证据：`mobile/src/lib/transcript-stop.test.ts`、`mobile/src/components/thread-stop-roster.test.tsx`、`mobile/e2e/worker-stop-replay.spec.ts`；来源：Issue #85。

### F-224 Browse and preview PC conversation files on the phone

- 配对手机默认浏览当前任务成果，可切到项目目录；加密文件分页和预览沿用同一个文件根。旧 PC 不提供切换入口。关联 `C-052`；证据：`internal/remote/files.go`、`mobile/e2e/file-preview-controls.spec.ts`。

- 目的：在已配对手机上查看 PC 对话的工作区产物；使用者：手机用户；入口：PC 对话页输入框上方 → Outputs／产出物；输入：工作区列表、文件选择与搜索；输出：可折叠文件树、按后缀渲染的 Markdown／源码高亮／纯文本／HTML 沙箱／CSV 表格／DOCX／XLSX／PPTX／EPUB／常见图片／PDF 预览及其他文件的下载入口；前置条件：PC 在线且该对话存在；失败表现：RPC 错误、文件过大或不支持的预览明确显示；关联契约：`C-004`, `C-006`, `C-015`, `C-020`；实现证据：`internal/remote/files.go`, `internal/remote/files_test.go`, `mobile/src/components/file-browser.tsx`, `mobile/src/lib/remote-files.ts`, `mobile/src/lib/remote-files.test.ts`, `mobile/e2e/walkthrough.spec.ts`, `mobile/e2e/ios-wait-layout.swift`。
- 手机产出物初次加载与刷新后只显示工作区根的全部直接子项；展开一个目录就读取该目录的全部直接子项。搜索返回每一个匹配，结果到达前不显示局部列表。读取失败不把未完成的分页当成完整结果。证据：`mobile/src/components/file-browser.tsx`, `mobile/src/components/file-browser.test.tsx`, `mobile/src/lib/remote-files.ts`, `mobile/e2e/walkthrough.spec.ts`。

- 手机文本产出物按后缀选择 Markdown 文档或现有源码语言高亮；未知文本与超过 256 Ki 字符的文本完整显示为纯文本。HTML 使用与桌面相同的沙箱预览并可切回源码，CSV/TSV 显示表格，DOCX、XLSX、PPTX 和 EPUB 解析包内容；SVG 与 MDX 组件不执行，原文件下载保持不变。证据：`mobile/src/components/text-file-preview.tsx`、`mobile/src/components/document-preview.tsx`、`mobile/src/components/document-preview.test.tsx`、`mobile/src/components/text-file-preview.test.tsx`、`mobile/src/components/file-browser.test.tsx`。

- 手机 EPUB 阅读只在顶部显示一次文件名。点击正文左侧或右侧翻一页，读到章节边界时继续相邻章节；点击中央显示或收起目录、字号和下载。浮层默认隐藏，展开时浮于正文上方，当前页位置保持不变。双指调整字号后，书页按新字号重排并尽量保留当前阅读文字。关联契约：`C-020`。证据：`mobile/src/components/file-browser.tsx`、`mobile/src/components/epub-preview.tsx`、`mobile/e2e/file-preview-controls.spec.ts`、`mobile/e2e/ios-epub-preview.swift`。

- 手机打开产出物后，按已校验的分块显示已接收字节、文件总大小与百分比。持续收到数据时等待窗口随之续期，暂时无数据时显示等待提示并继续读取。PNG、JPEG、GIF、WebP 在手机 32 MiB 读取上限内可直接预览。返回列表时取消当前读取。关联 `C-020`。见 `mobile/src/components/file-browser.tsx`、`mobile/src/lib/remote-files.ts`、`mobile/e2e/file-preview-controls.spec.ts`。

- 手机预览工具：图片提供放大／缩小、比例、100% 原始大小、适应宽度和适应窗口。默认按宽度等比适配，从图片顶部开始阅读，长图可向下滚动。适应窗口可查看整张图，两种模式均不放大小图。双指捏合以两指中点缩放图片，工具栏保持原尺寸，单指可横纵滚动。按钮缩放范围为 10%—400%，双指可从低于 10% 的适配比例平滑放大。加载或解码失败时禁用缩放。Markdown、源码、纯文本和 EPUB 共用双指字号调整、放大／缩小和重置。字号范围为 12—28 px，普通文本默认 14 px，标题、表格和代码同步缩放。EPUB 默认基准字号为 18 px，保留书内字号层级。工具栏及下载保留在内容滚动区外，重新打开恢复默认，下载保留原字节。关联契约：`C-020`；证据：`mobile/src/components/image-file-preview.tsx`, `mobile/src/components/image-file-preview.test.tsx`, `mobile/src/components/text-file-preview.test.tsx`, `mobile/e2e/file-preview-controls.spec.ts`。

- 手机预览加载中即可点击下载，等同一份传输完成后自动开始下载。返回列表取消等待。原生手机的下载打开系统保存／导出面板，选择位置后写出原文件；取消不显示成功，保存失败在预览中提示并允许重试。浏览器保留下载链接。证据：`mobile/src/components/file-download.tsx`, `mobile/src/lib/file-save.ts`, `mobile/src/components/file-browser.test.tsx`, `mobile/android/app/src/main/java/com/lubyruffy/zwai/FileSavePlugin.java`, `mobile/ios/App/App/FileSavePlugin.swift`；关联契约：`C-020`。

### F-225 Manage PC projects from the phone

- 目的：手机直接创建和配置已配对 PC 的项目；入口：首页菜单 → 项目管理、新建会话 → 项目管理；输入：名称、项目指令、PC 已存在的工作目录、记忆开关、女娲开关和规划／执行模型；输出：PC 中同一项目的持久化设置，新建后可直接用于手机新会话。
- 前置条件：PC 在线且支持项目 RPC；全局关闭记忆时禁用项目记忆开关；女娲使用 PC 就绪模型目录。失败表现：旧版 PC 提示升级、加载失败可重试、保存失败保留输入；恢复阶段拒绝写入。只发送编辑过的字段，切换 PC／关闭面板后忽略迟到回复，不把配置写入手机直连模型的 Chat。
- 关联契约：`C-006`、`C-026`；实现证据：`internal/remote/project_settings.go`、`internal/remote/project_settings_test.go`、`mobile/src/components/project-manager.tsx`、`mobile/src/components/project-manager.test.tsx`、`mobile/e2e/project-management.spec.ts`。

### F-230 Direct model chat

- 直连 Chat 的「关闭思考」随会话保存，下一请求使用该端点的关闭字段。模型仍报告思考时显示错误。关联 `C-009`；证据：`mobile/src/lib/openai-wire.test.ts`、`mobile/e2e/direct-chat-reasoning.spec.ts`。

- 手机直连 Chat 分别获取登录账号已授权的第三方预置和额外网关对话目录，按来源保留不同路由。网关模式直聊独立于 PC，LOCAL 还可使用个人模型。无权限或空目录不提供该来源，撤权不退出登录。历史账号直连记录按同账号来源隔离。LOCAL 语音默认使用手机账号获准的网关模型，也可按方向引用个人 provider 覆盖，凭据不进语音偏好。证据：`mobile/src/components/gateway-account-shell.tsx`、`mobile/src/lib/local-speech.test.ts`、`mobile/e2e/gateway-account.spec.ts`；关联 C-013／C-048。

- 新旧 Chat Completions 推理字段兼容沿用 `F-170`／`C-009`；通过直连的同一思考区块显示，字段名称不影响正文。证据：`mobile/src/lib/openai-wire.ts`、`mobile/e2e/direct-chat-reasoning.spec.ts`。
- 用户与流式回答复用 `F-220` 的手机 Markdown 代码围栏渲染及原文复制，遵守 `C-032`；证据：`mobile/src/components/direct-chat-screen.tsx`, `mobile/e2e/markdown-highlight.spec.ts`。
- 目的：手机在没有 PC 时直接连接模型，并在应用内按服务商选择模型；使用者：手机用户；入口：Models、Chat，或点开输入框后显示的当前模型名称和思考等级；输入：兼容端点、所选模型、消息及附件，Models 里可把某个自配接口从输入框列表里关掉，或经确认后移除；输出：流式回答，长模型名和长输入不挤出发送按钮，关掉的接口不出现在列表里，已经选中它的对话继续用；账号模型没有移除入口，移除只改本机自配存储，不删除已有直连对话；新对话在当前接口被移除且仍有其他接口时改用剩余接口，已打开并绑定被移除接口的对话保留原选择并显示来源不可用；前置条件：端点可访问；失败表现：连接或模型错误；关联契约：`C-002`, `C-006`, `C-009`, `C-013`, `C-015`, `C-017`；实现证据：`mobile/src/components/direct-chat-screen.tsx`, `mobile/src/components/provider-sheet.tsx`, `mobile/src/components/provider-sheet.test.tsx`, `mobile/src/components/model-picker.tsx`, `mobile/src/components/composer.test.tsx`, `mobile/e2e/composer-layout.spec.ts`, `mobile/src/lib/openai-client.ts`。
- 直连流式回复期间输入框主按钮显示可点击停止图标，取消后保留已收到的回复并恢复发送。证据：`mobile/src/components/direct-chat-screen.tsx`, `mobile/src/components/direct-chat-screen.test.tsx`。

### F-240 Phone updates

- 目的：发现并安装 Android 可用版本；使用者：Android 手机用户；入口：菜单 Check for updates 和更新提示；输入：最近一个含匹配 APK 的 GitHub Release；输出：Android 安装请求；前置条件：网络和新版本；失败表现：手动检查显示错误。有新版本时，更新提示固定在用户中心顶部，位于标题和账号内容之上，未登录的 Account 页同样可见，不随用户中心滚动；关掉一处提示后，被盖住的另一处同时收起。iOS 测试版由 TestFlight 分发和更新，应用内不显示 GitHub 检查入口；关联契约：`C-010`；实现证据：`mobile/src/components/host-chrome.tsx`, `mobile/src/components/update-notice.tsx`, `mobile/src/components/gateway-account-screen.tsx`, `mobile/src/lib/app-update.ts`, `mobile/e2e/version-check.spec.ts`, `mobile/ios/App/AppUITests/BindFlowTests.swift`。

### F-310 HTTP API and live events

- 缺失记录诊断：404 区分会话不存在、排队消息已离队和通用记录缺失；附请求 ID、方法、路由和可用的记录身份，使用同 ID 在默认 WARN 日志定位，避免裸存储哨兵。关联契约：`C-012`；证据：`internal/server/missing_record.go`、`internal/server/missing_record_test.go`。

- 目的：给 PC 客户端提供相同服务；使用者：内置客户端及本机调用方；入口：`/api`、SSE、WebSocket；输入：同源请求；输出：状态和事件；前置条件：引擎在线；失败表现：HTTP 状态和 code；关联契约：`C-002`, `C-003`, `C-005`；实现证据：`internal/server`, `docs/API.md`。

### F-320 CLI commands

- 目的：从 shell 启动、诊断和管理对话；使用者：终端用户；入口：`zwai` 子命令；输入：命令与参数；输出：终端文本或客户端；前置条件：可运行二进制；失败表现：命令错误；关联契约：`C-001`, `C-012`；实现证据：`cmd/zwai`, `docs/CLI.md`。

### F-330 Swarm library

- worker 收取状态：`Registry.PendingAgents` 非消费地列出运行中及已完成未收取的 worker，保留精确执行 `generation`；`wait_agents`／`close_agent` 的回执及 checkpoint 的 `Generation`、`Collected`、`CloseRequested` 绑定本次执行，查询与归档不得丢失待收取结果，resume 开始新 generation；缺 generation 身份的旧 finished roster 保持已结束兼容，旧 running 仍需收取。关联契约：`C-019`、`C-027`；证据：`settlement.go`、`tools.go`、`handoff.go`、`restore.go`、`settlement_http_test.go`。

- 逐次生成生命周期通知沿用 `F-130`／`C-035`；公开通知与输出身份见 `docs/LIBRARY.md`，验证：`generation_test.go`。

- worker lifetime 默认一小时，已有显式正值优先；live registry 用同步 `SetAgentTimeout`、`SetMaxTurns`、`SetMaxConcurrent` 更新后续执行，运行中 deadline 与三次 watchdog 恢复预算不变。关联契约：`C-009`、`C-019`；实现／验证：`swarm.go`、`worker.go`、`timeout_settings_test.go`、`worker_timeout_test.go`、`docs/LIBRARY.md`。
- 同时运行的子代理是一个任务的实时上限。桌面／Web 集群设置输入无效数字时显示至少为 1 的提示，修正为正整数后自动保存。保存后立刻作用到正在跑和已暂停的 swarm。调低时，后占到名额的子代理停掉，先占到的继续；停掉的要等有空位才能再续跑。已经排队的在有空位时放行。新的启动和续跑在满员时被拒绝。关联契约：`C-009`；实现／验证：`frontend/e2e/settings.spec.ts`、`internal/server/settings_http_test.go`、`gate.go`、`swarm.go`、`gate_test.go`、`models/swarm/ConcurrencyCap.tla`。
- 模型输出通知提供可选 `MessageID`，同一流的思考、正文与 checkpoint 保留身份，新模型轮次使用不同身份；无身份的旧通知保持兼容。关联契约：`C-003`；证据：`signal.go`、`signal_identity_test.go`、`docs/LIBRARY.md`。
- 进程交接 API 与恢复边界见 `F-110`、`C-027`；库使用说明：`docs/LIBRARY.md`；证据：`handoff.go`、`handoff_test.go`。

- 目的：让 Go 程序嵌入 manager 与 sub-agent；使用者：Go 开发者；入口：模块公开 API；输入：registry、provider 和任务；输出：结果与通知；前置条件：依赖可编译；失败表现：Go 错误或结构化工具错误；关联契约：`C-003`；实现证据：`swarm.go`, `run.go`, `docs/LIBRARY.md`, `examples`。

### F-410 Local conversation storage and search

- 原始项目历史提炼：入口：项目菜单“提炼项目历史”，在所选项目新建明确标题的报告会话并保留原草稿；既有会话也可自然语言请求并自主调用 `query_history`，此时只把读取结果当线索并给出普通回答，host 渲染的原文证据报告仅由菜单入口的显式请求产生。manager／worker 自选 SELECT／WITH，空 SQL 发现 Schema；项目视图自动绑定所属项目，独立会话保留跨项目视图，Memory 关闭及计划模式仍可读。该入口不改变 Full access 的系统权限。关联：`C-036`、`C-008`；实现证据：`frontend/src/App.tsx`、`internal/engine/history_tool.go`、`internal/history/scope.go`；验证：`internal/server/history_report_request_test.go`、`internal/engine/history_tool_http_test.go`、`internal/engine/history_worker_http_test.go`、`frontend/e2e/project-history.spec.ts`。

- 历史原文与建议：显式报告或实际历史读取后，host 依据规范 message／event 引用核验原文及真实归属，完整展示正文、来源、日期和定位；建议与问题另列，计数／区块由 host 生成。原文变化／缺失／越项目／超限时阻断旧答案，复用持久门禁纠正与恢复；建议本身是否成立仍需内容验收，不把原文展示当作语义通过。不会因读取历史而自动保存人工 summary 或写入 Memory。关联：`C-036`；实现：`internal/engine/history_report.go`、`internal/engine/history_answer.go`、`internal/store/history_evidence.go`；验证：`internal/engine/history_report_test.go`、`internal/engine/history_answer_http_test.go`、`internal/engine/history_answer_schedule_test.go`、`internal/engine/history_answer_recovery_http_test.go`、`tools/project_history_eval.py`。

- 嵌入模型统一在 Settings → Models → 功能模型配置，用于会话语义搜索：未配置时关闭，配置后自动开启，关键词搜索始终可用。清空模型或选择“未配置（关闭）”可覆盖网关默认和旧搜索配置；恢复默认则继续跟随当前网关预置。旧 `search.embedding=false` 不再覆盖已填写模型。图片生成尚无客户端适配器，设置行使用同一选择器并保留已选模型；翻译缺少可编辑权限时保留说明行。LOCAL 功能绑定可以指向已登录的 `gateway:` 目录，退出登录后保留该绑定并按登录要求失败，不改走同名个人 provider。证据：`frontend/src/components/app/functional-models.tsx`、`internal/config/search.go`、`internal/server/search_model_configuration_test.go`、`frontend/e2e/model-policy.spec.ts`。

- PC `⌘K` 同时筛选操作和检索会话；会话命中保留服务端排序，语义命中不要求包含原词。请求中、无匹配与请求失败分别显示状态；失败时仅回退到匹配的本地会话，旧查询／关闭面板后的响应不得覆盖当前结果。关键词检索覆盖标题、目标与用户／助手正文，不包含工具日志或归档会话。证据：`frontend/src/components/app/palette.tsx`、`frontend/src/components/app/palette.test.tsx`、`frontend/e2e/palette-search.spec.ts`、`internal/store/search.go`。

- 会话摘要与成功记忆复核从完整持久历史获得当前 worker ID、generation、生命周期和独立收取状态；早期完成事件即使落在正文尾窗外仍有身份事实，同 ID 续办不沿用旧代完成。快照整行限额并明确省略数量，不自动改写旧摘要或笔记。关联 `C-008`、`C-012`；证据：`internal/engine/worker_lifecycle_identity_test.go`。

- 运行时纠偏来源：同轮 ADK 和持久化消息显式保留来源，下一轮回放、compact 与 session briefing 排除 runtime 指令；旧行仅按完整常量及合法 worker-ID 后缀兼容，真实人类引用保留。关联契约：`C-008`；实现／验证：`internal/engine/runtime_cues.go`、`internal/store/message_source.go`、`internal/engine/ordinary_worker_completion_test.go`。

- 目的：持久保存对话、事件与检索索引；使用者：PC 用户；入口：对话列表和搜索；输入：事件与查询；输出：可恢复历史和结果；前置条件：数据目录可写；失败表现：存储或索引错误；关联契约：`C-003`, `C-008`；实现证据：`internal/store`, `internal/search`, `docs/DATA_MODEL.md`。

### F-420 Configuration and project memory

- 设置 → 记忆支持「核验并修正项目技能」。成功会话读过现有技能并产生完整的后续工具证据时，可自动核对其历史补丁。适用的回退进入原回合 Trace，后续会话可读取修订版。默认开启，显式关闭后保留设置。关联 `C-025`；实现证据：`internal/engine/dream.go`、`internal/engine/dream_discovery.go`；验证证据：`internal/engine/dream_http_test.go`。

- 用户触发的“整理记忆”支持原笔记升为技能、无步骤短技能降为笔记；完整保留源文字、唯一匹配、预算与目标冲突校验后先存目标再删源，部分失败记录已保存内容，重试按已有最终状态幂等。自动复盘／worker／共享库不获搬家权限。结果显示升降类计数与流式动作，有既有 turn 时动作和陈旧名单进入同 turn Trace。失败摘要按已提交笔记及完整搬家回执判断保存结果，不将仅创建技能的半完成提升当作笔记已保存。入口：项目 Memory 整理按钮、既有 tidy-skills API；关联契约：`C-025`、`C-012`；实现证据：`internal/memory/reclassify.go`、`internal/engine/review_tidy.go`；测试证据：`internal/engine/review_reclassify_test.go`、`internal/server/memory_reclassify_test.go`、`frontend/e2e/memory-boundary.spec.ts`、`frontend/e2e/memory-tidy-partial.spec.ts`。

- 技能查看统计：manager/worker 的成功 skill_view 累加 usage.json，列表、HTTP 查看及复制不计数；面板显示次数、最近查看与可编辑阈值的陈旧 Badge，以及每个技能的成本（索引行字数、正文字数）和索引总字数中未被查看技能所占部分；整理时陈旧候选附带相同成本，作为裁剪依据。merge 累加来源统计、delete 删除边车、copy 从零开始，正文修改时间和摘要不变。整理陈旧候选仅供模型判断，候选名单随结果／原 turn Trace 保留。入口：Memory、Settings → Memory、skill_view；关联契约：`C-025`、`C-012`；实现证据：`internal/memory/skill_usage.go`、`internal/engine/review_tidy.go`、`internal/server/projects.go`；测试证据：`internal/memory/skill_usage_test.go`、`internal/memory/prompt_stale_test.go`、`internal/server/skill_usage_copy_test.go`、`internal/server/memory_stale_test.go`、`internal/engine/review_stale_test.go`、`frontend/e2e/memory-boundary.spec.ts`。

- 笔记以“不知道会走偏”为判据，常驻注入偏好、事实、硬约束及已有技能指针；技能以“不知道如何做成”为判据，按需加载适用条件、步骤、验证及失败分支。仅某流程相关的约束留在技能前置条件；description 写适用时机。指针仅在原样引用已有技能名且未复述正文时豁免重叠拒绝；纯文本中的不存在名称不能取得豁免。关联契约：`C-025`；实现证据：`internal/memory/prompt.go`、`internal/memory/library.go`、`internal/memory/tools.go`、`internal/memory/skills.go`；测试证据：`internal/memory/prompt_test.go`、`internal/memory/store_test.go`。

- 项目笔记新增、替换、技能降类和面板保存均要求新条目至少包含一个 Unicode 字母或数字。面板可保留、纠正或删除历史条目。保存被拒时，用户可编辑原草稿后重试。关联 `C-025`。回归见 `internal/memory/note_content_test.go`、`frontend/e2e/memory-boundary.spec.ts`。

- 目的：保留本机设置、项目说明和技能；使用者：PC 用户；入口：Settings、Memory、技能管理；输入：配置及项目文件；输出：后续运行可用的上下文；前置条件：本机数据目录；失败表现：保存或解析错误；关联契约：`C-009`、`C-012`、`C-025`；实现证据：`internal/config`, `internal/memory`, `docs/CONFIG.md`。
- 会话摘要与成功回合记忆复核接收从完整历史重建、按 turn 归属的持久化 worker 生命周期事实；摘要预算与落库优先保留事实，复核先从源总 rune 预算预留完整事实块并独立前置，再选择事件来源，不把事实或派生摘要当作可引用写入来源。失败回合 human-only 不接收该前置块；旧摘要和模型自述只作待核对叙述，冲突创建量不能提升为执行证据。旧笔记／技能仍可在 Memory 中人工编辑，系统不按用户业务关键词清洗。关联契约：`C-008`、`C-012`；实现／测试证据：`internal/engine/worker_lifecycle_facts.go`、`internal/engine/worker_lifecycle_facts_test.go`、`internal/engine/session_memory.go`、`internal/engine/review_source.go`。
- 压缩提示预览只显示摘要正文；生命周期证据继续保存在内部摘要、模型上下文与诊断中。关联契约：`C-008`；证据：`internal/engine/compact.go`、`internal/engine/autocompact.go`、`internal/engine/compact_evidence_test.go`、`internal/server/compact_evidence_test.go`。
- 技能可以复制到另一个项目，得到的是独立文件。来源以后的修改要在副本上点更新才会过来；副本改过则不会被静默覆盖，也不会写回来源。没改过的副本仍跟踪最初的项目。入口：Memory 里每个技能的复制，以及多于一个技能时的复制全部。实现证据：`internal/memory/skill_copy.go`, `internal/server/skill_copy.go`, `frontend/src/components/app/skill-copy-dialog.tsx`, `frontend/e2e/skill-copy.spec.ts`。
- 不在项目里的会话没有笔记。回合正常结束后，可复用流程写入数据目录下的共享技能库（数据目录中的 library/skills/）。之后的纯对话会带上这份索引，并用 `skill_view` 打开；当场不能改库。Memory 页可以整理目录，也可以把技能复制进某个项目；副本的更新从技能库拉取。副本改过、两边都改过、或来源已删，文案按这个状态说，不把来源是技能库当成唯一信息。点项目文件夹不会把这份列表换成那个项目的笔记。总开关 `memory.enabled` 关掉后，提示词里不再带上，也不再写入。实现证据：`internal/engine/library_test.go`, `internal/memory/library.go`, `internal/server/library.go`, `frontend/e2e/library-skills.spec.ts`。

- Settings → Memory 的 Review thinking 可独立指定回合复盘推理档位，默认“默认（低）”，可选跟随会话、关闭推理或显式低/中/高；只影响自动复盘和 Review now。记忆关闭时只保留总开关；自动复盘关闭时不展示复盘通知、推理级别、复盘回合和技能核验。笔记上限和技能索引在记忆开启后仍可编辑。入口证据：`frontend/src/components/app/memory-settings.tsx`、`frontend/e2e/settings.spec.ts`、`internal/config/memory.go`、`internal/engine/review_profile_test.go`；关联契约 `C-025`。
- 来源身份 framing 超过单源 rune 上限时整组省略，配对工具不拆开，被省略正文不能作为写入证据。技能提炼指令区分常规路径与已发生故障的独立恢复分支，明确有证据的进入、恢复及回归/结束条件，不要求重演失败探测。该指令仍须效果评测验证。关联契约：`C-025`；证据：`internal/engine/review_source_select_test.go`、`internal/memory/prompt_test.go`。
- 自动复盘不再因 manager 曾写入一项记忆而跳过整轮；成功回合复盘完整有效来源，启用项目知识库时，成功非计划回合在同一自动提案中复盘笔记、Skills 与知识库；失败项目回合只保留真实人类长期要求，不提炼技能、工具结果或派生摘要；取消不触发自动复盘。当前 notes 在项目复盘锁内、模型调用前读取；源事件优先保留人类纠正，再选择近期完整工具配对，按 rune 限额包含来源 framing；现有 notes、技能目录及系统指令另计。入口：正常/失败回合后的自动复盘、共享 Skills 的 Review now（最近成功回合）、项目 Memory 的 Distill（`C-054`）；实现证据：`internal/engine/review.go`、`internal/engine/review_human.go`、`internal/engine/review_source_select.go`、`internal/engine/review_quality_test.go`、`internal/engine/review_source_select_test.go`。
- 自动 reviewer 用一次完整批次提交 notes/skills 变更；notes 在内存草稿复用既有校验，最终按首次模型输入的同一 notes revision 一次提交，普通拒绝后不重基；思考期间的并发编辑终止本轮，不追加修复请求，失败或冲突不半写；skills 仍即时落盘，后续失败也记录真实技能变化，不声称跨文件事务。每项变更引用源 seq 与原文，notes 的新增数字须出现在引文或被替换旧条目中；这是来源/拼写约束，不证明语义真值。成功后程序收尾，不再请求模型写结束语；拒绝可在原迭代上限内修复，同 turn Trace 保留可选 evidence/refused。关联契约：`C-025`、`C-012`；实现证据：`internal/engine/review_batch.go`、`internal/engine/review_evidence.go`、`internal/memory/notes_draft.go`、`internal/engine/review_batch_test.go`、`internal/engine/review_evidence_test.go`、`internal/memory/notes_draft_test.go`。
- 提案预检一次返回全部可独立发现的引用、数字和 notes 校验问题；容量反馈给出拒绝候选用量、当前空闲及可写内容上限。reviewer 在原预算内自动缩写相关条目或合入本轮新事实，保留事实、条件、否定、范围和数字拼写；无关条目不变，任一预检问题都不写 notes 或 skills。语义保留仍依赖模型，来源与长度校验不证明语义等值。关联契约：`C-025`、`C-012`；证据：`internal/engine/review_validation.go`、`internal/engine/review_validation_all_test.go`、`internal/memory/capacity_feedback_test.go`、`frontend/e2e/memory-review.spec.ts`。
- `memory.review_deduplicate_feedback` 可选择减少复盘 reviewer 的聚合拒绝反馈中的重复字段，完整保留每条问题和修复数据。默认关闭。入口：`config.yaml`；关联契约：`C-025`；证据：`internal/config/memory.go`、`internal/engine/review_validation.go`、`TestReviewFeedbackDedupOpenSSLReplay`。

- 失败时 transcript 与 Memory 面板显示停止原因、提案次数、拒绝类别及可用容量数字，区分未提交 notes 与已落盘 skills；Trace 记录有界 attempts 安全元数据，不保存被拒提案正文、原文引句或旧条目。容量反馈向模型保留 usage/over_by/current_entries/matched，原容量、来源、数字、revision 校验不变，只能在原预算内先有依据地缩短或替换相关条目再新增，不能清理无关记忆；无法安全提交时明确失败，旧事件缺新增字段仍兼容。失败 Trace 按钮会打开侧栏并加载原回合，历史聚焦不冒用最新 token 用量；切换后返回原会话时恢复手动复盘结果，结果落在已加载 tail 之外时只读一次已知 turn 的 Trace，并校验点击代次、线程、回合和事件起点，不轮询。入口：回合自动复盘、Memory 的 Review now、同 turn Trace；关联契约：`C-025`、`C-012`；实现证据：`internal/engine/review_diagnostics.go`、`internal/engine/review_diagnostics_test.go`、`internal/engine/review_finalize_test.go`、`internal/server/review_diagnostics_test.go`、`frontend/src/lib/review-diagnostics.ts`、`frontend/src/lib/review-diagnostics.test.ts`、`frontend/e2e/memory-review.spec.ts`、`frontend/src/lib/transcript-review.ts`。

- 记忆写入提示与 Memory 面板的复盘状态提供默认折叠的“查看详情”，展开已保存的新增／删除内容；notes 净变更和支持的 skill create/patch/delete 以事件内完整 before/after 为依据，后续文件编辑不改写历史。旧事件和无快照动作只显示明确标注的预览，失败复盘仍可查看已实际保存的部分变更。关联契约：`C-025`、`C-012`；实现证据：`frontend/src/components/app/review-status.tsx`、`frontend/src/components/app/review-changes.tsx`、`internal/engine/review_batch.go`；测试证据：`frontend/src/components/app/review-status.test.tsx`、`frontend/e2e/memory-review.spec.ts`、`internal/engine/review_change_details_test.go`。

### F-422 Local backup and restore

- 入口：设置 → 备份；`zwai backup` / `zwai restore`；`POST /api/backup`、`GET /api/backup/progress`、`/api/backup/preview`、`/api/backup/restore`。整库备份可整份还原。项目备份可加入现有数据，或在明确确认后替换同一个项目。关联契约：`C-044`。
- 有对话还在进行时，备份会先询问：可以不备份，也可以跳过这些对话继续。跳过的对话不进入这份文件，整库还原也带不回它们；本机上的对话继续运行。还原、重启或另一次备份／还原仍要等待。备份和还原进行时，设置页显示进度：复制数据库、已读取的文件数、归档已写入的比例。进度接口只读，不开始或结束传输。目录中的符号链接不进入归档，页面会说明留下了多少个；被复制的类别目录本身是符号链接时，这次备份失败。还原失败回到还原前的库和文件。成功还原后 `data_generation` 加一，桌面／Web 重新加载，手机清空已加载对话列表再拉第一页。
- 实现证据：`internal/backup`、`internal/engine/backup.go`、`internal/server/backup.go`、`cmd/zwai/backup.go`、`frontend/src/components/app/settings-backup.tsx`、`internal/backup/backup_test.go`、`internal/engine/backup_test.go`、`internal/server/backup_http_test.go`、`cmd/zwai/backup_test.go`、`frontend/e2e/backup.spec.ts`、`models/backup/DataBackup.tla`。来源：GitHub Issue #164。

### F-421 External Agent Skills packages

- 入口：PC 项目菜单的外部技能面板。用户选择包含一个 `SKILL.md` 和配套资源的 ZIP，安装完成后可查看或移除。每个新回合向经理与 worker 提供当前项目的外部技能摘要，`load_skill` 按名称读取正文及资源基址。项目关闭记忆时外部技能仍可用，同名沉淀技能继续走 `skill_view`。
- 安装以可信来源的 ZIP、有效清单和可写项目数据目录为前置条件。同名外部包、无效 ZIP 和不安全条目会被拒绝。关联契约：`C-043`。实现证据：`internal/skills/store.go`、`internal/engine/external_skills.go`、`frontend/src/components/app/external-skills-dialog.tsx`。测试证据：`internal/skills/store_test.go`、`internal/server/installed_skills_test.go`、`internal/engine/installed_skills_http_test.go` 和 `frontend/e2e/installed-skills.spec.ts`。

### F-430 Build and release paths

- Windows 已验收原包首次／部分上传恢复：统一发布入口核版本／源码、两个架构回执、原包 SHA 与 PE，复用接受字节；批次先发布所有非 iOS 平台，再等待 Apple。`--platform ios --ios-status` 对当前 TestFlight 构建实时只读观察，核完整双向组关系，返回可用或等待状态；正式恢复跳过已确认发布历史的详情请求，未知候选保护保持。关联契约：`C-010`；实现／验证：`tools/release.py`、`tools/release_ios.py`、`tools/test_release_resume_paths.py`、`docs/RELEASE.md`。

- Linux amd64/arm64 Web/CLI 包：`make linux-release` 生成 `zwai-<version>-linux-<arch>.tar.gz`（`amd64`、`arm64`），只有 0755、little-endian ELF64 且机器类型与文件名架构匹配的 `zwai`，包含前端，默认 Web 模式；不包含原生桌面窗口。批次显式包含 linux 才发布，旧行不新增交付义务。关联契约：`C-010`；证据：`tools/release_linux.py`、`tools/test_release_linux.py`、`cmd/zwai/default_shell_server_test.go`。

- Windows amd64 与 arm64 桌面 zip：入口是 `make windows-release`，以及批次工作集包含 windows 时的 `tools/release.py`。两份 zip 的唯一文件都是对应架构的 `zwai.exe`；缺一、不是 zip、或 PE 机器类型与文件名不符时不上传。没有 windows 字段的旧台账行不会因此变成待发布。应用内更新仍只安装 macOS zip。关联契约：`C-010`；实现证据：`internal/desktop/winpack`、`internal/update/version.go`、`internal/release/publish.go`、`tools/release.py`、`tools/test_release_windows.py`。

- 初始原生 readiness 对已完整落证且精确匹配当前调用的 `AXRole`／`kAXErrorCannotComplete` 可在原共享期限内重新取得完整 snapshot；失败图不进入窗口／输入证明，其他未知与输入失败不重试。helper 保留严格 Role／Children 首错和当前进程 AX 消息默认上限。关联契约：`C-031`；证据：`tools/native_macos_smoke.py`、`tools/native_macos_ax.m`、`tools/test_native_macos_initial_observation.py`；来源：Issue #135。

- 已分配未公开批次可在 main 后续前进时，从包含完整冻结工作集的原源码恢复同一版本；发布入口验证分配／范围／祖先身份，保留原生、安装包、签名、State 和远端版本门禁。新分配仍使用最新完整 main；后续改动不进入旧批次。关联契约：`C-010`；证据：`tools/release.py`、`tools/test_release_public_recovery.py`、`docs/RELEASE.md`；来源：Issue #134。

- 发布版本建议：`python3 tools/next_release_version.py` 只读查询远端数字 tags、含 draft 的全部 Release 和已分配批次，按数字组件建议下一个有效 patch 版本；未知基线、无效台账或远端读取失败明确拒绝。建议不占用版本、不改源码或台账；发布重试复用已冻结版本，实际发布仍须原生验收与并发版本预检。关联契约：`C-010`；证据：`tools/next_release_version.py`、`tools/test_next_release_version.py`、`docs/RELEASE.md`。

- 本机验证资源准入：`release.py --verify` 在 preflight 前到自有进程清理结束持有同宿主／UID／canonical Go module 的稳定 inode 租约，独立副本共享 mutex；锁内只读核实当前 PID/birth/cwd/module，现存手工重型验证或 unknown 只有限等待／阻断，不发送信号。旧 released 台账不能替代实际进程观测；未来绕过协议的新手工命令不受此 mutex 保证，未包装 `make test/check`。关联契约：`C-010`；实现／验证：`tools/release_host_lease.py`、`tools/test_release_host_lease.py`、`tools/release_verify.py`、`tools/test_release_verify.py`；边界见 `docs/RELEASE.md`。

- 本地发布验证入口 `make release-verify` 复用现有发布器，先做只读来源／依赖／本地 E2E 收集检查，再以最多两个独立阶段执行完整默认检查和正式打包；精确输入与输出字节匹配才复用 headless 成功结果，失败、排队、取消与原始证据独立保留，重叠命令按区间并集计时。返回 pending native 不代表原生通过或发布。普通启动使用 engine／AX／像素入口，恢复日志另属恢复场景；登录点位必须在本次画面后明确批准。关联契约：`C-010`、`C-031`；证据：`tools/release_verify.py`、`tools/release_preflight.py`、`tools/release_stages.py`、`tools/native_macos_readiness.py`、`tools/test_release_verify.py`、`tools/test_release_preflight.py`、`tools/test_release_stages.py`。

- 原生 case 区分真实场景截止与子命令观测超时，保留首错原始流；库存失败仍取消并等待已持有的直接子进程，未知后代继续保留清理拒绝标记与私有数据。草稿验收再次读回持久文本、附件原字节与真实输入框值，不依赖正常保存时已隐藏的提示。关联契约：`C-031`、`C-033`；证据：`tools/native_macos_case.py`、`tools/test_native_macos_case.py`、`tools/test_native_macos_case_timeouts.py`、`tools/native_macos_resilience.py`、`tools/test_native_macos_resilience.py`。
- SIGKILL 清理回归以本测试真实私有进程库存执行故障和恢复，并在失败时保留 runner 首错与证据；测试命名空间不代替生产完整 collector 验收。关联契约：`C-031`；证据：`tools/test_native_macos_fault_cleanup.py`、`tools/test_native_macos_resilience_fault.py`。

- 原生 helper 每次调用保存独立完整字节收据及执行时间；自动继任验收先观察产品自己的新像素 ready，再激活检查窗口，避免 QA 动作制造恢复成功。关联契约：`C-031`、`C-034`；证据：`tools/native_macos_resilience.py`、`tools/native_macos_resilience_phases.py`、`tools/test_native_macos_native_evidence.py`、`tools/test_native_macos_resilience_phases.py`。

- 原生工具可显式选择验证阶段；阶段成功与完整 profile 成功分开记录。Issue #94 的固定长测时长约定已撤销，停止的任务不会因工具 profile 自动恢复，也不改动其他任务的验收范围。关联契约：`C-031`、`C-034`；证据：`tools/native_macos_resilience.py`、`docs/TESTING.md`。

- 原生 case 的内部 `prepare` 接缝在原真实锁、未清理回执及私有进程清单下完成有限登录／power 交接；App 发布前执行最终准入，scenario 用同 birth 三类断言确认接管。登录仅接收一次内存凭据，缺失 SDK lock 字段保持 unknown，仍须本次新鲜桌面图批准；未提供回调的公共 CLI 不变。关联契约：`C-031`；实现／验证：`tools/native_macos_case.py`、`tools/native_macos_case_power.py`、`tools/native_macos_prepare_login.py`、`tools/native_macos_prepare_login_client.py`、`tools/native_macos_prepare_login.swift`、`tools/test_native_macos_case_prepare.py`、`tools/test_native_macos_case_power.py`、`tools/test_native_macos_prepare_login.py`。HOST 回归不代表来宾窗口或恢复验收。

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

语音设置按当前来源协商到的 transcription／speech 能力分别显示；LOCAL 个人配置保留自身能力。可配置网关来源在 Models 显示获准角色的模型选择，固定来源仅在 Speech 显示已获准的语音参数。缺失或失败的能力清单不能由旧 configurable 布尔值重新开放控件；保存的选择保留，重试后恢复。

### F-137 HTML/JS task panels

- 桌面和 Web 通过宿主注册表显示当前会话的 Task panels 标签，多个实例共用一个入口。首次发现新实例时自动打开，数据更新不抢焦点；切换标签保留 iframe。切换会话或移除实例使所选标签不可用时，暂时显示 Agents。插件目录继续只列可由 Agent 打开的 HTML/JS 定义。见 [开发者接入](TASK_PANELS.md#开发者接入)。

- 目的：将任务的结构化结果持续呈现在插件自定义界面；入口：会话右侧任务面板、配对手机会话的全屏面板、Settings → Tools、Agent通用面板工具；输入：已安装插件定义、带来源的快照或增量；输出：展示运行中或已有观测的实例、持久状态、地图与本地/触摸交互；三端均可持久关闭实例并中断其数据源；前置条件：插件版本兼容，数据源执行由当前任务拥有，手机通过Pairlink读取PC投影；失败表现：协议/版本/加载错误保留旧状态，取消与重启显示中断，旧PC不展示入口；关联契约：`C-050`。实现证据：`internal/taskpanel`、`internal/store/task_panels.go`、`internal/engine/task_panels.go`、`internal/remote/task_panels.go`、`internal/panelplugins/fofaworld`、`frontend/src/task-panels`、`frontend/src/components/app/task-panel-frame.tsx`、`mobile/src/components/task-panels.tsx`；说明：[Task panels](TASK_PANELS.md)。
