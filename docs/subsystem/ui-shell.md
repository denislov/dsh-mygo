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
| `internal/view/view.go`、`model_regions.go`、`model_actions.go`、`chrome.go`、`shell.go`、`style.go`、`tokens.go` | `Model` 区域组合、页面布局、原生标题栏与菜单、会话详情、transcript 行、语义主题/尺寸 token 与 SVG 图标 |
| `internal/view/composer.go`、`composer_toolbar.go`、`quick_select.go`、`attachments.go` | 一体化输入区、发送/停止、附件暂存与原生图片预览 |
| `internal/view/preferences.go`、`internal/i18n/` | 设备级偏好、受门禁约束的命令、集中双语文案 |
| `internal/app/app.go` | Host 启动/停止、版本握手、`session/create` / `session/prompt`、有界通知分发、`Window.Update` |
| `internal/app/sessions.go` / `session_reader.go` | 会话操作、草稿隔离、列表/历史分页校验与后台回放 |
| `internal/view/sessions.go`、`session_groups.go` | 原生固定侧栏、项目目录分组、窄窗口弹窗、标题搜索、会话管理表单与 transcript 控件重置 |
| `internal/view/workspace.go`、`session_rail.go`、`workspace_split.go`、`internal/app/workspace.go`、`internal/workspace/` | 原生工作面板、独立响应式 rail、留白拖动热区、终端资源与项目文件/Git 的有界后台读取 |
| `internal/app/session_management.go` | 重命名、归档/取消归档、Host 确认与失败刷新屏障 |
| `internal/app/settings.go` / `internal/view/settings.go`、`settings_center.go` | 白名单模型目录、会话级应用状态机与统一设置中心 |
| `internal/app/configuration.go` / `internal/view/configuration.go` | 持久默认模型、只写密码凭据、配置快照校验与超时后刷新屏障 |
| `internal/projection/` | 提交事件 → `Item`（user / assistant / tool / notice） |
| `main.go` | flag、窗口、退出时停止 Host |

会话主界面由自绘标题栏、transcript 列表与 composer 组成：

- 窗口使用 MyGo `TitleBarHidden`，保留 Windows 自绘的最小化/最大化/关闭按钮；视图通过 `c.TitleBar()` 取得系统按钮占位，标题栏可拖动，操作按钮始终避开 `bar.Right`。根布局先垂直切分为左侧导航与右侧会话；侧栏贯穿窗口高度，顶部放应用菜单与收起按钮。右侧独立标题栏左侧显示会话标题，右侧依次提供 Session rail、Terminal、Changes、Preview、Files 和 More；前五项使用可切换图标与选中态，More 收纳会话列表、子代理、设置和应用操作。侧栏隐藏时导航按钮回到会话标题栏；窄窗口把次要入口收进菜单。无标题时使用本地化的未命名提示，不把会话 ID 当作页面标题。
- 应用菜单使用 MyGo 原生系统菜单：New conversation、Sessions、Show/Hide sidebar、Subagents、Settings、About dsh-mygo、Quit；不可用的能力隐藏或禁用，不提供无后端动作的菜单项。凭据入口统一位于设置中心 Connection，不在新建会话和标题栏重复显示。
- Session rail 展示 Environment、Context 与 Progress：本机/项目目录、Git 分支、Host 已报告的上下文用量、任务与子代理信息；不伪造未提供的 token 分类、连接器或变更指标。rail 独立于工具网格，顶部按钮用短竖条与其余工具入口分组，选中态使用中性底色。扣除 sidebar 和工具区后的对话侧宽度至少为 820 DIP 时，rail 以 340 DIP 宽停靠于对话与工具区之间，保留至少 480 DIP 对话空间；空间不足时使用由 rail 按钮控制、定位于对话区右上角的非模态浮层，不替换对话或重排工具面板。仅工具标签可见、无对话列时回退到按钮附近。窗口/工具区尺寸变化自动切换形式，保留开关和章节展开状态；Escape 或点击浮层外的对话/工具区关闭浮层，工具栏/侧栏导航与设置/审批等弹层保留 rail 开关。
- Terminal、Changes、Preview、Files 位于对话右侧，支持独立开关、拖动调整与单面板最大化。多个工具面板按列/行组合；窗口宽度小于 1000 DIP 时以标签切换，提供返回对话入口，避免挤压输入区。设置/审批等弹层不清空面板布局。
- 相邻工具面板及对话与工具区之间使用 13 DIP 留白，无常驻分割线；圆角表面保留自身边框。整个间隔为原生拖动热区，横向/纵向分别使用 EW/NS 缩放光标，悬停、拖动或键盘焦点可见时显示居中的短 bar。焦点进入后方向键调整，窗口变化保持比例；单面板最大化与窄窗口标签模式不保留空白占位。
- Terminal 使用 MyGo terminal 插件与 libghostty-vt，在当前项目目录启动真实本机 shell；隐藏面板保留进程，重启终端、切换项目与退出回收旧进程。终端启动和关闭等待不阻塞 UI；启动结果检查项目版本，过期资源关闭。终端输出不进入 Host 协议或会话日志，程序读取剪贴板默认拒绝、程序写剪贴板禁用；显式用户复制/粘贴沿用原生组件。
- Files 按需后台读取项目目录，虚拟列表显示已展开条目；读取校验绝对根目录、相对路径和实际符号链接目标，拒绝越界。每目录最多 2000 项，缓存最多 64 个目录；项目切换丢弃旧结果与缓冲。
- Changes 后台读取 Git 分支和工作区变更，提供只读文件差异；禁用外部 diff/textconv、可选索引锁和 fsmonitor，输出最多 1 MiB、命令超时 10 秒，不提供提交、应用或撤销动作。
- Preview 原生展示用户选取的 Markdown、图片和只读源码；文件最多 512 KiB、图片最多 400 万像素，二进制/超限明确提示。源码使用可选择复制的原生只读编辑器。开发服务 URL 只通过显式动作打开系统浏览器，限制无凭据的 HTTP(S)，不嵌入网页。文件、图片与终端缓冲不持久化、不进入模型上下文。

