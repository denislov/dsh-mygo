# 测试

## 目标

测试证明三件事：

1. **投影正确**：同一段 session log 在 UI 里得到稳定、可解释的结果。
2. **协议可靠**：请求、通知、取消、非法帧都按契约处理。
3. **Host 真实可用**：不是只在 fake 上通过，而是能启动真实 DSH profile 完成回合。

## 测试层级

| 层 | 范围 | 工具 |
| --- | --- | --- |
| Go 单元 | 投影、状态机、编解码、交互队列、UI Model | `go test`、表驱动 |
| UI | 视图、输入、焦点、弹窗、快捷键 | `ui.NewTester` |
| 协议 | 帧、版本、取消、顺序、背压 | `resources/host/tests` + fixtures |
| Host 集成 | 真实 `dsh --profile mygo`、真实 session log | 进程测试 |
| DSH 组合 | Loader 启动 profile | 临时 `DSH_HOME` + 真实 CLI |
| E2E | 真实模型或 mock adapter | `DEEPSEEK_API_KEY` 可选 |
| 打包 | 安装、启动、更新、签名 | 平台 CI |

## Go 单元

- 投影：`user/message`、`assistant/message`、`tool/call|result`、`turn/end`、重复 seq、未知事件、截断。
- 流式：start/chunk/end、缺口、abandoned、提交前后交错、reasoning 与工具参数不泄露。
- 历史：分页、边界、游标、部分页失败不发布、历史与实时合并。
- 协议：合法/非法帧、版本、未知方法、超时、取消、重复回答、背压。
- 交互：审批/提问的载荷校验、FIFO、上限、过期身份、表单草稿。
- 会话/设置：摘要校验、切换门禁、草稿隔离、模型确认事件、配置 revision 与写入屏障。
- 会话管理：rename/archive/delete/fork 的参数、事件、列表更新与冲突。
- 恢复：seq 缺口 resync、baseline 重连、resync 失败时的停止路径。
- 通用设置与偏好：命名空间白名单、secret 排除、UI 偏好 schema/原子写/损坏回退。
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

`ui.NewTester(view, width, height)` 驱动视图：

- 点击、输入、快捷键、焦点、弹窗、列表滚动与最小窗口。
- 断言 `HasText` / `Texts` 与可见状态，不断言像素。
- 用 fake `ChannelClient` 或直接构造 `view.Model` 提供固定状态。
- 覆盖：transcript 行、Enter/Shift+Enter、停止生命周期、审批/提问表单、Markdown 与复制、工具折叠/检视器、Sessions/Settings 弹窗、错误横幅、配置密码清空。
- 目标态 UI：侧栏、搜索、重命名/归档/删除/fork、附件与 `@`、设置分区、主题/语言、resync 指示与更新提示。
- 一帧可能构建多次；测试覆盖元素消失/返回、控件身份变化、弹窗关闭后的焦点回收。

真实窗口检查在 Windows 与 macOS 上执行；Windows 是首发门槛。

## 协议 conformance

```text
tests/fixtures/protocol/
  invalid-frames.json
```

Go channel 与 Node transport 共用非法帧 fixture。`resources/host/tests/protocol.test.mjs` 覆盖握手门槛、版本/能力协商、非法 JSON/帧、超长输入、未终止 EOF、输出背压、请求串行、shutdown 与错误信息隔离；`history.test.mjs`、`models.test.mjs`、`configuration.test.mjs`、`approval.test.mjs`、`question.test.mjs` 分别覆盖对应子系统的边界与预算。目标态还需覆盖 `$/cancel`、`subagent/*`、附件帧、`session/rename|archive|delete|fork` 与 resync。

## Fake Host

`testdata/fake-mygo-host.mjs` 复用正式 `MygoServer` 与 `LineChannel`，替换 DSH 服务，支持：

- 显式创建会话、连续回合、工具投影、错误版本与无效 session 响应。
- `--wait-for-cancel`：第一回合保持运行以验证 Stop；`--no-cancel` 模拟旧 Host。
- `--approval`、`--question` / `--question-twice`：审批与提问表单。
- `--stream` / `--stream-wait` / `--no-stream`：流式、取消流式与旧 Host 仅提交模式。
- `--tool-diff`、`--markdown` / `--markdown-wait`：工具详情与 Markdown 渲染。
- `--sessions`、`--models`、`--configuration`：会话、模型与配置面板。
- `--bad-version`、`--bad-session` 等错误注入。

