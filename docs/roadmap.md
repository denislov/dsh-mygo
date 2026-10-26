# 开发进度与路线图

> 统计基线：2026-10-25（运行时拆分完成）；能力进度包含应用/runtime 拆分、首启运行时引导、设置中心 Runtime 分区与事务式切换，更新于 2026-10-25。设计文档（`docs/architecture.md` 与 `docs/subsystem/`）描述**目标态**；本页只记录当前进度、目标差距、里程碑与验收。实现契约仍以设计文档为准；更新进度时改这里，不把同一份状态复制到其他文档。

## 1. 结论

项目在约 11 小时内从零走到"核心聊天闭环全部跑通"：自定义 channel、流式、回合取消、审批、提问、历史/会话、模型与凭据设置均已落地，并有真实 DSH Loader（`0.2.1-rc.2`）集成测试覆盖。当前处于**阶段 C（Windows 私有测试版首发）**。

一句话：**应用与 Node+DSH runtime 已拆分；首启引导、运行时下载/导入/切换/移除与事务回滚可用，下一步是重新打包发布应用安装器与独立 runtime 资产并做干净机器验收。**

判断依据：协议与 UI 主链路已经闭环，M0 工程基线、自签名发布构建、手工崩溃恢复、会话管理、缺口恢复、权限 UI、草稿/界面偏好、附件与引用、有界历史窗口/句柄回收、运行时 store 与设置中心管理已完成；干净机器验收、运行时线上下载链路验证、多会话后台交互与 macOS 仍是主要缺口。

## 2. 事实快照

| 项 | 值 |
| --- | --- |
| 提交 | 拆分工作从 `9faa9b6` 起至 `06bf5dd`；2026-10-24 → 2026-10-25 |
| 规模 | `docs/` 21 篇；Go 生产/测试与 Node Host/测试继续增长；runtime lock 约 400 KB |
| 版本 | identifier `com.denislov.dshmygo`；app/host `0.1.1`、`protocolVersion 1`、DSH `0.2.1-rc.2`、`appCompatibility 0.1.1 <= app < 0.2.0`、MyGo `v0.6.2`、Node 24+、runtime Node `24.14.0` |
| 测试 | Go 506 项通过（CGO_ENABLED=0 同）；race projection/app/view 359 项；Node Host 86/86、scripts 10/10；`node scripts/test-real-host.mjs --runtime` 真实组合通过 |
| 构建产物 | 拆分后本机构建：应用 exe 14.5 MB、安装器 4.9 MB；runtime `.tgz` 212.2 MB（约 202 MiB）与 catalog；未发布新资产。线上仍为旧 v0.1.1 单体安装器 |
| 工程基线 | 公共 GitHub remote（2026-10-25 切换）；tag `v0.1.1` 指向 `728a3ed`；发布提交的 Windows 全量与 Linux Go CI 全绿 |

2026-10-23 本机（Windows）实跑结果：

```sh
go test ./...                                   # 506 项通过
go test -race ./internal/projection ./internal/app ./internal/view  # 359 项通过
CGO_ENABLED=0 go test ./...                     # 506 项通过
go tool mygo vet . && go vet ./...              # 通过
node --test resources/host/tests/*.test.mjs     # 86/86 通过
node --test scripts/*.test.mjs                  # 10/10 通过（bundle/发布/签名）
node scripts/build-runtime.mjs                  # runtime tgz + catalog
node scripts/test-real-host.mjs --runtime       # 真实 runtime 组合通过
go tool mygo build                              # 应用本体 14.5 MB + 安装器 4.9 MB
node scripts/prepare-release.mjs                # 应用资产，签名通过
node scripts/prepare-runtime-release.mjs        # runtime 资产
node scripts/smoke-packaged.mjs                 # 无 runtime 首启 + -runtime-dir + 进程树
node scripts/smoke-packaged.mjs                 # 解压发布 tgz 后重跑也通过
node scripts/smoke-installer.mjs                # 未运行：本机已安装 dsh-mygo，脚本按设计拒绝
```

真实链路覆盖：composition/lifecycle、approval、question、cancellation、streaming/Markdown、history 重启回放、session switching、model settings、configuration、host kill/restart、进程树回收、会话 resume 与运行时切换。

未跑：`smoke-installer`（本机已有真实安装，未卸载用户应用）、公开 GitHub runtime 下载链路（仓库已公开，链路未验证）、真实付费模型 `TestAgainstMygoRealModel`、macOS 全部验证、干净机器验收、应用自动更新。

## 3. 已完成（按子系统）

