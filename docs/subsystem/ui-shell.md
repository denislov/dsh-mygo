# UI 壳层（Native UI Shell）

## 职责

UI 壳层是 MyGo 进程里的原生界面：

- 用 `ui.View` 把 app 状态投影为窗口内容。
- 承载会话列表、聊天、工具卡片、diff、输入区、设置与弹窗。
- 消费 `ChannelClient` 的投影状态；不直接解析协议帧。
- 管理窗口、菜单、托盘、通知、快捷键与主题。

它不拥有 Agent、Session、模型或工具状态；这些在 Host。

## 组件与文件

| 文件 | 责任 |
| --- | --- |
| `internal/view/view.go` | `Model`、页面布局、transcript 行、composer、状态栏、错误横幅 |
| `internal/app/app.go` | Host 启动/停止、版本握手、`session/create` / `session/prompt`、有界通知分发、`Window.Update` |
| `internal/app/sessions.go` / `session_reader.go` | 会话操作、草稿隔离、列表/历史分页校验与后台回放 |
| `internal/view/sessions.go` | 原生会话选择弹窗、刷新/新建/选择、transcript 控件重置 |
| `internal/app/settings.go` / `internal/view/settings.go` | 白名单模型目录、会话级应用状态机与原生 Settings 面板 |
| `internal/app/configuration.go` / `internal/view/configuration.go` | 持久默认模型、只写密码凭据、配置快照校验与超时后刷新屏障 |
| `internal/projection/` | 提交事件 → `Item`（user / assistant / tool / notice） |
| `main.go` | flag、窗口、退出时停止 Host |

会话主界面由 header、transcript 列表与 composer 组成：

