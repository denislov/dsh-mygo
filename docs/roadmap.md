# 开发进度与路线图

> 代码基线：`caab7d9`（2026-10-23 10:29）；决策更新于 2026-10-23。设计文档（`docs/architecture.md` 与 `docs/subsystem/`）描述**目标态**；本页只记录当前进度、目标差距、里程碑与验收。实现契约仍以设计文档为准；更新进度时改这里，不把同一份状态复制到其他文档。

## 1. 结论

项目在约 11 小时内从零走到"核心聊天闭环全部跑通"：自定义 channel、流式、回合取消、审批、提问、历史/会话、模型与凭据设置均已落地，并有真实 DSH Loader（`0.2.1-rc.2`）集成测试覆盖。当前处于**阶段 C（产品化）前半段**。

一句话：**能开发、能演示、能在开发机上真实跑；还不能交给普通用户安装使用。**

判断依据：协议与 UI 主链路已经闭环，M0 工程基线、M1 运行时/打包 smoke 与手工崩溃恢复已完成；安装签名、自动重同步、会话管理 UI 与 Windows 发布工程仍是主要缺口；macOS 作为后续发布单元。

## 2. 事实快照

| 项 | 值 |
| --- | --- |
| 提交 | 25 个，2026-10-22 15:00 → 2026-10-23 10:29（约 19.5 小时，含中断） |
| 规模 | 136 个跟踪文件；Go 生产 6.1k 行 / Go 测试 5.8k 行；Node Host 1.1k 行 / 测试 1.0k 行；`docs/` 19 篇约 3.1k 行；runtime lock 约 400 KB |
| 版本 | identifier `com.denislov.dshmygo`；app/host `0.1.0`、`protocolVersion 1`、DSH `0.2.1-rc.2`（精确绑定）、MyGo `v0.6.2`、Node >= 24、runtime Node `24.14.0` |
| 测试 | 175 个 Go Test 入口（233 项通过、24 项因缺少真实 Host 环境跳过）+ 46 个 Node test；覆盖率 app 84%、view 89%、interaction 87%、projection 83%、channel 72%、host 70%、main 8.5% |
| 构建产物 | `build/windows-amd64/` 重建于 2026-10-23 10:22，含 `resources/host` 与 `resources/runtime`（Node 24.14.0 + DSH 0.2.1-rc.2）；安装器 117.1 MB，应用 11.5 MB |
| 工程基线 | CI 已建立（Windows 全量 + Linux Go）；remote 已建立（私有）；无 tag；版本单点为 `versions.json` |

2026-10-23 本机（Windows）实跑结果：

```sh
go build ./...                                  # 通过
go test ./...                                   # 通过（未设 DSH_MYGO_DSH_DIR 时真实链路自动跳过）
go test -race ./...                             # 通过
go tool mygo vet .                              # 通过
node --test resources/host/tests/*.test.mjs     # 46/46 通过
DSH_MYGO_DSH_DIR=D:/Workspace/deepseek-harness \
  go test ./internal/app -v                     # 真实 Loader 组全部通过
node scripts/build-runtime.mjs                  # bundled runtime 构建
node scripts/test-real-host.mjs --runtime       # bundled runtime 真实组合通过
go tool mygo build                              # 含 runtime 的安装器
node scripts/smoke-packaged.mjs                 # 打包资源 + 干净首启 + 日志
```

真实链路覆盖：composition/lifecycle、approval、question、cancellation、streaming/Markdown、history 重启回放、session switching、model settings、configuration、host kill/restart 与会话 resume。

未跑：真实付费模型 `TestAgainstMygoRealModel`（需要 key 且产生调用费用）、macOS 全部验证、安装包/签名/更新验证。

## 3. 已完成（按子系统）