| 子系统 | 已落地 | 主要证据 |
| --- | --- | --- |
| 进程与协议 | spawn/stdio/stderr 尾、版本+能力协商、15 个方法、有界帧、通知背压、非法帧关闭 | `internal/host`、`internal/channel`、`resources/host/transport.mjs`、`tests/fixtures/protocol/` |
| 会话 | create/list/resume/history、`nextSeq` 起点、单写者、分页游标与预算、冷恢复 | `resources/host/server.mjs`、`internal/app/session_reader.go`、`TestMygoRealHistoryAfterRestart` |
| 会话管理后端 | rename、归档/取消归档、持久标题、真实活动时间、最近活动排序与归档过滤；独立 capability 与写入预占 | `resources/host/sessions.mjs`、`history.mjs`、`tests/history.test.mjs`、`TestMygoRealHistoryAfterRestart` |
| 流式 | `session/assistant-stream`、reasoning 临时行、提交替换、缺口/abandoned/截断；baseline 可按 capability 恢复 reasoning | `internal/projection/stream.go`、`stream-baseline.mjs`、`TestAgainstMygoRealStreaming` |
| 人机交互 | approval 与 question 的 answerer、FIFO 队列、超时/撤销/过期身份、多问题表单 | `internal/interaction`、`TestAgainstMygoRealApprovals`、`TestAgainstMygoQuestions` |
| 投影与渲染 | user/assistant/reasoning/tool/diff/notice、原生 Markdown 流式与代码复制、工具折叠与只读 hunk、默认折叠的活动组 | `internal/projection`、`internal/view/activity.go`、`markdown.go`、`tools.go` |
| 会话 UI | 原生双栏框架、自绘标题栏与应用菜单、可收起项目分组侧栏、按需展开的宽窗口详情面板、窄窗口 Sessions 弹窗与详情 Popover、标题搜索、归档筛选、重命名/归档/恢复；新建/刷新/恢复、草稿隔离与写入失败确认屏障 | `internal/view/chrome.go`、`sessions.go`、`session_groups.go`、`details.go`、`internal/app/session_management.go`、`TestAgainstMygoSessionManagement`、`TestAgainstMygoSessionSwitching` |
| 模型与设置 | `model.selection` 目录与 effort、`settings.configuration` 默认模型（revision）、DeepSeek 凭据只写 | `internal/app/settings.go`、`configuration.go`、`TestAgainstMygoConfiguration` |
| 会话权限 | 独立能力、当前预设和实际 sandbox/approval、危险 bundle 确认、Host 公共服务切换/持久化、失败刷新门禁 | `resources/host/permissions.mjs`、`internal/app/permissions.go`、`internal/view/permissions.go`、`TestAgainstMygoPermissions` |
| 本机编辑状态 | 按 DSH_HOME/profile 隔离的有界草稿与最近会话、原子保存/冲突检测/损坏备份、确认发送后清除、启动恢复/失败复制与异步退出保存 | `internal/preferences`、`internal/app/local_state.go`、`TestAgainstMygoLocalEditingState` |
| 界面偏好与设置中心 | 统一设置中心（General/Conversation model/Permissions/Connection/Shortcuts/About）、跟随系统/浅色/深色、中英文界面、Enter/Ctrl+Enter 与有限命令改绑/禁用、v1→v2 迁移与保存失败保护 | `internal/view/settings_center.go`、`settings.go`、`preferences.go`、`internal/i18n`、`internal/app/ui_preferences.go`、偏好/输入/重启回归 |
| 附件与引用 | 独立 `attachments` 能力与图片策略、canonical base64/类型/预算/路由校验、事件前持久提交、历史元数据行、粘贴/拖放/选择文件与 `@path` 补全 | `resources/host/attachments.mjs`、`internal/attachment`、`internal/app/attachments.go`、`internal/view/mentions.go`、`TestAgainstMygoRealAttachments` |
| 深度信息（usage/context、plan/todo/goal、子代理） | 最新一步 token 与路由容量、待办整表与回合清空、计划模式 latest-wins、目标快照/清除、子代理 start/end 实时状态、header 与 composer 上方有界摘要 | `internal/projection`（usage、`request/context`、`todo/write`、`plan/mode`、`goal/change`）、`internal/view/usage.go`、`internal/view/depth.go`、`internal/view/subagents.go`、`internal/app/subagents.go`、`resources/host/server.mjs`、`TestAgainstMygoRealUsageContext`、`TestAgainstMygoRealTodoProjection`、`TestAgainstMygoRealSubagentStatus` |
| 子代理详情/结果 | 工具栏只读目录、任务与状态、消息/工具输出/结束原因、早页/尾页/刷新、重启后持久读取、运行中查看与父任务停止 | `internal/app/subagent_inspection.go`、`internal/view/subagent_inspection.go`、`resources/host/subagents.mjs`、Node/Go/Tester 与真实 `TestAgainstMygoRealSubagentStatus` 冷暖读取 |
| 大历史与资源回收 | 反向尾页/更早页/返回最新、独立持久目录投影、缓存缺失降级、只读旧窗口和展示缓存上限、非当前句柄回收 | `resources/host/catalog.mjs`、`history.mjs`、`server.mjs`、`internal/app/history_window.go`、`TestAgainstMygoLargeHistoryAndReleasedSessions` |
| 回归入口 | 旧 SDK 链路保留为显式 `-sdk-protocol -profile sdk` | `main.go`、`testdata/fake-sdk-host.mjs` |
| 工程基线 | 正式 identifier、版本单点、Windows/Linux CI、构建产物重建 | `versions.json`、`internal/version`、`.github/workflows/ci.yml` |
| 运行时拆分 | 应用安装器只含本体；`runtime/` 生成 Node+DSH+`mygo-host`+Host bundle+manifest，确定性 `.tgz` 与 catalog | `runtime/`、`scripts/build-runtime.mjs`、`scripts/runtime-bundle.mjs`、`scripts/prepare-runtime-release.mjs` |
| 运行时管理 | device-local store、manifest/catalog 严格校验、安全解压、GitHub 下载、external/legacy/direct、事务式切换与失败回滚、external shim 修复 | `internal/runtimes/`、`internal/app/runtime.go`、`internal/host/launch.go` |
| 首启与运行时设置 | 全窗口引导（已安装 DSH / 下载 / 导入）、设置中心 Runtime 分区（Use/Remove/更新）、恢复对话框 Change runtime | `internal/view/runtime_setup.go`、`runtime_settings.go`、`settings_center.go`、`TestRuntimeSetup*`、`TestSwitchRuntime*` |
| 打包 smoke | 安装布局无 runtime、空 store 首启不启动 Host、`-runtime-dir` 启动与进程树 | `scripts/smoke-packaged.mjs` |
| 安装器 smoke | 静默安装、安装布局无 runtime、快捷方式/卸载键、签名、首次引导、显式 runtime 启动、静默卸载、`$DSH_HOME` 与会话保留 | `scripts/smoke-installer.mjs` |
| 进程回收 | Windows Job Object（kill-on-close）与 POSIX 进程组；Host 退出、Stop、应用崩溃/强杀都回收 Host 与插件子进程 | `internal/host/job_windows.go`、`internal/host/supervisor.go`、`TestStopKillsProcessTree`、`TestHostExitKillsProcessTree` |
| 崩溃恢复 | 恢复对话框、手工 Restart、`session/resume` + history 回放、`PathLogs` 日志与崩溃报告 | `internal/app/app.go`、`internal/app/crash.go`、`internal/view/recovery.go`、`TestAgainstMygoRestart` |
| 实时缺口恢复 | `session.resync`、固定历史前缀、statusRevision 与流式 revision/activeAttempt 基线、有界缓冲/最多 3 次重读；持久/流式缺口在原 Host/会话续接，失败降级恢复 | `resources/host/history.mjs`、`stream-baseline.mjs`、`internal/app/resync.go`、`TestAgainstMygoResync` |
| 发布签名 | 自签名证书生成与复用、MyGo `signCommand`、exe/安装器/卸载器签名、签名验证与 smoke 接入；runtime 使用 catalog SHA256 | `scripts/self-sign.mjs`、`scripts/sign-windows.mjs`、`scripts/verify-signatures.mjs`、`scripts/prepare-runtime-release.mjs` |

2026-10-25 界面调整：左侧导航贯穿窗口，右侧独立标题/工具栏；消息与输入区统一阅读边界，用户气泡靠右、复制动作放到气泡外侧，助手复制靠左；会话按项目目录分组，行操作仅 hover/focus 显示；输入区提供目录切换、权限快捷列表、可搜索的模型/effort 列表与上下文用量；连接配置统一到 Settings → Connection。快捷操作沿用 Host 确认和失败刷新屏障，不新增协议。 本轮验证：`go test ./...` 和 `CGO_ENABLED=0 go test ./...` 各 514 项通过（真实集成默认跳过）；`go test -race ./internal/app ./internal/view` 317 项通过；MyGo vet、Go vet 通过；显式设置 `DSH_MYGO_DSH_DIR` 后，真实 DSH 的模型/权限/首次使用回归通过。Windows 原生窗口复核中文消息、两组项目、输入区和工具栏排版；原生点击自动化未能确认状态变更，快捷交互由 Tester + fake/真实 Host 集成验证。未重建安装器，macOS 未验证。

2026-10-25 历史浏览交互调整：取消顶部文字分页按钮，向上接近顶部自动加载并保留阅读锚点；返回最新改为输入区上方圆形箭头，尾部仍在缓存时只本地滚动；切换会话的加载提示移到中间对话区，侧栏保持稳定。历史窗口复用冻结 cursor 补齐完整回合，提交正文预算扩大到 65536 rune，临时流和 baseline 维持 8000 rune。 验证：全量/纯 Go 各 521 项，projection/app/view race 374 项，MyGo vet 与 Go vet 通过；真实 DSH 完整长回合、大历史、resync、流式、模型回归通过，长回合由 mock 模型实际执行 70 次无副作用工具生成。Windows 原生预览复核宽/最小窗口的浮动箭头和中间加载状态，滚动锚点与请求去重由 Tester 验证；未重打安装器。

2026-10-25 历史加载闪烁修正：后台历史读取解除 `SessionBusy` 全局门禁，只使用 `History.Busy` 防重复；发送/会话设置取消旧读取并拒绝迟到结果，取消后同一边界可再次自动加载。全量/纯 Go 各 525 项、app/view race 327 项、MyGo vet 与 Go vet 通过；显式设置 `DSH_MYGO_DSH_DIR` 后，真实完整回合、大历史、会话切换与模型设置回归通过。Windows 原生预览对照确认旧状态使全局控件变暗，独立读取状态保持原有外观与启用状态；未重打安装器。

2026-10-25 原生工作区与设置重排：顶部改为 Session rail / Terminal / Changes / Preview / Files / More；面板可组合、调整分隔线、最大化，窄窗口用标签切换与返回对话。终端使用真实原生 PTY，隐藏保留，项目切换/退出回收；文件与变更采用根目录约束、有界读取和原生只读预览，网页 URL 由系统浏览器打开。设置使用搜索/分组导航、宽幅内容与右上角关闭，设备偏好逐项保存，现有提供商用卡片和内嵌模型表单展示；按用户范围不增加自定义提供商 CRUD。

本批验证：`go test ./...`、`CGO_ENABLED=0 go test ./...` 各 535 项；`go test -race ./internal/app ./internal/view ./internal/workspace` 337 项；显式设置 `DSH_MYGO_NATIVE_TERMINAL=1` 后，两项原生终端/PTY 生命周期竞态 smoke 通过；显式设置 `DSH_MYGO_DSH_DIR` 后，模型、配置、权限、本机编辑状态、会话切换和完整历史回合共 14 项真实 Host 回归通过。MyGo vet、Go vet、修改文档链接与差异检查通过。Windows 1280×760 / 560×420 原生复核设置、提供商卡片、模型编辑与终端/文件/预览组合；桌面点击注入未确认状态变化，交互由 Tester 与真实 Host/PTY 验证。安装器、macOS 未重验。末轮并行复验曾出现一次 fake 审批 Host 初始化超时；单独定向和纯 Go 全量重跑均通过。


