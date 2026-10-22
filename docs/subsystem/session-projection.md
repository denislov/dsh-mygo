# 会话投影（Session Projection）

## 职责

把 Host 发来的两类数据投影成 UI 可渲染的不可变状态：

- `session/assistant-stream`：进程内、临时的逐字流。
- `session/event`：DSH session log 的持久事件。

投影层不产生新的会话真相，也不把 UI 状态回写 Host。打开旧会话时，先回放历史，再接实时事件。

## 当前实现（P1 + 自定义 channel 阶段 1）

`internal/projection` 只消费提交后的 `session/event`，输出扁平的 `Item` 列表：

| 事件 | 投影 |
| --- | --- |
| `user/message`（`source.kind == "user"`） | `KindUser`，文本块拼接 |
| `assistant/message` | `KindAssistant`，只取 text 块；`interrupted` 写入 `Detail` |
| `tool/call` | `KindTool`，按 `callId` 建行，参数格式化 JSON |
| `tool/result` | 更新同 `callId` 的工具行状态与结果；`error.reason` 优先于原始内容 |
| `turn/end`（非 `completed`） | `KindNotice`，显示取消/失败原因 |

约束：

- synthetic user context（`source.kind != "user"`）、system/developer 消息、reasoning、`assistant/attempt`、请求元数据不进入 transcript。
- 每个事件的 `seq` 去重；工具结果先到时单独成行，不猜测配对。
- 自定义 channel 的 `session/create.session.nextSeq` 提供实时事件起点；App 校验外层 `seq` 与 `event.seq` 一致，重复事件忽略，缺口停止 Host 并报错。history capability 落地前不尝试拼接缺失事件。
- 旧 SDK 回归模式仍消费 `session.event`；两种 envelope 共用相同提交事件投影，UI 不生成或持久化会话历史。
- 文本按 rune 截断：消息 8000、工具内容 4000；完整历史与展开查看留给 P2。
- 无逐字流式、无 history、无 usage/context 展示、无 diff。

## 真相源

- DSH session log 是唯一真相。
- 流式帧不是持久真相；`end.committed` 指向的 `assistant/message` / `assistant/attempt` 才是。
- UI 的滚动位置、折叠状态、选中的会话、输入草稿属于本地 UI 状态，不进入 session log。
- 模型可见的输入必须由 Host 写入 session log；UI 不得构造隐藏上下文。

## 事件分类

DSH 核心事件（`SessionEventMap`）：

| 事件 | 投影 |
| --- | --- |
| `turn/start` / `turn/end` | turn 边界、结束原因 |
| `step/start` / `step/end` | 模型调用分段；通常不直接展示 |
| `user/message` | 用户消息（区分 human / synthetic / goal continuation） |
| `developer/message` | 开发者/系统注入，按需折叠 |
| `system/message` | 系统提示，默认不展示 |
| `assistant/message` | 完成的 assistant 消息、usage、`interrupted` |
| `assistant/attempt` | 未产生可见消息的尝试；用于错误/重试展示 |
| `tool/call` | 工具调用开始（名称、原始参数 JSON） |
| `tool/result` | 工具结果、错误、`meta`（diff 等展示数据） |
| `request/header` / `request/context` | 请求元数据；用于诊断，不直接展示 |
| `session/end-seed` | fork/恢复边界 |

插件扩展事件按类型前缀处理，常见类别：

- `session/title`：标题。
- `compaction/*`：压缩开始/结束/摘要。
- `goal/*`、`todo/*`、`plan/*`：目标、待办、计划。
- `subagent/*`：子代理关系与状态。
- `session/*-delivery`、telemetry：日志/诊断，默认不展示。

未知事件类型必须被容忍：忽略并记录一次诊断，不要让 UI 崩溃。

## UI 数据模型

```go
type SessionView struct {
    ID        string
    Title     string
    Cwd       string
    Provider  string
    Model     string
    Effort    string
    Status    Status // idle | running | resyncing | failed
    Turns     []TurnView
    Usage     UsageView
    Context   ContextView
    Pending   []QueuedInput
}

type TurnView struct {
    Number int
    Reason string
    Items  []ItemView
}

type ItemView interface{ item() }

type UserMessageView struct { Seq uint64; Blocks []ContentBlock; Source string }
type AssistantView struct {
    Seq       uint64
    Text      string // 流式时为草稿，提交后替换
    Reasoning string
    Streaming bool
    Interrupted bool
    Usage     *UsageView
}
type ToolCallView struct {
    CallID    string
    Name      string
    Arguments string
    Status    ToolStatus // pending | running | done | error
    Result    *ToolResultView
    Diff      *DiffView   // 从 tool/result.meta 提取
}
type NoticeView struct { Kind string; Text string; Level Level }
```

字段名是实现建议，不是协议契约；协议契约以 [通道协议](channel-protocol.md) 为准。

## 流式与提交的合并

### 状态机

