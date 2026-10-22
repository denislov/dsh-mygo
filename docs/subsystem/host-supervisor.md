# 宿主监督（Host Supervisor）

## 职责

`HostSupervisor` 是 Go 进程唯一拥有 Node Host 生命周期的组件：

- 解析 Host 入口与运行时路径。
- 启动、初始化、健康检查、停止、重启 Host。
- 拥有 stdin/stdout 管道；stdout 交给 `ChannelClient`，stderr 进入日志环形缓冲。
- 把 Host 退出、协议错误、启动超时转成 UI 可展示的恢复状态。

它不解释会话事件，也不持有 UI 状态；这些属于 [通道协议](channel-protocol.md) 与 [会话投影](session-projection.md)。

## 实现要点

`internal/host` 的契约：

- `ResolveLaunch`：入口按 `-host-command` 覆盖、`-dsh-dir` source checkout、PATH 上的 `dsh` 依次解析；`-dsh-home` 以大小写不敏感的方式替换 `DSH_HOME`。发布版优先 `resources/` 下的私有入口。
- `Start`：建立 stdin/stdout/stderr 管道，stderr 保留最后 64 KiB；调用方通过 `Stdin` / `Stdout` 接 `ChannelClient`。
- `Stop`：关闭 stdin，等待优雅退出；超时后杀进程。POSIX 杀进程组，Windows 用 Job Object 回收进程树。
- `App` 在初始化失败、Host 意外退出、协议读循环失败时进入 `Host error`；自定义 channel 不把原始 stderr 放入错误横幅。显式 SDK 回归模式保留有界 stderr 详情。

Host 入口在启动前完成版本校验、`mygo` profile 选择与会话创建；SDK 模式由 `-sdk-protocol -profile sdk` 显式选择。

## 入口选择

| 模式 | 命令 | 用途 |
| --- | --- | --- |
| 开发 | `dsh --profile mygo` | 复用 CLI 的 profile 解析、`profileContext`、插件管理、`--patch` |
| 发布 | 私有 `mygo-host`（调用 `runProfile`） | 控制参数、stdout、启动顺序；不依赖 PATH 上的 `dsh` |

两种模式必须启动同一个 profile 与 bundle。发布版不得回退到另一个组合。

### 入口解析顺序

1. `resources/host/` 下的私有 `mygo-host` 入口（发布版）。
2. `resources/runtime/` 下的 `dsh` CLI（开发/兼容模式）。
3. `PATH` 上的 `dsh`（仅开发期，且必须校验版本）。

开发期还可以用 `-host-command` 覆盖入口，或用 `-dsh-dir` 直接运行 DSH source checkout。找不到入口时，UI 显示可操作的错误，而不是静默降级。

### 环境

Host 继承启动环境，并显式设置：

| 变量 | 作用 |
| --- | --- |
| `DSH_HOME` | profile、会话、设置的根目录 |
| `DSH_PERMISSION_MODE` | `workspace-write` / `danger-full-access` 等；由 UI 的权限选择写入 |
| `DEEPSEEK_API_KEY` / `DEEPSEEK_BASE_URL` | 模型凭据；也可由 DSH 的 `.env` 层解析 |
| `LANG` / `LC_ALL` | Host 侧 locale；UI 自己也有 locale |
| `PATH` | 让 Host 能找到 shell、git、pnpm 等工具 |

规则：

- 不把 `DSH_HOME` 指向 MyGo 的 `PathUserData`。DSH 数据目录由用户配置或默认 `~/.dsh` 决定，UI 只读取/展示。
- 不把 API key 写进命令行参数、日志或崩溃报告。
- 第三方插件的生命周期脚本继承用户环境；这是 DSH 的既有模型，不额外收紧。

### 工作目录

Host 进程的 cwd 使用稳定的应用目录，而不是用户当前打开的 workspace。每个会话的 cwd 通过 `session/create` / `session/working-directory/set` 设置，并由 DSH 记录到 session header。这样重启 Host 不会改变已有会话的目录语义。

## stdio

