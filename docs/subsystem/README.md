# 子系统文档

| 文档 | 负责 | 关键契约 |
| --- | --- | --- |
| [host-supervisor.md](host-supervisor.md) | Node Host 的启动、stdio、停止、重启与恢复 | 进程边界与生命周期 |
| [channel-protocol.md](channel-protocol.md) | Go UI 与 Host 的 JSON-RPC 协议 | 方法、通知、顺序、错误、版本 |
| [profile-bundle.md](profile-bundle.md) | DSH profile 与 `@dsh-mygo/host` bundle | 组合、startup、兼容性 |
| [runtime-manager.md](runtime-manager.md) | runtime store、安装/下载/选择/切换 | 独立发布、manifest、事务切换 |
| [session-projection.md](session-projection.md) | session 事件与流式片段 → UI 状态 | 真相源、流式合并、历史 |
| [interaction.md](interaction.md) | 审批与用户提问 | answerer、队列、取消 |
| [settings-credentials.md](settings-credentials.md) | 设置、模型、凭据、UI 偏好 | 所有权、redaction |
| [ui-shell.md](ui-shell.md) | MyGo 原生 UI 壳层 | 页面、输入、性能、测试 |
| [packaging-updates.md](packaging-updates.md) | 打包、签名、更新 | 版本绑定、首启、迁移 |
| [testing.md](testing.md) | 测试策略 | 分层、fixture、平台矩阵 |

## 阅读顺序

1. 新加入：`channel-protocol` → `session-projection` → `ui-shell`。
2. 改 Host：`profile-bundle` → `host-supervisor` → `interaction`。
3. 发布：`packaging-updates` → `testing`。

## 相关文档

- [架构](../architecture.md)
- [研究](../research/README.md)
- [进度与路线](../roadmap.md)
- [AGENTS.md](../../AGENTS.md)
