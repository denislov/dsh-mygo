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
| `user/message`（`source.kind == "user"`） | `KindUser`，文本块拼接；image 块投影为 `Attachments` 元数据行 |
| `assistant/message` | `KindAssistant`，只取 text 块；`interrupted` 写入 `Detail` |
| `tool/call` | `KindTool`，按 `callId` 建行，参数格式化 JSON |
| `tool/result` | 更新同 `callId` 的工具行状态与结果；`error.reason` 优先于原始内容 |
| `turn/end`（非 `completed`） | `KindNotice`，显示取消/失败原因 |

约束：

- synthetic user context（`source.kind != "user"`）、system/developer 消息、reasoning、`assistant/attempt`、请求元数据不进入 transcript。
- 每个事件的 `seq` 去重；工具结果先到时保留稳定占位，之后仅按确切 callId 补齐，不按工具名/位置猜测。工具行身份始终是 `tool:<callId>`，补齐不覆盖已记录结果与错误状态。
- 自定义 channel 的 `session/create.session.nextSeq` 提供实时事件起点；打开旧会话时以已验证历史快照的 nextSeq 为起点。展示可以只是日志尾部，不能以显示行数推断事件边界。App 校验外层 `seq` 与 `event.seq` 一致，重复事件忽略；协商 `session.resync` 后实时缺口触发后台补齐，失败才停止 Host，旧 Host 保留手工重启恢复路径。
- 旧 SDK 回归模式仍消费 `session.event`；两种 envelope 共用相同提交事件投影，UI 不生成或持久化会话历史。
- Stop 不直接修改 transcript；DSH 提交的 `turn/end.reason.kind == "aborted"` 投影为取消 notice。若取消发生在回合完成之后，按实际提交结果展示，不补造取消行。取消后的同一 session 继续从原有 `nextSeq` 接收下一回合。
- `approval/request` / `approval/closed` 是连接内临时交互，由 `internal/interaction` 管理，不进入 transcript。审批结果只通过 Host 提交的工具结果体现；弹窗展示原始关联参数，不使用下述截断后的工具行。idle、失败和退出清理临时队列，不伪造审批历史。
- `question/request` / `question/closed` 同样由交互层管理，不把本地表单或取消动作写成历史。结构化答案通过真实 `ask_user_question` 工具结果进入 Host 日志；协议只支持阻塞提问，timed/continued 问题与迟到回复不进入投影。会话 idle 清理本会话提问，无 agent 请求不随会话 idle 消失。
- 列表预览按 rune 截断：消息 8000、工具参数/结果 4000；工具详情按下述独立字节预算保留，超限或淘汰必须提示，不冒充完整内容。打开历史也遵守相同展示上限，不承诺完整原文复制或按需加载被淘汰详情。
- 附件只投影显示元数据：`attachmentId`（不透明标识）、`mediaType`、`bytes`、`width`、`height`、可选 `name`；不读取或缓存图片字节，不把引用解释成路径。只有 png/jpeg/webp/gif 且引用字段有界的块才进入 UI，畸形引用丢弃而不是渲染可疑内容；纯图片消息同样保留一行，不能因文本为空而消失。附件不进入本地草稿文件。
- 助手 Markdown 增量展示及工具折叠/只读 diff；reasoning、工具参数不展示为临时正文。原生 Sessions 历史入口回放正文；usage/context 不展示。
- model/selection 是 Host 提交的持久选择意图，参与 nextSeq 顺序与历史回放，但不增加聊天正文行。Settings 以匹配的 Host 确认更新当前模型标签；真实请求配置仍由 DSH 生成。模型变更产生的 synthetic model-selection context 不是用户输入，不进入 transcript。
- session/title 参与持久 seq 校验，更新 Host 会话摘要而不增加聊天正文行；rename 返回的 nextSeq 包含该提交。列表更新时间来自已读持久事件时间，标题与排序以 Host 列表为准。归档状态由 DSH workspace registry 管理，不生成 transcript 行；取消归档后的历史仍按原序列恢复，不重写原日志。
- 当前会话重命名的 UI 确认必须匹配提交事件的标题和序列；RPC 超时或确认失败后，目录刷新与 history 回放一起重建安全边界，保留 composer 草稿，不把目录中的 nextSeq 直接当成已消费序列。后台目标重命名的事件不进入当前会话投影。
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
- `revision` 或 `chunk.index` 不连续时清除草稿；协商流式 baseline 后自动补齐，未协商时等待下次 `start` 或提交事件，不拼接不连续内容。版本高水位只在切换 Agent 或重启 Host 时重置。
- 提交事件（`assistant/message` / `assistant/attempt`）到达后，用其 `stream` 或 `message` 替换草稿；提交事件是权威。
- DSH 先提交再发 end，不能等待结束帧才去重。Go 按当前 turn/step 识别提交；end 先到时按 seq 等待提交。assistant/attempt 不伪造可见历史。临时行使用 `stream:<attemptId>`，提交行使用 durable seq；最终界面只有一份答案。
- history 补齐可以先包含某个 assistant 提交，再回放它的临时帧；迟到 `end.committed.seq` 小于当前 nextSeq 时清除对应临时 attempt，最终 idle 清理预览，不能在完整答案后重复显示一条 Streaming 行。
- `end.abandoned` 没有提交事件；丢弃草稿，必要时展示一次“已取消/已中断”提示。
- 流式期间不要重新解析整个 Markdown；用 `markdown.Stream` 的增量能力。

