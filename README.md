# dsh-mygo

DeepSeek Harness 的原生桌面客户端：MyGo 原生 UI + Node Host（`dsh-base` + 自有 bundle）。

- 不使用 `dsh-web-app`，不嵌入浏览器。
- 默认协议：自定义 channel（`mygo` profile）；SDK 通道只作显式回归。
- 首发平台：Windows x64；macOS 随后，Linux 视需要。

默认入口使用 `mygo` profile 的自定义 channel：校验 UI/Host/DSH 版本，显式创建会话，发送 prompt，显示 user/assistant/tool 事件与 running/idle 状态。原生助手正文支持 Markdown、只读高亮代码块与代码/源文复制；提供 Stop、审批与问答表单，保留下一条草稿。工具默认折叠，支持有界详情与只读 hunk；流式提交后不重复答案。Sessions 提供会话列表、新建、恢复、分页历史与草稿隔离；Settings 提供当前会话 provider/model/effort、持久默认模型和 DeepSeek 凭据配置。首发平台为 Windows（x64），发布包自带 Node + DSH runtime；macOS 随后。当前缺口、里程碑与验收见 [docs/roadmap.md](docs/roadmap.md)。

## 文档

| 文档 | 内容 |
| --- | --- |
| [docs/architecture.md](docs/architecture.md) | 进程、组合、协议、状态、生命周期 |
| [docs/roadmap.md](docs/roadmap.md) | 当前进度、缺口、里程碑与验收 |
| [LESSONS.md](LESSONS.md) | 开发过程中的经验教训 |
| [docs/code-standards.md](docs/code-standards.md) | 重复 3 次后固化的编码/文档规范 |
| [docs/subsystem/](docs/subsystem/) | Host 监督、通道、投影、交互、UI、打包、测试 |
| [docs/research/](docs/research/) | 立项调研与取舍 |
| [AGENTS.md](AGENTS.md) | 文档路由、硬规则、命令 |

## 开发

```sh
go tool mygo dev
go test ./...
go tool mygo build          # buildCommand 先构建 resources/runtime
node scripts/build-runtime.mjs
node scripts/smoke-packaged.mjs  # 打包产物 smoke（Windows）
```

原生窗口 + fake host（无需 DSH 或 API key）：

```sh
go run . -host-command node -host-arg testdata/fake-mygo-host.mjs
```

验证 Stop 按钮时额外传入 `-host-arg --wait-for-cancel`：第一回合会等待取消，下一回合恢复普通 Echo 回复。

验证审批时额外传入 `-host-arg --approval`：第一回合等待 Allow once / Deny / Stop turn；后续回合恢复 Echo，不写真实文件。

验证问答时额外传入 `-host-arg --question`（连续两次用 `--question-twice`）：展示单选、多选、自由文本问题；Submit answers 提交整批答案，Cancel question 或 Escape 取消提问，Stop turn 停止回合。

验证流式时额外传入 `-host-arg --stream`：第一回合先显示 `Live prefix`，再提交完整答案。`--stream-wait` 保持部分文本直到 Stop；`--no-stream` 模拟未声明能力的旧 Host。后续回合恢复 Echo。

验证工具卡片时额外传入 `-host-arg --tool-diff`：发送任意消息后展开 `Tool · edit`，点击 `View details`，切换 Arguments / Result / Changes 并逐个查看记录的 hunk。该演示不写真实文件。详情超限/缓存淘汰会明确提示，diff 不提供编辑或应用操作。

验证 Markdown 时额外传入 `-host-arg --markdown`：助手先流式显示标题和开放代码围栏，完成后可 `Copy code` / `Copy Markdown`，保留代码缩进与换行。`--markdown-wait` 可测试 Stop 后的部分源文复制。图片不自动加载，链接仅显式点击 HTTP(S) 才打开；用户和工具内容保持原文。截断或中断内容不会被当成完整代码复制。

验证会话列表时额外传入 `-host-arg --sessions`：点击 **Sessions**，选择 `Session demo-history` 回放已有正文；可继续发送，或 **New conversation** 后切回验证草稿隔离。fake 历史仅在当前 Host 内存中保存，不跨进程重启持久化。

验证模型设置时额外传入 `-host-arg --models -provider test -model echo`（可与 `--sessions` 组合）：空闲时点击 **Settings**，Model ID 填 `route-probe`，选择 effort 后 **Apply model**，再发送消息；回复会显示实际使用的测试路由。选择 Provider default 清除 effort，`bad` 模型演示错误反馈，不调用外部 API。

**Apply model** 只影响当前会话的下个回合；**Save selection as default** 将表单路由保存到 Host profile，当前会话不变。未显式传 `-provider` / `-model` / `-effort` 时，支持配置能力的 Host 在启动、新建、冷恢复时使用持久默认值；显式 flags 继续覆盖。切回已拥有会话保留其选择，旧 Host/SDK 保持原行为。

Settings 下半部提供 DeepSeek 凭据状态、密码输入 **Save key** 和需勾选确认的 **Clear managed key**；可能需要滚动。不会回读/展示 key，不做付费调用验证。保存由 DSH 管理本地凭据文件（不是加密系统保险库）；清除只移除管理值，环境或 `.env` fallback 可能继续生效，不等于撤销远端 key。写入失败时先 Refresh 确认再继续。详见 [设置与凭据](docs/subsystem/settings-credentials.md)。

无需真实密钥的完整配置体验：

```sh
go run . -host-command node -host-arg testdata/fake-mygo-host.mjs -host-arg --sessions -host-arg --models -host-arg --configuration
```

此 fixture 的默认模型和假凭据只存在当前 Host 内存；不要输入真实 key。真实 Host 则保存在同一 DSH_HOME/profile，重启后保留。

原生窗口 + 真实自定义 profile（Node 24+；DSH `0.2.1-rc.2`）：

```sh
# DSH checkout 先安装依赖并构建；DEEPSEEK_API_KEY 只从环境读取
go run . -dsh-dir D:/Workspace/deepseek-harness -dsh-home D:/Temp/dsh-mygo-dev
```

首启自动从 `resources/host` 创建 `mygo` profile；已有 profile 不会被覆盖。需要把 bundle 以 junction 链接进 profile 时，仍可运行 `node resources/host/setup-profile.mjs --dsh-home <目录>`。

不依赖 DSH checkout、直接验证 bundled runtime：

```sh
node scripts/build-runtime.mjs
go run . -dsh-home D:/Temp/dsh-mygo-runtime
```

真实历史保存在同一 `DSH_HOME`/profile 的 DSH 存储中。重新运行相同命令后会先创建新会话，再通过 **Sessions** 选择旧会话；冷恢复按上述默认/显式 flags 规则选择模型，使用历史记录中的工作目录。仅空闲时切换，各会话草稿只在本次 App 内存中保留。旧 Host/SDK 未协商 history 时隐藏入口。当前 Host 默认同时拥有最多 8 个会话，超限会提示失败，不静默删除历史。

无需 API key 的真实 Loader + mock 模型集成（PowerShell）：

```sh
node scripts/test-real-host.mjs D:/Workspace/deepseek-harness
node scripts/build-runtime.mjs
node scripts/test-real-host.mjs --runtime
go test ./internal/app -run TestAgainstMygoRealComposition -v -timeout 2m
go test ./internal/app -run TestAgainstMygoSessionSwitching -v -timeout 3m
go test ./internal/app -run TestAgainstMygoModelSettings -v -timeout 3m
go test ./internal/app -run TestAgainstMygoConfiguration -v -timeout 3m
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
