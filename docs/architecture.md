# dsh-mygo 架构

## 1. 目标

dsh-mygo 是 DeepSeek Harness 的原生桌面客户端：

- 用 MyGo 的原生 UI 渲染窗口，不嵌入浏览器。
- 复用 DSH 的 agent runtime、工具、会话、设置、凭据与人机交互能力。
- 不加载 `@deepseek-ai/dsh-web-app`，不依赖 Electron、`dsh-app://`、`WebContentsView`、`<webview>` 或 session partition。
- 保持 UI 与 Host 的清晰边界：Go 进程拥有窗口与交互，Node 进程拥有 Agent 与持久化。
- 首发平台 macOS（arm64/x64）与 Windows（x64）；Linux 视后续需要。

## 2. 非目标

- 不追求与 Web/Desktop 版逐像素或逐功能对齐。
- 不实现内嵌第三方网页、浏览器侧栏、Platform 嵌入页。
- 不在 Go 进程内运行 Cordis 或 JS。
- 不把 UI 状态做成第二份会话真相；会话真相始终在 DSH session log。
- 不承诺 Electron 的 API 兼容或旧 Desktop 的更新迁移。

## 3. 系统上下文

```text
┌──────────────────────────────────────────────────────────────┐
│ MyGo 应用（Go 进程）                                          │
│                                                              │
│  ui.View / Router / List / Markdown / Editor / Diff          │
│  Window / Menu / Tray / Dialog / Notification / Updater      │
│  App 状态：会话摘要、transcript 投影、UI 选择、输入草稿      │
│  HostSupervisor：spawn / stdio / 生命周期 / 崩溃恢复         │
│  ChannelClient：JSON-RPC 请求、通知、取消、版本协商          │
└───────────────────────────────┬──────────────────────────────┘
                                │ stdio: newline-delimited JSON-RPC
                                │ stdout = 协议，stderr = 日志
┌───────────────────────────────▼──────────────────────────────┐
│ Node Host（dsh --profile mygo）                               │
│                                                              │
│  @deepseek-ai/dsh-base                                       │
│    llm / agent / session / tools / settings / credentials    │
│    user-questions / approval / permission / jobs / subagent  │
│  @dsh-mygo/host                                              │
│    startup provider / channel server                         │
│    approval answerer / question answerer                     │
│    session event 投影与历史读取                               │
│                                                              │
│  $DSH_HOME/profiles/mygo + 第三方插件                         │
└───────────────────────────────┬──────────────────────────────┘
                                │
                     $DSH_HOME/sessions, settings, credentials
```

## 4. 进程与职责

### 4.1 MyGo 进程

| 职责 | 说明 |
| --- | --- |
| 窗口与 UI | 原生 UI，主线程构建视图；所有界面状态在这里投影 |
| 用户输入 | 键盘、输入法、粘贴、拖放、附件、`@` 引用 |
| Host 监督 | 启动、就绪、停止、崩溃恢复、日志尾 |
| 通道客户端 | JSON-RPC 编解码、请求关联、取消、事件排序 |
| 本地配置 | 窗口位置、主题、快捷键、最近会话等设备级偏好 |
| 系统集成 | 菜单、托盘、通知、深链、单实例、文件对话框 |
| 更新 | MyGo updater；更新前后协调 Host 生命周期 |

### 4.2 Node Host

| 职责 | 说明 |
| --- | --- |
| Agent 生命周期 | 创建/恢复/关闭 agent，驱动 turn |
| 模型访问 | provider、model、effort、重试、流式输出 |
| 工具执行 | 文件、shell、web、subagent、jobs、MCP、skills |
| 持久化 | session log、projection cache、settings、credentials |
| 人机交互 | approval、user questions 的服务与 answerer |
| 协议服务 | channel server；stdout 只写协议帧 |

### 4.3 Profile

Profile 是组合根。当前设计：

```json
{
  "name": "dsh-mygo-profile",
  "private": true,
  "dsh": {
    "profile": {
      "bundles": [
        "@deepseek-ai/dsh-base",
        "@dsh-mygo/host"
      ]
    }
  }
}
```

细节见 [profile 与 bundle](subsystem/profile-bundle.md)。

## 5. 运行流程

### 5.1 启动

1. Go 进程启动，加载设备级设置与最近会话。
2. 解析 `$DSH_HOME`，确认 `profiles/mygo` 存在；首启则初始化。
3. 启动 Host 子进程（开发期 `dsh --profile mygo`；发布版可用私有 `mygo-host`）。
4. Host 完成 Cordis boot，channel server 就绪。
5. Go 发送 `initialize`，协商协议版本与能力。
6. Go 拉取 `session/list` 与必要的 `session/history`，渲染首页。
7. 用户选择会话或新建；`session/resume` / `session/create`。
8. 用户输入 → `session/prompt`；Host 回 `session/assistant-stream`、`session/event` 与 `session/status`。

P1 用 `dsh --profile sdk` 走这条流程的前半段：没有自定义 channel、没有 `session/assistant-stream`，只消费提交后的 `session/event`。第 2–8 步中的 `session/list`、`session/resume` 等自定义方法属于 P2。

