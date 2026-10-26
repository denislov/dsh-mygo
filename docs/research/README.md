# 研究文档

本目录记录立项阶段的事实调研与取舍。研究文档回答“为什么这样选”，不描述当前实现；实现契约以 [架构文档](../architecture.md) 和 [子系统文档](../subsystem/README.md) 为准。

| 文档 | 回答的问题 | 结论 |
| --- | --- | --- |
| [dsh-composition.md](dsh-composition.md) | DSH 的 profile / bundle 如何组合？自定义 surface 能否不加载 web-app？ | 可以：`dsh-base` + 自有 bundle |
| [mygo-native-ui.md](mygo-native-ui.md) | MyGo 0.6.2 提供哪些原生 UI 能力？有哪些生命周期约束？ | 足以承载桌面聊天 UI |
| [transport-options.md](transport-options.md) | Host 与 Go UI 之间用什么协议？ | 先用 SDK 验证，再落自定义 channel |
| [dsh-tui-reference.md](dsh-tui-reference.md) | dsh-TUI 如何作为非 Web surface 落地？哪些可借鉴？ | 借鉴组合与适配层，不复刻其 UI |
| [refactor-tooling.md](refactor-tooling.md) | 是否为协议词汇做代码生成、是否写主线程 analyzer、哪些度量/依赖/生成物检查进 CI？ | 暂不生成/不自造 analyzer；保留 fixture/vet/依赖与生成物检查 |

## 参考版本

| 项目 | 研究时版本 |
| --- | --- |
| DeepSeek Harness | `0.2.1-rc.2` |
| MyGo | `v0.6.2` |
| dsh-TUI | `0.16.1` |

版本会变化；协议与 API 以实际检出的源码和 package manifest 为准。

## 本地参考检出

研究时使用的仓库：

| 仓库 | 本地路径 | 用途 |
| --- | --- | --- |
| DeepSeek Harness | `D:\Workspace\deepseek-harness` | profile、bundle、agent runtime、SDK/ACP 协议 |
| MyGo | `D:\Workspace\neof\mygo` | 原生 UI、窗口、打包、更新 |
| dsh-TUI | `D:\Workspace\dsh-TUI` | 非 Web profile 的参考实现 |

文档中的 `packages/...`、`apps/...` 路径均指对应仓库内的路径，不是本仓库路径。

## 研究结论摘要

1. DSH 的 surface 由 profile 决定。`dsh-base` 已包含 agent、session、工具、设置、凭据、审批与用户提问；`dsh-web-app` 只是额外叠加 Web 传输和浏览器 UI。
2. dsh-mygo 应组合 `dsh-base` + 自有 app bundle，不加载 `dsh-web-app`。
3. MyGo 的原生 UI 足以实现聊天、Markdown 流式渲染、工具卡片、diff、列表与弹窗；不需要 webview。
4. Host 与 UI 之间需要一条协议。SDK 协议适合验证，但缺少审批、用户提问和历史回放；产品阶段使用自定义 channel。
5. dsh-TUI 证明了“base + UI bundle”的可行性。它的审批/提问适配层与 channel 语义可借鉴，但它的 UI 在 Node 进程内，不能直接照搬到 Go 进程。