| 子系统 | 已落地 | 主要证据 |
| --- | --- | --- |
| 进程与协议 | spawn/stdio/stderr 尾、版本+能力协商、15 个方法、有界帧、通知背压、非法帧关闭 | `internal/host`、`internal/channel`、`resources/host/transport.mjs`、`tests/fixtures/protocol/` |
| 会话 | create/list/resume/history、`nextSeq` 起点、单写者、分页游标与预算、冷恢复 | `resources/host/server.mjs`、`internal/app/session_reader.go`、`TestMygoRealHistoryAfterRestart` |
| 流式 | `session/assistant-stream`、临时行、提交替换、缺口/abandoned/截断 | `internal/projection/stream.go`、`TestAgainstMygoRealStreaming` |
| 人机交互 | approval 与 question 的 answerer、FIFO 队列、超时/撤销/过期身份、多问题表单 | `internal/interaction`、`TestAgainstMygoRealApprovals`、`TestAgainstMygoQuestions` |
| 投影与渲染 | user/assistant/tool/diff/notice、原生 Markdown 流式与代码复制、工具折叠与只读 hunk | `internal/projection`、`internal/view/markdown.go`、`tools.go` |
| 会话 UI | Sessions 弹窗、新建/刷新/恢复、草稿隔离、切换后整体替换、窄窗口适配 | `internal/view/sessions.go`、`TestAgainstMygoSessionSwitching` |
| 模型与设置 | `model.selection` 目录与 effort、`settings.configuration` 默认模型（revision）、DeepSeek 凭据只写 | `internal/app/settings.go`、`configuration.go`、`TestAgainstMygoConfiguration` |
| 回归入口 | 旧 SDK 链路保留为显式 `-sdk-protocol -profile sdk` | `main.go`、`testdata/fake-sdk-host.mjs` |
| 工程基线 | 正式 identifier、版本单点、Windows/Linux CI、构建产物重建 | `versions.json`、`internal/version`、`.github/workflows/ci.yml` |
| 运行时打包 | bundled Node + DSH 闭包 + 私有 `mygo-host` + 首启 profile seed + manifest 版本校验 | `resources/runtime/`、`scripts/build-runtime.mjs`、`internal/host/profile.go`、`internal/host/launch.go` |
| 打包 smoke | 打包资源 composition/restart、干净首启、session 与应用日志 | `scripts/smoke-packaged.mjs` |
| 崩溃恢复 | 恢复对话框、手工 Restart、`session/resume` + history 回放、`PathLogs` 日志与崩溃报告 | `internal/app/app.go`、`internal/app/crash.go`、`internal/view/recovery.go`、`TestAgainstMygoRestart` |

## 4. 缺口

### 4.1 发布阻塞（没有它不能称为产品）

- **运行时打包与分发**：`resources/runtime` 已有 bundled Node、DSH 闭包与私有 `mygo-host`，`PathResources` 解析、首启 profile seed 与 manifest 启动前版本校验可用；仍缺 profile 损坏修复与 macOS 产物。
- **安装包与签名**：`go tool mygo build` 的产物已包含 `resources/runtime`，打包目录 smoke 覆盖首启/真实回合/日志；仍无 Windows 安装器 smoke、Authenticode 签名、macOS Developer ID/公证。
- **崩溃恢复**：窗口恢复对话框、手工 Restart、`session/resume` + history 回放、`PathLogs` 日志与崩溃报告已落地；自动重启退避、崩溃循环保护、日志轮转与 `seq` 缺口自动 resync（M3）未做。
- **进程回收**：Windows Job Object 未落地（仅有进程句柄回收）；POSIX 进程组已实现。插件子进程泄漏风险仍在。
- **发布工程**：无 tag、无三平台矩阵；真实 Host 测试只能靠开发者本地环境变量，无法在 CI 常态执行（CI 已覆盖 Windows 全量与 Linux Go 测试）。

### 4.2 产品体验缺口（能用，但不像完整桌面客户端）