```text
Go ── stdin ──▶ Host      (JSON-RPC 请求/响应/通知)
Go ◀─ stdout ── Host      (JSON-RPC 响应/通知)
Go ◀─ stderr ── Host      (日志、诊断、插件输出)
```

- stdout 只允许 JSON-RPC 帧。任何非协议输出都是缺陷。
- stderr 进入有界环形缓冲（例如最后 64 KiB），用于崩溃对话框与技术详情。
- 协议帧有最大长度；超长帧关闭连接并报告错误。
- 编码 UTF-8；一行一个 JSON 对象；拒绝二进制帧。

## 生命周期

```text
Idle → Starting → Initializing → Ready → Stopping → Stopped
                      │             │
                      ├─ Fatal ─────┘
                      └─ Timeout ───┘
```

### 启动

1. `spawn` 子进程，建立 stdin/stdout/stderr 管道。
2. 等待 `initialize` 响应；设置启动超时（例如 30 秒）。
3. 校验 `protocolVersion` 与 required capabilities；不兼容则停止并报告。
4. 进入 `Ready`，开始接受 UI 请求。

启动期间收到的 UI 请求排队或直接拒绝，取决于请求类型；不要静默丢弃。

### 停止

1. 停止接受新请求。
2. 发送 `shutdown` 请求。
3. 关闭 stdin（EOF 是 Host 的兜底退出信号）。
4. 等待优雅退出（例如 10 秒）。
5. 超时则 `SIGTERM`，再超时则 `SIGKILL`。
6. 在 POSIX 上杀死整个进程组；Windows 使用 Job Object，确保插件子进程一并回收。

### 崩溃与重启

- Host 退出码非 0、协议解析失败、启动超时都进入 `Fatal`。
- UI 保留当前会话投影，显示恢复对话框：
  - 重启 Host。
  - 查看日志/技术详情。
  - 退出。
- 重启使用有界退避（例如 1s、2s、4s），避免崩溃循环。
- 更新安装期间不自动重启。

## 单实例与深链

- 启动时调用 `mygo.App.RequestSingleInstanceLock()`；第二个实例把参数交给第一个实例后退出。
- `OnSecondInstance` 把深链/文件参数转发给已运行实例。
- `OnOpenURL` 处理 `dsh-mygo://` 深链；只接受已知的 host/path，拒绝任意命令执行。
- macOS 的 `open-file` 与 Windows 的命令行参数走同一条归一化路径。

## 日志与诊断

| 内容 | 位置 |
| --- | --- |
| Go 应用日志 | `mygo.App.Path(mygo.PathLogs)` |
| Host stderr 环形缓冲 | 内存；崩溃时写入日志文件 |
| 协议错误 | 日志 + UI 技术详情 |
| 崩溃报告 | `PathLogs/crash-<UTC>-<source>.log` |

崩溃报告包含：版本、平台、Host 退出码/信号、stderr 尾部、当前会话 id、最后一条协议错误。**不包含** API key、token、cookie、附件内容。

## 失败模式

| 失败 | 处理 |
| --- | --- |
| 找不到 Host 入口 | 启动前失败；显示安装/路径错误 |
| profile 缺失/损坏 | 提示初始化或修复；不静默创建错误组合 |
| 版本不兼容 | 显示双方版本与修复建议；不继续 |
| stdout 出现非协议文本 | 视为致命协议错误；关闭连接 |
| 子进程无法杀死 | 记录 PID 与平台信息；提示重启系统 |
| 磁盘满/权限错误 | 保留 UI；显示可操作错误 |

## 测试

- 用 fake Host 进程验证启动/超时/崩溃/停止状态机。
- 用真实 `dsh --profile mygo` 验证 initialize 与 shutdown。
- Windows 上验证 Job Object 回收；POSIX 上验证进程组回收。
- 验证 stdout 纯净性：任何日志写入 stdout 都让测试失败。
- 验证单实例与深链转发。

## 相关文档

- [通道协议](channel-protocol.md)
- [profile 与 bundle](profile-bundle.md)
- [打包与更新](packaging-updates.md)
- [架构](../architecture.md)
