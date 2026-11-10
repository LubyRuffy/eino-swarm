# 手机与 desktop 近期功能核对

核对范围：2026-11-01 至 2026-11-10，当前基线 `0007c4c3`。
本表区分已经可达的手机功能与待补齐的入口，不表示全部同步完成。

| 能力 | 手机状态 | 实现依据／缺口 |
| --- | --- | --- |
| 项目新建、名称、指令、工作目录、记忆、女娲开关 | 本次实现 | `mobile/src/components/project-manager.tsx`、`internal/remote/project_settings.go` |
| 女娲项目规划／执行模型组合 | 本次实现 | 同一项目编辑器及 PC 就绪模型目录；保存整组、关闭保留，后续任务使用 |
| MCP 长调用实时进度 | 已有 | `mobile/src/components/thread-blocks-mcp-progress.test.tsx`、`mobile/src/lib/transcript.ts`；不是本次新增 |
| Markdown 代码高亮 | 已有 | `mobile/src/components/markdown.tsx` 复用 `frontend/src/lib/highlighted-lines.tsx` |
| 文件上传、下载、HTML／表格／文档预览 | 已有 | `mobile/src/lib/stage-files.ts`、`mobile/src/components/document-preview.tsx`、`mobile/e2e/file-preview-controls.spec.ts`；手机不使用桌面本地文件选择器 |
| 官方账号、电脑选择、可读设备名 | 已有 | `mobile/src/components/gateway-account-shell.tsx`、`mobile/src/components/host-chrome.tsx`、`mobile/e2e/gateway-account.spec.ts` |
| 独立任务侧聊 | 待同步 | PC 有 `internal/engine/side_chat.go`；手机缺独立请求／取消／临时历史入口，不能借主对话 Send 代替 |
| 被移除服务的工作者模型显式恢复 | 待同步 | PC 有 `internal/engine/worker_model_recovery.go`；手机缺专用 RPC 和人工确认入口，普通模型切换不能替代恢复 |
| 女娲协作图和共享状态面板 | 待同步 | 手机可配置项目女娲模式，但没有 desktop 协作图入口 |
| 外部 Skills 包管理 | 待同步 | PC 已有独立包安装／加载接口；手机缺包目录与安装管理入口，不能与项目学习技能混为一体 |
| PC MCP 服务配置和工具检查 | 待同步 | 手机已有工具进度展示，缺配置与检查入口 |
| Mermaid 图预览、缩放 | 本次实现 | 配对对话与直连 Chat 复用 desktop 严格渲染；触控适应、缩放、原始尺寸和可复制源码 |
| 下一步消息建议 | 本次实现 | 配对 PC 空输入框显示最新已完成轮次建议，点击／Tab 填入草稿；遵守 PC 开关 |
| 点击选区外收起引用菜单 | 本次实现 | `mobile/src/components/thread-screen.tsx`；仍可重新选择并插入 |
| 纯空白消息不占布局 | 已有 | `mobile/src/components/thread-blocks.tsx` 与 `dropPackedJson` 保留事件、只过滤呈现 |
| PC 服务模型分类、上下文长度与默认服务设置 | 待核对并补齐 | 手机的直连模型源与配对 PC 设置是不同作用域，不能把手机模型配置写进 PC 或反向覆盖 |

项目管理入口：首页菜单 → 项目管理；新建会话 → 项目管理。
使用说明见 [mobile README](../mobile/README.md)，协议见 [API](API.md)。
测试包含真实 RPC dispatcher、PC 项目持久化、手机单测和 Chromium／WebKit
项目编辑流程；原生安装包、TestFlight 和设备验收是独立阶段。