- **会话侧栏与管理**：仍是弹窗；无固定侧栏、搜索、重命名、归档、删除、fork。Host 侧 `session/rename|archive|delete|fork` 方法尚未实现；列表 `title` 为空、`updatedAt` 等于 `createdAt`，没有按最近活动排序。
- **权限模式**：只有 `DSH_PERMISSION_MODE` 环境变量，UI 不能展示或切换，也没有危险模式确认。
- **UI 偏好**：主题、语言、快捷键、最近会话、跨重启草稿未持久化（窗口尺寸/位置由 MyGo `StateKey` 处理）。
- **附件与引用**：图片粘贴、文件拖放、`@path` 引用、`/` 命令均未实现；协议里 `attachments` 仍是后续契约。
- **深度信息**：usage/context/compaction 不展示；`subagent/*` 协议表有定义但 Host 不发送、UI 不投影；plan/todo/goal 未投影。
- **会话形态**：只支持单活跃会话；运行中会话不能 attach；后台会话的审批/提问不呈现；最多 8 个已拥有会话。
- **本地化**：UI 文案硬编码英文（约 75 处 label 引用），握手却固定 `zh-CN`，没有 i18n 层。

### 4.3 协议与健壮性缺口

- 实时缺口自动 resync（现在直接停 Host，需重启应用）。
- RPC 请求级取消（`$/cancel`），超时只能丢弃结果，不能撤销请求。
- 流式重连 baseline（`revision` + `activeAttempt`）。
- 历史增量向上加载；现在是全量回放，受 32 MiB / 20k 事件 / 1024 页预算限制，大会话可能打不开。
- `session/queue`、`session/wait`、`session/working-directory/*`、`settings/get|set`、任意命名空间设置。
- timed/continued 提问（当前 `tool-ask-user` 只有 `mode: legacy` 阻塞模式）。
- 大附件方案（16 MiB 单帧上限下的分片/路径引用）。

### 4.4 工程与测试缺口

- 并发/竞态只在本地跑过；无三平台矩阵；CI 不含真实 Host 组合（本地可用 source checkout 或 bundled runtime 跑）。
- macOS 零验收（Metal、输入法、字体、菜单、剪贴板、签名/公证）。
- 无打包测试、无安装/升级/卸载 smoke、无更新测试。
- `internal/app/app.go` 约 700 行、`view.Model` 是承载 60+ 字段的扁平结构，设置/会话/交互状态交叉增长；目前靠测试守边界，继续加功能前需要拆分。
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

- 运行时打包（已完成）：`resources/runtime/{node,node_modules,mygo-host.mjs,manifest.json}`；私有 `mygo-host`（`runProfile`）入口；首启从 `resources/host` 创建 profile；`mygo.App.Path(PathResources)` 解析；manifest 启动前版本校验；离线可用。profile 损坏修复仍待补。
- 打包 smoke（已完成）：`scripts/smoke-packaged.mjs` 覆盖打包资源 composition/restart、干净首启、session 与应用日志。
- 安装与签名：Windows per-user 安装器 + Authenticode；UI/Host/bundle 版本一致校验。macOS 的 `.app`/`.dmg` + Developer ID + 公证放到 macOS 发布单元。
- 生命周期收口（部分完成）：恢复对话框、手工重启、`PathLogs` 日志与崩溃报告已落地；Windows Job Object、自动重启退避与版本不匹配提示仍待补。
- 验收：干净 Windows 机器安装 → 离线首启 → 完成一个 mock 回合；`kill` Host 可恢复；升级/卸载保留 `$DSH_HOME`。

### M2 日常可用（1–2 周）

目标：像桌面客户端，而不是演示程序。

- 固定侧栏：会话常驻列表、搜索、新建；窄窗口降级为弹窗。
- 会话管理：Host 侧 `rename/archive` + 事件 + UI；标题、`updatedAt`、按最近活动排序；delete/fork 后置。
- 权限模式 UI：展示与切换（走 Host，不本地改环境变量），`danger-full-access` 明确确认。
- UI 偏好持久化：主题/语言/快捷键/最近会话/草稿，原子写；i18n 决策落地。
- 附件与引用：图片（类型白名单、base64 限额）、`@path`、拖放；`/` 命令先留扩展。
- 深度信息：usage/context 最小展示；subagent/plan/todo 先做投影。

验收：560×420 到宽屏都可用；"找会话 → 改名 → 切模型 → 发图 → 审批/提问 → 停止"全流程通过；错误恢复不丢草稿。

### M3 健壮性与深度（1–2 周）

目标：长时间、大仓库、多会话下不丢真相。

- 自动 resync：`seq` 缺口用 history 补齐，`resyncing` 状态，不杀 Host；流式 baseline 重连。
- `$/cancel` 请求级取消与超时分级。
- 历史增量加载（向上滚动），突破全量回放预算。
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

