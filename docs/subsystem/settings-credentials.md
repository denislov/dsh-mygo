# 设置与凭据

## 职责

区分三类配置，避免“看起来都是设置”却由不同进程、不同文件拥有：

| 类别 | 拥有者 | 存储 | 示例 |
| --- | --- | --- | --- |
| DSH 设置 | Host | `$DSH_HOME` 的 settings/credentials | 模型路由、权限模式、搜索 API key |
| Profile 配置 | Host / 文件 | `$DSH_HOME/profiles/mygo` | bundle 列表、插件配置、`cordis.patch.yml` |
| UI 偏好 | Go | MyGo `PathUserData` | 主题、语言、快捷键、窗口位置、最近会话、草稿 |

原则：**一个设置只有一个权威来源**。UI 不缓存 DSH 设置的副本；显示时从 Host 读取，写入时走 Host。

## 当前实现：会话模型设置

- 已实现 `model.selection` capability、`model/list`、`model/select` 与原生 Settings 面板。只在双方支持时显示入口；旧 Host/SDK 不显示，未协商调用返回 capability-unavailable。
- 面板提供 provider 目录、模型目录快捷选择、实际使用的 Model ID 和 adapter 声明的 effort；允许手填模型 ID，目录成员身份不充当路由校验。空 effort 表示 provider 默认，切回默认会清除上一请求继承的显式 effort，不硬编码 low/high。
- 目录使用公开 llm.listProviders/listModels/resolveModelInfo；仅返回 id/name、可用性与 effort 白名单，不返回完整 adapter 配置、endpoint、authorization、凭据引用或原始异常。目录可读不代表 API key 可用，也不承诺模型调用成功。不做付费生成来探测配置。
- 当前会话必须由此 Host 拥有且 idle；校验路由期间禁止 prompt、重复模型写入、history/resume。Host 用公开 resolveCallConfig 校验，再向真实 session log 提交 model/selection，通过 Agent setup 中安装的公开 installModelSelection 生效；不直接改 DSH core 或仅改 UI 标签。
- Apply model 仅影响此已拥有会话的下个回合；本次 App 内切回同一会话保留其已选模型。新建/冷恢复的启动路由按下节决定，不自动从旧 model/selection 改写默认配置。
- App 在后台读取/提交，以同一通知队列处理 durable event 和响应，收到匹配会话/路由/idle 的确认且序列已前进后才更新标签。提交失败保留表单、草稿与原标签，并标记结果未确认：即使关闭面板，也禁止发送、切换会话或再次 Apply，必须 Refresh 成功查询 Host 当前路由后才解除，避免超时后的迟到提交导致误用模型。没有选择事件的确认使连接失败，不能伪装成功。
- 读取目录可 Close/Escape 取消；Apply 期间不能关闭、重复提交、发送或切换会话。审批/提问优先，不与 Settings/工具检视器叠加。元数据与校验各有 5 秒 Host 上限，Go 操作上限 15 秒；退出/过期响应不能回写新会话或失败状态。
- 目录预算为最多 32 providers、当前 provider 最多 128 models、每模型最多 32 efforts，响应不超过 256 KiB 且不超过配置帧上限的一半；Go 再校验类型、长度、唯一身份和 512 KiB 总输入预算。未知/失败 provider、目录或模型元数据明确提示不可用，不展示底层异常；手动模型仍由 Host 独立校验。
- **尚未实现**：任意命名空间 settings/get/set、权限切换、多 provider 凭据编辑、账户登录、主题/语言等 UI 偏好持久化。下文未列为当前实现的通用设置/账户/UI 偏好仍是设计。

## 当前实现：持久默认模型与 DeepSeek 凭据

