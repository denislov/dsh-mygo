# 设置与凭据

## 职责

区分三类配置，避免“看起来都是设置”却由不同进程、不同文件拥有：

| 类别 | 拥有者 | 存储 | 示例 |
| --- | --- | --- | --- |
| DSH 设置与会话事实 | Host | `$DSH_HOME` 的 settings/credentials/session log | 默认模型、凭据、会话权限 |
| Profile 配置 | Host / 文件 | `$DSH_HOME/profiles/mygo` | bundle 列表、插件配置、`cordis.patch.yml` |
| UI 偏好 | Go | MyGo `PathUserData` | 主题、语言、快捷键、窗口位置、最近会话、草稿 |

原则：**一个设置只有一个权威来源**。UI 不缓存 DSH 设置的副本；显示时从 Host 读取，写入时走 Host。

## 会话模型设置

- `model.selection` capability 覆盖 `model/list`、`model/select` 与原生 Settings 面板；只在双方支持时显示入口，旧 Host/SDK 不显示，未协商调用返回 capability-unavailable。
- 面板提供 provider 目录、模型目录快捷选择、实际使用的 Model ID 和 adapter 声明的 effort；允许手填模型 ID，目录成员身份不充当路由校验。空 effort 表示 provider 默认，切回默认会清除上一请求继承的显式 effort，不硬编码 low/high。
- 目录使用公开 llm.listProviders/listModels/resolveModelInfo；仅返回 id/name、可用性与 effort 白名单，不返回完整 adapter 配置、endpoint、authorization、凭据引用或原始异常。目录可读不代表 API key 可用，也不承诺模型调用成功。不做付费生成来探测配置。
- 会话必须由此 Host 拥有且 idle；校验路由期间禁止 prompt、重复模型写入、history/resume。Host 用公开 resolveCallConfig 校验，再向真实 session log 提交 model/selection，通过 Agent setup 中安装的公开 installModelSelection 生效；不直接改 DSH core 或仅改 UI 标签。
- Apply model 仅影响此已拥有会话的下个回合；本次 App 内切回同一会话保留其已选模型。新建/冷恢复的启动路由由默认模型与显式 flags 决定，不自动从旧 model/selection 改写默认配置。
- App 在后台读取/提交，以同一通知队列处理 durable event 和响应，收到匹配会话/路由/idle 的确认且序列已前进后才更新标签。提交失败保留表单、草稿与原标签，并标记结果未确认：即使关闭面板，也禁止发送、切换会话或再次 Apply，必须 Refresh 成功查询 Host 当前路由后才解除，避免超时后的迟到提交导致误用模型。没有选择事件的确认使连接失败，不能伪装成功。
- 读取目录可 Close/Escape 取消；Apply 期间不能关闭、重复提交、发送或切换会话。审批/提问优先，不与 Settings/工具检视器叠加。元数据与校验各有 5 秒 Host 上限，Go 操作上限 15 秒；退出/过期响应不能回写新会话或失败状态。
- 目录预算为最多 32 providers、当前 provider 最多 128 models、每模型最多 32 efforts，响应不超过 256 KiB 且不超过配置帧上限的一半；Go 再校验类型、长度、唯一身份和 512 KiB 总输入预算。未知/失败 provider、目录或模型元数据明确提示不可用，不展示底层异常；手动模型仍由 Host 独立校验。
- 面板覆盖模型、持久默认模型与 DeepSeek 凭据；通用命名空间设置通过 `settings/get` / `settings/set` 白名单接入；权限切换、多 provider 凭据、账户登录与 UI 偏好持久化按各自章节实现。

## 持久默认模型与 DeepSeek 凭据

