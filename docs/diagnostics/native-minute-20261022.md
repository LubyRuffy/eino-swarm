# ci-macOS 日常原生回归一分钟验收（2026-10-22）

关联：`F-430` / `C-031`。三轮串行真实原生回归全部通过；包含独立前后核验的完整单轮耗时为 **14.50、12.30、12.06 秒**。每轮使用全新状态，完成原生输入、离线任务、结果和截图断言后，确认进程退出并删除私有数据。

## 计时范围与入口

前提是 UTM `ci-macOS` 已启动且可控制、候选 app 与预编译 AX helper 已就绪。应用构建、helper 编译、首次传输和控制通道恢复属于准备阶段，单独留证；本记录接受准备完成后的日常回归。

```bash
# 准备候选时执行一次；不启动窗口。
make native-smoke-prepare
# 在 ci-macOS 中执行；每次使用新的证据目录。
make native-smoke APP=/absolute/candidate/zwai.app EVIDENCE=/absolute/private/new-round
```

runner 默认整轮预算 60 秒，场景上限 50 秒，清理预留最多 5 秒。准备、启动、行为、证据和清理共享单调时钟；超预算不能成功，清理仍必须完成。候选可复用，用例 app/home/cache/data/workspace 不可复用。超时、取消、失败和物理宿主拒绝由无 GUI 回归覆盖，本次真实窗口计时覆盖成功路径。

## 冻结执行身份

| 输入 | 身份 |
|---|---|
| 应用源码 / 干净构建 | `2937bd78d11f26f589aaecc6ed48126e222a1898` / `make build` |
| 窗口版本 | `v0.1.33-50-g2937bd7` |
| 实际 harness 源码 | `6e9e8b6ad33747faf0321b99f4da55a25b55c76e` |
| 私有签名 app executable SHA-256 | `50ebe258bcff41b7ec4a23e2cd949559531f43746a83170f5afe91e59f68711a` |
| candidate.zip SHA-256 | `7949a674090eb8be31a3caf161b5706bfbdc1883cfa5b59b5cba35bb0930b776` |
| AX helper 源码 SHA-256 | `55e6a4f602fe1d8f03beb18775f4c9fb9ebe0275cd3c5508854e58694e297fc7` |
| AX helper 二进制 SHA-256 | `2725597492ff82a3f55d1b025afd29850db761dfd6aa0eb3b41e37a1b1874870` |
| 输入清单 prepared-current.json SHA-256 | `d6e1379e62febf1c719bb548344c1e824d2cd7be987fedaefbdb0f5db9238b01` |
| 三轮外部驱动 SHA-256 | `2121f9f3c9d0d0472a55b8bf0c3c72a85695d64c47f5dc1bfad4f6e582ef906a` |

应用源码到 harness 源码之间只改验收工具、测试和文档，应用 Go/前端输入未变化。应用没有可用的 buildinfo VCS stamp；源码归因使用实际干净构建日志与输入清单。原始 `smoke.json` 的 source 字段保留 unknown，外部清单通过实际执行字节绑定源码，没有用环境变量伪造 stamp。

来宾为 `ci-macOS.local`、uid 501、`kern.hv_vmm_present=1`，UTM UUID 为 `088C3975-9321-4204-A0E6-DE1C784C002A`。私有 app plist 的 version/build 均为 `0.1`；这是开发验收候选，不是公开安装包或新版 main 的验收。

## 三轮测量与真实行为

| 轮次 | 完整轮次，含独立核验 | make 启动至实际 reap | runner 总计 | runner 清理 | app / engine PID | 原生窗口 ID |
|---|---:|---:|---:|---:|---|---:|
| 1 | 14.496928 s | 14.381511 s | 14.222954 s | 1.148438 s | 63495 / 63509 | 1398 |
| 2 | 12.295480 s | 12.168837 s | 11.954420 s | 0.389414 s | 63645 / 63660 | 1409 |
| 3 | 12.061568 s | 11.928881 s | 11.723197 s | 0.422407 s | 63793 / 63807 | 1424 |

完整轮次采用驱动的 `finished_monotonic - started_monotonic`，包含候选身份核验、整个命令及退出后的独立路径/进程审计。make 区间到实际 `wait` 回收为止。三轮串行总跨度为 38.860511 秒；不使用跨宿主/来宾的墙钟相减。

| 轮次 | case ID | Trace turn ID |
|---|---|---|
| 1 | `9d1ef87c922b4aec9cef1391f3b6a9f6` | `tn_5a2519801816d633` |
| 2 | `0d397ac1404a4cc1ba7e8be21cd2e338` | `tn_a08a9de2f4c7dcae` |
| 3 | `1fcfa098a8f943ec993808c2136ba42f` | `tn_f0d5bd25f45c23d0` |

每轮同时满足：