- transcript 显示提交后的 user 消息、assistant 文本、tool call/result 与 turn 结束通知；不显示 synthetic user context、system/developer 消息、reasoning。
- 协商 `session.assistant-stream` 后，额外展示带 `Streaming…` 标记的临时 assistant 行，稳定绑定 attemptId；提交时由唯一的 durable 行替换。没有可见 text 时不显示空卡片；缺口/abandoned/idle/失败/关闭移除临时行，不改 composer 草稿。
- 助手正文使用 MyGo 原生 `markdown.Stream` / `markdown.View`：标题、强调、列表、表格、内联代码与只读语法高亮代码块。流式同一 key 增量解析，直接显示已收到内容，不额外逐字延迟；最终提交使用 durable 行身份，不把 Markdown 展示状态写回历史。用户输入、工具参数/结果仍为可选择的原文，不解释为 Markdown。
- `Copy Markdown` 复制保留的助手源文，`Copy message` 复制可见用户文本；流式按钮标为 `Copy current text`。代码块有独立复制按钮，复制解析后的代码正文（代码行以换行结尾），不带围栏；正在生成的开放代码块禁用复制。Interrupted 消息的代码块也禁用完整代码复制，可显式 `Copy partial Markdown`。
- 复制反馈绑定消息 ID 与源文；流式源文改变或换消息时不继承“已复制”。超过 8000 rune 的消息明确标为截断，回退原文展示并只提供 `Copy visible text`，不把截断围栏解释为完整代码。复制的是当前保留内容，不承诺复制完整历史。
- 图片只显示描述，不配置网络或文件加载器；HTML 不接入执行/浏览器渲染器，代码不执行。链接只响应显式点击，并限制为无凭据、无控制字符的 HTTP(S) 地址；file/javascript/data/mailto/相对路径等不打开。剪贴板/URL 回调仅捕获 `c.Services()`，不保存帧作用域 Context/Element。
- Enter 发送，Shift+Enter 换行；发送期间仍可编辑草稿，但发送按钮与 Enter 发送禁用。
- 状态来自自定义 channel 的 `session/status`（显式 SDK 模式为 `session.status`）：starting / ready / working / resyncing / host error。发送失败保留草稿并显示可关闭的错误横幅。自定义 channel 的错误详情不展示原始 stderr。
- Host 失败或 `seq` 缺口时显示不可 Escape 关闭的恢复对话框：Restart host / View logs / Quit。重启期间显示 `Restarting the host…` 并禁用操作；恢复成功后自动回到 idle 并保留当前会话投影。
- 协商 `session.cancel` 后，working 时的 Send 替换为 Stop；点击后显示禁用的 `Stopping…`，直到取消请求返回且 Host 确认 idle。提前收到 idle 也不会放行新消息；取消失败显示错误并允许重试。取消不清除下一条草稿。Stop 和 Send 使用不同的稳定 key，反复消失/返回时不复用错误控件状态。SDK/旧 Host 不启用 Stop。
- 协商 `approval` 后，通过原生 Modal 按 FIFO 展示工具、原因和完整关联参数；提供 Allow once / Deny / Stop turn。Escape 或点击弹窗外拒绝，提交时防重复，失败可重试，草稿保留；external 请求显示未关联警告。请求身份作为稳定 key，关闭后下一请求不继承旧控件状态。
- 协商 `question` 后，原生 Modal 支持整批单选、多选和自由文本，完整展示题目、选项说明和 detail；所有题目回答后一次提交。表单状态按请求/题目稳定绑定，重试保留答案，Escape/点击外部等同 Cancel question；Stop turn 另行停止回合。无 agent 请求明确标注，不显示 Stop turn。审批优先，同一时刻只有一个交互弹窗。
- 工具卡片使用原生 Collapsible，默认折叠，展开状态绑定稳定工具行 ID，结果更新/列表位置变化不重置。展开显示预览和 `View details`；原生检视器提供 Arguments / Result / Changes、逐 hunk 导航和只读 DiffView，大 hunk 明确回退文本。不读写显示路径，不提供应用/撤销改动操作。
- 检视器只保留一个 hunk 的 editor 状态；关闭、结果更新和淘汰释放旧缓冲。审批/提问优先，暂时隐藏检视器，避免两个 Modal 叠加；交互结束后可以继续查看。
- 协商 `session.history` 后，header 提供 Sessions，当前会话 ID 与 cwd 显示在独立信息行。侧栏或选择弹窗显示当前 profile 的有界完整摘要列表、时间、状态与目录，支持 Refresh / New conversation / 选择恢复；窄窗口用弹窗降级。旧 Host/SDK 不显示入口。
- 仅 idle 且无审批/提问时允许导航。刷新可 Close/Escape 取消；历史加载期间显示 Loading、禁用发送与编辑，不能重复新建/切换或关闭弹窗。失败保留当前 transcript/草稿，弹窗中显示可重试的安全错误；审批/提问优先，不叠加会话弹窗与工具检视器。
- 切换成功后才整体替换 transcript，清除工具展开/检视器、复制反馈、旧 ListState 和临时流式状态；transcript 与 composer 的控件身份包含 session id。每个会话草稿仅在本 App 内存中保存，返回时恢复，重启不恢复草稿。新建使用启动 cwd；新建/冷恢复按 Host 默认或显式 flags 选择模型，冷恢复保持持久 cwd，已拥有会话保持 Host 原路由。
- Settings 提供当前会话模型面板：provider、模型目录建议、实际 Model ID、effort 与 Refresh/Apply model/Close。只在 idle 无交互、无会话操作时启用，Host 确认后显示新模型；读取可取消，提交期间防重复/禁止关闭，失败保留表单和草稿。面板内容可滚动，底部操作始终可达；header 将模型/Host 版本信息放在独立行，避免窄窗口挤掉 Sessions/Settings。
- 协商 settings.configuration 后，Settings 可滚动区域增加 Save selection as default、DeepSeek 凭据状态、Password 输入与确认清除；操作范围、存储与默认 flags 优先级以 [设置与凭据](settings-credentials.md) 为准。密码提交/刷新/关闭后清空并更换稳定 key；写入期间不能关闭或重复操作，超时/失败后禁止发送/导航，必须 Refresh 确认。底部 Close/Refresh/Apply 始终可达。

## 视图模型

```text
App
├─ Host          *HostSupervisor
├─ Channel       *ChannelClient
├─ Sessions      SessionStore        // 摘要、当前会话、历史窗口
├─ Projection    *SessionProjection  // 当前会话的 transcript
├─ Prefs         UIPreferences       // 主题、语言、快捷键、窗口状态
├─ Router        *ui.Router
├─ Dialogs       DialogState         // approval、question、rename、confirm
└─ Composer      ComposerState       // 草稿、附件、引用、焦点
```

