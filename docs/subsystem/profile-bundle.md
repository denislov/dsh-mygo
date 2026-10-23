# Profile 与 Bundle

## 职责

`@dsh-mygo/host` 是 dsh-mygo 在 DSH 侧的唯一边界：

- 把 dsh-mygo profile 组合成 `dsh-base` + 自有行。
- 提供 Host 启动入口（命令解析、生命周期、stdio 占用）。
- 注册 approval / user-question 的 answerer。
- 实现 channel server 与 session 投影所需的 Host 侧服务。
- 不包含 Web 传输、浏览器客户端或 Electron 适配。

## 组成

实现位于 `resources/host/`：`index.mjs` 提供 startup，`channel.mjs` 装配 DSH 公开服务，`server.mjs` 实现会话方法，`transport.mjs` 负责有界 stdio 帧。bundle 是无需 JS 构建步骤的本地 ESM，由 profile 的 `node_modules` 引入，不发布 npm；发布分发（Node/DSH runtime、私有入口）见 [打包与更新](packaging-updates.md)。

profile 由 `dsh-base` + `@dsh-mygo/host` 组成。channel 依赖 launcher 的 `appReady`，只在真实 Loader 启动成功后接管 stdio；握手再次等待 Loader 稳定，并检查 `profileContext.startedBundles`。能力与版本以 [通道协议](channel-protocol.md) 为准。

回合取消通过 `session.cancel` capability 启用；channel 的 `cancelTimeoutMs` 默认 5000，控制等待 Agent idle 的上限，不通过杀进程实现取消。

文本流式通过 `session.assistant-stream` capability 启用；自有 channel 监听公开 `agent/assistant-stream`，仅转发拥有的确切 agent，不修改 DSH core，也不移除持久提交中内嵌的 stream。