`testdata/fake-sdk-host.mjs` 是最小 SDK 协议 host，用于显式回归模式。fake host 只验证 UI/协议适配，不验证真实模型行为。

## Host 集成

真实 Loader 测试使用临时 `DSH_HOME`，不会写入用户数据：

```sh
$env:DSH_MYGO_DSH_DIR = 'D:/Workspace/deepseek-harness'
go test ./internal/app -run 'TestAgainstMygo|TestMygoReal' -v -timeout 10m
node --test resources/host/tests/*.test.mjs
```

不设置 `DSH_MYGO_DSH_DIR` 时，真实组合组自动 skip；报告结果时要区分"跑了"与"跳过"。

覆盖范围：

- `TestAgainstMygoRealComposition`：真实 CLI/Loader、profile、stdout 纯净、连续 seq、退出码。
- `TestMygoRealProtocolLifecycle`：错误版本、非法帧、initialize 前 shutdown、stdin EOF。
- `TestAgainstMygoRealCancellation` / `TestAgainstMygoRealApprovals` / `TestAgainstMygoQuestions`：真实 agent loop 的取消、审批与提问。
- `TestAgainstMygoRealStreaming`：mock adapter 暂停生成，验证流式先可见、提交替换、取消与同会话继续。
- `TestMygoRealHistoryAfterRestart`：两个进程共享同一 `DSH_HOME`，验证列表、恢复、分页回放与继续发送。
- `TestAgainstMygoSessionSwitching`、`TestAgainstMygoModelSettings`、`TestAgainstMygoConfiguration`、`TestAgainstMygoToolDiffs`：真实会话切换、模型请求、配置持久化与 write/edit diff。

需要真实模型与 key 的 `TestAgainstMygoRealModel` 默认跳过；运行前确认会产生 API 调用费用。

## 打包测试

- 安装包 smoke：窗口打开、Host ready、完成一个不调用模型的回合。
- 签名：Windows `signtool verify`；macOS `codesign --verify` 与公证。
- 更新：旧版本 → 新版本，会话/设置保留；delta 校验。
- 卸载保留 `$DSH_HOME`；断网、磁盘满、权限错误。
- 任务运行中更新被拒绝或确认后中断。

打包测试的命令与产物约定见 [打包与更新](packaging-updates.md)。

## 平台矩阵

| 平台 | 必测 |
| --- | --- |
| Windows x64（首发） | 窗口、输入法、弹窗、原生 UI Automation、安装器、签名、Host 回收 |
| macOS arm64/x64（随后） | 窗口、输入法、剪贴板、签名/公证、更新 |
| Linux | 不首发；如支持再补原生 UI 与安装包 |

DSH 的会话与设置跨平台共享；测试不要写入用户真实 `$DSH_HOME`。

## 确定性与并发

- 测试不依赖 wall clock；用事件等待或 fake clock，不用固定延时猜时点。
- 并行测试各自拥有临时 `DSH_HOME`、端口与进程。
- 不在测试之间共享 profile 或 `node_modules`。
- 进程测试在 teardown 等待退出；泄漏进程视为失败。
- UI 测试不并行共享窗口；每个测试独立。

## CI 矩阵

| 阶段 | 命令 |
| --- | --- |
| Go | `go build ./...`、`go test ./...`、`go tool mygo vet .` |
| UI | 同上（`ui.NewTester`） |
| 协议 | `node --test resources/host/tests/*.test.mjs`、`go test ./internal/channel` |
| 真实 Host | 可选自托管 job：设置 `DSH_MYGO_DSH_DIR` 后跑真实组合组 |
| 打包 | 平台 runner 上构建、签名、安装 smoke（发布阶段） |

## 测试范围取舍

- 真实付费模型 E2E 需要 key，作为可选/自托管任务，不进默认流水线。
- macOS 真机验收与签名/公证在 macOS 发布门槛执行。
- 打包、签名与更新测试在平台 runner 上执行，作为发布门槛。
- 原生窗口的部分只读控件模式无法用 UI Automation 断言，相关选择/effort 由 Tester 与真实 Loader 覆盖。

## 不测试什么

- 不测试 Electron/Web 行为。
- 不测试 MyGo 自身控件；只测试我们的视图与状态。
- 不用快照测试像素；视觉验收在真实窗口完成。

## 相关文档

- [会话投影](session-projection.md)
- [通道协议](channel-protocol.md)
- [宿主监督](host-supervisor.md)
- [打包与更新](packaging-updates.md)
- [进度与路线](../roadmap.md)