原则：

- `App` 字段是跨帧状态；`Element`、`ui.Context` 不进入字段。
- 会话真相在 Host；`Projection` 是可重建的缓存。
- UI 偏好（滚动、折叠、草稿）可以本地持久化，但不进入 session log。
- 所有 Host 事件的写入都在主线程完成，或先进入队列再由主线程合并。

## 页面

使用 `ui.Router`：

| 路径 | 页面 |
| --- | --- |
| `/` | 首页/会话选择/新建 |
| `/session/{id}` | 会话主界面 |
| `/settings` | 设置首页 |
| `/settings/{section}` | 设置子页（模型、外观、快捷键、权限、关于） |
| `/welcome` | 首次运行/登录 |

路由状态属于 app；深链（`dsh-mygo://session/<id>`）通过 `router.Push` 进入对应页面。

### 会话主界面

```text
┌───────────────┬──────────────────────────────────────────────┐
│ Sidebar       │ Header: title / model / status / actions     │
│ 会话列表      ├──────────────────────────────────────────────┤
│ 搜索          │ Transcript (ui.List)                         │
│ 新建          │  turn / assistant / tool / diff / notice     │
│ 工作区        │                                              │
│               ├──────────────────────────────────────────────┤
│               │ Composer: 输入 / 附件 / @引用 / 发送          │
└───────────────┴──────────────────────────────────────────────┘
```

右侧可以按需增加面板（plan/todo/子代理/文件）。

## 聊天渲染

### 消息列表

- 用 `ui.List` + `ui.ListState`；只构建可见行。
- 每个 turn 一个或多个行；稳定 key 用 turn/seq。
- 流式 assistant 行固定在一个位置；提交后原位替换。
- 滚动到底部跟随流式；用户向上滚动后停止自动跟随，显示“回到最新”。

### Markdown

- 流式：`markdown.Stream(c, text, &options)`。
- 完成：`markdown.View(c, text, &options)`。
- 只在文本变化时更新；不要每个 token 重建整个列表。
- 代码块用 `ui/editor` 或等宽文本；长代码折叠。
- 链接点击前校验 scheme；外部链接交给 `mygo.Shell.OpenExternal`。

### 工具卡片

- 默认折叠；标题显示工具名、状态、耗时、结果摘要。
- 展开后：
  - 参数：格式化 JSON 或命令。
  - 结果：文本、Markdown、图片、diff。
  - 错误：错误码 + 可读原因。
- 文件编辑：优先 `editor.DiffView`；没有 diff 时退化为代码块。
- 大输出：截断 + “在编辑器中打开/复制”；不要一次性渲染数 MB。

### 状态

- `session/status = running`：输入区禁用或排队，显示工作指示。
- `idle`：恢复输入。
- `resyncing`：显示“正在同步历史”，暂停发送。
- `failed`：显示错误与重试。

## 输入区

- 多行文本：`ui.TextArea` / `ui.TextAreaBuffer`；大草稿用 `ui.TextBuffer`；支持输入法、选区、撤销。
- Enter 发送，Shift+Enter 换行；可在设置中交换。
- 附件：粘贴/拖放/选择文件；图片走 `session/prompt` 的 image 块；路径引用走 `@path`。
- `@` 引用：文件/目录补全；引用在发送时解析为 Host 可见的路径。
- `/` 命令：提供命令补全与执行；不要伪造不存在的命令。
- 发送失败：保留草稿，显示错误；不要丢失用户输入。

## 审批与提问

- `ui.Modal` / `ui.AlertDialog`；一次一个，显示队列位置。
- 键盘：Tab 在选项间移动，Enter 确认，Esc 取消/拒绝。
- 审批展示工具名、原因和可滚动的完整原始参数；不使用截断摘要做允许判断。
- 提问支持多问题、单选/多选/自由文本，提交前校验必填；不自动预选推荐答案，单选的选项和自定义文本互斥，多选可混合。
- 队列变化时不要抢走用户正在编辑的输入焦点。

## 会话列表