原生 `markdown.Stream` 增量显示，提交后用 `markdown.View`，不每个 token 重建解析器；具体复制与安全策略见 [UI 壳层](ui-shell.md)。每 attempt 最多保留 128 个块和 8000 个可见 rune，按块顺序拼接 text；block-end 的完整 text 可修正未提交前缀。reasoning/工具参数不进入临时文本缓存。idle、失败、关闭或 turn/end 清理临时状态；取消后的 Interrupted 文本只能来自 Host 提交。

### 重连

- `session/history` 通过 `session.assistant-stream-baseline` 协商并显式请求临时快照；`session/resume` 的运行中 attach 可复用该快照机制。字段与预算见 [通道协议](channel-protocol.md#流式-baseline)。
- Host 从公开帧保留有界文本/块状态，快照包含 revision、activeAttempt 的身份/turn/step/nextIndex、按序 blocks 与 truncated，不包含 reasoning/工具参数。持久提交同步清除匹配 attempt，历史首页同步捕获持久、状态及流式边界，分页固定。
- UI 从 baseline 的 revision/nextIndex 继续接收；旧帧不重放。基线在后台整体校验后与历史一起发布，不显示半份状态；校验缓冲中的流式连续性，后续缺口共用历史重读上限。
- activeAttempt 缺省表示流式空闲，回合状态仍由 status/statusRevision 决定；未协商 baseline 且收到缺少 start 的 chunk 时丢弃，等待新 start 或提交。baseline 文字截断与普通临时行共用 8000 rune/128 块限制，不落盘。

## 历史回放

`permission/preset`、`sandbox/mode`、`approval/policy` 作为 Host 所有的会话元数据参与 seq 连续性检查，不把它们伪装成用户消息。当前权限从 permission/get/select 快照读取，权限事件使旧显示失效；初始化、冷恢复和切换会话重新读取确切身份的快照。确认快照中的 nextSeq 必须等于已收到的持久事件边界；改变权限必须推进边界，幂等切换允许边界不变。

Host 通过 list/resume/history 提供历史，具体参数、快照边界和资源预算以 [通道协议](channel-protocol.md#host-历史能力) 为准。回放使用现有 Transcript.Apply，UI 不持久化副本。

会话列表/弹窗显式打开旧会话或新建。后台先校验 resume/create 的确切身份、idle 或运行中状态、模型路由与 nextSeq，再构建新的 Transcript。协商 session.history-window 时读取最近的有界事件窗口；旧 Host 从 seq 0 分页。完整验证请求范围和固定边界后才在主线程替换，失败保持旧会话/草稿。

加载目标会话期间缓冲其 durable/status/交互通知，成功后以历史 nextSeq 为边界有序消费，丢弃重复事件；运行中会话通过 baseline 续接临时流。回放完成前不允许发送新 prompt。运行中 attach、自动 resync 与流式 baseline 是历史回放的目标能力。

打开流程：

打开旧会话：

1. `session/resume` 恢复 Agent（不重发旧事件）。
2. `session/history` 分页拉取 `SessionEvent`。
3. 验证连续 `seq`，按日志顺序调用同一 Transcript.Apply，不排序掩盖缺口。
4. 打开完成后，接入实时 `session/event`；以最后一个历史 `seq` 为边界，避免重复。
5. 如果历史拉取期间收到实时事件，先缓冲；历史完成后按 `seq` 合并。

分页：

- `session/history` 使用 cursor/limit/direction；forward 从 seq 0，协商后的 backward 从尾部或 beforeSeq 读取，页内仍按升序应用同一 Transcript.Apply。
- 首屏与更早页各最多 64 条事件。更早页在后台投影，再在主线程前插；工具 call/result 跨页时按 callId 合并，保留较新结果与稳定身份。分页 startSeq 与实时 nextSeq 分开保存；seq 去重用合并区间，连续的不可见事件不逐条占用 map。
- 原生虚拟列表仅构建可见行；向上浏览时显式加载更早历史，已有阅读锚点保持，位于最新端时跳到新页。单份投影最多 4096 行、16 MiB 预览文本，工具详情仍共用 4 MiB；实时增长淘汰最旧展示，向前浏览溢出则淘汰较新展示并标为历史浏览状态，返回最新窗口前禁止发送。释放淘汰行的工具展开、检视和复制缓冲；不删除日志，任意更早 offset 都可继续加载。
- 返回最新、重启或 resync 重建尾部窗口，较早展示可再次加载；失败保留旧窗口与草稿。投影和原始历史不落本机文件。DSH Agent 冷恢复自身仍由上游重放完整日志，本协议不承诺 Host 的冷恢复内存与时延只与窗口大小相关。
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
- `resyncing` 表示事件流出现缺口，暂停发送并显示同步指示；可协商时自动用固定历史前缀与有界实时缓冲重建投影。读回失败、载荷非法或旧 Host 不支持时提供手工恢复；运行状态通过 statusRevision 校准，不用旧 idle 或 RPC 回调解锁仍在运行的会话。
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
| `seq` 缺口 | 协商后标记 `resyncing` 并自动补齐；无法安全恢复或旧 Host 不支持时显示恢复对话框 |
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