2026-10-26 活动与权限交互精简：活动组中的每条思考可独立折叠，保留跨流式提交的展开身份；工具折叠行显示动作、命令说明/文件路径等有界摘要，成功状态不再占用独立 pill，运行与失败仍可见。横纵面板之间保持 13 DIP 间隔。权限快捷菜单改为模式名称、实际策略短说明与当前勾选态，点选即提交，沿用 Host 确认/seq 与失败刷新门禁；完整设置页保留确认框。

本批验证：全量/纯 Go 各 540 项、app/view race 339 项通过；真实 DSH 权限持久化与策略执行回归 6 项通过，快捷控件与 fake Host 的确认/草稿链路通过。MyGo vet、Go vet 与差异检查通过；Windows 原生预览复核四面板间隔、宽/最小窗口权限菜单与展开思考。原生点击注入仍未能确认状态变化，交互由 Tester 与 Host 集成验证。安装器与 macOS 未重验。

2026-10-26 面板拖动与 rail 响应式布局：移除工具面板之间及对话/工具区之间的常驻分割线，保留 13 DIP 留白，悬停/拖动/键盘焦点显示短 bar 并使用方向缩放光标。Session rail 独立于工具网格；顶部用竖条分组，以对话侧可用空间决定停靠或悬浮，浮层限制在对话区右上角，开关不改变工具比例。尺寸与弹层变化保留 rail 开关及章节状态。

本批验证：全量/纯 Go 各 546 项、view/app race 345 项通过；MyGo vet、Go vet、差异检查与应用构建通过。Tester 覆盖鼠标/键盘调整、悬停提示和光标、浮层位置/关闭、1920/1356/560 宽度往返与状态保留。Windows 原生复核 1480×860 的停靠布局（调整工具比例以适配检查机屏幕）、1356×900 的浮层与 560×420 的最小窗口；实际输入行为由 Tester 验证。未重打安装器，macOS 未验证。

2026-10-26 启动与发送边界首批重构：Restart 和 Runtime 切换在主线程捕获恢复模型路由，候选失败回滚沿用同一快照；初始化只接收显式参数及本次 launch 的期望身份，移除 App 共享 hostIdentity，worker 不再读取 ActiveRoute。App.Send 与 composer 共用业务就绪判断，传输和本次文本/附件校验保留在调用侧，后台历史读取不变成全局门禁。SDK 回归保留，wire 与协议版本不变。

本批验证：全量与纯 Go 各 579 项、app/view race 378 项通过；MyGo vet、Go vet、差异检查与应用构建通过。显式设置 DSH_MYGO_DSH_DIR，composition、Restart、模型设置、配置和会话切换相关 11 项（含子用例）通过；真实 Restart 在修改模型为 route-probe/high 后 kill/recover，确认同会话历史、流版本重置与后续模型输出。fake Runtime 切换/回滚同时验证非默认模型和 effort 保留。Windows 隔离 fake Host 原生窗口用 UI Automation 写入文本并点击 Send，确认空输入禁用、有内容启用、运行中显示 Stop、提交后恢复 Ready 和空输入禁用。键盘门禁与附件-only 由 Tester/Host 测试覆盖；安装器、macOS 未重验。

2026-10-26 能力解析重构：internal/protocol 集中完整 17 项能力名、逐请求独立的声明副本和已知能力值对象；握手校验后只解析一次，create/resume 共用该值，替代 initializedSession 的 14 个布尔字段。保留 reasoning baseline 独立能力、未知名称忽略、重复幂等及 SDK 零能力。共享能力 fixture 同时约束 Go 客户端声明与 Node 基础/历史 Host 协商子集；附件策略、身份和字段校验保持原样，wire/协议版本/Node handlers 不变。

本批验证：全量与纯 Go 各 605 项、protocol/channel/app/view race 446 项、Node Host 87 项通过；MyGo vet、Go vet、差异检查通过。显式设置 DSH_MYGO_DSH_DIR，composition、Restart、resync、流式、审批/提问、权限、附件、模型/配置、子代理和会话管理相关 46 项（含子用例）通过，使用 mock provider。protocol 生产代码无 import，保持无 UI/传输依赖。未重验原生窗口、安装器和 macOS；本批没有修改布局或输入绑定。下一批按 initialize、create/resume、prompt/cancel 分别推进载荷类型化，保留缺失/null/空字符串与大小边界。

2026-10-26 initialize 载荷类型化：internal/protocol 定义 custom/SDK 请求、客户端/服务端信息、响应和附件策略；App 改用具名载荷，channel.InitializeResult/AttachmentPolicy 保留类型别名及原校验。请求编码先以旧实现建立回归，再迁移类型；双端 initialize fixture 验证 custom 请求与 Node 响应。保持旧 Host 范围字段缺失/空/null 的精确版本回退、未知响应字段容忍、必填字段/类型/预算/附件与身份校验，以及 SDK 空 effort 省略。

本批验证：全量/纯 Go 各 646 项、protocol/channel/app/view race 487 项、Node Host 88 项通过；MyGo vet、Go vet、差异检查通过。显式设置 DSH_MYGO_DSH_DIR，真实 composition、Restart、协议生命周期、模型/配置、附件相关 15 项（含子用例）通过，使用 mock provider。protocol 仅依赖 attachment 纯 Go 规则，不依赖 channel/view/host。wire、协议版本、UI 和 Node handlers 不变；原生窗口、安装器与 macOS 未重验。后续继续 create/resume 及 prompt/cancel 载荷类型化。

## 4. 缺口

### 4.1 发布阻塞（没有它不能称为产品）

- **运行时发布验收**：应用/runtime 拆分、首启引导、设置管理、下载/导入、事务切换已实现；仍需用新脚本构建并跑通 app-only 安装器与独立 `.tgz`，确认公开 GitHub 下载链路与 catalog 更新。macOS runtime 产物未开始。
- **安装包与签名**：`go tool mygo build` 只打本体，自签名 Authenticode 已接入；runtime 使用 HTTPS + SHA256。仍无干净机器验收、macOS Developer ID/公证。
- **运行时安全后续**：暂无 detached signature、增量下载与镜像源；GitHub 不可达时只能本地导入。
- **恢复与诊断**：手工 Restart、持久历史回放、日志/崩溃报告与持久/流式缺口自动补齐已落地；日志轮转未做，自动重启退避/崩溃循环保护继续后续评估，维持用户触发 Host 重启的决策。
- **发布工程**：已有 v0.1.0 私有 prerelease、版本 tag 与 Windows/Linux CI；真实 Host 组合仍在开发机使用 bundled runtime 验证，干净 Windows VM 与 macOS 发布矩阵待补。

### 4.2 产品体验缺口（能用，但不像完整桌面客户端）

- **会话管理扩展**：侧栏、标题搜索、重命名/归档/恢复已接通；delete/fork 后置。独立目录检查点与退休句柄回收已接入，连续访问不再累积全部 Agent；目录仍限制 1000 条/256 KiB。冷检查点可能落后或缺失，会显示提示并在打开时由日志重建，尚无后台全库索引修复任务。
- **权限模式扩展**：当前会话展示/切换、危险确认和冷恢复已接通；未来会话默认权限编辑仍后置，UI 不更改部署环境。
- **UI 偏好扩展**：草稿、最近会话、主题、中英文与发送/新建/偏好/停止快捷键可配置并恢复；侧栏选择、搜索/复制等更广命令及任意按键录制后置。窗口尺寸/位置仍由 MyGo StateKey 处理。
- **附件与引用**：图片输入契约、粘贴/拖放/选择文件、`@path` 补全与历史引用展示已完成；暂存图片已有原生预览（单图 400 万像素上限，超限回退元数据）；`/` 命令与历史附件缩略图读取后置。
- **深度信息**：usage/context、plan/goal 与子代理实时计数在会话详情中查看，todo 在 composer 上方折叠展示；工具栏子代理入口可查看直接子任务的持久消息、工具输出与结束原因，手动刷新提交结果；compaction 与子代理 token 流继续扩展。
- **会话形态**：只支持单活跃会话；运行中会话不能 attach；后台会话的审批/提问不呈现。Host 同时拥有上限仍为 8，导航与冷目标管理会自动回收退休句柄；释放后重开使用冷恢复路由。
- **本地化扩展**：应用标签/常用提示使用集中双语词表，并同步 MyGo 内建控件；用户/工具/提问载荷保留原文，技术诊断允许英文回退。Host 采用加载后的 locale，运行中更改在下次 Host 启动生效；其它语言与系统语言自动匹配后置。