- 聊天与输入区共享最大 860 DIP 的居中阅读宽度与水平留白。助手消息直接在页面上靠左排版，用户消息以自然内容宽度、最大 620 DIP 的圆角气泡靠右排版；正文使用 16 DIP 字号，助手 Markdown 使用 1.62 行高，用户原文保持字面语义。角色名保留为消息容器的可访问名称。复制动作位于正文/气泡外侧下方，用户消息靠右、助手消息靠左；平时透明，在消息 hover 或键盘焦点进入时显示。按钮保留名称、Tooltip 和复制反馈，透明时仍可通过键盘聚焦。

- transcript 显示提交后的 user 消息、assistant 文本、reasoning、tool call/result 与 turn 结束通知；不显示 synthetic user context、system/developer 消息。
- 同一回合内连续的 reasoning 与 tool 行显示为默认折叠的活动组：折叠头给出思考时长、工具数量或工具名。展开后各条思考与工具仍默认折叠为单行：思考显示有界的纯文本预览；工具显示动作名称，以及参数中的命令说明、路径或命令摘要，省略成功状态 pill，保留运行指示与失败状态。进一步展开可查看原文与完整详情。思考展开状态使用跨流式/提交的 ActivityKey（缺失时使用行 ID），不写入会话日志；窗口淘汰与切换清理状态。活动组是视图派生的展示行，工具行身份与详情入口保持不变。
- 协商 `session.assistant-stream` 后，额外展示带 `Streaming…` 标记的临时 assistant 行与 `Thinking…` 的临时 reasoning 行，稳定绑定 attemptId；提交时由 durable 行替换。没有可见 text 时不显示空卡片；缺口/abandoned/idle/失败/关闭移除临时行，不改 composer 草稿。
- 助手正文使用 MyGo 原生 `markdown.Stream` / `markdown.View`：标题、强调、列表、表格、内联代码与只读语法高亮代码块。流式同一 key 增量解析，直接显示已收到内容，不额外逐字延迟；最终提交使用 durable 行身份，不把 Markdown 展示状态写回历史。用户输入、工具参数/结果仍为可选择的原文，不解释为 Markdown。
- `Copy Markdown` 复制保留的助手源文，`Copy message` 复制可见用户文本；流式按钮标为 `Copy current text`。代码块有独立复制按钮，复制解析后的代码正文（代码行以换行结尾），不带围栏；正在生成的开放代码块禁用复制。Interrupted 消息的代码块也禁用完整代码复制，可显式 `Copy partial Markdown`。
- 复制反馈绑定消息 ID 与源文；流式源文改变或换消息时不继承“已复制”。超过 65536 rune 的提交正文（reasoning/临时正文各 8000 rune）明确标为截断，回退原文展示并只提供 `Copy visible text`，不把截断围栏解释为完整代码。复制的是当前保留内容，不承诺复制完整历史。
- 图片只显示描述，不配置网络或文件加载器；HTML 不接入执行/浏览器渲染器，代码不执行。链接只响应显式点击，并限制为无凭据、无控制字符的 HTTP(S) 地址；file/javascript/data/mailto/相对路径等不打开。剪贴板/URL 回调仅捕获 `c.Services()`，不保存帧作用域 Context/Element。
- Enter 发送，Shift+Enter 换行；发送期间仍可编辑草稿，但发送按钮与 Enter 发送禁用。
- 状态来自自定义 channel 的 `session/status`（显式 SDK 模式为 `session.status`）：starting / ready / working / resyncing / host error。发送失败保留草稿并显示可关闭的错误横幅。自定义 channel 的错误详情不展示原始 stderr。
- Host 失败、自动同步失败或旧 Host 的 `seq` 缺口显示不可 Escape 关闭的恢复对话框：Restart host / View logs / Quit。协商后的缺口自动同步显示 `Syncing history…`，保持当前正文与草稿、暂停输入及导航，不弹出重启对话框；交互表单暂时隐藏并保留状态，待同步完成后继续。重启期间显示 `Restarting the host…` 并禁用操作；恢复成功后自动回到 idle 并保留当前会话投影。
- 协商 `session.cancel` 后，working 时的 Send 替换为 Stop；点击后显示禁用的 `Stopping…`，直到取消请求返回且 Host 确认 idle。提前收到 idle 也不会放行新消息；取消失败显示错误并允许重试。取消不清除下一条草稿。Stop 和 Send 使用不同的稳定 key，反复消失/返回时不复用错误控件状态。SDK/旧 Host 不启用 Stop。
- 协商 `approval` 后，通过原生 Modal 按 FIFO 展示工具、原因和完整关联参数；提供 Allow once / Deny / Stop turn。Escape 或点击弹窗外拒绝，提交时防重复，失败可重试，草稿保留；external 请求显示未关联警告。请求身份作为稳定 key，关闭后下一请求不继承旧控件状态。
- 协商 `question` 后，原生 Modal 支持整批单选、多选和自由文本，完整展示题目、选项说明和 detail；所有题目回答后一次提交。表单状态按请求/题目稳定绑定，重试保留答案，Escape/点击外部等同 Cancel question；Stop turn 另行停止回合。无 agent 请求明确标注，不显示 Stop turn。审批优先，同一时刻只有一个交互弹窗。
- 工具卡片使用原生 `CollapsibleBase` 组合出自定义 header（工具图标、动作名称、单行摘要、运行/失败状态与折叠箭头），默认折叠，展开状态绑定稳定工具行 ID，结果更新/列表位置变化不重置。摘要最多 180 rune，仅提取已知参数字段，不直接展示任意 JSON 或结果内容；参数不可解析时保留工具名称。展开显示预览和 `View details`；原生检视器提供 Arguments / Result / Changes、逐 hunk 导航和只读 DiffView，大 hunk 明确回退文本。不读写显示路径，不提供应用/撤销改动操作。
- 检视器只保留一个 hunk 的 editor 状态；关闭、结果更新和淘汰释放旧缓冲。审批/提问优先，暂时隐藏检视器，避免两个 Modal 叠加；交互结束后可以继续查看。
- 协商 `session.history` 后，More 菜单提供 Sessions，当前会话 ID 与 cwd 在会话详情中查看。侧栏或选择弹窗显示当前 profile 的有界完整摘要列表、时间、状态与目录，支持 Refresh / New conversation / 选择恢复；窄窗口用弹窗降级。旧 Host/SDK 不显示入口。
- 会话详情与 composer 的上下文用量入口显示最新一步 token 用量与路由容量：入口为百分比与容量，点击展开原生 Popover，显示已用/容量、按输入/输出分段的占用条、In / Out / cache / reasoning / 剩余明细，并标注只统计最新回合。只在 Host 提供对应事件时出现；容量或用量未知时省略对应片段，不阻塞 Sessions/Settings 入口，也不参与发送或历史回放。
- 仅 idle 且无审批/提问时允许导航。刷新可 Close/Escape 取消；切换会话读取历史期间在对话区显示 Loading、关闭发送边界，不能重复新建/切换或关闭弹窗。侧栏、项目头、输入区与 shell 保持正常对比度，不随切换整体变灰；输入仍可编辑，但到读取完成前不会发送新 prompt，新导航点击同样不生效。同一会话向上加载是独立的后台读取，不改变侧栏、目录与输入区控件的启用状态；读取期间防重复请求，发送或打开会话设置时取消读取，导航沿用操作版本使旧结果失效。取消后可再次加载相同历史边界。失败保留当前 transcript/草稿，显示可重试的安全错误；审批/提问优先，不叠加会话弹窗与工具检视器。
- 切换成功后才整体替换 transcript，清除工具展开/检视器、复制反馈、旧 ListState 和临时流式状态；transcript 与 composer 的控件身份包含 session id。草稿按会话隔离并通过设备级编辑文件恢复。新建沿用当前会话 cwd，项目分组的加号明确指定该组 Host 摘要的 cwd；新建/冷恢复按 Host 默认或显式 flags 选择模型，冷恢复保持持久 cwd，仍由 Host 持有的会话保持原路由。
- 设置中心的 Runtime 分区管理本机 DSH 后端：当前/已安装/可下载条目、Use/Remove、检测已安装 DSH、从文件安装与检查更新；不依赖 Host，下载/安装/切换期间锁定关闭，详见下文“设置中心 Runtime 分区”。
- 设置中心的 Models 分区先展示 Host 注册的提供商卡片、当前会话路由与编辑入口，再在选中卡片内提供模型目录、手填 Model ID、effort、Apply model 和持久默认模型操作。目录可用不代表凭据验证成功，不提供未协商的提供商新增/删除动作。模型写入仍等待 Host 确认，失败刷新屏障保持；composer 的模型入口继续使用快捷列表。
- 协商 settings.configuration 后，Connection 提供 DeepSeek 凭据状态、Password 输入与确认清除；密码提交/刷新/关闭后清空并更换稳定 key。刷新和提交动作放在各自内容区域，右上角关闭按钮在最小窗口始终可达，写入期间禁用；超时/失败沿用原有确认屏障，规则见 [设置与凭据](settings-credentials.md)。

