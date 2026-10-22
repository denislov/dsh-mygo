# 通道协议（dsh-mygo channel）

## 职责

通道协议连接两件东西：

- **Go UI**：MyGo 进程，发起请求、消费事件、展示界面。
- **Host 插件**：`@dsh-mygo/host`，在 DSH 的 Cordis 树中调用 agent/session/approval/user-questions 服务。

协议只描述消息、顺序与错误，不描述 UI 布局，也不复制 DSH 的业务方法。

## 当前实现（自定义 channel + 取消 + 人机交互 + 文本流式）

- UI/Host 发布版本 `0.1.0`、`protocolVersion: 1`、DSH `0.2.1-rc.2` 精确绑定；不匹配时拒绝进入 ready。
- 已实现 `initialize`、`session/create`、`session/prompt`、`session/cancel`、`approval/respond`、`question/respond`、`question/cancel`、`shutdown`；通知为 `session/event`、`session/assistant-stream`、`session/status`、`approval/request`、`approval/closed`、`question/request`、`question/closed`、`host/fatal`。
- UI 声明 `session.cancel`，Host 仅在双方支持时返回该 capability；未协商时调用取消返回 `protocol/capability-unavailable`。旧 Host 不声明时，UI 不展示 Stop。下文其余方法、附件、RPC 请求取消和 history 恢复仍是后续契约。
- 文本流式通过 `session.assistant-stream` capability 启用；Host 只转发自己拥有的确切 agent 的公开流式帧。旧 Host/SDK 保持仅提交模式，不自动降级协议。Go 只展示 text，reasoning/工具参数不进入临时行。
- `session/create` 当前要求已有绝对目录 `cwd`、`provider`、`model`，可选 `effort`；Host 验证模型路由后创建并拥有 session id。只接受自己创建的会话和 text content，忙碌时拒绝新 prompt。
- 创建结果中的 `session.nextSeq` 是创建完成时 DSH 的下一事件 offset。初始会话元数据不重放；后续完整提交事件从此 offset 开始转发。`session/event` 同时携带外层 `seq` 和原始 `event.seq`，两者必须相同。
- Go 忽略重复事件；缺口或无效载荷使连接失败并停止 Host。当前没有 `session.history` capability，因此不能自动补齐。
- 旧 SDK 的 `contentBlocks`、点分隔通知和隐式会话创建只在显式 `-sdk-protocol` 模式下启用，不自动降级。
- UI 声明 `approval` / `question`，Host 返回双方交集；未协商时对应交互不可用，不能调用回答/取消提问方法。无 agent 提问使用空 `sessionId`，不冒用当前会话身份。当前仅支持阻塞提问，不声明 timed/continued 回复能力。

## 传输

第一版：**stdio，newline-delimited JSON-RPC 2.0**。

- 一行一个 JSON 对象；UTF-8；不接收二进制帧。
- 请求：`{"jsonrpc":"2.0","id":1,"method":"session/create","params":{...}}`
- 响应：`{"jsonrpc":"2.0","id":1,"result":{...}}` 或 `{"jsonrpc":"2.0","id":1,"error":{"code":...,"message":"...","data":{...}}}`
- 通知：`{"jsonrpc":"2.0","method":"session/event","params":{...}}`
- 当前最大帧长为 `16 MiB`（包含换行）：流式通知落地后，提交事件仍保留内嵌完整 stream，不能缩小上限后丢弃持久记录。Go 用 `-max-frame`，Host 用 `mygo-channel.config.maxFrameBytes` 配置，范围需一致。
- 不接受空行、无换行的末帧、非法 UTF-8、非法 id 或混合请求/响应结构；`params` 仅接受对象。id 为不超过 128 字符的非空字符串或安全整数。
- 非法 JSON、未知帧结构、超长帧：关闭连接，Host 退出；UI 报协议错误。

未来如果需要多窗口或调试端口，可以在同一消息集上增加 loopback WebSocket transport；消息语义不变。

## 握手

### `initialize`

```json
{
  "protocolVersion": 1,
  "client": { "name": "dsh-mygo", "version": "0.1.0", "platform": "darwin", "locale": "zh-CN" },
  "capabilities": ["session.history", "approval", "question", "attachments", "settings.read"]
}
```

响应：