- `settings.configuration` capability 只公开 `configuration/get`、`configuration/default-model`、`configuration/credential` 三个白名单方法。服务缺失不协商，旧 Host/SDK 保持原行为。UI 不读取或解析 `.env`、profile patch 或凭据文件。
- 默认模型通过公开 `agentDefaultModel.currentSelection()` 读取，并核对 `settings.describe({redactSecrets:true})` 中固定 `agent-default-model` 行；用公开 `settings.replace` 携带预期 revision 写回完整路由。只写 provider/model/可选 reasoningEffort，空 effort 省略该字段以移除旧用户 override，不把空字符串伪装成 effort ID；恢复 base 继承值后再检查实际路由，不匹配则报告未确认并要求刷新。标准 base 未设置 effort，清除后为 provider 默认。目录校验不调用生成 API，也不承诺认证有效。
- **Save selection as default** 保存表单路由，当前/已拥有会话不变。不显式传 `-provider`、`-model`、`-effort` 时，CLI 对支持此能力的 Host 发送 `useDefaultModel:true`，首次启动、新建、冷恢复均使用 Host 当时的默认值；显式传任一路由 flag（包括空 `-effort`）则使用完整启动路由。旧 Host/SDK 仍使用原有 fallback flags。程序化 App Options 需显式开启 UseHostDefaults。
- 凭据范围仅为公开 llm configurable-provider 目录中 `deepseek-official` → 固定 `llm-deepseek` 行的根 `apiKeyEnv` 引用；绑定缺失/不兼容时显示不可用，不猜测 DEEPSEEK_API_KEY，也不允许 UI 任意指定其它环境引用。返回状态只含 available/reference/configured/writable/source；source 固定为 none/managed/environment/fallback，不传路径、完整 descriptor、明文或原始异常。
- 状态使用公开 `credentials.describe`，从不调用 `resolve` 回读明文。输入使用原生 Password；只在显式 Save key 时通过 stdio 写入 `credentials.set`。提交、刷新、关闭、失败/退出时清空表单并更换控件 key，调试格式化也只显示 redacted。读取结果永远没有密钥值；“已配置”不等于经过认证验证。
- **Clear managed key** 需先勾选确认，只调用公开 `credentials.unset` 删除 DSH 管理的值；环境变量或 `.env` fallback 仍可使状态保持 configured，这不是远端撤销 API key。DSH 管理值优先于环境/fallback；设置与清除都不编辑用户 `.env`。
- 凭据由 DSH 写入 `$DSH_HOME/.credentials.yaml`，不是本应用的明文副本，更不是加密 OS vault。Unix 文件权限由 DSH 管理；不承诺 Windows ACL 或系统钥匙串加密。
- 全局写入要求所有已拥有会话 idle，期间阻止 prompt/会话创建恢复/模型写入/重复配置写入。默认模型有 revision 冲突检查；凭据写入前后校验绑定，底层原子写与跨进程锁由 DSH 拥有，不宣称凭据值有 revision/CAS 保护。
- 写入不使用提前返回的 Promise.race：底层锁/写入未结束，Host 一直保留 pending；查询等待 pending 完成才建立安全快照。Go 写入上限 60 秒，读取仍为 15 秒。失败或超时后 UI 禁止发送、导航、再次写入，必须 Refresh 成功读取模型和配置后解锁；Host 也要求失败写入后先查询。退出期间不报告迟到成功，持久原子性由 DSH 保证。
- 成功只以 Host 回传的白名单快照确认；保存默认不会产生 session model/selection，也不改当前标签/聊天/草稿。配置写入通过设置变更通知广播；其它窗口刷新后采用。

## DSH 设置

### Host 服务

base 提供：

- `settings`：命名空间、读取、更新、替换、变更通知。
- `authorization`：凭据引用解析。
- `credentials-local`：`$DSH_HOME/.credentials.yaml` 等本地存储。
- `deepseek-account` / Platform：账户登录（如启用）。
- `config-editor`：把设置写回 profile patch（仅在有 `profileContext` 时）。

### 协议

协议暴露最小面：