### 4.3 协议与健壮性缺口

- 同步边界：协商历史窗口后 resync 只重建尾部，旧展示可再加载；混合设置/模型/元数据写入、非法载荷、溢出或重读预算耗尽时走停止 Host + 手工恢复。旧 Host 的兼容全量回放仍受原预算约束。
- RPC 请求级取消（`$/cancel`），超时只能丢弃结果，不能撤销请求。
- 流式 baseline 已有独立能力和有界文本快照；运行中 attach 仍未开放，超出块上限的 attempt 等待新 start 或最终提交。
- 历史窗口与显式加载更早页已接入并突破 UI 全量回放预算；自动滚动触发、按消息/turn 定位仍后置。DSH Agent 冷恢复仍会重放完整日志，不承诺任意大小日志的 Host 内存和首启时延；单条事件的帧/页预算继续生效。
- `session/queue`、`session/wait`、`session/working-directory/*`、`settings/get|set`、任意命名空间设置。
- timed/continued 提问（当前 `tool-ask-user` 只有 `mode: legacy` 阻塞模式）。
- 大附件方案（16 MiB 单帧上限下的分片/路径引用）。

### 4.4 工程与测试缺口

- 并发/竞态只在本地跑过；无三平台矩阵；CI 不含真实 Host 组合（本地可用 source checkout 或 bundled runtime 跑）。
- macOS 零验收（Metal、输入法、字体、菜单、剪贴板、签名/公证）。
- 已有打包与安装/卸载 smoke；无干净机器验收、升级/更新测试。
- App 与 view.Model 仍聚合多个领域状态；启动快照、共享发送门禁、能力解析与 initialize 载荷已收敛，其余载荷类型、回合状态、Host 一代资源、设置/导航/历史和 View 子状态继续分批拆分。先明确所有权与时序护栏，不用字段数或行数代替验收。
- DSH 升级（rc 版本、精确 peer 依赖、公开 API 漂移）没有 runbook 和兼容矩阵。
- 遗留物：`cmd/spike`、SDK 兼容链路的退役时间未定。

## 5. 推荐路线

顺序原则：先让状态可复现（M0），再解决"装得上"（M1）；会话管理需要先补 Host 方法再做侧栏（M2）；可靠性与大历史必须在分发前处理（M3）；更新器依赖打包与签名，放最后（M4）。估时为单人全职粗估。

### M0 防回归与收口（已完成）

目标：让当前状态可复现、可发布追溯，再动大功能。

- 仓库与 CI：建立 `denislov/dsh-mygo` 远端，接 Windows 为主的 CI（`go build/test`、`go tool mygo vet .`、`node --test`）；macOS runner 随后加入；真实 Host job 作为可选/自托管任务。
- 版本与标识单点：正式 `identifier`、版本来源、DSH pin、`protocolVersion` 记录在一处。
- 构建 smoke：`go tool mygo build` + fake host 启动检查，重建陈旧产物。
- 真实链路脚本化：`scripts/test-real-host.mjs` 固定 `DSH_MYGO_DSH_DIR`；未设置时真实组自动 skip。

验收：干净 clone 一条命令全绿；CI 在远端可见；构建产物带版本信息；`build/` 与源码同步。

### M1 可安装（进行中，发布阻塞）

目标：不依赖开发机环境，装完即用。

- 运行时拆分（已完成）：应用只含 `host/` + 图标；`runtime/{node,node_modules,mygo-host.mjs,host,manifest.json}` 生成确定性 `.tgz` + catalog；managed/external/legacy/direct 选择、安装、切换与回滚可用；离线可导入本地 `.tgz`。profile 损坏修复仍待补。
- 打包 smoke：`scripts/smoke-packaged.mjs` 断言安装布局无 runtime、空 store 首启不启动 Host、`-runtime-dir` 启动与进程树回收。
- 安装器 smoke：`scripts/smoke-installer.mjs` 覆盖静默安装、无 runtime 布局、快捷方式/卸载键、签名、首次引导、显式 runtime 启动、静默卸载与 `$DSH_HOME`/会话保留。
- 安装与签名：Windows per-user 安装器 + 自签名 Authenticode；应用与 runtime 按 manifest 范围/身份组合。macOS 的 `.app`/`.dmg` + runtime 产物 + Developer ID + 公证放到 macOS 发布单元。
- 生命周期收口（部分完成）：恢复对话框、手工重启、Windows Job Object、`PathLogs` 日志与崩溃报告已落地；自动重启退避与版本不匹配提示仍待补。
- 验收：干净 Windows 机器安装 → 离线首启 → 完成一个 mock 回合；`kill` Host 可恢复；升级/卸载保留 `$DSH_HOME`。自签名安装器在干净机器上可能需要用户确认 SmartScreen 或预先导入公钥证书。

### M2 日常可用（1–2 周）

目标：像桌面客户端，而不是演示程序。

- 固定侧栏（已完成）：会话常驻列表、标题搜索、新建；宽度小于 820 时通过 Sessions 弹窗访问，搜索与表单状态共用。
- 会话管理（已完成）：Host 与原生 UI 的重命名、归档/恢复、更新时间和最近活动排序；当前归档保留正文/草稿并禁用发送，写入失败需刷新确认，delete/fork 后置。
- 权限模式 UI（已完成）：实际预设/sandbox/approval 展示与当前会话切换，危险 bundle 明确确认；Host 写入会话日志并等待落盘，失败必须刷新。
- UI 偏好持久化（已完成）：草稿/最近会话与主题、双语界面和有限快捷键配置，编辑文件 v1→v2 无损迁移，保存失败保留原文/旧文件；更多侧栏和命令偏好继续扩展。
- 附件与引用（已完成）：图片类型白名单、预算校验与 Host 事件前提交、粘贴/拖放/选择文件、`@path` 补全与引用展示已接通；`/` 命令留扩展，缩略图读取未实现。
- 深度信息（基础已完成）：usage/context、plan/todo/goal、子代理实时状态与持久历史/结果入口；compaction 与子代理 token 流作为后续扩展。

验收：560×420 到宽屏都可用；"找会话 → 改名 → 切模型 → 发图 → 审批/提问 → 停止"全流程通过；错误恢复不丢草稿。

### M3 健壮性与深度（1–2 周）

目标：长时间、大仓库、多会话下不丢真相。

- 自动 resync（已完成）：持久或流式序列缺口用 live history 固定前缀、状态/流式 baseline 与有界缓冲补齐，不重启 Host；版本化状态防止旧 idle 解锁，恢复流式文本不等待新 start。
- `$/cancel` 请求级取消与超时分级。
- 历史窗口（基础已完成）：最近尾页、显式更早页、返回最新、目录独立检查点和安全句柄回收；自动滚动加载与定位继续扩展。
- 运行中会话 attach、多会话后台活动与跨会话审批呈现。
- 通用 `settings/get|set`（命名空间白名单）、多 provider 凭据、账户登录。
- timed/continued 提问（依赖 DSH 侧 provider API 放开）。

验收：断点、大历史、后台会话场景不丢事件；协议 conformance 扩展并通过。

### M4 发布与生态（1–2 周）

目标：可持续发版与升级。

- Updater：清单/签名/delta/回滚；与 Host 空闲协调、任务中断提示。
- 单实例、深链（`dsh-mygo://session/<id>`）、托盘、菜单、通知。
- 发布流水线与签名验证；安装/升级/卸载 smoke；DSH 升级 runbook 与兼容矩阵。
- Windows x64 验收矩阵；macOS arm64/x64 作为后续发布单元；崩溃上报隐私评审。

验收：0.1 完成定义全部满足，升级失败可回滚。