```json
{
  "server": { "name": "dsh-mygo-host", "version": "0.1.0", "dshVersion": "0.2.1-rc.2" },
  "protocolVersion": 1,
  "capabilities": ["session.history", "approval", "question", "attachments"],
  "permissionModes": ["workspace-write", "danger-full-access"],
  "models": [ ... ]
}
```

规则：

- `protocolVersion`、客户端 release 或 DSH 版本不兼容时，Host 返回错误并退出；UI 显示版本不匹配。协议/能力校验通过后才发送 `session/create`。
- capability 取双方交集；UI 不调用未协商的方法。
- `initialize` 之前只接受 `initialize` 与 `shutdown`。
- 重复 `initialize` 返回 `already-initialized`。

## 方法

### 会话

| 方法 | 参数 | 结果 |
| --- | --- | --- |
| `session/list` | `{ cursor?, limit? }` | `{ sessions: SessionSummary[], nextCursor? }` |
| `session/create` | `{ cwd, provider?, model?, effort?, preset? }` | `{ session: SessionSummary }` |
| `session/resume` | `{ sessionId }` | `{ session: SessionSummary }` |
| `session/rename` | `{ sessionId, title }` | `{ session: SessionSummary }` |
| `session/archive` | `{ sessionId }` | `{}` |
| `session/delete` | `{ sessionId }` | `{}`（需要 capability `session.delete`） |
| `session/fork` | `{ sessionId, messageId? }` | `{ session: SessionSummary }` |
| `session/history` | `{ sessionId, cursor?, limit?, direction? }` | `{ events: SessionEvent[], nextCursor?, hasMore }` |
| `session/prompt` | `{ sessionId, content: ContentBlock[] }` | `{ messageId }` |
| `session/cancel` | `{ sessionId }`（需 capability `session.cancel`） | `{ cancelled: boolean, status: "idle" }` |
| `session/queue` | `{ sessionId, action: "list"|"edit"|"remove"|"steer", ... }` | `{ queue: QueuedInput[] }` |
| `session/wait` | `{ sessionId }` | `{}`（回合结束后返回） |
| `session/working-directory/get` | `{ sessionId }` | `{ cwd }` |
| `session/working-directory/set` | `{ sessionId, path }` | `{ cwd }` |

`SessionSummary` 至少包含：`id`、`title`、`cwd`、`provider`、`model`、`effort`、`createdAt`、`updatedAt`、`status`、`parentId?`、`archived`。

`session/resume` 与 `session/history` 可以附带 `assistantStream` baseline，让 UI 在打开或重连时接上正在进行的 attempt；没有 baseline 时按“无活动 attempt”处理。

### 模型与设置

| 方法 | 参数 | 结果 |
| --- | --- | --- |
| `model/list` | `{}` | `{ providers: ProviderCatalog[] }` |
| `model/select` | `{ sessionId, provider, model, effort? }` | `{ session: SessionSummary }` |
| `settings/get` | `{ namespace? }` | `{ values }`（只返回非 secret） |
| `settings/set` | `{ namespace, values }` | `{ values }` |
| `settings/open-document` | `{ namespace }` | `{}`（可选；由 Host 打开原生编辑器） |

第一版可以只实现 `model/list`、`model/select` 和少量设置；其余用 profile patch 或 CLI。

### 交互

| 方法 | 参数 | 结果 |
| --- | --- | --- |
| `approval/respond` | `{ requestId, decision: "allow"|"deny" }`（需 `approval` capability） | `{}` |
| `question/respond` | `{ requestId, answers: { id, selected: string[], custom? }[] }`（需 `question` capability） | `{}` |
| `question/cancel` | `{ requestId }`（需 `question` capability） | `{}` |

`approval/respond` 与 `question/respond` 必须幂等：重复回答同一个 `requestId` 返回 `stale-request`，不改变已决结果。

审批和提问请求身份只由 Host 生成，回答只接受当前连接待处理的身份。审批不支持 `remember`。迟到/重复回答或取消返回 `approval/stale-request` / `question/stale-request`，不改变已决结果。问题/答案的校验、有界队列与作用域见 [交互](interaction.md)。

### 生命周期

| 方法 | 参数 | 结果 |
| --- | --- | --- |
| `shutdown` | `{}` | `{}`，随后 Host 退出 |

## 通知

