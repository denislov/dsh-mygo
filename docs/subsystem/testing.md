# 测试

## 目标

测试要证明三件事：

1. **投影正确**：同一段 session log 在 UI 里得到稳定、可解释的结果。
2. **协议可靠**：请求、通知、取消、重连、非法帧都按契约处理。
3. **Host 真实可用**：不是只在 fake 上通过，而是能启动真实 DSH profile 完成回合。

## 测试层级

| 层 | 范围 | 工具 |
| --- | --- | --- |
| Go 单元 | 投影、状态机、编解码、错误分类 | `go test`、表驱动 |
| UI | 视图、输入、焦点、弹窗、快捷键 | `ui.NewTester` |
| 协议 | 帧、版本、取消、顺序、背压 | fake transport + fixtures |
| Host 集成 | 真实 `dsh --profile mygo`、真实 session log | 进程测试 |
| DSH 组合 | real-composition：Loader 启动 profile | DSH 测试工具 |
| E2E | 真实模型或 fake provider | `DEEPSEEK_API_KEY` 可选 |
| 打包 | 安装、启动、更新、签名 | 平台 CI |

## Go 单元测试

- 投影：输入 fixture 事件序列，断言 `TurnView` / `ItemView` / `ToolCallView`。
- 流式：start/chunk/end、缺口、abandoned、重连 baseline。
- 历史：分页、去重、历史与实时混合。
- 协议：合法/非法帧、未知方法、超时、`$/cancel`、重复回答。
- 错误：分类、用户可读消息、诊断字段。
- 纯函数优先；I/O 用接口注入。

### 命令

```sh
go test ./...
go test -race ./...
CGO_ENABLED=0 go test ./...
go tool mygo vet .
```

`go tool mygo vet` 检查存储的构建值与 goroutine 捕获；UI 改动必须跑。

## UI 测试

用 `ui.NewTester(view, width, height)`：

- 点击按钮、输入文本、按快捷键、切换焦点。
- 断言 `HasText` / `Texts`，不断言像素。
- 测试宽度变化、长标题、空状态、错误状态、深色主题。
- 弹窗：打开、Escape、Tab、焦点回收。
- 列表：滚动、选择、虚拟化边界（首行/末行/空列表）。
- 流式：追加 chunk 后文本出现；提交后草稿被替换。

规则：

- 视图里不要做 I/O；测试因此无需 mock 网络。
- 用 fake `ChannelClient` 提供固定状态。
- 一帧可能构建多次；测试要覆盖元素消失/返回的场景。

P1 的 `internal/view/view_test.go` 覆盖：transcript 文本、Enter 发送、Shift+Enter 换行、running 时禁止发送、发送按钮、错误横幅关闭。

## 协议 conformance

阶段 1 已落地的 fixture：

```text
tests/fixtures/protocol/
  invalid-frames.json
```

Go channel 与 Node transport 共用非法帧 fixture；握手、重复初始化、模型路由校验、busy/外来 session、text content 校验、输出背压、顺序响应、shutdown 和错误信息隔离由 `resources/host/tests/protocol.test.mjs` 与 Go App 测试覆盖。

```sh
node --test resources/host/tests/protocol.test.mjs
go test ./internal/channel ./internal/app
```

回合取消已由 Node/Go 测试覆盖：能力协商、外来 session、空闲取消、并发合并、Host 超时、错误隔离、立即取消的请求顺序、旧响应隔离和草稿保留。`ui.NewTester` 覆盖 Stop → Stopping → Send → Stop 的生命周期与无 capability 时隐藏按钮。

审批、RPC 请求取消、流式等尚未实现的协议随功能落地增加对应 fixture。完整目标断言：

断言：

- 每个请求都有响应或明确超时。
- 非法帧关闭连接，不尝试恢复。
- 取消只影响对应请求。
- 重复回答返回 `stale-request`。
- 版本不匹配时拒绝启动。
- 流式缺口丢弃草稿。

## Fake Host

- `testdata/fake-mygo-host.mjs` 复用正式 `MygoServer` 和 `LineChannel`，替换 DSH 服务；支持显式创建 session、两个连续回合、工具投影、错误版本与无效 session 响应。`--wait-for-cancel` 让第一回合保持运行以验证 Stop，后续回合正常回复；`--no-cancel` 模拟旧 Host。默认窗口 smoke 使用此入口。
- `testdata/fake-sdk-host.mjs` 是最小 SDK 协议 host：initialize、prompt、一个提交回合（user、tool call/result、assistant）、wait、shutdown；prompt 文本会回显为 `Echo: <text>`。
- HostSupervisor 的 helper process 另覆盖退出和超时；默认 fake host 不模拟所有失败模式。
- 用于 `HostSupervisor` 状态机测试、`internal/app` 全链路测试与手动 smoke；不验证真实模型行为。
- 必须能验证进程树回收（Windows Job Object / POSIX 进程组）。

P0 协议 spike：

```sh
go run ./cmd/spike -host-command node -host-arg testdata/fake-sdk-host.mjs -prompt "hello"
```

P1 全链路（Go 测试，Node 不在时跳过）：

```sh
go test ./internal/app -run 'TestAgainstFakeHost|TestAgainstMygoFakeHost|TestMygoRejectsInvalidHost' -v
```

P1 手动窗口 smoke：

```sh
go run . -host-command node -host-arg testdata/fake-mygo-host.mjs
```

## Host 集成

用真实 DSH：

```sh
node resources/host/setup-profile.mjs --dsh-home /absolute/temp/dsh-mygo
DSH_HOME=/absolute/temp/dsh-mygo dsh --profile mygo
```