## 6. 建议的下一批任务（PR 粒度）

界面已完成 Claude Desktop 风格重构：窗口使用 MyGo `TitleBarHidden` 自绘 44 DIP 标题栏（`c.TitleBar()` 系统按钮留白 + `DragWindow` + 原生应用菜单），宽窗口显示 260 DIP 日期分组侧栏与 300 DIP 右侧详情栏（窄窗口回退 Sessions 弹窗与详情 Popover），主区改为无角色行消息、`CollapsibleBase` 工具卡、`Progress` 任务卡与浮动 composer。Settings 与 Preferences 合并为带左侧分区的设置中心：General / Runtime / Conversation model / Permissions / Connection / Shortcuts / About 按能力出现；Runtime 为设备级分区，始终可用。视觉 token 集中在 `tokens.go`，浅深主题使用 Claude 暖灰 + 赤陶橙；动效复用 MyGo `Transition` / `ContentTransition` / `Spinner` / `CollapsibleBase` / `Progress`，没有新增动画引擎、Web UI 或框架依赖。侧栏/详情栏开关本轮仅内存保存，持久化留待 preferences schema 扩展。

本轮检查：`go test ./...`（420 项通过）、`go test -race ./internal/view ./internal/app`（283 项）、`go tool mygo vet .`、`go vet ./...`、`CGO_ENABLED=0 go test ./...` 均通过。真实 Windows 窗口（125% 缩放）通过 UI Automation 验收：1280×800 浅/深色会话、900×700 详情 Popover、560×420 composer 与设置中心、Sessions 弹窗、设置中心 General/Conversation model/Permissions/Connection/Shortcuts/About 全部分区；标题栏拖动位移 120×36 DIP、双击最大化/还原、系统最小化/最大化按钮均通过。验收中发现并修复标题栏标题区域未继承拖动属性、空状态建议被首次使用控件挡住、Shortcuts/About 标题重复。未覆盖 150% 缩放与 macOS 验收。

1. **私有测试版试用反馈**：围绕安装、选择项目、配置 API key 与第一轮真实对话收集反馈，优先修复首发阻塞和稳定性问题；后台会话、compaction、子代理 token 流和递归后代导航按实际使用反馈排序。
2. **M1-4 干净机器验收**：项目所有者因没有干净 Windows VM，决定本轮跳过并继续 M2；未验收，不标记通过。Windows 发布前重新安排离线首启与证书提示流程检查。

M2-1 会话管理后端已完成：重命名写入 `session/title` 并等待落盘；归档复用独立挂载的 DSH workspace registry，保留日志并支持取消归档。实际检查：`go test ./...`（235 项）、`go vet ./...`、Node Host/脚本回归（55/55）、`node scripts/test-real-host.mjs --runtime`、`git diff --check` 及修改文档的本地链接检查均通过。真实 `TestMygoRealHistoryAfterRestart` 验证标题/归档跨重启保留、取消归档和历史前缀不变；付费模型测试因没有 key 跳过。该批未重建安装器；侧栏与管理 UI 的验证见下述 M2-2。

M2-2 会话侧栏及管理 UI 已完成：UI 协商两个管理能力，宽窄布局共享筛选和表单；冷目标重命名不切走当前会话，当前标题以提交事件确认，失败后刷新目录并在必要时重放历史。Windows 原生窗口通过 UI Automation 验证重命名、归档、恢复及最小窗口弹窗，并检查宽/窄窗口截图。归档当前会话后的 Host 恢复创建新会话，原草稿按 id 留存；真实 Host 回归覆盖恢复归档会话后取回草稿。实际检查：`CGO_ENABLED=0 go test ./...`（242 项）、`go test -race ./internal/app ./internal/view`、`go tool mygo vet .`、`go vet ./...`、Node Host/脚本测试（55/55）、bundled runtime 完整真实组合及新增归档崩溃恢复定向测试、文档链接与 diff 检查均通过。修正了历史测试上下文早于 Host cleanup 取消造成的偶发关闭报错。本轮不重建安装器，不做干净 VM 或付费模型验收。

M3-1a 持久事件自动补齐已完成：运行中 history 通过新 capability 返回固定序列与状态版本，Go 后台重建投影并有序消费缓冲，保留草稿、审批/提问和取消确认。新增丢帧 fixture 验证同一 Host/会话续接、普通/流式结果不重复、审批/提问继续及下一回合；失败和上限覆盖于单元测试。实际检查：`CGO_ENABLED=0 go test ./...`（253 项）、`go test -race ./internal/app ./internal/projection ./internal/view`、MyGo vet、Go vet、Node 测试（58/58）和 bundled runtime 完整真实组合均通过。另用 Node `--env-file=.env` 仅在测试子进程中加载凭据，实际跑过 `TestAgainstMygoRealModel`，DeepSeek flash 返回精确 pong 并正常关闭；默认完整组合不加载该文件，付费组仍 skip。Windows 原生窗口注入丢帧后回到 Ready，显示唯一用户消息与答案，无 Host 重启，截图及 UI Automation 检查通过。本轮未重建安装器，干净 VM 验收继续按决定跳过。

M3-1b 流式 baseline 已完成：独立协商 `session.assistant-stream-baseline`，Host 从公开帧累计有界文本/块状态，live history 首页同步捕获持久/状态/流式边界并跨页固定；Go 恢复版本和 chunk offset、过滤旧帧，并对仅流式缺口执行同样的有界 resync。baseline 最多 128 KiB、128 块和 8000 rune，不携带 reasoning/工具参数，计入历史页和总预算。真实暂停 adapter 验证缺失文本块起始帧后，不等待新 start/提交就恢复前缀，并完成续流/取消及下一回合；Host 重启回归校验新 Agent 的版本空间重置。实际检查：`CGO_ENABLED=0 go test ./...`（264 项）、`go test -race ./internal/app ./internal/projection ./internal/view`（191 项）、MyGo vet、Go vet、Node Host/脚本（61/61）、bundled runtime 完整真实组合均通过；Windows 原生窗口注入流式缺口后显示临时前缀，Stop 返回 Ready，截图和 UI Automation 通过。本轮不调用付费模型，未读取或修改 `.env`，未重建安装器；干净 VM 验收继续跳过。

M2-3 会话权限 UI 已完成：独立协商 permission/get/select，显示实际预设/sandbox/approval，按目标 bundle 提供危险确认；Host 公开服务写入和落盘，seq 冲突或失败要求刷新确认，保留草稿并禁止未确认时发送/导航/下一写入。新建默认不受影响，切换会话和 Host 冷恢复保留权限。实际检查：`CGO_ENABLED=0 go test ./...`（272 项）、`go test -race ./internal/app ./internal/view ./internal/projection`（199 项）、MyGo vet、Go vet、Node Host/脚本（64/64）、bundled runtime 完整真实组合均通过。真实 approval-probe 验证 ask 提问与 never 拒绝显式审批，不把 never 误当自动批准。Windows 原生 UI Automation 验证预置完全访问时确认门禁、幂等保存、当前状态和宽窄窗口；下拉选项的直接自动化选择受 MyGo 原生接口限制，完整切换由 Tester 与真实 Host 覆盖。本轮不调用付费模型、不读取或修改 `.env`，不重建安装器；干净 VM 验收继续跳过。

M2-4a 草稿与最近会话持久化已完成：本机编辑文件按 DSH_HOME/profile 隔离，400 ms 合并保存并限制单份 64 KiB/总量 1 MiB/128 份；同目录替换、OS 锁、内容冲突和完整损坏备份保护旧状态，新版本文件不降级覆盖。发送在有效 messageId 确认前保留原文，失败与新编辑合并；启动恢复最近会话和草稿，失败新建并允许显式复制旧草稿。退出异步保存最后编辑，失败保留窗口，普通页面和 Host 恢复对话框都能显式放弃。实际验证包括真实 Host 三实例的两会话草稿、确认发送后清除、历史恢复；Windows 原生自动保存、重启恢复、最后编辑关闭、超限保存失败和显式放弃均通过。主题/语言/快捷键仍后置；安装器未重建、打包/安装 smoke 未重跑，两个脚本的 UI 状态目录已隔离；干净 VM 按决定继续跳过。