| 通知 | 载荷 | 说明 |
| --- | --- | --- |
| `session/event` | `{ sessionId, seq, event }` | DSH session log 事件 |
| `session/assistant-stream` | `{ sessionId, frame }` | process-local assistant 帧：start / chunk / end |
| `session/status` | `{ sessionId, status: "running"|"idle" }` | 整回合状态 |
| `approval/request` | `{ requestId, sessionId, agentId, toolName, reason, external, callId?, arguments? }` | 需要用户决定；参数只来自活跃提交工具调用 |
| `approval/closed` | `{ requestId, sessionId }` | 请求已决或撤销；可能先于回答 RPC 响应到达 |
| `question/request` | `{ requestId, sessionId, questions }` | 需要用户回答 |
| `question/closed` | `{ requestId, sessionId }` | 已回答或撤销；可能先于 RPC 响应到达 |
| `subagent/started` | `{ parentSessionId, childSessionId }` | 子代理创建 |
| `subagent/finished` | `{ parentSessionId, childSessionId, reason }` | 子代理结束 |
| `host/notice` | `{ level, message }` | 非致命提示 |
| `host/fatal` | `{ message, diagnostic? }` | 致命错误，随后退出 |

### `session/assistant-stream`

复用 DSH `agent/assistant-stream` 的语义：

```ts
type AssistantStreamFrame =
  | { type: 'start'; attemptId; revision; turn; step }
  | { type: 'chunk'; attemptId; revision; index; time; chunk }
  | { type: 'end'; attemptId; revision; index
      outcome: { kind: 'committed'; eventType: 'assistant/message' | 'assistant/attempt'; seq }
             | { kind: 'abandoned' } }
```

- `revision` 在一次 Agent attach 生命周期内单调；每次 attach 从 1 开始。当前 Host 只创建会话，不替换/恢复 agent，Go 保留该连接的 revision 高水位并忽略重复/迟到帧；将来重连必须以明确的生命周期/baseline 重置，不能把旧 start 当成新答案。
- `chunk.index` 从 0 开始、密集递增；UI 发现缺口时丢弃当前 attempt，等待下一次 `start` 或提交事件。
- 真实 DSH 先提交对应 `seq` 的 `assistant/message` / `assistant/attempt`，再发送 `end.committed`，不是结束帧先到。Go 在同 turn/step 的提交到达时替换/清理草稿，迟到 end 不恢复旧行；也容忍 end 先到，保留前缀直到对应提交、idle 或退出，不把 end 本身当历史。
- `end.abandoned` 表示没有提交事件；UI 丢弃该 attempt 的草稿。
- 临时状态最多 128 个块、8000 个可见 Unicode 字符；超长文本只截断显示。帧结构/类型非法使连接失败；revision/index 缺口或块关联异常只丢弃当前临时 attempt，提交日志仍按原有 seq 校验继续。
- 重连 baseline（`revision` + `activeAttempt`）仍是后续设计，当前没有 `session/resume` / `session/history` capability。

## 事件顺序

- 每个 session 的 `session/event` 带单调递增的 `seq`。
- 同一 session 的 `session/status` 与 `session/event` 按 Host 发出顺序到达；Go 不重排。
- 跨 session 不保证全局顺序；UI 按 session 分开处理。
- Go 发现 `seq` 缺口时：
  1. 标记该 session 为 `resyncing`；
  2. 调用 `session/history` 拉取缺口范围；
  3. 合并后继续消费实时事件。
- 禁止把不连续的事件拼成“当前状态”。

## 取消与超时

- 回合取消已实现：Host 只接受自己拥有的 session，调用公开 `Agent.cancel({ kind: "user" })`，默认清空待处理输入，然后等待 `Agent.whenIdle()`。会话不会销毁，工具/模型的 abort signal 由 DSH 传播。
- 空闲取消返回 `{ cancelled: false, status: "idle" }`，不预先取消未来回合；活跃回合终止后返回 `{ cancelled: true, status: "idle" }`。取消期间的并发请求共用同一任务；后续重复取消等同空闲取消。
- Host 的 `cancelTimeoutMs` 默认 5000（可通过 channel config 配置）；未能确认 idle 时返回 `session/cancel-timeout`，不伪造成功，也不改变日志。Go 请求超时为 10 秒；失败后仍保持 working 并允许重试，最终 idle 由 Host 通知确认。
- Go 在 prompt 被 Host 接受后才发出取消，避免立即点击 Stop 时取消越过 prompt。取消期间保留下一条草稿并禁用重复点击；必须等取消请求返回且 Host 确认 idle 后才重新允许发送。提前收到 idle 只记录状态，不解除发送屏障，避免迟到的取消请求误停新回合。旧回合的迟到取消响应不能改变新回合。
- 取消提示来自 Host 提交的 `turn/end`（`aborted`），UI 不自行生成历史。若回合恰好已经完成，则保留实际完成结果。

