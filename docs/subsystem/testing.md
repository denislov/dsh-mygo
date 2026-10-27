# 测试

## 目标

测试证明三件事：

1. **投影正确**：同一段 session log 在 UI 里得到稳定、可解释的结果。
2. **协议可靠**：请求、通知、取消、非法帧都按契约处理。
3. **Host 真实可用**：不是只在 fake 上通过，而是能启动真实 DSH profile 完成回合。

## 测试层级

| 层 | 范围 | 工具 |
| --- | --- | --- |
| Go 单元 | 投影、状态机、编解码、交互队列、UI Model | `go test`、表驱动 |
| UI | 视图、输入、焦点、弹窗、快捷键 | `ui.NewTester` |
| 协议 | 帧、版本、取消、顺序、背压 | `resources/host/tests` + fixtures |
| Host 集成 | 真实 `dsh --profile mygo`、真实 session log | 进程测试 |
| DSH 组合 | Loader 启动 profile | 临时 `DSH_HOME` + 真实 CLI |
| E2E | 真实模型或 mock adapter | `DEEPSEEK_API_KEY` 可选 |
| Runtime | store、安装/下载、external、切换 | Go + Node 单元、真实归档 |
| 打包 | 安装、首启引导、runtime 资产、签名 | 平台 CI |

## Go 单元

- 投影：`user/message`、`assistant/message`（reasoning/text run 顺序、独立预算与截断）、`tool/call|result`、`turn/end`、重复 seq、未知事件、事件边界裁剪。
- 活动组：默认折叠、展开思考原文与工具卡片、流式 `Thinking…`、工具-only 摘要、回合断组、唯一 key、状态清理与中英文摘要。
- 视觉：token 对比度、明暗主题、宽/窄/最小窗口；无头 Tester 截图复核后删除临时脚本，不引入 golden 图片依赖。
- 流式：start/chunk/end、缺口、abandoned、提交前后交错、reasoning 可见且受预算截断、工具参数不泄露。
- 历史：分页、边界、游标、部分页失败不发布、历史与实时合并。
- 协议：合法/非法帧、版本、未知方法、超时、取消、重复回答、背压。
- 交互：审批/提问的载荷校验、FIFO、上限、过期身份、表单草稿。
- 工作区：目录/路径/符号链接越界、文件与输出预算、Git 只读变更、项目切换丢弃迟到文件结果、虚拟文件列表、面板开关/窄窗口标签/最大化、关闭图标避让系统按钮；rail 独立停靠/浮层、响应式切换不丢状态、不改变工具布局、浮层关闭与模态恢复；无常驻线的横纵拖动热区、悬停短 bar、方向光标与键盘调整；设置即时保存与持久化。原生 PTY smoke 使用 `DSH_MYGO_NATIVE_TERMINAL=1 go test ./internal/workspace -run TestNativeTerminalShellRoundTrip`，需要 libghostty-vt，普通测试不触发下载。
- 会话/设置：摘要校验、切换门禁、草稿隔离、模型确认事件、配置 revision 与写入屏障。
- 恢复/日志：seq 缺口进入 resyncing、恢复对话框状态、崩溃报告命名/脱敏、应用日志落盘。
- 会话管理：rename/archive/delete/fork 的参数、事件、列表更新与冲突。
- 恢复：seq 缺口 resync、baseline 重连、resync 失败时的停止路径。
- 通用设置与偏好：命名空间白名单、secret 排除、UI 偏好 schema/原子写/损坏回退。
- 运行时：manifest/catalog 严格解析、状态损坏、归档路径/链接/上限、原子安装、SHA/下载取消、external `.cmd`/shim 修复、legacy 候选、事务切换与回滚。
- 纯函数优先；I/O 用接口注入。

### 命令

```sh
go test ./...
go test -race ./...
CGO_ENABLED=0 go test ./...
go tool mygo vet .
```

`go tool mygo vet` 检查存储的构建值与 goroutine 捕获；UI 改动必须跑。

## UI 测试

`ui.NewTester(view, width, height)` 驱动视图：