| 方法 | 用途 |
| --- | --- |
| `model/list` | 可用 provider/model/effort |
| `model/select` | 当前会话模型 |
| `configuration/get` | 默认模型与凭据安全状态 |
| `configuration/default-model` | 固定默认模型行的 revision 写入 |
| `configuration/credential` | 配置绑定的 DeepSeek 引用 set/unset |
| `permission/get` | 已拥有会话的当前权限、实际 sandbox/approval 和可选预设 |
| `permission/select` | 通过公开权限服务切换当前会话并落盘 |
| `settings/get` | 按命名空间读取非 secret 设置 |
| `settings/set` | 按命名空间写入白名单设置 |
| `settings/open-document` | 由 Host 打开对应设置文档/编辑器 |

规则：

- 凭据只返回引用/状态，协议从不返回明文；`settings/get` 也永不返回 secret。
- 通用设置只接受已声明命名空间；未知命名空间、越权 key 或 secret 写入返回 `settings-rejected`。
- 写入失败返回结构化错误（`settings-conflict`、`settings-rejected`）。
- 配置写入以 Host 返回的快照为准，UI 不做乐观覆盖；Host 通过设置变更通知广播新快照，其它窗口刷新后采用。
- 不在 UI 里解析 `cordis.patch.yml`；那是 Host 的所有权。

### 模型与 effort

- 默认模型来自 base 的 `agent-default-model`。
- 新建会话可以显式传 provider/model/effort。
- 已有会话的选择是会话级事实；UI 切换模型时调用 `model/select`，不要只改本地显示。选择通过 model/selection + 公开 Agent-scoped selection 实现，不更改 agent-default-model。
- 模型目录来自 `model/list`；不可用的 provider 显示原因，不要隐藏。

### 权限模式

- `permission-presets` 与部署配置（包括 `DSH_PERMISSION_MODE`）决定新会话初值；会话日志中的 `permission/preset`、`sandbox/mode`、`approval/policy` 是当前会话事实。切换保存于该会话，不改环境变量或未来会话默认值，恢复保留原权限。
- `session.permissions` 独立协商 `permission/get|select`，不依赖模型目录能力。Host 使用公开 `permissionPresets.catalog/current/resolve/set`、`sandboxPolicy.resolve`、`approval.effectivePolicy` 与 `sessionPersistence.flush`；只操作本 Host 拥有的会话，不读取私有字段或自行重写工具策略。
- 快照包括当前预设、实际 sandbox/approval、nextSeq 与有界选项；`custom` 仅显示，不可选。默认 workspace-write 为 workspace-write/ask，默认 danger-full-access 为 danger-full-access/never；自定义名称和标签不代表实际权限，以公开 resolve 的 bundle 为准。`never` 不弹出审批，但显式要求审批的操作会被 DSH 拒绝，不等于自动批准。
- 首页在初始化、恢复、切换会话时从 Host 读取权限；Settings 刷新目录和实际状态。当前模式展示包括实际 knobs；收到权限事件或进入 resync 时使旧快照失效，不能继续标成当前值。实时恢复不推断遗漏的权限事件，显示刷新提示。
- 切换要求所属会话 idle，无模型/元数据/配置写入。请求带 expectedNextSeq 检查会话边界；Host 读取目标预设的实际 bundle，如果 sandbox 是 danger-full-access 或 approval 是 never，必须有 confirmDangerous:true。UI 展示目标实际值、风险和默认不勾选的确认；改变选择、刷新、关闭或提交后清除确认。
- 写入预占会话直至公开 set 和 flush 完成，期间拒绝 prompt/resume/history/模型或元数据写入，退出等待已接受任务结算。返回完整快照确认 current/knobs/nextSeq；幂等选择不要求产生新事件。UI 不乐观覆盖，保持草稿和当前会话，15 秒读取、60 秒写入，后台处理并拒绝过期结果。
- 失败或超时后显示固定错误，禁止发送、会话导航及下一设置写入，必须 Refresh 成功后解除。Host 同样保留失败确认屏障，get 等待已有写入完成并重新 flush；不以 RPC 超时冒充底层写入取消。选项最多 32，快照最多 32 KiB 且不超过配置帧上限一半；只返回白名单字符串/枚举/计数，不返回 workspaceRoot、配置对象或原始异常。

