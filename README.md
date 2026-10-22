# dsh-mygo

DeepSeek Harness 的原生桌面客户端：MyGo 原生 UI + Node Host（`dsh-base` + 自有 bundle）。

- 不使用 `dsh-web-app`，不嵌入浏览器。
- 当前阶段：路线 A（SDK 验证）→ 路线 C（自定义 channel）。
- 目标平台：macOS、Windows；Linux 视后续需要。

自定义 channel 阶段 1、回合取消与工具审批已落地：默认使用 `mygo` profile，校验 UI/Host/DSH 版本，显式创建会话、发送 prompt、显示提交后的 user/assistant/tool 事件与 running/idle 状态。协商后提供 Stop 与原生 Allow once / Deny 审批面板，保留会话与下一条草稿。SDK 链路保留作回归入口。逐字流式、提问、历史、设置与打包尚未实现。

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
go run . -host-command node -host-arg testdata/fake-mygo-host.mjs
```

验证 Stop 按钮时额外传入 `-host-arg --wait-for-cancel`：第一回合会等待取消，下一回合恢复普通 Echo 回复。

验证审批时额外传入 `-host-arg --approval`：第一回合等待 Allow once / Deny / Stop turn；后续回合恢复 Echo，不写真实文件。

原生窗口 + 真实自定义 profile（Node 24+；DSH `0.2.1-rc.2`）：

```sh
# DSH checkout 先安装依赖并构建；DEEPSEEK_API_KEY 只从环境读取
node resources/host/setup-profile.mjs --dsh-home D:/Temp/dsh-mygo-dev
go run . -dsh-dir D:/Workspace/deepseek-harness -dsh-home D:/Temp/dsh-mygo-dev
```

初始化脚本只创建新的 `mygo` profile，将本仓库的 `resources/host` 链接为 bundle；已有 profile 不会被覆盖。`go tool mygo dev` 使用的 `DSH_HOME` 中也需要先初始化该 profile；这是开发入口，运行时打包留到后续阶段。

无需 API key 的真实 Loader + mock 模型集成（PowerShell）：

```powershell
$env:DSH_MYGO_DSH_DIR = 'D:/Workspace/deepseek-harness'
go test ./internal/app -run TestAgainstMygoRealComposition -v -timeout 2m
node --test resources/host/tests/*.test.mjs
```

显式使用旧 SDK 窗口链路：

```sh
go run . -sdk-protocol -profile sdk -host-command node -host-arg testdata/fake-sdk-host.mjs
```

P0 协议 spike（不开窗口，打印提交事件）：

```sh
go run ./cmd/spike -host-command node -host-arg testdata/fake-sdk-host.mjs -prompt "hello"
```

Host 侧需要 DeepSeek Harness 与 Node。真实集成测试使用临时 `DSH_HOME`，不要写入用户数据；不要提交 API key。