- 新增 `settings.configuration` capability；仅公开 `configuration/get`、`configuration/default-model`、`configuration/credential` 三个白名单方法。服务缺失不协商，旧 Host/SDK 保持原行为。UI 不读取或解析 `.env`、profile patch 或凭据文件。
- 默认模型通过公开 `agentDefaultModel.currentSelection()` 读取，并核对 `settings.describe({redactSecrets:true})` 中固定 `agent-default-model` 行；用公开 `settings.replace` 携带预期 revision 写回完整路由。只写 provider/model/可选 reasoningEffort，空 effort 省略该字段以移除旧用户 override，不把空字符串伪装成 effort ID；恢复 base 继承值后再检查实际路由，不匹配则报告未确认并要求刷新。标准 base 未设置 effort，清除后为 provider 默认。目录校验不调用生成 API，也不承诺认证有效。
- **Save selection as default** 保存表单路由，当前/已拥有会话不变。不显式传 `-provider`、`-model`、`-effort` 时，CLI 对支持此能力的 Host 发送 `useDefaultModel:true`，首次启动、新建、冷恢复均使用 Host 当时的默认值；显式传任一路由 flag（包括空 `-effort`）则使用完整启动路由。旧 Host/SDK 仍使用原有 fallback flags。程序化 App Options 需显式开启 UseHostDefaults。
- 凭据范围仅为公开 llm configurable-provider 目录中 `deepseek-official` → 固定 `llm-deepseek` 行的根 `apiKeyEnv` 引用；绑定缺失/不兼容时显示不可用，不猜测 DEEPSEEK_API_KEY，也不允许 UI 任意指定其它环境引用。返回状态只含 available/reference/configured/writable/source；source 固定为 none/managed/environment/fallback，不传路径、完整 descriptor、明文或原始异常。
- 状态使用公开 `credentials.describe`，从不调用 `resolve` 回读明文。输入使用原生 Password；只在显式 Save key 时通过 stdio 写入 `credentials.set`。提交、刷新、关闭、失败/退出时清空表单并更换控件 key，调试格式化也只显示 redacted。读取结果永远没有密钥值；“已配置”不等于经过认证验证。
- **Clear managed key** 需先勾选确认，只调用公开 `credentials.unset` 删除 DSH 管理的值；环境变量或 `.env` fallback 仍可使状态保持 configured，这不是远端撤销 API key。DSH 管理值优先于环境/fallback；设置与清除都不编辑用户 `.env`。
- 凭据由 DSH 写入 `$DSH_HOME/.credentials.yaml`，不是本应用的明文副本，更不是加密 OS vault。Unix 文件权限由 DSH 管理；不承诺 Windows ACL 或系统钥匙串加密。
- 全局写入要求所有已拥有会话 idle，期间阻止 prompt/会话创建恢复/模型写入/重复配置写入。默认模型有 revision 冲突检查；凭据写入前后校验绑定，底层原子写与跨进程锁由 DSH 拥有，不宣称凭据值有 revision/CAS 保护。
- 写入不使用提前返回的 Promise.race：底层锁/写入未结束，Host 一直保留 pending；查询等待 pending 完成才建立安全快照。Go 写入上限 60 秒，读取仍为 15 秒。失败或超时后 UI 禁止发送、导航、再次写入，必须 Refresh 成功读取模型和配置后解锁；Host 也要求失败写入后先查询。退出期间不报告迟到成功，持久原子性由 DSH 保证。
- 成功只以 Host 回传的白名单快照确认；保存默认不会产生 session model/selection，也不改当前标签/聊天/草稿。当前连接不广播给其它窗口，多窗口通知仍未实现。

## DSH 设置

### Host 服务

base 提供：

- `settings`：命名空间、读取、更新、替换、变更通知。
- `authorization`：凭据引用解析。
- `credentials-local`：`$DSH_HOME/.credentials.yaml` 等本地存储。
- `deepseek-account` / Platform：账户登录（如启用）。
- `config-editor`：把设置写回 profile patch（仅在有 `profileContext` 时）。

### 协议

第一版只暴露最小面：

| 方法 | 用途 |
| --- | --- |
| `model/list` | 可用 provider/model/effort |
| `model/select` | 当前会话模型 |
| `configuration/get` | 已实现：默认模型与凭据安全状态 |
| `configuration/default-model` | 已实现：固定默认模型行的 revision 写入 |
| `configuration/credential` | 已实现：配置绑定的 DeepSeek 引用 set/unset |
| `settings/get` | 读取一个命名空间（redacted） |
| `settings/set` | 写入一个命名空间 |
| `settings/open-document` | 可选：Host 打开原生设置文档 |