本批实际检查：`CGO_ENABLED=0 go test ./...`（290 项）、`go test -race ./internal/preferences ./internal/app ./internal/view`（193 项）、MyGo vet、Go vet、Node Host/脚本（64/64）、两个 smoke 脚本语法检查、bundled runtime 完整真实组合，以及最终 `TestAgainstMygoLocalEditingState` 的真实 Host 竞态验证均通过。另交叉编译 Linux preferences 测试二进制，未在 Linux 运行；macOS 未验证。本轮未加载 `.env` 或调用付费模型。

M3-2 大历史与句柄预算已完成：独立能力启用反向历史窗口与 session/release，打开/重启/resync 使用最近页；原生控制条可加载更早页和返回最新，跨页工具配对、阅读锚点与草稿保留。展示缓存有行数、预览与详情预算，旧窗口淘汰较新行后禁止发送，返回最新解锁；连续不可见事件使用区间去重。目录读取独立 mygoCatalog 持久检查点，缺失/损坏回退单会话提示并在打开时重建；冷缓存明确标记，不能声称总是最新。导航和后台重命名按退休集合回收句柄，忙碌/读取/交互/写入期间不能释放，失败保留可重试身份。真实测试通过 35001 条且物理大小超过 32 MiB 的日志、连续 12 次新建、释放后恢复/继续回合、第二 Host 的目录和历史恢复；Windows 原生检查通过 25001 条可见消息的宽窄窗口、加载/返回与草稿保留。本批不重建安装器，不跑安装 smoke；干净 VM 按决定继续跳过，不读取 `.env` 或调用付费模型。

本批实际检查：`CGO_ENABLED=0 go test ./...`（297 项）、`go test -race ./internal/app ./internal/view ./internal/projection`（218 项）、MyGo vet、Go vet、Node Host/脚本（70/70）、bundled runtime 完整真实组合均通过。最终补跑大会话与立即重命名重启两项真实 Host 竞态测试，覆盖超过 32 MiB 的 fixture、冷恢复检查点预占和公开后台活动检查；Windows 原生窗口检查及截图复核通过。文档本地链接与 diff 检查通过。安装器仍为先前构建，macOS 未验证。

思考显示与活动组已完成：投影层新增 `KindReasoning`，reasoning 在实时流、提交历史与历史回放中可见；同一回合内连续的 reasoning/tool 行聚合为默认折叠的活动组，摘要显示思考时长、工具数量或工具名，展开后显示思考原文与工具卡片。baseline 新增 `session.assistant-stream-baseline-reasoning` capability，未协商时保持旧快照形状；子代理历史继续剥离 reasoning。对话区与外壳统一视觉：活动组低对比面板、组内扁平工具行、阅读宽度 780 DIP/15.5 DIP 正文、空状态品牌圆标、设置内容卡片与 token 对比度测试。实际检查：`CGO_ENABLED=0 go test ./...`（506 项）、`go test -race ./internal/projection ./internal/app ./internal/view`（359 项）、MyGo vet、Go vet、Node Host（86/86）、脚本（10/10）、`node scripts/test-real-host.mjs --runtime` 完整真实组合通过。Windows 原生窗口复核暗色空状态与标题栏/详情布局；受窗口尺寸与自动化限制，未在真实窗口完成发送回合并展开活动组的复核，交互与展开态由 Tester 和无头明暗截图覆盖。本轮未重建安装器，macOS 未验证。

M2-4b 界面偏好已完成：独立设备级面板支持 system/light/dark、en-US/zh-CN、Enter/Ctrl+Enter 与新建/偏好/停止命令的有限改绑/禁用，重复组合拒绝；默认保持英文和 Enter。表单取消不改值，应用与重置不触碰 Host 配置，模态和审批/提问期间不执行背后命令。应用文案集中翻译，数据载荷保留原文；内建控件同步 locale，Host 在下一启动采用。v1 编辑文件迁移到包含 ui 的 v2，草稿/冲突/原子保存与退出屏障保持；非法偏好只读保护原文件。MyGo locale 镜像和窗口状态通过 ui-state-dir 一起隔离，编辑文件为偏好权威来源。下一项为附件与引用。本批未重建安装器，干净 VM 按决定继续跳过。

本批实际检查：`CGO_ENABLED=0 go test ./...`（309 项通过，36 项真实 Host/付费组跳过）、`go test -race ./internal/app ./internal/view ./internal/preferences ./internal/projection`（235 项）、`go tool mygo vet .`、`go vet ./...`、Node Host/脚本（70/70）、bundled runtime 完整真实组合均通过。补齐 MyGo `zh-Hans` 词表链接后，主程序不再输出“MyGo 自身文案仍是英文”的告警，代码复制等内建控件随界面语言切换；Windows 原生窗口复验 v1→v2 迁移、深色/中文/Ctrl+Enter 重启恢复、幂等应用、560×420 弹窗、恢复默认与浅色种子，截图复核通过。文档相对链接与 diff 检查通过。本批未重建安装器、未跑打包/安装 smoke；未读取 `.env` 或调用付费模型，macOS 未验证。

M2-5 附件与引用已完成：`attachments` 只在 Host 挂载附件存储且能校验模型模态时协商，`initialize` 返回媒体类型、base64/单图/总量/数量/像素/边长上限；`session/prompt` 接受 image 块（canonical base64、可选显示名），Host 先校验预算与当前路由模态，再经 DSH 附件服务在 user 事件前提交持久引用；任何失败不产生部分写入且不回传 payload。投影只保留 `attachmentId/mediaType/bytes/width/height/name`，畸形引用丢弃、纯图片消息保留一行；`model/list` 增加 sanitized `inputModalities`，未知模态放行、text-only 拒绝。Composer 支持 `transfer.PNG` 剪贴板粘贴、元素与窗口级文件拖放、原生图片选择、暂存 chips 移除与纯图片发送；非图片文件转为 `@path` 引用，`@` 补全用 TextSelection+Popover 列出会话工作目录候选并支持 Tab/Enter/Escape。App 发送前按协商上限校验并构造 text+image 块，成功后清空暂存、失败保留草稿与暂存图片；暂存图片不写入本地草稿文件，切换会话或重启清空。下一项为 M2-6 深度信息。

本批实际检查：`CGO_ENABLED=0 go test ./...`（346 项通过，37 项真实 Host/付费组跳过）、`go test -race ./internal/app ./internal/view ./internal/projection ./internal/attachment ./internal/channel ./internal/i18n`（297 项）、`go tool mygo vet .`、`go vet ./...`、Node Host/脚本（79/79）、bundled runtime 完整真实组合均通过。`TestAgainstMygoFakeHostAttachments` 覆盖 fake Host 全链路；`TestAgainstMygoRealAttachments` 用真实 DSH 附件存储提交 2×2 PNG，验证事件前持久化、规范化元数据与历史只带引用；Tester 覆盖粘贴、拖放、chips、纯图片发送与 `@` 补全。Windows 原生窗口用 UIA 复验 `@path` 补全（Popover 候选、点击插入 `@notes.txt`、发送与回显）并截图；同一构建中确认 Attach image 按钮与原生选择器可打开，但受当前会话剪贴板/UIA 输入注入限制，粘贴与文件选择未能完成端到端自动化，相关路径由 Tester 与真实 Host 覆盖。未重建安装器、未跑打包/安装 smoke；未读取 `.env` 或调用付费模型；macOS 未验证。

M2-6a usage/context 已完成：`assistant/message.usage` 的最新一步 token 计数（文本与工具-only 步骤均更新）和 `request/context` 的 `provider`/`model`/`contextWindow` 进入会话级指标，不新增正文行；负数或超界计数按缺失处理，未知容量/用量保持未知，不跨历史窗口求和、不写回日志、不改变 nextSeq。Header 增加单行摘要（`Context 已用 / 容量 · 百分比 · In / Out · cache`），Host 未提供事件时保持安静；指标行与中英文词表覆盖，Tester 验证组合/部分/空指标。fake Host 的 `--usage` 提供事件形状，真实 Host 用 mock adapter 端到端验证 `mygo-mock/echo` 路由与 1/1 token、未知容量保持 0。下一步为 plan/todo/goal 投影。