- 点击、输入、快捷键、焦点、弹窗、列表滚动与最小窗口。
- 断言 `HasText` / `Texts` 与可见状态，不断言像素。
- 用 fake `ChannelClient` 或直接构造 `view.Model` 提供固定状态。
- 覆盖：transcript 行、Enter/Shift+Enter、停止生命周期、审批/提问表单、Markdown 与复制、工具折叠/检视器、Sessions/Settings 弹窗、错误横幅、配置密码清空。
- 目标态 UI：侧栏、搜索、重命名/归档/删除/fork、附件与 `@`、设置分区、主题/语言、resync 指示与更新提示。
- 一帧可能构建多次；测试覆盖元素消失/返回、控件身份变化、弹窗关闭后的焦点回收。

真实窗口检查在 Windows 与 macOS 上执行；Windows 是首发门槛。

## 协议 conformance

```text
tests/fixtures/protocol/
  invalid-frames.json
  capabilities.json
  initialize.json
  session.json
  prompt.json
  history.json
  model.json
  configuration.json
  permission.json
  interaction.json
  management.json
  notifications.json
```

Go channel 与 Node transport 共用非法帧 fixture。Go protocol 与 Node server 共用能力 fixture，固定客户端声明的名称/顺序以及基础服务、历史服务的协商子集；单项能力、独立 reasoning baseline、未知名称与重复声明分别验证，解析不能代替握手载荷校验。

initialize fixture 固定 custom 请求与 Host 响应，由 Go 载荷编码/解码和 Node handler 共用；Go 侧同时验证 SDK 请求的 effort 省略与显式空字符串。编码比较使用解析后的 JSON 对象，字段排列不作为协议语义；channel 回归另行固定缺失/null/空值、版本回退、字段类型与大小边界的既有接受范围。

session fixture 固定 create/resume 的 Host 默认与显式路由、空 effort 省略和完整摘要字段，由 Go 载荷编码/摘要解码与 Node create/resume handler 共用。双方按 JSON 对象比较；`useDefaultModel` 与显式 provider/model 混用必须被拒绝，摘要的必填字符串、boolean、时间与 nextSeq 不能以零值代替存在性校验。

prompt fixture 固定 custom 文本、文本与图片、仅图片、SDK `contentBlocks`、投递确认及取消结果。Go 分别验证文本块、带名/无名图片块和取消结果 `cancelled` 的存在性；Node 用同一 fixture 驱动 prompt、cancel 与图片 admission。图片顺序与 canonical base64 仍由 Host 边界校验。

history fixture 固定目录分页、forward、live、backward `beforeSeq`/cursor、stream baseline 与 release 请求，以及 list/history 结果信封。Go 按读取流程比较字段子集并校验结果指针；Node 用同一 fixture 驱动 list、forward history 与 release。方向、固定 cut、跨页 cursor、单页预算和跨会话拒绝仍由各 flow/Host 边界测试覆盖。

model/configuration/permission fixture 固定模型目录与选择、默认模型、凭据 set/unset、权限快照与 select 请求。Go 校验空/显式 effort、revision、凭据 value 仅 set 以及 expectedNextSeq/confirmDangerous；Node 用同一 fixture 驱动 list/select、默认/凭据写入与权限读取/危险确认。凭据 secret 只出现在 set 请求，RPC 返回后由请求对象清空。

interaction fixture 固定 approval respond/closed、question respond/cancel/closed 与空 sessionId 的全局关闭。Go 校验 decision、答案空 selected/custom 与 closed sessionId 存在性；Node 用同一 fixture 驱动审批回答、提问回答与取消。FIFO、重复/迟到回答和表单草稿仍由 interaction/app 回归覆盖。

management fixture 固定 rename/archive（含默认归档与取消归档）、subagent list/history 请求、目录/历史结果信封及 start/finish 状态。Go 校验归档 boolean 省略、目录 label/createdAt 与历史边界字段的存在性；Node 用同一 fixture 驱动 rename、归档/取消归档与子代理目录/分页。两类地址校验、提交事实确认、直接父子关联、fork 前缀、页预算与句柄释放仍由各 flow/Host 回归覆盖。