## 视图模型

```text
App
├─ Host          *HostSupervisor
├─ Channel       *ChannelClient
├─ Sessions      SessionStore        // 摘要、当前会话、历史窗口
├─ Projection    *SessionProjection  // 当前会话的 transcript
├─ Runtime       RuntimeState        // 当前/已安装/可下载 runtime、进度、切换状态
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

`Model` 按交互区域组合：`ChatState`（投影与历史窗口）、`ComposerState`（草稿、附件、提及与发送门禁）、`SessionsState`（导航、目录与管理表单）、`SettingsState`（设置、配置、权限与凭据）、`ModelRouteState`（当前路由与目录）、`InteractionState`（审批/提问队列）、`RecoveryState`（生命周期与恢复），以及具名的 `Workspace`、`Shell`、`Prefs`、`LocalState`、`Runtime` 资源。区域以匿名嵌入保持既有字段地址，迁移字段不改变控件 key、列表状态或滚动位置。

每个区域只消费自己的动作结构（`ComposerActions`、`SessionActions`、`SettingsActions`、`InteractionActions`、`RecoveryActions`），不建立一个包揽全部回调的大接口；回调缺席表示动作不可用，能力字段（`CanCancel`、`CanApprove`、`CanModels`、`CanConfigure` 等）独立表达协商结果，不能把读取成功当成写入确认。普通构建只读取状态和处理显式输入事件；会话导航等需要等整棵视图构建结束后才能安全提交的业务动作进入有界队列，在 `View` 末尾执行并请求下一帧。应用自有提示使用 `i18n` 的具名 key 与 `T`/`Tf`；用户正文、工具输出、提问选项、模型 ID 与技术错误详情不经过词表。布局只把重复且具设计语义的尺寸放进 `tokens.go`，其余数值留在所属布局函数。

## 页面

使用 `ui.Router`：

| 路径 | 页面 |
| --- | --- |
| `/` | 首页/会话选择/新建 |
| `/session/{id}` | 会话主界面 |
| `/settings` | 设置首页 |
| `/settings/{section}` | 设置子页（模型、外观、运行时、快捷键、权限、关于） |
| `/welcome` | 首次运行/登录 |
| `/setup/runtime` | 无 runtime 时的全窗口引导 |

路由状态属于 app；深链（`dsh-mygo://session/<id>`）通过 `router.Push` 进入对应页面。