本批实际检查：`CGO_ENABLED=0 go test ./...`（356 项通过，38 项真实 Host/付费组跳过）、`go test -race ./internal/app ./internal/view ./internal/projection ./internal/attachment ./internal/channel ./internal/i18n`（307 项）、`go tool mygo vet .`、`go vet ./...`、Node Host/脚本（79/79）、bundled runtime 完整真实组合均通过。新增 `TestAgainstMygoFakeHostUsageAndContext` 与真实 `TestAgainstMygoRealUsageContext`；投影层覆盖文本/工具-only usage、非法计数、路由容量、重复事件与畸形载荷上界。本批未重建安装器、未跑打包/安装 smoke；未读取 `.env` 或调用付费模型；macOS 未验证。

M2-6b plan/todo/goal 已完成：`todo/write` 整表替换为 composer 上方清单（最多 8 行、`+N more`），下一 `turn/start` 按 DSH `todos` 折叠清空；`plan/mode` 为 latest-wins，`goal/change` 接受快照或 clear 墓碑；非法状态/空内容/超界计数丢弃，objective 单行 400 rune、清单行 200 rune、最多 64 行，不进入正文、不写回日志。Header 增加计划模式与目标摘要（objective、phase、rounds），无事件时不占位。fake Host `--depth` 覆盖事件形状，真实 Host 用 mock adapter 调用 `todo_write` 工具验证 `todo/write` 端到端投影。下一项为子代理运行状态。

本批实际检查：`CGO_ENABLED=0 go test ./...`（368 项通过，39 项真实 Host/付费组跳过）、`go test -race ./internal/app ./internal/view ./internal/projection ./internal/attachment ./internal/channel ./internal/i18n`（319 项）、`go tool mygo vet .`、`go vet ./...`、Node Host/脚本（79/79）、bundled runtime 完整真实组合均通过。新增 `TestAgainstMygoFakeHostDepthEvents`、真实 `TestAgainstMygoRealTodoProjection`；投影与 Tester 覆盖整表替换、回合清空、非法行/状态、计划模式切换、目标暂停/清除、清单边界与中英文摘要。本批未重建安装器、未跑打包/安装 smoke；未读取 `.env` 或调用付费模型；macOS 未验证。

M2-6c 子代理实时状态已完成：Host 监听 `subagent/start|end`，只在父会话由本 Host 持有时转发 `subagent/started|finished`，父/子 id 与 provider 经标识符白名单校验，`reason` 取 completed/aborted/error/max-tokens/refusal（未知归为 error）；start 时缓存 runId→父子身份，避免 child 结算后从 agent 注册表消失导致 end 丢失，缓存上限 256。App 只为当前会话维护最多 8 条状态，未知父会话/非法载荷丢弃，切换会话清空；header 增加子代理运行/完成计数行。fake Host `--subagents` 覆盖通知链路，真实 Host 用 mock adapter 调用 `subagent` 工具验证本地子代理启动、结算与状态清理。下一步为 M2-6 收尾（子代理历史/结果入口）或 M3。

本批实际检查：`CGO_ENABLED=0 go test ./...`（373 项通过，40 项真实 Host/付费组跳过）、`go test -race ./internal/app ./internal/view ./internal/projection ./internal/attachment ./internal/channel ./internal/i18n`（324 项）、`go tool mygo vet .`、`go vet ./...`、Node Host/脚本（79/79）、bundled runtime 完整真实组合（含 `TestAgainstMygoRealSubagentStatus`）均通过。新增 App 单测覆盖当前会话过滤、start/finish、未知 reason、非法 id/载荷与 8 条上界；View/Tester 覆盖计数与中英文摘要；fake Host 与真实 Host 各一条子代理通知链路。本批未重建安装器、未跑打包/安装 smoke；未读取 `.env` 或调用付费模型；macOS 未验证。

M2-6d 子代理详情/结果已完成：新增独立 `subagent.inspection` 与 list/history，只读查询所属父会话的直接子代理；通过 DSH 公开目录与观察服务读取运行中或冷子代理，不恢复子 Agent。历史过滤继承前缀、reasoning 与隐藏配置，连续分页最多 64 事件/256 KiB，读取参与父句柄预占与退出回收。工具栏原生 Modal 复用 MyGo List、Collapsible 和消息/Markdown，支持任务与状态、消息/工具输出/结束原因、早页/尾页/刷新、关闭与运行中停止父任务；审批/提问优先，迟到响应隔离，草稿保持。提交输出手动刷新，子代理 token 流、递归后代导航与 compaction 后续规划。

本批检查：`CGO_ENABLED=0 go test ./...`（384 项通过，真实 Host/付费组默认跳过）、`go test -race ./internal/app ./internal/view ./internal/projection`（292 项）、`go tool mygo vet .`、`go vet ./...`、Node Host/脚本（86/86）与 `node scripts/test-real-host.mjs --runtime` 完整真实组合通过。真实组覆盖子代理输出/结束原因以及第二 Host 冷恢复父会话后的持久读取；新增单元/Tester 覆盖授权、预算、隐藏载荷、关闭/切换、结束原因失效、提问优先、最小窗口和源文复制。Windows 原生窗口复核运行中打开、停止/完成结果、工具展开、宽窄布局与关闭返回；原生自动化的系统剪贴板读回未确认，精确源文由 Tester 校验。本批未重建安装器、未做打包/安装 smoke 或 macOS 验收，未读取 `.env` 或调用付费模型。

首次使用与 Windows 私有测试版首发已完成：空会话提供项目文件夹选择/确认和直接 DeepSeek 连接设置，选择不同目录新建会话并保留原草稿；安装版默认工作目录从安装位置切到用户主目录，已使用会话从持久 header 恢复项目。连接设置复用原生 Password 和已有配置通道，不依赖模型目录，首次打开读取安全凭据状态；小窗口使用 Scroll 并收起装饰标题，避免引导被输入框裁剪。发布脚本只生成具名安装器、公开证书、首次使用说明、发布说明和 SHA256SUMS；拒绝版本身份不符或未知输出文件。

本批本地检查：`CGO_ENABLED=0 go test ./...`（397 项）、`CGO_ENABLED=1 go test -race ./internal/app ./internal/view`（263 项）、`go tool mygo vet .`、`go vet ./...`、Node Host/脚本（88/88）、bundled runtime 完整真实组合通过。`TestAgainstMygoFirstUse` 验证项目选择、配置读取、mock 第一轮消息和第二 Host 的目录恢复；Windows 原生验证目录选择、连接保存/关闭、草稿、最小窗口及实际签名二进制。最终安装器 123147064 bytes（约 117.4 MiB）；`node scripts/prepare-release.mjs`、`node scripts/smoke-packaged.mjs`、`node scripts/smoke-installer.mjs` 通过，包括签名、空 PATH 首启、进程树回收、快捷方式/卸载项清理和会话保留。干净 VM、macOS、付费模型本轮未验收。