notifications fixture 固定 session/event、session/status、assistant-stream 外层与三个已消费错误 kind。Go 校验 event 外层 seq、嵌套事件 seq 存在性、status revision 与流帧原始对象，错误 kind 对缺失/畸形载荷返回空；Node 用同一 fixture 驱动事件、状态与流式通知。方法/通知名称、身份/序列校验、未知事件与非法帧仍由 app/projection 回归覆盖。

`resources/host/tests/protocol.test.mjs` 覆盖握手门槛、版本/能力协商、非法 JSON/帧、超长输入、未终止 EOF、输出背压、请求串行、shutdown 与错误信息隔离；`history.test.mjs`、`models.test.mjs`、`configuration.test.mjs`、`approval.test.mjs`、`question.test.mjs`、`subagent-inspection.test.mjs` 分别覆盖对应子系统的边界与预算。目标态还需覆盖 `$/cancel`、附件帧、`session/delete|fork` 与 resync。

## Fake Host

`testdata/fake-mygo-host.mjs` 复用正式 `MygoServer` 与 `LineChannel`，替换 DSH 服务，支持：

- 显式创建会话、连续回合、工具投影、错误版本与无效 session 响应。
- `--wait-for-cancel`：第一回合保持运行以验证 Stop；`--no-cancel` 模拟旧 Host。
- `--approval`、`--question` / `--question-twice`：审批与提问表单。
- `--stream` / `--stream-wait` / `--no-stream`：流式、取消流式与旧 Host 仅提交模式；`--reasoning` 在提交与流式内容中加入 reasoning 块；`--sessions --stream-wait --stream-gap` 丢弃文本块起始帧，验证仅流式缺口的 baseline 恢复与取消。
- `--tool-diff`、`--markdown` / `--markdown-wait`：工具详情与 Markdown 渲染。
- `--sessions`、`--models`、`--configuration`：会话、模型与配置面板。
- `--permissions` 配合 `--sessions` 提供权限读取和切换；`--permission-full-start` 仅在 fake Host 预置完全访问初值，供原生确认门禁和幂等保存检查，不修改用户 DSH 数据。
- `--bad-version`、`--bad-session` 等错误注入。

`testdata/fake-sdk-host.mjs` 是最小 SDK 协议 host，用于显式回归模式。fake host 只验证 UI/协议适配，不验证真实模型行为。

## Host 进程回收

```sh
go test ./internal/host -run 'TestStopKillsProcessTree|TestHostExitKillsProcessTree' -v
```

Windows 上，helper Host 在收到指令后再派生一个长期运行的 helper 子进程；测试分别验证 Stop 杀死 Host、Host 自行退出两种路径都会让 Job Object 回收子进程。非 Windows 上这两个测试不编译，进程组回收由 POSIX 路径覆盖。

## Host 集成

真实 Loader 测试使用临时 `DSH_HOME`，不会写入用户数据：

```sh
node scripts/test-real-host.mjs D:/Workspace/deepseek-harness
node scripts/build-runtime.mjs
node scripts/test-real-host.mjs --runtime
node --test resources/host/tests/*.test.mjs
```

脚本把 `DSH_MYGO_DSH_DIR` 固定到指定 checkout，或把 `DSH_MYGO_RUNTIME_DIR` 指向已构建的 `runtime/`（`--runtime`）以跑 managed 入口；两者都跑 `TestAgainstMygo|TestMygoReal` 组。`DSH_MYGO_RESOURCES_DIR` 保留给旧 bundle 布局。环境变量都不设置时，真实组合组自动 skip；报告结果时要区分"跑了"与"跳过"。

测试用 `host.EnsureProfile` 在临时 `DSH_HOME` 里创建 mygo profile，和发布首启走同一条 Go 路径。

覆盖范围：

