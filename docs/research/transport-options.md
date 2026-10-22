# Host 与 UI 传输方案研究

## 结论

采用 **路线 A 验证 → 路线 C 落地**：

- **A（验证）**：先用 `dsh-sdk-app` 的 stdio JSON-RPC 跑通“Go 启动 Host → 发 prompt → 收 session 事件 → 渲染”。
- **C（落地）**：产品阶段使用 dsh-mygo 自己的 channel 协议，补上审批、用户提问、历史回放、设置与模型选择。

SDK 与 ACP 都是“能跑起来”的协议，但它们的设计目标不是桌面 UI；直接把它们当产品协议会缺关键交互。

## 需求

一个桌面 UI 对 Host 的需求：

| 需求 | 说明 |
| --- | --- |
| 生命周期 | 启动、初始化、就绪、优雅关闭、崩溃恢复 |
| 会话 | 创建、恢复、列表、重命名、归档/删除、fork |
| 回合 | prompt、排队、取消、等待空闲 |
| 流式事件 | assistant 文本、reasoning、工具调用/结果、状态 |
| 历史 | 打开旧会话时回放 transcript，分页/游标 |
| 审批 | 工具执行前的 allow/deny，带请求详情 |
| 用户提问 | 模型 `ask_user_question` 的选项/自由文本回答 |
| 模型与 effort | 列表、选择、按会话生效 |
| 设置与凭据 | 读取/写入设置，凭据只写不读 |
| 附件 | 图片/文件，大小与类型限制 |
| 后台任务/子代理 | 列表、状态、取消 |
| 版本协商 | UI 与 Host 独立升级时的能力检测 |
| 错误 | 结构化错误、取消、超时、不可用 |

## 方案 A：SDK stdio JSON-RPC

DSH 的 `@deepseek-ai/dsh-sdk-app` profile 挂载 `@deepseek-ai/dsh-sdk-jsonrpc-server`，通过 stdin/stdout 提供 newline-delimited JSON-RPC 2.0。

### 方法

client → server：

- `initialize`
- `session/prompt`
- `session/resume`
- `session/wait`
- `session/working-directory/get`
- `session/working-directory/set`
- `shutdown`

server → client notifications：

- `session.event`（所有 session 的完整事件信封）
- `session.status`（running / idle）
- `subagent.started`
- `subagent.finished`

### 优点

- 协议小、实现现成、已有 TypeScript/Python 客户端。
- 提交事件完整：`session.event` 带完整 `SessionEvent`，足以做 transcript 投影。
- 进程生命周期与 stdio 绑定，简单可靠。
- 适合 2–5 天验证最关键的链路。

### 缺口

- **没有 live token 流**：`session.event` 只发送已提交的 session log 事件；assistant 文本在 step 结束的 `assistant/message` 里才出现。实时逐字渲染需要 `agent/assistant-stream`（process-local 事件），SDK server 不转发它。路线 A 只能验证“提交后渲染”，不能验证流式。
- 没有审批请求。`@deepseek-ai/dsh-sdk-protocol` 明确记录 server→client request 是“dead capability”，没有 approval 流程。
- 没有用户提问（`ask_user_question`）。
- 没有会话列表、重命名、删除、fork。
- 没有历史回放：`session/resume` 恢复会话但不重发旧事件。
- 没有模型/effort 列表与选择（`initialize` 时固定）。
- 没有设置/凭据管理。
- 没有附件上传的独立通道；图片只能作为 prompt 的 base64 块。

### 适用

验证 Host 启动、prompt、事件流、取消、退出。**不作为产品协议**。

## 方案 B：ACP stdio

`@deepseek-ai/dsh-acp-app` 挂载标准 ACP v1 server。它是自动化协议，不是 DSH 专属 UI 协议。

### 方法

- `initialize`
- `authenticate`
- `session/new`
- `session/list`
- `session/resume`
- `session/close`
- `session/set_config_option`（model、reasoning_effort）
- `session/prompt`
- `session/cancel`
- `session/update`（committed assistant messages、thoughts、tool lifecycle、config、context usage）
- `session/request_permission`（标准权限请求，client 可自动回答）

### 优点

- 标准协议，Go 侧可以复用 ACP 客户端实现。
- 有权限请求，覆盖工具审批。
- 有 session list/resume、模型/effort 选择。
- 多会话、MCP、图片 prompt。

### 缺口

- 不支持 elicitation（`ask_user_question`）。
- 不支持历史回放（`session/load` 不在 surface 内）。
- 不支持 fork、删除、额外目录、plan/terminal/presentation。
- 没有设置/凭据/插件管理。
- ACP 的 `session/update` 是语义投影，不是 DSH 原始事件；渲染 DSH 专属卡片需要额外映射。

### 适用

如果想要“标准协议 + 审批”，ACP 是比 SDK 更完整的第一版；但它仍不足以支撑完整桌面产品。

## 方案 C：自定义 channel（选择）

定义 dsh-mygo 自己的协议，Host 侧是一个 Cordis 插件，Go 侧是一个 JSON-RPC client。

### 形态

参考 dsh-TUI 的 Channel 语义（`tui-profile/notes/0007-tui-channel.md`）：

