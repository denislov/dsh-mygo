# 设置与凭据

## 职责

区分三类配置，避免“看起来都是设置”却由不同进程、不同文件拥有：

| 类别 | 拥有者 | 存储 | 示例 |
| --- | --- | --- | --- |
| DSH 设置 | Host | `$DSH_HOME` 的 settings/credentials | 模型路由、权限模式、搜索 API key |
| Profile 配置 | Host / 文件 | `$DSH_HOME/profiles/mygo` | bundle 列表、插件配置、`cordis.patch.yml` |
| UI 偏好 | Go | MyGo `PathUserData` | 主题、语言、快捷键、窗口位置、最近会话、草稿 |

原则：**一个设置只有一个权威来源**。UI 不缓存 DSH 设置的副本；显示时从 Host 读取，写入时走 Host。

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
| `model/select` | 当前会话或默认模型 |
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
- 已有会话的选择是会话级事实；UI 切换时调用 `model/select`，不要只改本地显示。
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
| 凭据拒绝 | 显示 provider 的可读原因；保留输入以便修正 |
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
