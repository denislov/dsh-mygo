# 通道协议（dsh-mygo channel）

## 职责

通道协议连接两件东西：

- **Go UI**：MyGo 进程，发起请求、消费事件、展示界面。
- **Host 插件**：`@your-scope/dsh-mygo-app`，在 DSH 的 Cordis 树中调用 agent/session/approval/user-questions 服务。

协议只描述消息、顺序与错误，不描述 UI 布局，也不复制 DSH 的业务方法。

## 传输

第一版：**stdio，newline-delimited JSON-RPC 2.0**。

- 一行一个 JSON 对象；UTF-8；不接收二进制帧。
- 请求：`{"jsonrpc":"2.0","id":1,"method":"session/create","params":{...}}`
- 响应：`{"jsonrpc":"2.0","id":1,"result":{...}}` 或 `{"jsonrpc":"2.0","id":1,"error":{"code":...,"message":"...","data":{...}}}`
- 通知：`{"jsonrpc":"2.0","method":"session/event","params":{...}}`
- 最大帧长：自定义 channel 第一版建议 `1 MiB`（附件另算，见“附件”）；SDK 验证期因为 assistant 事件内嵌完整 stream，使用 `16 MiB` 上限。上限必须可配置。
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

- `protocolVersion` 不兼容时，Host 返回错误并退出；UI 显示版本不匹配。
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
| `session/cancel` | `{ sessionId }` | `{}` |
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
| `approval/respond` | `{ requestId, decision: "allow"|"deny", remember? }` | `{}` |
| `question/respond` | `{ requestId, answers: Answer[] }` | `{}` |

`approval/respond` 与 `question/respond` 必须幂等：重复回答同一个 `requestId` 返回 `stale-request`，不改变已决结果。

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
| `approval/request` | `{ requestId, sessionId, ...request }` | 需要用户决定 |
| `question/request` | `{ requestId, sessionId, questions }` | 需要用户回答 |
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

- `revision` 在一次 Agent attach 生命周期内单调；`start.revision === 1` 表示新的生命周期。
- `chunk.index` 从 0 开始、密集递增；UI 发现缺口时丢弃当前 attempt，等待下一次 `start` 或提交事件。
- `end.committed` 表示随后会到达对应 `seq` 的提交事件；UI 用提交事件替换流式草稿。
- `end.abandoned` 表示没有提交事件；UI 丢弃该 attempt 的草稿。
- 重连时 Host 在 `session/resume` / `session/history` 结果中提供 baseline（`revision` + `activeAttempt`），UI 从 baseline 之后继续接收。

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

- Go → Host：请求队列有界；超过上限时 UI 显示“忙碌”，不无限堆积。
- Host → Go：`session/event` 是持久事件，不能丢；UI 侧队列满时暂停读取 stdout（stdio 自然背压）。
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
