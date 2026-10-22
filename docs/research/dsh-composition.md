# DSH 组合模型研究

## 结论

DSH 的 surface 由 profile 决定，而不是由某个固定入口决定。一个自定义 profile 可以只加载 `@deepseek-ai/dsh-base` 和一个自有 bundle，不加载 `@deepseek-ai/dsh-web-app`。因此 dsh-mygo 可以复用完整的 agent runtime，同时避开 Web/Electron 专属能力。

## Profile 的组成

一个 profile 是 `$DSH_HOME/profiles/<name>` 下的目录：

| 文件 | 作用 |
| --- | --- |
| `package.json` | 声明第三方插件依赖，以及 `dsh.profile.bundles` 有序列表 |
| `cordis.patch.yml` | 用户自己的 patch 层 |
| `node_modules/` | pnpm 安装的 profile 级插件 |

组合顺序（来自 `packages/boot/app-boot/src/profile.ts`）：

1. 按 `dsh.profile.bundles` 顺序应用每个 bundle 的 patch 列表。
2. 应用 profile 自己的 `cordis.patch.yml`。
3. 应用启动器的 overlay（`--patch` 文件、命令行派生 patch）。

每个 patch 列表是一组 insert / override / disable 行，按行 id 覆盖；后写入者胜出。`dsh.profile.bundles` 的顺序因此是组合顺序，不是加载顺序。

### 初始化与启动

- `initProfile(dir, bundles)` 创建缺失的 manifest、空的用户 patch 和 pnpm workspace 文件，不覆盖已有内容。
- `PROFILE_TEMPLATES` 是 shipped profile 的默认 bundle 列表。当前包含 `acp`、`web`、`headless`、`sdk`、`sdk-minimal`。
- 没有模板的 profile 名使用 `DEFAULT_PROFILE_BUNDLES = ['@deepseek-ai/dsh-base']`。
- `dsh plugin --profile <name> add <package>` 会在 profile 中安装插件；如果包声明了 `dsh.bundle.patch`，它还会加入 `dsh.profile.bundles`，普通插件只作为依赖安装。
- `runProfile()` 是 CLI 与 `apps/desktop-host` 共用的启动实现：它负责组合 patch、创建 Cordis app、提供 `profileContext`、安装运行时的模块解析，并返回 `ctx` 与 shutdown 控制器。

### `profileContext`

`runProfile()` 在 boot 前把 `profileContext` 提供到根 context：

```ts
interface ProfileContext {
  name: string
  dir: string
  patchPath: string
  installAnchor: string
  cwd: string
  home: string
  startedBundles: readonly string[]
  overlays: readonly PatchOptions[]
  telemetryDisabledEnv: string | undefined
  packageManager?: ProfilePnpmInvocation
}
```

它的 `name` 是 surface 条件判断的入口。Web/Desktop 用 `ctx.get('profileContext')?.name === 'desktop'` 控制桌面专属行；dsh-mygo 可以用同样的方式控制自己的行，但不应把 profile 名当作唯一能力判断，能力开关优先用显式配置。

## Bundle 的形态

一个 bundle 是一个普通 npm 包，manifest 声明：

```json
{
  "dsh": {
    "bundle": {
      "patch": "./cordis.patch.yml"
    }
  }
}
```

`patch` 可以是一个文件，也可以是有序文件列表。bundle 的运行时入口不是必需的；`@deepseek-ai/dsh-web-app` 同时提供 `lib/index.js` 作为 glue plugin，但 `@deepseek-ai/dsh-base` 只有 patch。

bundle patch 的典型内容：

```yaml
- insert:
    - id: mygo-host
      name: '@your-scope/dsh-mygo-app'
      config:
        transport: stdio
```

也可以覆盖 base 行：

```yaml
- id: system-prompt
  config:
    personaPrefix: >-
      You are a coding agent powered by the {{model}} model.
```

或者禁用不需要的行：

```yaml
- id: hmr
  disabled: true
```

## `dsh-base` 提供什么

`packages/bundle/base/cordis.patch.yml` 是共享核心，包含：

| 能力 | 代表行 |
| --- | --- |
| 模型访问 | `llm`、`deepseek-llm-api-extensions`、`llm-retry` |
| Agent 循环 | `agent`、`agent-default-model` |
| Session | `session`、`session-log-deepseek`、`session-title` |
| 工具 | bash、文件读写编辑、web search/fetch、subagent、jobs、goal、plan、todo 等 |
| 设置与凭据 | `settings`、`config-editor`、`authorization`、`credentials-local` |
| 人机交互 | `user-questions`、`approval`、`permission-presets` |
| 持久化与压缩 | session persistence、`compaction-basic` |
| 技能与 MCP | skills、`mcp-resources`、`mcp-client` |
| API 基础 | `typert-registry`、`typert-loader`、`api-gateway`、`api-remote-access` |
| 运维 | `plugin-manager`、`hmr`、telemetry、session-log contributors |

两个关键默认值：

