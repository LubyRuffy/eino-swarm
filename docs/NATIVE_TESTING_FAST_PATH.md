# 原生测试最短路径

本页是项目内唯一的短操作流程：**先查输入和资源，先完成不占 guest GUI 的工作，冻结一次候选，再串行验证受影响场景，清理后结束。** 适用于 `ci-macOS`，不改变 [AGENTS.md](../AGENTS.md) 的测试要求、[测试合同](TESTING.md#native-desktop-isolation-f-430--c-031)或[发布门槛](RELEASE.md#local-verification-before-publication)。节省重复工作，不降低验收标准。

## 一分钟选路径

1. 记录本轮范围、源码 SHA／dirty 内容、所需检查及专项；保护其他任务。纯文档走本页末尾的轻量检查，不占 VM。
2. 在独立副本检查 Git、依赖、工具链和验证计划；有发布任务时提前查发布前置条件。缺项先修准备，不进入 GUI。
3. 按影响范围完成强制非 GUI 检查；完整 Mac 候选需要统一验证／打包时才用 `release-verify` 取得宿主租约。其他裸检查先协调资源，不为取得租约而扩大为整套发布验证；同输入复用有效阶段。
4. 固定源码、版本、最终 ZIP／可执行文件哈希、签名和 harness 身份；helper 编译、传输和校验先做完。
5. 选择真实 `ci-macOS`，确认控制通道、当前登录和实际测试主体的权限；通过 guest case 锁后，按矩阵验证。普通 smoke 基线可由专项中已包含的同等断言逐项举证，避免重复。
6. 保存首错与完整证据；确认 app、engine、子进程退出及私有数据删除，才开始下一例或报告完成。

## 哪些已经自动执行

| 入口／机制 | 当前真实行为 | 仍需操作者完成 |
|---|---|---|
| `release-plan`／`release_preflight.py --check-only` | 打印计划／离线输入检查；不安装依赖，不取宿主租约。检查会运行本地版本和 Playwright collection 探针 | 确定候选与范围；检查失败时先分类 |
| `release-verify` | `HostValidationLease`、checkout 锁、最多两个独立阶段、完整默认检查及打包；精确匹配的非 GUI 阶段可恢复 | 裸 Go／fuzz、`make test/check` 仍须协调，不能在它旁边另起重型检查 |
| `native-smoke` | guest-user case 锁内做本次 helper／AX／录屏／console 预检，随后隔离 app/home/data、启动、行为检查和清理 | 选对 VM、准备登录与已授权执行主体、传输和最终产物溯源；不会自动解锁 |
| `native_macos_case.py` | 通用生命周期、私有环境、串行与清理 | 自定义 scenario 必须实现所需权限、身份、窗口、像素与业务断言；不能假定它有 smoke 的全部预检 |

**操作约定，不是代码已强制的能力：**普通 `make release` 不会自动先跑 `release-verify`；宿主租约没有独立 CLI，也没有全局 VM 调度器。不要编造租约探针、自动登录或通用文件／升级验收命令。VM 池、迁移、统一所有入口和常驻 GUI actor 不属于本页已实现能力。

## 1. 在 host 冻结输入，先挡住廉价失败

开发可以使用 worktree；`release-verify` 要求 `.git` 为目录的**干净 standalone Git 副本**，拒绝 linked worktree 和缺 VCS 身份的拷贝。以本轮已核对的完整候选为准，不把无关任务或用户主目录当临时验收副本。

以下是完整 Mac 候选的输入检查；局部开发只查其实际所需输入，不因此扩大为整套发布。变量均填写本轮真实值；版本用于验证，不代表分配或发布新版本。证据目录在 checkout 外，路径只用字母、数字、点、斜杠、下划线和连字符。

```bash
SOURCE_COPY=/absolute/clean-standalone-copy
CANDIDATE_SHA=FULL_COMMIT_SHA
RUN_VERSION=x.y.z
RUN_ROOT=/absolute/private/unique-candidate-run
cd "$SOURCE_COPY"
git status --short
git rev-parse HEAD
make release-plan VERSION="$RUN_VERSION" VERIFY_JOBS=2
python3 -B tools/release_preflight.py \
  --checkout "$SOURCE_COPY" --source-sha "$CANDIDATE_SHA" \
  --project frontend --project mobile --check-only
```

必须核对 HEAD 等于候选、工作区干净。离线 preflight **0=通过，2=阻塞**，不会修复依赖。仅当确认依赖缺失／不完整，且已协调宿主空档后，分别在 `frontend`、`mobile` 运行 `npm ci --include=dev`，然后重查一次；不要复制未知 `node_modules`、改 lockfile 或删版本约束凑通过。自定义 CC／CXX／GOROOT／非 local GOTOOLCHAIN 当前不被该入口接受。

确有发布任务且已有相应授权时，提前运行 `make release-check VERSION="$RUN_VERSION"` 查来源、签名及远端前置；它可能运行 Go 工具、联系远端并建立本地 release 锁，**不是廉价离线 preflight**，也不构建或上传。纯修复／验证任务不因此获得发布授权。

## 2. 非 GUI 检查先完成，少占 guest

先按 [AGENTS.md](../AGENTS.md#commands) 保留硬门槛：触 Go 仍跑全仓 race；触 `frontend/src` 仍跑 lint 和 unit；触 `mobile/` 仍跑对应测试；用户可见改动仍跑 E2E。下面的原生矩阵只选择附加场景，不能裁掉这些检查。

完整 Mac 发布候选的现有可恢复入口：

```bash
# 首次运行创建；恢复已有 run 时保留原目录，跳过 mkdir。
mkdir -m 700 "$RUN_ROOT"
make release-verify VERSION="$RUN_VERSION" \
  EVIDENCE="$RUN_ROOT/headless" VERIFY_JOBS=2
```

它在 preflight 前自动取得宿主租约，默认准入等待 30 秒，最多两个阶段并发；检查完整默认 Go／前端／手机／tools／docs／PC E2E 后才打包。**验证脚本以 3 和 `headless_passed_native_pending` 表示 headless 通过、仍待 native**，不是失败重跑理由，也不是已发布。Make 包装可能显示 `Error 3`、自身返回 2；以 `verification.json` 和阶段 `result.json` 判定，不能把 Make 的 2 一概当 preflight 阻塞。手机 E2E、opt-in E2E 和专项仍按影响范围补齐。

自动调用若需直接取得验证脚本的退出码，使用等价入口；它保留相同租约、检查和预算：

```bash
python3 -B tools/release.py --version "$RUN_VERSION" --platform macos \
  --verify --evidence "$RUN_ROOT/headless" --jobs 2
```

所有 Make 包装命令都应同时读原工具回执，不能假定 Make 原样传出子进程状态。

- 已有同候选有效回执时，用**同一命令、同一 evidence 目录**恢复；由 StageRunner 判定复用，不手动把旧结果改为 passed。
- 复用须原阶段 passed、清理已确认，且版本／源码、命令、工具链、依赖与 lockfile、环境／配置、输入输出字节及原 stdout/stderr 回执匹配。变化会使相关阶段失效；失败回执不能复用为成功。
- GUI 回执不由 StageRunner 自动复用。只有精确候选及完整接受证据仍有效时才沿既有合同记录；不能拿旧包、旧 helper 或登录成功代替新业务验收。
- Go 输出 `(cached)`、阶段 `reused` 与本轮真实执行分开记录；性能对比另用固定输入和相同新鲜执行命令，不将缓存命中宣称为执行提速。
- 不同时另跑 `make check`、完整 `make docs-check` 或第二份同范围 Go/E2E。Playwright 仍单 worker、零自动 retry；并发任务各有自己的 port、data、output 和 served dist。

## 3. 固定产物和通道，进入 guest 前准备好

1. 保存最终版本、完整 source SHA／必要 dirty-file 清单、ZIP 名和 SHA-256、包内可执行文件哈希／`go version -m` 来源、`Info.plist` 与 deep/strict 签名验证。取验证入口实际生成的 package provenance，独立检查最终执行包与它相符。
2. host 上先执行 `make native-smoke-prepare`；它只构建／识别 helper，不开窗口。传输匹配的 `tools/`、`bin/native-macos-ax` 及邻接 JSON，保留布局和文件哈希。不要为每例重编译相同 helper。
3. 使用已有获授权的 pinned SSH／共享通道；新建不可变 payload，复制到 guest 本地并核完整字节及执行权限。不要覆盖正在执行的共享目录；传输／签名校验另计时。
4. 首次 GUI 前确认实际目标是 `ci-macOS`、上例 cleanup 完成且没有其他控制者。VM 名称不由通用 runner 的 hypervisor guard 证明；历史 released／ready 文件不是当前空闲证据。

`native-smoke` 不接 ZIP／source-SHA 参数，因此**该命令不能单独证明分发 ZIP 的完整来源、签名与哈希**。后续重签名、公证、staple、重打包或代码变化导致字节变化，必须重新绑定受影响证据；不要再次构建一个同版本文件就继承接受。

可选的现有观察入口（使用本机已配置的 profile，不新建凭据）：

```bash
python3 -B tools/native_macos_access.py \
  --profile "$EXISTING_PROFILE" --evidence "$RUN_ROOT/access-01"
```

这是**不修网络的控制通道观察**，仍会冻结／投递探针并写证据，可能走 SSH 或共享队列；0只表示控制通道可用，3表示不可用／pending，2表示输入或脚本错误。它不接受登录、AX、窗口或产品。不要加 `--repair-network`、重启 VM、刷新密钥或修改 TCC 来抢过门槛。

恢复旧 pending 时先把当前 guest 状态视为 unknown，重新观察。宿主锁屏不证明 guest 不可控；SSH helper 无权限也不证明已有获授权的 guest Terminal 主体无权限。只使用已获授权路径；guest 登录恢复与应用验收分开，在同一受控交接内完成新鲜观察、必要登录、保活接管和启动，避免返回模型后画面过期／再次锁屏。不要修改锁屏策略，凭据不得进入 argv、环境、文件或日志。

已有私有 observation 时，`python3 -B tools/audit_macos_acceptance.py --observation "$OBSERVATION"` 只做离线决策；exit0表示输入合法，仍须读 `status`／`next_action`，它不生成真实窗口证据。

## 4. guest 内串行运行：普通 smoke 加受影响专项

在有既定权限的 guest 执行主体中，进入已核对的 harness 目录：

```bash
make native-smoke APP="$CANDIDATE_APP" \
  EVIDENCE="$NEW_NATIVE_EVIDENCE"
```

每轮用全新 evidence 目录。现有入口默认 scenario 50 秒、整轮 60 秒；锁内最多 5 秒的本次非交互预检共享预算，之后仍检查正确 PID／bundle／origin、真实 AX 窗口、非空像素、一次输入／Send、worker／文件／Trace 和可见结果。`ready_for_attempt`、App 登记、激活、AXPress 或 API 200 都不等于这些断言通过。只有明确 registration/window/content pending 可在原共享 deadline 内等待；未知、身份／权限拒绝或业务断言失败不盲轮询、不重复 Send。

### 场景触发矩阵

| 改动／目标 | 必须补的原生观察 | 现有入口或定位 |
|---|---|---|
| 纯文档／操作规范 | 无 GUI | 本页末尾轻量检查 |
| 普通桌面启动、输入／Send、基础 worker／Trace | 一轮普通 smoke；为测速才另留至少三轮串行样本 | `make native-smoke`；[daily 合同](TESTING.md#daily-native-round-60-seconds-including-cleanup) |
| renderer／GPU／白屏与自动恢复 | 对应恢复、原数据／窗口、继任身份、有限重试；不能只主动激活后看最终截图 | [resilience](TESTING.md#desktop-renderer-resilience-f-115--c-034)，按实际范围选择 phase |
| engine handoff／重启、Stop、人问或 worker 续跑 | 真实进程替换、原 turn／worker／call、恢复状态和实际用户动作 | [自动交接](TESTING.md#automatic-restart-handoff-f-110--c-027)；[生成中断](TESTING.md#generation-interruption-and-recovery-issue-95--c-035) |
| 安装包／升级／回滚／重开 | 精确签名旧→新安装、版本、结果／回滚、同包重开；图标变化还看解压包 Finder 图标 | [升级生命周期](TESTING.md#desktop-update-lifecycle-f-190--c-024)；无统一升级 CLI |
| 文件／目录选择、内容预览、粘贴／图片 | 真实系统 picker／原生控件、选定路径身份、实际保存／重载字节及可见内容 | [目录选择](TESTING.md#project-working-directory-selection-f-120--c-026)、[PC 预览](TESTING.md#pc-workspace-file-preview-f-140--c-005)、[图片输入](TESTING.md#decodable-image-input-and-sent-image-preview-f-140--c-028)；无通用文件专项 CLI |
| 报告的 checkout 启动命令失败 | guest 私有副本运行原命令及精确源码／dirty 内容，观察窗口、资产和离线回合 | [隔离规则](TESTING.md#native-desktop-isolation-f-430--c-031)；预构建 App 不接受该命令 |
| 实际 sleep/wake 或长时间无人输入目标 | 真正 power transition／全程有界连续观测；独立预算 | [resilience](TESTING.md#desktop-renderer-resilience-f-115--c-034)；普通 smoke／VM pause 不替代；已停止的 #94 不自动恢复 |

专项可明确复用已包含的普通基线断言，避免重复相同行为；必须逐项列证据，不能从 exit0推定包含。发布要求的全部相关场景仍须满足。未触发的专项不运行、不标 passed；属于未测范围。

resilience 的实际 guest CLI 示例（仅当该 phase 受影响且已授权）：

```bash
python3 -B tools/native_macos_resilience.py launch \
  --zip "$CANDIDATE_ZIP" --source "$CANDIDATE_SHA" \
  --evidence "$NEW_CAMPAIGN_EVIDENCE" --phases baseline renderer
```

可选 phase 为 `baseline renderer gpu exhaustion sleepwake soak`；这里只演示参数形状，不要求每次全跑。保留各 phase 的原预算／故障准入，不能缩成 60 秒。升级、预览等没有一键专项时，先找 [TESTING.md](TESTING.md) 的实际断言与已审查 scenario；若缺入口，明确 native 缺口，停止在业务任务中临时扩建整套设施。已有 scenario 的通用生命周期调用形状为：

```bash
python3 -B tools/native_macos_case.py \
  --app "$CANDIDATE_APP" --evidence "$NEW_CASE_EVIDENCE" \
  --timeout "$CASE_TIMEOUT_SECONDS" --budget "$CASE_BUDGET_SECONDS" \
  -- python3 "$REVIEWED_SCENARIO"
```

timeout／budget 按该场景现有合同明确选择，不靠失败后延长来变绿。重启步骤在同一 case 内保留状态，独立场景用新 case；所有子进程继承 `ZWAI_NATIVE_CASE_ID`。后续上传仍按独立[发布流程](RELEASE.md)及授权，不是以上验证命令的副作用。

## 5. 首错分类，有限重试与清理

以下重试规则是操作约定，不能覆盖 helper 的 `retryable=false`、未知副作用、清理拒绝或安全守卫：

| 类别 | 判据与动作 |
|---|---|
| 资源排队 | host／guest 锁、活动 owner、inventory unknown；记录 owner／queue，返回 pending。等待释放的新鲜证据后再申请，不重复抢锁、不启动竞争套件 |
| 环境不可用 | SSH／登录／实际主体 TCC／录屏、平台能力异常；保存当前证据，在 App 前停止。先处理独立前置条件，不反复运行整个业务场景 |
| 产品失败 | 已进入真实窗口／业务，目标断言不满足；保留首错、来源、Trace／像素，走最小产品修复及相应回归，不改断言掩盖 |
| harness／断言失败 | 参数、SDK／模块类型、动态标签、回执归属或证据 writer 异常；用离线实际 caller 回归先修，不因此判 VM／产品失败 |
| 结果不明 | 派发／连接断开；先查同一 action／case 的结果、锁与进程，不再次输入或重复启动。未知区间保持 unknown |
| 清理失败 | 先保留 refusal latch、证据与私有数据；停止下一例。只恢复有身份凭据的自有进程，不删除锁 inode，不杀不明 PID |

同一原因没有新证据就不重试。已确认结果与清理、找到可区分的新证据并修正后，最多做一次新的有界验证；再遇同层问题则收口为具体缺口／独立设施问题，保留业务成果。不得重放未知工具效果、反复点 Send／Paste、`killall`／`pkill`、删锁或关闭别人的 VM。变更源码／harness 后是新输入，须按影响范围重验，不能覆盖原失败。

**完成清单：**

- [ ] 候选与 helper 身份匹配；所需非 GUI 检查／专项均有明确结果，未测项诚实列出。
- [ ] 真实窗口／像素、实际用户动作、durable结果／字节与Trace满足本场景，而非只启动成功。
- [ ] 原 stdout／stderr、超时部分流、命令／deadline／首错先持久化，解析或证据 I/O 错不能替换原失败。
- [ ] `cleanup_complete=true`、无 remaining owned PID、私有 data已删除；关窗不算 engine退出。
- [ ] 清理失败／结果不明保持 pending；预算到期仍清理到底，不为守时丢下进程。
- [ ] 报告分别标代码／headless／native／发布状态；exit3、登录通过或后续重试变绿不改写首次失败。

## 证据目录与时间口径

推荐一个 checkout 外 mode0700的 `RUN_ROOT`：`headless/`给可恢复验证，`access-01/`给当前观察，`native/<scope>-<attempt>/`给每个全新case；来源／候选清单和最终结论同根保留。这是目录约定，不是新增 CLI；实际原件路径以工具回执为准。私有证据文件（包括截图）显式0600；截图工具可能替换预创建文件，必须核对最终文件模式和字节，不能以预建0600当完成。保留正常编译／测试子进程umask，不能全局`umask 077`改变fixture语义。不满足时按证据writer故障处理，不反复启动业务场景。不要复制凭据或无关进程完整argv。

| 阶段 | 目标／现有约束 | 计时要求 |
|---|---|---|
| 输入检查、计划、资源准入 | 第一轮前完成；host准入默认30s后blocked，不循环空等 | 分开记录探针执行与资源排队 |
| 依赖／构建／helper／首次传输／登录 | 尽量在占GUI前完成；无统一已测SLA | 记录准备与返工，不能从端到端报告隐藏 |
| 普通smoke | 现有整轮成功预算60s，含清理；历史12–15s仅为已就绪该场景 | 记录preflight／prepare／launch／scenario／cleanup／total |
| 专项与长测 | 按原合同单独预算，不承诺一分钟 | 选定scope、完整原始时序及未测项 |
| 清理与端到端 | 清理是完成条件；没有“全发布5分钟／全engine1分钟”的承诺 | 总墙钟含排队／准备／执行／清理／返工；缺时钟为unknown |

这些是优化目标与现有budget，不是新测出的SLA。host和guest各记录wall／monotonic，不直接跨机器相减；并行阶段取时间区间并集，不相加。参考[三轮历史smoke](diagnostics/native-minute-20261022.md)及[隔离原则](diagnostics/native-isolation-20261021.md)，它们不接受当前候选。

## 给后续 agent 的入口

> 先读本页和AGENTS。列本轮源码／输入身份、强制检查和原生触发矩阵；先查输入与资源，按影响跑headless。完整Mac候选才用release-verify统一租约和打包，其他检查先协调，不能无条件扩成全套。需要native时提前准备helper、冻结最终字节，只在ci-macOS的既定授权主体中串行执行所需case。记录原失败／unknown，最多一次有新证据的有界重试；确认完整清理后结束。报告执行／排队／准备／返工与未测／未发布，不操作用户host GUI、不扩建无关设施。

纯文档修改的轻量检查：

```bash
python3 -B tools/docs_check.py
git diff --check
```

另核本次相对链接／anchor、Make目标与CLI参数是否真实存在；`docs_check.py`只自动扫描既定Feature／Contract引用，不覆盖所有新文档链接。`make docs-check`还会运行全部tools单测，纯文档不必为此启动完整套件；触实现时仍按原规范运行必要检查。
