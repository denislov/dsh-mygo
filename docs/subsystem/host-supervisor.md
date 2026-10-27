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

- `ResolveLaunch`：入口按 `-host-command` 覆盖、`-dsh-dir` source checkout、选中 runtime（managed/external/legacy/direct）依次解析；`-dsh-home` 以大小写不敏感的方式替换 `DSH_HOME`。设备级选择由 [运行时管理](runtime-manager.md) 持久化，Host 不自行寻找 `dsh`。
- `Start`：建立 stdin/stdout/stderr 管道，stderr 保留最后 64 KiB；调用方通过 `Stdin` / `Stdout` 接 `ChannelClient`。
- `Stop`：关闭 stdin，等待优雅退出；超时后杀进程。POSIX 杀进程组，Windows 用 Job Object 回收进程树。
- Windows Job Object：`Start` 在 spawn 后立即把 Host 加入 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` 的 Job；Host 启动的子进程继承 Job。Host 退出、`Stop` 完成或应用崩溃/被强杀时关闭 Job 句柄，内核回收 Host 与插件子进程。Job 创建或分配失败时启动失败，不运行无 Job 的 Host。
- `App` 为每次启动建立不可复用的 Host 实例，持有 client、supervisor、generation 与关闭状态。Restart 与 Runtime 切换先 detach 当前实例，再异步停止捕获的实例；故障路径也在派发后台清理前捕获实例。通知、读循环与 watcher 在发布时按实例身份校验。旧实例的迟到通知、失败与重复/慢 Stop 不关闭或覆盖新 Host；最后一个实例在清理期间保留，重复 Stop 等待同一次清理完成，重启或切换在其完成后才启动新 Host。清理完成后只保留只读 PID/stderr 诊断，旧代清理不得替换新代诊断对象。
- `App` 在初始化失败、Host 意外退出、协议读循环失败时进入 `Host error`；自定义 channel 不把原始 stderr 放入错误横幅。显式 SDK 回归模式保留有界 stderr 详情。

Host 入口在启动前完成版本校验、`mygo` profile 选择与会话创建；SDK 模式由 `-sdk-protocol -profile sdk` 显式选择。

## 入口选择

| 模式 | 命令 | 用途 |
| --- | --- | --- |
| managed runtime | `<runtime>/node/node(.exe) <runtime>/mygo-host.mjs <profile>` | 自带 Node、DSH、私有入口与配套 Host bundle；版本独立于应用 |
| existing DSH | `dsh --profile mygo` 或 `cmd.exe /d /s /c call <dsh.cmd> --profile mygo` | 复用用户已安装的精确版本 DSH + 应用自带 Host bundle |
| 开发 | `-host-command` / `-dsh-dir` / `-runtime-dir` | 显式命令、source checkout、已解压 runtime；不写入设备级选择 |

所有模式必须启动同一个 profile 组合，但允许不同的 runtime 版本；应用与托管 runtime 通过 `protocolVersion`、Host bundle 版本与 app 兼容范围配对。

### 入口解析顺序

1. `-host-command` 或 `-dsh-dir`（开发/测试显式覆盖）。
2. `-runtime-dir` 指定的已解压 runtime（开发/smoke，不写状态）。
3. runtime store 中选中的 managed/external/legacy 条目。
4. 无有效选择 → 运行时引导；不启动 Host。

managed runtime 缺少 Node、入口、manifest 或与本 app/协议/DSH 不兼容时返回错误，不静默回退到 PATH。external 条目的命令或 Node 丢失时按 PATH 重新探测；只有 DSH 版本一致才修复条目。UI 显示可操作的错误与 Change runtime。

### 首启 profile

启动前确保 `$DSH_HOME/profiles/mygo` 存在。不存在时写入 profile manifest、空 `cordis.patch.yml` 与 Host bundle：

- managed/legacy/direct 使用该 runtime 自己的 `host/`；
- external 使用应用安装目录下的 `host/`。

已有 profile 的 manifest、patch、设置和凭据保持原样；应用自有 Host 包与当前 bundle 内容指纹不同时（不要求版本号不同），先将旧包保留到同一命名空间下的 `.host-backup-*`，再以暂存副本替换，替换失败回滚。内容相同则不做 I/O 替换；版本号与 `engines.dsh` 仍用于身份和兼容性校验。同步前校验包身份、入口和解析后的目录归属；兼容的开发链接在内容一致时保持，内容不一致的外部链接不自动替换。创建或同步失败时窗口进入 `Host error`，不启动 Host。

### 环境

Host 继承启动环境，并显式设置：

| 变量 | 作用 |
| --- | --- |
| `DSH_HOME` | profile、会话、设置的根目录 |
| `DSH_PERMISSION_MODE` | 部署提供新会话权限初值；UI 的会话权限选择经 Host 服务写入日志，不重写环境变量 |
| `DEEPSEEK_API_KEY` / `DEEPSEEK_BASE_URL` | 模型凭据；也可由 DSH 的 `.env` 层解析 |
| `LANG` / `LC_ALL` | Host 侧 locale；UI 自己也有 locale |
| `PATH` | 让 Host 能找到 shell、git、pnpm 等工具；managed/external runtime 把选中 Node 的目录放在最前 |

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
- Host 入口与 `@dsh-mygo/host` 在加载插件前把 console 的 stdout 方法（`log/info/debug`、`dir/table`、`count/group/time` 等）改道 stderr；`warn/error/trace/assert` 保持原生 stderr。DSH 插件（如账号模型目录）的诊断不得进入协议。该改道不是协议容错：stdout 出现非 JSON 行仍关闭连接并报告致命错误。
- stderr 进入有界环形缓冲（例如最后 64 KiB），用于崩溃对话框与技术详情。
- 协议帧有最大长度；超长帧关闭连接并报告错误。
- 编码 UTF-8；一行一个 JSON 对象；拒绝二进制帧。

## 生命周期

```text
NoRuntime → AwaitingRuntime → Starting → Initializing → Ready → Stopping → Stopped
                                   │             │
                                   ├─ Fatal ─────┘
                                   └─ Timeout ───┘

