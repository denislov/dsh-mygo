# 交互（审批与用户提问）

## 职责

`interaction` 子系统把 DSH 的两类人机交互桥接到原生 UI：

- **审批**：工具执行前要求用户允许/拒绝。
- **用户提问**：模型通过 `ask_user_question` 向用户提问。

没有这两条链路，需要人工决策的工具调用会停在 Host 里，UI 看起来像“卡住”。这是非 Web surface 必须补齐的适配层。

## 审批

### Host 侧

DSH 的权限层调用 `ctx.approval.request()`，派发 `approval/request` waterfall。`@your-scope/dsh-mygo-app` 注册一个 listener：

1. 暂停该请求（不立即回答）。
2. 生成稳定的 `requestId`，把请求投影为 `approval/request` 通知发给 Go。
3. 等待 `approval/respond`，把结果解析为 DSH 的 outcome。
4. 若请求的 abort signal 触发、Host 关闭或插件卸载，返回 `cancelled`。

DSH 的 outcome 是封闭集合：`allowed-once`、`rejected`、`cancelled`。没有“永久允许”或“附带反馈”的协议通道；不要伪造。

请求中应投影给 UI 的字段：

| 字段 | 说明 |
| --- | --- |
| `toolName` | 需要审批的工具 |
| `reason` | 请求方给出的可读原因 |
| `callId` | 关联的 tool/call；用于从 session log 恢复被门控的命令 |
| `agentId` | 发起请求的 agent/session |
| `external` | Host 根据 session log 计算的派生事实：请求是否无法锚定到当前会话的活跃工具调用 |
| `options` | UI 只提供 allow once / reject |

### 排队与并发

- 并行工具调用可能同时产生多个审批；按 FIFO 排队，一次只展示一个。
- 同一 `callId` 的重复审批是异常：标记为 `external`，不要用一个回答去翻转另一个请求。
- 被取消的会话/工具调用要清掉对应队列项；迟到的 `approval/respond` 返回 `stale-request`，不改变已决结果。
- 后台会话的审批也要呈现；可以进入“需要输入”状态，而不是静默挂起。

### UI

- 使用 `ui.Modal` 或 `ui.AlertDialog`；显示工具名、原因、被门控的命令/参数。
- 主按钮“允许一次”，次按钮“拒绝”；Escape 等同拒绝或取消，按产品选择。
- 显示队列位置（例如“1 / 3”），避免用户以为只有一个。
- `external` 请求显示警告，不把它当成 agent 的确定命令。
- 不要把完整命令/路径无条件复制到日志；它们可能含敏感信息。

## 用户提问

### Host 侧

模型调用 `ask_user_question` → `ctx.userQuestions.ask(request)` → 注册的 provider/answerer。

DSH 有两代接口：

- 旧版：`service.registerProvider(provider)` 的单一 provider seat。
- 新版：`user-questions/request` waterfall，按 agent scope 过滤。

Host 插件应探测当前接口并注册，类似 dsh-TUI 的 `prepareQuestionAnswerer`：

- waterfall：监听 `user-questions/request`；只处理当前 agent 的请求，其他 `next()` 委托。
- legacy：`registerProvider`；处理 `DUPLICATE_PROVIDER`，不要抢其他插件的 seat。
- agentless 请求（例如 `/auth` 向导）也要处理，不能因为找不到 agent 就丢弃。

### 请求内容

`AskUserQuestionRequest` 可以包含：

- 一个或多个问题。
- 单选/多选选项。
- 是否允许自由文本。
- 默认值/推荐值。
- 与当前 agent/session 的关联。

UI 应支持多问题表单；一次提交全部答案。答案形状以 DSH 的 `AskUserQuestionAnswer` 为准。

### 取消

- 用户关闭表单 / 按 Esc：返回 `UserQuestionError`，code 为 `ASK_CANCELLED`（与 plan mode 的“用户想插话”语义一致）。
- 工具调用被取消、Host 关闭、插件卸载：同样返回取消错误；不要让 promise 悬挂。
- 不要用空答案冒充取消。

## 权限模式

- 沿用 base 的 `@deepseek-ai/dsh-permission-presets` 与 `DSH_PERMISSION_MODE`。
- 常用值：`workspace-write`、`danger-full-access`。
- `danger-full-access` 时 approval policy 为 `never`；其余默认 `ask`。
- UI 只展示/切换模式，不自行把 `ask` 降级为 `never`。
- sandbox 策略由 DSH 配置决定；UI 不提供绕过入口。

## 协议映射

| 方向 | 消息 | 说明 |
| --- | --- | --- |
| Host → UI | `approval/request` | `requestId` + 上述字段 |
| UI → Host | `approval/respond` | `{ requestId, decision: allow\|deny }` |
| Host → UI | `question/request` | `requestId` + questions |
| UI → Host | `question/respond` | `{ requestId, answers }` |

规则：

- `requestId` 由 Host 生成，在连接生命周期内唯一。
- `approval/respond` / `question/respond` 幂等；重复回答返回 `stale-request`。
- 超时：由 UI 侧提示，但不要自动拒绝关键审批；可以保持显示直到用户处理或 Host 取消。
- 连接断开时，Host 取消所有未决请求；重连后不重放旧请求。
- 请求/答案都不进入模型可见历史，除非 DSH 自己记录（审批结果可能由工具结果体现）。

## Secret 处理

- 凭据输入只写不读；UI 不缓存明文。
- 审批中的命令、问题中的选项可能含敏感路径；日志与崩溃报告只记录 `requestId`、工具名、agentId，不记录完整参数。
- 设置页的 API key 输入使用 `ui.TextInput(...).Password()` 或等价控件；提交后立即清空内存中的输入缓冲。

## 失败模式

| 失败 | 处理 |
| --- | --- |
| 没有 answerer | 请求挂起；启动时自检并报告配置错误 |
| UI 关闭/崩溃 | Host 取消未决请求；工具收到 cancelled |
| 回答过期 | `stale-request`；UI 移除该项 |
| 重复 `callId` | 标记 external 警告；不合并回答 |
| legacy provider 冲突 | `DUPLICATE_PROVIDER`；让出 seat，不抢 |
| 提问被取消 | `UserQuestionError` / `ASK_CANCELLED` |
| Host 退出 | 所有未决请求取消；UI 显示恢复 |

## 测试

- 审批：allow / deny / cancel / 重复回答 / abort / Host 退出。
- 提问：单选、多选、自由文本、多问题、取消、agentless。
- 并发：同会话多个审批、跨会话审批、后台会话。
- 权限模式：workspace-write 与 danger-full-access 的行为差异。
- 真实 Host 集成：用一个需要审批的工具（例如写工作区外文件）跑完整往返。

## 相关文档

- [通道协议](channel-protocol.md)
- [会话投影](session-projection.md)
- [设置与凭据](settings-credentials.md)
- [profile 与 bundle](profile-bundle.md)
- [研究：dsh-TUI 参考](../research/dsh-tui-reference.md)