```text
idle
  └─ start(attemptId, revision, turn, step)
       └─ chunk(index 0..n)  → 追加到 AssistantView.Streaming
            ├─ end.committed(seq) → 提交事件到达后替换草稿 → idle
            └─ end.abandoned      → 丢弃草稿 → idle
```

规则：

- `start.revision === 1` 表示新的 Agent attach 生命周期；清空当前 attempt。
- `revision` 不连续时，丢弃当前 attempt，等待下一次 `start` 或提交事件。
- `chunk.index` 必须密集递增；缺口时丢弃草稿（不要拼接不连续内容）。
- 提交事件（`assistant/message` / `assistant/attempt`）到达后，用其 `stream` 或 `message` 替换草稿；提交事件是权威。
- `end.abandoned` 没有提交事件；丢弃草稿，必要时展示一次“已取消/已中断”提示。
- 流式期间不要重新解析整个 Markdown；用 `markdown.Stream` 的增量能力。

### 重连

- `session/resume` / `session/history` 可返回 `assistantStream` baseline：`revision` + `activeAttempt`（含已累计 stream）。
- UI 从 baseline 的 `revision` 继续接收；baseline 之前的内容不重放。
- 没有 baseline 时按 idle 处理；如果随后收到 `chunk` 而没有 `start`，丢弃并等待 `start`。

## 历史回放

打开旧会话：

1. `session/resume` 恢复 Agent（不重发旧事件）。
2. `session/history` 分页拉取 `SessionEvent`。
3. 按 `seq` 排序，投影为 `TurnView` / `ItemView`。
4. 打开完成后，接入实时 `session/event`；以最后一个历史 `seq` 为边界，避免重复。
5. 如果历史拉取期间收到实时事件，先缓冲；历史完成后按 `seq` 合并。

分页：

- `session/history` 使用 `cursor` + `limit` + `direction`；第一版从最新往回翻，或从最旧往后翻，固定一种。
- UI 只保留窗口内的 turn；向上滚动时再拉上一页。
- 历史页与实时事件的投影函数必须相同，避免两套渲染。

## 工具调用与 diff

- `tool/call.arguments` 是模型产出的原始 JSON 字符串；展示前解析，解析失败时显示原文。
- `tool/result.message` 是模型可见结果；`error` 是失败身份与用户可读原因；`meta` 是工具私有展示数据。
- 文件编辑工具可能在 `tool/result.meta` 中携带 diff；有则用 `editor.DiffView`，没有则退化为文本 diff 或纯文本结果。
- 工具结果可能很大：默认折叠，按需展开；不要一次性放进 Markdown 渲染。
- 同一 `callId` 的 call/result 必须配对；结果先到（异常）时保留占位并在结果到达后补齐。

## 状态与使用量

- `session/status` 驱动“正在工作”指示、输入禁用/排队、托盘/通知。
- `assistant/message.usage` 提供 token 统计；`request/context` 提供上下文容量。
- compaction 事件清除过期的上下文估算，并展示压缩摘要。
- 不要在 UI 侧重新计算 token；只展示 Host 提供的数字。

## 交互状态

投影层持有：

- 当前会话的 transcript。
- 每个 turn 的展开/折叠。
- 工具卡片的展开/折叠。
- 流式草稿。
- 待处理的 approval / question 引用（真正的请求由 [交互](interaction.md) 管理）。

不持有：

- 模型上下文。
- 工具执行状态机。
- 会话持久化。

## 性能

- 只渲染可视区域：`ui.List` 只构建可见行。
- 每个事件只更新受影响的行；避免整棵 transcript 重建。
- 流式 chunk 合并到固定频率（例如每帧一次）再触发 `Window.Update`，不要每个 token 都重绘。
- 保留有界缓存：折叠的历史、工具输出、diff；超出后按需重新拉取。
- 不在投影层做网络/磁盘 I/O。

## 失败模式

| 失败 | 处理 |
| --- | --- |
| 未知事件类型 | 忽略 + 一次性诊断日志 |
| `seq` 缺口 | 标记 `resyncing`，拉 history，合并后恢复 |
| 流式帧乱序/缺口 | 丢弃当前 attempt，等待 `start` |
| 提交事件缺失 | 保留草稿并在 turn 结束时标记“未确认” |
| 工具参数 JSON 非法 | 展示原文 + 解析错误 |
| 历史页损坏 | 展示可操作错误；不猜测内容 |

## 测试

- 用 fixture session log 驱动投影，断言 turn/item/tool/diff 结构。
- 用流式帧序列测试 start/chunk/end 合并、缺口、abandoned、重连 baseline。
- 用历史 + 实时混合序列测试去重与顺序。
- 用超长工具输出/大 diff 测试折叠与性能。
- 用未知事件测试容忍性。

## 相关文档

- [通道协议](channel-protocol.md)
- [UI 壳层](ui-shell.md)
- [交互](interaction.md)
- [架构](../architecture.md)