Ready/Failed → SwitchRequested → Stopping → Starting(candidate) → Initializing
                    │                                              │
                    └──────────── rollback + old host ◀───────────┘
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
6. 在 POSIX 上杀死整个进程组；Windows 关闭 Job 句柄，确保 Host 与插件子进程一并回收。

### 崩溃与重启

- Host 退出码非 0、协议解析失败、启动超时都进入 `Fatal`。
- UI 保留当前会话投影，显示不可 Escape 关闭的恢复对话框：
  - Restart host。
  - Change runtime…（有 runtime store 时）。
  - View logs。
  - Quit。
- 重启由用户触发：停止旧进程，启动新 Host，完成 `initialize`，再用 `session/resume` + `session/history` 回放同一会话；恢复失败保留旧投影并重新显示对话框。
- 当前会话已知为归档时，用户重启 Host 后创建新会话而不 resume 归档会话；原会话草稿按 id 保留，用户取消归档并打开后恢复，不隐式改动归档集合。
- 重启不自动重试，也不自动重连；自动退避与崩溃循环保护未实现。
- 更新安装期间不自动重启。

## 单实例与深链

- 启动时调用 `mygo.App.RequestSingleInstanceLock()`；第二个实例把参数交给第一个实例后退出。
- `OnSecondInstance` 把深链/文件参数转发给已运行实例。
- `OnOpenURL` 处理 `dsh-mygo://` 深链；只接受已知的 host/path，拒绝任意命令执行。
- macOS 的 `open-file` 与 Windows 的命令行参数走同一条归一化路径。

## 日志与诊断

| 内容 | 位置 |
| --- | --- |
| Go 应用日志 | `PathLogs/dsh-mygo.log`（追加） |
| Host stderr 环形缓冲 | 内存；失败时写入崩溃报告 |
| 协议错误 | 日志 + UI 技术详情 |
| 崩溃报告 | `PathLogs/crash-<UTC>-<source>.log` |

崩溃报告包含：版本、平台、Host 进程 id、stderr 尾部、当前会话 id、失败摘要与详情。写入前把 API key、Bearer token、password/secret 形状的值替换为 `[REDACTED]`；不写环境变量、cookie 或附件内容。日志文件、写锁、报告命名与脱敏策略由诊断组件持有，App 只提供已定界的摘要、详情和目标 Host 诊断；自定义模式不把 stderr 原文放进错误横幅，SDK 回归模式保留有界诊断。

## 失败模式

| 失败 | 处理 |
| --- | --- |
| 没有选中 runtime | 进入运行时引导；不启动 Host |
| runtime 文件/命令缺失 | external 先尝试 PATH 修复；失败进入引导或恢复中的 Change runtime |
| 找不到 Host 入口 | 启动前失败；显示安装/路径错误 |
| profile 缺失/损坏 | 提示初始化或修复；不静默创建错误组合 |
| 版本不兼容 | 显示双方版本与修复建议；不继续 |
| 切换候选初始化失败 | 恢复旧 profile 与旧 Host；候选不成为选中项 |
| stdout 出现非协议文本 | 视为致命协议错误；关闭连接 |
| 子进程无法杀死 | 记录 PID 与平台信息；提示重启系统 |
| Job Object 创建/分配失败 | 启动失败；不静默运行无 Job 的 Host |
| 磁盘满/权限错误 | 保留 UI；显示可操作错误 |

## 测试

- 用 fake Host 进程验证启动/超时/崩溃/停止状态机。
- 用真实 Host 验证 kill → Restart → `session/resume` + history 回放 → 继续发送。
- 用真实 `dsh --profile mygo` 验证 initialize 与 shutdown。
- 用已解压 runtime（`DSH_MYGO_RUNTIME_DIR` / `-runtime-dir`）验证 managed 入口、profile seed、会话与切换。
- Windows 上用 helper 进程验证 Job Object 回收：Stop 杀 Host 后子进程消失；Host 自行退出后子进程也消失（`TestStopKillsProcessTree`、`TestHostExitKillsProcessTree`）。POSIX 上验证进程组回收。
- 验证 stdout 纯净性：任何日志写入 stdout 都让测试失败。
- 验证单实例与深链转发。

## 相关文档

- [通道协议](channel-protocol.md)
- [运行时管理](runtime-manager.md)
- [profile 与 bundle](profile-bundle.md)
- [打包与更新](packaging-updates.md)
- [架构](../architecture.md)