线上发布：[v0.1.0 私有测试版](https://github.com/denislov/dsh-mygo/releases/tag/v0.1.0) 于 2026-10-24 发布，目标提交 `cb5201422597f0c9ae7531d703ffe5533e545aed`，prerelease=true、draft=false；仓库保持 private。发布提交的 [Windows/Linux CI](https://github.com/denislov/dsh-mygo/actions/runs/40063014325) 全绿；5 个附件名称/大小/SHA256 与本地核对一致，版本 tag 指向目标提交。已从线上重新下载安装器并确认 SHA256 一致；首次使用指南、签名证书和校验和随 Release 分发。

0.1.1 二次启动修复已发布：用户安装包的 pi-ai `.manifest.json` 被 MyGo 点文件过滤排除，导致提供已保存 magpie 默认模型的插件无法激活。补齐原版匹配的公开模型清单后，用户原安装版连续两次正常启动并恢复同一原会话，模型、凭据和会话设置保持；问题不需要修改默认模型。构建流程将该固定清单适配为 `models-manifest.json` 并修正生成依赖引用，保留原始 JSON 字节；补丁版本同步 UI/Host，已有 profile 的应用自有 Host 包先备份再升级，用户配置与兼容开发链接保留。

本批检查：Go 全量、Host/App 竞态、MyGo/Go vet、Node（90/90）与 bundled runtime 完整真实组合通过；新增旧 Host 包升级和第二次启动的真实回归。最终 `go tool mygo build -skip-build-command` 与 `node scripts/smoke-packaged.mjs` 通过，实际导入打包后的 pi-ai provider，`node scripts/prepare-release.mjs` 校验本版签名并准备资产。安装器 123171808 bytes（约 117.5 MiB）；7-Zip 只读解包确认清单与修正后的引用均在安装器内，JSON 字节与源数据一致。0.1.1 原生窗口连续两次就绪并恢复同一会话。现有用户安装保留，未运行会创建/清理真实卸载项的 fresh-install smoke；干净 VM、macOS 与付费回合本轮未运行。

线上修复包：[v0.1.1 私有测试版](https://github.com/denislov/dsh-mygo/releases/tag/v0.1.1) 指向 `728a3edf49c0d8727a67d14f902feacd8fb7cc04`，prerelease=true、draft=false；[Windows/Linux CI](https://github.com/denislov/dsh-mygo/actions/runs/40077232233) 成功。5 个附件名称/大小/SHA256 与本地一致，重新下载线上安装器后校验一致；v0.1.0 发布页保留历史资产并提示使用修复版，仓库保持 private。

应用与 runtime 拆分已完成：应用安装器只含 Go 二进制、`host/` 与图标；`runtime/` 生成 Node + DSH 闭包 + 私有 `mygo-host` + 配套 Host bundle + manifest，确定性 `.tgz` 约 212 MiB，应用安装器约 4.3 MiB、exe 约 12.4 MiB。设备级 runtime store 支持 managed/external/legacy/direct、安全解压、GitHub catalog/下载（SHA256/size）、external `.cmd`/shim 修复与事务式切换。首启无 runtime 时进入全窗口引导（已安装 DSH、下载、导入 `.tgz`）；设置中心 Runtime 分区支持查看、Use/Remove、检测 DSH、从文件安装、检查更新；切换在候选 initialize 成功后才持久化选择，失败恢复旧 profile 与旧 Host。真实窗口用 UIA 验证外部 DSH → managed 切换，会话 `d5497fc2` 保留，当前运行时/路径/版本正确显示；560×420 引导与设置页可用。

本批实际检查：`go test ./...`（489）、`CGO_ENABLED=0 go test ./...`（489）、`go test -race ./internal/app ./internal/view ./internal/runtimes`（343）、`go tool mygo vet .`、`go vet ./...`、Node Host（84/84）、scripts（10/10）、`node scripts/test-real-host.mjs --runtime`、`go tool mygo build`、`node scripts/prepare-release.mjs`、`node scripts/prepare-runtime-release.mjs`、`node scripts/smoke-packaged.mjs`（源码 runtime 与解压后的发布 tgz 各一次）均通过。`smoke-installer` 因本机已安装 dsh-mygo 按设计拒绝运行；公开仓库下载链路因仓库仍为 private 未验收。干净 VM、macOS 仍需发布前确认。

## 7. 风险与依赖

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| DSH `0.2.1-rc.2` 精确 pin + 精确 peer 依赖 | 上游任何 patch 都可能破坏 Host | 保持握手版本展示；补 conformance 与升级 runbook；DSH stable 后评估放宽版本范围 |
| 打包复杂度（Node runtime 体积、原生模块 ABI、签名、公证、更新源） | M1/M4 延期，首发不可控 | 应用/runtime 已拆分；应用安装器 4.3 MB，runtime 独立 212 MB；签名自签名 + SHA256；macOS 与更新后续 |
| GitHub 运行时下载不可达/限流 | 新用户无法在线安装 runtime | 本地 `.tgz` 导入保底；catalog 缓存与明确重试错误；发布验收验证公开下载链路 |
| external DSH shim 路径变化 | 已保存的 existing DSH 启动失败 | PATH 重新探测且仅在同 DSH 版本时修复；版本不符回到引导 |
| `app.go` / `view.Model` 集中式状态 | 设置、会话、交互交叉越权，回归难定位 | 继续用状态机测试守边界；M2 前拆 controller/子模型 |
| 资源上限（16 MiB 帧、窗口展示/详情预算、8 个同时拥有句柄、目录总预算） | 单事件过大或极大目录仍会拒绝；DSH 冷恢复完整日志可能较慢 | 历史窗口与退休句柄回收控制 UI/Agent 累积；大日志真实回归，后续补定位与上游恢复基准 |
| macOS 零验证 | 不影响 Windows 首发，但 macOS 发布单元有风险 | macOS 发布前接 CI 与真机验收清单 |
| 平台验收矩阵不全 | 干净机器与 macOS 覆盖不足 | 版本 tag 与 Windows/Linux CI 已建立；Windows 干净 VM、macOS 发布前补矩阵 |
| 单窗口 + 后台交互不可见 | 多会话体验受限 | M3 支持后台审批/提问呈现 |
| 审批参数、路径可能含敏感信息 | 日志/崩溃报告泄露面 | 维持"只记身份与工具名"的日志规则；崩溃报告落地时专项评审 |

## 8. 决策记录

已定：

| 决策 | 选择 | 日期 |
| --- | --- | --- |
| 首发平台 | Windows x64；macOS 随后 | 2026-10-23 |
| 分发形态 | 应用安装器只含本体；Node + DSH + 私有 `mygo-host` + 配套 Host bundle 作为独立 `.tgz`（含 runtime 副本） | 2026-10-25 |
| 会话管理 | 0.1 只做固定侧栏 + 重命名 + 归档；delete/fork 后置 | 2026-10-23 |
| 仓库 | GitHub `denislov/dsh-mygo` | 2026-10-23 |
| 应用标识 | `com.denislov.dshmygo`（发布后不再更改） | 2026-10-23 |
| 首发优先级与渠道 | 先交付首次使用闭环与 Windows x64 测试版；仓库转为公开，应用与 runtime 使用独立 GitHub Releases | 2026-10-25 |
| 崩溃恢复 | 用户触发重启 + `session/resume` + history 回放；不做自动重连 | 2026-10-23 |
| 运行时来源 | managed `.tgz`（npm `@deepseek-ai/dsh` 精确 pin + 私有 `mygo-host`）或用户已有精确版本 DSH；legacy 仅作候选 | 2026-10-25 |
| 运行时兼容 | `protocolVersion` 精确 + `appMin <= app < appMaxExclusive` + Host `engines.dsh`；切换成功 initialize 后才提交选择 | 2026-10-25 |
| Windows 代码签名 | 自签名证书（零成本）；接受 Unknown publisher/SmartScreen 提示；后续若开源可评估 OSS 签名 | 2026-10-23 |

待定：

1. 应用自动更新器与 delta；runtime 增量/镜像源、detached signature。
2. 0.1 范围：账户登录、多 provider、timed 提问是否进入首个安装版。
3. 遗留代码策略：SDK 回归链路保留到何时，`cmd/spike` 是否移除。
4. macOS 发布窗口、runtime 产物与公证凭证。

## 9. 0.1 完成定义

- 干净 Windows 机器安装后，用本地 runtime `.tgz` 或已安装的精确版本 DSH 完成离线首启，不要求应用安装器自带 Node/DSH。
- 完成一次真实回合；Host 崩溃可恢复；应用退出/被强杀后无 Host/插件子进程残留；日志可导出。
- 会话、模型、凭据闭环在安装版可复现。
- Windows Authenticode 签名可验证（自签名；接受 Unknown publisher/SmartScreen 提示）；卸载保留 `$DSH_HOME`。
- Windows 安装/卸载 smoke 与手测矩阵通过。
- CI 全绿、有 release tag 与发布说明；若带自动更新，升级与回滚 smoke 通过。

macOS 不在 0.1 范围，作为后续发布单元。

不在 0.1 范围（除非另行拍板）：Web/Electron 兼容、插件市场 UI、远程访问、Linux 首发、多窗口、macOS 安装包。

## 相关文档

- [架构](architecture.md)
- [通道协议](subsystem/channel-protocol.md)
- [会话投影](subsystem/session-projection.md)
- [运行时管理](subsystem/runtime-manager.md)
- [打包与更新](subsystem/packaging-updates.md)
- [测试](subsystem/testing.md)
- [文档索引](README.md)