- `open`：创建/附着 channel，返回完整 snapshot。
- `subscribe`：从指定 version 订阅，先返回 snapshot，再发增量。
- `invoke`：调用一个方法，返回 JSON 结果。
- `close`：释放 channel。

dsh-mygo 可以更简单：一个连接、多个 session、每个 session 单调 sequence；不一定要实现完整的 Channel 抽象。

### 建议的方法集

| 方法 | 作用 |
| --- | --- |
| `initialize` | 协议版本、客户端信息、locale、能力协商 |
| `session/list` | 会话摘要，分页 |
| `session/create` | 新建会话（provider/model/effort/cwd） |
| `session/resume` | 恢复持久会话 |
| `session/fork` | 从某条消息 fork |
| `session/rename` | 重命名 |
| `session/archive` | 归档/删除 |
| `session/history` | 分页回放 transcript |
| `session/prompt` | 发送文本/图片/引用 |
| `session/cancel` | 取消当前回合 |
| `session/queue` | 排队输入/编辑/删除 |
| `model/list` | 模型与 effort 目录 |
| `model/select` | 选择会话模型 |
| `settings/get` / `settings/set` | 设置读写（可选） |
| `approval/respond` | 回答审批 |
| `question/respond` | 回答用户提问 |
| `shutdown` | 优雅退出 |

### 通知

| 通知 | 作用 |
| --- | --- |
| `session/event` | 原始或投影后的 session 事件 |
| `session/assistant-stream` | process-local assistant 帧：start / chunk / end |
| `session/status` | running / idle |
| `approval/request` | 审批请求 |
| `question/request` | 用户提问 |
| `subagent/started` / `subagent/finished` | 子代理 |
| `host/fatal` | Host 启动/运行失败 |

`session/assistant-stream` 复用 DSH 的 `agent/assistant-stream` 语义：chunk 帧是临时的，`end.committed` 指向随后提交的 `assistant/message` 或 `assistant/attempt` 的 seq；UI 用它做逐字渲染，再用提交事件校准。

### 优点

- 完全按桌面 UI 的需要设计；审批、提问、历史、设置都能进协议。
- 不依赖 Web/客户端插件体系。
- 可以把 Host 侧的实现集中在 `@your-scope/dsh-mygo-app`，便于版本化。
- 可以按 capability 渐进增强，不要求第一版就实现全部方法。

### 成本

- 协议与 DSH 版本的兼容要自己维护。
- 需要自己实现审批/提问 answerer（或复用 dsh-TUI 的适配层思路）。
- 需要自己处理事件顺序、重连、取消、背压。

## 方案 D：复用 API Gateway

`@deepseek-ai/dsh-api-gateway` 已在 base 中，Web 的 Remote 面（session/settings/jobs/terminal/workspace/account）通过 Typert 描述符和 HTTP/WS 连接暴露。理论上 Go 客户端可以实现同一套 wire，拿到最高功能覆盖。

### 优点

- 与 Web 后端同源，功能覆盖最高。
- 已有完整的 controller / Remote 定义。

### 成本

- 需要 `host-webserver`、`client-connection`、`api-remotes` 与各 controller，profile 会重新变重。
- Go 端要实现 Typert 描述符校验、HTTP 调用、WebSocket 流、取消、事件转发。
- 客户端类型是从 TypeScript 生成的；Go 侧没有现成 SDK。

### 适用

如果以后需要“Web 与原生 UI 共用同一套后端 API”，可以再评估。第一版不采用。

## 决策

| 阶段 | 协议 | 目的 |
| --- | --- | --- |
| A | SDK stdio JSON-RPC | 验证进程、提交事件、渲染链路；不做 live 流式；可丢弃 |
| C | dsh-mygo channel | 产品协议；覆盖 live 流式、审批、提问、历史、设置 |

### 为什么不直接上 C

SDK 已经提供了 `session.event` 的完整提交事件流。先用它验证 UI 的投影与渲染，可以把“协议设计”与“UI 可行性”分开验证。C 的方法集在 A 跑通后再冻结，避免一开始设计过度。路线 A 的验收标准是“提交后渲染正确”，不是“逐字流式”。

### 为什么不选 ACP 作为产品协议

ACP 的权限模型和 session list 很有吸引力，但它明确是 automation-only：没有 elicitation、历史回放、fork、DSH 专属展示。dsh-mygo 需要这些能力，最终仍要加私有方法；与其在 ACP 上叠私有扩展，不如直接定义一个小而完整的 channel。

## 未决问题

- 历史回放用原始 `SessionEvent` 还是 Host 侧投影后的数据？倾向原始事件 + UI 投影，避免 Host 与 UI 两份展示逻辑。
- 附件走 base64 还是 Host 侧临时文件 + 路径引用？第一版 base64，限制大小。
- 设置第一版暴露多少？先只做模型/effort/权限模式，其余用 profile patch。
- 是否需要多窗口/多连接？第一版单窗口单连接。
- 协议版本号用整数还是 semver？倾向整数 `protocolVersion` + capability 集。

## 相关文档

- [DSH 组合模型](dsh-composition.md)
- [dsh-TUI 参考](dsh-tui-reference.md)
- [通道协议](../subsystem/channel-protocol.md)
- [会话投影](../subsystem/session-projection.md)