- 原生 AX 读取窗口、输入唯一 case 文本、完整读回草稿，再按唯一且可用的 Send；核对本例 PID、存在且解析后相同的 bundle、WebView origin 和 desktop shell。
- 实际离线适配器产生 10 次 mock model call；Trace 中 manager/turn 完成，两名 worker 各自的写文件调用及 tool result 成对，读取 workspace 文件核对真实字节与哈希。
- 原生最终窗口可见本轮结果；截图来自本例 app PID 的窗口，PNG 实际解码为 1391×917。三张截图均由执行者及独立复核者逐张查看，输入、Result、两名 worker done、mock 和空闲状态可见。
- 场景及命令退出码 0，`budget_exceeded=false`、`cleanup_complete=true`、`data_removed=true`、`remaining_owned_pids=[]`。命令退出后独立重查 case root、app/home/data/workspace 均不存在，归属进程为空；确认前例清理后才开始下一例。

## 控制恢复与收尾

用户授权临时聚焦 UTM，恢复来宾显示、登录并引导一次性后台队列；随后隐藏 UTM，三轮计时期间仅后台控制来宾。宿主前台观察器每 0.5 秒采样，请求发布到响应结束共 79 个样本，UTM 前台样本为 0，观察器正常退出。宿主没有启动测试 app。

临时队列未修复 SSH，也不是新增仓库服务。它校验来宾身份、会话 nonce、原子请求和独占 claim，串行执行并留存真实 stdout/stderr/退出状态。来宾临时 `caffeinate` 绑定队列 PID，最长一小时，没有永久修改锁屏/电源设置。完成后普通 stop 请求得到 stopped 响应，bootstrap 实际 wait 确认 agent PID 62679 和 wake PID 62680 均退出码 0、已回收。

各轮私有状态已在计时内清理；另行核对本轮六个准备缓存的精确路径、uid、device/inode 和候选哈希、确认无引用进程后删除，保留独立清理收据。未按进程名批量终止或通配符清空目录。

## 回归与证据位置

当前 harness 的 `make docs-check` 通过：26 项 Feature、31 项 Contract 引用闭合，170 项工具测试通过（54.296 秒）。真实 Objective-C 编译及 AX 图/跨语言 JSON 回归通过；修改的生产工具文件分别 444、469、223 行。应用构建通过；构建及首次准备时间单独记录，不计入上述结果。

交付验证另行运行默认离线 headless Web E2E：147 项通过、14 项未启用场景按预期跳过，0 failed/flaky，退出码 0，完整耗时 384.308 秒，自动重试为 0。首次尝试在收集阶段加载了工作树与主目录两套 Playwright runtime，没有进入业务断言；保留首次失败与实际模块解析诊断，仅在任务 worktree 执行 `npm ci --include=dev` 恢复依赖，随后用新端口/data/output 完整重跑。最终端口 55756 无监听，测试进程及具有本轮精确私有环境/路径的服务和浏览器均已退出；该独立 data 与报告作为私有诊断证据保留。原生一分钟计时与这项广覆盖 Web 交付检查分别报告。

真实来宾曾暴露 Darwin 路径别名、AX 自环和 Objective-C 数值冒充 JSON boolean 三类问题，均保留失败输出并补充观察失败后的回归修复。共享目录原地覆盖还曾返回旧/截断的驱动，后续使用全新 payload 路径、复制至来宾本地并核对全部哈希；不以一次重试变绿解释首次失败。首次失败和清理回执未被最终成功覆盖。

私有原始证据位于 miniclash/dist 下：

```text
utm-minute-control-027a1d7d252c/
  payload-6e9e8b6-51e8ef48/       # 冻结输入、helper、候选 ZIP 和驱动
  evidence/native-three-9aaa454caa/
    three-rounds.json             # 身份、串行时钟、独立清理审计
    independent-review.json       # 三轮只读复核和实际 PNG 查看
    evidence-index.json           # 60 个原始文件的字节数和 SHA-256
    prepared-input-cleanup.json   # 六个准备缓存的精确清理
    control-shutdown.json         # 脱敏 stop 响应和实际 child wait
    round-{1,2,3}/                # 原始 AX、PNG、Trace、workspace、日志、cleanup
utm-minute-control-2fc67b5161d0/   # 本次已停止的后台控制会话
```

宿主前台观察日志、完整构建/测试输出与当前验收范围保存在任务 worktree 的 `bin/utm-minute-validation/`，不提交临时产物、会话 nonce 或来宾数据。此记录接受上述冻结输入的日常成功回归，不外推为完整功能套件、冷启动/长恢复或发布验收。

Web E2E 的首次失败/诊断位于 `recovery-20261022/web-e2e-default-5db5b33cf8b6/`；通过结果及实际退出核验位于 `recovery-20261022/web-e2e-default-restored-724477379771/{status.json,report.json,e2e.log,teardown.json}`，受测 HEAD 为 `edb29ccfc701539e4701b269b89385bf51d1af32`，与原生验收的生产 harness 字节相同，仅新增验收文档。

`evidence-index.json` 自身 SHA-256 为 `f85ac01d28df3edba04c81f7a707dceb246578a0730e97fd37e9e0197659200b`，不包含该索引自身或后续 Web E2E 产物。
