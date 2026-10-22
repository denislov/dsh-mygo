# dsh-mygo

DeepSeek Harness 的原生桌面客户端：MyGo 原生 UI + Node Host（`dsh-base` + 自有 bundle）。

- 不使用 `dsh-web-app`，不嵌入浏览器。
- 当前阶段：路线 A（SDK 验证）→ 路线 C（自定义 channel）。
- 目标平台：macOS、Windows；Linux 视后续需要。

自定义 channel、回合取消、工具审批、用户提问、Markdown 流式与工具折叠/diff 已落地：默认使用 `mygo` profile，校验 UI/Host/DSH 版本，显式创建会话、发送 prompt、显示提交后的 user/assistant/tool 事件与 running/idle 状态。原生助手正文支持 Markdown、只读高亮代码块与代码/源文复制；提供 Stop、审批与问答表单，保留下一条草稿。工具默认折叠，支持有界详情与只读 hunk；流式提交后不重复答案。SDK 链路保留作回归入口。Sessions 支持会话列表、新建、恢复、分页历史与草稿隔离；Settings 支持当前会话的 provider/model/effort，Host 确认后下个回合生效。timed/continued 提问、自动补齐实时缺口、通用设置/凭据编辑与打包尚未实现。

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

验证问答时额外传入 `-host-arg --question`（连续两次用 `--question-twice`）：展示单选、多选、自由文本问题；Submit answers 提交整批答案，Cancel question 或 Escape 取消提问，Stop turn 停止回合。

验证流式时额外传入 `-host-arg --stream`：第一回合先显示 `Live prefix`，再提交完整答案。`--stream-wait` 保持部分文本直到 Stop；`--no-stream` 模拟未声明能力的旧 Host。后续回合恢复 Echo。

验证工具卡片时额外传入 `-host-arg --tool-diff`：发送任意消息后展开 `Tool · edit`，点击 `View details`，切换 Arguments / Result / Changes 并逐个查看记录的 hunk。该演示不写真实文件。详情超限/缓存淘汰会明确提示，diff 不提供编辑或应用操作。

验证 Markdown 时额外传入 `-host-arg --markdown`：助手先流式显示标题和开放代码围栏，完成后可 `Copy code` / `Copy Markdown`，保留代码缩进与换行。`--markdown-wait` 可测试 Stop 后的部分源文复制。图片不自动加载，链接仅显式点击 HTTP(S) 才打开；用户和工具内容保持原文。截断或中断内容不会被当成完整代码复制。

验证会话列表时额外传入 `-host-arg --sessions`：点击 **Sessions**，选择 `Session demo-history` 回放已有正文；可继续发送，或 **New conversation** 后切回验证草稿隔离。fake 历史仅在当前 Host 内存中保存，不跨进程重启持久化。

验证模型设置时额外传入 `-host-arg --models -provider test -model echo`（可与 `--sessions` 组合）：空闲时点击 **Settings**，Model ID 填 `route-probe`，选择 effort 后 **Apply model**，再发送消息；回复会显示实际使用的测试路由。选择 Provider default 清除 effort，`bad` 模型演示错误反馈，不调用外部 API。

Settings 只影响当前已拥有会话的下个回合，不修改启动默认、profile 或 API key。新建及重启后的显式冷恢复仍使用启动参数；当前 App 内切回已拥有会话保留其选择。旧 Host/SDK 未协商模型能力时隐藏入口。API key 继续由 Host 的环境/凭据机制管理，面板不读取 `.env` 或展示 secret。

原生窗口 + 真实自定义 profile（Node 24+；DSH `0.2.1-rc.2`）：

```sh
# DSH checkout 先安装依赖并构建；DEEPSEEK_API_KEY 只从环境读取
node resources/host/setup-profile.mjs --dsh-home D:/Temp/dsh-mygo-dev
go run . -dsh-dir D:/Workspace/deepseek-harness -dsh-home D:/Temp/dsh-mygo-dev
```

初始化脚本只创建新的 `mygo` profile，将本仓库的 `resources/host` 链接为 bundle；已有 profile 不会被覆盖。`go tool mygo dev` 使用的 `DSH_HOME` 中也需要先初始化该 profile；这是开发入口，运行时打包留到后续阶段。

真实历史保存在同一 `DSH_HOME`/profile 的 DSH 存储中。重新运行相同命令后会先创建新会话，再通过 **Sessions** 选择旧会话；冷恢复使用启动模型和历史记录中的工作目录。仅空闲时切换，各会话草稿只在本次 App 内存中保留。旧 Host/SDK 未协商 history 时隐藏入口。当前 Host 默认同时拥有最多 8 个会话，超限会提示失败，不静默删除历史。

无需 API key 的真实 Loader + mock 模型集成（PowerShell）：

```powershell
$env:DSH_MYGO_DSH_DIR = 'D:/Workspace/deepseek-harness'
go test ./internal/app -run TestAgainstMygoRealComposition -v -timeout 2m
go test ./internal/app -run TestAgainstMygoSessionSwitching -v -timeout 3m
go test ./internal/app -run TestAgainstMygoModelSettings -v -timeout 3m
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