1. **M1-4 安装与签名**（3–5 天）：Windows per-user 安装器 + Authenticode；干净机器离线首启验收；Windows Job Object。
2. **M2-1 会话管理后端**（2–3 天）：`session/rename` + `session/archive` + 列表 `title/updatedAt`；随后接侧栏第一版。

## 7. 风险与依赖

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| DSH `0.2.1-rc.2` 精确 pin + 精确 peer 依赖 | 上游任何 patch 都可能破坏 Host | 保持握手版本展示；补 conformance 与升级 runbook；DSH stable 后评估放宽版本范围 |
| 打包复杂度（Node runtime 体积、原生模块 ABI、签名、公证、更新源） | M1/M4 延期，首发不可控 | 先做 Windows 最小可分发包，再补 macOS 与更新；证书/更新源提前确认 |
| `app.go` / `view.Model` 集中式状态 | 设置、会话、交互交叉越权，回归难定位 | 继续用状态机测试守边界；M2 前拆 controller/子模型 |
| 资源上限（16 MiB 帧、32 MiB 历史、4 MiB 工具详情、8 会话） | 长会话/大仓库不可用 | M3 增量加载与更细预算；用真实大会话做基准 |
| macOS 零验证 | 不影响 Windows 首发，但 macOS 发布单元有风险 | macOS 发布前接 CI 与真机验收清单 |
| 无 tag / 三平台矩阵 | 发布不可追溯、平台覆盖不足 | M0 已建立远端与 CI；补 tag 与矩阵 |
| 单窗口 + 后台交互不可见 | 多会话体验受限 | M3 支持后台审批/提问呈现 |
| 审批参数、路径可能含敏感信息 | 日志/崩溃报告泄露面 | 维持"只记身份与工具名"的日志规则；崩溃报告落地时专项评审 |

## 8. 决策记录

已定：

| 决策 | 选择 | 日期 |
| --- | --- | --- |
| 首发平台 | Windows x64；macOS 随后 | 2026-10-23 |
| 分发形态 | 自带 Node + DSH runtime + 私有 `mygo-host` | 2026-10-23 |
| 会话管理 | 0.1 只做固定侧栏 + 重命名 + 归档；delete/fork 后置 | 2026-10-23 |
| 仓库 | GitHub `denislov/dsh-mygo` | 2026-10-23 |
| 应用标识 | `com.denislov.dshmygo`（发布后不再更改） | 2026-10-23 |
| 崩溃恢复 | 用户触发重启 + `session/resume` + history 回放；不做自动重连 | 2026-10-23 |
| 运行时来源 | npm `@deepseek-ai/dsh` 精确 pin + 私有 `resources/runtime/mygo-host.mjs` | 2026-10-23 |

待定：

1. 更新基础设施：GitHub Releases / 自有源；首版是否做 delta。
2. 0.1 范围：账户登录、多 provider、timed 提问是否进入首个安装版。
3. 遗留代码策略：SDK 回归链路保留到何时，`cmd/spike` 是否移除。
4. macOS 发布窗口与公证凭证。

## 9. 0.1 完成定义

- 干净 Windows 机器安装后离线启动，不要求预装 Node/DSH。
- 完成一次真实回合；Host 崩溃可恢复；日志可导出。
- 会话、模型、凭据闭环在安装版可复现。
- Windows 签名通过；卸载保留 `$DSH_HOME`。
- Windows 手测矩阵通过。
- CI 全绿、有 release tag 与发布说明；若带自动更新，升级与回滚 smoke 通过。

macOS 不在 0.1 范围，作为后续发布单元。

不在 0.1 范围（除非另行拍板）：Web/Electron 兼容、插件市场 UI、远程访问、Linux 首发、多窗口、macOS 安装包。

## 相关文档

- [架构](architecture.md)
- [通道协议](subsystem/channel-protocol.md)
- [会话投影](subsystem/session-projection.md)
- [打包与更新](subsystem/packaging-updates.md)
- [测试](subsystem/testing.md)
- [文档索引](README.md)