- `approval` 的默认策略是 `(DSH_PERMISSION_MODE ?? 'workspace-write') === 'danger-full-access' ? 'never' : 'ask'`。
- `user-questions` 是模型 `ask_user_question` 工具的落点。没有 UI answerer 时，提问会停在服务上。

`dsh-base` 不是最小树。它带 plugin-manager、HMR、settings、telemetry、session title 等。如果目标体积或启动时间，可以在 patch 中禁用不需要的行；`@deepseek-ai/dsh-sdk-minimal` 是一个独立最小树的参考，但它不继承 base。

## `dsh-web-app` 额外提供什么

`packages/bundle/web-app/cordis.patch.yml` 在 base 之上插入：

- `@deepseek-ai/dsh-host-webserver`：HTTP/HTTPS 监听。
- `@deepseek-ai/dsh-host-frontend-static`：静态前端与 fallback。
- `@deepseek-ai/dsh-client-connection`：浏览器侧的连接、认证、RPC、WebSocket。
- `@deepseek-ai/dsh-client-modules`：客户端插件模块加载。
- 大量 `@deepseek-ai/dsh-client-ui-*`：聊天、侧栏、设置、插件管理、浏览器、账户等。
- `@deepseek-ai/dsh-api-*` 控制器：session、job、terminal、workspace、settings、account。
- 以 `profileContext.name === 'desktop'` 为条件的 Desktop 行：产品遥测、浏览器侧栏、Platform 视图等。

这些行正是 dsh-mygo 不需要的：它们服务于浏览器 UI、Electron 的 `dsh-app://` origin、`WebContentsView`、`<webview>`、session partition 和请求拦截。dsh-mygo 的原生 UI 不消费这些能力。

## 自定义 profile 的最小配方

### 1. bundle 包

```json
{
  "name": "@your-scope/dsh-mygo-app",
  "version": "0.1.0",
  "type": "module",
  "main": "lib/index.js",
  "dsh": {
    "bundle": {
      "patch": "./cordis.patch.yml"
    }
  },
  "peerDependencies": {
    "@deepseek-ai/cordis": "workspace:~"
  }
}
```

### 2. bundle patch

```yaml
# 以 dsh-base 为底，只加 dsh-mygo 的宿主侧入口。
- insert:
    - id: mygo-startup
      name: '@your-scope/dsh-mygo-app'
      config:
        profile: mygo
```

### 3. profile manifest

```json
{
  "name": "dsh-mygo-profile",
  "private": true,
  "dsh": {
    "profile": {
      "bundles": [
        "@deepseek-ai/dsh-base",
        "@your-scope/dsh-mygo-app"
      ]
    }
  }
}
```

初始化命令：

```sh
dsh plugin --profile mygo add @your-scope/dsh-mygo-app
```

### 4. startup provider

如果通过 `dsh --profile mygo` 启动，需要处理 CLI 的参数与生命周期。参考 `packages/bundle/sdk-app/src/index.ts`：

- `inject = ['cmdlineArgs']`。
- 用 `@deepseek-ai/dsh-cmdline` 解析零选项命令与 help。
- 解析成功后提供一个服务（例如 `mygoAppStartup`）。
- transport 行 `inject: [mygoAppStartup]`，等服务出现后再占用 stdin/stdout。
- 用 `exitOnStdinEnd` 把 stdin EOF 绑定到有界成功退出。

这样 `dsh --profile mygo --help` 只打印帮助，不会启动 transport 或占用 stdout。

## 启动方式的选择

| 方式 | 优点 | 缺点 |
| --- | --- | --- |
| `dsh --profile mygo` | 复用 CLI 的 profile 解析、`profileContext`、插件管理、`--patch` | 多一层 CLI 解析；stdout 帮助/日志需要约束 |
| 私有 `mygo-host` bin 调用 `runProfile` | 完全控制参数、生命周期、stdout；与 desktop-host 同构 | 需要自己维护启动入口与 profile 初始化 |
| 进程内嵌 | 无跨进程协议 | MyGo 是 Go，无法运行 Cordis/Node；不可行 |

当前选择：**开发期用 `dsh --profile mygo`，发布版可用私有 `mygo-host`**。两者都必须走同一个 profile 与 bundle，避免开发/发布组合漂移。

## 对 dsh-mygo 的影响

1. 不需要 `dsh-web-app`、`host-webserver`、`client-*`、`dsh-app://`。
2. 需要自己提供 Host ↔ UI 的协议；这是本项目的核心增量。
3. 需要自己注册 approval / user-question 的 answerer，否则需要人工决策的工具调用会停住。
4. 需要自己做会话历史投影；base 负责持久化与事件记录，不负责 UI 状态。
5. 仍然需要 Node 与 DSH runtime，体积不会降到“纯 Go 小工具”。

## 相关文档

- [研究：传输方案](transport-options.md)
- [研究：dsh-TUI 参考](dsh-tui-reference.md)
- [profile 与 bundle](../subsystem/profile-bundle.md)
- [通道协议](../subsystem/channel-protocol.md)
