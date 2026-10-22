# 宿主监督（Host Supervisor）

## 职责

`HostSupervisor` 是 Go 进程唯一拥有 Node Host 生命周期的组件：

- 解析 Host 入口与运行时参数。
- 启动、停止 Host，持有 stdin/stdout/stderr。
- stdout 交给 `ChannelClient`，stderr 进入有界环形缓冲。
- 把启动失败、协议错误、Host 意外退出转成 UI 可展示的失败状态。

它不解释会话事件，也不持有 UI 状态；这些属于 [通道协议](channel-protocol.md) 与 [会话投影](session-projection.md)。

## 实现

`internal/host` 提供：

- `ResolveLaunch`：按 `-host-command` 覆盖 → `-dsh-dir` source checkout → PATH 上的 `dsh` 解析入口；`-dsh-home` 以大小写不敏感的方式替换 `DSH_HOME`。
- `Start`：建立 stdin/stdout/stderr 管道，启动 `cmd`，stderr 保留最后 64 KiB；调用方把 `Stdin`/`Stdout` 接到 `ChannelClient`。
- `Stop`：关闭 stdin 等待优雅退出，超时后杀进程并等待回收；幂等。POSIX 杀进程组，Windows 杀进程句柄。
- `App` 在初始化失败、Host 意外退出、协议读循环失败时进入 `Host error`；自定义 channel 不把原始 stderr 放入错误横幅，显式 SDK 模式保留有界 stderr 详情。

## 入口解析

| 顺序 | 条件 | 命令 |
| --- | --- | --- |
| 1 | `-host-command` | 使用给定命令与 `-host-arg`，cwd 为启动 cwd |
| 2 | `-dsh-dir` | `node --import tsx/esm <dsh-dir>/apps/cli/src/bin.ts --profile <profile>`，cwd 为 DSH checkout |
| 3 | 默认 | PATH 上的 `dsh --profile <profile>`，cwd 为启动 cwd |

`-dsh-dir` 要求存在 `apps/cli/src/bin.ts` 且显式给出 profile；缺入口或 profile 时启动前失败。发布版使用 `resources/` 下的私有 `mygo-host` 入口；当前构建只提供上述三种开发入口，见 [打包与更新](packaging-updates.md) 与 [路线图](../roadmap.md)。

### 环境

Host 继承启动环境，`-dsh-home` 只覆盖 `DSH_HOME`：

| 变量 | 作用 |
| --- | --- |
| `DSH_HOME` | profile、会话、设置与凭据的根目录 |
| `DSH_PERMISSION_MODE` | 权限模式；由环境/profile 提供，UI 不修改 |
| `DEEPSEEK_API_KEY` / `DEEPSEEK_BASE_URL` | 模型凭据；也可由 DSH 的 `.env` 层解析 |
| `LANG` / `LC_ALL` | Host 侧 locale |
| `PATH` | 让 Host 找到 shell、git、pnpm 等工具 |

规则：

- 不把 `DSH_HOME` 指向 MyGo 的 `PathUserData`；DSH 数据目录由用户配置或默认 `~/.dsh` 决定。
- 不把 API key 写进命令行参数、日志或崩溃报告。
- 第三方插件生命周期脚本继承用户环境；这是 DSH 的既有模型，不额外收紧。

### 工作目录

Host 进程的 cwd 与 `session/create` 的 `cwd` 来自 App 启动参数（`-cwd`，默认当前目录）。会话 header 记录该目录；冷恢复继续使用持久 header.cwd，必须仍是已有绝对目录。

## stdio

```text
Go ── stdin ──▶ Host      (JSON-RPC 请求/响应/通知)
Go ◀─ stdout ── Host      (JSON-RPC 响应/通知)
Go ◀─ stderr ── Host      (日志、诊断、插件输出)
```

- stdout 只允许 JSON-RPC 帧；任何非协议输出都是缺陷。
- stderr 进入 64 KiB 环形缓冲，供错误横幅与调试使用；不落盘。
- 帧有最大长度；超长、非法 JSON 或未知结构关闭连接并报错。
- 编码 UTF-8；一行一个 JSON 对象；拒绝二进制帧。

## 生命周期

```text
Idle → Starting → Initializing → Ready → Stopping → Stopped
                      │             │
                      ├─ Fatal ─────┘
                      └─ Timeout ───┘
```

### 启动

1. `spawn` 子进程并建立管道。
2. 发送 `initialize` 并等待响应；App 侧上限 60 秒。
3. 校验 `protocolVersion`、UI/Host release 与 DSH 版本；不兼容时停止并报告。
4. 发送 `session/create` 或 `session/resume`，拿到 `nextSeq` 后进入 Ready。

启动期间的 UI 请求由 App 排队或拒绝；不会静默丢弃。

### 停止

1. 停止接受新请求。
2. 发送 `shutdown` 请求；App 等待上限 5 秒。
3. 关闭 stdin（EOF 是 Host 的兜底退出信号）。
4. `Supervisor.Stop` 在上下文到期后杀进程并等待回收。
5. 退出时 Host 清理拥有的会话、待处理审批/提问与子代理句柄。

### 失败与恢复

- Host 退出码非 0、协议解析失败、启动超时都进入 `Host error`。
- App 保留当前 transcript，显示错误横幅与有限 stderr 详情；不自动重启。
- 实时 `seq` 缺口或非法事件同样停止 Host，UI 提示重启应用；自动 resync 不在协议内。
- 应用退出时 `App.Shutdown` 关闭 channel、停止 Host，Crash 场景由 stdin EOF 回收。

## 日志与诊断

| 内容 | 位置 |
| --- | --- |
| Host stderr 尾部 | 内存环形缓冲（64 KiB） |
| 协议错误 | UI 错误横幅 + 有界详情 |

stderr 尾部只保留最近一段；文件日志与崩溃报告随打包阶段接入，细则见 [打包与更新](packaging-updates.md)。

## 失败模式

| 失败 | 处理 |
| --- | --- |
| 找不到 Host 入口 | 启动前失败；显示安装/路径错误 |
| profile 缺失/损坏 | Host 启动失败；提示初始化或修复命令 |
| 版本不兼容 | 显示双方版本与修复建议；不继续 |
| stdout 出现非协议文本 | 视为致命协议错误；关闭连接 |
| 子进程无法杀死 | 记录 PID 与平台信息；提示重启系统 |
| 磁盘满/权限错误 | 保留 UI；显示可操作错误 |

## 测试

- `internal/host`：入口解析、环境覆盖、启动/停止、stderr 尾、不可响应进程的升级杀进程。
- `testdata/fake-mygo-host.mjs` 驱动 App 全链路；helper process 覆盖退出与超时。
- 真实 Loader：`TestAgainstMygoRealComposition`、`TestMygoRealProtocolLifecycle` 验证启动、stdin EOF、非法帧退出与退出码。
- 真实窗口 smoke：`go run . -host-command node -host-arg testdata/fake-mygo-host.mjs`。

## 相关文档

- [通道协议](channel-protocol.md)
- [profile 与 bundle](profile-bundle.md)
- [打包与更新](packaging-updates.md)
- [进度与路线](../roadmap.md)
