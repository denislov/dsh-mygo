# 会话投影（Session Projection）

## 职责

把 Host 发来的两类数据投影成 UI 可渲染的不可变状态：

- `session/assistant-stream`：进程内、临时的逐字流。
- `session/event`：DSH session log 的持久事件。

投影层不产生新的会话真相，也不把 UI 状态回写 Host。打开旧会话时，先回放历史，再接实时事件。

## 投影规则

`internal/projection.Transcript` 消费提交后的 `session/event`，输出扁平的 `Item` 列表；`AssistantStream` 单独折叠临时流式帧，绝不写入 Transcript：

| 事件 | 投影 |
| --- | --- |
| `user/message`（`source.kind == "user"`） | `KindUser`，文本块拼接 |
| `assistant/message` | `KindAssistant`，只取 text 块；`interrupted` 写入 `Detail` |
| `tool/call` | `KindTool`，按 `callId` 建行，参数格式化 JSON |
| `tool/result` | 更新同 `callId` 的工具行状态与结果；`error.reason` 优先于原始内容 |
| `turn/end`（非 `completed`） | `KindNotice`，显示取消/失败原因 |

约束：

- synthetic user context（`source.kind != "user"`）、system/developer 消息、reasoning、`assistant/attempt`、请求元数据不进入 transcript。
- 每个事件的 `seq` 去重；工具结果先到时保留稳定占位，之后仅按确切 callId 补齐，不按工具名/位置猜测。工具行身份始终是 `tool:<callId>`，补齐不覆盖已记录结果与错误状态。
- 自定义 channel 的 `session/create.session.nextSeq` 提供实时事件起点；打开旧会话时以完整 history 的 nextSeq 为起点。App 校验外层 `seq` 与 `event.seq` 一致，重复事件忽略，实时缺口停止 Host 并报错，不自动 resync。
- 旧 SDK 回归模式仍消费 `session.event`；两种 envelope 共用相同提交事件投影，UI 不生成或持久化会话历史。
- Stop 不直接修改 transcript；DSH 提交的 `turn/end.reason.kind == "aborted"` 投影为取消 notice。若取消发生在回合完成之后，按实际提交结果展示，不补造取消行。取消后的同一 session 继续从原有 `nextSeq` 接收下一回合。
- `approval/request` / `approval/closed` 是连接内临时交互，由 `internal/interaction` 管理，不进入 transcript。审批结果只通过 Host 提交的工具结果体现；弹窗展示原始关联参数，不使用下述截断后的工具行。idle、失败和退出清理临时队列，不伪造审批历史。
- `question/request` / `question/closed` 同样由交互层管理，不把本地表单或取消动作写成历史。结构化答案通过真实 `ask_user_question` 工具结果进入 Host 日志；协议只支持阻塞提问，timed/continued 问题与迟到回复不进入投影。会话 idle 清理本会话提问，无 agent 请求不随会话 idle 消失。
- 列表预览按 rune 截断：消息 8000、工具参数/结果 4000；工具详情按下述独立字节预算保留，超限或淘汰必须提示，不冒充完整内容。打开历史也遵守相同展示上限，不承诺完整原文复制或按需加载被淘汰详情。
- 助手 Markdown 增量展示及工具折叠/只读 diff；reasoning、工具参数不展示为临时正文。原生 Sessions 历史入口回放正文；usage/context 不展示。
- model/selection 是 Host 提交的持久选择意图，参与 nextSeq 顺序与历史回放，但不增加聊天正文行。Settings 以匹配的 Host 确认更新当前模型标签；真实请求配置仍由 DSH 生成。模型变更产生的 synthetic model-selection context 不是用户输入，不进入 transcript。
- configuration/default-model 和凭据写入属于 Host profile 配置，不产生聊天/模型选择事件，不推进当前 session nextSeq。默认模型只在显式 useDefaultModel 的新建/冷恢复中读取；会话标签始终取实际 Host session 路由，不把配置快照投影成当前路由。
- 助手保留文本块的前导空白与末尾换行，不用 TrimSpace 改写代码缩进/开放围栏；多文本块仍以两个换行连接。临时和提交投影均显式标记 Truncated，截断上限仍为 8000 rune（省略标记另计），不保留第二份完整历史或推断缺失内容。

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
| `assistant/message` | 完成的 assistant 消息、usage 与 `interrupted` 标记 |
| `assistant/attempt` | 未产生可见消息的尝试；用于错误/重试展示 |
| `tool/call` | 工具调用开始（名称、原始参数 JSON） |
| `tool/result` | 工具结果、错误、`meta`（diff 等展示数据） |
| `request/header` / `request/context` | 请求元数据；用于诊断，不直接展示 |
| `session/end-seed` | fork/恢复边界 |

