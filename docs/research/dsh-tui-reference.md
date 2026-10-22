# dsh-TUI 参考研究

## 结论

dsh-TUI 证明了“非 Web surface = `dsh-base` + 自有 UI bundle”的可行性。它的 **profile 结构、审批/提问适配层、channel 语义、启动器自举** 都值得借鉴；但它的 UI 运行在 Node 进程内，dsh-mygo 不能照搬。

## 它是什么

- 包名：`@deepseek-harness-tui/dsh-tui`。
- 一个 bundle：`package.json` 的 `dsh.bundle.patch` 指向 `cordis.patch.yml`。
- 自己的 profile：`$DSH_HOME/profiles/dsh-tui`。
- 启动：`dsh --profile dsh-tui`；`dsh-tui` / `dst` 是委托启动器。
- UI：Ink/React，在 Host 的 Node 进程内渲染终端界面。
- 不修改 DSH core；只消费公开导出。

## Profile 组合

`cordis.yml` 是一个裸组合示例，包含：

| 行 | 作用 |
| --- | --- |
| `user-questions` | 提供 `ctx.userQuestions`，承接模型的 `ask_user_question` |
| `approval` | 提供 `ctx.approval`，承接 sandbox 升级等审批 |
| `dsh-tui-workspaces` | 工作区 |
| `dsh-tui-command-trees` | 命令目录 |
| `dsh-tui-settings-sections` | 设置页 |
| `dsh-tui-scenes` | 场景 |
| `dsh-tui-plugin-host` | 插件互操作 |
| `dsh-tui-extensions` | 对话框、状态栏、快捷键、渲染器、主题 |
| `dsh-tui` | TUI 本体 |
| `dsh-tui-webserver` / `dsh-tui-auth` | 可选远程 channel |

bundle 的 `cordis.patch.yml` 则以 `dsh-base` 为底，覆盖 persona、模型、agent 预设、工具等行。

## 适配层

dsh-TUI 的 `src/dsh-adapter/` 是它与 DSH 的边界。对本项目最有参考价值的部分：

| 文件 | 作用 |
| --- | --- |
| `plugin.ts` | 在 Cordis 中挂载 TUI，创建/恢复 agent，绑定服务 |
| `approvals.ts` | `ApprovalStore`：接收 `approval/request`，在 UI 里排队、展示、回答 |
| `questions.ts` / `questions-answerer.ts` | `UserQuestionService` 的 provider/answerer |
| `sessionTree.ts` | 从 session log 构建 transcript、工具卡片、turn 边界 |
| `channel.ts` / `channel-facade-actions.ts` | 本地 channel 与远程 channel 的桥接 |
| `presets.ts` | 会话 preset 与工具过滤 |
| `backend-registry.ts` | 多后端（DSH/Claude/Codex）的注册与准入 |

关键点：**审批与提问必须有 UI answerer**。base 只提供服务，不替用户回答。dsh-TUI 在插件 apply 时注册 answerer，并把请求投影到面板。

## Channel 协议

`tui-profile/notes/0007-tui-channel.md` 定义了一个有状态 Channel：

- `open`：创建/附着，返回完整 snapshot。
- `subscribe`：从指定 version 订阅，先 snapshot 后增量。
- `invoke`：调用方法，返回 JSON 结果；状态变化同时返回最新 snapshot。
- `close`：释放附着。

约定：

- 每个 connection scope 只绑定一个 provider。
- snapshot 必须可序列化，不含函数、进程句柄、secret。
- version 单调递增；consumer 发现缺口/倒退必须重新 `open` 或 `subscribe`。
- 取消只撤销一次 invocation，不关闭 channel。
- provider 拥有 Agent/Session/Workspace authority；transport 不解释业务方法。

`0008-tui-channel-http.md` 给出 HTTP/WebSocket 端点：`/health`、`/endpoint`、`/connection`，默认 `127.0.0.1:10721`。

## 启动器

`bin/dsh-tui.js` 是双态启动器：

- 全局安装副本：找到 profile 内的同包 bin，委托执行；首次运行探测 `dsh`/`pnpm` 并自举 `dsh plugin --profile dsh-tui add ...`。
- profile 内副本：预检、版本核对、`dsh --profile dsh-tui` 启动、退出码透传。

它还维护 rescue profile（base + TUI，无第三方插件）作为干净启动通道。

## 可借鉴

1. **profile 结构**：base + 一个自有 bundle；不碰 web-app。
2. **审批/提问 answerer**：这是任何非 Web UI 都必须补的适配层。
3. **session 投影**：从 `SessionEvent` 构建 turn / tool / transcript，而不是另建状态源。
4. **channel 语义**：open/subscribe/invoke/close、单调 version、snapshot + 增量。
5. **启动器自举**：首次运行安装 profile bundle；rescue profile。
6. **可选服务降级**：用 `ctx.get` 读取可选服务，缺失时让扩展面失效而不是启动失败。

## 不可照搬

1. **UI 在进程内**：dsh-TUI 直接调用 `ctx.get('agents')`；dsh-mygo 的 UI 是 Go 进程，必须走 wire protocol。
2. **Ink/React 渲染**：终端布局、键盘、图片协议、OSC 通知都是 TUI 专属；原生 UI 用 MyGo 的组件。
3. **完整的 TUI surface**：dsh-TUI 有大量终端专属交互（alt-screen、鼠标选区、Kitty 图片、OSC 通知）。dsh-mygo 不需要。
4. **TUI 私有协议坐标**：`tui.dsh/v1alpha1` 是 TUI 的生态协议；dsh-mygo 应定义自己的小协议，而不是复用 TUI 的全部语义。
5. **Node 进程内的插件生态**：dsh-TUI 的插件 host/扩展面服务于 TUI 插件；dsh-mygo 的扩展面应听从自己的 UI 需求。

## 对 dsh-mygo 的直接影响

- 采用与 dsh-TUI 相同的 **bundle + profile** 形态：`@your-scope/dsh-mygo-app`。
- 在 Host 插件里注册 approval / question answerer，把请求通过 channel 发给 Go。
- 会话投影参考 `sessionTree.ts` 的事件分类，但用 Go 实现。
- 启动器参考 `bin/dsh-tui.js` 的自举与 rescue 思路。
- 不复制 TUI 的 channel 全部字段；只保留 dsh-mygo 需要的方法与通知。

## 相关文档

- [DSH 组合模型](dsh-composition.md)
- [传输方案](transport-options.md)
- [profile 与 bundle](../subsystem/profile-bundle.md)
- [交互](../subsystem/interaction.md)
