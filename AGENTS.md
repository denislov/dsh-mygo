# AGENTS.md — dsh-mygo

dsh-mygo 是 DeepSeek Harness 的原生桌面客户端：MyGo 原生 UI + Node Host（`dsh-base` + 自有 bundle）。不使用 `dsh-web-app`，不嵌入浏览器。

当前阶段：**路线 A（SDK 验证）→ 路线 C（自定义 channel）**。UI 选择 **MyGo native UI**。

- P0 已落地：`internal/host`、`internal/channel`、`cmd/spike`。
- P1 已落地：`internal/projection`、`internal/view`、`internal/app`、根 `main.go`；原生窗口可启动 Host、发送 prompt、投影提交事件、显示运行状态与错误。
- 自定义 channel 阶段 1 已落地：`resources/host` bundle、`mygo` profile 初始化、版本握手、显式创建会话和真实 Loader + mock 模型测试。默认协议为自定义 channel；SDK 回归须显式 `-sdk-protocol -profile sdk`。
- 回合取消已落地：`session.cancel` capability、`session/cancel`、原生 Stop 按钮、草稿保留，以及真实 agent loop 的 abort → idle → 同会话再次发送测试。
- 工具审批已落地：`approval` capability、Host answerer、有界队列、原生 Allow once / Deny / Stop turn，以及真实 approval 服务的允许/拒绝/取消执行验证。
- 用户提问已落地：`question` capability、阻塞式 `ask_user_question` 工具行、Host answerer、原生多问题表单（单选/多选/自由文本）、明确取消与真实工具闭环；无 agent 请求单独标注。
- 文本流式已落地：`session.assistant-stream` capability、公开帧转发、有界临时 text 投影、提交替换与顺序批处理；真实 agent loop 验证提交前可见、完成/取消/退出与同会话继续发送。
- 工具折叠/diff 已落地：默认折叠、稳定身份、有界参数/结果详情、FileDiff 元数据校验、只读原生 hunk 检视与大 hunk 文本回退；真实 write/edit 提交验证。
- Markdown/复制已落地：MyGo 原生 Stream/View、只读高亮代码、代码/保留源文复制、空白保留、截断/中断保护与 HTTP(S) 点击限制；不加载外部图片或执行代码/HTML。
- 会话历史已落地：`session.history` capability、公开 persistence/agents.resume、有界分页与原生 Sessions 弹窗、新建/恢复和内存草稿隔离；真实 App 重启回放后可继续发送，不持久化 UI 历史副本。
- 会话模型设置已落地：model.selection capability、白名单 provider/model/effort 目录、公开 installModelSelection、持久选择事件与原生 Settings；仅下回合生效，不更改启动默认或凭据，真实请求验证模型切换与 effort 清除。
- 配置闭环已落地：settings.configuration capability、公开 settings/agentDefaultModel/credentials 服务、revision 保护的持久默认模型、DeepSeek 绑定凭据的安全状态/只写/清除；默认启动读取 Host 配置，显式路由 flags 覆盖。真实隔离 Host 重启和 Windows 原生操作验证，不触碰项目 .env。
- 未落地（P2）：timed/continued 提问、实时缺口自动 resync、固定侧栏与会话搜索/管理、任意命名空间设置/多 provider 凭据与账户登录、RPC 请求级取消、打包。

## 文档路由

改代码前，先读对应文档：

| 你要改什么 | 先读 |
| --- | --- |
| 文档索引与阅读路线 | [docs/README.md](docs/README.md) |
| 子系统索引 | [docs/subsystem/README.md](docs/subsystem/README.md) |
| 整体结构、进程边界、数据流 | [docs/architecture.md](docs/architecture.md) |
| profile / bundle / DSH 组合 | [docs/subsystem/profile-bundle.md](docs/subsystem/profile-bundle.md)、[research/dsh-composition.md](docs/research/dsh-composition.md) |
| Host 启动、stdio、崩溃、退出 | [docs/subsystem/host-supervisor.md](docs/subsystem/host-supervisor.md) |
| JSON-RPC 方法/通知/版本/取消 | [docs/subsystem/channel-protocol.md](docs/subsystem/channel-protocol.md) |
| 会话事件 → UI 状态、流式、历史 | [docs/subsystem/session-projection.md](docs/subsystem/session-projection.md) |
| 审批、用户提问、权限模式 | [docs/subsystem/interaction.md](docs/subsystem/interaction.md) |
| 设置、模型、凭据、UI 偏好 | [docs/subsystem/settings-credentials.md](docs/subsystem/settings-credentials.md) |
| MyGo 原生 UI、页面、输入、性能 | [docs/subsystem/ui-shell.md](docs/subsystem/ui-shell.md) |
| 打包、签名、更新 | [docs/subsystem/packaging-updates.md](docs/subsystem/packaging-updates.md) |
| 测试策略 | [docs/subsystem/testing.md](docs/subsystem/testing.md) |
| 为什么这样选 | [docs/research/](docs/research/README.md) |
| MyGo API 与生命周期 | [research/mygo-native-ui.md](docs/research/mygo-native-ui.md)；MyGo 源码 `docs/ui/` |
| MyGo 维护规则（元素身份、线程、测试） | [.agents/skills/mygo-maintenance/SKILL.md](.agents/skills/mygo-maintenance/SKILL.md) |

## 硬规则