- `TestAgainstMygoRealComposition`：真实 CLI/Loader、profile、stdout 纯净、连续 seq、退出码。
- `TestMygoRealProtocolLifecycle`：错误版本、非法帧、initialize 前 shutdown、stdin EOF。
- `TestAgainstMygoRealCancellation` / `TestAgainstMygoRealApprovals` / `TestAgainstMygoQuestions`：真实 agent loop 的取消、审批与提问。
- `TestAgainstMygoRealStreaming`：mock adapter 暂停生成，验证流式先可见、reasoning 与 text 分列、提交替换、取消与同会话继续。
- `TestMygoRealHistoryAfterRestart`：两个进程共享同一 `DSH_HOME`，验证列表、恢复、分页回放与继续发送。
- `TestMygoRealHistoryAfterRestart` 同时覆盖重命名提交、冷列表标题/更新时间、单写者冲突后重试、归档跨进程重启保留、取消归档及完整日志前缀不变；全程使用 mock provider，不调用付费模型。
- `TestAgainstMygoSessionSwitching`、`TestAgainstMygoModelSettings`、`TestAgainstMygoConfiguration`、`TestAgainstMygoToolDiffs`：真实会话切换、模型请求、配置持久化与 write/edit diff。
- `TestAgainstMygoSessionManagement`：fake/真实 Host 的目录启动加载、冷目标及当前会话重命名、提交确认、归档/恢复、草稿隔离与继续发送；真实组还覆盖归档当前会话后 kill/Restart、新建会话和恢复原草稿。Tester 覆盖侧栏/弹窗切换、标题过滤、归档筛选与表单身份，Windows UI Automation 验证原生按钮和输入流程。
- `TestAgainstMygoResync`：fake Host 与真实 Host 传输过滤器丢弃持久通知或文本块起始帧，不修改日志；验证同一 Host/会话自动补齐、用户消息唯一、流式提交不重复、草稿保留、后续回合、审批/提问继续。baseline-complete/cancel 在真实模型 adapter 暂停时验证无需新 start 就恢复文本，并续流或取消。`resync_test.go`、`stream_baseline_test.go` 覆盖状态基线、流式续接/重放、分页变化、连续性重试上限、溢出、失败后迟到结果及取消确认屏障；Node 历史与 baseline 测试覆盖同步捕获、固定分页、能力隔离、资源限制、reasoning 能力门控与工具参数排除。
- `TestAgainstMygoRestart`：完成一个回合后 kill Host，等待恢复对话框，Restart 后 resume 同一会话、回放 transcript 并继续发送。
- `TestAgainstMygoLocalEditingState`：三个真实 Host 实例共享临时 DSH_HOME 与本机编辑文件，验证两会话草稿、最近会话自动恢复、确认发送后只清除对应草稿，以及历史仍来自 Host。单元测试覆盖无效最近会话回退/显式复制、未确认提交与后续编辑、退出最终保存及失败不关闭。
- `TestAgainstMygoLargeHistoryAndReleasedSessions`：通过公开 persistence 写入 35001 条合法标题事件并确认物理大小超过 32 MiB；真实 Agent 冷恢复、尾部窗口、更早页、返回最新、连续 12 次新建、释放后恢复/继续回合和第二 Host 的目录检查点/持久尾部。fixture 不读写私有 JSONL 布局，不调用付费模型。
- 历史窗口单元与 Tester：非法/变化边界、连续 seq、超过旧事件上限、页字节预算、方向/能力/游标绑定、跨页工具配对、去重区间、4096 行/16 MiB 展示缓存和 4 MiB 详情预算、旧窗口禁发、读失败保留草稿、失败释放重试及淘汰控件缓冲。Windows 原生窗口用 25001 条可见消息验证尾页/加载/返回、草稿与宽窄布局。
- `internal/preferences`：严格 schema/UTF-8/上限、scope 隔离、完整损坏备份、新版本保护、原子替换、OS 文件锁和内容冲突、失败后重试，以及合并保存/不可变快照。Tester 验证原生绑定回调、关闭期间禁用编辑、最小窗口保存错误动作及 Host 恢复对话框的放弃入口。
- 界面偏好：v1→v2 草稿迁移、主题/语言/发送键/命令枚举、重复快捷键、非法偏好保留文件、保存失败保持当前编辑。App 验证 locale 在 UI 队列执行前供握手读取；真实 LocalEditingState 回归同时验证主题/语言/发送键跨重启恢复。词表检查静态 UI 翻译与格式参数一致，Tester 验证稳定选项身份、中英文数据隔离、MyGo 内建控件词表随 locale 链接（代码复制按钮）、主题帧切换、Ctrl+Enter、命令改绑/禁用/忙碌/模态门禁。
- ViewModel 区域与动作：`model_actions_test.go` 验证 Composer/Session/Settings 动作可独立设置、缺席不暗示能力；`build_test.go` 验证普通构建不执行发送/取消/切换/保存，建议的草稿持久化只在构建结束后执行一次，且源码不使用 `Context.Memo`；`tokens_test.go` 固定控制/图标/header/附件缩略图的语义尺寸；`localization_test.go` 验证具名应用消息在英文与中文渲染、技术详情保持原文。`internal/i18n` 强制所有具名 key 有英文与中文、格式参数一致。
- 附件与引用：`internal/attachment` 覆盖握手策略校验（媒体类型/上限子集）、批预算（数量/单图/总量/base64）与显示名清洗；`internal/channel` 拒绝能力与策略不一致的握手；Node `attachments.test.mjs` 覆盖能力协商、canonical base64、超限、模型输入模态拒绝、有序提交、失败重置与不回传 payload；`projection_attachments_test.go` 覆盖图片元数据行、纯图片消息与畸形引用丢弃；App 测试覆盖暂存图片的请求载荷、无策略/超限拒绝且保留草稿、拖放图片与 `@path` 引用、候选目录排序。Tester 覆盖 `transfer.PNG` 剪贴板粘贴、`DropFiles` 路径、chips 移除、纯图片发送、附件错误与选择按钮、`@` 补全的候选/Tab/Enter/Escape；fake 与真实 Host 各有一条图片全链路（事件前持久化、规范化元数据、历史只带引用）。Windows 原生窗口验证拖放与粘贴后的焦点、滚动与截图。
- 深度信息（usage/context）：`projection_usage_test.go` 覆盖文本/工具-only 步骤的 usage、非法计数丢弃、`request/context` 路由容量与重复事件、畸形载荷上界；`usage_test.go` 覆盖 token 缩写、容量与用量组合、纯容量/纯用量、空指标隐藏、中英文标签与 header 行；App 的 fake Host `--usage` 全链路断言最新一步 1200/350 与容量 128000；真实 Host 用 mock adapter 断言 `mygo-mock/echo` 路由、1/1 token 与未知容量保持 0。
- 深度信息（plan/todo/goal）：`projection_depth_test.go` 覆盖整表替换、`turn/start` 清空、非法行/状态/上限、计划模式切换、目标快照/暂停/清除与非法目标丢弃；`depth_test.go` 覆盖清单进度、最多 8 行与 `+N more`、空清单隐藏、计划模式/目标中英文摘要；fake Host `--depth` 全链路断言清单、计划模式与目标；真实 Host 用 mock adapter 调用 `todo_write` 工具，验证 `todo/write` 事件端到端投影。
- 深度信息（子代理）：subagents_test.go 覆盖当前会话过滤、start/finish 状态、未知 reason 归一、非法 id/载荷丢弃与 8 条上界；subagents_test.go（view）覆盖运行/完成计数与中英文摘要、空状态隐藏；fake Host --subagents 验证 Host 转发的 start/end 通知到 header 行；真实 Host 用 mock adapter 调用 subagent 工具，验证本地子代理启动、结算 reason 与状态清理。
- 子代理详情：Node `subagent-inspection.test.mjs` 验证独立能力协商、所属父/直接子授权、fork 前缀隔离、连续分页、隐藏配置/reasoning 过滤、目录/页预算、观察句柄释放、父读取预占与退出中止、依赖错误脱敏。Go `subagent_inspection_test.go` 验证严格解码、运行中打开、迟到响应隔离和失败保持草稿；View Tester 验证原生列表/详情、Markdown 精确复制、早页/尾页、提问优先、旧 Host 隐藏与最小窗口按钮。`TestAgainstMygoRealSubagentStatus` 在真实 DSH 用 mock adapter 验证提交输出与结束原因，并用第二 Host 冷恢复父会话后读取持久子代理结果；不恢复子 Agent、不调用付费模型。Windows 原生窗口检查运行中详情、停止后刷新结果、宽窄布局与关闭返回。
- 回合与 Host 生命周期：`turn_test.go` 表驱动提交/认领/idle/取消/idle-while-cancel/超时/reset/resync 恢复；`host_test.go` 覆盖旧代通知/失败、重复与慢 stop、关闭后安装、通知提交边界与首次失败保持；`operation_test.go` 覆盖版本令牌与取消；`workspace_test.go` 覆盖独立目录读取不被单点失败取消；现有 stream/resync 测试继续覆盖有序批次、单次投影、seq/revision 与取消屏障。真实 Restart/resync/取消/审批/提问/会话管理与子代理冷恢复同时回归。
- 组件生命周期：`settings_component_test.go`、`session_component_test.go`、`history_component_test.go`、`local_component_test.go`、`runtime_component_test.go`、`subagent_component_test.go`、`workspace_component_test.go`、`attachment_component_test.go` 分别覆盖目标会话/换代失效、目录缓冲与退休句柄、resync cut/revision、草稿与退出单飞、运行时操作代际与切换阶段、子代理父身份/读取令牌/分页前缀、项目代际与面板请求、附件暂存/预览身份/补全代际。`App` 的 HostReady、SessionChanged、ResyncCommitted 与 RequestQuit 顺序由 `TestAgainstMygo*` 全链路和会话切换/重同步/退出测试共同约束；组件不持有 App，跨域依赖通过窄 provider 或显式参数传入。
- 入口与工具：`main_test.go` 表驱动 launch mode 优先级、profile/session 校验、重复 `-host-arg` 与显式空 `-effort`；`cmd/metrics` 测试只统计生产 AST 并排除生成/测试文件；`scripts/check-repository.test.mjs` 覆盖生成物/私钥/运行时 payload 拒绝；`internal/{view,projection}/imports_test.go` 固定依赖方向。
- `TestAgainstMygoPermissions`：fake/真实 Host 的初始化快照、会话权限切换、草稿保持、新建默认不变和切回；真实组 kill/Restart 后冷恢复确认权限持久化。`TestAgainstMygoPermissionEnforcement` 以真实 approval-probe 对比 ask 弹出审批与 never 拒绝显式审批，均不执行被拒绝的临时标记写入。
- 权限单元与 Tester：白名单载荷/实际 knobs/危险确认/seq 冲突/写入屏障/失败刷新/旧 Host 隐藏/独立于模型能力/最小窗口；快捷模式点选即提交、当前勾选态仅随 Host 确认改变、忙碌/不确定时禁止重复写入。完整设置页验证确认框门禁；快捷列表验证精简结构与说明，Windows 原生窗口检查宽窄布局，选项切换使用 Tester 与真实 Host 验证。
- `node scripts/test-real-host.mjs --runtime`：同一组测试改用 `runtime/` 的 Node、DSH 闭包与私有入口；用于打包前后检查运行时闭包。
- 首次使用：`first_use_test.go` 覆盖文件夹路径校验、取消/失败/跨 generation 迟到结果、草稿与新会话边界、连接读取不依赖模型目录且不清除模型不确定状态；View Tester 验证目录/凭据禁发、精简密码表单和最小窗口。`TestAgainstMygoFirstUse` 使用 fake 与真实 mock Host 验证确认目录、新项目会话、配置读取与第一轮消息，第二 Host 从持久数据恢复项目目录和对话。入口测试验证安装版默认主目录、显式 cwd 优先与无 bundled runtime 的开发目录。
- 发布资产：`scripts/release.test.mjs` 验证应用输出白名单、每个附件 SHA256、排除 PFX/密码/.env、版本身份冲突、拒绝内嵌 runtime 与未知输出；`scripts/prepare-runtime-release.test.mjs` 验证 runtime catalog/归档/兼容范围与校验和。打包/安装 smoke 验证安装器不含 runtime、首启引导、显式 runtime 启动；上传后检查 release tag/commit、draft 状态、资产名称/大小/digest，不以本地产物存在代替线上可下载。
- 补丁重启回归：`runtime-bundle.test.mjs` 验证确定性归档、manifest 首项、隐藏文件与 `go.mod` 排除；真实 runtime 测试使用 bundled Node 实际导入 DSH。`profile_test.go` 覆盖应用私有 Host 的内容指纹同步（含同版本内容变化）、用户配置与旧副本保留、不完整升级失败保护、路径边界及兼容开发链接；`TestAgainstMygoProfileUpgradeAndSecondStart` 用真实 Host 从旧包升级后连续启动并恢复会话/草稿。签名脚本测试确保旧安装器不会影响本版选择。

