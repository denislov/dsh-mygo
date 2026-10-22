# AGENTS.md — dsh-mygo

dsh-mygo 是 DeepSeek Harness 的原生桌面客户端：MyGo 原生 UI + Node Host（`dsh-base` + 自有 bundle）。不使用 `dsh-web-app`，不嵌入浏览器。

当前阶段：**自定义 channel 可用（默认 `mygo` profile），SDK 通道只作显式回归**。首发平台 Windows（x64），发布包自带 Node + DSH runtime；macOS 随后。进度、缺口与里程碑只维护在 [docs/roadmap.md](docs/roadmap.md)，本文件不重复状态清单。

- 默认协议：`initialize` 协商版本与能力，随后 `session/create|list|resume|history|prompt|cancel`；通知为 `session/event`、`session/assistant-stream`、`session/status` 与交互请求。审批、提问、模型、配置各自通过 capability 协商。
- UI 能力：原生 Markdown 流式与代码/源文复制、工具折叠与只读 diff、Sessions 弹窗、Settings（会话模型、持久默认模型、DeepSeek 凭据）。
- 发布形态：MyGo 二进制 + `resources/`（私有 `mygo-host`、Node、DSH runtime、profile seed）；落地顺序见路线图。
- 实现顺序看路线图：timed/continued 提问、实时缺口自动 resync、固定侧栏与会话管理、通用命名空间设置、多 provider 凭据、账户登录、RPC 请求级取消、打包按路线图推进。设计文档保留这些目标，不因尚未实现而删除；本文件不重复状态清单。

## 文档路由

改代码前，先读对应文档：

| 你要改什么 | 先读 |
| --- | --- |
| 文档索引与阅读路线 | [docs/README.md](docs/README.md) |
| 当前进度、缺口与下一步路线 | [docs/roadmap.md](docs/roadmap.md) |
| 经验教训（开发过程） | [LESSONS.md](LESSONS.md) |
| 固化后的编码/文档规范 | [docs/code-standards.md](docs/code-standards.md) |
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
versions.json        发布身份单点：identifier、版本、protocolVersion、DSH pin
.github/workflows/   CI（Windows 全量、Linux Go 测试）
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

- **设计文档描述目标态，不是现状快照。** `docs/architecture.md` 与 `docs/subsystem/` 描述项目最终设计目标与契约；未实现的功能保留在设计中，不能因为代码还没做而删除。进度、缺口与"何时完成"只写 [docs/roadmap.md](docs/roadmap.md) 与 git log。
- **只有实现迫使设计变更时才改设计文档。** 日常实现进度变化不改目标；当开发发现契约、结构或流程必须调整时，修改受影响的设计文档现有段落，保持描述口吻，不写"已落地/已实现/尚未实现"等变更历史。
- **一个事实一个主页。** 架构文档管跨系统结构，子系统文档管目标契约；状态、缺口与里程碑只在 [docs/roadmap.md](docs/roadmap.md)；经验教训只在 [LESSONS.md](LESSONS.md)；重复 3 次的教训或项目所有者直接指定的规则固化到 [docs/code-standards.md](docs/code-standards.md)。
- **提交信息记录影响面。** 按 [D-002](docs/code-standards.md#d-002-任务完成后用-git-commit-记录影响子系统--实现功能) 写：标题 `type(scope): summary`，正文必须列出**影响子系统**与**实现功能**（按子系统各写具体能力/契约），再列契约变化与实际跑过的检查。
- **任务开始前通读 `docs/`。** 先读全部设计文档与路线图，确认目标不偏离整体设计。
- `docs/research/` 回答“为什么”；不要在里面写当前实现契约。
- 未定事项只放路线图的“未决问题”，不要写成既成事实。
- 改协议时同时改 [channel-protocol.md](docs/subsystem/channel-protocol.md) 与 [session-projection.md](docs/subsystem/session-projection.md)。
- 新增文档后，在本文件的“文档路由”里加一行。

## 验证

- 改投影：跑 `go test ./internal/projection` 与 `go test ./internal/app`（fake host 全链路）。
- 改协议：跑 conformance fixture + 真实 Host 集成。
- 改 UI：`ui.NewTester` + Windows 真实窗口检查；macOS 随后。
- 改 Host/打包：启动 smoke、stdout 纯净、退出回收。
- 改文档：核对链接可达、设计契约自洽；实现与目标的差距记录在路线图，不要求逐条已实现，也不要为了对齐现状删除设计目标；设计文档中不得出现进度/变更历史口吻。
- 提交前报告实际跑过的命令，不报告未跑的检查。

## 提交

- 每完成一个可验证的任务做一个提交；不要把多个无关任务混进同一个提交。
- 提交信息按 [D-002](docs/code-standards.md#d-002-任务完成后用-git-commit-记录影响子系统--实现功能) 写：标题 `type(scope): summary`；正文列出**影响子系统**与**实现功能**（按子系统各写具体能力/契约），再列契约变化与实际跑过的检查。禁止 `update`、`fix` 这类无信息提交。
- 任务未完成或检查未通过时不提交；需要中间检查点时先说明。
