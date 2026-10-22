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
node --test resources/host/tests/*.test.mjs
go test ./internal/channel ./internal/app
```

回合取消已由 Node/Go 测试覆盖：能力协商、外来 session、空闲取消、并发合并、Host 超时、错误隔离、立即取消的请求顺序、旧响应隔离和草稿保留。`ui.NewTester` 覆盖 Stop → Stopping → Send → Stop 的生命周期与无 capability 时隐藏按钮。

审批已有 Node 队列、Go 状态、App 和 ui.NewTester 覆盖：一次性回答、撤销、重复/过期身份、队列上限、参数超限、关联与 external 警告、FIFO、防重复点击、失败重试、Escape 拒绝和草稿保留。RPC 请求取消等尚未实现的协议随功能落地增加对应 fixture。完整目标断言：

提问覆盖整批答案校验、空答案拒绝、单选互斥、多选混合文本、无 agent/外来 agent、capability、FIFO/队列上限、abort/停止/退出、重复/过期回答、失败重试保留表单、连续表单身份和审批优先不叠加 Modal。`TestAgainstMygoQuestions` 包含 fake 与真实 Loader 两组，各验证 answer/cancel/stop/shutdown；真实组调用正式 `ask_user_question` 工具，不需要 API key。

流式覆盖 capability/确切 agent 作用域、帧校验、revision/index 缺口、重复、重试、abandoned、提交前后交错、可见文本与 Unicode 上限、reasoning/工具参数隐藏、idle 清理，以及有界批处理顺序和关闭中断背压。已有取消回归同时验证 RPC 回调不会越过尚未发布的提交事件。

`TestAgainstMygoRealStreaming` 使用真实 DSH agent loop 与 mock adapter，在随机 localhost 测试控制端口暂停生成。先断言 UI 已显示 `Live prefix` 而 durable transcript 尚无 assistant，再放行完整答案或 Stop/退出；验证唯一最终行、保留草稿与同会话 pong。不依赖付费模型，也不靠固定延时猜测真实流式时点。fake `--stream` / `--stream-wait` 和 ui.NewTester 覆盖原生行生命周期与旧 Host 仅提交模式。

工具详情覆盖结果先到的精确配对、不可变快照、64 KiB UTF-8 截断、4 MiB 淘汰、非法元数据整批回退、创建空 diff、多文件/hunk 与大 hunk 文本降级。ui.NewTester 验证默认折叠、稳定身份、详情页切换、只读 DiffView、更新/淘汰释放缓冲、Escape 关闭和交互优先；动画后点击使用测试时钟推进，不靠真实等待。

`TestAgainstMygoToolDiffs/real` 通过真实 `write` / `edit` 工具在临时工作区创建并编辑文件，验证创建无 hunk 的元数据与真实 before/after hunk，不调用付费模型。fake `--tool-diff` 提供多路径 hunk，供 Windows 原生 UI Automation 检查折叠、详情、导航、关闭与 Host 回收，不触碰真实文件。

Markdown 的投影测试验证前导缩进、末尾换行、开放围栏和截断标记；ui.NewTester 验证原生格式、中文代码精确复制、完整源文复制、开放/中断代码禁用复制、截断原文回退、反馈身份以及用户/工具原文隔离。链接测试使用 Tester 的虚拟 URL 服务，不实际打开网站；不安装图片/HTML 加载器。

`resources/host/tests/history.test.mjs` 覆盖 history 能力协商、拥有句柄、固定前缀与列表快照、forward 分页、游标作用域/过期/淘汰、页与目录预算、busy/非法输入、短页/乱序拒绝、错误脱敏、失败重试、退出中的恢复与句柄关闭。`TestMygoRealHistoryAfterRestart` 使用真实 Loader、JSONL persistence 和 mock adapter：第一进程完成并关闭，第二进程 list/resume/分页回放，确认原始日志前缀不变、继续 pong，使用现有投影得到恰好两个助手答案；额外进程验证单写者争用拒绝。设置 DSH_MYGO_DSH_DIR 运行，不需要 API key。

原生会话阶段另由 Go reader/App/ui.NewTester 覆盖：字段类型、空/重复摘要、分页边界和游标循环、部分页失败不发布、旧响应隔离、空闲导航门禁、草稿往返、历史与实时重复/追加交错、刷新取消、Host 失败后迟到结果、工具/复制/编辑器身份清理、交互优先、加载防重复、Escape 与最小窗口。`TestAgainstMygoSessionSwitching/{fake,real}` 驱动实际 App：新建/列举/切回继续第二回合、两份草稿隔离；real 额外重启到同一 DSH_HOME，从列表打开原会话并继续第三回合，不调用付费模型。

fake host 的 `--sessions` 提供内存 persistence 和 `demo-history` 已存会话，供 Windows 原生 UI Automation 检查 Sessions 弹窗、历史正文、恢复后发送、新建、草稿往返、窄窗口布局与退出回收。它不承诺跨 fake Host 重启持久化；真实持久化由真实 Loader 测试证明。macOS 原生窗口仍需平台验收。

模型设置：Node models.test.mjs 覆盖能力协商、白名单目录/secret 排除、目录与帧预算、元数据失败、字段与 busy 校验、持久事件提交失败回滚、并发写入预约、校验超时和退出后拒绝迟到提交。Go reader/App 测试覆盖目录 schema、Host 确认后更新、缺失选择事件时失败、表单/草稿保留、读取取消与迟到响应隔离、提交结果未确认时禁止发送/切换直到 Host Refresh 完成；ui.NewTester 覆盖 Apply 防重复、effort/default、Escape、旧 Host 隐藏入口、未确认模型提示和最小窗口。

`TestAgainstMygoModelSettings/{fake,real}` 在已完成一次请求的同一会话切到 route-probe/high，断言实际生成请求返回选择的 provider/model/effort；再清除 effort，断言下一请求不继承 high。新建不改变启动默认，切回已有会话保留其模型并可继续请求。real 使用正式公开 installModelSelection、真实 Agent loop 和 mock adapter，不使用付费 API。

fake `--models` 暴露 test provider 与 echo/route-probe，可结合 --sessions 演示。Windows 原生窗口检查 Settings、手动模型应用、实际后续回复、错误反馈、草稿保留、窄窗口与退出回收；effort 选择/清除由 Tester 与真实 Loader 请求测试验证，不以 UI Automation 不支持的只读 Value/SelectionItem 模式冒充原生点击验收。

持久配置：Node configuration.test.mjs 覆盖 capability、固定白名单、revision 冲突、绑定变更、secret/source 脱敏、空值/长度/控制字符拒绝、busy、失败后必须刷新、未完成写入的读写屏障、清除后 ambient fallback。Go 覆盖配置 schema/8 KiB 预算、安全错误、默认保存不改当前模型/草稿、失败写入隔离，以及 CLI 显式 flags（包括空 effort）优先级。Tester 验证密码输入/提交/控件重建清空、调试格式化 redaction、最小窗口操作和写入期间 Escape 禁用。

`TestAgainstMygoConfiguration/{fake,real}` 使用临时 cwd 与 DSH_HOME，只绑定 MYGO_TEST_ONLY_KEY 并移除子进程的测试引用/官方 key 环境值，真实 source runner 固定 TSX_TSCONFIG_PATH 以在隔离 cwd 启动。不读项目 .env，不调用付费模型。验证公开 settings/credentials 写入、安全状态、Host 默认新建后的真实 mock 路由、effort 清除、冷恢复后的真实请求，以及真实 Host 重启后的模型/凭据保留。fake `--configuration` 仅内存模拟，与 --models/--sessions 组合。

Windows 原生已检查 Save selection as default、Password 的 IsPassword/无可读明文、Save key 后状态/输入清空、勾选确认后清除、保存默认后的 New conversation 实际回复、700×560 物理窗口截图与退出时 Host 回收。macOS 原生验收尚未执行。

`TestAgainstMygoRealStreaming/markdown` 与 `markdown-cancel` 在真实 Loader mock adapter 暂停点验证完整/中断 Markdown 的源文（包括空白）从 stream 到 durable 提交保持一致，之后同会话继续 pong。fake `--markdown` / `--markdown-wait` 供 Windows 原生渲染、代码/消息复制和系统剪贴板精确内容验收，无付费模型调用。

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

`TestAgainstMygoRealApprovals` 使用同一真实 Loader 和 mock adapter，触发真实 approval 服务：允许后才写临时目录的标记文件，拒绝/取消不写，随后在同一 session 完成 pong 并正常退出；另验证待审批时退出会清空面板、回收 Host 且不执行工具。无需 API key。`TestAgainstMygoFakeApprovals` 覆盖 fake Host 的相同三条路径与草稿保留；Windows 原生窗口可用 `--approval` fixture 验收。

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