## 凭据

### 规则

- 凭据只写不读：Host 不回传明文，UI 不缓存明文。
- API key 输入使用密码控件（`ui.TextInput(...).Password()`），提交后清空缓冲。
- 日志、崩溃报告、错误消息、协议 `data` 不包含凭据。
- 凭据引用（名称/来源）可以展示；值不可以。
- 环境变量与 `.env` 的优先级由 DSH 决定；UI 只展示“已配置/未配置”，不猜测来源内容。

### 账户登录

- `deepseek-account` 在 profile 中启用时，由 Host 负责登录与状态。
- 登录使用系统浏览器 OAuth/PKCE 或 Host 提供的设备码流程；token 只存在 Host/DSH 存储，不进入 UI、日志或崩溃报告。
- UI 只展示登录状态、账户标识与退出入口；退出清除 Host 侧 token，不删除项目数据。

## UI 偏好

UI 偏好属于 Go 进程，持久化在 `PathUserData`，写入使用原子替换（临时文件 + rename），只保存设备/窗口偏好，不复制 DSH 设置。

```json
{
  "version": 1,
  "window": { "width": 1100, "height": 760, "sidebar": true },
  "theme": "system",
  "locale": "zh-CN",
  "lastSessionId": "...",
  "drafts": { "<sessionId>": "..." },
  "shortcuts": { "send": "Enter", "cancel": "Escape" }
}
```

- 只保存可重建的 UI 状态：窗口尺寸/位置、主题、语言、侧栏、最近会话、快捷键、有界草稿。
- 不保存 transcript、工具输出、附件内容、凭据或任何模型可见上下文。
- 草稿按会话有界保存（例如每会话 64 KiB、总 1 MiB），退出/切换后仍可恢复；损坏文件回退默认值并备份一次。
- 主题默认跟随系统；语言与 Host locale 可以不同。

## 并发与冲突

- 设置写入以 Host 的 revision/快照为准；UI 的旧值被拒绝时刷新。
- 多窗口/多连接：设置以 Host 广播快照为准，其它窗口刷新后采用；UI 不假定自己是唯一写入者。
- 凭据更新与运行中的请求：Host 负责让新请求使用新凭据；UI 不重放旧请求。
- 模型/effort 变更在下个回合生效。

## 失败模式

| 失败 | 处理 |
| --- | --- |
| 写入冲突 | 刷新后重试；不覆盖他人修改 |
| 凭据拒绝 | 显示安全固定错误、清空输入，刷新确认后重输 |
| 配置写入超时 | 禁止发送/导航，Refresh 成功后才解锁 |
| 目录或元数据失败 | 标记不可用，不展示底层异常 |
| 旧 Host 未协商 | 隐藏入口，未协商调用返回 capability-unavailable |
| 未知命名空间/secret 写入 | `settings-rejected`；UI 展示不支持，不重试 |
| 账户登录失败/过期 | 显示状态与重新登录入口；不显示 token |

## 测试

- 模型目录：schema、secret 排除、预算、不可用 provider。
- 会话选择：确认事件、失败保留表单与草稿、未确认时禁止发送。
- 持久配置：revision 冲突、凭据 set/unset、安全状态、失败后必须刷新。
- 通用设置：命名空间白名单、secret 排除、变更广播与多窗口刷新。
- UI 偏好：schema、原子写、损坏回退、草稿有界恢复。
- 账户登录：状态展示、退出清 token、失败重试（不触网时用 fake provider）。
- UI：`ui.NewTester` 覆盖最小窗口、密码输入清空与控件重建。
- 真实 Loader：`TestAgainstMygoModelSettings`、`TestAgainstMygoConfiguration` 验证实际请求路由与 Host 重启后的持久化。

## 相关文档

- [交互](interaction.md)
- [UI 壳层](ui-shell.md)
- [profile 与 bundle](profile-bundle.md)
- [进度与路线](../roadmap.md)
- [架构](../architecture.md)
