# dsh-mygo 文档

dsh-mygo 是 DeepSeek Harness 的原生桌面客户端：MyGo 负责窗口与 UI，Node 进程负责 DSH agent runtime。项目不使用 `dsh-web-app`，也不嵌入浏览器。

## 阅读路线

| 目标 | 顺序 |
| --- | --- |
| 查当前进度与下一步 | [开发进度与路线图](roadmap.md) |
| 第一次了解项目 | 本页 → [架构](architecture.md) → [研究：DSH 组合](research/dsh-composition.md) |
| 开始写代码 | [架构](architecture.md) → [UI 壳层](subsystem/ui-shell.md) → [通道协议](subsystem/channel-protocol.md) → [宿主监督](subsystem/host-supervisor.md) |
| 改 Host 侧 | [profile 与 bundle](subsystem/profile-bundle.md) → [通道协议](subsystem/channel-protocol.md) → [交互](subsystem/interaction.md) |
| 改会话展示 | [会话投影](subsystem/session-projection.md) → [UI 壳层](subsystem/ui-shell.md) |
| 改设置/模型/凭据 | [设置与凭据](subsystem/settings-credentials.md) → [交互](subsystem/interaction.md) |
| 打包发布 | [打包与更新](subsystem/packaging-updates.md) |
| 查子系统索引 | [subsystem/README.md](subsystem/README.md) |
| 查取舍原因 | [研究文档](research/README.md) |

## 文档分层

- `docs/research/`：立项时的事实调研与取舍。回答“为什么这样选”，不描述当前实现。
- `docs/architecture.md`：跨子系统的结构、进程边界、数据流、关键决策。
- `docs/subsystem/`：一个子系统一份文档，描述职责、接口、失败模式与测试。
- `AGENTS.md`：给协作 agent 的路由与硬规则。改代码前先读它。

## 文档约定

- 一个事实只有一个主页；其他文档链接过去，不复制。
- 设计文档描述当前契约；未定事项放在“未决问题”，不要写成既成事实。
- 代码路径、方法名、协议字段名保持英文原文。
- 每份文档末尾列出相关文档，形成双向链接。
