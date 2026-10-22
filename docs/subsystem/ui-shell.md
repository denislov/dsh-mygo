# UI 壳层（Native UI Shell）

## 职责

UI 壳层是 MyGo 进程里的原生界面：

- 用 `ui.View` 把 `internal/view.Model` 投影为窗口内容。
- 承载 header、transcript、composer、Sessions/Settings 弹窗与审批/提问弹窗。
- 消费 `internal/app` 推送的不可变快照；不解析协议帧，也不直接调用 Host。

它不拥有 Agent、Session、模型或工具状态；这些在 Host。

## 结构与文件

| 文件 | 责任 |
| --- | --- |
| `internal/view/view.go` | `Model`、窗口布局、transcript 行、composer、状态栏、错误横幅 |
| `internal/view/sessions.go` | Sessions 弹窗：刷新、新建、选择、加载/错误状态 |
| `internal/view/settings.go` | 模型设置面板：provider、Model ID、effort、Apply/Refresh |
| `internal/view/configuration.go` | 持久默认模型、DeepSeek 凭据状态与只写密码输入 |
| `internal/view/approval.go` / `question.go` | 审批与提问弹窗（单选、多选、自由文本） |
| `internal/view/markdown.go` | 助手 Markdown 渲染、代码/源文复制 |
| `internal/view/tools.go` | 工具折叠卡片、只读 diff 检视器 |
| `internal/app/` | Host/channel/投影/交互装配，向 `view.Model` 发布快照 |
| `main.go` | flag、窗口、退出时停止 Host |

## 窗口布局

单窗口，内容为三段落：

```text
┌──────────────────────────────────────────────┐
│ Header: 名称 / Sessions / Settings / 模型 /  │
│         会话 ID 与 cwd / 状态                 │
├──────────────────────────────────────────────┤
│ Transcript (ui.List，跟随最新行)              │
│  user / assistant / tool / notice            │
├──────────────────────────────────────────────┤
│ Composer: 多行输入 / hint / Send 或 Stop      │
└──────────────────────────────────────────────┘
```

- 窗口默认 960×760，最小 560×420；`StateKey: "main"` 保存窗口状态。
- header 的模型与 Host 版本放在独立信息行，窄窗口下不挤掉 Sessions/Settings。
- UI 文案为英文；`initialize.client.locale` 固定 `zh-CN`。
- 弹窗包括 Sessions、Settings、审批、提问；工具检视器是 transcript 上的内嵌面板，不另开窗口。

## 状态模型

`internal/view.Model` 是主线程共享的跨帧状态：

- 连接：`Phase`（Starting / Ready / Working / Stopped / Host error）、`ModelLabel`、`Detail`、错误横幅与详情。
- transcript：`Items`、`List`、工具卡片展开/检视器、Markdown 选项、复制反馈。
- composer：`Draft`、`Send`、`Cancel`、`CanCancel`、`CancelPending`。
- 会话：`SessionID`、`SessionCWD`、`Sessions`、切换/加载/错误标志、草稿由 app 按 session 保存。
- 设置：`ActiveRoute`、`Catalog`、菜单状态、`Configuration`、凭据草稿与写入屏障。

原则：

- 视图只读快照，不在视图函数里做 I/O、排序大集合或解析大 JSON。
- `Element`、`ui.Context` 不进入字段；跨帧身份用 `c.Key`、`ui.Handle`、`ListState`。
- 会话真相在 Host；`Projection` 是可重建缓存，UI 草稿与折叠状态不回写会话历史。

## Transcript

- 用 `ui.List` + `ListState` 只构建可见行；行身份来自投影 `Item.ID`（turn/seq/tool callId）。
- 行类型：user 原文、assistant Markdown、tool 折叠卡片、turn 结束 notice。
- synthetic user context、system/developer 消息、reasoning、`assistant/attempt` 元数据不展示。
- 助手正文使用 `markdown.Stream`（流式）与 `markdown.View`（提交后），代码块只读高亮。
- 流式行带 `Streaming…` 标记，绑定 `stream:<attemptId>`；提交事件到达后用 durable 行原位替换，最终只显示一份答案。
- 文本超过 8000 rune 时标记截断，回退原文并只提供可见文本复制。
- 图片只显示描述，不配置网络/文件加载器；HTML 与代码不执行；链接只接受无凭据、无控制字符的 HTTP(S) 显式点击。
- 复制：代码块复制解析后的正文；`Copy Markdown` 复制保留源文；用户与工具内容保持原文；开放代码块与中断消息禁用完整代码复制。

## 工具卡片

- 默认折叠，标题显示工具名与状态；展开显示预览与 `View details`。
- 展开状态与 `tool:<callId>` 绑定，结果更新或列表位置变化不重置。
- 检视器分 Arguments / Result / Changes，逐 hunk 导航。
- 文件改动使用只读 `editor.DiffView`；前后文本超过 16 KiB 或 400 行时改为 Before/After 文本。
- 参数/结果各保留最多 64 KiB，详情缓存 4 MiB；超限或淘汰明确提示，不冒充完整内容。
- 检视器不读写显示路径，不提供应用/撤销改动；审批/提问期间隐藏，避免 Modal 叠加。