插件扩展事件按类型投影：

- `session/title`：会话标题与最近活动时间。
- `compaction/*`：压缩开始/结束、摘要与上下文变化。
- `goal/*`、`todo/*`、`plan/*`：目标、待办、计划面板。
- `subagent/*`：父子会话关系、运行状态与结果入口。
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
    Seq         uint64
    Text        string // 流式时为草稿，提交后替换
    Streaming   bool
    Interrupted bool
    Usage       *UsageView
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

- 每个 Host 所有的 agent 只有一个 attach 生命周期，revision 从 1 开始并保持高水位；重复/迟到帧忽略。新 start 可作为缺口后的恢复点；不因旧 start.revision == 1 回放旧答案。协议不提供重连/替换 agent 的显式 reset。
- `revision` 不连续时，丢弃当前 attempt，等待下一次 `start` 或提交事件。
- `chunk.index` 必须密集递增；缺口时丢弃草稿（不要拼接不连续内容）。
- 提交事件（`assistant/message` / `assistant/attempt`）到达后，用其 `stream` 或 `message` 替换草稿；提交事件是权威。
- DSH 先提交再发 end，不能等待结束帧才去重。Go 按当前 turn/step 识别提交；end 先到时按 seq 等待提交。assistant/attempt 不伪造可见历史。临时行使用 `stream:<attemptId>`，提交行使用 durable seq；最终界面只有一份答案。
- `end.abandoned` 没有提交事件；丢弃草稿，必要时展示一次“已取消/已中断”提示。
- 流式期间不要重新解析整个 Markdown；用 `markdown.Stream` 的增量能力。

原生 `markdown.Stream` 增量显示，提交后用 `markdown.View`，不每个 token 重建解析器；具体复制与安全策略见 [UI 壳层](ui-shell.md)。每 attempt 最多保留 128 个块和 8000 个可见 rune，按块顺序拼接 text；block-end 的完整 text 可修正未提交前缀。reasoning/工具参数不进入临时文本缓存。idle、失败、关闭或 turn/end 清理临时状态；取消后的 Interrupted 文本只能来自 Host 提交。

### 重连

- `session/resume` / `session/history` 可返回 `assistantStream` baseline：`revision` + `activeAttempt`（含已累计 stream）。
- UI 从 baseline 的 `revision` 继续接收；baseline 之前的内容不重放。
- 没有 baseline 时按 idle 处理；如果随后收到 `chunk` 而没有 `start`，丢弃并等待 `start`。

## 历史回放

