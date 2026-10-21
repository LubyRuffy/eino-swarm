# ci-macOS 原生测试隔离验收（2026-10-21）

关联：`F-430` / `C-031`。结论：冻结候选的串行成功生命周期已通过真实窗口验收；旧测试残留已按精确归属清理。此记录不接受其他功能、较新主线或公开发布。

## 实际环境与后台通道

- 来宾：UTM `ci-macOS`，UUID `088C3975-9321-4204-A0E6-DE1C784C002A`，hostname `ci-macOS.local`、uid 501、`kern.hv_vmm_present=1`。
- SSH 当时不可用；本次采用共享目录中的一次性 Go 命令队列，未修复或宣称恢复 SSH。队列校验来宾身份、会话 nonce、原子请求与独占 claim；串行执行并保存实际 stdout/stderr、退出状态，TTL 上限一小时。
- 引导只向既有 UTM PID 和核对过的 ci-macOS 窗口发送定向按键，使用窗口专属截图；没有全局输入、宿主应用激活或 UTM 启动/前台操作。来宾权限仅在来宾设置中配置。
- 共享目录返回 `F_FULLFSYNC: ENOTTY`。临时队列仅对此错误回退到普通 `fsync`；其他错误及回退失败仍失败。这不等于完整硬件缓存刷新。
- 临时队列源码、二进制、协议和私有执行证据留在共享会话；它不是仓库公共 CLI 或常驻服务。

## 冻结执行身份

| 项目 | 身份 |
|---|---|
| 应用源码 | `9119eefab6c3a3c3f2d35bbdf81aa511b6a8e1a8` |
| 窗口版本 | `v0.1.33-20-g9119eef` |
| 标准 app 的 zwai SHA-256 | `9ad677802117ac875c03189d7a311eb34f246ef628712612cc3655198b9acad7` |
| case runner SHA-256 | `90c4b3a68c9a38a3c325824d02b895ac79bb02a9fca3bb3ca290e34c56f5672d` |
| 场景 SHA-256 | `cae560858cde7c8e827e58d1703bf87e84a56ffff309e3933803a25de156cd73` |
| 原生 AX helper SHA-256 | `4c646d6c38a8f15115be783948a4eb8f06631cb7caf77b489135a9f1cee1f1e7` |

应用在独立 worktree 中通过 `make build`，随后组成标准 `CFBundleExecutable=zwai` 私有 app。旧 wrapper app 含原始绝对 core/data 路径，不能作为隔离副本验收。

## 清理与真实窗口结果

1. 当前进程 argv、executable、启动时间、目录 dev/inode/uid 与历史清单交叉核对后，终止唯一旧 fixture PID 49011；确认退出后才删除 12 个测试专属目录，约 1.77 GB 逻辑文件大小。没有按进程名称批量终止或按目录通配符删除。
2. 每例从空对话开始，在实际 Wails 窗口按 New conversation、键入独立输入、按 Send；核对本次 turn 的持久化 done、manager done、离线结果、原生 AX 与可解码窗口截图。
3. 通过原生 Close 关窗，确认 app 退出；runner 停止同例 engine/后代并验证剩余进程为空，随后删除 app/home/cache/data/workspace。驱动确认第一例全部清理后才启动第二例，并验证所有身份和路径不同。
4. 三次串行双例均通过。最终有宿主观察见证的双例：desktop PID 56512/56682，engine PID 56524/56694；两例 `cleanup_complete=true`、`data_removed=true`、`remaining_owned_pids=[]`。
5. 最终来宾盘点无旧 fixture 或 zwai app/engine。正常 `.zwai-swarm` 与 `Library/Caches/zwai` 目录元数据与清理前相同；未读取或复制其内部配置、数据库内容。

宿主观察器每 0.5 秒读取前台 PID。以宿主自身单调时钟包围请求发布至响应到达的完整区间，最终双例区间共 42 个样本，全部为锁屏 PID 665，UTM 前台样本为零；不把来宾 UTC 与宿主 UTC 直接比较，也不把单条变化日志当周期采样。

## 私有证据与范围

共享会话名：`zwai-ci-backchannel-20261021-6ab91fd3`，位于来宾 My Shared Files 的 miniclash/dist 目录。证据保留在该会话的 evidence 子目录，测试临时数据已删除。

- `legacy-cleanup-plan.json`、`legacy-cleanup.json`、`inventory-before.json`、`inventory-final.json`：清理目标、当前身份与结果。
- `execution-manifest.json`、`native-pair-witness/pair.json`：候选/工具字节、串行顺序与独立目录。
- 每例 `native.json`、`cleanup.json`、AX、初始/结果 PNG、Trace、turns、log、stdout/stderr：真实输入、输出、关闭与收尾。
- `host-window-witness.json`、`host-foreground.jsonl`、观察器及定向输入源码：宿主执行区间和操作边界。
- `independent-review.json`：第一组双例的独立只读核对，候选与全部原生证据哈希匹配。

当前真实窗口证据覆盖成功路径。失败、超时、取消、物理宿主拒绝与未清理前例拒绝由 `tools/test_native_macos_case.py` 的无 GUI 回归覆盖，不能把它们写成已完成来宾窗口场景。较新应用改动及发布必须绑定自己的准确候选重新验收。
