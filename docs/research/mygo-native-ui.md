# MyGo 原生 UI 能力研究

## 结论

MyGo 0.6.2 的原生 UI（`github.com/egoist/mygo/ui`）足以承载 dsh-mygo：窗口、布局、虚拟列表、流式 Markdown、代码编辑器、diff 查看器、弹窗、路由、焦点与无障碍都已具备，不需要 webview，也不需要前端构建。

## 版本与模型

- MyGo `v0.6.2`，Go `1.27.2`，纯 Go，无 cgo。
- 原生 UI 由 MyGo 自己绘制：macOS 用 Metal，Windows 用 Direct3D 11，Linux 用 OpenGL。
- 窗口可以显示网页（系统 webview）或原生 UI。本项目只使用原生 UI。
- 一个窗口同一时间显示一种内容：`WindowOptions.Content: ui.View(view)`。
- 原生 UI 窗口不启动 webview 进程，启动快、内存小。

## 视图生命周期

视图是一个普通 Go 函数：

```go
func (a *app) view(c *ui.Context) {
    ui.Column(c).Fill().Children(func() {
        ui.Text(c, "Hello")
    })
}
```

规则（来自 MyGo `ui/doc.go` 与 `docs/ui/views.md`）：

- 视图在主线程构建一帧；输入、`Window.Update`、`Window.Invalidate` 和动画都会触发重建。
- 一帧可能构建多次。`Element` 与自定义 part 在下次构建前失效；**不要把 Element 存进 app 状态或后台 goroutine**。
- 跨帧状态放在 app 字段里；控件状态用 `ui.Handle`、`FocusBind`、`c.Key` 保持身份。
- `Children(func())` 会切换 `ui.Context` 的父作用域；不要把 Context 存起来。需要跨回调的服务用 `c.Services()`。
- 输入在构造函数返回时已经生效；`Changed` / `Submitted` 在下一帧报告；`OnClick` / `OnShortcut` / `OnChange` / `OnSubmit` 在构建后、重建前运行。
- 慢 I/O 与耗时计算不要放在视图里；结果通过 `Window.Update` 发布，或用 `Invalidate` 触发重绘。

这些规则直接决定 dsh-mygo 的 UI 状态分层：模型、会话、通道状态在 app 层；滚动、选区、输入法、焦点在控件层；视图只做投影。

## 组件

与本项目直接相关的能力：

| 需求 | MyGo 能力 |
| --- | --- |
| 布局 | `ui.Row`、`ui.Column`、`ui.Grid`、`Flex`、`Grow`、`Padding`、`Gap` |
| 虚拟列表 | `ui.List`：只构建可见行；`ui.ListState` 保存滚动与选择；`ItemKey` 提供稳定身份 |
| 文本输入 | `ui.TextInput`；多行与编辑器见 `ui/editor` |
| Markdown | `ui/markdown`：`View` 显示完整文档，`Stream` 显示流式回复；`Options.Reveal` 控制节奏与淡入 |
| 代码块 | `ui/editor`：语法高亮、折叠、查找、补全、诊断、inlay hints |
| Diff | `ui/editor` 的 `DiffView`：并排或统一、行内高亮、折叠未改动行 |
| 路由 | `ui.Router`：`NewRouter`、`Push`、`Replace`、`Back`、`Forward`；`router.View` 按路径构建页面 |
| 弹窗 | `ui.Modal`、`ui.AlertDialog`；`Dismissed`、焦点回收、Escape |
| 菜单/弹出层 | `ui.Popover`、`ui.Overlay`、元素的 `.ContextMenu(func(m *ui.Menu))` |
| 通知 | `c.AddToast`、`ui.Toast`；系统通知用 `mygo.NewNotification` |
| 无障碍 | 语义角色、名称、描述、公告；`docs/ui/accessibility.md` |
| 主题 | `c.Theme()`；跟随系统外观；`docs/ui/styling.md` |
| 动效 | `ui` 的 transitions、motion、spring；`docs/ui/motion.md` |

聊天界面可以按以下方式组合：

- 消息列表：`ui.List` + `ui.ListState`，每行一个 turn 或一个 assistant 段落。
- 流式回复：`markdown.Stream`，完成后在同一位置切换为 `markdown.View`。
- 工具调用：可折叠卡片；输入/输出用等宽文本或 `editor.View`；diff 用 `editor.DiffView`。
- 审批/提问：`ui.Modal` 或 `ui.AlertDialog`；选项用 `ui.Radio` / `ui.Select`；自由文本用 `ui.TextInput`。
- 会话列表：左侧 `ui.List` 或 `ui.Sidebar`；搜索框；新建/恢复/重命名/归档。
- 输入区：多行输入、附件、`@path` 引用、快捷键、输入法。
- 设置：`ui.Router` 的子页面；表单控件；模型/effort 选择。

## 线程与后台工作

- 视图、事件回调、窗口操作在主线程。
- 从 goroutine 更新 UI 用 `Window.Update`；它异步安排一次新帧。
- 不要在主线程等待需要主线程的工作；宿主进程的 I/O 放在 goroutine，结果再 `Window.Update`。
- 通道读取、进程等待、文件读取都属于后台工作；UI 只消费不可变快照。

## 进程模型

MyGo 应用是一个 Go 二进制，不包含 JS 运行时。dsh-mygo 需要：

1. 启动时解析/初始化 profile。
2. 启动 Node Host 子进程（`dsh --profile mygo` 或私有 `mygo-host`）。
3. 通过 stdio 通道收发 JSON-RPC。
4. 把会话事件投影为 UI 状态。

MyGo 的 `os/exec` 足够管理子进程；Windows 下注意隐藏控制台窗口、使用 `CREATE_NO_WINDOW`，并在退出时确保子进程被回收。

## 打包与发布

- `mygo build` 生成 `.app` / `.dmg`（macOS）、`.exe` / 安装器（Windows）、Debian 包与 `install.sh`（Linux）。
- `resources/` 下的文件随应用分发；运行时用 `mygo.App.Path(mygo.PathResources)` 读取。
- 支持代码签名、公证、签名更新、delta 更新、更新窗口。
- `mygo.Updater` 是更新 API；`mygo.NewNotification`、`mygo.NewMenu`、托盘、深链、单实例都有对应能力。
- 原生 UI 的 release 构建可用 `-tags mygo_noinspector` 去掉 inspector。

dsh-mygo 需要把 Node Host、DSH runtime、profile seed 放进 `resources/`；这意味着安装包不会只有几 MB，但比 Electron + Chromium + 完整 Web 前端小。

## 约束与风险

- MyGo 仍在 `0.x`，API 会演进；升级时按 `docs/ui/migration.md` 检查元素/Handle/FocusBind 的变化。
- 原生 UI 需要自己实现所有界面；dsh-TUI 的体量说明 UI 工作量不小。
- 平台差异（macOS/Windows/Linux）需要实际验证；尤其字体、输入法、快捷键、窗口装饰。
- 原生 UI 不提供 webview 的 DOM/CSS 生态；Markdown、diff、代码编辑要用 MyGo 的组件，而不是前端库。
- 测试用 `ui.NewTester`，但它不能替代真实窗口的视觉/平台验收。

## 相关文档

- [研究：DSH 组合](dsh-composition.md)
- [研究：传输方案](transport-options.md)
- [UI 壳层](../subsystem/ui-shell.md)
- [打包与更新](../subsystem/packaging-updates.md)