Host 通过 list/resume/history 提供历史，具体参数、快照边界和资源预算以 [通道协议](channel-protocol.md#host-历史能力) 为准。回放使用现有 Transcript.Apply，UI 不持久化副本。

会话列表/弹窗显式打开旧会话或新建。后台先校验 resume/create 的确切身份、idle 或运行中状态、模型路由与 nextSeq，再从 seq 0 分页构建新的 Transcript；所有页成功并确认固定边界后，在主线程整体替换旧投影。加载失败不保留或展示部分新历史，旧会话/草稿保持原样。

加载目标会话期间缓冲其 durable/status/交互通知，成功后以历史 nextSeq 为边界有序消费，丢弃重复事件；运行中会话通过 baseline 续接临时流。回放完成前不允许发送新 prompt。运行中 attach、自动 resync 与流式 baseline 是历史回放的目标能力。

打开流程：

打开旧会话：

1. `session/resume` 恢复 Agent（不重发旧事件）。
2. `session/history` 分页拉取 `SessionEvent`。
3. 验证连续 `seq`，按日志顺序调用同一 Transcript.Apply，不排序掩盖缺口。
4. 打开完成后，接入实时 `session/event`；以最后一个历史 `seq` 为边界，避免重复。
5. 如果历史拉取期间收到实时事件，先缓冲；历史完成后按 `seq` 合并。

分页：

- `session/history` 使用 `cursor` + `limit` + `direction`；Host 固定从 seq 0 向前分页。
- 首屏完成有界回放后，用原生虚拟列表仅构建可见行；向上滚动时按 cursor 增量读取更早历史，不重放整段日志。投影与原始历史不落本地文件。
- 历史页与实时事件的投影函数必须相同，避免两套渲染。

## 工具调用与 diff

- `tool/call.arguments` 是模型产出的原始 JSON 字符串；展示前解析，解析失败时显示原文。
- `tool/result.message` 是模型可见结果；`error` 是失败身份与用户可读原因；`meta` 是工具私有展示数据。
- 投影识别 DSH `FileDiff`：`meta.diffs: [{ path, oldText: string|null, newText: string }]`。每项是记录的上下文 hunk，不是整文件快照；同一路径可有多个 hunk。`oldText: null` 仅表示此 hunk 无旧文本，不据此断言文件创建。
- 新建文件可能只有 `operation: create` 和空 `diffs`，显示创建状态，不从参数或磁盘合成 diff。空非创建 diff 显示无文本 hunk；缺少/非法/超限元数据回退结果文本，不展示半份有效元数据。
- 路径只展示，不 resolve、打开、执行或读取；本地投影不重新计算文件真相，不改 Host/协议。元数据对象最多 256 KiB、16 个 hunk，路径最多 4096 UTF-8 字节，全部前后文本与路径共最多 128 KiB；字段类型和 NUL 校验失败时整批回退。
- 工具卡片默认折叠，标题和运行/完成/错误状态常驻；展开看短预览，`View details` 切换 Arguments / Result / Changes。结果按可见内容保留，错误原因独立于原始结果显示，不因错误覆盖原始结果详情。
- 每项参数/结果各最多保留 64 KiB，按 UTF-8 边界截断并明确标记。Transcript 额外详情缓存预算 4 MiB，超限淘汰最旧详情，保留列表预览和淘汰提示；不自动重新拉取，不能把残留预览称为完整历史。长/复杂 JSON 参数不全量缩进，避免预览放大。
- 原生 `editor.DiffView` 仅按需构建一个只读 hunk，禁用编辑和 Revert/Keep；前后文本合计超过 16 KiB 或 400 个换行时不在 UI 线程计算 diff，改为明确标注的只读 Before/After 文本。切换 hunk、结果更新或缓存淘汰会替换/释放旧缓冲，关闭检视器清理全部检视状态。
- 同一 `callId` 的 call/result 必须配对；结果先到（异常）时保留占位并在结果到达后补齐。

## 状态与使用量

- `session/status` 驱动“正在工作”指示、输入禁用/排队、托盘/通知。
- `assistant/message.usage` 提供 token 统计；`request/context` 提供上下文容量。
- `resyncing` 表示事件流出现缺口，暂停发送并显示同步指示；本版本通过重启 Host 后回放 history 恢复。
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
- App 将通知与 RPC UI 回调按序合并，16 ms / 64 项 / 256 KiB 任一阈值触发批次，最多等待 32 个批次再背压 stdout；每批只发布一次组合后的 Items。慢 UI 不丢提交事件，停止会打断背压等待。
- 保留有界缓存：折叠的历史、工具输出、diff；超出后按需重新拉取。
- 不在投影层做网络/磁盘 I/O。

## 失败模式

| 失败 | 处理 |
| --- | --- |
| 未知事件类型 | 忽略 + 一次性诊断日志 |
| `seq` 缺口 | 标记 `resyncing`，暂停发送并显示恢复对话框；重启 Host 后用 history 回放恢复 |
| 流式帧乱序/缺口 | 丢弃当前 attempt，等待 `start` |
| 提交事件缺失 | end 本身不成为历史；保留前缀等待提交，idle/turn 结束时清理，不伪造最终答案 |
| 工具参数 JSON 非法 | 展示原文 + 解析错误 |
| 历史页损坏 | 展示可操作错误；不猜测内容 |

## 测试

- 用 fixture session log 驱动投影，断言 turn/item/tool/diff 结构。
- 用流式帧序列测试 start/chunk/end 合并、缺口与 abandoned。
- 用历史 + 实时混合序列测试去重与顺序。
- 用超长工具输出/大 diff 测试折叠与性能。
- 用未知事件测试容忍性。

## 相关文档

- [通道协议](channel-protocol.md)
- [UI 壳层](ui-shell.md)
- [交互](interaction.md)
- [架构](../architecture.md)