1. **Native UI only.** 不使用 webview、HTML、前端构建。窗口用 `WindowOptions.Content: ui.View(...)`。
2. **不加载 `dsh-web-app`.** 不引入 `host-webserver`、`client-connection`、`client-modules`、`client-ui-*`、`dsh-app://`、`WebContentsView`、`<webview>`、session partition。
3. **会话真相在 Host.** DSH session log 是唯一真相；UI 只做投影，不把本地状态回写成历史。
4. **必须有 answerer.** 新增审批/提问能力时，同时实现 Host 侧 answerer；没有 answerer 的工具调用会挂起。
5. **stdout 是协议.** Host 的 stdout 只能写 JSON-RPC 帧；日志走 stderr。Go 侧不得把日志写进协议管道。
6. **进程边界校验.** 所有入站 JSON 校验字段、类型、大小；不信任 UI 输入。跨进程 id/路径按不可信处理。
7. **不记录 secret.** API key、token、cookie、附件内容不得进入日志、崩溃报告、错误消息。
8. **协议版本化.** 破坏性变更提升 `protocolVersion`；新能力通过 capability 声明；旧 Host 不认识的能力不启用。
9. **MyGo 元素是帧作用域的.** 不把 `ui.Element` / `ui.Context` 存进 app 状态或 goroutine；跨帧状态用字段、`ui.Handle`、`FocusBind`、`c.Key`。
10. **主线程只做 UI.** 慢 I/O 与进程等待在 goroutine；结果用 `Window.Update` 发布。
11. **版本绑定.** UI、Host、bundle 作为同一发布单元验证；不要只升级其中一个。
12. **profile 是组合根.** 不改 DSH core；通过 bundle patch 增加/覆盖/禁用行。行 id 是兼容接口。

## 仓库布局

当前：

```text
main.go              MyGo 入口：flag、窗口、Host 生命周期
mygo.json            MyGo 配置
cmd/spike/           P0 协议验证：initialize → prompt → events
internal/channel/    JSON-RPC 编解码与客户端
internal/host/       HostSupervisor + ResolveLaunch：spawn / stdio / stderr / stop
internal/projection/ 提交的 session.event → transcript 行
internal/view/       MyGo 原生界面（Model、List、Composer、状态）
internal/app/        Host + channel + projection + window.Update 装配
testdata/            fake-sdk-host.mjs 等测试数据
resources/host/      自定义 Host bundle、profile 初始化、Node 协议测试
resources/           随应用分发的资源
docs/                研究、架构、子系统文档
AGENTS.md            本文件
```

目标布局（按阶段落地）：

```text
internal/
  host/              HostSupervisor
  channel/           JSON-RPC client/server
  projection/        session 投影
  interaction/       审批/提问身份、载荷校验、FIFO 与表单状态
  view/              页面、组件、状态（不叫 ui，避免与 MyGo ui 包重名）
  app/               App 状态与装配
  platform/          菜单、托盘、深链、单实例
resources/            Node、DSH runtime、profile seed
docs/
```

## 命令

```sh
go tool mygo dev                    # 开发运行（默认 dsh --profile mygo；先初始化 profile）
go tool mygo build                  # 打包
go test ./...                       # 单元测试
go test -race ./...                 # 并发改动
CGO_ENABLED=0 go test ./...         # 纯 Go 构建
go tool mygo vet .                  # UI 存储值与 goroutine 捕获

# 原生窗口 + fake host（不需要 DSH 或 API key）
go run . -host-command node -host-arg testdata/fake-mygo-host.mjs

# 原生窗口 + 真实 DSH source checkout（key 只从环境读取）
node resources/host/setup-profile.mjs --dsh-home <临时目录的绝对路径>
go run . -dsh-dir D:/Workspace/deepseek-harness -dsh-home <临时目录>

node --test resources/host/tests/*.test.mjs
# 设置 DSH_MYGO_DSH_DIR 后，无需 key 的真实组合测试
go test ./internal/app -run TestAgainstMygoRealComposition -v -timeout 2m
```

DSH 侧（在 DSH checkout 中）：

```sh
pnpm run build:artifacts
pnpm run typecheck
pnpm run test
```

真实 Host 集成需要 DSH 的 `dsh` 可执行文件与 Node；测试使用临时 `DSH_HOME`。

## 文档约定

- `docs/research/` 回答“为什么”；不要在里面写当前实现契约。
- `docs/architecture.md` 是跨系统主页；一个事实只写一次，其他文档链接。
- `docs/subsystem/` 一子系统一文档：职责、接口、失败、测试。
- 未定事项放“未决问题”；不要写成既成事实。
- 改行为时同步改对应文档；改协议时同时改 [channel-protocol.md](docs/subsystem/channel-protocol.md) 与 [session-projection.md](docs/subsystem/session-projection.md)。
- 新增文档后，在本文件的“文档路由”里加一行。

## 验证

- 改投影：跑 `go test ./internal/projection` 与 `go test ./internal/app`（fake host 全链路）。
- 改协议：跑 conformance fixture + 真实 Host 集成。
- 改 UI：`ui.NewTester` + 真实窗口检查（macOS/Windows）；真实窗口可用 fake host 手动 smoke。
- 改 Host/打包：启动 smoke、stdout 纯净、退出回收。
- 提交前报告实际跑过的命令，不报告未跑的检查。

## 提交

- 每完成一个可验证的任务做一个提交；不要把多个无关任务混进同一个提交。
- 提交信息写明阶段或模块，并在正文列出实际跑过的检查。
- 任务未完成或检查未通过时不提交；需要中间检查点时先说明。