需要真实模型与 key 的 `TestAgainstMygoRealModel` 默认跳过；运行前确认会产生 API 调用费用。

## 打包测试

- 打包目录 smoke：`node scripts/smoke-packaged.mjs` 验证 manifest/版本、打包资源真实回合（composition + restart）、干净 `DSH_HOME` 首启、session 与应用日志，并用同一 `DSH_HOME`/UI 状态连续启动确认恢复到同一会话；两个 smoke 脚本都通过 `-ui-state-dir` 隔离编辑文件，避免写入用户偏好。
- 安装包 smoke：窗口打开、Host ready、完成一个不调用模型的回合。
- 签名：Windows `signtool verify`；macOS `codesign --verify` 与公证。
- 更新：旧版本 → 新版本，会话/设置保留；delta 校验。
- 卸载保留 `$DSH_HOME`；断网、磁盘满、权限错误。
- 任务运行中更新被拒绝或确认后中断。

打包测试的命令与产物约定见 [打包与更新](packaging-updates.md)。

## 平台矩阵

| 平台 | 必测 |
| --- | --- |
| Windows x64（首发） | 窗口、输入法、弹窗、原生 UI Automation、安装器、签名、Host 回收 |
| macOS arm64/x64（随后） | 窗口、输入法、剪贴板、签名/公证、更新 |
| Linux | 不首发；如支持再补原生 UI 与安装包 |