测试：

- `initialize` → `session/create` → `session/prompt` → 收到 `session/event`。
- 审批：触发一个需要审批的工具，走 `approval/request` / `approval/respond`。
- 提问：触发 `ask_user_question`，走 `question/request` / `question/respond`。
- 历史：新建会话、跑一个回合、重启、`session/resume` + `session/history`。
- 取消：长回合 `session/cancel`，确认状态回到 idle。
- stdout 纯净：Host 的任何日志都不能出现在 stdout。

无 API key 时用 fake provider 或只跑不调用模型的控制面测试；有 key 时跑真实 e2e。

P1 的真实链路测试（无 key 或未设置 checkout 时自动跳过）：

```sh
DEEPSEEK_API_KEY=... DSH_MYGO_DSH_DIR=D:/Workspace/deepseek-harness \
  go test ./internal/app -run TestAgainstRealDSH -v -timeout 5m
```

测试使用临时 `DSH_HOME`，只发送一个 `Reply with exactly: pong` 回合，不写入用户数据。

自定义 channel 的真实模型测试为 `TestAgainstMygoRealModel`：不加载 mock overlay，使用 `deepseek-official/deepseek-flash`，在临时工作目录和临时 `DSH_HOME` 中验证精确 `pong` 回复及 Host 正常退出。需设置 `DEEPSEEK_API_KEY` 与 `DSH_MYGO_DSH_DIR`；缺失时跳过。

项目根目录 `.env` 可通过 Node 加载到测试子进程环境，凭据不进入命令参数：

```sh
node -e 'process.loadEnvFile(".env"); process.env.DSH_MYGO_DSH_DIR = "D:/Workspace/deepseek-harness"; const result = require("node:child_process").spawnSync("go", ["test", "./internal/app", "-run", "^TestAgainstMygoRealModel$", "-v", "-count=1", "-timeout", "5m"], { stdio: "inherit", env: process.env }); process.exit(result.status ?? 1)'
```

## DSH real-composition

阶段 1 已落地 `TestAgainstMygoRealComposition`：设置 `DSH_MYGO_DSH_DIR` 即可运行，不需要 API key。测试创建临时 `DSH_HOME`、初始化真实 `mygo` profile，再用 `--patch` 加入 `testdata/mygo-mock-llm.mjs`。通过真实 CLI/Loader、session log 和 agent loop 完成一轮 `pong`，严格校验协议帧、连续 seq 和退出码。Host 自身校验 `profileContext.startedBundles` 包含两个 bundle。测试不修改 DSH checkout，也不手工 `ctx.plugin(...)` 拼装应用。

`TestAgainstMygoRealCancellation` 进一步验证真实 agent loop 的取消：mock adapter 用随机 localhost 端口仅向测试进程发送 `started` / `aborted` 信号；测试等待真实模型 stream 开始后调用 Stop，断言 abort signal、提交的取消 notice、草稿保留，并在同一 session 再完成 `pong`。该控制端口只存在于 fixture，不是产品 Host transport，也不需要 API key。

```sh
go test ./internal/app -run TestAgainstMygoRealComposition -v -timeout 2m
go test ./internal/app -run TestAgainstMygoRealCancellation -v -timeout 2m
```

bundle 必须有一个 real-composition 测试：

- 通过 Loader 启动 `dsh-base` + `@dsh-mygo/host` 的真实组合。
- 断言行存在、`profileContext` 正确、channel server 就绪、stdout 未被污染。
- 不只用手工 `ctx.plugin(...)` 拼装。

## 打包测试

- 安装包启动 smoke：窗口打开、Host ready、完成一个不调用模型的回合。
- 签名：macOS `codesign --verify` / 公证；Windows `signtool verify`。
- 更新：旧版本 → 新版本，会话/设置保留；delta 校验。
- 卸载保留 `$DSH_HOME`。
- 断网、磁盘满、权限错误。

## 平台矩阵

| 平台 | 必测 |
| --- | --- |
| macOS arm64 | 窗口、输入法、托盘、签名/公证、更新 |
| macOS x64 | 同上（如发布） |
| Windows x64 | 窗口、输入法、托盘、Job Object、安装器、签名 |
| Linux（如支持） | 原生 UI 渲染、依赖、安装包 |

DSH 的会话与设置跨平台共享；测试不要写入用户真实 `$DSH_HOME`。

## 确定性与并发

- 测试不依赖 wall clock；用 fake clock 或事件等待。
- 并行测试各自拥有临时 `DSH_HOME`、端口、进程。
- 不在测试之间共享 profile/node_modules。
- 进程测试必须在 teardown 等待退出；泄漏进程视为失败。
- UI 测试不并行共享窗口；`ui.NewTester` 每个测试独立。

## CI 建议

| 阶段 | 命令 |
| --- | --- |
| Go | `go test ./...`、`go vet`、`go tool mygo vet .` |
| UI | 同上（Tester） |
| 协议 | `go test ./internal/channel/...` |
| Host | 需要 DSH checkout 与 Node；启动真实 profile |
| E2E | 有 key 时跑；无 key 跳过 |
| 打包 | 平台 runner 上构建、签名、安装 smoke |

## 不测试什么

- 不测试 Electron/Web 行为。
- 不测试 MyGo 自身控件；只测试我们的视图与状态。
- 不用快照测试像素；视觉验收在真实窗口完成。

## 相关文档

- [架构](../architecture.md)
- [会话投影](session-projection.md)
- [通道协议](channel-protocol.md)
- [宿主监督](host-supervisor.md)
