# 交互（审批与用户提问）

## 职责

`interaction` 子系统把 DSH 的两类人机交互桥接到原生 UI：

- **审批**：工具执行前要求用户允许/拒绝。
- **用户提问**：模型通过 `ask_user_question` 向用户提问。

`approval` / `question` capability、Host answerer、有界队列与原生审批/问答面板构成这条链路。未协商审批时返回 `unavailable`，未协商提问时返回 `UserQuestionError/NO_PROVIDER`，不会悬挂等待。

没有这两条链路，需要人工决策的工具调用会停在 Host 里，UI 看起来像“卡住”。这是非 Web surface 必须补齐的适配层。

## 审批

### Host 侧

DSH 的权限层调用 `ctx.approval.request()`，派发 `approval/request` waterfall。`@dsh-mygo/host` 注册一个 listener，只回答自己拥有的 agent；其他请求交给 `next()`：

1. 暂停该请求（不立即回答）。
2. 生成稳定的 `requestId`，把请求投影为 `approval/request` 通知发给 Go。
3. 等待 `approval/respond`，把结果解析为 DSH 的 outcome。
4. 若请求的 abort signal 触发、Host 关闭或插件卸载，返回 `cancelled`。

DSH 的 outcome 是封闭集合：`allowed-once`、`rejected`、`cancelled`、`unavailable`。没有“永久允许”或“附带反馈”的协议通道；Host 拒绝携带 `remember` 的回答。

请求中应投影给 UI 的字段：

| 字段 | 说明 |
| --- | --- |
| `toolName` | 需要审批的工具 |
| `reason` | 请求方给出的可读原因 |
| `callId` | 关联的 tool/call；用于从 session log 恢复被门控的命令 |
| `requestId` / `sessionId` / `agentId` | Host 生成的请求身份与所属会话 |
| `external` | Host 根据 session log 计算的派生事实：请求是否无法锚定到当前会话的活跃工具调用 |
| `arguments` | 仅关联成功时携带当前活跃 `tool/call` 的原始参数，不使用截断后的 transcript |

工具名上限 256 UTF-8 字节，原因 4096 字节，参数 64 KiB；超限返回 `unavailable`，不截断后请求允许。Host 最多保留每会话 256 个活跃工具调用，工具结果或回合结束后清理关联。

### 排队与并发

- 并行工具调用可能同时产生多个审批；按 FIFO 排队，一次只展示一个。
- 同一 `callId` 的重复审批是异常：标记为 `external`，不要用一个回答去翻转另一个请求。
- 被取消的会话/工具调用要清掉对应队列项；迟到的 `approval/respond` 返回 `stale-request`，不改变已决结果。
- Host 默认最多等待 32 个审批，`maxPendingApprovals` 可配置为 1–64，超限返回 `unavailable`。Go 最多保留 64 个待处理请求，并拒绝已关闭身份复用。
- UI 显示当前会话的审批，并按会话隔离；后台会话的审批进入队列，在侧栏/通知中提示，切换会话后处理。

### UI

- 使用 `ui.Modal` 或 `ui.AlertDialog`；显示工具名、原因、被门控的命令/参数。
- 主按钮 `Allow once`，次按钮 `Deny`；Escape 或点击弹窗外等同拒绝。回答期间禁用重复操作；失败时允许重试或 `Stop turn`。
- 显示队列位置（例如“1 / 3”），避免用户以为只有一个。
- `external` 请求显示警告，不把它当成 agent 的确定命令。
- 不要把完整命令/路径无条件复制到日志；它们可能含敏感信息。
- 草稿不因审批改变；请求关闭、回合 idle、连接失败和退出时清理面板。弹窗以 requestId 绑定稳定身份，连续审批不复用旧控件状态。

## 用户提问

### Host 侧

模型调用 `ask_user_question` → `ctx.userQuestions.ask(request)` → 注册的 provider/answerer。

精确绑定的 DSH 使用 `user-questions/request` waterfall，不探测或抢占旧版 provider seat。Host 只回答自己创建的确切 agent；外来 agent 调用 `next()` 委托。无 agent 的 Host 请求也支持，通知使用 `sessionId: ""`，原生面板明确标注未关联会话。

`dsh-base` 只提供 `userQuestions` 服务，不注册模型工具；自有 bundle 通过稳定行 `tool-ask-user` 注册 `@deepseek-ai/dsh-tool-ask-user`。提问支持阻塞与 timed/continued 两种模式：阻塞等待用户一次提交；timed 请求在截止时间返回，迟到的回答按 `question/respond` 规则结算。`wait.timed` 在没有对应 provider 时返回 `NO_PROVIDER`，不伪装支持。

### 请求内容

通知为 `{ requestId, sessionId, questions }`。每题包含 `id`、`question`，可选 `header`、`detail`、`options: [{ label, description? }]`、`multiSelect`。问题 id 和选项 label 不得重复。详情完整展示；plan-review intent 在 Host 校验后按通用题型呈现，不隐藏所审阅的 detail，不推断选项审批语义。