DSH 的会话与设置跨平台共享；测试不要写入用户真实 `$DSH_HOME`。

## 确定性与并发

- 测试不依赖 wall clock；用事件等待或 fake clock，不用固定延时猜时点。
- 并行测试各自拥有临时 `DSH_HOME`、端口与进程。
- 不在测试之间共享 profile 或 `node_modules`。
- 进程测试在 teardown 等待退出；泄漏进程视为失败。
- UI 测试不并行共享窗口；每个测试独立。

## CI 矩阵

| 阶段 | 命令 |
| --- | --- |
| Go | `go build ./...`、`go test ./...`、`go vet ./...`、`go tool mygo vet .` |
| UI | 同上（`ui.NewTester`）；`internal/view/imports_test.go` 守住依赖方向 |
| 协议 | `node --test resources/host/tests/*.test.mjs`、`go test ./internal/channel` |
| 依赖/生成物 | `go run ./cmd/metrics -top 10`、`node scripts/check-repository.mjs` |
| 真实 Host | 可选自托管 job：设置 `DSH_MYGO_DSH_DIR` 或 `DSH_MYGO_RESOURCES_DIR` 后跑真实组合组；托管 CI 暂不加入 |
| 打包 | 平台 runner 上构建、签名、安装 smoke（发布阶段） |

## 测试范围取舍

- 真实付费模型 E2E 需要 key，作为可选/自托管任务，不进默认流水线。
- macOS 真机验收与签名/公证在 macOS 发布门槛执行。
- 打包、签名与更新测试在平台 runner 上执行，作为发布门槛。
- 原生窗口的部分只读控件模式无法用 UI Automation 断言，相关选择/effort 由 Tester 与真实 Loader 覆盖。

## 不测试什么

- 不测试 Electron/Web 行为。
- 不测试 MyGo 自身控件；只测试我们的视图与状态。
- 不用快照测试像素；视觉验收在真实窗口完成。

## 相关文档

- [会话投影](session-projection.md)
- [通道协议](channel-protocol.md)
- [宿主监督](host-supervisor.md)
- [打包与更新](packaging-updates.md)
- [进度与路线](../roadmap.md)