### 5.2 一个回合

```text
UI 输入
  → channel session/prompt
  → Host 写入 user message（session log）
  → agent loop 调用模型与工具
  → 模型流：agent/assistant-stream → session/assistant-stream（start/chunk/end）
  → 提交事件：session/event（assistant/message、tool/call、tool/result…）
  → Go 合并流式片段与提交事件，投影为 transcript / 工具卡片 / diff / 状态
  → 需要审批时 Host 发 approval/request；Go 弹窗；用户回答
  → turn 结束：session/status = idle
```

模型可见的输入必须经过 Host 服务并写入 session log；UI 不得构造只存在于本地的模型上下文。逐字流式来自 process-local 的 `agent/assistant-stream`，它不进入 session log；最终以提交的 `assistant/message` 或 `assistant/attempt` 为准。

### 5.3 关闭

- 用户退出：Go 发送 `shutdown`；Host 停止新工作、等待当前回合收尾、flush 持久化、退出。
- Host 先退出：Go 显示恢复对话框，提供重启、查看日志、退出。
- Go 崩溃：Host 通过 stdin EOF 退出，不留孤儿进程。

## 6. 传输与协议

第一版：**stdio + newline-delimited JSON-RPC 2.0**。

- 一行一个 JSON 对象；UTF-8；不接受二进制帧。
- 请求：`{jsonrpc, id, method, params}`；响应：`{jsonrpc, id, result|error}`；通知：`{jsonrpc, method, params}`。
- 大附件用 base64 编码对象，并限制单帧与总大小。
- stdout 只写协议帧；日志、诊断、第三方输出全部走 stderr。
- `initialize` 协商 `protocolVersion` 与 capability 集；不认识的 capability 不启用。
- 每个 session 的持久事件按单调 `seq` 排序；UI 发现缺口时用 `session/history` 补齐。
- 逐字输出走 `session/assistant-stream`（start/chunk/end），不写 session log；提交事件到达后用它校准。

方法、通知、错误码、版本规则见 [通道协议](subsystem/channel-protocol.md)。

### 6.1 为什么是 stdio

- 生命周期与进程绑定：父进程退出，管道关闭，Host 收到 EOF。
- 不需要端口、认证、防火墙与端口冲突处理。
- 适合单窗口桌面应用。
- 未来需要多窗口/远程调试时，可以在同一协议上增加 loopback WebSocket transport；协议本身不绑定 stdio。

## 7. 会话与投影

### 7.1 真相源

- DSH session log 是唯一真相。
- Host 负责写入、恢复、压缩与投影缓存。
- Go 只保存“投影后的 UI 状态”，不把 UI 状态回写成会话历史。

### 7.2 投影

Go 的 `SessionProjection` 从 `session/assistant-stream` 与 `session/event` 构建：

- turn 边界与用户消息。
- assistant 文本与 reasoning：流式帧先渲染，提交的 `assistant/message` / `assistant/attempt` 到达后校准并替换。
- 工具调用：名称、参数、状态、结果、错误。
- 文件编辑 → diff。
- plan / todo / goal。
- subagent 关系与状态。
- usage / context / 压缩。
- 错误与取消。

打开旧会话时用 `session/history` 回放，再接入实时事件；避免“历史 + 实时”重复。

细节见 [会话投影](subsystem/session-projection.md)。

## 8. 人机交互

- **审批**：Host 的 `ctx.approval` 发起请求；`dsh-mygo-app` 注册 answerer；通过 `approval/request` 发给 Go；Go 弹窗；`approval/respond` 返回 allow/deny。
- **用户提问**：模型 `ask_user_question` → `ctx.userQuestions` → answerer → `question/request` → Go 表单 → `question/respond`。
- **权限模式**：沿用 base 的 `dsh-permission-presets` 与 `DSH_PERMISSION_MODE`；UI 只展示与切换，不自行放宽。
- **Secret**：凭据只写不读；日志、错误、崩溃报告不得包含 token、API key、cookie。

细节见 [交互](subsystem/interaction.md)。

## 9. 生命周期与错误

| 场景 | 行为 |
| --- | --- |
| Host 启动失败 | UI 显示错误摘要 + 重试/退出；stderr 尾部写入日志 |
| 协议帧非法 | 关闭连接；Host 退出；UI 报错，不尝试猜测 |
| 请求超时 | 返回结构化超时；UI 标记对应操作失败 |
| 会话忙 | `session/prompt` 返回忙或排队，按协议语义处理 |
| 用户取消 | `session/cancel`；只取消当前回合，不关闭会话 |
| Host 崩溃 | UI 保留当前投影，显示恢复对话框；提供重启 |
| Go 崩溃 | Host 因 stdin EOF 退出；下次启动恢复会话 |
| 更新中 | 先停止 Host 或等待回合结束；更新单元包含 UI + Host + profile 版本 |

细节见 [宿主监督](subsystem/host-supervisor.md)。

## 10. 安全与隐私