### 会话主界面

```text
┌───────────────┬──────────────────────────────────────────────┐
│ ☰ / Sidebar   │ 会话标题                         工具/系统按钮 │
│ 新建          ├──────────────────────────────────┬───────────┤
│ 搜索/筛选     │ Transcript (ui.List)             │ Details   │
│ 项目分组会话  │  user / assistant / activity     │ (按需展开)│
│               ├──────────────────────────────────┤           │
│               │ Composer: 附件 / 输入 / 发送      │           │
│ 状态/设置     │ 项目 / 权限     模型 / 上下文     │           │
└───────────────┴──────────────────────────────────┴───────────┘
```

宽窗口可按需展开右侧详情栏；窄窗口以 Popover 提供同一内容。plan/todo/子代理/文件进入该详情栏，不再另开并列面板。

## 聊天渲染

### 消息列表

- 用 `ui.List` + `ui.ListState`；只构建可见行。
- 每个 turn 一个或多个行；稳定 key 用 turn/seq。
- 流式 assistant 行固定在一个位置；提交后原位替换。
- 滚动到底部跟随流式；用户向上滚动后停止自动跟随，显示“回到最新”。
- 协商 session.history-window 后首屏按回合加载最近窗口；用户向上滚动接近顶部时自动前插更早的完整回合，不提供 Load earlier messages 文字按钮。内容不足一屏时自动补充，已有阅读锚点保持；每个边界只调度一次读取，窗口变更先完成原生列表布局再判断触顶。读取期间禁用重复操作/发送，允许保留编辑；失败在输入区上方提供错误和图标重试，旧内容不清空。
- 返回最新使用输入框上方居中的圆形向下箭头，以浮层放置，不占正文或输入区布局高度；离开底部或较新缓存被淘汰时才显示。最新内容仍在缓存时直接本地滚动到底；历史浏览已淘汰尾部时重新读完整尾部回合，保留草稿。
- 切换会话的加载状态位于中间对话区，替换正文为居中 Spinner 和说明；侧栏不插入加载/刷新进度文字，项目头和会话行位置保持稳定。加载期间不把禁用态叠加到侧栏、输入区或工具按钮上，避免整窗降透明度；只有真正不可用的动作（发送、附件、写入表单）保持禁用。
- 展示预算与淘汰由 [会话投影](session-projection.md#历史回放) 定义；浏览缓存淘汰较新行时显示 Viewing earlier history 并禁用发送，返回最新后恢复。历史控制条在最小窗口保持可达；没有可见消息的页提示 No visible messages，不冒充空会话。
- 用户消息的附件显示元数据行（类型、尺寸、大小、可选文件名），图片未解码或读取失败时不阻塞其余正文；只有图片、没有文本的消息仍保留卡片和标题，但此时不显示复制正文动作。

### 任务与目标

- 宽窗口未显示详情栏时，composer 上方使用带边框与阴影的浮动卡片，以 MyGo `CollapsibleBase` + `Progress` 展示最新 `todo/write` 清单：默认只显示 `Tasks done/total` 与完成度进度条，展开后在有界滚动区显示最多 8 行，完成/进行中/待办各有标记，超出显示 `+N more`；下一回合开始清空，清单不进入正文。详情栏显示时由 Progress 卡片承担同一清单，避免重复；展开高度根据窗口收紧并扣除标题栏高度，不把输入或发送按钮推出最小窗口。
- 会话详情显示计划模式、当前目标（objective、phase、rounds）与子代理实时状态（运行中/已完成计数）；没有对应事件时不占位，切换会话清空子代理状态。

### 运行时引导

- 没有有效选中 runtime 时，根 View 只渲染全窗口引导，不渲染依赖 Host 的 shell；标题栏保留拖动、Quit 与系统按钮留白。
- 两种来源：已安装 DSH（PATH 检测、自选 `dsh` 命令、显示 Node/DSH 版本与不兼容原因）和 runtime bundle（下载最新、导入本地 `.tgz`）。
- 下载/校验/解压显示进度与可取消操作；失败保留重试与改选入口。安装成功后自动启动 Host。
- 引导期间禁用设置、会话、子代理、快捷键与发送；最小窗口 560×420 下标题、来源与操作可达，内容可滚动。
- 已有 legacy runtime 只作为候选显示，需用户确认；版本或协议不兼容时不提供使用按钮。

### 设置中心 Runtime 分区

- Runtime 分区始终可用，不依赖 Host capability，也不调用 Settings 刷新。
- Current runtime：类型、DSH、Node、Host bundle 版本、路径与复制；错误/通知固定显示在正文。
- Installed runtimes：当前项显示 Current；其他项可 Use / Remove；Remove 需二次确认，当前项不可移除，external/legacy 只遗忘条目。
- Add runtime：Check installed DSH、Install from file…、Check for updates；下载/安装只加入列表，不自动切换。
- 下载/安装/切换期间禁用关闭与导航，当前内容区显示进度并允许取消安装；切换只显示等待状态，完成后保留设置页并提示结果。
- 切换失败恢复旧 runtime 时显示“已恢复之前的运行时”；旧 Host 也无法恢复时进入恢复对话框的 Change runtime。

### 首次使用与工作文件夹

- 输入区下方持续显示工作文件夹与 Choose project folder 入口，空会话和有历史会话共用；新建会话不提供单独的连接入口。文件夹选择使用 MyGo 原生 Directory 对话框，主线程只发布状态；打开期间禁止发送、切换会话和冲突设置操作。取消保持原会话，非法/不可访问路径显示固定错误；迟到结果用 Host generation、会话身份与 picking 状态隔离。
- 选择不同文件夹通过现有 session/create 新建会话，不修改旧会话 header、草稿或历史。此后 New conversation 使用当前会话文件夹；历史会话通过 Host 的持久 header 恢复自己的目录。创建失败保留原正文/草稿，并在输入区下方展示可重试错误。
- 安装版未显式指定 cwd 的空会话需选择或确认显示的文件夹后才发送。新建同目录空会话不绕过确认；恢复含消息的会话直接沿用持久目录。空会话没有新增本机完成标记，重开空会话可以再次要求确认。
- DeepSeek 凭据在 Settings → Connection 中配置，沿用 Password 输入和 configuration/credential 生命周期，只显示必需的状态、保存、获取 API key 链接与本机存储说明；输入区不展示凭据引用或内部 source 标识。已知官方 provider 缺少可配置凭据时禁发，并显示前往设置连接分区的文字提示；旧 Host、其他 provider 与未知状态不据此推断认证有效性。
- 文件夹选择保留原会话草稿，密钥配置不清空当前草稿。文件夹确认、选择进度与错误位于输入区下方；小窗口收起空会话装饰标题，保持目录、输入和发送动作可达。原生菜单、翻译、焦点和禁用行为由 MyGo 组件提供。

### 子代理详情

- 会话工具栏在协商 `subagent.inspection` 后提供 Subagents 入口，空闲和运行中均可打开。使用 MyGo Modal、虚拟 List、Collapsible 与现有消息/Markdown 组件；主列表展示直接子代理的任务与状态，详情展示父会话关系、任务、消息、工具输出与结束原因。外部或未知任务显示没有本地历史的说明。
- Refresh 读取最新提交输出，Earlier activity 查看前一页，Latest activity 返回尾页；一次只保留一页，未出现可见内容的窗口明确提示。Close/Escape 关闭并清理读取，运行中保留 Stop turn 以停止父任务；读取失败提供刷新重试，不影响父草稿。
- 面板打开时阻止父消息发送、会话导航、设置及冲突快捷键；审批/提问优先显示，处理后可继续原详情。窗口缩小时收紧正文列表高度，保持返回、关闭、刷新和停止按钮可达。目录和历史的权限、预算、冷读取与资源边界见 [通道协议](channel-protocol.md#子代理只读查询)。

### Markdown

- 流式：`markdown.Stream(c, text, &options)`。
- 完成：`markdown.View(c, text, &options)`。
- 只在文本变化时更新；不要每个 token 重建整个列表。
- 代码块用 `ui/editor` 或等宽文本；长代码折叠。
- 链接点击前校验 scheme；外部链接交给 `mygo.Shell.OpenExternal`。

### 工具卡片

- 默认折叠；标题显示工具图标、工具名、状态 pill 与结果摘要；耗时只在投影提供可靠起止时间时显示，缺失不伪造。
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
- composer 使用圆角 18、1 DIP 边框的输入表面，TextArea 与左侧附件按钮、右侧发送/停止按钮处于同一行。TextArea 清除组件的默认最小高度，按窗口收紧最大行数；暂存图片独立限高滚动。下方工具区左侧是项目目录与权限，右侧是模型/effort 与上下文用量入口（百分比与容量，点击展开明细）；窄窗口自动换行，不把控件推出窗口。
- 模型按钮使用锚定输入区的原生 Popover，提供 provider 切换、搜索、可滚动模型列表、当前项勾选和当前模型支持的 effort。选择直接调用现有 model/select 并等待 Host 确认，读取与写入复用设置操作编号、门禁和错误刷新屏障。More settings 转入完整 Conversation model 设置；读取时可关闭，写入时禁止关闭，迟到读取不能重新打开列表。
- 权限按钮独立显示当前模式，Popover 列出 Host 声明的选项和实际 sandbox/approval；危险选项要求默认不勾选的确认，再显式 Apply。确认后等待 Host 快照再关闭，失败保留选择并要求刷新；不根据预设名称推断危险程度。两种快捷列表在审批/提问/恢复界面出现时让位，不叠加显示。

- Enter 发送，Shift+Enter 换行；可在设置中交换。
- 附件：粘贴/拖放/选择文件；图片走 `session/prompt` 的 image 块；路径引用走 `@path`。粘贴用 `OnPaste` + `mygo.Clipboard.ReadImage`/`ReadFiles`，拖放用 `DroppedFiles`/`FileDragOver`（窗口级 `OnFileDrop` 兜底），选择用 `mygo.Dialog.Open`（在 goroutine 打开，结果经 `Window.Update` 发布）。待发图片进入当前草稿的暂存列表，发送前按协商上限校验类型、单图、总量与数量；超限或失败保留草稿与暂存图片并显示可读原因。暂存图片不写入本地草稿文件，切换会话或重启后清空。
- `@` 引用：用 `TextSelection` 定位光标处的 `@token`，`ui.Popover` 锚定输入区列出匹配路径（MyGo 不提供 TextArea 光标矩形）；Up/Down/Tab/Escape 在 `OnTextKey` 中消费；选中后替换 token 为 `@path`。引用只是文本，发送时不复制文件内容，Host 与 UI 同机。
- 暂存图片通过后台 MyGo DecodeBitmap 生成原生预览；独立的预览准入上限为单图 400 万像素，超限或解码失败保留元数据行，不改变附件发送准入。MyGo 处理 EXIF 方向、缩小显示与纹理生命周期；结果通过 Window.Update 对应的发布队列返回，并检查会话身份与单调暂存 ID，移除/清空/切换后的迟到结果丢弃。预览与字节都不落入本机草稿文件；历史附件继续只显示 Host 提供的引用元数据，不把标识符解释成路径。
- `/` 命令：提供命令补全与执行；不要伪造不存在的命令。
- 发送失败：保留草稿，显示错误；不要丢失用户输入。
- 设置中心的 General 与 Shortcuts 分区配置主题、语言、发送键和应用命令，不依赖会话 Settings 能力。表单与已应用值分离，底部取消/重置/应用动作在最小窗口保持可达；滚动只包围表单内容。固定状态、菜单标签、无障碍名称随语言切换，原始消息/代码/提问与工具载荷保留原文。

## 审批与提问

- `ui.Modal` / `ui.AlertDialog`；一次一个，显示队列位置。
- 键盘：Tab 在选项间移动，Enter 确认，Esc 取消/拒绝。
- 审批展示工具名、原因和可滚动的完整原始参数；不使用截断摘要做允许判断。
- 提问支持多问题、单选/多选/自由文本，提交前校验必填；不自动预选推荐答案，单选的选项和自定义文本互斥，多选可混合。
- 队列变化时不要抢走用户正在编辑的输入焦点。

## 会话列表

- 会话列表读取 `session/list` 的固定快照并加载全部有界摘要，按项目目录分组，组内遵守 Host 的排序，不自行按 UI 活动时间重排。`blank` 的当前会话显示为本地化的 New conversation，其他空白会话不显示；非空但没有标题的会话显示短 session id，标题栏使用未命名提示，不把 id 当标题。冷检查点显示 Cached metadata；缺失或损坏显示 Open to rebuild metadata，不能将回退创建时间称为精确最近活动。目录只用于显示，不通过 UI 读取文件或改写持久 cwd。
- 启动从设备级编辑状态选择最近会话，握手后尝试恢复并回放历史；没有最近会话或恢复失败时新建，失败保留旧草稿并提示重试或显式复制到当前编辑框。正文来自 Host，分页完成后才允许发送。后台加载摘要不阻塞发送，新导航或发送取消过期列表操作。
- 列表包含 profile persistence 可见的根/分叉会话，不按工作目录筛选，不展示 subagent；不同目录明确显示。冷会话 stored 不代表未被其他进程占用，恢复失败可重试，不能绕过单写者约束。
- 支持新建、刷新、恢复与空/错误/加载状态；最多加载 1000 条摘要。当前会话已是同一目录、同一模型路由的空白会话时，New conversation 复用它而不追加空日志；目录或路由改变时新建。Host 数量与资源限制见 [通道协议](channel-protocol.md#host-历史能力)。
- 侧栏固定展示会话列表，支持搜索、标题编辑、归档/删除、fork 与更大目录检索；不提供无后端能力的假按钮。
- 窗口宽度至少 820 时显示宽 280 的固定侧栏，可由侧栏顶部开关收起；窄窗口通过 Sessions 打开弹窗。两种布局共用搜索、筛选、表单草稿与按 session id 绑定的列表身份。标题搜索使用 MyGo SearchField，只搜索有界摘要。列表按规范化完整 cwd 分组，目录分隔符统一，Windows 路径不区分大小写；项目按首次出现顺序、组内按 Host 顺序排列，不混淆同名的不同目录。项目头显示末级目录名，Tooltip 提供完整路径，加号在该项目中新建会话；无 cwd 的项归入 Other conversations。
- 侧栏列表只显示紧凑标题，时间/状态/缓存提示保留在 Tooltip 与选择弹窗，当前行使用中性背景。各行菜单按钮保留布局和键盘焦点，平时透明，整行 hover 或 focus 时显示；重命名、归档/恢复使用原生菜单，选择动作时再次校验能力与 busy 门禁。侧栏顶部为 New conversation、标题搜索、归档筛选与 Refresh，底部显示当前连接状态与设置入口。

- UI 分别协商 `session.rename` 与 `session.archive`，入口随能力显示。取消归档通过同一归档方法的 `archived:false`；目录请求在支持归档时携带 `includeArchived:true`，由 Show archived 本地筛选。归档会话的打开按钮禁用，先恢复再打开；重命名表单按目标会话 id 绑定，冷目标在后台 resume 取得 Host 单写者句柄后再 rename，不切换当前正文、模型或草稿。
- 归档前提供目标操作确认，写入中不能关闭、切换、新建或重复提交。当前会话归档后保留可读正文及草稿，显示归档提示并禁用发送，允许新建、切换或恢复。Host 重启遇到已知归档的当前会话时新建会话，旧草稿留在原 id 下，不自动取消归档；恢复旧会话后取回该草稿。
- 协商 session.release 时，下一次导航/管理操作前释放非当前和非目标的退休句柄，不把所有访问过的会话长期保活。目标验证失败保留当前 Agent，目标句柄待后续回收；后台重命名同样纳入回收。已释放会话重新打开采用冷恢复路由；紧邻返回、仍被拥有的会话保持原路由。释放不删除日志/草稿，也不改变归档状态。
- 管理成功以 Host 摘要与新列表的目标身份、标题/归档状态为准；当前会话重命名还需匹配有序到达的 `session/title` 提交及 nextSeq。失败或超时保留表单与草稿，禁止发送、导航和设置写入，要求 Refresh sessions；重命名涉及当前会话时刷新还需回放 history 校准序列。失败、关闭和新操作通过 operation 身份丢弃迟到结果，不让旧响应覆盖新会话。

## 设置中心

设置中心是一个 MyGo Modal：左侧搜索与分组导航、右侧宽幅可滚动正文、右上角关闭图标。弹层为系统标题栏预留高度，关闭设置不与关闭窗口重叠；小窗口缩窄导航并纵向排列表单行。通用、快捷键修改即生效并保存，无统一重置/取消/应用页脚；Host 写入按钮位于对应内容中，保留写入屏障。分区按能力出现，不显示死入口：

| 分区 | 内容 | 来源 |
| --- | --- | --- |
| General | 主题、语言、Host 语言提示 | 本地偏好 |
| Models | 提供商卡片、当前路由、卡片内模型/effort 编辑与持久默认模型 | `model/list`、`model/select`、`configuration/default-model` |
| Permissions | 权限模式、实际 sandbox/approval、危险确认 | Host 会话权限快照 |
| Connection | DeepSeek API 凭据 | `configuration/credential` |
| Shortcuts | 发送键与三个可配置命令 | 本地偏好 |
| About | App 版本、Host/DSH、session id | 本地 + `initialize.server` |

General、Shortcuts、About 不依赖 Host 能力；Conversation、Permissions、Connection 分别按模型目录、权限能力与配置能力显示。侧栏设置入口、Ctrl+, 与 More → Settings 打开 General；composer 模型 chip 打开快捷列表，More settings 打开 Conversation；凭据通过 Connection 分区配置。切换宿主机分区时在点击回调里触发 RefreshSettings，不在渲染期读取。

其余 DSH 设置通过 `settings/get` / `settings/set` 接入；不要一次性复刻 Web 设置页。所有权与凭据规则见 [设置与凭据](settings-credentials.md)。

权限能力独立于模型目录，任一能力可打开设置中心；当前权限在会话页展示，权限表单位于 Permissions 分区。选择使用稳定 preset id，确认按目标 preset 绑定，默认不勾选；写入期间显示 Applying permissions，禁用关闭、重复提交和导航。失败后保留草稿，通过 Refresh 读取 Host 真相，发送区显示权限未确认提示。关闭图标保持可达，Refresh 和权限提交位于内容区；具体风险判定和确认契约见 [权限模式](settings-credentials.md#权限模式)。

编辑框通过绑定后的变更回调触发本机草稿快照，不在构建视图时启动文件 I/O。保存失败提示独立于 Host 错误，不清空编辑；普通关闭需等待后台保存，失败后由用户重试或显式放弃。Host 恢复对话框内同样能选择放弃未保存编辑，避免两种错误互相遮挡。格式、隔离与恢复规则见 [编辑状态文件](settings-credentials.md#编辑状态文件)。

## 主题、语言与无障碍

- 主题：c.Theme() 的语义色，允许跟随系统或固定浅色/深色；选择不直接修改控件的持久状态或绘制回调。窗口外观通过 MyGo Theme.SetSource 同步，视图使用对应原生调色板。
- 视觉样式以 MyGo LightTheme/DarkTheme 为底，统一中性色、低饱和强调色、字阶与组件圆角；Windows 英文界面使用 Segoe UI，中文使用 Microsoft YaHei UI，其他平台沿用系统字体。图标是一次解析的静态 SVG，交给 ui.Icon 渲染；图标按钮始终有本地化 Label 与 Tooltip。
- Toolbar 负责轻量按钮、键盘组导航和溢出；系统菜单、Popover、Collapsible 与 Modal 负责各自的焦点及关闭行为。动效复用 MyGo 的颜色 Transition、状态 ContentTransition、工作 Spinner 与弹层/折叠动画，遵循 ReduceMotion；不为流式正文添加位置/尺寸动画或人为逐字延时，不自行实现计时器或动画引擎。
- 语言：应用字符串集中管理，支持英文/简体中文并同步 MyGo 内建控件；不翻译数据载荷。Host locale 通过 initialize.client.locale 传入，运行中切换在 Host 下次启动采用；格式与持久化契约见 [设置中心的设备分区](settings-credentials.md#设置中心的设备分区)。
- 无障碍：为按钮、输入、列表行、弹窗提供名称/角色/描述；不要把关键操作只放在图标上。
- 高对比度/减少动态：遵循系统设置；流式淡入可关闭。

## 快捷键与焦点

- 全局：新建会话、搜索、切换侧栏、设置、退出。
- 会话：发送、取消、上一条/下一条、回到最新、复制。
- 弹窗：Escape、Tab、Enter；焦点回到触发控件。
- 使用 `ui.Handle` 绑定持久身份；用 `FocusBind` 表达“希望聚焦”，不要直接操作元素。
- 快捷键冲突时以当前焦点/模态为准；不要在后台窗口抢键。
- 发送键与新建/界面偏好/停止命令使用有限选项，校验重复和禁用项；应用命令通过 c.OnShortcut 绑定当前视图可执行动作，所有模态/审批/提问期间暂停，不覆盖输入法或 Escape 的既有语义。Composer 提示和空会话说明始终反映所选发送方式；Shift+Enter 保持换行。

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