- 会话列表读取 `session/list` 的固定快照并加载全部有界摘要，遵守 Host 的排序，不自行按 UI 活动时间重排；没有标题时显示短 session id。目录只用于显示，不通过 UI 读取文件或改写持久 cwd。
- 首次启动新建当前会话，不自动选择上次历史。重启后从会话列表选择旧会话，分页完成后恢复正文，再允许继续发送。
- 列表包含 profile persistence 可见的根/分叉会话，不按工作目录筛选，不展示 subagent；不同目录明确显示。冷会话 stored 不代表未被其他进程占用，恢复失败可重试，不能绕过单写者约束。
- 支持新建、刷新、恢复与空/错误/加载状态；最多加载 1000 条摘要。Host 数量与资源限制见 [通道协议](channel-protocol.md#host-历史能力)。
- 侧栏固定展示会话列表，支持搜索、标题编辑、归档/删除、fork 与更大目录检索；不提供无后端能力的假按钮。

## 设置

设置页覆盖影响使用的配置：

| 设置 | 来源 |
| --- | --- |
| Provider / Model / Effort | `model/list`、`model/select` |
| 权限模式 | `DSH_PERMISSION_MODE` / Host 配置 |
| 语言 | 本地 + Host locale |
| 主题 | MyGo 主题（跟随系统） |
| 快捷键 | 本地 |
| 关于/版本 | 本地 + `initialize.server` |
| 更新 | MyGo updater |

其余 DSH 设置通过 `settings/get` / `settings/set` 接入；不要一次性复刻 Web 设置页。所有权与凭据规则见 [设置与凭据](settings-credentials.md)。

## 主题、语言与无障碍

- 主题：`c.Theme()`；跟随系统明暗；自定义强调色需谨慎，优先使用 MyGo 的语义色。
- 语言：UI 字符串集中管理；Host locale 通过 `initialize.client.locale` 传入；两者可以不同。
- 无障碍：为按钮、输入、列表行、弹窗提供名称/角色/描述；不要把关键操作只放在图标上。
- 高对比度/减少动态：遵循系统设置；流式淡入可关闭。

## 快捷键与焦点

- 全局：新建会话、搜索、切换侧栏、设置、退出。
- 会话：发送、取消、上一条/下一条、回到最新、复制。
- 弹窗：Escape、Tab、Enter；焦点回到触发控件。
- 使用 `ui.Handle` 绑定持久身份；用 `FocusBind` 表达“希望聚焦”，不要直接操作元素。
- 快捷键冲突时以当前焦点/模态为准；不要在后台窗口抢键。

## 性能

- 虚拟列表：只构建可见行。
- 流式合并：chunk 以帧率上限合并（例如 16–33ms），不要每 token 触发重建。
- 增量 Markdown：依赖 `markdown.Stream` 的增量解析，不要全量重排。
- 工具输出/diff 缓存有上限；超出后按需重新请求/重新解析。
- 避免在视图函数里做 I/O、排序大集合、解析大 JSON。
- 用 MyGo 的 inspector 观察帧耗时；必要时降低动画/淡入。

## 测试

- `ui.NewTester(view, width, height)`：点击、输入、快捷键、焦点、滚动。
- 断言文本与结构，不断言像素；视觉验收用真实窗口。
- 用 fake `ChannelClient` 驱动固定事件序列，测试流式/提交/审批/提问。
- 测试窗口宽度变化、长标题、空会话、错误态、深色主题。
- 平台验收：Windows 首发，macOS 随后；覆盖字体、输入法、窗口装饰、托盘。

## 失败模式

| 失败 | 处理 |
| --- | --- |
| Host 未就绪 | 显示加载页；禁用发送 |
| Host 断开 | 保留 transcript；显示重连/重启 |
| 协议错误 | 显示技术详情入口；不假装成功 |
| 流式缺口 | 丢弃草稿；等待提交事件或重连 |
| 审批/提问队列变化 | 保持焦点稳定；显示队列 |
| 更新中 | 禁止新回合；显示进度 |

## 相关文档

- [会话投影](session-projection.md)
- [交互](interaction.md)
- [通道协议](channel-protocol.md)
- [研究：MyGo 原生 UI](../research/mygo-native-ui.md)