Host 历史通过 `session.history` capability 启用，使用公开 sessionPersistence 与 agents.resume，不扫描私有存储或修改 DSH core。根会话目录、冷恢复路由、分页边界和预算以 [通道协议](channel-protocol.md#host-历史能力) 为准；原生 Sessions 弹窗提供列表/新建/恢复。

会话管理分别通过 `session.rename` 与 `session.archive` 协商，使用 base 的公开 `sessionTitle` 与自有稳定行 `mygo-workspace` 挂载的 `@deepseek-ai/dsh-workspace` registry。registry 复用 base 的 storageDomain 与 sessionPersistence，不加载 Web 控制器或客户端。标题进入会话日志，归档集合由 registry 持久化；参数、单写者、活动检查和错误边界见 [通道协议](channel-protocol.md#会话管理)。

模型设置通过 model.selection capability 启用；channel 从同版本 @deepseek-ai/dsh-agent 引入公开 installModelSelection，在 create/resume 的 Agent setup 中安装作用域内选择引用。校验后向 Host 日志提交 model/selection，再由该公开适配器在下一次 prompt/request 组装时应用；不修改 Agent 内部字段、全局默认模型或 profile patch。目录及 secret 边界以 [设置与凭据](settings-credentials.md) 为准。

持久配置独立通过 settings.configuration 启用，复用 base 的公开 agentDefaultModel/settings/credentials 服务；channel 的 credentialRef 来自同版本 @deepseek-ai/dsh-credentials。只允许 agent-default-model 路由与官方 llm-deepseek 行绑定的凭据，持久化和锁归 Host；不改 DSH core，不自行写 profile/credentials。显式 flags 与 Host 默认值的优先级见 [设置与凭据](settings-credentials.md)。

审批通过 `approval` capability 启用，等待原生 UI 允许一次或拒绝；`maxPendingApprovals` 默认 32，范围 1–64，超限返回 unavailable。提问通过 `question` capability 启用，`maxPendingQuestions` 默认 32，范围 1–64；未协商或超限返回 NO_PROVIDER，不悬挂。

base 只提供提问服务；自有 patch 增加稳定行 `tool-ask-user`，绑定 `@deepseek-ai/dsh-tool-ask-user` 同版本。阻塞提问使用 `mode: legacy`；timed/continued 需要 Host 侧 provider seat，未提供时明确返回 NO_PROVIDER，不通过隐式改配置冒充支持。

## 包形态

```json
{
  "name": "@dsh-mygo/host",
  "version": "0.1.0",
  "type": "module",
  "main": "index.mjs",
  "exports": {
    ".": "./index.mjs",
    "./channel": "./channel.mjs",
    "./cordis.patch.yml": "./cordis.patch.yml",
    "./package.json": "./package.json"
  },
  "dsh": {
    "bundle": {
      "patch": "./cordis.patch.yml"
    }
  },
  "peerDependencies": {
    "@deepseek-ai/cordis": "4.0.5"
  }
}
```

- `main` 是 Cordis 插件入口。
- `dsh.bundle.patch` 让 `dsh plugin --profile mygo add` 识别这个包是 bundle。
- 本仓库不属于 DSH workspace，使用精确 peer 版本并由 profile 的 runtime resolution 提供；完整 manifest 见 `resources/host/package.json`。不依赖 DSH 内部实现路径。

## Bundle patch

```yaml
# 以 dsh-base 为底，只加 dsh-mygo 的宿主侧入口。
- insert:
    - id: mygo-app-startup
      name: '@dsh-mygo/host'
      config:
        profile: mygo

    - id: mygo-channel
      name: '@dsh-mygo/host/channel'
      inject: [mygoAppStartup, loader]
      config:
        maxFrameBytes: 16777216
        maxQueuedBytes: 33554432
        maxSessions: 8
```

行 id 是 patch 的稳定接口；重命名会破坏用户的 profile patch。新增行用新 id；废弃行先 disable，再在下个 major 移除。

### 需要的 base 行

| 行 | 用途 |
| --- | --- |
| `agent` | Agent 创建与 turn |
| `session` | 持久化与事件 |
| `user-questions` | `ask_user_question` 服务 |
| `approval` | 工具审批服务 |
| `permission`（`@deepseek-ai/dsh-permission-presets`） | 权限模式 |
| `settings` / `authorization` / `credentials-local` | 设置与凭据 |
| `llm` / `agent-default-model` | 模型路由 |

base 已经包含这些行；bundle 不应重复插入，除非需要覆盖配置。

### 可以禁用的行

| 行 | 条件 |
| --- | --- |
| `hmr` | 不需要模块热重载 |
| telemetry 行 | 不需要遥测 |
| `session-title-first-prompt-llm` | 不需要模型生成标题 |
| `api-gateway` / `api-remote-access` | 不实现 Remote 面（确认没有其他行依赖） |
| `plugin-manager` | 不需要 UI 内插件管理（CLI 仍可用） |

禁用前先确认依赖关系；`api-gateway` 被其他行注入时不要贸然禁用。

## 启动入口

### 开发入口：`dsh --profile mygo`

- 复用 CLI 的 profile 解析、`profileContext`、插件管理、`--patch`。
- 需要 startup provider 处理参数与 help，避免 `dsh --profile mygo` 打印帮助后退出。
- stdout 必须只属于 channel。

### 发布入口：私有 `mygo-host`

入口位于 `resources/runtime/mygo-host.mjs`：

- 直接 `import { runProfile } from '@deepseek-ai/dsh/profile-boot'`，用 `loadLayeredEnv('dsh')` 提供 `environment`。
- 接收 profile 名与 `--patch` 覆盖；其余参数交给 `runProfile` 的 `args`。
- channel、审批、提问与 ready 由 profile 的 `@dsh-mygo/host` bundle 在 `runProfile` 内安装；入口不写 stdout。
- 发布版使用该入口；开发期可以使用 `dsh --profile mygo` 或同一入口。

两种方案都必须在 `runProfile` 之后：

1. 确认 `ctx.fiber.state` 已稳定。
2. 安装 approval / question answerer。
3. 启动 channel server。
4. 通知 Go 侧 ready（或由 `initialize` 响应隐式完成）。

## Startup provider

参考 `packages/bundle/sdk-app/src/index.ts`：

```ts
export const name = 'mygo-app-startup'
export const inject = ['cmdlineArgs']

export const MYGO_APP_STARTUP_SERVICE = 'mygoAppStartup'

export function apply(ctx: Context, config: Config) {
  const program = new Command()
    .name(`dsh --profile ${config.profile}`)
    .description('dsh-mygo host')
    .action(() => {
      ctx.provide(MYGO_APP_STARTUP_SERVICE, { profile: config.profile })
    })
  // 解析 ctx.cmdlineArgs；help 时只打印并返回。
  // 成功时 provide 服务；channel 行 inject 它。
}
```

生命周期：

- `exitOnStdinEnd`：stdin EOF → 有界成功退出。
- `SIGINT` / `SIGTERM`：由 profile runner 的 shutdown 控制器处理。
- `shutdown` 协议请求：停止新工作、等待回合、flush、退出。

## Profile 初始化

### 开发初始化

应用启动前会自动创建缺失的 mygo profile（见下）。需要把本仓库 bundle 以 junction 链接进 profile 时，仍可手动运行：

```sh
node resources/host/setup-profile.mjs --dsh-home D:/Temp/dsh-mygo-dev
```

这会：

1. 在指定绝对 `DSH_HOME` 下创建新的 `profiles/mygo`；目标已存在时拒绝，不覆盖用户文件。
2. 将本仓库 bundle 链接到 profile 的 `node_modules/@dsh-mygo/host`；Windows 使用 junction，不修改 DSH checkout。
3. 写入 `dsh.profile.bundles: ["@deepseek-ai/dsh-base", "@dsh-mygo/host"]` 和空用户 patch；不运行包安装器。

### 发布首启

`host.EnsureProfile` 在启动 Host 前检查 `$DSH_HOME/profiles/mygo`：

1. profile 已存在时不做任何修改。
2. 不存在时在 staging 目录创建 profile：把 `resources/host` 复制到 `node_modules/@dsh-mygo/host`，写入 `dsh.profile.bundles: ["@deepseek-ai/dsh-base", "@dsh-mygo/host"]` 与空 `cordis.patch.yml`，再原子改名到目标目录。
3. 创建失败时窗口进入 `Host error`，不启动 Host。

该 profile 通过 DSH 的正式 CLI 和 Loader 启动，并复用安装侧公开 peer 包。发布时由安装器携带完整 bundle 与 runtime，不依赖开发机的仓库链接。

### 首启校验（发布构建）

发布构建在首启时校验：

- manifest 存在且 `dsh.profile.bundles` 包含 base 与 mygo bundle。
- bundle 版本与 UI 版本匹配（同一发布单元）。
- 兼容性检查失败时显示修复命令，不自动改写用户 profile。

开发初始化脚本只创建并链接 profile；发布构建才执行上述校验。

### 包管理器

- 第三方插件安装使用 profile 的 pnpm 与用户配置。
- 应用自带 pnpm/Node 时，只在包操作中注入自有路径；不改变 Host 继承的 `PATH`。
- 首启不运行 pnpm；内置 bundle 随应用分发。

## 兼容性

- 与 DSH 版本绑定：`dshVersion` 写入 channel 的 `initialize` 响应，UI 记录并展示。
- bundle 的 peer 依赖与 `dsh` 版本对齐；不依赖 `src/` 或未导出的内部模块。
- 使用公开服务：`ctx.agents`、`ctx.session*`、`ctx.userQuestions`、`ctx.approval`、`ctx.settings`、`ctx.credentials` 等。
- 可选服务用 `ctx.get(name)`，缺失时降级并报告，而不是崩溃。
- 行 id、配置字段、channel 方法一起做兼容性评审。

## 失败模式

| 失败 | 处理 |
| --- | --- |
| profile 缺失 | 提示初始化命令；不静默创建错误组合 |
| bundle 未安装/版本不符 | 提示 `dsh plugin ... add`；不继续启动 |
| base 行缺失 | 启动失败并指名缺失服务 |
| 端口/stdio 冲突 | 不适用（stdio）；检查 stdout 是否被日志污染 |
| 第三方插件启动失败 | 报告插件名与错误；提供禁用/救援路径 |

## 测试

- **composition 测试**：加载 bundle patch + base patch，断言行、顺序、配置。
- **real-composition**：通过 Loader 启动真实 profile，验证 `profileContext`、stdout 纯净、channel ready。
- **profile 初始化**：临时 `DSH_HOME`，执行安装/初始化，断言 manifest。
- **兼容性**：模拟 bundle 缺失、版本不符、base 行缺失。

## 相关文档

- [研究：DSH 组合](../research/dsh-composition.md)
- [通道协议](channel-protocol.md)
- [宿主监督](host-supervisor.md)
- [打包与更新](packaging-updates.md)
