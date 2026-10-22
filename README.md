# dsh-mygo

DeepSeek Harness 的原生桌面客户端：MyGo 原生 UI + Node Host（`dsh-base` + 自有 bundle）。

- 不使用 `dsh-web-app`，不嵌入浏览器。
- 当前阶段：路线 A（SDK 验证）→ 路线 C（自定义 channel）。
- 目标平台：macOS、Windows；Linux 视后续需要。

当前 P1：原生窗口可以启动 Host、发送 prompt、显示提交后的 user/assistant/tool 事件与 running/idle 状态。逐字流式、审批、提问、历史、设置与打包在 P2。

## 文档

| 文档 | 内容 |
| --- | --- |
| [docs/architecture.md](docs/architecture.md) | 进程、组合、协议、状态、生命周期 |
| [docs/subsystem/](docs/subsystem/) | Host 监督、通道、投影、交互、UI、打包、测试 |
| [docs/research/](docs/research/) | 立项调研与取舍 |
| [AGENTS.md](AGENTS.md) | 文档路由、硬规则、命令 |

## 开发

```sh
go tool mygo dev
go test ./...
go tool mygo build
```

原生窗口 + fake host（无需 DSH 或 API key）：

```sh
go run . -host-command node -host-arg testdata/fake-sdk-host.mjs
```

原生窗口 + 真实 SDK profile：

```sh
# DSH checkout 先 pnpm install；DEEPSEEK_API_KEY 只从环境读取
go run . -dsh-dir D:/Workspace/deepseek-harness -dsh-home <临时目录>
```

P0 协议 spike（不开窗口，打印提交事件）：

```sh
go run ./cmd/spike -host-command node -host-arg testdata/fake-sdk-host.mjs -prompt "hello"
```

Host 侧需要 DeepSeek Harness 与 Node。真实集成测试使用临时 `DSH_HOME`，不要写入用户数据；不要提交 API key。