- Host 是本地可信进程，但用户安装的第三方插件与 Web 版一样以用户权限运行；不承诺额外沙箱。
- 协议边界是进程边界：所有入站 JSON 都要校验字段、大小与类型。
- 不在 stdout/stderr/日志/崩溃报告中写入 secret。
- 附件按类型与大小准入；不执行附件内容。
- 深链、文件拖放、剪贴板都视为不可信输入。
- 权限策略由 base 的 sandbox/approval 配置决定；UI 不能绕过。

## 11. 打包与更新

- MyGo 应用包含 Go 二进制与 `resources/`。
- `resources/` 携带 Node Host、DSH runtime、profile seed、第三方 runtime（按需）。
- 首启创建/校验 profile；离线可用；第三方插件安装走 profile 的 pnpm。
- UI、Host、profile 版本作为一个发布单元验证；不独立升级。
- MyGo updater 负责签名、delta、更新窗口；更新前协调 Host 收尾。

细节见 [打包与更新](subsystem/packaging-updates.md)。

## 12. 测试

| 层 | 内容 |
| --- | --- |
| Go 单元 | 投影、协议编解码、状态机、错误分类 |
| UI | `ui.NewTester` 点击/输入/快捷键/焦点；Markdown 与 diff 的渲染文本 |
| 协议 | 请求/通知/取消/版本/非法帧的 conformance fixtures |
| Host 集成 | 真实 `dsh --profile mygo`；真实 session log；approval/question 流程 |
| DSH 组合 | real-composition：通过 Loader 启动 profile，验证行、插件、stdout |
| E2E | 有 API key 时跑真实模型；无 key 时用 fake provider |
| 打包 | 安装包 smoke、签名校验、更新安装 |

细节见 [测试](subsystem/testing.md)。

## 13. 阶段计划

### 阶段 A：验证（SDK）

- profile：`sdk` 模板（`dsh-base` + `@deepseek-ai/dsh-sdk-app`）。
- Go：启动 Host、`initialize`、`session/prompt`、消费 `session.event`。
- UI：单窗口显示提交后的 assistant 文本与工具状态（路线 A 不验证逐字流式）。
- 成功标准：登录/模型配置后能完成一个真实回合；取消与退出可用。
- 不做：live 流式、审批、提问、历史、设置。

**当前进度**：P1 原生窗口和 SDK 闭环已完成；自定义 channel 阶段 1 和回合取消已完成，默认入口使用 `mygo` profile，支持版本握手、显式创建会话、prompt、提交事件、状态、Stop 与关闭。SDK 作为显式回归模式保留。当前能力边界见 [通道协议](subsystem/channel-protocol.md)；审批、提问、历史、设置和逐字流式仍在后续阶段。

### 阶段 B：自定义 profile

- 新建 `@dsh-mygo/host` bundle 与 `mygo` profile（最小闭环已完成）。
- 实现 startup provider、stdio channel、session create/resume/prompt/cancel。
- 转发 `agent/assistant-stream` 为 `session/assistant-stream`，实现逐字渲染。
- 注册 approval/question answerer。
- 成功标准：不依赖 SDK server 完成同一回合；流式、审批与提问可用。

### 阶段 C：产品化

- `session/history`、fork/rename/archive、模型/effort、设置、附件。
- 完整的原生 UI：会话列表、聊天、工具卡片、diff、设置、更新。
- 打包、签名、更新、CLI、深链。
- 成功标准：安装版可离线启动、升级、恢复；macOS/Windows 验收。

## 14. 关键决策

| 决策 | 选择 | 理由 |
| --- | --- | --- |
| UI 技术 | MyGo 原生 UI | 无 webview、启动快、内存小；避开 Electron 能力缺口 |
| Profile | `dsh-base` + 自有 bundle | 复用 agent runtime，去掉 Web/Electron 组合 |
| 验证协议 | SDK stdio JSON-RPC | 现成、提交事件完整、验证成本低 |
| 产品协议 | 自定义 channel | 审批/提问/历史/设置按 UI 需要设计 |
| 传输 | stdio | 生命周期简单、无端口/认证 |
| 会话真相 | DSH session log | 模型可见 ⟺ 已记录；UI 不造第二份真相 |
| 打包 | MyGo build + resources | 统一签名/更新；Host 作为资源分发 |

## 15. 文档路由

| 主题 | 文档 |
| --- | --- |
| 研究结论 | [research/](research/README.md) |
| Host 监督 | [subsystem/host-supervisor.md](subsystem/host-supervisor.md) |
| 通道协议 | [subsystem/channel-protocol.md](subsystem/channel-protocol.md) |
| Profile/bundle | [subsystem/profile-bundle.md](subsystem/profile-bundle.md) |
| 会话投影 | [subsystem/session-projection.md](subsystem/session-projection.md) |
| 审批/提问 | [subsystem/interaction.md](subsystem/interaction.md) |
| 设置/模型/凭据 | [subsystem/settings-credentials.md](subsystem/settings-credentials.md) |
| UI 壳层 | [subsystem/ui-shell.md](subsystem/ui-shell.md) |
| 打包/更新 | [subsystem/packaging-updates.md](subsystem/packaging-updates.md) |
| 测试 | [subsystem/testing.md](subsystem/testing.md) |
| Agent 路由 | [AGENTS.md](../AGENTS.md) |