## Composer

- `ui.TextArea`（1–8 行），Enter 发送，Shift+Enter 换行；只处理无修饰键的 Enter。
- Working 时 Send 替换为 Stop；点击后显示禁用的 `Stopping…`，直到取消请求返回且 Host 确认 idle。
- 发送期间仍可编辑草稿；失败保留草稿并显示可关闭的错误横幅。
- 会话切换、审批/提问、Settings 写入或模型未确认时禁用发送。
- 不提供附件、拖放、`@` 引用或 `/` 命令。

## Sessions 弹窗

- 入口在 header，仅在协商 `session.history` 后显示；旧 Host/SDK 不显示。
- 显示当前 profile 的有界摘要列表（时间、状态、cwd、无标题时短 id），按 Host 创建时间倒序，不按 UI 活动重排。
- 操作：Refresh、New conversation、选择会话、Close/Escape。
- 只允许 idle 且无审批/提问时导航；加载期间显示 Loading 并禁用发送与编辑。
- 失败保留当前 transcript/草稿，在弹窗内显示可重试的安全错误。
- 切换成功后整体替换 transcript，并清除工具展开、检视器、复制反馈、ListState 与临时流式状态。
- 每个会话的草稿只存在本次 App 内存中，返回时恢复，重启不恢复。
- 不提供固定侧栏、搜索、重命名、归档、删除或 fork。

## Settings 弹窗

- 入口在 header，分三段：
  1. 当前会话模型：provider、目录建议、实际 Model ID、effort、Refresh / Apply model。
  2. 持久默认模型：当前默认与 `Save selection as default`。
  3. DeepSeek 凭据：安全状态、Password 输入 `Save key`、勾选确认后的 `Clear managed key`。
- 内容可滚动，底部 Close / Refresh / Apply 始终可达。
- 只在 idle 且无会话操作/交互时启用；读取可取消，写入期间不能关闭或重复提交。
- 写入超时或失败后禁止发送/导航，必须 Refresh 确认后才解除。
- 不提供权限模式、主题、语言、快捷键或更新设置。

## 审批与提问

- 一次只显示一个弹窗；审批优先于提问，不叠加。
- 审批：显示工具名、原因与完整关联参数，提供 `Allow once` / `Deny` / `Stop turn`；Escape 或点击窗外等同拒绝。
- 提问：整批展示单选、多选与自由文本；提交前校验每题至少一个选择或非空文本；单选与自定义文本互斥，多选可混合。
- 提问提供 `Submit answers` / `Cancel question` / `Stop turn`；无 agent 请求标注未关联会话且不显示 Stop turn。
- 表单状态按 requestId/题目稳定绑定；重试保留答案；请求关闭、idle、连接失败与退出时清理。

## 键盘与焦点

- Enter 发送、Shift+Enter 换行；Escape 关闭弹窗或拒绝审批；Tab 在选项间移动。
- 弹窗关闭后焦点回到触发控件；同一请求连续出现时不复用旧控件状态。
- 不提供全局快捷键、托盘、菜单或深链。

## 性能

- transcript 用虚拟列表，只构建可见行。
- 通知与 RPC 回调节流：16 ms / 64 项 / 256 KiB 触发批次，最多等待 32 批；每批只发布一次组合快照。
- Markdown 使用增量解析；流式更新不重建整棵列表。
- 工具详情与 diff 有独立预算，超限淘汰，不在 UI 线程计算大 diff。

## 失败模式

| 失败 | 处理 |
| --- | --- |
| Host 未就绪 | 显示 Starting，禁用发送 |
| Host 断开或失败 | 保留 transcript，显示 `Host error` 与错误横幅；不自动重启 |
| 协议错误 | 停止 Host，显示可关闭错误；不伪装成功 |
| 流式缺口/abandoned | 丢弃临时行，等待提交事件 |
| 审批/提问请求关闭 | 移除弹窗，保留草稿 |
| 历史加载失败 | 保留原会话与草稿，弹窗内显示可重试错误 |
| 配置写入未确认 | 禁用发送与导航，必须 Refresh |

## 测试

- `ui.NewTester(view, width, height)`：点击、输入、快捷键、焦点、滚动、最小窗口。
- 断言文本与结构，不断言像素；真实窗口在 Windows 手测，macOS 随后。
- 覆盖：transcript 行、Enter/Shift+Enter、Stop 生命周期、审批/提问表单、Markdown 复制、工具检视器、Sessions/Settings 弹窗、错误横幅。
- 真实窗口用 fake host 手动 smoke；原生 UI Automation 覆盖 Windows 弹窗与窄窗口。

## 相关文档

- [会话投影](session-projection.md)
- [交互](interaction.md)
- [通道协议](channel-protocol.md)
- [设置与凭据](settings-credentials.md)
- [进度与路线](../roadmap.md)
