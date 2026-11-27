# 统一图谱内核设计

状态：已落地。本文件记录设计决定；当前行为以 [GRAPH.md](../GRAPH.md) 和契约台账为准。

## 目标

用一个图谱内核取代三套同构的存储：工作记录、知识库（含 Wiki 建图）。模型通过两个工具
`update_graph` 和 `read_graph` 在工作过程中实时维护一张主体图和一张有序 to-do 清单。
我们只提供方法论，任务本身由模型动态生成和推进。

## 不变量（由 `models/graph/GraphStore.tla` 检查）

- 属性合并是增量的，不需要版本号；覆盖和改状态必须带当前版本（`NoLostUpdate`、`AdditiveKeepsAttributes`）。
- 同一个工具调用 ID 重放时，只有请求体完全相同才返回原回执（`ReplayNeedsSameBody`）。
- 一次写入同时命中两个不同主体时拒绝，由模型决定（`RefusesTwoSubjects`）。
- `confirmed` 必须带证据（`ConfirmedHasEvidence`）。
- 身份键的值不能改，只能补充新的身份键（`IdentityKeyStable`）。
- 归属被删除后，迟到的写入不落库（`NoWriteAfterDelete`）。

模型只检查协议，不证明 Go 代码的原子性；真实的事务、并发和重放由 `internal/store` 的测试在真实 SQLite 上回归。
to-do 的重排没有进入模型：它是单事务内对一个有序列表的整体改写，由存储测试覆盖并发重排。

## 数据

五张表：`graph_subjects`、`graph_subject_keys`、`graph_links`、`graph_todos`、`graph_revisions`。
归属是项目（会话属于项目时）或会话，在回合开始时冻结。身份键表按
`(owner, kind, normalizer, value)` 唯一，写入时任一身份键命中就合并到已有主体。

## 主体类型注册表

`frontend/src/graph/kinds.json` 是 Go、桌面和手机共用的目录。每个条目声明类型 ID、中英文名称、
属性、身份键（可由几个属性组合）、每个身份键的归一器和显示名属性。新增类型只需加条目；
只有需要新的归一规则时才改 Go。类型清单只出现在 `update_graph` 的参数定义里，不进提示词。

## to-dos

一张有序清单，不是图里的节点。状态 `open`、`doing`、`done`、`failed`。位置就是优先级，
写入时用 `before` 指定放到哪一条前面。结算一条 to-do 时，同批写入的主体和连线记到它名下。
主体的分析状态不存储，由针对它的 to-dos 推出。

## 过程中实时更新

每次模型请求注入一段有预算的摘要和"自上次被接受的 `update_graph` 以来收到多少工具结果"的计数。
每次提交立刻发 `graph_updated` 具名帧。回合结束不拦截，只把剩余未记录数写进 Trace。

## 不做的事

- 不导入旧数据：旧表和知识库 Markdown 留在磁盘上，不再显示。
- 不做自动节点合并、不为每个新主体自动生成 to-do、不在宿主里按关键词判断内容。