以下是尚未实现的 RPC 请求级取消契约，独立于已实现的回合取消：

- 每个请求有 `id`；取消用 `$/cancel` 通知：`{ "id": <requestId> }`。
- `session/cancel` 取消当前回合，不取消“列出会话”这类请求。
- 请求超时由 Go 侧计时；超时后发送 `$/cancel`，并把 UI 操作标记为失败。
- 取消是尽力而为：Host 可能已经完成操作；响应中要说明实际结果。

## 错误

遵循 JSON-RPC 2.0：`error.code` 是整数，应用错误用 `data.kind` 细分。

```json
{
  "jsonrpc": "2.0",
  "id": 7,
  "error": {
    "code": -32000,
    "message": "Session is running",
    "data": { "kind": "session/busy", "sessionId": "..." }
  }
}
```

| `data.kind` 前缀 | 含义 |
| --- | --- |
| `protocol/*` | 帧、版本、未知方法、参数 |
| `session/*` | 会话不存在、忙、恢复失败 |
| `model/*` | 模型/effort 不可用 |
| `approval/*` | 请求过期、已决、未知 |
| `question/*` | 同上 |
| `host/*` | 内部失败、不可用 |

标准 JSON-RPC 错误码：`-32700` parse、`-32600` invalid request、`-32601` method not found、`-32602` invalid params、`-32603` internal。应用错误使用 `-32000..-32099`，并以 `data.kind` 区分。

规则：

- `message` 可以给用户看；`data` 只放非 secret 的结构化事实。
- 不把 stderr、堆栈、插件输出放进 `message`；放到 `diagnostic`（仅日志）。
- 未知 `data.kind` 按 `host/*` 处理，但保留原始值供日志。

## 附件

- 图片：`ContentBlock` 的 image 块，`data` 为 base64，`mimeType` 限定 png/jpeg/webp/gif。
- 文件：第一版用路径引用（Host 与 UI 同机），或 base64 小文件；路径必须由 UI 显式选择/拖放，Host 侧再次校验存在性与可读性。
- 单帧上限：base64 后不超过 `8 MiB`；总附件大小由 Host 的 attachment 服务再校验。
- 不在日志中打印附件内容或路径中的敏感片段。

## 背压

- Go → Host：当前 UI 在 busy 时禁用新 prompt；Host 顺序处理请求，并通过 `maxSessions`（默认 8）限制会话数量。
- Host → Go：通知按接收顺序进入同一有界批处理队列，16 ms、64 项或累计 256 KiB 时发布一批；最后一帧受单帧上限约束，可使该批超过字节阈值。最多等待 32 个 UI 批次，满时暂停读取 stdout；不静默丢弃事件或 token。Host 写队列由 `maxQueuedBytes`（默认 32 MiB）限制；超限关闭连接，使 UI 进入失败状态。
- RPC 结果的 UI 回调也经过同一队列，不能越过已经接收的提交通知或 idle。尤其取消响应不得提前解除发送屏障，让旧取消/idle 影响下一回合。每批只生成一次可见 transcript 快照，不逐 token 重建列表。
- 只影响 UI 的派生通知（例如百分比进度）可以合并/丢弃；持久事件不可以。

## 版本化

- `protocolVersion` 是整数；破坏性变更 +1。
- 新方法/通知通过 capability 声明；旧 Host 不声明时 UI 不调用。
- 删除方法时先保留一个版本的兼容路径，再在新 major 中移除。
- Host 与 UI 的发布版本绑定；协议版本是额外保险，不是独立升级承诺。

## 测试

- 协议 conformance：合法帧、非法帧、未知方法、版本不匹配、取消、重复回答。
- 顺序测试：乱序/缺口/重连后的 history 合并。
- 背压测试：大事件流、慢 UI、附件上限。
- 真实 Host 集成：用 `dsh --profile mygo` 跑一个回合，验证 approval/question 往返。

## 相关文档

- [宿主监督](host-supervisor.md)
- [会话投影](session-projection.md)
- [交互](interaction.md)
- [传输方案研究](../research/transport-options.md)