规则：

- `settings/get` 永不返回 secret；凭据只返回引用/状态。
- 写入失败返回结构化错误（`settings-conflict`、`settings-rejected`）。
- 变更后 Host 广播设置快照；UI 以广播值为准，不做乐观覆盖。
- 不在 UI 里解析 `cordis.patch.yml`；那是 Host 的所有权。

### 模型与 effort

- 默认模型来自 base 的 `agent-default-model`。
- 新建会话可以显式传 provider/model/effort。
- 已有会话的选择是会话级事实；UI 切换模型时调用 `model/select`，不要只改本地显示。当前通过 model/selection + 公开 Agent-scoped selection 实现，不更改 agent-default-model。
- 模型目录来自 `model/list`；不可用的 provider 显示原因，不要隐藏。

### 权限模式

- 由 `permission-presets` 与 `DSH_PERMISSION_MODE` 决定。
- UI 可以展示当前模式并提供切换，但切换必须通过 Host；不能只在本地改一个变量。
- `danger-full-access` 的确认文案要明确风险；不要默认选中。

## 凭据

### 规则

- 凭据只写不读：Host 不回传明文，UI 不缓存明文。
- API key 输入使用密码控件（`ui.TextInput(...).Password()`），提交后清空缓冲。
- 日志、崩溃报告、错误消息、协议 `data` 不包含凭据。
- 凭据引用（名称/来源）可以展示；值不可以。
- 环境变量与 `.env` 的优先级由 DSH 决定；UI 只展示“已配置/未配置”，不猜测来源内容。

### 账户登录

- 如果启用 `deepseek-account`，登录流程由 Host 提供服务；UI 打开系统浏览器或嵌入的登录页（第一版建议系统浏览器）。
- 回调 URL 只接受 loopback 或已注册 scheme；校验 state/PKCE。
- 登录状态通过 Host 的事件/查询获取；token 不进入 UI 状态。

## UI 偏好

保存在 MyGo 的 `PathUserData`，建议 JSON 文件：

```json
{
  "theme": "system",
  "locale": "zh-CN",
  "shortcuts": {},
  "window": { "width": 1280, "height": 820, "maximized": false },
  "recentSessions": ["..."],
  "layout": { "sidebar": true, "rightPanel": false }
}
```

规则：

- 写入使用原子替换（临时文件 + rename），避免半写。
- 启动时读取失败使用默认值，并记录一次诊断；不要阻止启动。
- 草稿可以按会话保存；关闭会话/退出时持久化，发送成功后清除。
- 不把 DSH 设置复制到这里；只保存“这个设备/这个窗口”的偏好。

## 并发与冲突

- 设置写入以 Host 的 revision/快照为准；UI 的旧值被拒绝时刷新。
- 多窗口（未来）共享 Host 设置；每个窗口的 UI 偏好独立或按窗口 id 区分。
- 凭据更新与运行中的请求：Host 负责让新请求使用新凭据；UI 不重放旧请求。
- 设置变更影响权限/模型时，明确提示“下个回合生效”或“立即生效”。

## 失败模式

| 失败 | 处理 |
| --- | --- |
| 设置命名空间不存在 | 显示可用命名空间/配置错误 |
| 写入冲突 | 刷新后重试；不覆盖他人修改 |
| 凭据拒绝 | 显示安全固定错误、清空输入，刷新确认后重输 |
| 设置文件损坏 | 使用默认值并报告；不静默重置用户数据 |
| 账户 token 过期 | 提示重新登录；不自动使用过期 token |

## 测试

- 设置读写：redaction、冲突、不存在命名空间。
- 凭据：只写不读、日志无 secret、错误消息无 secret。
- 模型选择：目录、会话级覆盖、不可用 provider。
- UI 偏好：原子写、损坏文件、默认值。
- 账户登录：state/PKCE、回调校验（如启用）。

## 相关文档

- [交互](interaction.md)
- [UI 壳层](ui-shell.md)
- [profile 与 bundle](profile-bundle.md)
- [架构](../architecture.md)