所有题目均可填写自由文本，不自动选择首项或替用户提交推荐值。一次提交全部答案：`{ requestId, answers: [{ id, selected: string[], custom? }] }`。每题至少一个选项或非空自定义文本；单选只能二者选一，多选允许选项与文本并存。选项必须属于原请求，禁止重复、缺题或未知题目。Host 再次完整校验，不信任 UI。

边界：每请求 1–8 题，每题最多 16 个选项；id 128、题目 4096、header 256、detail 32768、label 256、description 2048、自定义回答 8192 UTF-8 字节；整批规范化问题/答案各最多 64 KiB。超限明确失败，不截断。

Host `maxPendingQuestions` 默认 32，范围 1–64；Go 最多 64 个待处理请求，并以有界身份指纹拒绝复用/修改。各类型内部 FIFO；审批优先于提问，绝不叠加两个 Modal，等待期间保留问答草稿。每请求独立稳定 key；每题单选、多选、自由文本和错误状态跨帧保存，提交期间禁用重复操作，失败可重试。

### 取消

- 用户关闭表单 / 按 Esc：返回 `UserQuestionError`，code 为 `ASK_CANCELLED`（与 plan mode 的“用户想插话”语义一致）。
- 工具 abort 返回 `ASK_ABORTED`；停止回合、Host 关闭或插件卸载返回 `ASK_CANCELLED`，每个 promise 都结算。
- 不要用空答案冒充取消。
- `question/cancel` 只关闭该提问，不等于停止回合；`Stop turn` 使用现有回合取消路径。请求结算时发送 `question/closed`，可先于 RPC 响应到达；迟到回答返回 `question/stale-request`。
- 会话 idle/取消只撤销该会话提问，不撤销无 agent 请求；连接失败/退出清空全部。无 agent 请求等待时禁用新 prompt，但保留 composer 草稿；无 agent 面板不提供 Stop turn。

## 权限模式

- 权限模式由 `@deepseek-ai/dsh-permission-presets` 与 `DSH_PERMISSION_MODE` 决定。
- 常用值：`workspace-write`、`danger-full-access`。
- `danger-full-access` 时 approval policy 为 `never`；其余默认 `ask`。
- 模式切换经 Host 写入，不能只在本地改环境变量。
- `danger-full-access` 的确认文案明确风险；不要默认选中。
- UI 不自行把 `ask` 降级为 `never`。
- sandbox 策略由 DSH 配置决定；UI 不提供绕过入口。

## 协议映射

| 方向 | 消息 | 说明 |
| --- | --- | --- |
| Host → UI | `approval/request` | `requestId` + 上述字段 |
| UI → Host | `approval/respond` | `{ requestId, decision: allow\|deny }` |
| Host → UI | `approval/closed` | `{ requestId, sessionId }`；回答、abort、取消或退出后发送 |
| Host → UI | `question/request` | `{ requestId, sessionId, questions }` |
| UI → Host | `question/respond` | `{ requestId, answers }` |
| UI → Host | `question/cancel` | `{ requestId }` |
| Host → UI | `question/closed` | `{ requestId, sessionId }` |

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
| 未协商审批或队列超限 | 返回 unavailable，不让工具悬挂 |
| UI 关闭/崩溃 | Host 取消未决请求；工具收到 cancelled |
| 回答过期 | `stale-request`；UI 移除该项 |
| 重复 `callId` | 标记 external 警告；不合并回答 |
| 未协商提问、队列超限或 timed wait | `UserQuestionError/NO_PROVIDER` |
| 提问被取消 | `UserQuestionError` / `ASK_CANCELLED` |
| Host 退出 | 所有未决请求取消；UI 显示恢复 |

## 测试

- 审批：allow / deny / cancel / 重复回答 / abort / Host 退出。
- 提问：单选、多选、自由文本、多问题、取消、agentless。
- 并发：同会话多个审批、跨会话审批、后台会话。
- 权限模式：workspace-write 与 danger-full-access 的行为差异。
- 真实 Host 集成：mock adapter 驱动真实 agent loop 和 approval 服务，fixture 工具仅在允许后写临时工作区标记；拒绝/取消不写，之后同会话继续 pong，不调用付费模型。
- `TestAgainstMygoQuestions/real` 使用真实 `ask_user_question` 工具和服务，验证整批单选/多选/自由文本回答、取消、停止、待回答退出与同会话继续发送；不调用付费模型。

## 相关文档

- [通道协议](channel-protocol.md)
- [会话投影](session-projection.md)
- [设置与凭据](settings-credentials.md)
- [profile 与 bundle](profile-bundle.md)
- [研究：dsh-TUI 参考](../research/dsh-tui-reference.md)
